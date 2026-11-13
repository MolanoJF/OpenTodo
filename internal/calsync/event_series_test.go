package calsync

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/OpenTodoApp/opentodo/internal/calfeed"
)

// The worker's side of series writes (calendar-event-series task 2.5): the cache
// after each write equals what a fresh cycle reads from the fake.

func (h *harness) rows(c *Connection) []calfeed.EventRow {
	h.t.Helper()
	rows, err := h.w.Feeds.EventsOf(h.ctx, "alice", "2026-09-01", "2027-12-31", false, true)
	if err != nil {
		h.t.Fatal(err)
	}
	return rows
}

func (h *harness) rowOn(c *Connection, date string) calfeed.EventRow {
	h.t.Helper()
	for _, r := range h.rows(c) {
		if r.Start.Date == date && r.UID == "routine-1@google.com" {
			return r
		}
	}
	h.t.Fatalf("no occurrence on %s", date)
	return calfeed.EventRow{}
}

// sameAsACycle checks the cache against what the next cycle reads from the server.
func (h *harness) sameAsACycle(c *Connection) {
	h.t.Helper()
	before, _ := json.Marshal(h.rows(c))
	h.cycle(h.conn(c.ID))
	after, _ := json.Marshal(h.rows(c))
	if string(before) != string(after) {
		h.t.Fatalf("the cache is not what a cycle reads\n cache: %s\n cycle: %s", before, after)
	}
}

func TestCycleCachesASeriesWithoutLocks(t *testing.T) {
	in, _ := loadCase(t, "this-move")
	h, c, _ := seriesHarness(t, in)
	rows := h.rows(c)
	if len(rows) < 60 {
		t.Fatalf("rows = %d", len(rows))
	}
	var ids = map[string]bool{}
	firsts := 0
	for _, r := range rows {
		if r.Series == nil || r.Locked != "" || !r.Known || r.Series.Rev == "" || r.Series.Rev != r.Rev || r.Series.Repeat == nil {
			t.Fatalf("row %s: series %+v locked %q", r.Start.Date, r.Series, r.Locked)
		}
		ids[r.Series.ID] = true
		if r.Series.First {
			firsts++
		}
		a := AccessOf(h.conn(c.ID), h.srv.CalendarURL("work"), r)
		if !a.Update || !a.Delete || a.Reason != "" {
			t.Fatalf("row %s is not editable: %+v", r.Start.Date, a)
		}
	}
	if len(ids) != 1 || firsts != 1 {
		t.Fatalf("series ids %v, first occurrences %d", ids, firsts)
	}
	// The exception shows its own values; the excluded day is gone.
	if r := h.rowOn(c, "2026-11-13"); r.Summary != "Morning routine (late)" || *r.Start.Time != "09:00" {
		t.Fatalf("exception row = %+v", r)
	}
	for _, r := range rows {
		if r.Start.Date == "2026-11-14" {
			t.Fatal("the excluded day is shown")
		}
	}
}

func TestWorkerSeriesWritesMatchACycle(t *testing.T) {
	in, ch := loadCase(t, "this-move")
	_ = ch
	for name, tc := range map[string]struct {
		date string
		edit EventEdit
		new  bool // the edited occurrence's id changes
		refe bool // other occurrences changed
		ids  int  // series ids afterwards
	}{
		"this":      {"2026-11-12", EventEdit{Scope: calfeed.ScopeThis, Summary: ptr("Quiet")}, false, false, 1},
		"this move": {"2026-11-12", EventEdit{Scope: calfeed.ScopeThis, When: &EventWhen{Start: zonedPoint(t, "2026-11-12", "07:30", "Europe/Berlin"), End: zonedPoint(t, "2026-11-12", "09:00", "Europe/Berlin")}}, false, false, 1},
		"all":       {"2026-11-12", EventEdit{Scope: calfeed.ScopeAll, Summary: ptr("Everything")}, false, true, 1},
		"all move": {"2026-11-12", EventEdit{Scope: calfeed.ScopeAll, When: &EventWhen{Start: zonedPoint(t, "2026-11-12", "07:30", "Europe/Berlin"), End: zonedPoint(t, "2026-11-12", "09:00", "Europe/Berlin")}},
			true, true, 1},
		"following": {"2026-11-16", EventEdit{Scope: calfeed.ScopeFollowing, Summary: ptr("From now on")}, true, true, 2},
	} {
		t.Run(name, func(t *testing.T) {
			h, c, _ := seriesHarness(t, in)
			row := h.rowOn(c, tc.date)
			res, err := h.w.EditEvent(h.ctx, "alice", c.ID, row.ID, row.Rev, tc.edit)
			if err != nil {
				t.Fatal(err)
			}
			if res.Gone || res.Event.ID == "" || res.Replaces != row.ID || res.Refetch != tc.refe || res.Series == nil || res.Series.Rev == "" {
				t.Fatalf("result = %+v", res)
			}
			if tc.edit.Summary != nil && res.Event.Summary != *tc.edit.Summary {
				t.Errorf("summary = %q", res.Event.Summary)
			}
			if (res.Event.ID != row.ID) != tc.new && tc.edit.When != nil {
				t.Errorf("the edited occurrence's id %s (was %s): changed = %v, want %v", res.Event.ID, row.ID, res.Event.ID != row.ID, tc.new)
			}
			if res.Series.Rev != res.Event.Rev || res.Event.Series == nil || res.Event.Series.ID != res.Series.ID {
				t.Errorf("series %+v on event %+v", res.Series, res.Event.Series)
			}
			ids := map[string]bool{}
			for _, r := range h.rows(c) {
				if r.Series != nil {
					ids[r.Series.ID] = true
				}
				if r.Series != nil && r.Series.ID == res.Series.ID && r.Rev != res.Series.Rev {
					t.Fatalf("occurrence on %s keeps the old version", r.Start.Date)
				}
			}
			if len(ids) != tc.ids {
				t.Errorf("series ids = %v, want %d", ids, tc.ids)
			}
			if h.srv.Unconditional() != 0 {
				t.Fatalf("%d writes without a precondition", h.srv.Unconditional())
			}
			h.sameAsACycle(c)
		})
	}
}

func TestWorkerSeriesDeletesMatchACycle(t *testing.T) {
	in, _ := loadCase(t, "this-move")
	for name, tc := range map[string]struct {
		date  string
		scope calfeed.Scope
		gone  []string
		kept  string
	}{
		"this":      {"2026-11-12", calfeed.ScopeThis, []string{"2026-11-12"}, "2026-11-11"},
		"following": {"2026-11-16", calfeed.ScopeFollowing, []string{"2026-11-16", "2026-12-24"}, "2026-11-15"},
		"all":       {"2026-11-16", calfeed.ScopeAll, []string{"2026-11-15", "2026-11-11"}, ""},
	} {
		t.Run(name, func(t *testing.T) {
			h, c, _ := seriesHarness(t, in)
			row := h.rowOn(c, tc.date)
			res, err := h.w.DeleteEvent(h.ctx, "alice", c.ID, row.ID, row.Rev, tc.scope)
			if err != nil {
				t.Fatal(err)
			}
			if res.Replaces != row.ID || res.Event.ID != "" {
				t.Fatalf("result = %+v", res)
			}
			have := map[string]bool{}
			for _, r := range h.rows(c) {
				have[r.Start.Date] = true
			}
			for _, d := range tc.gone {
				if have[d] {
					t.Errorf("%s is still shown", d)
				}
			}
			if tc.kept != "" && !have[tc.kept] {
				t.Errorf("%s is gone", tc.kept)
			}
			h.sameAsACycle(c)
		})
	}
}

func TestWorkerSeriesRefusalsWriteAndCacheNothing(t *testing.T) {
	in, _ := loadCase(t, "this-move")
	h, c, _ := seriesHarness(t, in)
	row := h.rowOn(c, "2026-11-12")
	before, _ := json.Marshal(h.rows(c))
	for name, tc := range map[string]struct {
		edit         EventEdit
		code, reason string
	}{
		"no scope":          {EventEdit{Summary: ptr("x")}, EventScopeRequired, ""},
		"unknown scope":     {EventEdit{Scope: "everything", Summary: ptr("x")}, EventInvalid, "scope"},
		"repeat with this":  {EventEdit{Scope: calfeed.ScopeThis, Repeat: &RepeatEdit{Rule: &calfeed.Repeat{Freq: "daily"}}}, EventInvalid, "repeat"},
		"stop with all":     {EventEdit{Scope: calfeed.ScopeAll, Repeat: &RepeatEdit{}}, EventInvalid, "repeat"},
		"six reminders":     {EventEdit{Scope: calfeed.ScopeAll, Reminders: &[]int{1, 2, 3, 4, 5, 6}}, EventInvalid, "reminders"},
		"weekday not start": {EventEdit{Scope: calfeed.ScopeAll, Repeat: &RepeatEdit{Rule: &calfeed.Repeat{Freq: "weekly", Days: []string{"tu"}}}}, EventInvalid, "repeat"},
	} {
		_, err := h.w.EditEvent(h.ctx, "alice", c.ID, row.ID, row.Rev, tc.edit)
		var ee *EventError
		if !errors.As(err, &ee) || ee.Code != tc.code || ee.Reason != tc.reason {
			t.Errorf("%s: %v, want %s %s", name, err, tc.code, tc.reason)
		}
	}
	if _, err := h.w.DeleteEvent(h.ctx, "alice", c.ID, row.ID, row.Rev, ""); !errors.As(err, new(*EventError)) || err.(*EventError).Code != EventScopeRequired {
		t.Errorf("delete without scope: %v", err)
	}
	if _, err := h.w.EditEvent(h.ctx, "alice", c.ID, row.ID, "stale", EventEdit{Scope: calfeed.ScopeThis, Summary: ptr("x")}); err == nil || err.(*EventError).Code != EventChanged {
		t.Errorf("stale rev: %v", err)
	}
	// A day move of a monthly series is refused by the writer with the field, after the pre-read.
	if h.srv.Count("PUT") != 0 || h.srv.Count("DELETE") != 0 {
		t.Fatalf("a refused change wrote: %v", h.srv.Writes())
	}
	if after, _ := json.Marshal(h.rows(c)); string(after) != string(before) {
		t.Fatal("a refused change changed the cache")
	}
	// A single event refuses a scope.
	h2 := newHarness(t)
	h2.srv.Put("work", "rich.ics", richEvent)
	c2 := h2.connect(false)
	if _, err := h2.w.Store.Update(h2.ctx, "alice", c2.ID, Patch{EditEvents: ptr(true)}); err != nil {
		t.Fatal(err)
	}
	h2.cycle(h2.conn(c2.ID))
	single := h2.rows(c2)[0]
	_, err := h2.w.EditEvent(h2.ctx, "alice", c2.ID, single.ID, single.Rev, EventEdit{Scope: calfeed.ScopeThis, Summary: ptr("x")})
	var ee *EventError
	if !errors.As(err, &ee) || ee.Code != EventInvalid || ee.Reason != "scope" {
		t.Errorf("scope on a single event: %v", err)
	}
	if single.Series != nil {
		t.Errorf("a single event has a series: %+v", single.Series)
	}
}

func TestWorkerLegacyRecurringRowIsRefreshPending(t *testing.T) {
	in, _ := loadCase(t, "this-move")
	h, c, _ := seriesHarness(t, in)
	if _, err := h.db.Exec(`UPDATE calendar_event_remote SET locked = 'recurring'`); err != nil {
		t.Fatal(err)
	}
	row := h.rowOn(c, "2026-11-12")
	if a := AccessOf(h.conn(c.ID), h.srv.CalendarURL("work"), row); a.Update || a.Delete || a.Reason != ReasonRefreshPending {
		t.Fatalf("access = %+v", a)
	}
	_, err := h.w.EditEvent(h.ctx, "alice", c.ID, row.ID, row.Rev, EventEdit{Scope: calfeed.ScopeThis, Summary: ptr("x")})
	var ee *EventError
	if !errors.As(err, &ee) || ee.Code != EventNotEditable || ee.Reason != ReasonRefreshPending {
		t.Fatalf("edit = %v", err)
	}
}

func TestWorkerSeriesWritesSerialise(t *testing.T) {
	in, _ := loadCase(t, "this-move")
	h, c, _ := seriesHarness(t, in)
	row := h.rowOn(c, "2026-11-12")
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = h.w.EditEvent(h.ctx, "alice", c.ID, row.ID, row.Rev, EventEdit{Scope: calfeed.ScopeAll, Summary: ptr(strings.Repeat("x", i+1))})
		}()
	}
	wg.Wait()
	ok, changed := 0, 0
	for _, err := range errs {
		var ee *EventError
		switch {
		case err == nil:
			ok++
		case errors.As(err, &ee) && ee.Code == EventChanged:
			changed++
		default:
			t.Fatalf("error %v", err)
		}
	}
	if ok != 1 || changed != 1 || h.srv.Count("PUT") != 1 {
		t.Fatalf("ok %d, changed %d, PUT %d", ok, changed, h.srv.Count("PUT"))
	}
	h.sameAsACycle(c)
}

func TestWorkerSplitFailureCachesNothing(t *testing.T) {
	in, _ := loadCase(t, "this-move")
	h, c, _ := seriesHarness(t, in)
	row := h.rowOn(c, "2026-11-16")
	before, _ := json.Marshal(h.rows(c))
	h.srv.FailNext("PUT", "routine.ics", 412, 1)
	_, err := h.w.EditEvent(h.ctx, "alice", c.ID, row.ID, row.Rev, EventEdit{Scope: calfeed.ScopeFollowing, Summary: ptr("x")})
	var ee *EventError
	if !errors.As(err, &ee) || ee.Code != EventChanged {
		t.Fatalf("edit = %v", err)
	}
	if len(h.srv.Members("work")) != 1 {
		t.Fatalf("members = %v", h.srv.Members("work"))
	}
	if after, _ := json.Marshal(h.rows(c)); string(after) != string(before) {
		t.Fatal("a failed split changed the cache")
	}
	// A rollback that fails too is a partial write, and the cache learns of the duplicate.
	h.srv.FailNext("PUT", "routine.ics", 412, 1)
	h.srv.FailNext("DELETE", "ot-", 500, 1)
	_, err = h.w.EditEvent(h.ctx, "alice", c.ID, row.ID, row.Rev, EventEdit{Scope: calfeed.ScopeFollowing, Summary: ptr("x")})
	if !errors.As(err, &ee) || ee.Code != EventUnreachable || ee.Reason != ReasonPartialWrite {
		t.Fatalf("edit = %v", err)
	}
	h.cycle(h.conn(c.ID))
	dups := 0
	for _, r := range h.rows(c) {
		if r.Start.Date == "2026-11-16" {
			dups++
		}
	}
	if dups != 2 {
		t.Fatalf("the duplicate is not visible after the next cycle: %d occurrences on the day", dups)
	}
}

func TestWorkerCreateRepeatingEventCachesTheSeries(t *testing.T) {
	h := newHarness(t)
	h.clock = h.clock.AddDate(0, 0, 23) // 2026-11-12
	c := h.connect(false)
	if _, err := h.w.Store.Update(h.ctx, "alice", c.ID, Patch{EditEvents: ptr(true)}); err != nil {
		t.Fatal(err)
	}
	ev := EventNew{UID: "client-0001", Summary: "Planning", When: EventWhen{Start: zonedPoint(t, "2026-11-16", "09:00", "Europe/Berlin"), End: zonedPoint(t, "2026-11-16", "10:00", "Europe/Berlin")},
		Repeat: &calfeed.Repeat{Freq: "weekly", Days: []string{"mo", "we"}, Ends: calfeed.RepeatEnd{Type: calfeed.EndAfter, Count: 6}}}
	h.cycle(h.conn(c.ID))
	cal := h.srv.CalendarURL("work")
	res, err := h.w.CreateEvent(h.ctx, "alice", c.ID, cal, ev)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Created || res.Event.Series == nil || !res.Event.Series.First || res.Event.Series.Repeat == nil || res.Event.Series.Repeat.Ends.Count != 6 {
		t.Fatalf("result = %+v series %+v", res.Event, res.Event.Series)
	}
	rows := h.rows(h.conn(c.ID))
	if len(rows) != 6 {
		t.Fatalf("occurrences = %d", len(rows))
	}
	// A replay of the same client id writes nothing more.
	if res, err = h.w.CreateEvent(h.ctx, "alice", c.ID, cal, ev); err != nil || res.Created || h.srv.Count("PUT") != 1 {
		t.Fatalf("replay = %+v %v, PUT %d", res, err, h.srv.Count("PUT"))
	}
	h.sameAsACycle(h.conn(c.ID))
}

func TestWorkerDetailOfASeries(t *testing.T) {
	in, _ := loadCase(t, "this-move")
	h, c, _ := seriesHarness(t, in)
	row := h.rowOn(c, "2026-11-13")
	d, err := h.w.EventDetail(h.ctx, "alice", c.ID, row.ID)
	if err != nil {
		t.Fatal(err)
	}
	if d.SeriesDetail == nil || d.SeriesDetail.Repeat == nil || d.SeriesDetail.Repeat.Freq != "daily" || d.SeriesDetail.Exceptions != 2 || d.RemindersMax != 5 {
		t.Fatalf("detail = %+v", d)
	}
	// The exception has no alarm of its own.
	if len(d.Reminders) != 0 {
		t.Fatalf("reminders of the exception = %v", d.Reminders)
	}
	plain, err := h.w.EventDetail(h.ctx, "alice", c.ID, h.rowOn(c, "2026-11-12").ID)
	if err != nil || len(plain.Reminders) != 1 || plain.Reminders[0] != 10 {
		t.Fatalf("reminders of the master = %v %v", plain.Reminders, err)
	}
	// The calendar changed behind the cache: the detail brings the cache up to date.
	h.srv.Put("work", "routine.ics", strings.Replace(in, "SUMMARY:Morning routine\n", "SUMMARY:Renamed elsewhere\n", 1))
	d, err = h.w.EventDetail(h.ctx, "alice", c.ID, h.rowOn(c, "2026-11-12").ID)
	if err != nil || d.Event.Summary != "Renamed elsewhere" {
		t.Fatalf("detail after a change = %+v %v", d.Event, err)
	}
	h.sameAsACycle(c)
}
