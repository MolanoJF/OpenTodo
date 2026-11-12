package calfeed

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/OpenTodoApp/opentodo/internal/caldav"
)

// The oracle tests of design D18: for every golden case, the occurrences the
// engine's output expands to equal the occurrences of the input with the change
// applied, computed here from the expansions and the wall clock, not from the
// engine's own re-writing of the object.

type seen struct {
	Start, End Point
	Summary    string
}

// expandObject expands an object over a window around its first event.
func expandObject(t *testing.T, root *caldav.Component, win Window) (map[string]seen, []Event) {
	t.Helper()
	cal, err := ParseCalendar(root.Encode())
	if err != nil || len(cal.Events) == 0 {
		t.Fatalf("expand: %v", err)
	}
	occ, _ := Expand(cal.Events, win, 5000)
	out := map[string]seen{}
	for _, o := range occ {
		out[o.Instance] = seen{o.Start, o.End, o.Summary}
	}
	return out, cal.Events
}

func (s seen) String() string {
	return fmt.Sprintf("%s..%s", civil(s.Start).Format("2006-01-02T15:04:05"), civil(s.End).Format("2006-01-02T15:04:05"))
}

// oracleTrim leaves the days at the window's edges out of a comparison: whether an
// occurrence there is in the window depends on its form.
var oracleTrim [2]time.Time

func inside(s seen) bool {
	c := civil(s.Start)
	return oracleTrim[0].IsZero() || (c.After(oracleTrim[0]) && c.Before(oracleTrim[1]))
}

func sortedSpans(m map[string]seen) []string {
	var out []string
	for _, s := range m {
		if !inside(s) {
			continue
		}
		out = append(out, s.String())
	}
	sort.Strings(out)
	return out
}

func spansOf(list ...seen) string {
	var out []string
	for _, s := range list {
		if !inside(s) {
			continue
		}
		out = append(out, s.String())
	}
	sort.Strings(out)
	return strings.Join(out, "\n")
}

func TestSeriesOracle(t *testing.T) {
	dirs, _ := filepath.Glob(filepath.Join("testdata", "series", "*"))
	for _, dir := range dirs {
		name := filepath.Base(dir)
		if strings.Contains(name, "locked-") {
			continue
		}
		t.Run(name, func(t *testing.T) {
			var ch goldChange
			raw, _ := os.ReadFile(filepath.Join(dir, "change.json"))
			if err := json.Unmarshal(raw, &ch); err != nil {
				t.Fatal(err)
			}
			if ch.Error != "" || len(ch.Repeat) > 0 {
				t.Skip("an error or a new repeat has no oracle here")
			}
			in := loadFixture(t, name)
			first, _ := loadSeries(in, "")
			from := first.mev.Start.T.AddDate(0, 0, -2)
			win := Window{From: from, To: from.AddDate(0, 0, 220)}
			oracleTrim = [2]time.Time{civil(Point{T: win.From.AddDate(0, 0, 4)}), civil(Point{T: win.To.AddDate(0, 0, -4)})}
			before, evs := expandObject(t, in, win)
			_, res, _, _ := runCase(t, dir)
			if res.DeleteOrigin {
				return
			}
			var old, fresh map[string]seen
			for _, ob := range res.Objects {
				m, _ := expandObject(t, ob.Root, win)
				if ob.New {
					fresh = m
				} else {
					old = m
				}
			}
			switch {
			case ch.Op == "delete" && ch.Scope == ScopeThis:
				want := copySeen(before)
				delete(want, ch.Instance)
				sameSpans(t, "delete this", old, want)
			case ch.Op == "delete" && ch.Scope == ScopeFollowing:
				want := map[string]seen{}
				for k, v := range before {
					if keyTime(k).Before(keyTime(ch.Instance)) {
						want[k] = v
					}
				}
				sameSpans(t, "delete following", old, want)
			case ch.Scope == ScopeThis:
				want := copySeen(before)
				o := want[ch.Instance]
				if ch.When != nil {
					o.Start, o.End = ch.When.Start.point(t), ch.When.End.point(t)
				}
				if ch.Summary != nil {
					o.Summary = *ch.Summary
				}
				want[ch.Instance] = o
				sameSpans(t, "this", old, want)
				if got := old[ch.Instance]; got.Summary != o.Summary {
					t.Errorf("summary %q, want %q", got.Summary, o.Summary)
				}
			case ch.Scope == ScopeAll || (ch.Scope == ScopeFollowing && fresh == nil):
				oracleAll(t, ch, before, evs, old)
			case ch.Scope == ScopeFollowing:
				oracleFollowing(t, ch, before, evs, old, fresh)
			}
		})
	}
}

func copySeen(m map[string]seen) map[string]seen {
	out := make(map[string]seen, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func sameSpans(t *testing.T, what string, got, want map[string]seen) {
	t.Helper()
	if g, w := strings.Join(sortedSpans(got), "\n"), strings.Join(sortedSpans(want), "\n"); g != w {
		t.Errorf("%s: occurrences differ: %s", what, diffLines(g, w))
	}
}

// followers says which occurrences follow the series when it moves: the ones
// without an exception, and the exceptions that sat at their slot and had the
// master's length; an exception moved by hand keeps its time.
func followers(before map[string]seen, evs []Event) (follow map[string]bool, masterDur time.Duration) {
	exc := map[string]bool{}
	for _, ev := range evs {
		if ev.RecurrenceID != "" {
			exc[ev.RecurrenceID] = true
		}
	}
	for k, o := range before {
		if !exc[k] {
			masterDur = spanDur(o.Start, o.End)
			break
		}
	}
	follow = map[string]bool{}
	for k, o := range before {
		slot := keyTime(k)
		atSlot := slot.Equal(o.Start.T) && spanDur(o.Start, o.End) == masterDur
		follow[k] = !exc[k] || atSlot
	}
	return follow, masterDur
}

func oracleAll(t *testing.T, ch goldChange, before map[string]seen, evs []Event, got map[string]seen) {
	t.Helper()
	want := copySeen(before)
	if ch.When != nil {
		w := Span{Start: ch.When.Start.point(t), End: ch.When.End.point(t)}
		x := before[ch.Instance]
		follow, masterDur := followers(before, evs)
		d := wallDiff(w.Start, x.Start)
		newDur := masterDur + (spanDur(w.Start, w.End) - spanDur(x.Start, x.End))
		if kindOfPoint(w.Start) != kindOfPoint(x.Start) && (w.Start.AllDay || x.Start.AllDay) {
			newDur = spanDur(w.Start, w.End)
		}
		want = map[string]seen{}
		for k, o := range before {
			if follow[k] {
				s := fromCivil(civil(o.Start).Add(d), w.Start)
				o = seen{Start: s, End: endFrom(s, newDur), Summary: o.Summary}
			}
			want[k] = o
		}
	}
	sameSpans(t, "all", got, want)
}

func oracleFollowing(t *testing.T, ch goldChange, before map[string]seen, evs []Event, old, fresh map[string]seen) {
	t.Helper()
	split := keyTime(ch.Instance)
	x := before[ch.Instance]
	n, end := x.Start, x.End
	if ch.When != nil {
		n, end = ch.When.Start.point(t), ch.When.End.point(t)
	}
	newDur := spanDur(n, end)
	follow, _ := followers(before, evs)
	slotZone := x.Start.Zone
	delta := wallDiff(n, Point{T: split, Zone: slotZone, AllDay: x.Start.AllDay})
	wantOld := map[string]seen{}
	var wantNew []seen
	for k, o := range before {
		switch {
		case keyTime(k).Before(split):
			wantOld[k] = o
		case k == ch.Instance:
			wantNew = append(wantNew, seen{n, endFrom(n, newDur), ""})
		case follow[k]:
			s := fromCivil(civil(o.Start).Add(delta), n)
			wantNew = append(wantNew, seen{s, endFrom(s, newDur), ""})
		default:
			wantNew = append(wantNew, o)
		}
	}
	sameSpans(t, "old part", old, wantOld)
	if g, w := spansOf(seenList(fresh)...), spansOf(wantNew...); g != w {
		t.Errorf("new part differs: %s", diffLines(g, w))
	}
	for k := range old {
		if !keyTime(k).Before(split) {
			t.Errorf("the old series still has %s", k)
		}
	}
}

func seenList(m map[string]seen) []seen {
	var out []seen
	for _, s := range m {
		out = append(out, s)
	}
	return out
}

// diffLines lists the lines only one of two texts has.
func diffLines(got, want string) string {
	in := func(list []string) map[string]int {
		m := map[string]int{}
		for _, l := range list {
			m[l]++
		}
		return m
	}
	g, w := strings.Split(got, "\n"), strings.Split(want, "\n")
	gm, wm := in(g), in(w)
	var out []string
	for _, l := range g {
		if wm[l] == 0 {
			out = append(out, "got only:  "+l)
		}
	}
	for _, l := range w {
		if gm[l] == 0 {
			out = append(out, "want only: "+l)
		}
	}
	return strings.Join(out, "\n")
}

// TestRepeatChangeOracle checks the objects a new repeat leaves: the series that
// has it expands to the days the form says, from the occurrence's own start, and
// a stopped repeat leaves one event.
func TestRepeatChangeOracle(t *testing.T) {
	for _, name := range []string{"following-new-repeat", "all-new-repeat-drops-exceptions", "following-stop-repeating", "following-stop-repeating-first"} {
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join("testdata", "series", name)
			var ch goldChange
			raw, _ := os.ReadFile(filepath.Join(dir, "change.json"))
			if err := json.Unmarshal(raw, &ch); err != nil {
				t.Fatal(err)
			}
			_, res, _, _ := runCase(t, dir)
			obj := res.Objects[len(res.Objects)-1]
			if len(res.Objects) == 2 {
				obj = res.Objects[0] // the new series comes first
			}
			cal, _ := ParseCalendar(obj.Root.Encode())
			master := cal.Events[0]
			win := Window{From: master.Start.T.AddDate(0, 0, -1), To: master.Start.T.AddDate(0, 0, 200)}
			occ, _ := Expand(cal.Events, win, 5000)
			if string(ch.Repeat) == "null" {
				if len(occ) != 1 || master.RRule != "" || len(master.ExDates) != 0 || len(cal.Events) != 1 {
					t.Fatalf("a stopped repeat leaves %d occurrences, rule %q, %d events", len(occ), master.RRule, len(cal.Events))
				}
				return
			}
			var r Repeat
			if err := json.Unmarshal(ch.Repeat, &r); err != nil {
				t.Fatal(err)
			}
			want := bruteForce(r.Normalize(master.Start), master.Start, win.To)
			var got []string
			for _, o := range occ {
				got = append(got, civil(o.Start).Format("2006-01-02T15:04"))
			}
			if strings.Join(got, ",") != strings.Join(want, ",") || len(cal.Events) != 1 {
				t.Errorf("occurrences differ\n got  %v\n want %v (%d events)", got, want, len(cal.Events))
			}
		})
	}
}
