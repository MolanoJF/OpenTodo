package calfeed

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/OpenTodoApp/opentodo/internal/caldav"
)

func berlinStart(t *testing.T, date, clock string) Point {
	t.Helper()
	return goldWhen{Date: date, Time: clock, TZ: "Europe/Berlin"}.point(t)
}

func TestRRulesAndExrule(t *testing.T) {
	cal, err := ParseCalendar([]byte("BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nUID:a\r\nDTSTART:20261001T070000Z\r\nRRULE:FREQ=DAILY\r\nRRULE:FREQ=WEEKLY\r\nEXRULE:FREQ=DAILY;COUNT=2\r\nEND:VEVENT\r\nBEGIN:VEVENT\r\nUID:b\r\nDTSTART:20261001T070000Z\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cal.Events[0].RRules != 2 || !cal.Events[0].Exrule || cal.Events[0].RRule != "FREQ=WEEKLY" {
		t.Fatalf("first event: %+v", cal.Events[0])
	}
	if cal.Events[1].RRules != 0 || cal.Events[1].Exrule {
		t.Fatalf("second event: %+v", cal.Events[1])
	}
}

// ---- 1.2: the repeat form ----

// presetWant are the RRULE texts of D8's table for events that start on a Wednesday.
func TestRepeatTable(t *testing.T) {
	wed := berlinStart(t, "2026-11-11", "07:00") // the 2nd Wednesday
	fifth := berlinStart(t, "2026-12-30", "07:00")
	allDay := goldWhen{Date: "2026-11-11"}.point(t)
	floating := goldWhen{Date: "2026-11-11", Time: "07:00"}.point(t)
	utc := goldWhen{Date: "2026-11-11", Time: "07:00", TZ: "UTC"}.point(t)
	never := RepeatEnd{Type: EndNever}
	cases := []struct {
		name  string
		start Point
		r     Repeat
		want  string
	}{
		{"daily", wed, Repeat{Freq: "daily", Ends: never}, "FREQ=DAILY"},
		{"every 3 days", wed, Repeat{Freq: "daily", Interval: 3}, "FREQ=DAILY;INTERVAL=3"},
		{"weekly on the start's day", wed, Repeat{Freq: "weekly"}, "FREQ=WEEKLY"},
		{"weekly on three days", wed, Repeat{Freq: "weekly", Days: []string{"fr", "mo", "we"}}, "FREQ=WEEKLY;BYDAY=MO,WE,FR"},
		{"every weekday", wed, Repeat{Freq: "weekly", Days: []string{"mo", "tu", "we", "th", "fr"}}, "FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR"},
		{"every other week on two days", wed, Repeat{Freq: "weekly", Interval: 2, Days: []string{"mo", "we"}}, "FREQ=WEEKLY;INTERVAL=2;BYDAY=MO,WE;WKST=MO"},
		{"every other week on one day", wed, Repeat{Freq: "weekly", Interval: 2}, "FREQ=WEEKLY;INTERVAL=2"},
		{"monthly by day", wed, Repeat{Freq: "monthly"}, "FREQ=MONTHLY;BYMONTHDAY=11"},
		{"monthly by weekday", wed, Repeat{Freq: "monthly", By: ByWeekd}, "FREQ=MONTHLY;BYDAY=2WE"},
		{"monthly by the fifth weekday", fifth, Repeat{Freq: "monthly", By: ByWeekd}, "FREQ=MONTHLY;BYDAY=-1WE"},
		{"every 2 months", wed, Repeat{Freq: "monthly", Interval: 2}, "FREQ=MONTHLY;INTERVAL=2;BYMONTHDAY=11"},
		{"yearly", wed, Repeat{Freq: "yearly"}, "FREQ=YEARLY"},
		{"ends on a date, zoned", wed, Repeat{Freq: "daily", Ends: RepeatEnd{Type: EndOn, Date: "2026-12-31"}}, "FREQ=DAILY;UNTIL=20261231T225959Z"},
		{"ends on a date, summer time", wed, Repeat{Freq: "daily", Ends: RepeatEnd{Type: EndOn, Date: "2027-07-31"}}, "FREQ=DAILY;UNTIL=20270731T215959Z"},
		{"ends on a date, all-day", allDay, Repeat{Freq: "daily", Ends: RepeatEnd{Type: EndOn, Date: "2026-12-31"}}, "FREQ=DAILY;UNTIL=20261231"},
		{"ends on a date, floating", floating, Repeat{Freq: "daily", Ends: RepeatEnd{Type: EndOn, Date: "2026-12-31"}}, "FREQ=DAILY;UNTIL=20261231T235959"},
		{"ends on a date, UTC", utc, Repeat{Freq: "daily", Ends: RepeatEnd{Type: EndOn, Date: "2026-12-31"}}, "FREQ=DAILY;UNTIL=20261231T235959Z"},
		{"ends after", wed, Repeat{Freq: "weekly", Ends: RepeatEnd{Type: EndAfter, Count: 10}}, "FREQ=WEEKLY;COUNT=10"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := c.r.RRule(c.start)
			if err != nil || got != c.want {
				t.Fatalf("RRule = %q, %v; want %q", got, err, c.want)
			}
			back, ok := RepeatOf(got, c.start)
			if !ok {
				t.Fatalf("RepeatOf(%q) is not ok", got)
			}
			want := c.r.Normalize(c.start)
			if fmt.Sprint(back) != fmt.Sprint(want) {
				t.Fatalf("RepeatOf(RRule(r)) = %+v, want %+v", back, want)
			}
		})
	}
}

// TestRepeatOccurrences compares the occurrences Expand makes of each preset
// with the days the form says, across the spring and autumn clock changes.
func TestRepeatOccurrences(t *testing.T) {
	start := berlinStart(t, "2026-11-11", "07:00")
	cases := []Repeat{
		{Freq: "daily"}, {Freq: "daily", Interval: 3}, {Freq: "weekly"}, {Freq: "weekly", Days: []string{"mo", "we", "fr"}},
		{Freq: "weekly", Interval: 2, Days: []string{"mo", "we"}}, {Freq: "weekly", Interval: 3}, {Freq: "monthly"}, {Freq: "monthly", By: ByWeekd},
		{Freq: "monthly", Interval: 2}, {Freq: "yearly"}, {Freq: "daily", Ends: RepeatEnd{Type: EndAfter, Count: 7}},
		{Freq: "daily", Ends: RepeatEnd{Type: EndOn, Date: "2027-04-02"}}, {Freq: "weekly", Days: []string{"we", "th"}, Ends: RepeatEnd{Type: EndAfter, Count: 9}},
	}
	win := Window{From: start.T.AddDate(0, 0, -1), To: start.T.AddDate(2, 0, 0)}
	for _, r := range cases {
		r = r.Normalize(start)
		text, err := r.RRule(start)
		if err != nil {
			t.Fatal(err)
		}
		occ, _ := Expand([]Event{{UID: "u", Start: start, End: &Point{T: start.T.Add(time.Hour), Zone: start.Zone}, RRule: text}}, win, 5000)
		want := bruteForce(r, start, win.To)
		var got []string
		for _, o := range occ {
			got = append(got, civil(o.Start).Format("2006-01-02T15:04"))
		}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%s: occurrences differ\n got  %v\n want %v", text, got, want)
		}
	}
}

// bruteForce lists the wall-clock starts a repeat form means, day by day.
func bruteForce(r Repeat, start Point, until time.Time) []string {
	first := civil(start)
	var out []string
	var lastDay time.Time
	if r.Ends.Type == EndOn {
		lastDay, _ = time.Parse("2006-01-02", r.Ends.Date)
	}
	days := map[time.Weekday]bool{}
	for _, d := range r.Days {
		days[time.Weekday((dayIndex(d)+1)%7)] = true
	}
	weekOf := func(t time.Time) int { // Monday-based week number counted from the start's week
		monday := func(t time.Time) time.Time { return t.AddDate(0, 0, -((int(t.Weekday()) + 6) % 7)) }
		return int(monday(t).Sub(monday(first)).Hours() / 24 / 7)
	}
	for d := first; d.Before(until); d = d.AddDate(0, 0, 1) {
		if r.Ends.Type == EndOn && d.After(lastDay.Add(24*time.Hour-time.Second)) {
			break
		}
		n := int(d.Sub(first).Hours() / 24)
		ok := false
		switch r.Freq {
		case "daily":
			ok = n%r.Interval == 0
		case "weekly":
			ok = days[d.Weekday()] && weekOf(d)%r.Interval == 0
		case "monthly":
			months := (d.Year()-first.Year())*12 + int(d.Month()) - int(first.Month())
			if months%r.Interval == 0 {
				if r.By == ByWeekd {
					ok = d.Weekday() == first.Weekday() && ordinalOf(d) == ordinalOf(first)
				} else {
					ok = d.Day() == first.Day()
				}
			}
		case "yearly":
			ok = d.Month() == first.Month() && d.Day() == first.Day() && (d.Year()-first.Year())%r.Interval == 0
		}
		if ok {
			out = append(out, d.Format("2006-01-02T15:04"))
			if r.Ends.Type == EndAfter && len(out) == r.Ends.Count {
				break
			}
		}
	}
	return out
}

func ordinalOf(d time.Time) int { return ordinal(d.Day()) }

func TestRepeatOfCustom(t *testing.T) {
	wed := berlinStart(t, "2026-11-11", "07:00")
	for _, rule := range []string{
		"FREQ=MONTHLY;BYSETPOS=-1;BYDAY=MO,TU,WE,TH,FR", "FREQ=MONTHLY;BYDAY=1MO,3MO", "FREQ=MONTHLY;BYMONTHDAY=1,15", "FREQ=MONTHLY;BYMONTHDAY=12",
		"FREQ=MONTHLY;BYDAY=-1WE", "FREQ=WEEKLY;INTERVAL=2;BYDAY=MO,WE;WKST=SU", "FREQ=YEARLY;BYMONTH=3", "FREQ=DAILY;BYDAY=MO", "FREQ=WEEKLY;BYDAY=1MO",
		"FREQ=WEEKLY;BYDAY=MO", "FREQ=HOURLY", "FREQ=DAILY;COUNT=5;UNTIL=20261231T000000Z", "FREQ=DAILY;X-FOO=1", "FREQ=DAILY;BYHOUR=7",
		"FREQ=DAILY;INTERVAL=100", "FREQ=DAILY;COUNT=1000", "FREQ=DAILY;UNTIL=20261231T000000Z", "FREQ=YEARLY;BYMONTH=11;BYMONTHDAY=12", "",
	} {
		if r, ok := RepeatOf(rule, wed); ok {
			t.Errorf("RepeatOf(%q) = %+v, want custom", rule, r)
		}
	}
	// What the form can say, spelled the way other calendars write it.
	for _, rule := range []string{"FREQ=DAILY;UNTIL=20261231T060000Z", "FREQ=YEARLY;BYMONTH=11;BYMONTHDAY=11", "FREQ=WEEKLY;INTERVAL=2;WKST=SU", "FREQ=MONTHLY", "FREQ=DAILY;WKST=SU"} {
		if _, ok := RepeatOf(rule, wed); !ok {
			t.Errorf("RepeatOf(%q) is custom, want a form", rule)
		}
	}
	if _, err := (Repeat{Freq: "weekly", Days: []string{"mo"}}).RRule(wed); !errors.Is(err, ErrRepeatStart) {
		t.Errorf("a weekly repeat that skips the start's weekday: %v", err)
	}
	for _, r := range []Repeat{{Freq: "hourly"}, {Freq: "daily", Interval: 100}, {Freq: "daily", Days: []string{"mo"}}, {Freq: "weekly", Days: []string{"xx"}},
		{Freq: "daily", By: ByDay}, {Freq: "monthly", By: "x"}, {Freq: "daily", Ends: RepeatEnd{Type: EndAfter}}, {Freq: "daily", Ends: RepeatEnd{Type: EndOn, Date: "2026-11-01"}},
		{Freq: "daily", Ends: RepeatEnd{Type: EndOn, Date: "nope"}}, {Freq: "daily", Ends: RepeatEnd{Type: "x"}}} {
		if _, err := r.RRule(wed); !errors.Is(err, ErrRepeatInvalid) {
			t.Errorf("RRule(%+v) = %v, want ErrRepeatInvalid", r, err)
		}
	}
}

func TestSplitRule(t *testing.T) {
	start := berlinStart(t, "2026-10-01", "07:00")
	at := berlinStart(t, "2026-11-16", "07:00")
	before, after, err := SplitRule("FREQ=DAILY;COUNT=60", start, at)
	if err != nil || before != "FREQ=DAILY;UNTIL=20261115T060000Z" || after != "FREQ=DAILY;COUNT=14" {
		t.Fatalf("COUNT split = %q %q %v", before, after, err)
	}
	before, after, err = SplitRule("FREQ=WEEKLY;BYDAY=TH;UNTIL=20271231T225959Z", start, berlinStart(t, "2026-11-12", "07:00"))
	if err != nil || before != "FREQ=WEEKLY;BYDAY=TH;UNTIL=20261105T060000Z" || after != "FREQ=WEEKLY;BYDAY=TH;UNTIL=20271231T225959Z" {
		t.Fatalf("UNTIL split = %q %q %v", before, after, err)
	}
	if _, _, err := SplitRule("FREQ=DAILY", start, start); !errors.Is(err, ErrFirstInstance) {
		t.Errorf("first instance: %v", err)
	}
	if _, _, err := SplitRule("FREQ=WEEKLY;BYDAY=TH", start, berlinStart(t, "2026-11-13", "07:00")); !errors.Is(err, ErrNoSuchInstance) {
		t.Errorf("not an instance: %v", err)
	}
	if _, _, err := SplitRule("FREQ=HOURLY", start, at); !errors.Is(err, ErrSeriesUnsupported) {
		t.Errorf("unsupported rule: %v", err)
	}
	// An all-day series ends with a date, a floating one with a wall time.
	d := goldWhen{Date: "2026-10-01"}.point(t)
	before, _, err = SplitRule("FREQ=DAILY", d, goldWhen{Date: "2026-10-05"}.point(t))
	if err != nil || before != "FREQ=DAILY;UNTIL=20261004" {
		t.Errorf("all-day split = %q %v", before, err)
	}
	f := goldWhen{Date: "2026-10-01", Time: "07:00"}.point(t)
	before, _, err = SplitRule("FREQ=DAILY", f, goldWhen{Date: "2026-10-05", Time: "07:00"}.point(t))
	if err != nil || before != "FREQ=DAILY;UNTIL=20261004T070000" {
		t.Errorf("floating split = %q %v", before, err)
	}
}

// ---- 1.3: reading, locks, instance arithmetic ----

func loadFixture(t *testing.T, name string) *caldav.Component {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "series", name, "in.ics"))
	if err != nil {
		t.Fatal(err)
	}
	root, err := caldav.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func TestSeriesLockedFixtures(t *testing.T) {
	want := map[string]string{
		"locked-thisandfuture": LockSeriesUnsupported, "locked-rdate": LockSeriesUnsupported, "locked-exrule": LockSeriesUnsupported,
		"locked-two-rrules": LockSeriesUnsupported, "locked-unexpandable-rule": LockSeriesUnsupported, "locked-two-masters": LockSeriesUnsupported,
		"locked-guest-in-exception": LockGuests, "locked-orphan-exception": LockSeriesUnsupported, "locked-unknown-zone": LockSeriesUnsupported,
	}
	for name, reason := range want {
		if got := SeriesLocked(loadFixture(t, name), `"e1"`); got != reason {
			t.Errorf("%s: locked %q, want %q (%s)", name, got, reason, unsupportedWhy(loadFixture(t, name)))
		}
	}
	// The unlocked inputs of the goldens are editable.
	for _, name := range []string{"this-move", "all-shift", "dst-crossing", "google-utc-dtstart", "google-date-series-this", "floating-this", "exception-before-master", "exdate-list-all", "duration-master-this"} {
		if got := SeriesLocked(loadFixture(t, name), `"e1"`); got != "" {
			t.Errorf("%s: locked %q (%s)", name, got, unsupportedWhy(loadFixture(t, name)))
		}
	}
	if got := SeriesLocked(loadFixture(t, "this-move"), ""); got != LockNoETag {
		t.Errorf("no etag: %q", got)
	}
}

func TestSeriesLockedLimits(t *testing.T) {
	root := loadFixture(t, "this-move")
	// More than 400 VEVENTs.
	big := cloneComp(root)
	for i := 0; i < MaxSeriesEvents; i++ {
		ex := cloneComp(big.Children[1])
		ex.Children = nil
		big.Children = append(big.Children, ex)
	}
	if why := unsupportedWhy(big); why != "too many events" {
		t.Errorf("many events: %q", why)
	}
	// An object over 256 KiB.
	heavy := cloneComp(root)
	for i := 0; i < 6; i++ {
		heavy.Children[len(heavy.Children)-2].Props = append(heavy.Children[len(heavy.Children)-2].Props,
			caldav.Prop{Name: fmt.Sprintf("X-BLOB-%d", i), Value: strings.Repeat("a", 60000)})
	}
	if why := unsupportedWhy(heavy); why != "too big" {
		t.Errorf("heavy: %q", why)
	}
	// A single event is not a series and has no series reason, whatever its size.
	single, _ := caldav.Parse([]byte("BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nUID:s\r\nDTSTART:20261001T070000Z\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"))
	if got := SeriesLocked(single, "e"); got != "" {
		t.Errorf("single event: %q", got)
	}
	// An attendee in the master.
	guest, _ := caldav.Parse([]byte("BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nUID:s\r\nDTSTART:20261001T070000Z\r\nATTENDEE:mailto:a@b.c\r\nRRULE:FREQ=DAILY\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"))
	if got := SeriesLocked(guest, "e"); got != LockGuests {
		t.Errorf("guest: %q", got)
	}
}

func TestReadSeries(t *testing.T) {
	v, err := ReadSeries(loadFixture(t, "this-move"))
	if err != nil || !v.Recurring || v.RRule != "FREQ=DAILY" || v.Custom || v.Repeat == nil || v.Repeat.Freq != "daily" || v.First != "20261001T050000Z" || v.Exceptions != 2 {
		t.Errorf("daily series: %+v %v", v, err)
	}
	v, _ = ReadSeries(loadFixture(t, "locked-two-rrules"))
	if !v.Recurring {
		t.Errorf("two rules: %+v", v)
	}
	v, _ = ReadSeries(loadFixture(t, "google-utc-dtstart"))
	if v.First != "20261001T060000Z" || v.Repeat == nil || v.Repeat.Ends.Type != EndOn || v.Repeat.Ends.Date != "2026-12-31" {
		t.Errorf("UTC series: %+v", v)
	}
	v, _ = ReadSeries(loadFixture(t, "google-date-series-this"))
	if v.First != "20261001" || v.Repeat == nil {
		t.Errorf("all-day series: %+v", v)
	}
	v, _ = ReadSeries(loadFixture(t, "all-monthly-day-shift-refused"))
	if v.Repeat == nil || v.Repeat.Freq != "monthly" {
		t.Errorf("monthly series: %+v", v)
	}
	custom, _ := caldav.Parse([]byte("BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nUID:s\r\nDTSTART:20261001T070000Z\r\nRRULE:FREQ=MONTHLY;BYDAY=1MO,3MO\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"))
	if v, _ := ReadSeries(custom); !v.Custom || v.Repeat != nil {
		t.Errorf("custom rule: %+v", v)
	}
	single, _ := caldav.Parse([]byte("BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nUID:s\r\nDTSTART:20261001T070000Z\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"))
	if v, _ := ReadSeries(single); v.Recurring {
		t.Errorf("single event: %+v", v)
	}
}

func TestLocateInstances(t *testing.T) {
	o, err := loadSeries(loadFixture(t, "this-move"), "")
	if err != nil {
		t.Fatal(err)
	}
	// A RECURRENCE-ID in UTC under a TZID master is the exception of its instant.
	l, err := o.locate("20261113T060000Z")
	if err != nil || l.exc == nil || civil(l.start).Format("15:04") != "09:00" || civil(l.slot).Format("15:04") != "07:00" {
		t.Fatalf("exception: %+v %v", l, err)
	}
	// Summer time ended 2026-10-25: 2026-10-01 is 05:00Z, 2026-11-12 is 06:00Z.
	if l, err = o.locate("20261001T050000Z"); err != nil || l.idx != 0 {
		t.Fatalf("first instance: %+v %v", l, err)
	}
	if l, err = o.locate("20261112T060000Z"); err != nil || l.idx != 42 || l.exc != nil {
		t.Fatalf("plain instance: %+v %v", l, err)
	}
	for _, key := range []string{"20261114T060000Z", "20261112T070000Z", "20261112", "20261112T060000", "nonsense"} {
		if _, err := o.locate(key); !errors.Is(err, ErrNoSuchInstance) {
			t.Errorf("%s: %v, want no such occurrence", key, err)
		}
	}
	// Floating, all-day and Z masters key their instances in their own form.
	for name, key := range map[string]string{"floating-this": "20261112T070000", "google-date-series-this": "20261112", "google-utc-dtstart": "20261112T060000Z"} {
		o, err := loadSeries(loadFixture(t, name), "")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := o.locate(key); err != nil {
			t.Errorf("%s %s: %v", name, key, err)
		}
	}
	// Wall-clock arithmetic crosses the clock change: 07:00 stays 07:00.
	p := berlinStart(t, "2027-03-27", "07:00")
	q := addWall(p, 24*time.Hour)
	if civil(q).Format("15:04") != "07:00" || q.T.Sub(p.T) != 23*time.Hour {
		t.Errorf("addWall over the spring change: %v %v", q.T, q.T.Sub(p.T))
	}
	if d := wallDiff(q, p); d != 24*time.Hour {
		t.Errorf("wallDiff = %v", d)
	}
}

// ---- 1.4 to 1.7: what the goldens do not say in words ----

func TestSeriesKeepsWhatItDoesNotChange(t *testing.T) {
	dirs, _ := filepath.Glob(filepath.Join("testdata", "series", "*"))
	for _, dir := range dirs {
		if strings.Contains(dir, "locked-") {
			continue
		}
		name := filepath.Base(dir)
		t.Run(name, func(t *testing.T) {
			e, res, _, err := runCase(t, dir)
			if err != nil || len(res.Objects) == 0 {
				return
			}
			in := loadFixture(t, name)
			lines := map[string]int{}
			for _, ob := range res.Objects {
				countLines(ob.Root, lines)
			}
			// Unknown properties and alarms the change did not name survive in some object.
			var lost []string
			for _, c := range in.Children {
				if c.Name != "VEVENT" {
					continue
				}
				for _, p := range c.Props {
					if strings.HasPrefix(p.Name, "X-") && lines[p.Line()] == 0 {
						lost = append(lost, p.Line())
					}
				}
				if e.Change.Reminders == nil {
					for _, a := range c.Children {
						for _, p := range a.Props {
							if lines[p.Line()] == 0 {
								lost = append(lost, p.Line())
							}
						}
					}
				}
			}
			if len(lost) > 0 {
				t.Errorf("lost: %v", lost)
			}
			// The VTIMEZONEs of the input are all still there.
			for _, c := range in.Children {
				if c.Name == "VTIMEZONE" && lines["TZID:"+c.Prop("TZID").Value] == 0 {
					t.Errorf("lost the VTIMEZONE %s", c.Prop("TZID").Value)
				}
			}
		})
	}
}

func countLines(c *caldav.Component, into map[string]int) {
	for _, p := range c.Props {
		into[p.Line()]++
	}
	for _, ch := range c.Children {
		countLines(ch, into)
	}
}

// A this edit never touches the master but for an excluded date.
func TestThisLeavesTheMaster(t *testing.T) {
	root := loadFixture(t, "this-move")
	before := strings.Join(root.Children[1].Lines(), "\n")
	_, err := ApplySeries(SeriesEdit{Root: root, Instance: "20261112T060000Z", Scope: ScopeThis, Now: time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC),
		Change: SeriesChange{Summary: ptr("x")}})
	if err != nil {
		t.Fatal(err)
	}
	if after := strings.Join(root.Children[1].Lines(), "\n"); after != before {
		t.Errorf("master changed:\n%s\n---\n%s", before, after)
	}
	// An edit of an existing exception does not touch the master either.
	before = strings.Join(root.Children[1].Lines(), "\n")
	if _, err := ApplySeries(SeriesEdit{Root: root, Instance: "20261113T060000Z", Scope: ScopeThis, Now: time.Now(), Change: SeriesChange{Summary: ptr("y")}}); err != nil {
		t.Fatal(err)
	}
	if after := strings.Join(root.Children[1].Lines(), "\n"); after != before {
		t.Errorf("master changed by an exception edit")
	}
}

func ptr[T any](v T) *T { return &v }

// An object the engine could not read back is never written.
func TestRoundTripGuard(t *testing.T) {
	for _, name := range []string{"this-move", "all-shift", "floating-this", "google-date-series-this", "reminders-keep-other", "x-props-kept-this"} {
		if !RoundTrips(loadFixture(t, name)) {
			t.Errorf("%s does not round-trip", name)
		}
	}
	// A long folded line, a quoted parameter and an escaped text survive too.
	src := "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nUID:s\r\nDTSTART:20261001T070000Z\r\nRRULE:FREQ=DAILY\r\nDESCRIPTION:" + strings.Repeat("ä\\,x ", 60) +
		"\r\nATTACH;FMTTYPE=\"a/b;c\":https://example.org/x\r\nX-N;P=\"a,b\":v\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
	root, err := caldav.Parse([]byte(src))
	if err != nil || !RoundTrips(root) || SeriesLocked(root, "e") != "" {
		t.Errorf("folded and quoted lines: %v %v %q", err, RoundTrips(root), SeriesLocked(root, "e"))
	}
	// Two trees that differ are not the same.
	a, _ := caldav.Parse([]byte(src))
	b, _ := caldav.Parse([]byte(strings.Replace(src, "X-N;P=\"a,b\":v", "X-N;P=\"a,b\":w", 1)))
	if sameTree(a, b) {
		t.Error("different trees compare equal")
	}
}

func TestApplyRefusals(t *testing.T) {
	now := time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
	root := loadFixture(t, "this-move")
	for name, e := range map[string]SeriesEdit{
		"no scope":        {Root: root, Instance: "20261112T060000Z", Now: now},
		"following uid":   {Root: loadFixture(t, "this-move"), Instance: "20261116T060000Z", Scope: ScopeFollowing, Now: now},
		"locked series":   {Root: loadFixture(t, "locked-rdate"), Instance: "20261112T060000Z", Scope: ScopeThis, Now: now},
		"guest":           {Root: loadFixture(t, "locked-guest-in-exception"), Instance: "20261112T060000Z", Scope: ScopeThis, Now: now},
		"end before span": {Root: loadFixture(t, "this-move"), Instance: "20261112T060000Z", Scope: ScopeThis, Now: now, Change: SeriesChange{When: &Span{Start: berlinStart(t, "2026-11-12", "09:00"), End: berlinStart(t, "2026-11-12", "08:00")}}},
	} {
		if _, err := ApplySeries(e); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	if _, err := DeleteSeries(SeriesEdit{Root: loadFixture(t, "locked-rdate"), Instance: "x", Scope: ScopeAll, Now: now}); !errors.Is(err, ErrSeriesUnsupported) {
		t.Errorf("delete of a locked series: %v", err)
	}
	if _, err := DeleteSeries(SeriesEdit{Root: loadFixture(t, "this-move"), Instance: "20261114T060000Z", Scope: ScopeThis, Now: now}); !errors.Is(err, ErrNoSuchInstance) {
		t.Errorf("delete of an excluded day: %v", err)
	}
	if _, err := DeleteSeries(SeriesEdit{Root: loadFixture(t, "this-move"), Instance: "20261112T060000Z", Scope: "everything", Now: now}); !errors.Is(err, ErrNoSuchInstance) {
		t.Errorf("delete with an unknown scope: %v", err)
	}
}

func TestSeriesUID(t *testing.T) {
	a := NewSeriesUID("u", "i", "e")
	if a != NewSeriesUID("u", "i", "e") || a == NewSeriesUID("u", "i", "e2") || !strings.HasPrefix(a, "ot-") || !strings.HasSuffix(a, "@opentodo") || len(a) != 3+20+9 {
		t.Errorf("uid %q", a)
	}
	if SeriesID("s", "u") == SeriesID("s", "v") || len(SeriesID("s", "u")) != 16 {
		t.Error("series id")
	}
}
