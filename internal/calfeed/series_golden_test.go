package calfeed

import (
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/OpenTodoApp/opentodo/internal/caldav"
)

var update = flag.Bool("update", false, "rewrite the series golden files")

// A golden case is a directory of testdata/series: in.ics, change.json and the
// files the engine's answer is compared with (out.ics, out_new.ics, result.json).

type goldWhen struct {
	Date, Time, TZ string
}

type goldSpan struct{ Start, End goldWhen }

type goldChange struct {
	Op        string // apply (default) or delete
	Scope     Scope
	Instance  string
	Now       string
	NewUID    string
	Summary   *string
	Location  *string
	Desc      *string
	Free      *bool
	When      *goldSpan
	Repeat    json.RawMessage // absent: unchanged, null: stop repeating
	Reminders *[]int
	Error     string // the engine's error expected
}

func (w goldWhen) point(t *testing.T) Point {
	t.Helper()
	d, err := time.Parse("2006-01-02", w.Date)
	if err != nil {
		t.Fatalf("bad date %q", w.Date)
	}
	if w.Time == "" {
		return Point{T: d, AllDay: true}
	}
	hm, err := time.Parse("15:04", w.Time)
	if err != nil {
		t.Fatalf("bad time %q", w.Time)
	}
	switch w.TZ {
	case "":
		return Point{T: time.Date(d.Year(), d.Month(), d.Day(), hm.Hour(), hm.Minute(), 0, 0, time.UTC)}
	case "UTC":
		return Point{T: time.Date(d.Year(), d.Month(), d.Day(), hm.Hour(), hm.Minute(), 0, 0, time.UTC), Zone: "UTC"}
	}
	loc, ok := Resolve(w.TZ)
	if !ok {
		t.Fatalf("unknown zone %q", w.TZ)
	}
	return Point{T: time.Date(d.Year(), d.Month(), d.Day(), hm.Hour(), hm.Minute(), 0, 0, loc), Zone: w.TZ}
}

func (c goldChange) edit(t *testing.T, root *caldav.Component) SeriesEdit {
	t.Helper()
	now := time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
	if c.Now != "" {
		var err error
		if now, err = time.Parse(time.RFC3339, c.Now); err != nil {
			t.Fatal(err)
		}
	}
	e := SeriesEdit{Root: root, Instance: c.Instance, Scope: c.Scope, NewUID: c.NewUID, Now: now,
		Change: SeriesChange{Summary: c.Summary, Location: c.Location, Description: c.Desc, Free: c.Free, Reminders: c.Reminders}}
	if c.When != nil {
		e.Change.When = &Span{Start: c.When.Start.point(t), End: c.When.End.point(t)}
	}
	if len(c.Repeat) > 0 {
		var rep *Repeat
		if string(c.Repeat) != "null" {
			rep = &Repeat{}
			if err := json.Unmarshal(c.Repeat, rep); err != nil {
				t.Fatal(err)
			}
		}
		e.Change.Repeat = &rep
	}
	return e
}

var goldErrors = map[string]error{
	"ErrShiftUnsupported":  ErrShiftUnsupported,
	"ErrNoSuchInstance":    ErrNoSuchInstance,
	"ErrRepeatThis":        ErrRepeatThis,
	"ErrSeriesUnsupported": ErrSeriesUnsupported,
	"ErrRepeatStart":       ErrRepeatStart,
}

// lf makes a golden comparison independent of the checkout's line endings.
func lf(b []byte) string { return strings.ReplaceAll(string(b), "\r\n", "\n") }

func check(t *testing.T, path string, got []byte) {
	t.Helper()
	if *update {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s: %v (run with -update)", path, err)
	}
	if lf(want) != lf(got) {
		t.Errorf("%s differs:\n--- got ---\n%s\n--- want ---\n%s", path, lf(got), lf(want))
	}
}

type goldResult struct {
	DeleteOrigin bool   `json:"deleteOrigin"`
	UID          string `json:"uid"`
	Instance     string `json:"instance"`
	Refetch      bool   `json:"refetch"`
	Objects      int    `json:"objects"`
}

func runCase(t *testing.T, dir string) (SeriesEdit, SeriesResult, *caldav.Component, error) {
	t.Helper()
	in, err := os.ReadFile(filepath.Join(dir, "in.ics"))
	if err != nil {
		t.Fatal(err)
	}
	root, err := caldav.Parse(in)
	if err != nil {
		t.Fatal(err)
	}
	var ch goldChange
	raw, err := os.ReadFile(filepath.Join(dir, "change.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &ch); err != nil {
		t.Fatal(err)
	}
	e := ch.edit(t, root)
	var res SeriesResult
	if ch.Op == "delete" {
		res, err = DeleteSeries(e)
	} else {
		res, err = ApplySeries(e)
	}
	if ch.Error != "" {
		if !errors.Is(err, goldErrors[ch.Error]) {
			t.Fatalf("error = %v, want %s", err, ch.Error)
		}
		return e, res, root, err
	}
	if err != nil {
		t.Fatal(err)
	}
	return e, res, root, nil
}

func TestSeriesGolden(t *testing.T) {
	dirs, err := filepath.Glob(filepath.Join("testdata", "series", "*"))
	if err != nil || len(dirs) == 0 {
		t.Fatalf("no series fixtures: %v", err)
	}
	for _, dir := range dirs {
		if strings.Contains(filepath.Base(dir), "locked-") {
			continue
		}
		t.Run(filepath.Base(dir), func(t *testing.T) {
			_, res, _, err := runCase(t, dir)
			if err != nil {
				return
			}
			out := goldResult{DeleteOrigin: res.DeleteOrigin, UID: res.UID, Instance: res.Instance, Refetch: res.Refetch, Objects: len(res.Objects)}
			js, _ := json.MarshalIndent(out, "", "  ")
			check(t, filepath.Join(dir, "result.json"), append(js, '\n'))
			for _, ob := range res.Objects {
				name := "out.ics"
				if ob.New {
					name = "out_new.ics"
				}
				check(t, filepath.Join(dir, name), ob.Root.Encode())
			}
		})
	}
}
