package calfeed

import (
	"context"
	"testing"
	"time"
)

const seriesICS = "BEGIN:VCALENDAR\r\n" +
	"BEGIN:VEVENT\r\nUID:daily\r\nDTSTART;TZID=Europe/Berlin:20261001T070000\r\nDTEND;TZID=Europe/Berlin:20261001T083000\r\nRRULE:FREQ=DAILY;COUNT=40\r\nEXDATE;TZID=Europe/Berlin:20261114T070000\r\nSUMMARY:Daily\r\nEND:VEVENT\r\n" +
	"BEGIN:VEVENT\r\nUID:daily\r\nRECURRENCE-ID:20261113T060000Z\r\nDTSTART;TZID=Europe/Berlin:20261113T090000\r\nDTEND;TZID=Europe/Berlin:20261113T100000\r\nSUMMARY:Late\r\nEND:VEVENT\r\n" +
	"BEGIN:VEVENT\r\nUID:single\r\nDTSTART;TZID=Europe/Berlin:20261020T100000\r\nDTEND;TZID=Europe/Berlin:20261020T110000\r\nSUMMARY:Single\r\nEND:VEVENT\r\n" +
	"END:VCALENDAR\r\n"

var seriesNow = time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)

func seriesRefs() map[string]EventRemote {
	return map[string]EventRemote{
		"daily":  {Href: "/cal/daily.ics", ETag: `"d1"`, Series: &SeriesInfo{RRule: "FREQ=DAILY;COUNT=40", Repeat: `{"freq":"daily","interval":1,"days":[],"ends":{"type":"after","count":40}}`, First: "20261001T050000Z"}},
		"single": {Href: "/cal/single.ics", ETag: `"s1"`},
	}
}

func storeSeriesFixture(t *testing.T) (*Store, string, []Occurrence) {
	t.Helper()
	s, _ := newStore(t)
	sub := managedSub(t, s)
	cal, err := ParseCalendar([]byte(seriesICS))
	if err != nil {
		t.Fatal(err)
	}
	occ, _ := Expand(cal.Events, StoreWindow(seriesNow), MaxOccurrences)
	if err := s.StoreManagedRefs(context.Background(), sub, "v1", occ, nil, seriesRefs()); err != nil {
		t.Fatal(err)
	}
	return s, sub, occ
}

func count(t *testing.T, s *Store, q string, args ...any) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestSeriesRowsAreStoredWithTheRefs(t *testing.T) {
	s, sub, occ := storeSeriesFixture(t)
	ctx := context.Background()
	if n := count(t, s, `SELECT COUNT(*) FROM calendar_event_series WHERE subscription_id = ?`, sub); n != 1 {
		t.Fatalf("series rows = %d, want 1 (a single event has none)", n)
	}
	rows, err := s.Events(ctx, "u1", "2026-10-01", "2026-12-31")
	if err != nil {
		t.Fatal(err)
	}
	var withSeries, without int
	for _, r := range rows {
		if r.Series == nil {
			without++
			if r.UID != "single" {
				t.Errorf("%s has no series", r.UID)
			}
			continue
		}
		withSeries++
		sv := r.Series
		if sv.ID != SeriesID(sub, "daily") || sv.RRule != "FREQ=DAILY;COUNT=40" || sv.Repeat == nil || sv.Repeat.Ends.Count != 40 || sv.Custom || sv.Rev != r.Rev || sv.Rev == "" {
			t.Fatalf("series of %s: %+v rev %q", r.UID, sv, r.Rev)
		}
		if first := r.Start.Date == "2026-10-01"; sv.First != first {
			t.Errorf("%s: first = %v", r.Start.Date, sv.First)
		}
	}
	if withSeries == 0 || without != 1 {
		t.Fatalf("with series %d, without %d", withSeries, without)
	}
	ref, err := s.RefByEvent(ctx, sub, EventID(sub, "daily", occ[0].Instance))
	if err != nil || ref.Series == nil || ref.Series.ID != SeriesID(sub, "daily") || ref.Series.First != "20261001T050000Z" {
		t.Fatalf("RefByEvent: %+v %v", ref, err)
	}
	single, err := s.RefByEvent(ctx, sub, EventID(sub, "single", "20261020T080000Z"))
	if err != nil || single.Series != nil {
		t.Fatalf("an unknown series is a single event: %+v %v", single, err)
	}
	if uids, err := s.SeriesUIDs(ctx, sub, SeriesID(sub, "daily")); err != nil || len(uids) != 1 || uids[0] != "daily" {
		t.Fatalf("SeriesUIDs = %v %v", uids, err)
	}
	// A new cycle replaces the rows, and a custom rule says so.
	refs := seriesRefs()
	refs["daily"] = EventRemote{Href: "/cal/daily.ics", ETag: `"d2"`, Series: &SeriesInfo{RRule: "FREQ=MONTHLY;BYDAY=1MO,3MO"}}
	if err := s.StoreManagedRefs(ctx, sub, "v2", occ, nil, refs); err != nil {
		t.Fatal(err)
	}
	rows, _ = s.Events(ctx, "u1", "2026-10-01", "2026-10-02")
	if len(rows) == 0 || rows[0].Series == nil || !rows[0].Series.Custom || rows[0].Series.Repeat != nil {
		t.Fatalf("custom rule: %+v", rows[0].Series)
	}
	// A cycle without refs drops the series rows too.
	if err := s.StoreManagedRefs(ctx, sub, "v3", occ, nil, nil); err != nil {
		t.Fatal(err)
	}
	if n := count(t, s, `SELECT COUNT(*) FROM calendar_event_series`); n != 0 {
		t.Fatalf("series rows survived: %d", n)
	}
}

func TestReplaceSeriesIsOneTransaction(t *testing.T) {
	s, sub, _ := storeSeriesFixture(t)
	ctx := context.Background()
	tables := func() [3]int {
		return [3]int{count(t, s, `SELECT COUNT(*) FROM calendar_events`), count(t, s, `SELECT COUNT(*) FROM calendar_event_remote`), count(t, s, `SELECT COUNT(*) FROM calendar_event_series`)}
	}
	before := tables()
	if _, err := s.db.Exec(`UPDATE calendar_subscriptions SET etag = 'ctag' WHERE id = ?`, sub); err != nil {
		t.Fatal(err)
	}
	// The second written object fails: nothing of the first may stay, nothing dropped may go.
	if _, err := s.db.Exec(`CREATE TRIGGER boom BEFORE INSERT ON calendar_event_series WHEN NEW.uid = 'boom' BEGIN SELECT RAISE(ABORT, 'boom'); END`); err != nil {
		t.Fatal(err)
	}
	st := time.Date(2026, 11, 20, 8, 0, 0, 0, time.UTC)
	fresh := []Occurrence{
		{UID: "new1", Instance: "20261120T080000Z", Summary: "N1", Start: Point{T: st, Zone: "UTC"}, End: Point{T: st.Add(time.Hour), Zone: "UTC"}},
		{UID: "boom", Instance: "20261120T080000Z", Summary: "N2", Start: Point{T: st, Zone: "UTC"}, End: Point{T: st.Add(time.Hour), Zone: "UTC"}},
	}
	refs := map[string]EventRemote{
		"new1": {Href: "/cal/new1.ics", ETag: `"n1"`, Series: &SeriesInfo{RRule: "FREQ=DAILY"}},
		"boom": {Href: "/cal/boom.ics", ETag: `"n2"`, Series: &SeriesInfo{RRule: "FREQ=DAILY"}},
	}
	if _, err := s.ReplaceSeries(ctx, sub, []string{"daily"}, fresh, refs); err == nil {
		t.Fatal("the failing write succeeded")
	}
	if after := tables(); before != after {
		t.Fatalf("tables changed by a failed ReplaceSeries: %v -> %v", before, after)
	}
	var etag string
	_ = s.db.QueryRow(`SELECT etag FROM calendar_subscriptions WHERE id = ?`, sub).Scan(&etag)
	if etag != "ctag" {
		t.Fatalf("a failed write cleared the version: %q", etag)
	}
	if _, err := s.db.Exec(`DROP TRIGGER boom`); err != nil {
		t.Fatal(err)
	}
	// A good one replaces the old series by the new in all three tables and clears the version.
	out, err := s.ReplaceSeries(ctx, sub, []string{"daily", "gone"}, fresh[:1], refs)
	if err != nil || len(out) != 1 || out[0].UID != "new1" {
		t.Fatalf("ReplaceSeries: %+v %v", out, err)
	}
	for _, table := range []string{"calendar_events", "calendar_event_remote", "calendar_event_series"} {
		if n := count(t, s, `SELECT COUNT(*) FROM `+table+` WHERE uid = 'daily'`); n != 0 {
			t.Errorf("%s still has %d rows of the old series", table, n)
		}
	}
	ref, err := s.RefByEvent(ctx, sub, out[0].EventID)
	if err != nil || ref.Series == nil || ref.ETag != `"n1"` {
		t.Fatalf("the new occurrence: %+v %v", ref, err)
	}
	_ = s.db.QueryRow(`SELECT etag FROM calendar_subscriptions WHERE id = ?`, sub).Scan(&etag)
	if etag != "" {
		t.Errorf("the version is not cleared: %q", etag)
	}
	if n := count(t, s, `SELECT event_count FROM calendar_subscriptions WHERE id = ?`, sub); n != count(t, s, `SELECT COUNT(*) FROM calendar_events`) {
		t.Errorf("event_count = %d", n)
	}
	if err := s.DropSeries(ctx, sub, []string{"new1"}); err != nil {
		t.Fatal(err)
	}
	if n := count(t, s, `SELECT COUNT(*) FROM calendar_events WHERE uid = 'new1'`) + count(t, s, `SELECT COUNT(*) FROM calendar_event_series WHERE uid = 'new1'`); n != 0 {
		t.Errorf("DropSeries left %d rows", n)
	}
}

func TestReplaceEventKeepsTheSeriesRowOfItsUID(t *testing.T) {
	s, sub, _ := storeSeriesFixture(t)
	ctx := context.Background()
	// A Microsoft-like occurrence: the uid is the occurrence's, the series row says which series.
	st := time.Date(2026, 11, 20, 8, 0, 0, 0, time.UTC)
	oc := Occurrence{UID: "occ-1", Instance: "20261120T080000Z", Summary: "M", Start: Point{T: st, Zone: "UTC"}, End: Point{T: st.Add(time.Hour), Zone: "UTC"}}
	remote := EventRemote{Href: "occ-1", ETag: `"m1"`, Series: &SeriesInfo{ID: SeriesID(sub, "master-1")}}
	ref, err := s.AddEvent(ctx, sub, oc, remote, "c1", "client-0001")
	if err != nil {
		t.Fatal(err)
	}
	moved := oc
	moved.Instance = "20261120T090000Z"
	moved.Start.T, moved.End.T = st.Add(time.Hour), st.Add(2*time.Hour)
	if _, err := s.ReplaceEvent(ctx, sub, ref, &moved, EventRemote{Href: "occ-1", ETag: `"m2"`, Series: remote.Series}); err != nil {
		t.Fatal(err)
	}
	if uids, _ := s.SeriesUIDs(ctx, sub, SeriesID(sub, "master-1")); len(uids) != 1 {
		t.Fatalf("series uids = %v", uids)
	}
	if _, err := s.ReplaceEvent(ctx, sub, EventRef{UID: "occ-1", Instance: "20261120T090000Z"}, nil, EventRemote{}); err != nil {
		t.Fatal(err)
	}
	if uids, _ := s.SeriesUIDs(ctx, sub, SeriesID(sub, "master-1")); len(uids) != 0 {
		t.Fatalf("the series row outlived its last event: %v", uids)
	}
}
