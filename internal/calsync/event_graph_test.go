package calsync

import (
	"errors"
	"testing"

	"github.com/OpenTodoApp/opentodo/internal/calfeed"
	"github.com/OpenTodoApp/opentodo/internal/calsync/graphtest"
)

// The Graph event writer against the in-process Microsoft Graph (OpenSpec
// change calendar-event-edits, task 2.3).

func graphSetup(t *testing.T, f func(e *graphtest.Event)) (*harness, *graphtest.Server, *Connection, EventWriter, EventRef, string) {
	t.Helper()
	h := newHarness(t)
	ms, c := h.microsoft("W. Europe Standard Time")
	ev := graphtest.Event{Calendar: "cal-work", Subject: "Standup",
		Start: graphtest.DT{DateTime: "2026-10-21T10:00:00", TimeZone: "UTC"}, End: graphtest.DT{DateTime: "2026-10-21T10:30:00", TimeZone: "UTC"}}
	if f != nil {
		f(&ev)
	}
	id := ms.Add(ev)
	h.cycle(c)
	return h, ms, c, h.writer(c), h.davRef(c, "cal-work", id), id
}

func TestGraphUpdateMoveRenameResize(t *testing.T) {
	h, ms, _, w, ref, id := graphSetup(t, nil)
	title := "Standup (moved)"
	when := &EventWhen{Start: zonedPoint(t, "2026-10-22", "14:00", "Europe/Berlin"), End: zonedPoint(t, "2026-10-22", "14:45", "Europe/Berlin")}
	st, err := w.Update(h.ctx, ref, EventEdit{Summary: &title, When: when})
	if err != nil {
		t.Fatal(err)
	}
	if st.Ref.ETag == "" || st.Ref.ETag == ref.ETag || st.Ref.Href != id || st.Event.Summary != title || st.Locked != "" {
		t.Fatalf("state = %+v", st)
	}
	var got graphtest.Event
	for _, e := range ms.Events("cal-work") {
		if e.ID == id {
			got = e
		}
	}
	if got.Subject != title || got.Start.DateTime != "2026-10-22T14:00:00" || got.Start.TimeZone != "W. Europe Standard Time" ||
		got.End.DateTime != "2026-10-22T14:45:00" || got.IsAllDay {
		t.Fatalf("event = %+v", got)
	}
	// Rename only: no times in the request.
	ref.ETag = st.Ref.ETag
	name := "Renamed"
	if _, err := w.Update(h.ctx, ref, EventEdit{Summary: &name}); err != nil {
		t.Fatal(err)
	}
	for _, e := range ms.Events("cal-work") {
		if e.ID == id && (e.Subject != name || e.Start.DateTime != "2026-10-22T14:00:00") {
			t.Fatalf("rename changed the times: %+v", e)
		}
	}
	if ms.Unconditional() != 0 {
		t.Fatalf("%d writes without If-Match", ms.Unconditional())
	}
}

func TestGraphUpdateAllDayFloatingAndUTCFallback(t *testing.T) {
	h, ms, _, w, ref, id := graphSetup(t, nil)
	st, err := w.Update(h.ctx, ref, EventEdit{When: &EventWhen{Start: datePoint("2026-10-23"), End: datePoint("2026-10-24"), AllDay: true}})
	if err != nil {
		t.Fatal(err)
	}
	check := func(start, tz string, allDay bool) {
		t.Helper()
		for _, e := range ms.Events("cal-work") {
			if e.ID == id && (e.Start.DateTime != start || e.Start.TimeZone != tz || e.IsAllDay != allDay) {
				t.Fatalf("event = %+v, want %s %s all-day %v", e, start, tz, allDay)
			}
		}
	}
	check("2026-10-23T00:00:00", "W. Europe Standard Time", true)
	ref.ETag = st.Ref.ETag
	// Floating: the wall time in the mailbox zone.
	if st, err = w.Update(h.ctx, ref, EventEdit{When: &EventWhen{Start: floatingPoint("2026-10-23", "08:15"), End: floatingPoint("2026-10-23", "09:00")}}); err != nil {
		t.Fatal(err)
	}
	check("2026-10-23T08:15:00", "W. Europe Standard Time", false)
	ref.ETag = st.Ref.ETag
	// A zone without a Windows id is written as the same instant in UTC.
	p := calfeed.Point{T: zonedPoint(t, "2026-10-23", "10:00", "Europe/Berlin").T, Zone: "Antarctica/Troll"}
	q := calfeed.Point{T: zonedPoint(t, "2026-10-23", "11:00", "Europe/Berlin").T, Zone: "Antarctica/Troll"}
	if _, err := w.Update(h.ctx, ref, EventEdit{When: &EventWhen{Start: p, End: q}}); err != nil {
		t.Fatal(err)
	}
	check("2026-10-23T08:00:00", "UTC", false)
}

func TestGraphDelete(t *testing.T) {
	h, ms, _, w, ref, id := graphSetup(t, nil)
	if err := w.Delete(h.ctx, ref); err != nil {
		t.Fatal(err)
	}
	for _, e := range ms.Events("cal-work") {
		if e.ID == id {
			t.Fatal("event survived")
		}
	}
	if err := w.Delete(h.ctx, ref); !errors.Is(err, ErrEventGone) {
		t.Fatalf("second delete = %v", err)
	}
	if ms.Unconditional() != 0 {
		t.Fatalf("%d deletes without If-Match", ms.Unconditional())
	}
}

func TestGraphConflictsAndRefusals(t *testing.T) {
	h, ms, _, w, ref, id := graphSetup(t, nil)
	title := "x"
	edit := EventEdit{Summary: &title}
	// A change between the read and the PATCH is a 412.
	ms.RaceNextPatch()
	if _, err := w.Update(h.ctx, ref, edit); !errors.Is(err, ErrEventChanged) {
		t.Fatalf("race = %v", err)
	}
	// A change before the pre-read is caught without a PATCH.
	patches := ms.Count("PATCH")
	if _, err := w.Update(h.ctx, ref, edit); !errors.Is(err, ErrEventChanged) {
		t.Fatalf("stale etag = %v", err)
	}
	if ms.Count("PATCH") != patches {
		t.Fatal("a stale update sent a PATCH")
	}
	if err := w.Delete(h.ctx, ref); !errors.Is(err, ErrEventChanged) {
		t.Fatalf("stale delete = %v", err)
	}
	// Gone.
	ms.Delete(id)
	if _, err := w.Update(h.ctx, ref, edit); !errors.Is(err, ErrEventGone) {
		t.Fatalf("gone = %v", err)
	}
	if _, err := w.Read(h.ctx, ref); !errors.Is(err, ErrEventGone) {
		t.Fatalf("read of a gone event = %v", err)
	}
}

func TestGraphReadOnlyCalendarIsRefused(t *testing.T) {
	h, ms, _, w, ref, _ := graphSetup(t, nil)
	ms.SetCanEdit("cal-work", false)
	title := "x"
	if _, err := w.Update(h.ctx, ref, EventEdit{Summary: &title}); !errors.Is(err, ErrCalendarRO) {
		t.Fatalf("update = %v", err)
	}
	if err := w.Delete(h.ctx, ref); !errors.Is(err, ErrCalendarRO) {
		t.Fatalf("delete = %v", err)
	}
}

func TestGraphLockedEventsAreRefused(t *testing.T) {
	for name, tc := range map[string]struct {
		f      func(e *graphtest.Event)
		reason string
	}{
		"guests":    {func(e *graphtest.Event) { e.Attendees = []string{"a@example.org"} }, LockGuests},
		"organizer": {func(e *graphtest.Event) { e.NotOrganizer = true }, LockNotOrganizer},
		"series":    {func(e *graphtest.Event) { e.Type = "occurrence" }, LockRecurring},
	} {
		t.Run(name, func(t *testing.T) {
			h, ms, _, w, ref, _ := graphSetup(t, tc.f)
			title := "x"
			if _, err := w.Update(h.ctx, ref, EventEdit{Summary: &title}); !errors.Is(err, ErrEventLocked) || lockReason(err) != tc.reason {
				t.Fatalf("update = %v", err)
			}
			if ms.Count("PATCH") != 0 {
				t.Fatal("a locked event was patched")
			}
		})
	}
}
