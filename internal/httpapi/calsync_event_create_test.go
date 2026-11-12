package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/OpenTodoApp/opentodo/internal/audit"
	"github.com/OpenTodoApp/opentodo/internal/calsync"
)

// Creating an event and reading one with its description over HTTP (OpenSpec
// change calendar-event-editor, task 3.2). The calendar server is the
// in-process fake from calsync_events_test.go.

func (v *eventsEnv) createPath() string {
	return "/api/v1/calendar-sync/connections/" + v.connID + "/events"
}

// newBody is a valid create body for tomorrow, 14:00 to 14:30 in Lisbon.
func (v *eventsEnv) newBody(clientID string) map[string]any {
	return map[string]any{"clientId": clientID, "calendarUrl": v.calURL, "summary": "Call Ana",
		"start":    map[string]any{"date": v.day, "time": "14:00", "tz": "Europe/Lisbon"},
		"end":      map[string]any{"date": v.day, "time": "14:30", "tz": "Europe/Lisbon"},
		"location": "Cafe Central", "description": "Bring the draft."}
}

func TestCalendarEventCreate(t *testing.T) {
	v := newEventsEnv(t)
	e := v.e
	v.edits(true)
	before := v.notices.Load()
	resp, out := e.do("POST", v.createPath(), v.newBody("client-0001"))
	e.mustStatus(resp, 201, out)
	ev := out["event"].(map[string]any)
	if out["clientId"] != "client-0001" || ev["summary"] != "Call Ana" || ev["location"] != "Cafe Central" || ev["free"] != false ||
		ev["rev"] == nil || len(ev["rev"].(string)) != 16 || accessOf(ev)["update"] != true || accessOf(ev)["delete"] != true {
		t.Fatalf("answer = %v", out)
	}
	start := ev["start"].(map[string]any)
	if start["date"] != v.day || start["time"] != "14:00" || start["tz"] != "Europe/Lisbon" {
		t.Fatalf("start = %v", start)
	}
	if got := v.notices.Load() - before; got != 1 {
		t.Fatalf("notifications = %d, want 1", got)
	}
	obj := v.cal.Get("work", "client-0001.ics")
	for _, want := range []string{"UID:client-0001", "SUMMARY:Call Ana", "LOCATION:Cafe Central", "DESCRIPTION:Bring the draft.", "TRANSP:OPAQUE", "TZID=Europe/Lisbon"} {
		if !strings.Contains(obj, want) {
			t.Fatalf("object lacks %q:\n%s", want, obj)
		}
	}
	// It is served with the other events, with the same id and rev.
	if got := v.events(e)["Call Ana"]; got == nil || got["id"] != ev["id"] || got["rev"] != ev["rev"] {
		t.Fatalf("served = %v", got)
	}
	// Audited as created, without the title.
	list := e.events(audit.CalendarEventChangedName)
	if len(list) != 1 || list[0].Details["event_action"] != "created" || list[0].Details["connection_id"] != v.connID {
		t.Fatalf("audit = %+v", list)
	}
	if raw, _ := json.Marshal(list); strings.Contains(string(raw), "Call Ana") {
		t.Fatalf("the audit event holds a title: %s", raw)
	}
	// The same client id again: 200, the same event, nothing written, nothing audited.
	notices, puts := v.notices.Load(), v.cal.Count("PUT")
	resp, out = e.do("POST", v.createPath(), v.newBody("client-0001"))
	e.mustStatus(resp, 200, out)
	if again := out["event"].(map[string]any); again["id"] != ev["id"] || again["rev"] != ev["rev"] || out["clientId"] != "client-0001" {
		t.Fatalf("replay = %v", out)
	}
	if v.notices.Load() != notices || v.cal.Count("PUT") != puts || len(e.events(audit.CalendarEventChangedName)) != 1 {
		t.Fatal("a replay wrote, woke devices or was audited")
	}
	// The event is deleted in the calendar app and the cycle forgets it: the same id is not made again.
	resp, _ = e.do("DELETE", v.path(ev["id"].(string))+"?rev="+ev["rev"].(string), nil)
	if resp.StatusCode != 204 {
		t.Fatalf("delete = %d", resp.StatusCode)
	}
	resp, out = e.do("POST", v.createPath(), v.newBody("client-0001"))
	if resp.StatusCode != 404 || errCode(out) != "event_gone" || v.cal.Get("work", "client-0001.ics") != "" {
		t.Fatalf("replay after delete = %d %v", resp.StatusCode, out)
	}
}

func TestCalendarEventCreateAllDayFloatingAndFree(t *testing.T) {
	v := newEventsEnv(t)
	e := v.e
	v.edits(true)
	resp, out := e.do("POST", v.createPath(), map[string]any{"clientId": "client-allday", "calendarUrl": v.calURL, "summary": "Holiday", "free": true,
		"start": map[string]any{"date": v.day}, "end": map[string]any{"date": v.day}, "allDay": true})
	if resp.StatusCode != 400 {
		t.Fatalf("all-day with an empty span = %d %v", resp.StatusCode, out)
	}
	next := "2099-01-02"
	resp, out = e.do("POST", v.createPath(), map[string]any{"clientId": "client-allday", "calendarUrl": v.calURL, "summary": "Holiday", "free": true,
		"start": map[string]any{"date": "2099-01-01"}, "end": map[string]any{"date": next}})
	e.mustStatus(resp, 201, out)
	ev := out["event"].(map[string]any)
	if ev["allDay"] != true || ev["free"] != true {
		t.Fatalf("event = %v", ev)
	}
	if obj := v.cal.Get("work", "client-allday.ics"); !strings.Contains(obj, "DTSTART;VALUE=DATE:20990101") || !strings.Contains(obj, "TRANSP:TRANSPARENT") {
		t.Fatalf("object:\n%s", obj)
	}
	resp, out = e.do("POST", v.createPath(), map[string]any{"clientId": "client-floating", "calendarUrl": v.calURL, "summary": "Floating",
		"start": map[string]any{"date": v.day, "time": "09:00"}, "end": map[string]any{"date": v.day, "time": "10:00"}})
	e.mustStatus(resp, 201, out)
	if st := out["event"].(map[string]any)["start"].(map[string]any); st["tz"] != nil {
		t.Fatalf("floating start = %v", st)
	}
}

func TestCalendarEventCreateRefusals(t *testing.T) {
	v := newEventsEnv(t)
	e := v.e
	calls := func() int { return v.cal.Count("GET") + v.cal.Count("PUT") + v.cal.Count("DELETE") }
	base, notices := calls(), v.notices.Load()
	expect := func(name string, c *env, status int, code string, body map[string]any) map[string]any {
		t.Helper()
		resp, out := c.do("POST", v.createPath(), body)
		if resp.StatusCode != status || (code != "" && errCode(out) != code) {
			t.Fatalf("%s = %d %v, want %d %s", name, resp.StatusCode, out, status, code)
		}
		return out
	}
	good := func(id string) map[string]any { return v.newBody(id) }
	expect("edits off", e, 403, "event_edits_off", good("client-0001"))
	v.edits(true)
	m, _ := e.member("m@example.com", "198.51.100.45")
	expect("another user", m, 404, "not_found", good("client-0002"))
	expect("anonymous", newClientAt(e, "10.9.9.14"), 401, "unauthorized", good("client-0003"))
	for _, scope := range []string{"read", "agent"} {
		c := e.bearerClient()
		resp, out := c.do("POST", v.createPath(), good("client-0004"), bearer(e.token(scope))...)
		if resp.StatusCode != 403 {
			t.Fatalf("%s token = %d %v", scope, resp.StatusCode, out)
		}
	}
	// A calendar of the connection that is not shown names the field.
	other := good("client-0005")
	other["calendarUrl"] = v.cal.CalendarURL("not-shown")
	if out := expect("calendar not shown", e, 400, "invalid_input", other); !strings.Contains(out["error"].(map[string]any)["message"].(string), "calendarUrl") {
		t.Fatalf("message does not name the field: %v", out)
	}
	// Body rules.
	bad := map[string]func(b map[string]any){
		"no client id":    func(b map[string]any) { delete(b, "clientId") },
		"short client id": func(b map[string]any) { b["clientId"] = "abc" },
		"long client id":  func(b map[string]any) { b["clientId"] = strings.Repeat("a", 65) },
		"client id chars": func(b map[string]any) { b["clientId"] = "client 0001/../x" },
		"no calendar":     func(b map[string]any) { delete(b, "calendarUrl") },
		"no summary":      func(b map[string]any) { b["summary"] = "  " },
		"long summary":    func(b map[string]any) { b["summary"] = strings.Repeat("x", 501) },
		"no start":        func(b map[string]any) { delete(b, "start") },
		"no end":          func(b map[string]any) { delete(b, "end") },
		"end before start": func(b map[string]any) {
			b["end"] = map[string]any{"date": v.day, "time": "13:00", "tz": "Europe/Lisbon"}
		},
		"bad zone":            func(b map[string]any) { b["start"] = map[string]any{"date": v.day, "time": "14:00", "tz": "Mars/Base"} },
		"long location":       func(b map[string]any) { b["location"] = strings.Repeat("x", 501) },
		"long description":    func(b map[string]any) { b["description"] = strings.Repeat("x", 10001) },
		"nul description":     func(b map[string]any) { b["description"] = "a\x00b" },
		"free not a bool":     func(b map[string]any) { b["free"] = "yes" },
		"unknown member kept": func(b map[string]any) { b["clientId"] = 5 },
	}
	for name, mutate := range bad {
		b := good("client-0006")
		mutate(b)
		expect(name, e, 400, "invalid_input", b)
	}
	// No capability to create.
	if err := v.srv.calsync.Store.SetCaps(t.Context(), v.connID, v.calURL, calsync.Caps{Update: true, Delete: true}); err != nil {
		t.Fatal(err)
	}
	expect("no create capability", e, 403, "calendar_read_only", good("client-0007"))
	if calls() != base || v.notices.Load() != notices || len(v.cal.Members("work")) != 3 {
		t.Fatalf("a refused request reached the calendar (%d requests) or woke devices", calls()-base)
	}
	if err := v.srv.calsync.Store.SetCaps(t.Context(), v.connID, v.calURL, calsync.Caps{Create: true, Update: true, Delete: true}); err != nil {
		t.Fatal(err)
	}
	// Calendar sync off: the route is not there.
	resp, out := e.do("PUT", "/api/v1/settings/calendar-sync", map[string]any{"enabled": false})
	e.mustStatus(resp, 200, out)
	expect("sync off", e, 404, "calendar_sync_disabled", good("client-0008"))
}

func TestCalendarEventCreateUnreachableAndRateLimit(t *testing.T) {
	v := newEventsEnv(t)
	e := v.e
	v.edits(true)
	v.cal.Close()
	resp, out := e.do("POST", v.createPath(), v.newBody("client-0001"))
	if resp.StatusCode != 502 || errCode(out) != "calendar_unreachable" {
		t.Fatalf("unreachable = %d %v", resp.StatusCode, out)
	}
	// The limit is shared with PATCH and DELETE: 600 an hour per account.
	plain := v.events(e)["Plain"]
	id := plain["id"].(string)
	for i := 0; i < 199; i++ { // the create above was the first
		resp, _ = e.do("POST", v.createPath(), v.newBody("client-0002"))
		if resp.StatusCode == 429 {
			t.Fatalf("limited after %d requests", i+2)
		}
		resp, _ = e.do("PATCH", v.path(id), v.move(id, "0000000000000000"))
		resp, _ = e.do("DELETE", v.path(id)+"?rev=0000000000000000", nil)
	}
	// 1 + 199*3 = 598 so far.
	for i := 0; i < 2; i++ {
		if resp, _ = e.do("POST", v.createPath(), v.newBody("client-0003")); resp.StatusCode == 429 {
			t.Fatalf("limited at request %d", 599+i)
		}
	}
	resp, out = e.do("POST", v.createPath(), v.newBody("client-0004"))
	if resp.StatusCode != 429 || errCode(out) != "rate_limited" {
		t.Fatalf("the 601st = %d %v", resp.StatusCode, out)
	}
	if resp, _ = e.do("PATCH", v.path(id), v.move(id, "0000000000000000")); resp.StatusCode != 429 {
		t.Fatalf("patch over the shared limit = %d", resp.StatusCode)
	}
}

func TestCalendarEventGet(t *testing.T) {
	v := newEventsEnv(t)
	e := v.e
	getPath := func(id string) string { return v.path(id) }
	plain := v.events(e)["Plain"]
	id := plain["id"].(string)
	// Off by default.
	if resp, out := e.do("GET", getPath(id), nil); resp.StatusCode != 403 || errCode(out) != "event_edits_off" {
		t.Fatalf("edits off = %d %v", resp.StatusCode, out)
	}
	v.edits(true)
	resp, out := e.do("GET", getPath(id), nil)
	e.mustStatus(resp, 200, out)
	if out["event"].(map[string]any)["id"] != id || out["description"] != "" || out["descriptionEditable"] != true || out["replaces"] != nil {
		t.Fatalf("detail = %v", out)
	}
	// A locked event can be read (Duplicate and Convert need its description).
	guests := v.events(e)["Guests"]
	resp, out = e.do("GET", getPath(guests["id"].(string)), nil)
	e.mustStatus(resp, 200, out)
	if accessOf(out["event"].(map[string]any))["update"] != false {
		t.Fatalf("locked detail = %v", out)
	}
	// Changed in the calendar app: the cache takes it and the answer says what it replaces.
	v.cal.Put("work", "plain.ics", strings.Replace(strings.Replace(v.cal.Get("work", "plain.ics"), "SUMMARY:Plain", "SUMMARY:Plain v2", 1),
		"END:VEVENT", "DESCRIPTION:Fresh notes\r\nEND:VEVENT", 1))
	notices := v.notices.Load()
	resp, out = e.do("GET", getPath(id), nil)
	e.mustStatus(resp, 200, out)
	ev := out["event"].(map[string]any)
	if out["replaces"] != id || ev["summary"] != "Plain v2" || out["description"] != "Fresh notes" || v.notices.Load()-notices != 1 {
		t.Fatalf("after a remote change = %v", out)
	}
	if v.events(e)["Plain v2"] == nil || v.events(e)["Plain"] != nil {
		t.Fatalf("served = %v", v.events(e))
	}
	// Read and agent tokens may read it.
	for _, scope := range []string{"read", "agent"} {
		c := e.bearerClient()
		resp, out := c.do("GET", getPath(ev["id"].(string)), nil, bearer(e.token(scope))...)
		if resp.StatusCode != 200 {
			t.Fatalf("%s token = %d %v", scope, resp.StatusCode, out)
		}
	}
	// Not found, another user's, anonymous, gone.
	if resp, out := e.do("GET", getPath("ffffffffffffffffffffffff"), nil); resp.StatusCode != 404 || errCode(out) != "not_found" {
		t.Fatalf("unknown = %d %v", resp.StatusCode, out)
	}
	m, _ := e.member("m@example.com", "198.51.100.46")
	if resp, out := m.do("GET", getPath(ev["id"].(string)), nil); resp.StatusCode != 404 {
		t.Fatalf("another user = %d %v", resp.StatusCode, out)
	}
	if resp, _ := newClientAt(e, "10.9.9.15").do("GET", getPath(id), nil); resp.StatusCode != 401 {
		t.Fatalf("anonymous = %d", resp.StatusCode)
	}
	v.cal.Delete("work", "plain.ics")
	if resp, out := e.do("GET", getPath(ev["id"].(string)), nil); resp.StatusCode != 404 || errCode(out) != "event_gone" {
		t.Fatalf("deleted = %d %v", resp.StatusCode, out)
	}
}

func TestCalendarEventRichDescriptionIsUnprocessable(t *testing.T) {
	v := newEventsEnv(t)
	rec := httptest.NewRecorder()
	v.srv.eventFailure(rec, &calsync.EventError{Code: calsync.EventNotEditable, Reason: calsync.LockRichDescription})
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "event_not_editable") || !strings.Contains(rec.Body.String(), "description_rich") {
		t.Fatalf("description_rich = %d %s", rec.Code, rec.Body.String())
	}
}

func TestCalendarEventCreateRoutesDeniedOnDemo(t *testing.T) {
	d := newDemoServer(t)
	e, _ := d.visitor()
	resp, out := e.do("POST", "/api/v1/calendar-sync/connections/c1/events", map[string]any{"clientId": "client-0001"})
	if resp.StatusCode != 403 || errCode(out) != "demo_restricted" {
		t.Fatalf("post = %d %v", resp.StatusCode, out)
	}
	resp, out = e.do("GET", "/api/v1/calendar-sync/connections/c1/events/e1", nil)
	if resp.StatusCode != 403 || errCode(out) != "demo_restricted" {
		t.Fatalf("get = %d %v", resp.StatusCode, out)
	}
}
