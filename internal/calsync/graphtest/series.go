package graphtest

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Series (OpenSpec change calendar-event-series): a master with a recurrence,
// the occurrences Graph generates from it, the exceptions a patch or a delete of
// an occurrence stores, and what Graph does to them when the master changes
// (it drops them). Occurrences are not stored: their id is the master's id, an
// underscore and the date in the series' zone, and their ETag is derived from the
// master's, so it changes when the master does.

// Pattern is Graph's recurrence pattern.
type Pattern struct {
	Type           string   `json:"type"`
	Interval       int      `json:"interval"`
	DaysOfWeek     []string `json:"daysOfWeek,omitempty"`
	FirstDayOfWeek string   `json:"firstDayOfWeek,omitempty"`
	DayOfMonth     int      `json:"dayOfMonth,omitempty"`
	Month          int      `json:"month,omitempty"`
	Index          string   `json:"index,omitempty"`
}

// Range is Graph's recurrence range.
type Range struct {
	Type                string `json:"type"`
	StartDate           string `json:"startDate"`
	EndDate             string `json:"endDate,omitempty"`
	RecurrenceTimeZone  string `json:"recurrenceTimeZone,omitempty"`
	NumberOfOccurrences int    `json:"numberOfOccurrences,omitempty"`
}

// Recurrence is Graph's patternedRecurrence.
type Recurrence struct {
	Pattern Pattern `json:"pattern"`
	Range   Range   `json:"range"`
}

var days = []string{"monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday"}

func dayName(wd time.Weekday) string { return days[(int(wd)+6)%7] }

func hasDay(list []string, wd time.Weekday) bool {
	for _, d := range list {
		if strings.EqualFold(d, dayName(wd)) {
			return true
		}
	}
	return false
}

func parseDate(s string) time.Time {
	t, _ := time.Parse("2006-01-02", s)
	return t
}

func monday(t time.Time) time.Time { return t.AddDate(0, 0, -((int(t.Weekday()) + 6) % 7)) }

func daysIn(t time.Time) int { return time.Date(t.Year(), t.Month()+1, 0, 0, 0, 0, 0, time.UTC).Day() }

// matches says whether the pattern has an occurrence on day d.
func (p Pattern) matches(start, d time.Time) bool {
	n := p.Interval
	if n < 1 {
		n = 1
	}
	months := (d.Year()-start.Year())*12 + int(d.Month()) - int(start.Month())
	switch p.Type {
	case "daily":
		return int(d.Sub(start).Hours()/24)%n == 0
	case "weekly":
		weeks := int(monday(d).Sub(monday(start)).Hours() / 24 / 7)
		return hasDay(p.DaysOfWeek, d.Weekday()) && weeks%n == 0
	case "absoluteMonthly":
		return months%n == 0 && d.Day() == p.DayOfMonth
	case "relativeMonthly":
		if months%n != 0 || len(p.DaysOfWeek) != 1 || !hasDay(p.DaysOfWeek, d.Weekday()) {
			return false
		}
		if p.Index == "last" {
			return d.Day()+7 > daysIn(d)
		}
		idx := map[string]int{"first": 1, "second": 2, "third": 3, "fourth": 4}[p.Index]
		return (d.Day()-1)/7+1 == idx
	case "absoluteYearly":
		return (d.Year()-start.Year())%n == 0 && int(d.Month()) == p.Month && d.Day() == p.DayOfMonth
	}
	return false
}

// dates lists the local dates the series has an occurrence on up to to.
func (m *Event) dates(to time.Time) []string {
	rec := m.Recurrence
	start := parseDate(rec.Range.StartDate)
	last := to.AddDate(0, 0, 2)
	if rec.Range.Type == "endDate" {
		if end := parseDate(rec.Range.EndDate); end.Before(last) {
			last = end
		}
	}
	var out []string
	for d := start; !d.After(last); d = d.AddDate(0, 0, 1) {
		if !rec.Pattern.matches(start, d) {
			continue
		}
		out = append(out, d.Format("2006-01-02"))
		if rec.Range.Type == "numbered" && len(out) >= rec.Range.NumberOfOccurrences {
			break
		}
	}
	return out
}

// occurrence is the generated occurrence of a master on a date.
func (m *Event) occurrence(date string) *Event {
	o := *m
	o.ID = m.ID + "_" + date
	o.Type, o.SeriesMasterID, o.Recurrence, o.Original = "occurrence", m.ID, nil, date
	ms, me := instantParts(m.Start), instantParts(m.End)
	dur := me.Sub(ms)
	clock := m.Start.DateTime[min(11, len(m.Start.DateTime)):]
	if len(clock) < 8 {
		clock = "00:00:00"
	}
	o.Start = DT{DateTime: date + "T" + clock[:8], TimeZone: m.Start.TimeZone}
	end := parseLocal(o.Start.DateTime).Add(dur)
	o.End = DT{DateTime: end.Format("2006-01-02T15:04:05"), TimeZone: m.End.TimeZone}
	o.ETag = fmt.Sprintf(`W/"%s|%s"`, strings.Trim(m.ETag, `W/"`), date)
	return &o
}

func instantParts(d DT) time.Time { return parseLocal(d.DateTime) }

func parseLocal(v string) time.Time {
	t, _ := time.Parse("2006-01-02T15:04:05", v[:min(19, len(v))])
	return t
}

// occurrences lists the live occurrences and exceptions of a master that touch
// the window: generated ones, replaced by the exceptions stored for their date.
func (s *Server) occurrences(m *Event, from, to time.Time) []*Event {
	byID := map[string]*Event{}
	var order []string
	for _, d := range m.dates(to.AddDate(0, 0, 7)) {
		o := m.occurrence(d)
		byID[o.ID] = o
		order = append(order, o.ID)
	}
	for id, e := range s.events {
		if e.SeriesMasterID == m.ID && e.Type == "exception" {
			if _, ok := byID[id]; !ok {
				order = append(order, id)
			}
			byID[id] = e
		}
	}
	var out []*Event
	for _, id := range order {
		if o := byID[id]; !o.Cancelled && s.inWindow(o, from, to) {
			out = append(out, o)
		}
	}
	return out
}

// find returns a stored event, or the generated occurrence an id names.
func (s *Server) find(id string) *Event {
	if e := s.events[id]; e != nil {
		return e
	}
	i := strings.LastIndex(id, "_")
	if i < 0 {
		return nil
	}
	m := s.events[id[:i]]
	if m == nil || m.Type != "seriesMaster" || m.Recurrence == nil {
		return nil
	}
	date := id[i+1:]
	for _, d := range m.dates(parseDate(date)) {
		if d == date {
			return m.occurrence(date)
		}
	}
	return nil
}

// dropExceptions deletes the exceptions of a master, as Graph does when its
// times or its pattern change.
func (s *Server) dropExceptions(masterID string) {
	for id, e := range s.events {
		if e.SeriesMasterID == masterID && e.Type == "exception" {
			s.drop(id)
		}
	}
}

// instances answers GET /me/events/{id}/instances.
func (s *Server) instances(w http.ResponseWriter, r *http.Request) {
	from, to, ok := window(r)
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.events[r.PathValue("id")]
	switch {
	case m == nil || m.Type != "seriesMaster":
		graphError(w, 404, "ErrorItemNotFound")
		return
	case !ok:
		graphError(w, 400, "ErrorInvalidParameter")
		return
	}
	items := []map[string]any{}
	for _, o := range s.occurrences(m, from, to) {
		items = append(items, s.render(o, r, false))
	}
	s.page(w, r, items, "")
}

// Instances lists the occurrences of a master in a window, as the fake would serve them.
func (s *Server) Instances(masterID string, from, to time.Time) []Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.events[masterID]
	if m == nil {
		return nil
	}
	var out []Event
	for _, o := range s.occurrences(m, from, to) {
		out = append(out, *o)
	}
	return out
}

// Event returns a stored event (a master, an exception or a single event).
func (s *Server) Event(id string) (Event, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.events[id]; e != nil {
		return *e, true
	}
	return Event{}, false
}

// dropExceptionsAfter deletes the exceptions of a master that replace an
// occurrence after a date (the series now ends on it).
func (s *Server) dropExceptionsAfter(masterID, date string) {
	for id, e := range s.events {
		if e.SeriesMasterID == masterID && e.Type == "exception" && e.Original > date {
			s.drop(id)
		}
	}
}
