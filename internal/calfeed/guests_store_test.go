package calfeed

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func guestOccurrences(t *testing.T) []Occurrence {
	t.Helper()
	cal, err := ParseCalendar([]byte(refsICS))
	if err != nil {
		t.Fatal(err)
	}
	occ, _ := Expand(cal.Events, StoreWindow(time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)), MaxOccurrences)
	return occ
}

func guestCount(t *testing.T, s *Store) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM calendar_event_guests`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func sampleGuests() *GuestInfo {
	return &GuestInfo{
		Organizer: &Guest{Name: "Ana", Email: "ana@example.com", Role: RoleRequired, Response: ResponseAccepted},
		You:       ResponseNeedsAction,
		Count:     2,
		Guests: []Guest{
			{Name: "Ana", Email: "ana@example.com", Role: RoleRequired, Response: ResponseAccepted},
			{Name: "You", Email: "you@example.com", Role: RoleRequired, Response: ResponseNeedsAction, You: true},
			{Email: "room@example.com", Role: RoleRoom, Response: ResponseAccepted},
		},
	}
}

// TestGuestRowsStoredWithRefs: rows are written with the refs in one transaction,
// replaced by the next read and dropped with the series.
func TestGuestRowsStoredWithRefs(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	sub := managedSub(t, s)
	occ := guestOccurrences(t)
	refs := map[string]EventRemote{
		"plain":  {Href: "/cal/plain.ics", ETag: `"e1"`},
		"guests": {Href: "/cal/guests.ics", ETag: `"e2"`, Locked: "not_organizer", Guests: sampleGuests(), Conference: &Conference{URL: "https://meet.google.com/abc-defg-hij", Provider: "meet"}},
		"weekly": {Href: "/cal/weekly.ics", ETag: `"e3"`, Conference: &Conference{URL: "https://acme.zoom.us/j/1", Provider: "zoom"}},
	}
	if err := s.StoreManagedRefs(ctx, sub, "v1", occ, nil, refs); err != nil {
		t.Fatal(err)
	}
	if n := guestCount(t, s); n != 4 { // guests: 1 occurrence, weekly: 3 occurrences
		t.Fatalf("rows = %d, want 4", n)
	}
	rows, err := s.Events(ctx, "u1", "2026-10-19", "2026-11-30")
	if err != nil {
		t.Fatal(err)
	}
	var withGuests, withConf int
	for _, r := range rows {
		if r.Guests != nil {
			withGuests++
			if r.Guests.Count != 2 || r.Guests.You != ResponseNeedsAction || r.Guests.Organizer {
				t.Errorf("summary %+v", r.Guests)
			}
		}
		if r.Conference != nil {
			withConf++
		}
	}
	if withGuests != 1 || withConf != 4 {
		t.Errorf("guests on %d rows, conferences on %d", withGuests, withConf)
	}
	// The next read replaces what was cached: nothing of the old rows stays.
	delete(refs, "weekly")
	refs["guests"] = EventRemote{Href: "/cal/guests.ics", ETag: `"e2"`}
	if err := s.StoreManagedRefs(ctx, sub, "v2", occ, nil, refs); err != nil {
		t.Fatal(err)
	}
	if n := guestCount(t, s); n != 0 {
		t.Fatalf("rows after the replace = %d, want 0", n)
	}
}

// TestGuestRowsReplaceSeriesAndEvent: a scoped write and a single write replace and
// drop the rows of the uids they touch.
func TestGuestRowsReplaceSeriesAndEvent(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	sub := managedSub(t, s)
	occ := guestOccurrences(t)
	refs := map[string]EventRemote{
		"guests": {Href: "/cal/guests.ics", ETag: `"e2"`, Guests: sampleGuests()},
		"weekly": {Href: "/cal/weekly.ics", ETag: `"e3"`, Conference: &Conference{URL: "https://acme.zoom.us/j/1", Provider: "zoom"}},
	}
	if err := s.StoreManagedRefs(ctx, sub, "v1", occ, nil, refs); err != nil {
		t.Fatal(err)
	}
	if err := s.DropSeries(ctx, sub, []string{"weekly"}); err != nil {
		t.Fatal(err)
	}
	if n := guestCount(t, s); n != 1 {
		t.Fatalf("rows after DropSeries = %d, want 1", n)
	}
	var one Occurrence
	for _, o := range occ {
		if o.UID == "guests" {
			one = o
		}
	}
	ref, err := s.RefByUID(ctx, sub, "guests")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReplaceEvent(ctx, sub, ref, &one, EventRemote{Href: ref.Href, ETag: `"e9"`}); err != nil {
		t.Fatal(err)
	}
	if n := guestCount(t, s); n != 0 {
		t.Fatalf("rows after ReplaceEvent without guests = %d, want 0", n)
	}
	if _, err := s.ReplaceEvent(ctx, sub, ref, nil, EventRemote{}); err != nil {
		t.Fatal(err)
	}
	if n := guestCount(t, s); n != 0 {
		t.Fatalf("rows after dropping the event = %d", n)
	}
}

// TestGuestsOfIsScopedToTheUser: another user's event is not served, an event
// without guests is not either, and an over-cap list is cut.
func TestGuestsOfIsScopedToTheUser(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	sub := managedSub(t, s)
	occ := guestOccurrences(t)
	big := &GuestInfo{Count: 130, You: ResponseAccepted}
	for i := 0; i < 130; i++ {
		big.Guests = append(big.Guests, Guest{Name: fmt.Sprintf("G%d", i), Email: fmt.Sprintf("g%d@example.com", i), Role: RoleRequired, Response: ResponseNeedsAction})
	}
	refs := map[string]EventRemote{
		"plain":  {Href: "/cal/plain.ics", ETag: `"e1"`},
		"guests": {Href: "/cal/guests.ics", ETag: `"e2"`, Guests: sampleGuests()},
		"weekly": {Href: "/cal/weekly.ics", ETag: `"e3"`, Guests: big},
	}
	if err := s.StoreManagedRefs(ctx, sub, "v1", occ, nil, refs); err != nil {
		t.Fatal(err)
	}
	id := func(uid string) string {
		for _, o := range occ {
			if o.UID == uid {
				return EventID(sub, o.UID, o.Instance)
			}
		}
		t.Fatalf("no occurrence of %s", uid)
		return ""
	}
	row, ok, err := s.GuestsOf(ctx, "u1", id("guests"))
	if err != nil || !ok {
		t.Fatalf("GuestsOf = %v, %v", ok, err)
	}
	if row.Info.Organizer == nil || row.Info.Organizer.Email != "ana@example.com" || len(row.Info.Guests) != 3 || row.Info.Count != 2 || row.Info.You != ResponseNeedsAction || row.Info.Truncated {
		t.Errorf("row %+v", row.Info)
	}
	if _, ok, _ := s.GuestsOf(ctx, "u2", id("guests")); ok {
		t.Error("another user's guests were served")
	}
	if _, ok, _ := s.GuestsOf(ctx, "u1", id("plain")); ok {
		t.Error("an event without guests has a row")
	}
	if _, ok, _ := s.GuestsOf(ctx, "u1", "nosuchid"); ok {
		t.Error("an unknown event has a row")
	}
	row, ok, err = s.GuestsOf(ctx, "u1", id("weekly"))
	if err != nil || !ok {
		t.Fatalf("GuestsOf big = %v, %v", ok, err)
	}
	if len(row.Info.Guests) != MaxGuests || !row.Info.Truncated || row.Info.Count != 130 {
		t.Errorf("over-cap row: %d guests, truncated %v, count %d", len(row.Info.Guests), row.Info.Truncated, row.Info.Count)
	}
	// The event rows never carry an address.
	rows, err := s.Events(ctx, "u1", "2026-10-19", "2026-11-30")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if b := fmt.Sprintf("%+v", r); strings.Contains(b, "@") {
			t.Errorf("an event row holds an address: %s", b)
		}
	}
}
