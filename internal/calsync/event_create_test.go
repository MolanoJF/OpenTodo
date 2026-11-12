package calsync

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/OpenTodoApp/opentodo/internal/calfeed"
)

// Worker.CreateEvent and Worker.EventDetail (OpenSpec change
// calendar-event-editor, task 2.4).

func newCreate(clientID string) EventNew {
	return EventNew{UID: clientID, Summary: "New call", Location: "Room 4", Description: "Notes",
		When: EventWhen{Start: floatingPoint("2026-10-23", "15:00"), End: floatingPoint("2026-10-23", "16:00")}}
}

func (e *editEnv) requests() int {
	n := 0
	for _, c := range e.srv.Requests {
		n += c
	}
	return n
}

func TestCreateEventRefusalsWriteNothing(t *testing.T) {
	e := newEditEnv(t, false)
	cal := e.srv.CalendarURL("work")
	before := e.requests()
	check := func(name string, err error, code, reason string) {
		t.Helper()
		t.Run(name, func(t *testing.T) { wantCode(t, err, code, reason) })
		if e.requests() != before {
			t.Fatalf("%s contacted the calendar", name)
		}
		if e.notices.Load() != 0 || len(e.srv.Members("work")) != 2 {
			t.Fatalf("%s changed something", name)
		}
	}
	create := func(user, conn, calURL, id string) error {
		_, err := e.w.CreateEvent(e.ctx, user, conn, calURL, newCreate(id))
		return err
	}
	check("off by default", create("alice", e.c.ID, cal, "client-0001"), EventEditsOff, "")
	on := true
	if _, err := e.w.Store.Update(e.ctx, "alice", e.c.ID, Patch{EditEvents: &on}); err != nil {
		t.Fatal(err)
	}
	check("bad client id", create("alice", e.c.ID, cal, "short"), EventInvalid, "clientId")
	check("calendar not shown", create("alice", e.c.ID, e.srv.CalendarURL("other"), "client-0002"), EventInvalid, "calendarUrl")
	check("another user", create("bob", e.c.ID, cal, "client-0003"), EventNotFound, "")
	check("unknown connection", create("alice", "nope", cal, "client-0004"), EventNotFound, "")
	if err := e.w.Store.SetCaps(e.ctx, e.c.ID, cal, Caps{Update: true, Delete: true}); err != nil {
		t.Fatal(err)
	}
	check("no create capability", create("alice", e.c.ID, cal, "client-0005"), EventReadOnly, "")
}

func TestCreateEventCachesAndRecords(t *testing.T) {
	e := newEditEnv(t, true)
	cal := e.srv.CalendarURL("work")
	res, err := e.w.CreateEvent(e.ctx, "alice", e.c.ID, cal, newCreate("client-0001"))
	if err != nil {
		t.Fatal(err)
	}
	r := res.Event
	if !res.Created || r.Summary != "New call" || r.Location != "Room 4" || r.Start.Date != "2026-10-23" || r.Rev == "" ||
		r.Access == nil || !r.Access.Update || !r.Access.Delete || res.Calendar != cal || res.ConnectionID != e.c.ID || res.Replaces != "" {
		t.Fatalf("result = %+v", res)
	}
	if n := e.notices.Load(); n != 1 {
		t.Fatalf("notifications = %d", n)
	}
	if got := e.srv.Get("work", "client-0001.ics"); !strings.Contains(got, "UID:client-0001") {
		t.Fatalf("remote:\n%s", got)
	}
	if got := e.row("New call"); got.ID != r.ID || got.Rev != r.Rev {
		t.Fatalf("cached = %+v", got)
	}
	if _, uid, ok, err := e.w.Feeds.CreatedBy(e.ctx, e.c.ID, "client-0001"); err != nil || !ok || uid != "client-0001" {
		t.Fatalf("record = %q %v %v", uid, ok, err)
	}
	// The next cycle reads the calendar and agrees with the cache.
	e.later(time.Minute)
	e.cycle(e.conn(e.c.ID))
	if again := e.row("New call"); again.ID != r.ID || again.Rev != r.Rev {
		t.Fatalf("after the cycle = %+v, before = %+v", again, r)
	}
	if e.srv.Unconditional() != 0 {
		t.Fatalf("%d unconditional writes", e.srv.Unconditional())
	}
}

func TestCreateEventReplayAndDeletedReplay(t *testing.T) {
	e := newEditEnv(t, true)
	cal := e.srv.CalendarURL("work")
	first, err := e.w.CreateEvent(e.ctx, "alice", e.c.ID, cal, newCreate("client-0001"))
	if err != nil {
		t.Fatal(err)
	}
	before, notes := e.requests(), e.notices.Load()
	again, err := e.w.CreateEvent(e.ctx, "alice", e.c.ID, cal, newCreate("client-0001"))
	if err != nil || again.Created || again.Event.ID != first.Event.ID || again.Event.Rev != first.Event.Rev {
		t.Fatalf("replay = %+v %v", again, err)
	}
	if e.requests() != before || e.notices.Load() != notes || len(e.srv.Members("work")) != 3 {
		t.Fatal("a replay wrote or woke devices")
	}
	// A replay after the event was edited returns the edited event.
	edited, err := e.w.EditEvent(e.ctx, "alice", e.c.ID, first.Event.ID, first.Event.Rev, newTitle("Edited"))
	if err != nil {
		t.Fatal(err)
	}
	if again, err = e.w.CreateEvent(e.ctx, "alice", e.c.ID, cal, newCreate("client-0001")); err != nil || again.Created || again.Event.Summary != "Edited" || again.Event.ID != edited.Event.ID {
		t.Fatalf("replay after edit = %+v %v", again, err)
	}
	// Deleted since: the same id does not make it again.
	if _, err := e.w.DeleteEvent(e.ctx, "alice", e.c.ID, edited.Event.ID, edited.Event.Rev, ""); err != nil {
		t.Fatal(err)
	}
	puts := e.srv.Count("PUT")
	_, err = e.w.CreateEvent(e.ctx, "alice", e.c.ID, cal, newCreate("client-0001"))
	wantCode(t, err, EventGone, "")
	if e.srv.Count("PUT") != puts || len(e.srv.Members("work")) != 2 {
		t.Fatal("the deleted event was made again")
	}
}

func TestCreateEventConcurrentRetriesMakeOne(t *testing.T) {
	e := newEditEnv(t, true)
	cal := e.srv.CalendarURL("work")
	var wg sync.WaitGroup
	results := make([]*EventResult, 2)
	errs := make([]error, 2)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errs[i] = e.w.CreateEvent(e.ctx, "alice", e.c.ID, cal, newCreate("client-race1"))
		}()
	}
	wg.Wait()
	if errs[0] != nil || errs[1] != nil || results[0].Event.ID != results[1].Event.ID || results[0].Created == results[1].Created {
		t.Fatalf("results = %+v %+v errs %v", results[0], results[1], errs)
	}
	if got := e.srv.Count("PUT"); got != 1 || len(e.srv.Members("work")) != 3 {
		t.Fatalf("PUTs = %d, members = %v", got, e.srv.Members("work"))
	}
}

func TestCreateEventRemoteFailures(t *testing.T) {
	e := newEditEnv(t, true)
	cal := e.srv.CalendarURL("work")
	// A taken address is calendar_unreachable with the reason.
	e.srv.Put("work", "client-0001.ics", "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VEVENT\r\nUID:someone-else\r\nDTSTART:20261113T100000Z\r\nDTEND:20261113T110000Z\r\nSUMMARY:X\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n")
	_, err := e.w.CreateEvent(e.ctx, "alice", e.c.ID, cal, newCreate("client-0001"))
	wantCode(t, err, EventUnreachable, ReasonHrefTaken)
	if _, _, ok, _ := e.w.Feeds.CreatedBy(e.ctx, e.c.ID, "client-0001"); ok {
		t.Fatal("a failed create was recorded")
	}
	if e.notices.Load() != 0 {
		t.Fatal("a failed create woke devices")
	}
	// A calendar that says it is read-only when written clears its caps.
	e.srv.SetReadOnly("work", true)
	_, err = e.w.CreateEvent(e.ctx, "alice", e.c.ID, cal, newCreate("client-0002"))
	wantCode(t, err, EventReadOnly, "")
	if cal := e.conn(e.c.ID).Calendar(cal); cal == nil || cal.EffectiveCaps().Create {
		t.Fatalf("caps after a 403 = %+v", cal)
	}
}

func TestEventDetailNewerRemoteVersion(t *testing.T) {
	e := newEditEnv(t, true)
	plan := e.row("Plan")
	d, err := e.w.EventDetail(e.ctx, "alice", e.c.ID, plan.ID)
	if err != nil || d.Replaces != "" || d.Event.ID != plan.ID || !d.DescriptionEditable || e.notices.Load() != 0 {
		t.Fatalf("detail = %+v %v", d, err)
	}
	// Someone changed it in the calendar app.
	e.srv.Put("work", "a.ics", strings.Replace(event("a", "Plan v2", "DTSTART:20261021T120000Z", "DTEND:20261021T130000Z", e.clock), "END:VEVENT", "DESCRIPTION:Fresh notes\r\nEND:VEVENT", 1))
	d, err = e.w.EventDetail(e.ctx, "alice", e.c.ID, plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if d.Replaces != plan.ID || d.Event.ID == plan.ID || d.Event.Summary != "Plan v2" || d.Description != "Fresh notes" || d.Event.Rev == plan.Rev || e.notices.Load() != 1 {
		t.Fatalf("detail = %+v notices %d", d, e.notices.Load())
	}
	if got := e.row("Plan v2"); got.ID != d.Event.ID {
		t.Fatalf("cache = %+v", got)
	}
	// A locked event can still be read.
	g := e.row("Guests")
	if d, err = e.w.EventDetail(e.ctx, "alice", e.c.ID, g.ID); err != nil || d.Event.Access == nil || d.Event.Access.Update {
		t.Fatalf("locked detail = %+v %v", d, err)
	}
}

func TestEventDetailEditsOffAndGone(t *testing.T) {
	e := newEditEnv(t, false)
	plan := e.row("Plan")
	before := e.requests()
	_, err := e.w.EventDetail(e.ctx, "alice", e.c.ID, plan.ID)
	wantCode(t, err, EventEditsOff, "")
	if e.requests() != before {
		t.Fatal("contacted the calendar with edits off")
	}
	on := true
	if _, err := e.w.Store.Update(e.ctx, "alice", e.c.ID, Patch{EditEvents: &on}); err != nil {
		t.Fatal(err)
	}
	e.srv.Delete("work", "a.ics")
	_, err = e.w.EventDetail(e.ctx, "alice", e.c.ID, plan.ID)
	wantCode(t, err, EventGone, "")
	if !errors.Is(err, ErrEventGone) {
		t.Fatalf("err = %v", err)
	}
	managed, _ := e.w.Feeds.ManagedOf(e.ctx, e.c.ID)
	if _, rerr := e.w.Feeds.RefByEvent(e.ctx, managed[e.srv.CalendarURL("work")].SubscriptionID, plan.ID); !errors.Is(rerr, calfeed.ErrNotFound) {
		t.Fatalf("ref = %v", rerr)
	}
}

func TestCreateEventGraph(t *testing.T) {
	h := newHarness(t)
	ms, c := h.microsoft("W. Europe Standard Time")
	on := true
	if _, err := h.w.Store.Update(h.ctx, "alice", c.ID, Patch{EditEvents: &on}); err != nil {
		t.Fatal(err)
	}
	h.cycle(h.conn(c.ID))
	res, err := h.w.CreateEvent(h.ctx, "alice", c.ID, "cal-work", newCreate("client-0001"))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Created || res.Event.Summary != "New call" || res.Provider != ProviderMicrosoft || res.Event.Rev == "" || len(ms.Events("cal-work")) != 1 {
		t.Fatalf("result = %+v", res)
	}
	if _, uid, ok, _ := h.w.Feeds.CreatedBy(h.ctx, c.ID, "client-0001"); !ok || uid != ms.Events("cal-work")[0].ID {
		t.Fatalf("record uid = %q, graph id = %q", uid, ms.Events("cal-work")[0].ID)
	}
	again, err := h.w.CreateEvent(h.ctx, "alice", c.ID, "cal-work", newCreate("client-0001"))
	if err != nil || again.Created || again.Event.ID != res.Event.ID || len(ms.Events("cal-work")) != 1 {
		t.Fatalf("replay = %+v %v", again, err)
	}
	d, err := h.w.EventDetail(h.ctx, "alice", c.ID, res.Event.ID)
	if err != nil || d.Description != "Notes" || !d.DescriptionEditable || d.Replaces != "" {
		t.Fatalf("detail = %+v %v", d, err)
	}
}
