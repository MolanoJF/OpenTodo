package httpapi

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/OpenTodoApp/opentodo/internal/audit"
)

// Repeating events over HTTP (OpenSpec change calendar-event-series, tasks 3.1
// to 3.4): scope on PATCH and DELETE, the repeat and reminder members, the
// answers and the series the events route serves.

// seriesEnv is the events env plus a daily series with an exception and an
// excluded day, and a custom one.
func newSeriesEnv(t *testing.T) *eventsEnv {
	t.Helper()
	v := newEventsEnv(t)
	d0, _ := time.Parse("2006-01-02", v.day)
	stamp := func(n int) string { return d0.AddDate(0, 0, n).Format("20060102") }
	daily := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\n" +
		"BEGIN:VEVENT\r\nUID:daily\r\nSUMMARY:Daily\r\nDTSTART:" + stamp(0) + "T120000Z\r\nDTEND:" + stamp(0) + "T130000Z\r\nRRULE:FREQ=DAILY\r\nEXDATE:" + stamp(2) + "T120000Z\r\n" +
		"BEGIN:VALARM\r\nACTION:DISPLAY\r\nTRIGGER:-PT10M\r\nDESCRIPTION:Reminder\r\nEND:VALARM\r\nEND:VEVENT\r\n" +
		"BEGIN:VEVENT\r\nUID:daily\r\nRECURRENCE-ID:" + stamp(1) + "T120000Z\r\nSUMMARY:Daily (late)\r\nDTSTART:" + stamp(1) + "T150000Z\r\nDTEND:" + stamp(1) + "T160000Z\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
	custom := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VEVENT\r\nUID:custom\r\nSUMMARY:Custom\r\nDTSTART:" + stamp(0) + "T090000Z\r\nDTEND:" + stamp(0) + "T100000Z\r\nRRULE:FREQ=MONTHLY;BYMONTHDAY=1,15\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
	locked := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VEVENT\r\nUID:locked\r\nSUMMARY:Locked\r\nDTSTART:" + stamp(0) + "T080000Z\r\nDTEND:" + stamp(0) + "T090000Z\r\nRRULE:FREQ=DAILY\r\nRDATE:" + stamp(5) + "T080000Z\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
	v.cal.Put("work", "daily.ics", daily)
	v.cal.Put("work", "custom.ics", custom)
	v.cal.Put("work", "locked.ics", locked)
	resp, out := v.e.do("POST", "/api/v1/calendar-sync/connections/"+v.connID+"/sync", nil)
	v.e.mustStatus(resp, 200, out)
	v.edits(true)
	return v
}

// occurrence finds the served occurrence of a summary on a day offset from tomorrow.
func (v *eventsEnv) occurrence(summary string, offset int) map[string]any {
	v.e.t.Helper()
	d0, _ := time.Parse("2006-01-02", v.day)
	day := d0.AddDate(0, 0, offset).Format("2006-01-02")
	resp, out := v.e.do("GET", "/api/v1/calendar-feeds/events?from="+day+"&to="+day, nil)
	v.e.mustStatus(resp, 200, out)
	for _, x := range out["events"].([]any) {
		m := x.(map[string]any)
		if start, _ := m["start"].(map[string]any); m["summary"] == summary && start["date"] == day {
			return m
		}
	}
	v.e.t.Fatalf("no %q on day %d", summary, offset)
	return nil
}

func TestEventsRouteServesTheSeries(t *testing.T) {
	v := newSeriesEnv(t)
	first, second := v.occurrence("Daily", 0), v.occurrence("Daily", 3)
	sv, sv2 := first["series"].(map[string]any), second["series"].(map[string]any)
	if sv["id"] == "" || sv["id"] != sv2["id"] || sv["rrule"] != "FREQ=DAILY" || sv["custom"] != false || sv["first"] != true || sv2["first"] != false ||
		sv["rev"] != first["rev"] || sv["rev"] != sv2["rev"] || fmt.Sprint(sv["repeat"]) != "map[days:[] ends:map[type:never] freq:daily interval:1]" {
		t.Fatalf("series = %v / %v", sv, sv2)
	}
	if a := accessOf(first); a["update"] != true || a["delete"] != true || a["reason"] != nil {
		t.Fatalf("access = %v", a)
	}
	late := v.occurrence("Daily (late)", 1)
	if late["series"].(map[string]any)["id"] != sv["id"] || late["start"].(map[string]any)["time"] != "15:00" {
		t.Fatalf("exception = %v", late)
	}
	d0, _ := time.Parse("2006-01-02", v.day)
	resp, out := v.e.do("GET", "/api/v1/calendar-feeds/events?from="+d0.AddDate(0, 0, 2).Format("2006-01-02")+"&to="+d0.AddDate(0, 0, 2).Format("2006-01-02"), nil)
	v.e.mustStatus(resp, 200, out)
	for _, x := range out["events"].([]any) {
		if m := x.(map[string]any); m["summary"] == "Daily" && m["start"].(map[string]any)["date"] == d0.AddDate(0, 0, 2).Format("2006-01-02") {
			t.Fatalf("the excluded day is served: %v", x)
		}
	}
	// A custom rule has no repeat; one outside the envelope is locked; a single event has no series.
	custom := v.occurrence("Custom", 0)["series"].(map[string]any)
	if custom["custom"] != true || custom["repeat"] != nil || custom["rrule"] != "FREQ=MONTHLY;BYMONTHDAY=1,15" {
		t.Fatalf("custom series = %v", custom)
	}
	lockedEv := v.occurrence("Locked", 0)
	if a := accessOf(lockedEv); a["update"] != false || a["reason"] != "series_unsupported" || lockedEv["series"] == nil {
		t.Fatalf("locked series = %v %v", a, lockedEv["series"])
	}
	if v.events(v.e)["Plain"]["series"] != nil {
		t.Fatal("a single event is served with a series")
	}
	// The connected calendar says how many reminders an event of it takes.
	resp, out = v.e.do("GET", "/api/v1/calendar-feeds/events?from="+v.day+"&to="+v.day, nil)
	v.e.mustStatus(resp, 200, out)
	if sub := out["subscriptions"].([]any)[0].(map[string]any); sub["maxReminders"] != float64(5) {
		t.Fatalf("subscription = %v", sub)
	}
}

func TestEventPatchScopeRules(t *testing.T) {
	v := newSeriesEnv(t)
	e := v.e
	occ := v.occurrence("Daily", 3)
	id, rev := occ["id"].(string), occ["rev"].(string)
	plain := v.events(e)["Plain"]
	patch := func(id string, body map[string]any) (int, map[string]any) {
		resp, out := e.do("PATCH", v.path(id), body)
		return resp.StatusCode, out
	}
	baseline := v.cal.Count("PUT")
	for name, tc := range map[string]struct {
		id         string
		body       map[string]any
		status     int
		code, text string
	}{
		"an occurrence without scope":    {id, map[string]any{"rev": rev, "summary": "x"}, 400, "scope_required", "this, following or all"},
		"scope on a single event":        {plain["id"].(string), map[string]any{"rev": plain["rev"], "summary": "x", "scope": "this"}, 400, "invalid_input", "scope"},
		"an unknown scope":               {id, map[string]any{"rev": rev, "summary": "x", "scope": "everything"}, 400, "invalid_input", "scope"},
		"repeat with this":               {id, map[string]any{"rev": rev, "scope": "this", "repeat": map[string]any{"freq": "daily"}}, 400, "invalid_input", "repeat"},
		"repeat null with all":           {id, map[string]any{"rev": rev, "scope": "all", "repeat": nil}, 400, "invalid_input", "repeat"},
		"repeat null without a scope":    {id, map[string]any{"rev": rev, "repeat": nil}, 400, "invalid_input", "repeat"},
		"repeat null on a single event":  {plain["id"].(string), map[string]any{"rev": plain["rev"], "repeat": nil}, 400, "invalid_input", "repeat"},
		"an unknown frequency":           {id, map[string]any{"rev": rev, "scope": "all", "repeat": map[string]any{"freq": "hourly"}}, 400, "invalid_input", "repeat"},
		"an interval of 100":             {id, map[string]any{"rev": rev, "scope": "all", "repeat": map[string]any{"freq": "daily", "interval": 100}}, 400, "invalid_input", "repeat"},
		"days on a daily repeat":         {id, map[string]any{"rev": rev, "scope": "all", "repeat": map[string]any{"freq": "daily", "days": []string{"mo"}}}, 400, "invalid_input", "repeat"},
		"a bad weekday":                  {id, map[string]any{"rev": rev, "scope": "all", "repeat": map[string]any{"freq": "weekly", "days": []string{"xx"}}}, 400, "invalid_input", "repeat"},
		"a bad end":                      {id, map[string]any{"rev": rev, "scope": "all", "repeat": map[string]any{"freq": "daily", "ends": map[string]any{"type": "after", "count": 0}}}, 400, "invalid_input", "repeat"},
		"an unknown repeat member":       {id, map[string]any{"rev": rev, "scope": "all", "repeat": map[string]any{"freq": "daily", "every": 2}}, 400, "invalid_input", ""},
		"six reminders":                  {id, map[string]any{"rev": rev, "scope": "all", "reminders": []int{1, 2, 3, 4, 5, 6}}, 400, "invalid_input", "reminders"},
		"a repeated reminder":            {id, map[string]any{"rev": rev, "scope": "all", "reminders": []int{10, 10}}, 400, "invalid_input", "reminders"},
		"a reminder out of range":        {id, map[string]any{"rev": rev, "scope": "all", "reminders": []int{50000}}, 400, "invalid_input", "reminders"},
		"repeat on a series with a rule": {v.occurrence("Custom", 0)["id"].(string), map[string]any{"rev": v.occurrence("Custom", 0)["rev"], "scope": "all", "repeat": map[string]any{"freq": "daily"}}, 400, "invalid_input", "repeat"},
		"a locked series":                {v.occurrence("Locked", 0)["id"].(string), map[string]any{"rev": v.occurrence("Locked", 0)["rev"], "scope": "this", "summary": "x"}, 422, "event_not_editable", "series_unsupported"},
	} {
		status, out := patch(tc.id, tc.body)
		if status != tc.status || errCode(out) != tc.code || !strings.Contains(fmt.Sprint(out), tc.text) {
			t.Errorf("%s: %d %v, want %d %s %q", name, status, out, tc.status, tc.code, tc.text)
		}
	}
	if v.cal.Count("PUT") != baseline {
		t.Fatal("a refused change reached the calendar")
	}
	// A day move of a monthly series is refused with the field; the monthly series is made here.
	d0, _ := time.Parse("2006-01-02", v.day)
	monthly := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VEVENT\r\nUID:monthly\r\nSUMMARY:Monthly\r\nDTSTART:" + d0.Format("20060102") + "T070000Z\r\nDTEND:" + d0.Format("20060102") + "T080000Z\r\nRRULE:FREQ=MONTHLY;BYMONTHDAY=" + fmt.Sprint(d0.Day()) + "\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
	v.cal.Put("work", "monthly.ics", monthly)
	resp, out := e.do("POST", "/api/v1/calendar-sync/connections/"+v.connID+"/sync", nil)
	e.mustStatus(resp, 200, out)
	m := v.occurrence("Monthly", 0)
	next := d0.AddDate(0, 0, 1).Format("2006-01-02")
	status, out := patch(m["id"].(string), map[string]any{"rev": m["rev"], "scope": "all", "start": map[string]any{"date": next, "time": "07:00", "tz": "UTC"}, "end": map[string]any{"date": next, "time": "08:00", "tz": "UTC"}})
	if status != 400 || errCode(out) != "invalid_input" || !strings.Contains(fmt.Sprint(out), "start") {
		t.Fatalf("monthly day move = %d %v", status, out)
	}
}

func TestEventPatchScopesWriteAndAnswer(t *testing.T) {
	v := newSeriesEnv(t)
	e := v.e
	occ := v.occurrence("Daily", 3)
	id, rev := occ["id"].(string), occ["rev"].(string)
	sid := occ["series"].(map[string]any)["id"]
	// This event: the id stays (CalDAV), the series' version moves.
	before := v.notices.Load()
	resp, out := e.do("PATCH", v.path(id), map[string]any{"rev": rev, "scope": "this", "summary": "Just this one"})
	e.mustStatus(resp, 200, out)
	ev, series := out["event"].(map[string]any), out["series"].(map[string]any)
	if ev["id"] != id || out["replaces"] != id || ev["summary"] != "Just this one" || series["id"] != sid || series["rev"] != ev["rev"] || series["rev"] == rev || out["refetch"] != nil {
		t.Fatalf("this = %v", out)
	}
	if v.notices.Load()-before != 1 {
		t.Fatalf("notifications = %d", v.notices.Load()-before)
	}
	// Other occurrences carry the new version at once.
	if other := v.occurrence("Daily", 5); other["rev"] != series["rev"] {
		t.Fatalf("another occurrence has rev %v, want %v", other["rev"], series["rev"])
	}
	// All events: other occurrences changed, so the device is told to fetch again.
	resp, out = e.do("PATCH", v.path(id), map[string]any{"rev": series["rev"], "scope": "all", "summary": "Every day"})
	e.mustStatus(resp, 200, out)
	if out["refetch"] != true || out["series"].(map[string]any)["id"] != sid || out["event"].(map[string]any)["summary"] != "Just this one" {
		t.Fatalf("all = %v", out)
	}
	if got := v.cal.Get("work", "daily.ics"); !strings.Contains(got, "SUMMARY:Every day") || !strings.Contains(got, "TRIGGER:-PT10M") || !strings.Contains(got, "SUMMARY:Daily (late)") {
		t.Fatalf("calendar object:\n%s", got)
	}
	// Following events: a new series with a new identity, and the old one ended.
	again := v.occurrence("Every day", 4)
	resp, out = e.do("PATCH", v.path(again["id"].(string)), map[string]any{"rev": again["rev"], "scope": "following", "summary": "From here", "reminders": []int{30, 1440}})
	e.mustStatus(resp, 200, out)
	if out["refetch"] != true || out["series"].(map[string]any)["id"] == sid || out["event"].(map[string]any)["summary"] != "From here" {
		t.Fatalf("following = %v", out)
	}
	members := v.cal.Members("work")
	fresh := 0
	for _, m := range members {
		if strings.HasPrefix(m, "ot-") {
			fresh++
			if got := v.cal.Get("work", m); !strings.Contains(got, "SUMMARY:From here") || !strings.Contains(got, "TRIGGER:-P1D") || !strings.Contains(got, "TRIGGER:-PT30M") {
				t.Fatalf("new series object:\n%s", got)
			}
		}
	}
	if fresh != 1 {
		t.Fatalf("members = %v", members)
	}
	// Audit: the scope and no title.
	list := e.events(audit.CalendarEventChangedName)
	scopes := map[any]int{}
	for _, a := range list {
		scopes[a.Details["event_scope"]]++
	}
	if scopes["this"] != 1 || scopes["all"] != 1 || scopes["following"] != 1 {
		t.Fatalf("audit scopes = %v", scopes)
	}
	if raw, _ := json.Marshal(list); strings.Contains(string(raw), "Every day") || strings.Contains(string(raw), "From here") || strings.Contains(string(raw), "FREQ") {
		t.Fatalf("the audit event holds a title or a rule: %s", raw)
	}
}

func TestEventDeleteScopes(t *testing.T) {
	v := newSeriesEnv(t)
	e := v.e
	occ := v.occurrence("Daily", 3)
	del := func(o map[string]any, scope string) (int, map[string]any) {
		path := v.path(o["id"].(string)) + "?rev=" + o["rev"].(string)
		if scope != "" {
			path += "&scope=" + scope
		}
		resp, out := e.do("DELETE", path, nil)
		return resp.StatusCode, out
	}
	if status, out := del(occ, "everything"); status != 400 || errCode(out) != "invalid_input" || !strings.Contains(fmt.Sprint(out), "scope") {
		t.Fatalf("unknown scope = %d %v", status, out)
	}
	if status, out := del(occ, ""); status != 400 || errCode(out) != "scope_required" {
		t.Fatalf("no scope = %d %v", status, out)
	}
	plain := v.events(e)["Plain"]
	if status, out := del(plain, "this"); status != 400 || errCode(out) != "invalid_input" {
		t.Fatalf("scope on a single event = %d %v", status, out)
	}
	if status, _ := del(occ, "this"); status != 204 {
		t.Fatalf("this = %d", status)
	}
	if got := v.cal.Get("work", "daily.ics"); strings.Count(got, "EXDATE") != 2 {
		t.Fatalf("an excluded date is added:\n%s", got)
	}
	d0, _ := time.Parse("2006-01-02", v.day)
	later := v.occurrence("Daily", 6)
	if status, _ := del(later, "following"); status != 204 {
		t.Fatalf("following = %d", status)
	}
	if got := v.cal.Get("work", "daily.ics"); !strings.Contains(got, "UNTIL="+d0.AddDate(0, 0, 5).Format("20060102")) {
		t.Fatalf("the series is ended before the day:\n%s", got)
	}
	first := v.occurrence("Daily", 0)
	if status, _ := del(first, "all"); status != 204 || v.cal.Get("work", "daily.ics") != "" {
		t.Fatal("all did not delete the object")
	}
	// Audit: scopes.
	scopes := map[any]int{}
	for _, a := range e.events(audit.CalendarEventChangedName) {
		scopes[a.Details["event_scope"]]++
	}
	if scopes["this"] != 1 || scopes["following"] != 1 || scopes["all"] != 1 {
		t.Fatalf("audit scopes = %v", scopes)
	}
}

func TestEventDetailOfASeriesAndCreateWithRepeat(t *testing.T) {
	v := newSeriesEnv(t)
	e := v.e
	occ := v.occurrence("Daily", 3)
	resp, out := e.do("GET", v.path(occ["id"].(string)), nil)
	e.mustStatus(resp, 200, out)
	series := out["series"].(map[string]any)
	if series["rrule"] != "FREQ=DAILY" || series["custom"] != false || series["exceptions"] != float64(2) || fmt.Sprint(series["repeat"]) != "map[days:[] ends:map[type:never] freq:daily interval:1]" {
		t.Fatalf("series = %v", series)
	}
	if fmt.Sprint(out["reminders"]) != "[10]" || out["remindersMax"] != float64(5) || out["remindersOther"] != float64(0) {
		t.Fatalf("reminders = %v", out)
	}
	custom := v.occurrence("Custom", 0)
	resp, out = e.do("GET", v.path(custom["id"].(string)), nil)
	e.mustStatus(resp, 200, out)
	if cs := out["series"].(map[string]any); cs["custom"] != true || cs["repeat"] != nil {
		t.Fatalf("custom detail = %v", cs)
	}
	plain := v.events(e)["Plain"]
	resp, out = e.do("GET", v.path(plain["id"].(string)), nil)
	e.mustStatus(resp, 200, out)
	if out["series"] != nil || fmt.Sprint(out["reminders"]) != "[]" {
		t.Fatalf("plain detail = %v", out)
	}
	// Create with a repeat and reminders: the first occurrence is served with its series.
	body := map[string]any{"clientId": "client-series-1", "calendarUrl": v.calURL, "summary": "Planning",
		"start": map[string]any{"date": v.day, "time": "09:00", "tz": "Europe/Berlin"}, "end": map[string]any{"date": v.day, "time": "10:00", "tz": "Europe/Berlin"},
		"repeat": map[string]any{"freq": "weekly", "days": []string{}}, "reminders": []int{10}}
	d0, _ := time.Parse("2006-01-02", v.day)
	wantDay := d0.Weekday()
	_ = wantDay
	resp, out = e.do("POST", "/api/v1/calendar-sync/connections/"+v.connID+"/events", body)
	e.mustStatus(resp, 201, out)
	sv := out["event"].(map[string]any)["series"].(map[string]any)
	if sv["first"] != true || sv["custom"] != false || sv["repeat"].(map[string]any)["freq"] != "weekly" {
		t.Fatalf("created series = %v", sv)
	}
	if got := v.cal.Get("work", "client-series-1.ics"); !strings.Contains(got, "RRULE:FREQ=WEEKLY") || !strings.Contains(got, "TRIGGER:-PT10M") {
		t.Fatalf("created object:\n%s", got)
	}
	bad := map[string]any{"clientId": "client-series-2", "calendarUrl": v.calURL, "summary": "Bad",
		"start": body["start"], "end": body["end"], "repeat": map[string]any{"freq": "yearly", "interval": 0, "ends": map[string]any{"type": "on", "date": "tomorrow"}}}
	if resp, out = e.do("POST", "/api/v1/calendar-sync/connections/"+v.connID+"/events", bad); resp.StatusCode != 400 || errCode(out) != "invalid_input" || !strings.Contains(fmt.Sprint(out), "repeat") {
		t.Fatalf("bad repeat = %d %v", resp.StatusCode, out)
	}
	bad["repeat"], bad["reminders"] = nil, []int{1, 2, 3, 4, 5, 6}
	if resp, out = e.do("POST", "/api/v1/calendar-sync/connections/"+v.connID+"/events", bad); resp.StatusCode != 400 || !strings.Contains(fmt.Sprint(out), "reminders") {
		t.Fatalf("bad reminders = %d %v", resp.StatusCode, out)
	}
}
