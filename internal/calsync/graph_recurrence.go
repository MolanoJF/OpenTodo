package calsync

import (
	"strings"
	"time"

	"github.com/OpenTodoApp/opentodo/internal/calfeed"
)

// The repeat form of the editor and Graph's patternedRecurrence (OpenSpec change
// calendar-event-series, design D8). Graph says where a series starts with
// range.startDate in range.recurrenceTimeZone; the form's "the start's weekday /
// day of the month" read it from there.

// graphPattern is Graph's recurrence pattern.
type graphPattern struct {
	Type           string   `json:"type"`
	Interval       int      `json:"interval"`
	DaysOfWeek     []string `json:"daysOfWeek,omitempty"`
	FirstDayOfWeek string   `json:"firstDayOfWeek,omitempty"`
	DayOfMonth     int      `json:"dayOfMonth,omitempty"`
	Month          int      `json:"month,omitempty"`
	Index          string   `json:"index,omitempty"`
}

// graphRange is Graph's recurrence range.
type graphRange struct {
	Type                string `json:"type"`
	StartDate           string `json:"startDate"`
	EndDate             string `json:"endDate,omitempty"`
	RecurrenceTimeZone  string `json:"recurrenceTimeZone,omitempty"`
	NumberOfOccurrences int    `json:"numberOfOccurrences,omitempty"`
}

// graphRecurrence is Graph's patternedRecurrence.
type graphRecurrence struct {
	Pattern graphPattern `json:"pattern"`
	Range   graphRange   `json:"range"`
}

var graphDays = []string{"monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday"}

var graphIndexes = []string{"first", "second", "third", "fourth"}

func graphDay(wd time.Weekday) string { return graphDays[(int(wd)+6)%7] }

func graphDayIndex(name string) int {
	for i, d := range graphDays {
		if strings.EqualFold(d, name) {
			return i
		}
	}
	return -1
}

// recurrenceOf writes a repeat as Graph's recurrence for a series that starts on
// start (a date in the series' zone zone, a Windows id).
func recurrenceOf(r calfeed.Repeat, start time.Time, zone string) (*graphRecurrence, error) {
	if _, err := r.RRule(calfeed.Point{T: start}); err != nil { // the form's own rules: the weekday, the ends
		return nil, err
	}
	r = r.Normalize(calfeed.Point{T: start})
	p := graphPattern{Interval: r.Interval}
	switch r.Freq {
	case "daily":
		p.Type = "daily"
	case "weekly":
		p.Type, p.FirstDayOfWeek = "weekly", "monday"
		for _, d := range r.Days {
			p.DaysOfWeek = append(p.DaysOfWeek, graphDays[dayIndexOf(d)])
		}
	case "monthly":
		if r.By == calfeed.ByWeekd {
			p.Type, p.DaysOfWeek = "relativeMonthly", []string{graphDay(start.Weekday())}
			n := (start.Day()-1)/7 + 1
			p.Index = "last"
			if n < 5 {
				p.Index = graphIndexes[n-1]
			}
		} else {
			p.Type, p.DayOfMonth = "absoluteMonthly", start.Day()
		}
	case "yearly":
		p.Type, p.DayOfMonth, p.Month = "absoluteYearly", start.Day(), int(start.Month())
	}
	rg := graphRange{StartDate: start.Format("2006-01-02"), RecurrenceTimeZone: zone}
	switch r.Ends.Type {
	case calfeed.EndOn:
		rg.Type, rg.EndDate = "endDate", r.Ends.Date
	case calfeed.EndAfter:
		rg.Type, rg.NumberOfOccurrences = "numbered", r.Ends.Count
	default:
		rg.Type = "noEnd"
	}
	return &graphRecurrence{Pattern: p, Range: rg}, nil
}

func dayIndexOf(code string) int {
	for i, c := range []string{"mo", "tu", "we", "th", "fr", "sa", "su"} {
		if strings.EqualFold(c, code) {
			return i
		}
	}
	return 0
}

// repeatOfRecurrence reads Graph's recurrence in the editor's form; ok is false
// when the form cannot say exactly what it says (relative yearly, several
// weekdays of a month, a day of the month that is not the start's...).
func repeatOfRecurrence(g *graphRecurrence) (calfeed.Repeat, bool) {
	if g == nil {
		return calfeed.Repeat{}, false
	}
	start, err := time.Parse("2006-01-02", g.Range.StartDate)
	p := g.Pattern
	if err != nil || p.Interval < 1 || p.Interval > 99 {
		return calfeed.Repeat{}, false
	}
	out := calfeed.Repeat{Interval: p.Interval, Days: []string{}, Ends: calfeed.RepeatEnd{Type: calfeed.EndNever}}
	switch strings.ToLower(p.Type) {
	case "daily":
		out.Freq = "daily"
	case "weekly":
		out.Freq = "weekly"
		days := map[int]bool{}
		for _, d := range p.DaysOfWeek {
			i := graphDayIndex(d)
			if i < 0 {
				return calfeed.Repeat{}, false
			}
			days[i] = true
		}
		if !days[(int(start.Weekday())+6)%7] || (p.Interval > 1 && len(days) > 1 && !strings.EqualFold(p.FirstDayOfWeek, "monday")) {
			return calfeed.Repeat{}, false
		}
		for i, code := range []string{"mo", "tu", "we", "th", "fr", "sa", "su"} {
			if days[i] {
				out.Days = append(out.Days, code)
			}
		}
	case "absolutemonthly":
		if p.DayOfMonth != start.Day() {
			return calfeed.Repeat{}, false
		}
		out.Freq, out.By = "monthly", calfeed.ByDay
	case "relativemonthly":
		n := (start.Day()-1)/7 + 1
		want := "last"
		if n < 5 {
			want = graphIndexes[n-1]
		}
		if len(p.DaysOfWeek) != 1 || graphDayIndex(p.DaysOfWeek[0]) != (int(start.Weekday())+6)%7 || !strings.EqualFold(p.Index, want) {
			return calfeed.Repeat{}, false
		}
		out.Freq, out.By = "monthly", calfeed.ByWeekd
	case "absoluteyearly":
		if p.DayOfMonth != start.Day() || p.Month != int(start.Month()) {
			return calfeed.Repeat{}, false
		}
		out.Freq = "yearly"
	default:
		return calfeed.Repeat{}, false
	}
	switch strings.ToLower(g.Range.Type) {
	case "noend":
	case "enddate":
		if _, err := time.Parse("2006-01-02", g.Range.EndDate); err != nil {
			return calfeed.Repeat{}, false
		}
		out.Ends = calfeed.RepeatEnd{Type: calfeed.EndOn, Date: g.Range.EndDate}
	case "numbered":
		if g.Range.NumberOfOccurrences < 1 || g.Range.NumberOfOccurrences > 999 {
			return calfeed.Repeat{}, false
		}
		out.Ends = calfeed.RepeatEnd{Type: calfeed.EndAfter, Count: g.Range.NumberOfOccurrences}
	default:
		return calfeed.Repeat{}, false
	}
	return out, true
}

// rotatedWeekly moves the days of a weekly pattern by days days.
func (p graphPattern) rotated(days int) graphPattern {
	out := p
	out.DaysOfWeek = nil
	for _, d := range p.DaysOfWeek {
		if i := graphDayIndex(d); i >= 0 {
			out.DaysOfWeek = append(out.DaysOfWeek, graphDays[((i+days)%7+7)%7])
		}
	}
	return out
}

// shiftable reports whether the pattern's instances move as a whole by days days:
// daily and weekly ones do (a weekly interval above one only when no day crosses
// the week's Monday), monthly and yearly ones do not.
func (p graphPattern) shiftable(days int) bool {
	switch strings.ToLower(p.Type) {
	case "daily":
		return true
	case "weekly":
		if p.Interval == 1 || days == 0 {
			return true
		}
		step := days % 7
		for _, d := range p.DaysOfWeek {
			if i := graphDayIndex(d); i < 0 || i+step < 0 || i+step > 6 {
				return false
			}
		}
		return true
	}
	return days == 0
}
