package calsync

import (
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/OpenTodoApp/opentodo/internal/calfeed"
	"github.com/OpenTodoApp/opentodo/internal/calsync/calsynctest"
)

// Worker.EditEvent and DeleteEvent (OpenSpec change calendar-event-edits, task
// 2.4).

type editEnv struct {
	*harness
	c       *Connection
	notices *atomic.Int32
}

func newEditEnv(t *testing.T, edits bool) *editEnv {
	t.Helper()
	h := newHarness(t)
	h.srv.Put("work", "a.ics", event("a", "Plan", "DTSTART:20261021T100000Z", "DTEND:20261021T110000Z", h.clock))
	h.srv.Put("work", "g.ics", strings.Replace(event("g", "Guests", "DTSTART:20261022T100000Z", "DTEND:20261022T110000Z", h.clock),
		"END:VEVENT", "ATTENDEE:mailto:a@example.org\r\nEND:VEVENT", 1))
	c := h.connect(false)
	if edits {
		on := true
		if _, err := h.w.Store.Update(h.ctx, "alice", c.ID, Patch{EditEvents: &on}); err != nil {
			t.Fatal(err)
		}
	}
	h.cycle(h.conn(c.ID))
	n := &atomic.Int32{}
	h.w.Notify = func(string) { n.Add(1) }
	return &editEnv{harness: h, c: c, notices: n}
}

// row is the served event with the summary.
func (e *editEnv) row(summary string) calfeed.EventRow {
	e.t.Helper()
	rows, err := e.w.Feeds.EventsOf(e.ctx, "alice", "2026-10-19", "2026-10-30", false, true)
	if err != nil {
		e.t.Fatal(err)
	}
	for _, r := range rows {
		if r.Summary == summary {
			return r
		}
	}
	e.t.Fatalf("no event %q in %+v", summary, rows)
	return calfeed.EventRow{}
}

func newTitle(s string) EventEdit { return EventEdit{Summary: &s} }

func wantCode(t *testing.T, err error, code, reason string) {
	t.Helper()
	var ee *EventError
	if !errors.As(err, &ee) || ee.Code != code || ee.Reason != reason {
		t.Fatalf("error = %v, want %s %q", err, code, reason)
	}
}

func TestEditEventMovesAndRenames(t *testing.T) {
	e := newEditEnv(t, true)
	old := e.row("Plan")
	if old.Rev == "" || !old.Managed || !old.Known {
		t.Fatalf("row = %+v", old)
	}
	title := "Plan (moved)"
	res, err := e.w.EditEvent(e.ctx, "alice", e.c.ID, old.ID, old.Rev, EventEdit{Summary: &title,
		When: &EventWhen{Start: zonedPoint(t, "2026-10-23", "15:00", "UTC"), End: zonedPoint(t, "2026-10-23", "16:00", "UTC")}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Replaces != old.ID || res.Event.ID == old.ID || res.Event.Summary != title || res.Event.Start.Date != "2026-10-23" ||
		res.Event.Rev == "" || res.Event.Rev == old.Rev || res.Event.Access == nil || !res.Event.Access.Update || !res.Event.Access.Delete {
		t.Fatalf("result = %+v", res)
	}
	if n := e.notices.Load(); n != 1 {
		t.Fatalf("notifications = %d", n)
	}
	// The cache holds the new event only, and the remote object has the change.
	moved := e.row(title)
	if moved.ID != res.Event.ID || moved.Rev != res.Event.Rev {
		t.Fatalf("cached = %+v, result = %+v", moved, res.Event)
	}
	if got := e.srv.Get("work", "a.ics"); !strings.Contains(got, "DTSTART:20261023T150000Z") || !strings.Contains(got, title) {
		t.Fatalf("remote:\n%s", got)
	}
	// The next cycle reads the calendar again and agrees with the cache.
	e.later(time.Minute)
	e.cycle(e.conn(e.c.ID))
	if again := e.row(title); again.ID != moved.ID || again.Rev != moved.Rev {
		t.Fatalf("after the cycle = %+v, before = %+v", again, moved)
	}
	// The old rev is stale now.
	_, err = e.w.EditEvent(e.ctx, "alice", e.c.ID, moved.ID, old.Rev, newTitle("again"))
	wantCode(t, err, EventChanged, "")
}

func TestEditEventRefusalsWriteNothing(t *testing.T) {
	e := newEditEnv(t, false)
	plan, guests := e.row("Plan"), e.row("Guests")
	puts := e.srv.Count("PUT") + e.srv.Count("GET") + e.srv.Count("DELETE")
	check := func(name string, err error, code, reason string) {
		t.Helper()
		t.Run(name, func(t *testing.T) { wantCode(t, err, code, reason) })
		if got := e.srv.Count("PUT") + e.srv.Count("GET") + e.srv.Count("DELETE"); got != puts {
			t.Fatalf("%s sent a request to the calendar", name)
		}
		if e.notices.Load() != 0 {
			t.Fatalf("%s woke devices", name)
		}
	}
	edit := func(user, conn, id, rev string) error {
		_, err := e.w.EditEvent(e.ctx, user, conn, id, rev, newTitle("x"))
		return err
	}
	check("off by default", edit("alice", e.c.ID, plan.ID, plan.Rev), EventEditsOff, "")
	on := true
	if _, err := e.w.Store.Update(e.ctx, "alice", e.c.ID, Patch{EditEvents: &on}); err != nil {
		t.Fatal(err)
	}
	check("locked", edit("alice", e.c.ID, guests.ID, guests.Rev), EventNotEditable, LockGuests)
	check("stale rev", edit("alice", e.c.ID, plan.ID, "0000000000000000"), EventChanged, "")
	check("another user", edit("bob", e.c.ID, plan.ID, plan.Rev), EventNotFound, "")
	check("unknown event", edit("alice", e.c.ID, "ffffffffffffffffffffffff", plan.Rev), EventNotFound, "")
	check("unknown connection", edit("alice", "nope", plan.ID, plan.Rev), EventNotFound, "")
	if err := e.w.Store.SetCaps(e.ctx, e.c.ID, e.srv.CalendarURL("work"), Caps{Create: true}); err != nil {
		t.Fatal(err)
	}
	check("calendar read only", edit("alice", e.c.ID, plan.ID, plan.Rev), EventReadOnly, "")
	_, err := e.w.DeleteEvent(e.ctx, "alice", e.c.ID, plan.ID, plan.Rev)
	check("delete on a read only calendar", err, EventReadOnly, "")
	if got := e.srv.Get("work", "a.ics"); !strings.Contains(got, "SUMMARY:Plan") {
		t.Fatalf("remote changed:\n%s", got)
	}
}

func TestEditEventRemoteConflictsAndGone(t *testing.T) {
	e := newEditEnv(t, true)
	plan := e.row("Plan")
	// Changed in the calendar app after the last read: 409, cache unchanged.
	e.srv.Put("work", "a.ics", event("a", "Plan v2", "DTSTART:20261021T100000Z", "DTEND:20261021T110000Z", e.clock))
	_, err := e.w.EditEvent(e.ctx, "alice", e.c.ID, plan.ID, plan.Rev, newTitle("x"))
	wantCode(t, err, EventChanged, "")
	if e.row("Plan").ID != plan.ID || e.notices.Load() != 0 {
		t.Fatal("a conflict changed the cache or woke devices")
	}
	// Deleted there: gone, the row leaves the cache, devices are woken.
	e.srv.Delete("work", "a.ics")
	_, err = e.w.EditEvent(e.ctx, "alice", e.c.ID, plan.ID, plan.Rev, newTitle("x"))
	wantCode(t, err, EventGone, "")
	rows, _ := e.w.Feeds.EventsOf(e.ctx, "alice", "2026-10-19", "2026-10-30", false, true)
	for _, r := range rows {
		if r.Summary == "Plan" {
			t.Fatal("the gone event is still cached")
		}
	}
	if e.notices.Load() != 1 {
		t.Fatalf("notifications = %d", e.notices.Load())
	}
}

func TestEditEventRefusedByCalendarClearsCaps(t *testing.T) {
	h := newHarness(t)
	h.srv.AddCalendar("holidays", &calsynctest.Calendar{Name: "Holidays", ReadOnly: true})
	h.srv.SetPrivileges("holidays", "write") // claims more than the server allows
	h.srv.Put("holidays", "h.ics", event("h", "Holiday", "DTSTART:20261021T100000Z", "DTEND:20261021T110000Z", h.clock))
	c := h.connect(false)
	reads, on := []string{h.srv.CalendarURL("holidays")}, true
	if _, err := h.w.Store.Update(h.ctx, "alice", c.ID, Patch{ReadURLs: &reads, EditEvents: &on}); err != nil {
		t.Fatal(err)
	}
	h.cycle(h.conn(c.ID))
	e := &editEnv{harness: h, c: c, notices: &atomic.Int32{}}
	row := e.row("Holiday")
	_, err := h.w.EditEvent(h.ctx, "alice", c.ID, row.ID, row.Rev, newTitle("x"))
	wantCode(t, err, EventReadOnly, "")
	if caps := h.conn(c.ID).Calendar(h.srv.CalendarURL("holidays")).EffectiveCaps(); caps != (Caps{}) {
		t.Fatalf("caps after a 403 = %+v", caps)
	}
	// The second attempt is refused before any request.
	gets := h.srv.Count("GET")
	_, err = h.w.EditEvent(h.ctx, "alice", c.ID, row.ID, row.Rev, newTitle("x"))
	wantCode(t, err, EventReadOnly, "")
	if h.srv.Count("GET") != gets {
		t.Fatal("a known read-only calendar was asked again")
	}
}

func TestDeleteEvent(t *testing.T) {
	e := newEditEnv(t, true)
	plan := e.row("Plan")
	res, err := e.w.DeleteEvent(e.ctx, "alice", e.c.ID, plan.ID, plan.Rev)
	if err != nil || res.Replaces != plan.ID {
		t.Fatalf("delete = %+v %v", res, err)
	}
	if e.srv.Get("work", "a.ics") != "" || e.notices.Load() != 1 {
		t.Fatal("the object survived or devices were not woken once")
	}
	rows, _ := e.w.Feeds.EventsOf(e.ctx, "alice", "2026-10-19", "2026-10-30", false, true)
	for _, r := range rows {
		if r.Summary == "Plan" {
			t.Fatal("the event is still cached")
		}
	}
	if refs, _ := e.w.Feeds.Refs(e.ctx, e.sub()); len(refs) != 1 {
		t.Fatalf("refs = %+v", refs)
	}
}

func (e *editEnv) sub() string {
	m, _ := e.w.Feeds.ManagedOf(e.ctx, e.c.ID)
	return m[e.srv.CalendarURL("work")].SubscriptionID
}

func TestAccessReasonOrder(t *testing.T) {
	cal := Calendar{URL: "u", Caps: &Caps{Create: true, Update: true, Delete: true}}
	conn := &Connection{Calendars: []Calendar{cal}, EditEvents: true}
	row := calfeed.EventRow{Known: true}
	for name, tc := range map[string]struct {
		mod  func(c *Connection, r *calfeed.EventRow)
		want calfeed.Access
	}{
		"editable":   {func(*Connection, *calfeed.EventRow) {}, calfeed.Access{Update: true, Delete: true}},
		"edits off":  {func(c *Connection, _ *calfeed.EventRow) { c.EditEvents = false }, calfeed.Access{Reason: EventEditsOffReason}},
		"read only":  {func(c *Connection, _ *calfeed.EventRow) { c.Calendars[0].Caps = &Caps{} }, calfeed.Access{Reason: EventReadOnly}},
		"locked":     {func(_ *Connection, r *calfeed.EventRow) { r.Locked = LockRecurring }, calfeed.Access{Reason: LockRecurring}},
		"pending":    {func(_ *Connection, r *calfeed.EventRow) { r.Known = false }, calfeed.Access{Reason: ReasonRefreshPending}},
		"off+locked": {func(c *Connection, r *calfeed.EventRow) { c.EditEvents, r.Locked = false, LockGuests }, calfeed.Access{Reason: EventEditsOffReason}},
		"no delete": {func(c *Connection, _ *calfeed.EventRow) { c.Calendars[0].Caps = &Caps{Update: true} },
			calfeed.Access{Update: true, Reason: EventReadOnly}},
	} {
		c := *conn
		c.Calendars = []Calendar{cal}
		r := row
		tc.mod(&c, &r)
		if got := AccessOf(&c, "u", r); got != tc.want {
			t.Errorf("%s: access = %+v, want %+v", name, got, tc.want)
		}
	}
}

func TestEditAndCycleNeverInterleave(t *testing.T) {
	e := newEditEnv(t, true)
	plan := e.row("Plan")
	l := e.w.lock(e.c.ID)
	l.Lock()
	done := make(chan string, 2)
	go func() {
		_, err := e.w.EditEvent(e.ctx, "alice", e.c.ID, plan.ID, plan.Rev, newTitle("Edited"))
		if err != nil {
			done <- "edit failed: " + err.Error()
			return
		}
		done <- "edit"
	}()
	go func() {
		if _, err := e.w.Cycle(e.ctx, e.c); err != nil {
			done <- "cycle failed: " + err.Error()
			return
		}
		done <- "cycle"
	}()
	select {
	case got := <-done:
		t.Fatalf("%s ran while the connection was locked", got)
	case <-time.After(150 * time.Millisecond):
	}
	l.Unlock()
	for i := 0; i < 2; i++ {
		select {
		case got := <-done:
			if strings.Contains(got, "failed") {
				t.Fatal(got)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("waiting for the edit and the cycle")
		}
	}
	// Whichever went first, the end state is the edit, read back from the remote.
	if got := e.srv.Get("work", "a.ics"); !strings.Contains(got, "SUMMARY:Edited") {
		t.Fatalf("remote:\n%s", got)
	}
}
