package calfeed

import (
	"context"
	"errors"
	"testing"
	"time"
)

func createdOcc(uid, day string) Occurrence {
	start := time.Date(2026, 11, 13, 14, 0, 0, 0, time.UTC)
	if day != "" {
		start, _ = time.Parse("2006-01-02T15:04", day+"T14:00")
	}
	ev := Event{UID: uid, Summary: "New", Start: Point{T: start, Zone: "UTC"}, End: &Point{T: start.Add(time.Hour), Zone: "UTC"}}
	return OccurrenceOf(ev)
}

func countRows(t *testing.T, s *Store, q string, args ...any) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestAddEventStoresAllOrNothing(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	sub := managedSub(t, s)
	if _, err := s.db.Exec(`UPDATE calendar_subscriptions SET etag = 'ctag' WHERE id = ?`, sub); err != nil {
		t.Fatal(err)
	}
	occ := createdOcc("client-0001", "")
	ref, err := s.AddEvent(ctx, sub, occ, EventRemote{Href: "/cal/client-0001.ics", ETag: `"e1"`}, "c1", "client-0001")
	if err != nil {
		t.Fatal(err)
	}
	if ref.EventID != EventID(sub, occ.UID, occ.Instance) || ref.Href != "/cal/client-0001.ics" {
		t.Fatalf("ref = %+v", ref)
	}
	if countRows(t, s, `SELECT COUNT(*) FROM calendar_events WHERE subscription_id = ?`, sub) != 1 ||
		countRows(t, s, `SELECT COUNT(*) FROM calendar_event_remote WHERE subscription_id = ?`, sub) != 1 ||
		countRows(t, s, `SELECT COUNT(*) FROM calendar_event_created`) != 1 {
		t.Fatal("not all three rows were written")
	}
	var etag string
	var n int
	if err := s.db.QueryRow(`SELECT etag, event_count FROM calendar_subscriptions WHERE id = ?`, sub).Scan(&etag, &n); err != nil || etag != "" || n != 1 {
		t.Fatalf("subscription etag %q count %d %v", etag, n, err)
	}
	// A failing record (a connection that does not exist) rolls the whole write back.
	if _, err := s.AddEvent(ctx, sub, createdOcc("client-0002", ""), EventRemote{Href: "/cal/2.ics"}, "nope", "client-0002"); err == nil {
		t.Fatal("record for an unknown connection accepted")
	}
	if countRows(t, s, `SELECT COUNT(*) FROM calendar_events`) != 1 || countRows(t, s, `SELECT COUNT(*) FROM calendar_event_remote`) != 1 {
		t.Fatal("a failed AddEvent left rows behind")
	}
}

func TestAddEventPrunesOldRecords(t *testing.T) {
	s, clk := newStore(t)
	ctx := context.Background()
	sub := managedSub(t, s)
	if _, err := s.AddEvent(ctx, sub, createdOcc("client-old1", ""), EventRemote{Href: "/o.ics", ETag: `"o"`}, "c1", "client-old1"); err != nil {
		t.Fatal(err)
	}
	clk.t = clk.t.Add(31 * 24 * time.Hour)
	if _, err := s.AddEvent(ctx, sub, createdOcc("client-new1", "2026-11-14"), EventRemote{Href: "/n.ics", ETag: `"n"`}, "c1", "client-new1"); err != nil {
		t.Fatal(err)
	}
	if _, _, ok, _ := s.CreatedBy(ctx, "c1", "client-old1"); ok {
		t.Fatal("record older than 30 days kept")
	}
	if _, _, ok, _ := s.CreatedBy(ctx, "c1", "client-new1"); !ok {
		t.Fatal("new record missing")
	}
}

func TestCreatedByAndRefByUID(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	sub := managedSub(t, s)
	occ := createdOcc("client-0003", "")
	ref, err := s.AddEvent(ctx, sub, occ, EventRemote{Href: "/c.ics", ETag: `"c"`}, "c1", "client-0003")
	if err != nil {
		t.Fatal(err)
	}
	gotSub, uid, ok, err := s.CreatedBy(ctx, "c1", "client-0003")
	if err != nil || !ok || gotSub != sub || uid != "client-0003" {
		t.Fatalf("CreatedBy = %q %q %v %v", gotSub, uid, ok, err)
	}
	if _, _, ok, err := s.CreatedBy(ctx, "c1", "unknown-id"); ok || err != nil {
		t.Fatalf("unknown client id = %v %v", ok, err)
	}
	// A move: the instance changes, the uid does not.
	moved := createdOcc("client-0003", "2026-11-15")
	newRef, err := s.ReplaceEvent(ctx, sub, ref, &moved, EventRemote{Href: "/c.ics", ETag: `"c2"`})
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.RefByUID(ctx, sub, "client-0003")
	if err != nil || got.EventID != newRef.EventID || got.EventID == ref.EventID || got.ETag != `"c2"` {
		t.Fatalf("RefByUID after a move = %+v %v", got, err)
	}
	// A delete.
	if _, err := s.ReplaceEvent(ctx, sub, newRef, nil, EventRemote{}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RefByUID(ctx, sub, "client-0003"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("RefByUID after a delete = %v", err)
	}
	// The record outlives the event: that is how a replay after a delete is known.
	if _, _, ok, _ := s.CreatedBy(ctx, "c1", "client-0003"); !ok {
		t.Fatal("record lost with the event")
	}
}
