package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/OpenTodoApp/opentodo/internal/calsync"
	"github.com/OpenTodoApp/opentodo/internal/calsync/calsynctest"
	"github.com/OpenTodoApp/opentodo/internal/calsync/graphtest"
)

// Guests, replies and join links over HTTP (OpenSpec change calendar-event-guests,
// tasks 3.1 to 3.4). Microsoft is the in-process Graph on loopback; nothing leaves
// the machine.

// Every address and link of the fixtures: none of them may reach a log line, an
// audit detail or an error message (design D12).
var guestSecrets = []string{"ben@example.com", "cy@example.com", "room-4@example.com", "ana@example.com", "new@example.com",
	"me@outlook.com", "teams.microsoft.com", "meet.google.com", "meetup-join"}

type msGuestsEnv struct {
	e    *env
	srv  *Server
	ms   *graphtest.Server
	id   string
	day  string
	logs *lockedBuffer
}

// lockedBuffer is a log sink that handlers on several goroutines can write.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func msEvent(subject, day string, f func(e *graphtest.Event)) graphtest.Event {
	e := graphtest.Event{Calendar: "cal-work", Subject: subject,
		Start: graphtest.DT{DateTime: day + "T10:00:00", TimeZone: "UTC"}, End: graphtest.DT{DateTime: day + "T11:00:00", TimeZone: "UTC"},
		Guests: []graphtest.Attendee{
			{Address: "ben@example.com", Name: "Ben", Type: "required", Response: "accepted"},
			{Address: "room-4@example.com", Name: "Room 4", Type: "resource", Response: "accepted"},
			{Address: "cy@example.com", Name: "Cy", Type: "optional", Response: "notResponded"}}}
	if f != nil {
		f(&e)
	}
	return e
}

// newMSGuestsEnv connects a Microsoft account whose calendar lists Teams and holds an
// organiser's meeting with a Teams link, an invitation and a plain event, with the logs
// of the server and the worker captured.
func newMSGuestsEnv(t *testing.T) *msGuestsEnv {
	t.Helper()
	e, srv, ms := microsoftEnv(t)
	logs := &lockedBuffer{}
	logger := slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	prev := slog.Default()
	slog.SetDefault(logger)
	t.Cleanup(func() { slog.SetDefault(prev) })
	srv.log, srv.calsync.Log = logger, logger
	ms.AddCalendar(&graphtest.Calendar{ID: "cal-work", Name: "Calendar", Color: "#3366cc", CanEdit: true, OnlineProviders: []string{"teamsForBusiness"}})
	resp, out := e.do("PUT", "/api/v1/settings/calendar-sync/microsoft", map[string]any{"client_id": ms.ClientID, "client_secret": ms.ClientSecret, "tenant": "consumers"})
	e.mustStatus(resp, 200, out)
	state := msStart(t, e, nil)
	msCallback(t, e, url.Values{"code": {ms.Code}, "state": {state}})
	resp, out = e.do("GET", "/api/v1/calendar-sync/connections", nil)
	e.mustStatus(resp, 200, out)
	id := out["connections"].([]any)[0].(map[string]any)["id"].(string)
	resp, out = e.do("PATCH", "/api/v1/calendar-sync/connections/"+id, map[string]any{"target": "cal-work", "edit_events": true})
	e.mustStatus(resp, 200, out)
	day := time.Now().UTC().AddDate(0, 0, 1).Format("2006-01-02")
	ms.Add(msEvent("Mine", day, func(g *graphtest.Event) {
		g.JoinURL, g.IsOnlineMeeting = "https://teams.microsoft.com/l/meetup-join/abc", true
	}))
	ms.Add(msEvent("Invited", day, func(g *graphtest.Event) {
		g.NotOrganizer, g.Organizer = true, graphtest.Person{Name: "Ana", Address: "ana@example.com"}
		g.Guests = append(g.Guests, graphtest.Attendee{Address: "me@outlook.com", Name: "Me", Type: "required", Response: "notResponded"})
	}))
	ms.Add(msEvent("Alone", day, func(g *graphtest.Event) { g.Guests = nil }))
	resp, out = e.do("POST", "/api/v1/calendar-sync/connections/"+id+"/sync", nil)
	e.mustStatus(resp, 200, out)
	return &msGuestsEnv{e: e, srv: srv, ms: ms, id: id, day: day, logs: logs}
}

func (v *msGuestsEnv) events(c *env) (map[string]map[string]any, []any) {
	v.e.t.Helper()
	to := time.Now().UTC().AddDate(0, 0, 15).Format("2006-01-02")
	resp, out := c.do("GET", "/api/v1/calendar-feeds/events?from="+v.day+"&to="+to, nil)
	c.mustStatus(resp, 200, out)
	got := map[string]map[string]any{}
	for _, x := range out["events"].([]any) {
		m := x.(map[string]any)
		got[m["summary"].(string)] = m
	}
	return got, out["subscriptions"].([]any)
}

func (v *msGuestsEnv) path(id, tail string) string {
	return "/api/v1/calendar-sync/connections/" + v.id + "/events/" + id + tail
}

func (v *msGuestsEnv) writes() int {
	return v.ms.Count("PATCH") + v.ms.Count("POST /v1.0/me/events/") + v.ms.Count("DELETE") + v.ms.Count("POST /v1.0/me/calendars")
}

func errField(out map[string]any) string {
	if m, ok := out["error"].(map[string]any); ok {
		f, _ := m["field"].(string)
		return f
	}
	return ""
}

func messageOf(out map[string]any) string {
	if m, ok := out["error"].(map[string]any); ok {
		s, _ := m["message"].(string)
		return s
	}
	return ""
}

// noSecrets fails when text holds an address or link of the fixtures.
func noSecrets(t *testing.T, what, text string) {
	t.Helper()
	for _, s := range guestSecrets {
		if strings.Contains(text, s) {
			t.Errorf("%s holds %q", what, s)
		}
	}
}

// TestCalendarEventsServeGuests (task 3.3): the events route carries the summary, the
// join link, access.notifies and the provider's capabilities, and no address.
func TestCalendarEventsServeGuests(t *testing.T) {
	v := newMSGuestsEnv(t)
	evs, subs := v.events(v.e)
	mine, inv, alone := evs["Mine"], evs["Invited"], evs["Alone"]
	g, _ := mine["guests"].(map[string]any)
	if g["count"] != float64(3) || g["organizer"] != true || g["canRespond"] != false || g["you"] != nil {
		t.Fatalf("mine guests = %v", mine["guests"])
	}
	if c, _ := mine["conference"].(map[string]any); c["provider"] != "teams" || !strings.HasPrefix(fmt.Sprint(c["url"]), "https://teams.microsoft.com/") {
		t.Fatalf("mine conference = %v", mine["conference"])
	}
	if a := accessOf(mine); a["update"] != true || a["delete"] != true || a["notifies"] != true {
		t.Fatalf("mine access = %v", a)
	}
	g, _ = inv["guests"].(map[string]any)
	if g["you"] != "needs-action" || g["organizer"] != false || g["canRespond"] != true || accessOf(inv)["reason"] != "not_organizer" || accessOf(inv)["notifies"] != nil {
		t.Fatalf("invited = %v access %v", inv["guests"], accessOf(inv))
	}
	if alone["guests"] != nil || alone["conference"] != nil || accessOf(alone)["notifies"] != nil || accessOf(alone)["update"] != true {
		t.Fatalf("alone = %v", alone)
	}
	raw, _ := json.Marshal(evs)
	noSecrets(t, "the events answer", strings.ReplaceAll(string(raw), "https://teams.microsoft.com/l/meetup-join/abc", ""))
	var managed map[string]any
	for _, s := range subs {
		if m := s.(map[string]any); m["managed"] == true {
			managed = m
		}
	}
	if gc, _ := managed["guests"].(map[string]any); gc["respond"] != true || gc["silent"] != true || gc["edit"] != true {
		t.Fatalf("subscription guests = %v", managed)
	}
	if caps, _ := managed["caps"].(map[string]any); caps["onlineMeeting"] != true {
		t.Fatalf("subscription caps = %v", managed["caps"])
	}
}

// TestCalendarEventGuestsRoute (task 3.3): the cache-only list, never stored by a
// cache, 404 for anything that is not the caller's or has no guests.
func TestCalendarEventGuestsRoute(t *testing.T) {
	v := newMSGuestsEnv(t)
	evs, _ := v.events(v.e)
	before := v.ms.Count("GET")
	resp, out := v.e.do("GET", "/api/v1/calendar-feeds/events/"+evs["Mine"]["id"].(string)+"/guests", nil)
	v.e.mustStatus(resp, 200, out)
	if resp.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control = %q", resp.Header.Get("Cache-Control"))
	}
	if v.ms.Count("GET") != before {
		t.Fatal("the guest list asked the calendar")
	}
	guests := out["guests"].([]any)
	if out["count"] != float64(3) || out["truncated"] != false || len(guests) != 3 {
		t.Fatalf("list = %v", out)
	}
	first := guests[0].(map[string]any)
	if first["email"] != "ben@example.com" || first["role"] != "required" || first["response"] != "accepted" || first["you"] != false {
		t.Fatalf("first = %v", first)
	}
	if org := out["organizer"].(map[string]any); org["email"] != "me@outlook.com" || org["you"] != true {
		t.Fatalf("organizer = %v", out["organizer"])
	}
	if resp, out := v.e.do("GET", "/api/v1/calendar-feeds/events/"+evs["Alone"]["id"].(string)+"/guests", nil); resp.StatusCode != 404 || errCode(out) != "not_found" {
		t.Fatalf("no guests = %d %v", resp.StatusCode, out)
	}
	if resp, out := v.e.do("GET", "/api/v1/calendar-feeds/events/ffffffffffffffffffffffff/guests", nil); resp.StatusCode != 404 {
		t.Fatalf("unknown = %d %v", resp.StatusCode, out)
	}
	m, _ := v.e.member("m@example.com", "198.51.100.61")
	if resp, _ := m.do("GET", "/api/v1/calendar-feeds/events/"+evs["Mine"]["id"].(string)+"/guests", nil); resp.StatusCode != 404 {
		t.Fatalf("another user = %d", resp.StatusCode)
	}
	if resp, _ := newClientAt(v.e, "10.9.9.13").do("GET", "/api/v1/calendar-feeds/events/"+evs["Mine"]["id"].(string)+"/guests", nil); resp.StatusCode != 401 {
		t.Fatalf("anonymous = %d", resp.StatusCode)
	}
}

// TestCalendarEventDetailCarriesGuests (task 3.3): the detail route reads the guests
// and the online meeting from the calendar now.
func TestCalendarEventDetailCarriesGuests(t *testing.T) {
	v := newMSGuestsEnv(t)
	evs, _ := v.events(v.e)
	resp, out := v.e.do("GET", v.path(evs["Mine"]["id"].(string), ""), nil)
	v.e.mustStatus(resp, 200, out)
	g := out["guests"].(map[string]any)
	if g["count"] != float64(3) || g["canEdit"] != true || g["canRespond"] != false || len(g["guests"].([]any)) != 3 {
		t.Fatalf("guests = %v", g)
	}
	if om := out["onlineMeeting"].(map[string]any); om["available"] != true || om["on"] != true {
		t.Fatalf("onlineMeeting = %v", om)
	}
	if c := out["conference"].(map[string]any); c["provider"] != "teams" {
		t.Fatalf("conference = %v", c)
	}
	resp, out = v.e.do("GET", v.path(evs["Invited"]["id"].(string), ""), nil)
	v.e.mustStatus(resp, 200, out)
	if g := out["guests"].(map[string]any); g["canRespond"] != true || g["canEdit"] != false || g["you"] != "needs-action" {
		t.Fatalf("invited = %v", g)
	}
}

// TestCalendarEventNotifyRule (task 3.1): an organiser's change, delete or invitation
// is refused without notifyGuests true, and nothing is requested, written or sent.
func TestCalendarEventNotifyRule(t *testing.T) {
	v := newMSGuestsEnv(t)
	evs, _ := v.events(v.e)
	mine, alone := evs["Mine"], evs["Alone"]
	id, rev := mine["id"].(string), mine["rev"].(string)
	patch := func(members map[string]any) (int, map[string]any) {
		body := map[string]any{"rev": rev}
		for k, val := range members {
			body[k] = val
		}
		resp, out := v.e.do("PATCH", v.path(id, ""), body)
		return resp.StatusCode, out
	}
	for name, members := range map[string]map[string]any{
		"missing": {"summary": "Renamed"},
		"false":   {"summary": "Renamed", "notifyGuests": false},
		"free":    {"free": true},
		"invite":  {"guests": map[string]any{"add": []map[string]any{{"email": "new@example.com"}}}},
	} {
		status, out := patch(members)
		if status != 400 || errCode(out) != "invalid_input" || errField(out) != "notifyGuests" {
			t.Fatalf("%s = %d %v", name, status, out)
		}
		noSecrets(t, name+" answer", fmt.Sprint(out))
	}
	resp, out := v.e.do("DELETE", v.path(id, "")+"?rev="+rev, nil)
	if resp.StatusCode != 400 || errField(out) != "notifyGuests" {
		t.Fatalf("delete = %d %v", resp.StatusCode, out)
	}
	if resp, out := v.e.do("DELETE", v.path(id, "")+"?rev="+rev+"&notifyGuests=maybe", nil); resp.StatusCode != 400 || errField(out) != "notifyGuests" {
		t.Fatalf("bad flag = %d %v", resp.StatusCode, out)
	}
	if v.writes() != 0 {
		t.Fatalf("%d writes were made without a confirmation", v.writes())
	}
	v.ms.AssertNoMessages(t)
	// An event nobody is told about needs no confirmation.
	resp, out = v.e.do("PATCH", v.path(alone["id"].(string), ""), map[string]any{"rev": alone["rev"], "summary": "Solo"})
	v.e.mustStatus(resp, 200, out)
	v.ms.AssertNoMessages(t)
	// With it, exactly one update goes out to each guest.
	status, out := patch(map[string]any{"summary": "Renamed", "notifyGuests": true})
	if status != 200 {
		t.Fatalf("confirmed = %d %v", status, out)
	}
	if v.ms.Count("PATCH") != 2 || len(v.ms.Messages()) != 3 {
		t.Fatalf("patches %d, messages %+v", v.ms.Count("PATCH"), v.ms.Messages())
	}
	row := out["event"].(map[string]any)
	if row["summary"] != "Renamed" || row["guests"].(map[string]any)["count"] != float64(3) || accessOf(row)["notifies"] != true {
		t.Fatalf("answer = %v", row)
	}
	// The delete asks for the same confirmation; with it the meeting is cancelled once.
	v.ms.ClearMessages()
	resp, out = v.e.do("DELETE", v.path(id, "")+"?rev="+row["rev"].(string)+"&notifyGuests=true", nil)
	v.e.mustStatus(resp, 204, out)
	if v.ms.Count("DELETE") != 1 {
		t.Fatalf("deletes = %d", v.ms.Count("DELETE"))
	}
}

// TestCalendarEventGuestsBodyRules (task 3.1): every row of D3's body rules answers
// 400 invalid_input with the field path and the index, and never the value.
func TestCalendarEventGuestsBodyRules(t *testing.T) {
	v := newMSGuestsEnv(t)
	evs, _ := v.events(v.e)
	mine := evs["Mine"]
	long := strings.Repeat("a", 130)
	many := make([]map[string]any, 51)
	for i := range many {
		many[i] = map[string]any{"email": fmt.Sprintf("g%d@example.com", i)}
	}
	cases := map[string]struct {
		guests map[string]any
		field  string
	}{
		"empty":             {map[string]any{}, "guests"},
		"bad address":       {map[string]any{"add": []map[string]any{{"email": "new@example.com"}, {"email": "Ben <ben@example.com>"}}}, "guests.add[1].email"},
		"comma":             {map[string]any{"add": []map[string]any{{"email": "a@example.com, b@example.com"}}}, "guests.add[0].email"},
		"duplicate":         {map[string]any{"add": []map[string]any{{"email": "new@example.com"}, {"email": "NEW@example.com"}}}, "guests.add[1].email"},
		"name too long":     {map[string]any{"add": []map[string]any{{"email": "new@example.com", "name": long}}}, "guests.add[0].name"},
		"control in name":   {map[string]any{"add": []map[string]any{{"email": "new@example.com", "name": "A\u0007B"}}}, "guests.add[0].name"},
		"role":              {map[string]any{"add": []map[string]any{{"email": "new@example.com", "role": "host"}}}, "guests.add[0].role"},
		"too many to add":   {map[string]any{"add": many}, "guests.add"},
		"too many to drop":  {map[string]any{"remove": make([]string, 51)}, "guests.remove"},
		"bad removal":       {map[string]any{"remove": []string{"ben@example.com", "nope"}}, "guests.remove[1]"},
		"added and removed": {map[string]any{"add": []map[string]any{{"email": "new@example.com"}}, "remove": []string{"new@example.com"}}, "guests.remove[0]"},
		"own address":       {map[string]any{"add": []map[string]any{{"email": "me@outlook.com"}}}, "guests.add"},
	}
	for name, tc := range cases {
		resp, out := v.e.do("PATCH", v.path(mine["id"].(string), ""), map[string]any{"rev": mine["rev"], "notifyGuests": true, "guests": tc.guests})
		if resp.StatusCode != 400 || errCode(out) != "invalid_input" || !strings.HasPrefix(errField(out), tc.field) {
			t.Fatalf("%s = %d %v, want field %s", name, resp.StatusCode, out, tc.field)
		}
		noSecrets(t, name+" answer", fmt.Sprint(out))
		for _, bad := range []string{long, "Ben <", "a@example.com, b@"} {
			if strings.Contains(messageOf(out), bad) {
				t.Fatalf("%s: the message holds the value: %s", name, messageOf(out))
			}
		}
	}
	// onlineMeeting false cannot be honoured; a body that only asks for a meeting is a change.
	resp, out := v.e.do("PATCH", v.path(mine["id"].(string), ""), map[string]any{"rev": mine["rev"], "notifyGuests": true, "onlineMeeting": false})
	if resp.StatusCode != 400 || errField(out) != "onlineMeeting" {
		t.Fatalf("onlineMeeting false = %d %v", resp.StatusCode, out)
	}
	if v.writes() != 0 {
		t.Fatalf("%d writes for refused bodies", v.writes())
	}
	v.ms.AssertNoMessages(t)
}

// TestCalendarEventGuestsAddAndRemove: the guests of an organiser's meeting change, only
// the changed guests are told, and the answer carries the new summary.
func TestCalendarEventGuestsAddAndRemove(t *testing.T) {
	v := newMSGuestsEnv(t)
	evs, _ := v.events(v.e)
	mine := evs["Mine"]
	resp, out := v.e.do("PATCH", v.path(mine["id"].(string), ""), map[string]any{"rev": mine["rev"], "notifyGuests": true,
		"guests": map[string]any{"add": []map[string]any{{"email": "New@Example.com", "name": "New", "role": "optional"}}, "remove": []string{"ben@example.com"}}})
	v.e.mustStatus(resp, 200, out)
	if g := out["event"].(map[string]any)["guests"].(map[string]any); g["count"] != float64(3) {
		t.Fatalf("summary = %v", g)
	}
	kinds := map[string]string{}
	for _, m := range v.ms.Messages() {
		kinds[m.To] = m.Kind
	}
	if len(kinds) != 2 || kinds["new@example.com"] != "update" || kinds["ben@example.com"] != "cancel" {
		t.Fatalf("messages = %+v", v.ms.Messages())
	}
}

// TestCalendarEventCreateWithGuests (task 3.1): a create with guests needs the
// confirmation, then invites once and a replay invites nobody again.
func TestCalendarEventCreateWithGuests(t *testing.T) {
	v := newMSGuestsEnv(t)
	body := func(extra map[string]any) map[string]any {
		b := map[string]any{"clientId": "client-guests-1", "calendarUrl": "cal-work", "summary": "Kick-off",
			"start": map[string]any{"date": v.day, "time": "15:00", "tz": "UTC"}, "end": map[string]any{"date": v.day, "time": "16:00", "tz": "UTC"}}
		for k, val := range extra {
			b[k] = val
		}
		return b
	}
	guests := []map[string]any{{"email": "ben@example.com", "name": "Ben"}, {"email": "cy@example.com", "role": "optional"}}
	resp, out := v.e.do("POST", "/api/v1/calendar-sync/connections/"+v.id+"/events", body(map[string]any{"guests": guests}))
	if resp.StatusCode != 400 || errField(out) != "notifyGuests" {
		t.Fatalf("unconfirmed = %d %v", resp.StatusCode, out)
	}
	if resp, out := v.e.do("POST", "/api/v1/calendar-sync/connections/"+v.id+"/events", body(map[string]any{"guests": []map[string]any{{"email": "x"}}, "notifyGuests": true})); resp.StatusCode != 400 || errField(out) != "guests[0].email" {
		t.Fatalf("bad guest = %d %v", resp.StatusCode, out)
	}
	if v.writes() != 0 {
		t.Fatal("a refused create wrote")
	}
	resp, out = v.e.do("POST", "/api/v1/calendar-sync/connections/"+v.id+"/events", body(map[string]any{"guests": guests, "notifyGuests": true, "onlineMeeting": true}))
	v.e.mustStatus(resp, 201, out)
	ev := out["event"].(map[string]any)
	if ev["conference"].(map[string]any)["provider"] != "teams" || ev["guests"].(map[string]any)["count"] != float64(2) || len(v.ms.Messages()) != 2 {
		t.Fatalf("created = %v messages %+v", ev, v.ms.Messages())
	}
	resp, out = v.e.do("POST", "/api/v1/calendar-sync/connections/"+v.id+"/events", body(map[string]any{"guests": guests, "notifyGuests": true}))
	v.e.mustStatus(resp, 200, out)
	if len(v.ms.Messages()) != 2 {
		t.Fatalf("the replay invited again: %+v", v.ms.Messages())
	}
}

// TestCalendarEventRespondRoute (task 3.2): the body rules, the reply itself, the
// refusals and the policy.
func TestCalendarEventRespondRoute(t *testing.T) {
	v := newMSGuestsEnv(t)
	evs, _ := v.events(v.e)
	inv, mine := evs["Invited"], evs["Mine"]
	path := v.path(inv["id"].(string), "/response")
	rev := inv["rev"].(string)
	for name, tc := range map[string]struct {
		body  map[string]any
		field string
	}{
		"no rev":        {map[string]any{"response": "accepted"}, "rev"},
		"bad answer":    {map[string]any{"rev": rev, "response": "yes"}, "response"},
		"no answer":     {map[string]any{"rev": rev}, "response"},
		"following":     {map[string]any{"rev": rev, "response": "accepted", "scope": "following"}, "scope"},
		"scope on one":  {map[string]any{"rev": rev, "response": "accepted", "scope": "all"}, ""},
		"unknown field": {map[string]any{"rev": rev, "response": "accepted", "comment": "hi"}, ""},
	} {
		resp, out := v.e.do("POST", path, tc.body)
		switch {
		case tc.field != "":
			if resp.StatusCode != 400 || errField(out) != tc.field {
				t.Fatalf("%s = %d %v", name, resp.StatusCode, out)
			}
		case name == "scope on one":
			if resp.StatusCode != 400 || errField(out) != "scope" {
				t.Fatalf("%s = %d %v", name, resp.StatusCode, out)
			}
		default:
			if resp.StatusCode != 400 {
				t.Fatalf("%s = %d %v", name, resp.StatusCode, out)
			}
		}
	}
	if v.writes() != 0 {
		t.Fatal("a refused reply wrote")
	}
	// The user organises this one: 422 cannot_respond.
	resp, out := v.e.do("POST", v.path(mine["id"].(string), "/response"), map[string]any{"rev": mine["rev"], "response": "accepted"})
	if resp.StatusCode != 422 || errCode(out) != "event_not_editable" || !strings.HasSuffix(messageOf(out), "cannot_respond") {
		t.Fatalf("organiser = %d %v", resp.StatusCode, out)
	}
	// A stale rev.
	if resp, out := v.e.do("POST", path, map[string]any{"rev": "0000000000000000", "response": "accepted"}); resp.StatusCode != 409 || errCode(out) != "event_changed" {
		t.Fatalf("stale = %d %v", resp.StatusCode, out)
	}
	// Silent: the reply is kept from the organiser.
	resp, out = v.e.do("POST", path, map[string]any{"rev": rev, "response": "tentative", "sendResponse": false})
	v.e.mustStatus(resp, 200, out)
	row := out["event"].(map[string]any)
	if row["guests"].(map[string]any)["you"] != "tentative" || row["rev"] == rev || out["replaces"] == nil {
		t.Fatalf("answer = %v", out)
	}
	v.ms.AssertNoMessages(t)
	resp, out = v.e.do("POST", path, map[string]any{"rev": row["rev"], "response": "declined"})
	v.e.mustStatus(resp, 200, out)
	if m := v.ms.Messages(); len(m) != 1 || m[0].Kind != "reply" {
		t.Fatalf("messages = %+v", m)
	}
	// Policy: authentication, read and agent tokens, calendar sync off.
	if resp, _ := newClientAt(v.e, "10.9.9.14").do("POST", path, map[string]any{"rev": rev, "response": "accepted"}); resp.StatusCode != 401 {
		t.Fatalf("anonymous = %d", resp.StatusCode)
	}
	for _, scope := range []string{"read", "agent"} {
		secret := v.e.token(scope)
		if resp, out := v.e.bearerClient().do("POST", path, map[string]any{"rev": rev, "response": "accepted"}, bearer(secret)...); resp.StatusCode != 403 {
			t.Fatalf("%s token = %d %v", scope, resp.StatusCode, out)
		}
	}
	m, _ := v.e.member("m@example.com", "198.51.100.62")
	if resp, _ := m.do("POST", path, map[string]any{"rev": rev, "response": "accepted"}); resp.StatusCode != 404 {
		t.Fatalf("another user = %d", resp.StatusCode)
	}
	resp, out = v.e.do("PUT", "/api/v1/settings/calendar-sync", map[string]any{"enabled": false})
	v.e.mustStatus(resp, 200, out)
	if resp, out := v.e.do("POST", path, map[string]any{"rev": rev, "response": "accepted"}); resp.StatusCode != 404 || errCode(out) != "calendar_sync_disabled" {
		t.Fatalf("sync off = %d %v", resp.StatusCode, out)
	}
}

// TestCalendarEventRespondRouteDeniedOnDemo (task 3.2): the demo refuses both routes.
func TestCalendarEventRespondRouteDeniedOnDemo(t *testing.T) {
	d := newDemoServer(t)
	e, _ := d.visitor()
	resp, out := e.do("POST", "/api/v1/calendar-sync/connections/c1/events/e1/response", map[string]any{"rev": "x", "response": "accepted"})
	if resp.StatusCode != 403 || errCode(out) != "demo_restricted" {
		t.Fatalf("response = %d %v", resp.StatusCode, out)
	}
	resp, out = e.do("GET", "/api/v1/calendar-feeds/events/e1/guests", nil)
	if resp.StatusCode != 403 || errCode(out) != "demo_restricted" {
		t.Fatalf("guests = %d %v", resp.StatusCode, out)
	}
}

// TestCalendarEventRespondLimiter: a reply takes one unit of the event limiter, as
// every other event write does.
func TestCalendarEventRespondLimiter(t *testing.T) {
	v := newMSGuestsEnv(t)
	evs, _ := v.events(v.e)
	inv := evs["Invited"]
	path := v.path(inv["id"].(string), "/response")
	for i := 0; i < calsyncEventsPerHr; i++ {
		resp, _ := v.e.do("POST", path, map[string]any{"rev": "0000000000000000", "response": "accepted"})
		if resp.StatusCode == 429 {
			t.Fatalf("limited after %d requests", i)
		}
	}
	if resp, out := v.e.do("POST", path, map[string]any{"rev": "0000000000000000", "response": "accepted"}); resp.StatusCode != 429 || errCode(out) != "rate_limited" {
		t.Fatalf("over the limit = %d %v", resp.StatusCode, out)
	}
}

// TestEventFailureMapsResponseNotKept: the 502 row of D3 without a Google account.
func TestEventFailureMapsResponseNotKept(t *testing.T) {
	var s Server
	rec := httptest.NewRecorder()
	s.eventFailure(rec, &calsync.EventError{Code: calsync.EventUnreachable, Reason: calsync.ReasonResponseNotKept, Err: calsync.ErrResponseNotKept})
	if rec.Code != 502 || !strings.Contains(rec.Body.String(), `"calendar_unreachable"`) || !strings.Contains(rec.Body.String(), "response_not_kept") {
		t.Fatalf("answer = %d %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	s.eventFailure(rec, &calsync.EventError{Code: calsync.EventNotEditable, Reason: calsync.ReasonCannotRespond})
	if rec.Code != 422 || !strings.Contains(rec.Body.String(), "cannot_respond") {
		t.Fatalf("answer = %d %s", rec.Code, rec.Body.String())
	}
}

// TestCalDAVOrganiserEventsAreNeverWritten (task 3.1): a CalDAV event with guests,
// the user's own among them, is refused for every change with nothing written.
func TestCalDAVOrganiserEventsAreNeverWritten(t *testing.T) {
	v := newEventsEnv(t)
	e := v.e
	v.edits(true)
	stamp := strings.ReplaceAll(v.day, "-", "")
	v.cal.Scheduling, v.cal.Owner = calsynctest.SchedulingAuto, "me@example.com"
	v.cal.Put("work", "org.ics", "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VEVENT\r\nUID:org\r\nSUMMARY:Organised\r\nDTSTART:"+stamp+"T150000Z\r\nDTEND:"+stamp+"T160000Z\r\n"+
		"ORGANIZER:mailto:me@example.com\r\nATTENDEE:mailto:me@example.com\r\nATTENDEE:mailto:ben@example.com\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n")
	resp, out := e.do("POST", "/api/v1/calendar-sync/connections/"+v.connID+"/sync", nil)
	e.mustStatus(resp, 200, out)
	org := v.events(e)["Organised"]
	if org == nil || accessOf(org)["reason"] != "has_guests" || accessOf(org)["update"] != false || accessOf(org)["notifies"] != nil {
		t.Fatalf("organised = %v", org)
	}
	id, rev := org["id"].(string), org["rev"].(string)
	for name, call := range map[string]func() int{
		"rename": func() int {
			resp, _ := e.do("PATCH", v.path(id), map[string]any{"rev": rev, "summary": "x", "notifyGuests": true})
			return resp.StatusCode
		},
		"guests": func() int {
			resp, _ := e.do("PATCH", v.path(id), map[string]any{"rev": rev, "notifyGuests": true, "guests": map[string]any{"add": []map[string]any{{"email": "new@example.com"}}}})
			return resp.StatusCode
		},
		"delete": func() int {
			resp, _ := e.do("DELETE", v.path(id)+"?rev="+rev+"&notifyGuests=true", nil)
			return resp.StatusCode
		},
		"reply": func() int {
			resp, _ := e.do("POST", v.path(id)+"/response", map[string]any{"rev": rev, "response": "accepted"})
			return resp.StatusCode
		},
	} {
		if status := call(); status != 422 && status != 400 {
			t.Fatalf("%s = %d", name, status)
		}
	}
	for _, w := range v.cal.Writes() {
		if strings.Contains(w, "org.ics") {
			t.Fatalf("the organiser's event was written: %q", v.cal.Writes())
		}
	}
	v.cal.AssertNoMessages(t)
}

// TestGuestPathsLogAndAuditNoPersonalData (task 3.4, design D12, D18): after a reply, a
// guest change, a delete and a create, no log line, audit detail or error answer holds a
// fixture address or link, and the audit rows carry the new keys.
func TestGuestPathsLogAndAuditNoPersonalData(t *testing.T) {
	v := newMSGuestsEnv(t)
	evs, _ := v.events(v.e)
	mine, inv := evs["Mine"], evs["Invited"]
	answers := []string{}
	note := func(out map[string]any) { answers = append(answers, fmt.Sprint(out)) }
	resp, out := v.e.do("POST", v.path(inv["id"].(string), "/response"), map[string]any{"rev": inv["rev"], "response": "accepted"})
	v.e.mustStatus(resp, 200, out)
	note(out)
	_, out = v.e.do("POST", v.path(inv["id"].(string), "/response"), map[string]any{"rev": "0000000000000000", "response": "accepted"})
	note(out)
	_, out = v.e.do("PATCH", v.path(mine["id"].(string), ""), map[string]any{"rev": mine["rev"], "summary": "x"})
	note(out)
	_, out = v.e.do("PATCH", v.path(mine["id"].(string), ""), map[string]any{"rev": mine["rev"], "notifyGuests": true, "guests": map[string]any{"add": []map[string]any{{"email": "bad address"}}}})
	note(out)
	resp, out = v.e.do("PATCH", v.path(mine["id"].(string), ""), map[string]any{"rev": mine["rev"], "notifyGuests": true,
		"guests": map[string]any{"add": []map[string]any{{"email": "new@example.com", "name": "New Guest"}}, "remove": []string{"ben@example.com"}}})
	v.e.mustStatus(resp, 200, out)
	row := out["event"].(map[string]any)
	resp, out = v.e.do("POST", "/api/v1/calendar-sync/connections/"+v.id+"/events", map[string]any{"clientId": "client-guests-2", "calendarUrl": "cal-work", "summary": "Kick-off",
		"start": map[string]any{"date": v.day, "time": "17:00", "tz": "UTC"}, "end": map[string]any{"date": v.day, "time": "18:00", "tz": "UTC"},
		"guests": []map[string]any{{"email": "ben@example.com"}}, "notifyGuests": true, "onlineMeeting": true})
	v.e.mustStatus(resp, 201, out)
	resp, out = v.e.do("DELETE", v.path(row["id"].(string), "")+"?rev="+row["rev"].(string)+"&notifyGuests=true", nil)
	v.e.mustStatus(resp, 204, out)
	resp, out = v.e.do("GET", v.path(row["id"].(string), ""), nil)
	note(out)
	_ = resp
	for i, a := range answers {
		noSecrets(t, fmt.Sprintf("error answer %d", i), a)
	}
	slog.Info("capture-probe") // the capture works: a line written now is in the buffer
	if !strings.Contains(v.logs.String(), "capture-probe") {
		t.Fatal("the log capture is not attached")
	}
	noSecrets(t, "the captured log", v.logs.String())
	// The audit rows.
	rows, err := v.e.db.Query(`SELECT details FROM audit_events WHERE name = 'calendar_event.changed' ORDER BY rowid`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var seen []string
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			t.Fatal(err)
		}
		noSecrets(t, "an audit detail", d)
		seen = append(seen, d)
	}
	all := strings.Join(seen, "\n")
	for _, want := range []string{`"event_action":"responded"`, `"event_response":"accepted"`, `"event_notified":"true"`, `"event_action":"deleted"`, `"event_action":"created"`} {
		if !strings.Contains(strings.ReplaceAll(all, `": "`, `":"`), want) {
			t.Errorf("no audit detail with %s in:\n%s", want, all)
		}
	}
}
