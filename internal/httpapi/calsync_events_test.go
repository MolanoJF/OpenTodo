package httpapi

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/OpenTodoApp/opentodo/internal/audit"
	"github.com/OpenTodoApp/opentodo/internal/calsync"
	"github.com/OpenTodoApp/opentodo/internal/calsync/calsynctest"
)

// Editing the events of a connected calendar over HTTP (OpenSpec change
// calendar-event-edits, tasks 3.1, 3.2 and 3.4). The calendar server is an
// in-process fake on loopback.

type eventsEnv struct {
	e       *env
	srv     *Server
	cal     *calsynctest.Server
	connID  string
	calURL  string
	day     string
	notices *atomic.Int32
}

// newEventsEnv connects a CalDAV account whose work calendar holds a plain
// event, one with a guest and a weekly one, all tomorrow or later.
func newEventsEnv(t *testing.T) *eventsEnv {
	t.Helper()
	e, srv := calsyncEnv(t)
	calsyncOn(t, e)
	allowPrivateTargets(t, e)
	cal := calsynctest.New()
	t.Cleanup(cal.Close)
	resp, out := e.do("POST", "/api/v1/calendar-sync/connections", map[string]any{"server_url": cal.URL, "username": "me", "password": "app-pass"})
	e.mustStatus(resp, 201, out)
	c := connOf(out)
	id := c["id"].(string)
	calURL := c["calendars"].([]any)[0].(map[string]any)["url"].(string)
	resp, out = e.do("PATCH", "/api/v1/calendar-sync/connections/"+id, map[string]any{"target": calURL})
	e.mustStatus(resp, 200, out)
	day := time.Now().UTC().AddDate(0, 0, 1).Format("2006-01-02")
	stamp := strings.ReplaceAll(day, "-", "")
	ics := func(uid, summary, extra string) string {
		return "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VEVENT\r\nUID:" + uid + "\r\nSUMMARY:" + summary + "\r\nDTSTART:" + stamp + "T120000Z\r\nDTEND:" + stamp + "T130000Z\r\n" +
			extra + "END:VEVENT\r\nEND:VCALENDAR\r\n"
	}
	cal.Put("work", "plain.ics", ics("plain", "Plain", ""))
	cal.Put("work", "guests.ics", ics("guests", "Guests", "ATTENDEE:mailto:a@example.org\r\n"))
	cal.Put("work", "weekly.ics", ics("weekly", "Weekly", "RRULE:FREQ=WEEKLY;COUNT=2\r\n"))
	resp, out = e.do("POST", "/api/v1/calendar-sync/connections/"+id+"/sync", nil)
	e.mustStatus(resp, 200, out)
	ev := &eventsEnv{e: e, srv: srv, cal: cal, connID: id, calURL: calURL, day: day, notices: &atomic.Int32{}}
	orig := srv.calsync.Notify
	srv.calsync.Notify = func(uid string) {
		ev.notices.Add(1)
		if orig != nil {
			orig(uid)
		}
	}
	return ev
}

// events lists the served events of the next two weeks by summary.
func (v *eventsEnv) events(c *env) map[string]map[string]any {
	v.e.t.Helper()
	to := time.Now().UTC().AddDate(0, 0, 15).Format("2006-01-02")
	resp, out := c.do("GET", "/api/v1/calendar-feeds/events?from="+v.day+"&to="+to, nil)
	c.mustStatus(resp, 200, out)
	got := map[string]map[string]any{}
	for _, x := range out["events"].([]any) {
		m := x.(map[string]any)
		if _, dup := got[m["summary"].(string)]; !dup {
			got[m["summary"].(string)] = m
		}
	}
	return got
}

func (v *eventsEnv) path(id string) string {
	return "/api/v1/calendar-sync/connections/" + v.connID + "/events/" + id
}

func (v *eventsEnv) edits(on bool) {
	v.e.t.Helper()
	resp, out := v.e.do("PATCH", "/api/v1/calendar-sync/connections/"+v.connID, map[string]any{"edit_events": on})
	v.e.mustStatus(resp, 200, out)
}

func (v *eventsEnv) move(id, rev string) map[string]any {
	return map[string]any{"rev": rev, "summary": "Moved", "start": map[string]any{"date": v.day, "time": "14:00", "tz": "Europe/Berlin"},
		"end": map[string]any{"date": v.day, "time": "14:30", "tz": "Europe/Berlin"}}
}

func accessOf(ev map[string]any) map[string]any {
	a, _ := ev["access"].(map[string]any)
	return a
}

func TestCalendarConnectionEditEventsOption(t *testing.T) {
	v := newEventsEnv(t)
	e := v.e
	resp, out := e.do("GET", "/api/v1/calendar-sync/connections", nil)
	e.mustStatus(resp, 200, out)
	c := out["connections"].([]any)[0].(map[string]any)
	if c["edit_events"] != false {
		t.Fatalf("default edit_events = %v", c["edit_events"])
	}
	cal := c["calendars"].([]any)[0].(map[string]any)
	caps, _ := cal["caps"].(map[string]any)
	if caps["create"] != true || caps["update"] != true || caps["delete"] != true {
		t.Fatalf("caps = %v", cal)
	}
	resp, out = e.do("PATCH", "/api/v1/calendar-sync/connections/"+v.connID, map[string]any{"edit_events": true})
	e.mustStatus(resp, 200, out)
	if connOf(out)["edit_events"] != true {
		t.Fatalf("on = %v", connOf(out))
	}
	resp, out = e.do("PATCH", "/api/v1/calendar-sync/connections/"+v.connID, map[string]any{"edit_events": false})
	e.mustStatus(resp, 200, out)
	if connOf(out)["edit_events"] != false {
		t.Fatalf("off = %v", connOf(out))
	}
	var n int
	e.db.QueryRow(`SELECT COUNT(*) FROM audit_events WHERE name = 'calendar_connection.updated' AND details LIKE '%edit_events%'`).Scan(&n)
	if n != 2 {
		t.Fatalf("%d audit events naming edit_events, want 2", n)
	}
	if resp, out := e.do("PATCH", "/api/v1/calendar-sync/connections/"+v.connID, map[string]any{"edit_events": "yes"}); resp.StatusCode != 400 {
		t.Fatalf("wrong type = %d %v", resp.StatusCode, out)
	}
	m, _ := e.member("m@example.com", "198.51.100.40")
	if resp, _ := m.do("PATCH", "/api/v1/calendar-sync/connections/"+v.connID, map[string]any{"edit_events": true}); resp.StatusCode != 404 {
		t.Fatalf("another user = %d", resp.StatusCode)
	}
	anon := newClientAt(e, "10.9.9.12")
	if resp, out := anon.do("PATCH", "/api/v1/calendar-sync/connections/"+v.connID, map[string]any{"edit_events": true}); resp.StatusCode != 401 {
		t.Fatalf("anonymous = %d %v", resp.StatusCode, out)
	}
}

func TestCalendarEventsServeRevAndAccess(t *testing.T) {
	v := newEventsEnv(t)
	e := v.e
	evs := v.events(e)
	if evs["Plain"]["rev"] == "" || evs["Plain"]["rev"] == nil || len(evs["Plain"]["rev"].(string)) != 16 {
		t.Fatalf("rev = %v", evs["Plain"])
	}
	// Off by default.
	if a := accessOf(evs["Plain"]); a["update"] != false || a["delete"] != false || a["reason"] != "edits_off" {
		t.Fatalf("access with edits off = %v", a)
	}
	v.edits(true)
	evs = v.events(e)
	if a := accessOf(evs["Plain"]); a["update"] != true || a["delete"] != true || a["reason"] != nil {
		t.Fatalf("plain = %v", a)
	}
	if a := accessOf(evs["Guests"]); a["update"] != false || a["reason"] != "has_guests" {
		t.Fatalf("guests = %v", a)
	}
	if a := accessOf(evs["Weekly"]); a["update"] != true || a["delete"] != true || a["reason"] != nil {
		t.Fatalf("weekly = %v", a)
	}
	if sv, _ := evs["Weekly"]["series"].(map[string]any); sv == nil || sv["id"] == "" || sv["custom"] != false || sv["rev"] != evs["Weekly"]["rev"] || evs["Plain"]["series"] != nil {
		t.Fatalf("series of the weekly event = %v, of the plain one = %v", evs["Weekly"]["series"], evs["Plain"]["series"])
	}
	// The subscription entry names its connection, calendar and caps.
	resp, out := e.do("GET", "/api/v1/calendar-feeds/events?from="+v.day+"&to="+v.day, nil)
	e.mustStatus(resp, 200, out)
	sub := out["subscriptions"].([]any)[0].(map[string]any)
	caps, _ := sub["caps"].(map[string]any)
	if sub["connectionId"] != v.connID || sub["calendarUrl"] != v.calURL || sub["editEvents"] != true || caps["update"] != true {
		t.Fatalf("subscription = %v", sub)
	}
	// A calendar that can not be written.
	if err := v.srv.calsync.Store.SetCaps(t.Context(), v.connID, v.calURL, calsync.Caps{}); err != nil {
		t.Fatal(err)
	}
	if a := accessOf(v.events(e)["Plain"]); a["update"] != false || a["reason"] != "calendar_read_only" {
		t.Fatalf("read only = %v", a)
	}
	// Remote refs missing (an older database before its first re-read).
	if _, err := e.db.Exec(`DELETE FROM calendar_event_remote`); err != nil {
		t.Fatal(err)
	}
	if err := v.srv.calsync.Store.SetCaps(t.Context(), v.connID, v.calURL, calsync.Caps{Create: true, Update: true, Delete: true}); err != nil {
		t.Fatal(err)
	}
	p := v.events(e)["Plain"]
	if a := accessOf(p); a["update"] != false || a["reason"] != "refresh_pending" || p["rev"] != nil {
		t.Fatalf("pending = %v rev %v", a, p["rev"])
	}
}

func TestSubscribedFeedEventsHaveNoRevOrAccess(t *testing.T) {
	v := newEventsEnv(t)
	e := v.e
	resp, out := e.do("PUT", "/api/v1/settings/calendar-feeds", map[string]any{"enabled": true})
	e.mustStatus(resp, 200, out)
	uid := userIDOf(t, e)
	if _, err := e.db.Exec(`INSERT INTO calendar_subscriptions(id,user_id,url,name,color,enabled,interval_seconds,next_fetch_at,created_at,updated_at)
		VALUES('feed1',?,'https://cal.example/a.ics','Feed','',1,3600,0,1,1)`, uid); err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.Exec(`INSERT INTO calendar_events(subscription_id,uid,instance,summary,location,start_date,start_time,start_tz,end_date,end_time,end_tz,all_day,free,start_ms,end_ms)
		VALUES('feed1','f','i','Subscribed','',?,'09:00','UTC',?,'10:00','UTC',0,0,?,?)`, v.day, v.day, time.Now().AddDate(0, 0, 1).UnixMilli(), time.Now().AddDate(0, 0, 1).Add(time.Hour).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	ev := v.events(e)["Subscribed"]
	if ev == nil || ev["rev"] != nil || ev["access"] != nil {
		t.Fatalf("feed event = %v", ev)
	}
	if v.events(e)["Plain"]["access"] == nil {
		t.Fatal("a connected event lost its access")
	}
	// With calendar sync off the managed events are not served at all.
	resp, out = e.do("PUT", "/api/v1/settings/calendar-sync", map[string]any{"enabled": false})
	e.mustStatus(resp, 200, out)
	evs := v.events(e)
	if evs["Plain"] != nil || evs["Subscribed"] == nil || evs["Subscribed"]["access"] != nil || evs["Subscribed"]["series"] != nil {
		t.Fatalf("with calendar sync off: %v", evs)
	}
}

func TestCalendarEventPatchSucceeds(t *testing.T) {
	v := newEventsEnv(t)
	e := v.e
	v.edits(true)
	old := v.events(e)["Plain"]
	before := v.notices.Load()
	resp, out := e.do("PATCH", v.path(old["id"].(string)), v.move(old["id"].(string), old["rev"].(string)))
	e.mustStatus(resp, 200, out)
	ev := out["event"].(map[string]any)
	if out["replaces"] != old["id"] || ev["id"] == old["id"] || ev["summary"] != "Moved" || ev["rev"] == old["rev"] || accessOf(ev)["update"] != true {
		t.Fatalf("answer = %v", out)
	}
	start := ev["start"].(map[string]any)
	if start["date"] != v.day || start["time"] != "14:00" || start["tz"] != "Europe/Berlin" {
		t.Fatalf("start = %v", start)
	}
	if got := v.notices.Load() - before; got != 1 {
		t.Fatalf("notifications = %d, want 1", got)
	}
	if got := v.cal.Get("work", "plain.ics"); !strings.Contains(got, "SUMMARY:Moved") || !strings.Contains(got, "TZID=Europe/Berlin") {
		t.Fatalf("calendar object:\n%s", got)
	}
	evs := v.events(e)
	if evs["Plain"] != nil || evs["Moved"] == nil || evs["Moved"]["id"] != ev["id"] {
		t.Fatalf("served after the move = %v", evs)
	}
	// Audited without the title.
	list := e.events(audit.CalendarEventChangedName)
	if len(list) != 1 || list[0].Details["connection_id"] != v.connID || list[0].Details["provider"] != "caldav" || list[0].Details["event_action"] != "updated" {
		t.Fatalf("audit = %+v", list)
	}
	if raw, _ := json.Marshal(list); strings.Contains(string(raw), "Moved") || strings.Contains(string(raw), "Plain") {
		t.Fatalf("the audit event holds a title: %s", raw)
	}
	// Delete it.
	before = v.notices.Load()
	resp, _ = e.do("DELETE", v.path(ev["id"].(string))+"?rev="+ev["rev"].(string), nil)
	if resp.StatusCode != 204 {
		t.Fatalf("delete = %d", resp.StatusCode)
	}
	if v.cal.Get("work", "plain.ics") != "" || v.notices.Load()-before != 1 || v.events(e)["Moved"] != nil {
		t.Fatal("the event survived the delete, or devices were not woken once")
	}
	if list := e.events(audit.CalendarEventChangedName); len(list) != 2 || list[0].Details["event_action"] != "deleted" && list[1].Details["event_action"] != "deleted" {
		t.Fatalf("audit after delete = %+v", list)
	}
}

func TestCalendarEventRefusals(t *testing.T) {
	v := newEventsEnv(t)
	e := v.e
	plain := v.events(e)["Plain"]
	id, rev := plain["id"].(string), plain["rev"].(string)
	calls := func() int { return v.cal.Count("GET") + v.cal.Count("PUT") + v.cal.Count("DELETE") }
	baseline := calls()
	expect := func(name string, c *env, method, path string, body any, status int, code string) map[string]any {
		t.Helper()
		var headers []string
		resp, out := c.do(method, path, body, headers...)
		if resp.StatusCode != status || (code != "" && errCode(out) != code) {
			t.Fatalf("%s: %s %s = %d %v, want %d %s", name, method, path, resp.StatusCode, out, status, code)
		}
		return out
	}
	// Off by default.
	expect("edits off", e, "PATCH", v.path(id), v.move(id, rev), 403, "event_edits_off")
	expect("edits off delete", e, "DELETE", v.path(id)+"?rev="+rev, nil, 403, "event_edits_off")
	v.edits(true)
	// Not the caller's.
	m, _ := e.member("m@example.com", "198.51.100.41")
	expect("another user", m, "PATCH", v.path(id), v.move(id, rev), 404, "not_found")
	expect("unknown event", e, "PATCH", v.path("ffffffffffffffffffffffff"), v.move(id, rev), 404, "not_found")
	expect("unknown connection", e, "PATCH", "/api/v1/calendar-sync/connections/nope/events/"+id, v.move(id, rev), 404, "not_found")
	// Credentials and scopes.
	expect("anonymous", newClientAt(e, "10.9.9.13"), "PATCH", v.path(id), v.move(id, rev), 401, "unauthorized")
	for _, scope := range []string{"read", "agent"} {
		c := e.bearerClient()
		resp, out := c.do("PATCH", v.path(id), v.move(id, rev), bearer(e.token(scope))...)
		if resp.StatusCode != 403 {
			t.Fatalf("%s token = %d %v", scope, resp.StatusCode, out)
		}
	}
	// Locked.
	guests := v.events(e)["Guests"]
	out := expect("guests", e, "PATCH", v.path(guests["id"].(string)), v.move("", guests["rev"].(string)), 422, "event_not_editable")
	if !strings.Contains(fmt.Sprint(out), "has_guests") {
		t.Fatalf("the message does not name the reason: %v", out)
	}
	weekly := v.events(e)["Weekly"]
	expect("weekly without scope", e, "DELETE", v.path(weekly["id"].(string))+"?rev="+weekly["rev"].(string), nil, 400, "scope_required")
	// Body rules.
	badBodies := map[string]map[string]any{
		"no rev":           {"summary": "x"},
		"empty summary":    {"rev": rev, "summary": "  "},
		"long summary":     {"rev": rev, "summary": strings.Repeat("x", 501)},
		"nothing to do":    {"rev": rev},
		"start alone":      {"rev": rev, "start": map[string]any{"date": v.day, "time": "10:00"}},
		"allDay alone":     {"rev": rev, "allDay": true},
		"bad zone":         {"rev": rev, "start": map[string]any{"date": v.day, "time": "10:00", "tz": "Mars/Base"}, "end": map[string]any{"date": v.day, "time": "11:00", "tz": "Mars/Base"}},
		"end before start": {"rev": rev, "start": map[string]any{"date": v.day, "time": "11:00"}, "end": map[string]any{"date": v.day, "time": "10:00"}},
		"end equals start": {"rev": rev, "allDay": true, "start": map[string]any{"date": v.day}, "end": map[string]any{"date": v.day}},
		"bad date":         {"rev": rev, "start": map[string]any{"date": "tomorrow", "time": "10:00"}, "end": map[string]any{"date": v.day, "time": "11:00"}},
		"timed no time":    {"rev": rev, "allDay": false, "start": map[string]any{"date": v.day}, "end": map[string]any{"date": v.day}},
	}
	for name, body := range badBodies {
		expect(name, e, "PATCH", v.path(id), body, 400, "invalid_input")
	}
	expect("delete without rev", e, "DELETE", v.path(id), nil, 400, "invalid_input")
	if calls() != baseline {
		t.Fatalf("a refused request reached the calendar (%d requests)", calls()-baseline)
	}
	// A stale rev is a conflict and nothing is sent to the calendar.
	notices := v.notices.Load()
	expect("stale rev", e, "PATCH", v.path(id), v.move(id, "0000000000000000"), 409, "event_changed")
	expect("stale rev delete", e, "DELETE", v.path(id)+"?rev=0000000000000000", nil, 409, "event_changed")
	if calls() != baseline || v.notices.Load() != notices {
		t.Fatal("a stale rev reached the calendar or woke devices")
	}
	// Read only.
	if err := v.srv.calsync.Store.SetCaps(t.Context(), v.connID, v.calURL, calsync.Caps{}); err != nil {
		t.Fatal(err)
	}
	expect("calendar read only", e, "PATCH", v.path(id), v.move(id, rev), 403, "calendar_read_only")
	if err := v.srv.calsync.Store.SetCaps(t.Context(), v.connID, v.calURL, calsync.Caps{Create: true, Update: true, Delete: true}); err != nil {
		t.Fatal(err)
	}
	// Changed in the calendar app after the last read: the remote refuses, 409.
	v.cal.Put("work", "plain.ics", strings.Replace(v.cal.Get("work", "plain.ics"), "SUMMARY:Plain", "SUMMARY:Plain v2", 1))
	expect("remote changed", e, "PATCH", v.path(id), v.move(id, rev), 409, "event_changed")
	// Deleted there: gone, and the row leaves the served events.
	v.cal.Delete("work", "plain.ics")
	expect("gone", e, "PATCH", v.path(id), v.move(id, rev), 404, "event_gone")
	if v.events(e)["Plain"] != nil {
		t.Fatal("a gone event is still served")
	}
	// Calendar sync off: the routes are not there.
	resp, out2 := e.do("PUT", "/api/v1/settings/calendar-sync", map[string]any{"enabled": false})
	e.mustStatus(resp, 200, out2)
	expect("sync off", e, "PATCH", v.path(id), v.move(id, rev), 404, "calendar_sync_disabled")
}

func TestCalendarEventUnreachableAndRateLimit(t *testing.T) {
	v := newEventsEnv(t)
	e := v.e
	v.edits(true)
	plain := v.events(e)["Plain"]
	id, rev := plain["id"].(string), plain["rev"].(string)
	v.cal.Close()
	resp, out := e.do("PATCH", v.path(id), v.move(id, rev))
	if resp.StatusCode != 502 || errCode(out) != "calendar_unreachable" {
		t.Fatalf("unreachable = %d %v", resp.StatusCode, out)
	}
	// 600 changes an hour per account: refused ones count too.
	for i := 0; i < 599; i++ { // the unreachable one above was the first
		resp, _ = e.do("PATCH", v.path(id), v.move(id, "0000000000000000"))
		if resp.StatusCode == 429 {
			t.Fatalf("limited after %d requests", i+2)
		}
	}
	resp, out = e.do("PATCH", v.path(id), v.move(id, "0000000000000000"))
	if resp.StatusCode != 429 || errCode(out) != "rate_limited" {
		t.Fatalf("the 601st = %d %v", resp.StatusCode, out)
	}
	resp, out = e.do("DELETE", v.path(id)+"?rev=0000000000000000", nil)
	if resp.StatusCode != 429 {
		t.Fatalf("delete over the limit = %d %v", resp.StatusCode, out)
	}
}

func TestCalendarEventRoutesDeniedOnDemo(t *testing.T) {
	d := newDemoServer(t)
	e, _ := d.visitor()
	resp, out := e.do("PATCH", "/api/v1/calendar-sync/connections/c1/events/e1", map[string]any{"rev": "x", "summary": "y"})
	if resp.StatusCode != 403 || errCode(out) != "demo_restricted" {
		t.Fatalf("patch = %d %v", resp.StatusCode, out)
	}
	resp, out = e.do("DELETE", "/api/v1/calendar-sync/connections/c1/events/e1?rev=x", nil)
	if resp.StatusCode != 403 || errCode(out) != "demo_restricted" {
		t.Fatalf("delete = %d %v", resp.StatusCode, out)
	}
}

// patchOnly sends one PATCH with the rev and the given members and returns the answer.
func (v *eventsEnv) patchOnly(ev map[string]any, members map[string]any) (int, map[string]any) {
	v.e.t.Helper()
	body := map[string]any{"rev": ev["rev"]}
	for k, val := range members {
		body[k] = val
	}
	resp, out := v.e.do("PATCH", v.path(ev["id"].(string)), body)
	return resp.StatusCode, out
}

// TestCalendarEventPatchLocationDescriptionFree (calendar-event-editor task
// 3.1): each new member alone, clearing with an empty string, and the limits.
func TestCalendarEventPatchLocationDescriptionFree(t *testing.T) {
	v := newEventsEnv(t)
	e := v.e
	v.edits(true)
	cur := v.events(e)["Plain"]
	step := func(name string, members map[string]any, want ...string) {
		t.Helper()
		status, out := v.patchOnly(cur, members)
		if status != 200 {
			t.Fatalf("%s = %d %v", name, status, out)
		}
		cur = out["event"].(map[string]any)
		obj := v.cal.Get("work", "plain.ics")
		for _, w := range want {
			if !strings.Contains(obj, w) {
				t.Fatalf("%s: object lacks %q:\n%s", name, w, obj)
			}
		}
	}
	step("location", map[string]any{"location": "Room 4"}, "LOCATION:Room 4", "SUMMARY:Plain")
	if cur["location"] != "Room 4" || cur["summary"] != "Plain" {
		t.Fatalf("row = %v", cur)
	}
	step("description", map[string]any{"description": "Bring the draft.\nTwo lines"}, `DESCRIPTION:Bring the draft.\nTwo lines`, "LOCATION:Room 4")
	step("free", map[string]any{"free": true}, "TRANSP:TRANSPARENT")
	if cur["free"] != true || cur["location"] != "Room 4" {
		t.Fatalf("row after free = %v", cur)
	}
	step("busy again", map[string]any{"free": false}, "TRANSP:OPAQUE")
	step("clear location", map[string]any{"location": ""})
	step("clear description", map[string]any{"description": ""})
	if obj := v.cal.Get("work", "plain.ics"); strings.Contains(obj, "LOCATION") || strings.Contains(obj, "DESCRIPTION") {
		t.Fatalf("cleared properties survived:\n%s", obj)
	}
	if cur["location"] != nil && cur["location"] != "" {
		t.Fatalf("location after clearing = %v", cur["location"])
	}
	// All three at once with a title.
	step("together", map[string]any{"summary": "All", "location": "X", "description": "Y", "free": true}, "SUMMARY:All", "LOCATION:X", "DESCRIPTION:Y", "TRANSP:TRANSPARENT")
	if got := e.events(audit.CalendarEventChangedName); len(got) != 7 {
		t.Fatalf("audit events = %d, want 7", len(got))
	}
}

func TestCalendarEventPatchNewFieldRules(t *testing.T) {
	v := newEventsEnv(t)
	e := v.e
	v.edits(true)
	cur := v.events(e)["Plain"]
	calls := func() int { return v.cal.Count("GET") + v.cal.Count("PUT") + v.cal.Count("DELETE") }
	base := calls()
	bad := map[string]map[string]any{
		"long location":    {"location": strings.Repeat("x", 501)},
		"long description": {"description": strings.Repeat("x", 10001)},
		"nul location":     {"location": "a\x00b"},
		"nul description":  {"description": "a\x00b"},
		"free not bool":    {"free": "yes"},
		"nothing":          {},
	}
	for name, members := range bad {
		status, out := v.patchOnly(cur, members)
		if status != 400 || errCode(out) != "invalid_input" {
			t.Fatalf("%s = %d %v", name, status, out)
		}
	}
	if calls() != base {
		t.Fatal("a refused body reached the calendar")
	}
	// The limits themselves are accepted, counted in characters.
	status, out := v.patchOnly(cur, map[string]any{"location": strings.Repeat("é", 500), "description": strings.Repeat("é", 10000)})
	if status != 200 {
		t.Fatalf("at the limits = %d %v", status, out)
	}
}

func TestCalendarEventPatchGuestsStayLockedForNewFields(t *testing.T) {
	v := newEventsEnv(t)
	v.edits(true)
	g := v.events(v.e)["Guests"]
	status, out := v.patchOnly(g, map[string]any{"location": "Room"})
	if status != 422 || errCode(out) != "event_not_editable" {
		t.Fatalf("guests = %d %v", status, out)
	}
}
