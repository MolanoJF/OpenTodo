package calfeed

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/OpenTodoApp/opentodo/internal/caldav"
)

// FuzzSeriesEngine feeds the engine objects it did not write: it must never panic,
// and whatever it hands back to be written must read back unchanged.
func FuzzSeriesEngine(f *testing.F) {
	dirs, _ := filepath.Glob(filepath.Join("testdata", "series", "*", "in.ics"))
	for _, in := range dirs {
		if b, err := os.ReadFile(in); err == nil {
			f.Add(b, "20261112T060000Z", uint8(0))
		}
	}
	instances := []string{"20261112T060000Z", "20261116T060000Z", "20261001T050000Z", "20261112", "20261112T070000", "20261113T060000Z", "x"}
	now := time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
	f.Fuzz(func(t *testing.T, data []byte, instance string, pick uint8) {
		root, err := caldav.Parse(data)
		if err != nil {
			return
		}
		_, _ = ReadSeries(root)
		_ = SeriesLocked(root, "e")
		if pick%2 == 0 {
			instance = instances[int(pick/2)%len(instances)]
		}
		for _, scope := range []Scope{ScopeThis, ScopeFollowing, ScopeAll} {
			start := Point{T: now, Zone: "UTC"}
			ch := SeriesChange{Summary: ptr("x"), When: &Span{Start: start, End: Point{T: now.Add(time.Hour), Zone: "UTC"}}, Reminders: &[]int{10}}
			cp, err := caldav.Parse(data)
			if err != nil {
				return
			}
			res, err := ApplySeries(SeriesEdit{Root: cp, Instance: instance, Scope: scope, Change: ch, NewUID: "ot-new@opentodo", Now: now})
			if err == nil {
				for _, o := range res.Objects {
					if !RoundTrips(o.Root) {
						t.Fatalf("scope %s left an object that does not read back unchanged", scope)
					}
				}
			}
			cp, _ = caldav.Parse(data)
			_, _ = DeleteSeries(SeriesEdit{Root: cp, Instance: instance, Scope: scope, Now: now})
		}
	})
}
