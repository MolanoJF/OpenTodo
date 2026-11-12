package calfeed

import (
	"context"
	"errors"
	"testing"
	"time"
)

const refsICS = "BEGIN:VCALENDAR\r\n" +
	"BEGIN:VEVENT\r\nUID:plain\r\nDTSTART;TZID=Europe/Berlin:20261020T100000\r\nDTEND;TZID=Europe/Berlin:20261020T110000\r\nSUMMARY:Plain\r\nEND:VEVENT\r\n" +
	"BEGIN:VEVENT\r\nUID:guests\r\nDTSTART:20261021T100000Z\r\nDTEND:20261021T110000Z\r\nATTENDEE;CN=A:mailto:a@example.com\r\nSUMMARY:Guests\r\nEND:VEVENT\r\n" +
	"BEGIN:VEVENT\r\nUID:weekly\r\nDTSTART:20261022T100000Z\r\nDTEND:20261022T110000Z\r\nRRULE:FREQ=WEEKLY;COUNT=3\r\nSUMMARY:Weekly\r\nEND:VEVENT\r\n" +
	"END:VCALENDAR\r\n"

func managedSub(t *testing.T, s *Store) string {
	t.Helper()
	ctx := context.Background()
	if _, err := s.db.Exec(`INSERT INTO calsync_connections(id,user_id,provider,server_url,next_sync_at,created_at,updated_at) VALUES('c1','u1','caldav','https://x.test/dav',0,1,1)`); err != nil {
		t.Fatal(err)
	}
	m, err := s.EnsureManaged(ctx, "u1", "c1", "https://x.test/cal", "Work", "")
	if err != nil {
		t.Fatal(err)
	}
	return m.SubscriptionID
}

func TestEventIDMatchesServedID(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	sub := managedSub(t, s)
	cal, err := ParseCalendar([]byte(refsICS))
	if err != nil {
		t.Fatal(err)
	}
	occ, _ := Expand(cal.Events, StoreWindow(time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)), MaxOccurrences)
	if err := s.StoreManaged(ctx, sub, "v1", occ, nil); err != nil {
		t.Fatal(err)
	}
	rows, err := s.Events(ctx, "u1", "2026-10-19", "2026-11-30")
	if err != nil || len(rows) != 5 {
		t.Fatalf("rows = %d %v", len(rows), err)
	}
	want := map[string]bool{}
	for _, o := range occ {
		want[EventID(sub, o.UID, o.Instance)] = true
	}
	for _, r := range rows {
		if !want[r.ID] || !r.Managed || r.Known || r.Rev != "" {
			t.Fatalf("row %+v: id not from EventID, or ref present without refs", r)
		}
	}
}

func TestGuestsParse(t *testing.T) {
	cal, err := ParseCalendar([]byte(refsICS))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, e := range cal.Events {
		got[e.UID] = e.Guests
	}
	if got["plain"] || !got["guests"] || got["weekly"] {
		t.Fatalf("guests = %v", got)
	}
}

func TestStoreManagedRefs(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	sub := managedSub(t, s)
	cal, _ := ParseCalendar([]byte(refsICS))
	occ, _ := Expand(cal.Events, StoreWindow(time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)), MaxOccurrences)
	refs := map[string]EventRemote{
		"plain":  {Href: "/cal/plain.ics", ETag: `"e1"`},
		"guests": {Href: "/cal/guests.ics", ETag: `"e2"`, Locked: "has_guests"},
		"weekly": {Href: "/cal/weekly.ics", ETag: `"e3"`, Locked: "recurring"},
	}
	if err := s.StoreManagedRefs(ctx, sub, "v1", occ, nil, refs); err != nil {
		t.Fatal(err)
	}
	got, err := s.Refs(ctx, sub)
	if err != nil || len(got) != len(occ) {
		t.Fatalf("refs = %d, want one per occurrence (%d) %v", len(got), len(occ), err)
	}
	rows, _ := s.Events(ctx, "u1", "2026-10-19", "2026-11-30")
	byUID := map[string]EventRow{}
	for _, r := range rows {
		byUID[r.UID] = r
	}
	if r := byUID["plain"]; !r.Known || r.Locked != "" || r.Rev != Rev(`"e1"`) || len(r.Rev) != 16 {
		t.Fatalf("plain = %+v", r)
	}
	if r := byUID["guests"]; r.Locked != "has_guests" {
		t.Fatalf("guests = %+v", r)
	}
	ref, err := s.RefByEvent(ctx, sub, byUID["plain"].ID)
	if err != nil || ref.Href != "/cal/plain.ics" || ref.ETag != `"e1"` || ref.UID != "plain" {
		t.Fatalf("ref = %+v %v", ref, err)
	}
	if _, err := s.RefByEvent(ctx, sub, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing ref = %v", err)
	}
	// A re-read replaces them together with the events.
	if err := s.StoreManagedRefs(ctx, sub, "v2", occ[:1], nil, map[string]EventRemote{occ[0].UID: {Href: "/new.ics", ETag: `"n"`}}); err != nil {
		t.Fatal(err)
	}
	got, _ = s.Refs(ctx, sub)
	if len(got) != 1 || got[0].Href != "/new.ics" {
		t.Fatalf("after re-read: %+v", got)
	}
	// Deleting the subscription removes them.
	if _, err := s.db.Exec(`DELETE FROM calendar_subscriptions WHERE id = ?`, sub); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM calendar_event_remote`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("refs after cascade = %d %v", n, err)
	}
}

func TestFeedEventsHaveNoRevOrAccess(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	sub, err := s.Create(ctx, "u1", "https://cal.example/a.ics", "", "")
	if err != nil {
		t.Fatal(err)
	}
	cal, _ := ParseCalendar([]byte(refsICS))
	occ, _ := Expand(cal.Events, StoreWindow(time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)), MaxOccurrences)
	if err := s.Success(ctx, sub.ID, &Result{Replaced: true, Occurrences: occ}); err != nil {
		t.Fatal(err)
	}
	rows, _ := s.Events(ctx, "u1", "2026-10-19", "2026-11-30")
	if len(rows) == 0 {
		t.Fatal("no rows")
	}
	for _, r := range rows {
		if r.Managed || r.Rev != "" || r.Access != nil {
			t.Fatalf("feed row %+v", r)
		}
	}
}
