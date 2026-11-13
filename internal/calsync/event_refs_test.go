package calsync

import (
	"strings"
	"testing"

	"github.com/OpenTodoApp/opentodo/internal/calfeed"
	"github.com/OpenTodoApp/opentodo/internal/calsync/calsynctest"
	"github.com/OpenTodoApp/opentodo/internal/calsync/graphtest"
)

// Remote refs, locks, capabilities and the edit option (OpenSpec change
// calendar-event-edits, tasks 1.3 to 1.6).

// refsOf returns the refs of the connection's managed calendar by event uid.
func (h *harness) refsOf(c *Connection, calURL string) map[string]calfeed.EventRef {
	h.t.Helper()
	managed, err := h.w.Feeds.ManagedOf(h.ctx, c.ID)
	if err != nil {
		h.t.Fatal(err)
	}
	m, ok := managed[calURL]
	if !ok {
		h.t.Fatalf("no managed calendar %q in %+v", calURL, managed)
	}
	refs, err := h.w.Feeds.Refs(h.ctx, m.SubscriptionID)
	if err != nil {
		h.t.Fatal(err)
	}
	out := map[string]calfeed.EventRef{}
	for _, r := range refs {
		out[r.UID] = r
	}
	return out
}

func TestReadCalendarsStoresRefsAndLocks(t *testing.T) {
	h := newHarness(t)
	h.srv.Put("work", "plain.ics", event("plain", "Plain", "DTSTART:20261021T100000Z", "DTEND:20261021T110000Z", h.clock))
	h.srv.Put("work", "guests.ics", strings.Replace(event("guests", "Guests", "DTSTART:20261022T100000Z", "DTEND:20261022T110000Z", h.clock),
		"END:VEVENT", "ATTENDEE;CN=A:mailto:a@example.org\r\nEND:VEVENT", 1))
	h.srv.Put("work", "weekly.ics", strings.Replace(event("weekly", "Weekly", "DTSTART:20261023T100000Z", "DTEND:20261023T110000Z", h.clock),
		"END:VEVENT", "RRULE:FREQ=WEEKLY;COUNT=3\r\nEND:VEVENT", 1))
	c := h.connect(false)
	h.cycle(c)
	refs := h.refsOf(c, h.srv.CalendarURL("work"))
	if len(refs) != 3 {
		t.Fatalf("refs = %+v", refs)
	}
	if r := refs["plain"]; r.Locked != "" || r.ETag == "" || !strings.HasSuffix(r.Href, "/plain.ics") {
		t.Fatalf("plain = %+v", r)
	}
	if refs["guests"].Locked != LockGuests || refs["weekly"].Locked != "" || refs["weekly"].Series == nil {
		t.Fatalf("locks = %q %q", refs["guests"].Locked, refs["weekly"].Locked)
	}
	// One ref per occurrence: the weekly series has three.
	managed, _ := h.w.Feeds.ManagedOf(h.ctx, c.ID)
	all, _ := h.w.Feeds.Refs(h.ctx, managed[h.srv.CalendarURL("work")].SubscriptionID)
	if len(all) != 5 {
		t.Fatalf("refs per occurrence = %d, want 5", len(all))
	}
	// Re-read on a change: the refs follow the events.
	h.srv.Delete("work", "plain.ics")
	h.cycle(h.conn(c.ID))
	if refs := h.refsOf(c, h.srv.CalendarURL("work")); len(refs) != 2 {
		t.Fatalf("refs after delete = %+v", refs)
	} else if _, ok := refs["plain"]; ok {
		t.Fatalf("the deleted event kept its ref: %+v", refs)
	}
}

func TestGraphLocks(t *testing.T) {
	h := newHarness(t)
	ms, c := h.microsoft("W. Europe Standard Time")
	add := func(subject, day string, f func(e *graphtest.Event)) string {
		ev := graphtest.Event{Calendar: "cal-work", Subject: subject,
			Start: graphtest.DT{DateTime: day + "T10:00:00", TimeZone: "UTC"}, End: graphtest.DT{DateTime: day + "T11:00:00", TimeZone: "UTC"}}
		f(&ev)
		return ms.Add(ev)
	}
	plain := add("Plain", "2026-10-21", func(*graphtest.Event) {})
	series := add("Series", "2026-10-22", func(e *graphtest.Event) { e.Type = "occurrence" })
	guests := add("Guests", "2026-10-23", func(e *graphtest.Event) { e.Attendees = []string{"a@example.org"} })
	invited := add("Invited", "2026-10-24", func(e *graphtest.Event) { e.NotOrganizer = true })
	h.cycle(c)
	refs := h.refsOf(c, "cal-work")
	want := map[string]string{plain: "", series: LockSeriesUnsupported, guests: LockGuests, invited: LockNotOrganizer}
	for id, reason := range want {
		r, ok := refs[id]
		if !ok || r.Locked != reason || r.ETag == "" || r.Href != id {
			t.Fatalf("event %s: %+v (want lock %q)", id, r, reason)
		}
	}
}

func TestCapsFromPrivileges(t *testing.T) {
	h := newHarness(t)
	for id, privs := range map[string][]string{
		"all":    {"all"},
		"write":  {"write"},
		"bind":   {"bind"},
		"update": {"write-content"},
		"mixed":  {"write-content", "unbind"},
		"none":   {},
	} {
		h.srv.AddCalendar(id, &calsynctest.Calendar{Name: id})
		h.srv.SetPrivileges(id, privs...)
	}
	h.srv.AddCalendar("silent", &calsynctest.Calendar{Name: "silent"})
	h.srv.NoPrivileges("silent")
	h.srv.AddCalendar("ro", &calsynctest.Calendar{Name: "ro", ReadOnly: true})
	_, d, err := h.w.DiscoverCalDAV(h.ctx, h.srv.URL, h.srv.User, h.srv.Password)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Caps{}
	for _, cal := range d.Calendars {
		if cal.Caps == nil {
			t.Fatalf("%s has no caps", cal.Name)
		}
		got[cal.Name] = *cal.Caps
	}
	yes, no := Caps{true, true, true}, Caps{}
	want := map[string]Caps{"all": yes, "write": yes, "bind": {Create: true}, "update": {Update: true}, "mixed": {Update: true, Delete: true},
		"none": no, "silent": yes, "ro": no, "Work": yes}
	for name, w := range want {
		if got[name] != w {
			t.Errorf("%s caps = %+v, want %+v", name, got[name], w)
		}
	}
}

func TestGraphCaps(t *testing.T) {
	h := newHarness(t)
	ms, c := h.microsoft("W. Europe Standard Time")
	ms.AddCalendar(&graphtest.Calendar{ID: "cal-ro", Name: "Holidays"})
	if err := h.w.Rediscover(h.ctx, c); err != nil {
		t.Fatal(err)
	}
	for _, cal := range h.conn(c.ID).Calendars {
		want := Caps{true, true, true}
		if cal.ReadOnly {
			want = Caps{}
		}
		if cal.Caps == nil || *cal.Caps != want {
			t.Fatalf("%s caps = %+v, read_only = %v", cal.Name, cal.Caps, cal.ReadOnly)
		}
	}
}

func TestEffectiveCapsAndSetCaps(t *testing.T) {
	// A calendar saved before the change has none: derived from ReadOnly.
	if got := (Calendar{}).EffectiveCaps(); got != (Caps{true, true, true}) {
		t.Fatalf("writable without caps = %+v", got)
	}
	if got := (Calendar{ReadOnly: true}).EffectiveCaps(); got != (Caps{}) {
		t.Fatalf("read-only without caps = %+v", got)
	}
	h := newHarness(t)
	c := h.connect(false)
	url := h.srv.CalendarURL("work")
	if err := h.w.Store.SetCaps(h.ctx, c.ID, url, Caps{}); err != nil {
		t.Fatal(err)
	}
	if cal := h.conn(c.ID).Calendar(url); cal == nil || cal.EffectiveCaps() != (Caps{}) {
		t.Fatalf("after SetCaps = %+v", cal)
	}
	// Rediscovery brings the account's own answer back.
	if err := h.w.Rediscover(h.ctx, h.conn(c.ID)); err != nil {
		t.Fatal(err)
	}
	if got := h.conn(c.ID).Calendar(url).EffectiveCaps(); got != (Caps{true, true, true}) {
		t.Fatalf("after rediscovery = %+v", got)
	}
}

func TestEditEventsOption(t *testing.T) {
	h := newHarness(t)
	c := h.connect(false)
	if c.EditEvents || h.w.Store.EditEvents(h.ctx, c.ID) {
		t.Fatal("edit_events must default to off")
	}
	on := true
	got, err := h.w.Store.Update(h.ctx, "alice", c.ID, Patch{EditEvents: &on})
	if err != nil || !got.EditEvents || !h.w.Store.EditEvents(h.ctx, c.ID) {
		t.Fatalf("on = %+v %v", got, err)
	}
	if list, _ := h.w.Store.List(h.ctx, "alice"); len(list) != 1 || !list[0].EditEvents {
		t.Fatalf("list = %+v", list)
	}
	if err := h.w.Store.SetEditEvents(h.ctx, "alice", c.ID, false); err != nil || h.w.Store.EditEvents(h.ctx, c.ID) || h.conn(c.ID).EditEvents {
		t.Fatalf("off: %v", err)
	}
	if err := h.w.Store.SetEditEvents(h.ctx, "bob", c.ID, true); err != ErrNotFound {
		t.Fatalf("another user connection = %v", err)
	}
	if err := h.w.Store.SetEditEvents(h.ctx, "alice", c.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := h.w.Store.Delete(h.ctx, "alice", c.ID); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := h.db.QueryRow(`SELECT COUNT(*) FROM calsync_event_edit`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("rows after delete = %d %v", n, err)
	}
}

func TestFakeServerCounters(t *testing.T) {
	h := newHarness(t)
	h.srv.Put("work", "a.ics", event("a", "A", "DTSTART:20261021T100000Z", "DTEND:20261021T110000Z", h.clock))
	c := h.connect(false)
	h.cycle(c)
	if h.srv.Count("PROPFIND") == 0 || h.srv.Count("REPORT") == 0 {
		t.Fatal("method counters did not count")
	}
	if h.srv.Unconditional() != 0 {
		t.Fatalf("unconditional writes = %d", h.srv.Unconditional())
	}
}
