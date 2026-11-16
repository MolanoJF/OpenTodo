package calfeed

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// The repeat form of the editor and its RRULE (design D4, D8).

// Repeat is a repeat rule in the form the editor edits (a subset of RRULE).
type Repeat struct {
	Freq     string    `json:"freq"`     // daily, weekly, monthly, yearly
	Interval int       `json:"interval"` // 1 to 99
	Days     []string  `json:"days"`     // mo..su, weekly only; empty: the start's weekday
	By       string    `json:"by,omitempty"`
	Ends     RepeatEnd `json:"ends"`
}

// RepeatEnd is never ({Type: "never"}), on a date ({Type: "on", Date}) or after a count.
type RepeatEnd struct {
	Type  string `json:"type"`
	Date  string `json:"date,omitempty"` // YYYY-MM-DD, in the event's own zone: the last day an occurrence may start on
	Count int    `json:"count,omitempty"`
}

// Repeat kinds and ends.
const (
	EndNever = "never"
	EndOn    = "on"
	EndAfter = "after"
	ByDay    = "monthday"
	ByWeekd  = "weekday"
)

// ErrRepeatInvalid is a repeat the editor's form does not allow.
var ErrRepeatInvalid = errors.New("invalid repeat")

var dayCodes = []string{"MO", "TU", "WE", "TH", "FR", "SA", "SU"}

func dayCode(d time.Weekday) string { return dayCodes[(int(d)+6)%7] }

func dayIndex(code string) int {
	for i, c := range dayCodes {
		if strings.EqualFold(c, code) {
			return i
		}
	}
	return -1
}

// Normalize fills what a form leaves out, for an event that starts at start:
// interval 1, the start's weekday for a weekly repeat, the day of the month
// for a monthly one, an end of "never", and the days in week order.
func (r Repeat) Normalize(start Point) Repeat {
	if r.Interval == 0 {
		r.Interval = 1
	}
	r.Freq = strings.ToLower(r.Freq)
	if r.Ends.Type == "" {
		r.Ends.Type = EndNever
	}
	days := map[int]bool{}
	for _, d := range r.Days {
		if i := dayIndex(d); i >= 0 {
			days[i] = true
		}
	}
	r.Days = []string{}
	if r.Freq == "weekly" {
		if len(days) == 0 {
			days[(int(civil(start).Weekday())+6)%7] = true
		}
	}
	for i := range dayCodes {
		if days[i] {
			r.Days = append(r.Days, strings.ToLower(dayCodes[i]))
		}
	}
	if r.Freq == "monthly" && r.By == "" {
		r.By = ByDay
	}
	return r
}

// validate checks a normalised repeat.
func (r Repeat) validate(start Point) error {
	bad := func(why string) error { return fmt.Errorf("%w: %s", ErrRepeatInvalid, why) }
	switch r.Freq {
	case "daily", "weekly", "monthly", "yearly":
	default:
		return bad("freq")
	}
	if r.Interval < 1 || r.Interval > 99 {
		return bad("interval")
	}
	if r.Freq != "weekly" && len(r.Days) > 0 {
		return bad("days")
	}
	if r.Freq != "monthly" && r.By != "" {
		return bad("by")
	}
	if r.Freq == "monthly" && r.By != ByDay && r.By != ByWeekd {
		return bad("by")
	}
	switch r.Ends.Type {
	case EndNever:
	case EndOn:
		d, err := time.Parse("2006-01-02", r.Ends.Date)
		if err != nil || d.Before(time.Date(civil(start).Year(), civil(start).Month(), civil(start).Day(), 0, 0, 0, 0, time.UTC)) {
			return bad("ends")
		}
	case EndAfter:
		if r.Ends.Count < 1 || r.Ends.Count > 999 {
			return bad("ends")
		}
	default:
		return bad("ends")
	}
	return nil
}

// ordinal is the nth weekday of the month a day is, the fifth being -1 (last).
func ordinal(day int) int {
	n := (day-1)/7 + 1
	if n == 5 {
		return -1
	}
	return n
}

// RRule writes a repeat as an RRULE value for an event that starts at start:
// an UNTIL is the end of its day in the start's zone, in UTC for a zoned start
// (RFC 5545 3.3.10), a date for all-day and floating for floating. A weekly
// repeat that does not include the start's weekday is ErrRepeatStart.
func (r Repeat) RRule(start Point) (string, error) {
	for _, d := range r.Days {
		if dayIndex(d) < 0 {
			return "", fmt.Errorf("%w: days", ErrRepeatInvalid)
		}
	}
	r = r.Normalize(start)
	if err := r.validate(start); err != nil {
		return "", err
	}
	c := civil(start)
	parts := []string{"FREQ=" + strings.ToUpper(r.Freq)}
	if r.Interval > 1 {
		parts = append(parts, "INTERVAL="+strconv.Itoa(r.Interval))
	}
	switch r.Freq {
	case "weekly":
		sd := strings.ToLower(dayCode(c.Weekday()))
		has := false
		for _, d := range r.Days {
			has = has || d == sd
		}
		if !has {
			return "", ErrRepeatStart
		}
		if len(r.Days) > 1 {
			up := make([]string, len(r.Days))
			for i, d := range r.Days {
				up[i] = strings.ToUpper(d)
			}
			parts = append(parts, "BYDAY="+strings.Join(up, ","))
			if r.Interval > 1 {
				parts = append(parts, "WKST=MO")
			}
		}
	case "monthly":
		if r.By == ByWeekd {
			parts = append(parts, fmt.Sprintf("BYDAY=%d%s", ordinal(c.Day()), dayCode(c.Weekday())))
		} else {
			parts = append(parts, "BYMONTHDAY="+strconv.Itoa(c.Day()))
		}
	}
	switch r.Ends.Type {
	case EndOn:
		d, _ := time.Parse("2006-01-02", r.Ends.Date)
		parts = append(parts, "UNTIL="+endOfDay(d, start))
	case EndAfter:
		parts = append(parts, "COUNT="+strconv.Itoa(r.Ends.Count))
	}
	return strings.Join(parts, ";"), nil
}

// endOfDay is the UNTIL value that ends day d: the last second of the day in
// the start's zone, in UTC for a zoned start.
func endOfDay(d time.Time, start Point) string {
	y, m, day := d.Date()
	switch kindOfPoint(start) {
	case kindDate:
		return d.Format("20060102")
	case kindFloating:
		return time.Date(y, m, day, 23, 59, 59, 0, time.UTC).Format("20060102T150405")
	}
	return fromCivil(time.Date(y, m, day, 23, 59, 59, 0, time.UTC), start).T.UTC().Format("20060102T150405Z")
}

// ruleKeys are the RRULE parts a Repeat can say.
var ruleKeys = map[string]bool{"FREQ": true, "INTERVAL": true, "COUNT": true, "UNTIL": true, "BYDAY": true, "BYMONTHDAY": true, "BYMONTH": true, "WKST": true}

// RepeatOf reads an RRULE in the editor's form; ok is false when the form cannot
// say exactly what the rule says (any other part, BYSETPOS, ordinals on weekly
// days, a WKST that changes a weekly interval, a BYMONTHDAY that is not the
// start's day...): that is the one place a rule is "custom".
func RepeatOf(rrule string, start Point) (Repeat, bool) {
	for _, p := range splitParts(rrule) {
		if !ruleKeys[p[0]] {
			return Repeat{}, false
		}
	}
	r, _, ok := parseRule(rrule, start)
	if !ok || r.interval < 1 || r.interval > 99 || len(r.bySetPos) > 0 || (r.count > 0 && r.until != nil) {
		return Repeat{}, false
	}
	out := Repeat{Freq: strings.ToLower(r.freq), Interval: r.interval, Days: []string{}, Ends: RepeatEnd{Type: EndNever}}
	c := civil(start)
	if !shapeOf(&out, r, c) {
		return Repeat{}, false
	}
	switch {
	case r.count > 0 && r.count <= 999:
		out.Ends = RepeatEnd{Type: EndAfter, Count: r.count}
	case r.count > 999:
		return Repeat{}, false
	case r.until != nil:
		d, ok := untilDate(*r.until, start)
		if !ok {
			return Repeat{}, false
		}
		out.Ends = RepeatEnd{Type: EndOn, Date: d}
	}
	return out, true
}

// shapeOf fills the frequency-specific part of a repeat, false for a custom shape.
func shapeOf(out *Repeat, r rule, c time.Time) bool {
	switch r.freq {
	case "DAILY":
		return len(r.byDay) == 0 && len(r.byMonthDay) == 0 && len(r.byMonth) == 0
	case "WEEKLY":
		if len(r.byMonthDay) > 0 || len(r.byMonth) > 0 {
			return false
		}
		days := map[int]bool{}
		for _, d := range r.byDay {
			if d.n != 0 {
				return false
			}
			days[(int(d.day)+6)%7] = true
		}
		if len(r.byDay) == 0 {
			days[(int(c.Weekday())+6)%7] = true
		}
		if !days[(int(c.Weekday())+6)%7] {
			return false
		}
		if r.interval > 1 && len(days) > 1 && r.wkst != time.Monday {
			return false
		}
		for i := range dayCodes {
			if days[i] {
				out.Days = append(out.Days, strings.ToLower(dayCodes[i]))
			}
		}
		return true
	case "MONTHLY":
		if len(r.byMonth) > 0 {
			return false
		}
		switch {
		case len(r.byDay) == 0 && len(r.byMonthDay) <= 1:
			if len(r.byMonthDay) == 1 && r.byMonthDay[0] != c.Day() {
				return false
			}
			out.By = ByDay
		case len(r.byMonthDay) == 0 && len(r.byDay) == 1:
			if r.byDay[0].day != c.Weekday() || r.byDay[0].n != ordinal(c.Day()) {
				return false
			}
			out.By = ByWeekd
		default:
			return false
		}
		return true
	case "YEARLY":
		return len(r.byDay) == 0 && (len(r.byMonthDay) == 0 || (len(r.byMonthDay) == 1 && r.byMonthDay[0] == c.Day())) &&
			(len(r.byMonth) == 0 || (len(r.byMonth) == 1 && r.byMonth[0] == int(c.Month())))
	}
	return false
}

// untilDate reads an UNTIL as "the last day an occurrence may start on", when
// that is exactly what the rule says: no occurrence of the start's time of day
// is left out on that day.
func untilDate(until, start Point) (string, bool) {
	if until.AllDay {
		return until.T.Format("2006-01-02"), true
	}
	if start.AllDay {
		return "", false
	}
	uc := civil(until)
	if until.Zone == "UTC" && kindOfPoint(start) == kindZoned {
		uc = civil(Point{T: until.T, Zone: start.Zone})
	}
	sc := civil(start)
	clock := func(t time.Time) int { return t.Hour()*3600 + t.Minute()*60 + t.Second() }
	if clock(uc) < clock(sc) {
		return "", false
	}
	return uc.Format("2006-01-02"), true
}

// ---- rule text ----

// splitParts reads an RRULE value as ordered key and value pairs, keys in upper case.
func splitParts(v string) [][2]string {
	var out [][2]string
	for _, part := range strings.Split(v, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		k, val, _ := strings.Cut(part, "=")
		out = append(out, [2]string{strings.ToUpper(strings.TrimSpace(k)), strings.TrimSpace(val)})
	}
	return out
}

func joinParts(parts [][2]string) string {
	out := make([]string, len(parts))
	for i, p := range parts {
		out[i] = p[0] + "=" + p[1]
	}
	return strings.Join(out, ";")
}

// untilValue writes an instance as an UNTIL for a rule of the start's form.
func untilValue(p Point) string {
	switch kindOfPoint(p) {
	case kindDate:
		return p.T.Format("20060102")
	case kindFloating:
		return p.T.Format("20060102T150405")
	}
	return p.T.UTC().Format("20060102T150405Z")
}

// ErrFirstInstance is SplitRule at the first instance: there is nothing before it.
var ErrFirstInstance = errors.New("first instance has nothing before it")

// SplitRule splits the rule of a series that starts at start at the instance
// at: before is the rule of the old series, which ends with the instance before
// at (UNTIL, UTC for a zoned start; COUNT is dropped), and after the rule of the
// new one (COUNT lowered by the instances before at; UNTIL kept).
func SplitRule(rrule string, start, at Point) (before, after string, err error) {
	o := &sobj{mev: Event{RRule: rrule, Start: start}}
	r, ok := o.rule()
	if !ok {
		return "", "", ErrSeriesUnsupported
	}
	list, complete := o.instances(at)
	if !complete {
		return "", "", ErrSeriesUnsupported
	}
	idx := indexOfKey(list, instanceKey(at))
	switch {
	case idx < 0:
		return "", "", ErrNoSuchInstance
	case idx == 0:
		return "", "", ErrFirstInstance
	}
	if r.count > 0 && r.count-idx < 1 {
		return "", "", ErrNoSuchInstance // the rule ends before the instance: not one of its occurrences
	}
	var b, a [][2]string
	for _, p := range splitParts(rrule) {
		switch p[0] {
		case "COUNT":
			a = append(a, [2]string{"COUNT", strconv.Itoa(r.count - idx)})
		case "UNTIL":
			a = append(a, p)
		default:
			b, a = append(b, p), append(a, p)
		}
	}
	b = append(b, [2]string{"UNTIL", untilValue(list[idx-1])})
	return joinParts(b), joinParts(a), nil
}

// rotateDays moves the BYDAY of a rule by days, keeping ordinals.
func rotateDays(parts [][2]string, days int) {
	for i, p := range parts {
		if p[0] != "BYDAY" {
			continue
		}
		var out []string
		for _, d := range strings.Split(p[1], ",") {
			d = strings.TrimSpace(d)
			if len(d) < 2 {
				continue
			}
			idx := dayIndex(d[len(d)-2:])
			if idx < 0 {
				out = append(out, d)
				continue
			}
			out = append(out, d[:len(d)-2]+dayCodes[((idx+days)%7+7)%7])
		}
		parts[i][1] = strings.Join(out, ",")
	}
}

// canRotate reports whether a rule's instances move as a whole by days days
// (design D6): daily and weekly rules do, monthly and yearly ones do not, and
// a weekly interval above one only when no day crosses a week boundary.
func canRotate(r rule, start Point, days int) bool {
	if days == 0 {
		return true
	}
	if len(r.byMonthDay) > 0 || len(r.byMonth) > 0 || len(r.bySetPos) > 0 {
		return false
	}
	switch r.freq {
	case "DAILY":
		return true
	case "WEEKLY":
		if r.interval == 1 {
			return true
		}
		pos := func(d time.Weekday) int { return (int(d) - int(r.wkst) + 7) % 7 }
		step := days % 7
		check := []int{pos(civil(start).Weekday())}
		for _, d := range r.byDay {
			check = append(check, pos(d.day))
		}
		sort.Ints(check)
		for _, p := range check {
			if p+step < 0 || p+step > 6 {
				return false
			}
		}
		return true
	}
	return false
}
