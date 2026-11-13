package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

// calendar-event-guests group 6: `ot calendar event respond`, --guest, --no-guest,
// --notify-guests, --online-meeting and the GUESTS column, against the calendar fake.

const guestEventsJSON = `{"events":[
 {"id":"aaaaaaaaaaaa111111111111","subscriptionId":"s1","summary":"Planning","start":{"date":"2026-10-12","time":"09:00","tz":"Europe/Berlin"},"end":{"date":"2026-10-12","time":"09:30","tz":"Europe/Berlin"},"allDay":false,"rev":"r1",
  "access":{"update":false,"delete":false,"reason":"not_organizer"},"guests":{"count":3,"you":"tentative","organizer":false,"canRespond":true},"conference":{"url":"https://teams.example/j/1","provider":"teams"}},
 {"id":"bbbbbbbbbbbb222222222222","subscriptionId":"s1","summary":"Mine","start":{"date":"2026-10-13","time":"10:00","tz":"Europe/Berlin"},"end":{"date":"2026-10-13","time":"11:00","tz":"Europe/Berlin"},"allDay":false,"rev":"r2",
  "access":{"update":true,"delete":true,"notifies":true},"guests":{"count":2,"organizer":true,"canRespond":false}},
 {"id":"cccccccccccc333333333333","subscriptionId":"s1","summary":"Solo","start":{"date":"2026-10-14","time":"10:00","tz":"Europe/Berlin"},"end":{"date":"2026-10-14","time":"11:00","tz":"Europe/Berlin"},"allDay":false,"rev":"r3","access":{"update":true,"delete":true}},
 {"id":"dddddddddddd444444444444","subscriptionId":"s1","summary":"Weekly","start":{"date":"2026-10-15","time":"10:00"},"end":{"date":"2026-10-15","time":"11:00"},"allDay":false,"rev":"r4","series":{"id":"x","custom":false,"first":false,"repeat":{"freq":"weekly","interval":1}},
  "access":{"update":false,"delete":false,"reason":"not_organizer"},"guests":{"count":4,"you":"needs-action","organizer":false,"canRespond":true}},
 {"id":"eeeeeeeeeeee555555555555","subscriptionId":"f1","summary":"Holiday","start":{"date":"2026-10-16"},"end":{"date":"2026-10-17"},"allDay":true}
],"subscriptions":[{"id":"s1","name":"Work","managed":true,"connectionId":"conn1","guests":{"respond":true,"silent":false,"edit":true}},{"id":"f1","name":"Holidays","managed":false}]}`

func guestHarness(t *testing.T, scope string) (*harness, *calFake) {
	h, f := newCalHarness(t, scope)
	f.events = guestEventsJSON
	return h, f
}

func TestCalendarEventsShowGuests(t *testing.T) {
	h, _ := guestHarness(t, "read")
	out := h.mustRun("calendar", "events", "--from", "2026-10-12", "--to", "2026-10-18")
	for _, want := range []string{"GUESTS", "3 · maybe", "2", "4 · no answer"} {
		if !strings.Contains(out, want) {
			t.Fatalf("table lacks %q:\n%s", want, out)
		}
	}
	var list []map[string]any
	if err := json.Unmarshal([]byte(h.mustRun("calendar", "events", "--json")), &list); err != nil || len(list) != 5 {
		t.Fatalf("json: %v %v", list, err)
	}
	if list[0]["guests"] == nil || list[0]["conference"] == nil {
		t.Fatalf("--json must carry guests and conference: %v", list[0])
	}
}

func TestCalendarEventRespond(t *testing.T) {
	h, f := guestHarness(t, "full")
	out := h.mustRun("calendar", "event", "respond", "aaaa", "--maybe")
	if !strings.Contains(out, "Standup") && !strings.Contains(out, "answer:") {
		t.Fatalf("output %q", out)
	}
	req, body := lastBody(t, f)
	if req != "POST /api/v1/calendar-sync/connections/conn1/events/aaaaaaaaaaaa111111111111/response" || body["rev"] != "r1" || body["response"] != "tentative" {
		t.Fatalf("request %s %v", req, body)
	}
	if _, has := body["scope"]; has {
		t.Fatalf("a single event was sent a scope: %v", body)
	}
	if _, has := body["sendResponse"]; has {
		t.Fatalf("sendResponse sent without --no-tell: %v", body)
	}
	h.mustRun("calendar", "event", "respond", "aaaa", "--yes")
	if _, body := lastBody(t, f); body["response"] != "accepted" {
		t.Fatalf("--yes: %v", body)
	}
	h.mustRun("calendar", "event", "respond", "aaaa", "--no")
	if _, body := lastBody(t, f); body["response"] != "declined" {
		t.Fatalf("--no: %v", body)
	}
	var ev map[string]any
	if err := json.Unmarshal([]byte(h.mustRun("calendar", "event", "respond", "aaaa", "--yes", "--json")), &ev); err != nil || ev["rev"] != "r9" {
		t.Fatalf("json %v %v", ev, err)
	}
}

func TestCalendarEventRespondScope(t *testing.T) {
	h, f := guestHarness(t, "full")
	if code, _, errb := h.run("calendar", "event", "respond", "dddd", "--yes"); code != ExitUsage || !strings.Contains(errb, "scope") {
		t.Fatalf("an occurrence without --scope: exit %d %q", code, errb)
	}
	if code, _, _ := h.run("calendar", "event", "respond", "dddd", "--yes", "--scope", "following"); code != ExitUsage {
		t.Fatalf("following: exit %d", code)
	}
	if code, _, _ := h.run("calendar", "event", "respond", "aaaa", "--yes", "--scope", "all"); code != ExitUsage {
		t.Fatalf("a scope on a single event: exit %d", code)
	}
	if len(f.writes()) != 0 {
		t.Fatalf("a refused command wrote: %v", f.writes())
	}
	h.mustRun("calendar", "event", "respond", "dddd", "--maybe", "--scope", "all")
	if _, body := lastBody(t, f); body["scope"] != "all" || body["response"] != "tentative" {
		t.Fatalf("body %v", body)
	}
}

func TestCalendarEventRespondRefusals(t *testing.T) {
	h, f := guestHarness(t, "full")
	for _, args := range [][]string{
		{"calendar", "event", "respond", "aaaa"},
		{"calendar", "event", "respond", "aaaa", "--yes", "--no"},
		{"calendar", "event", "respond", "--yes"},
		// The subscription says silent is false: --no-tell is refused locally.
		{"calendar", "event", "respond", "aaaa", "--yes", "--no-tell"},
	} {
		if code, _, _ := h.run(args...); code != ExitUsage {
			t.Fatalf("%v: exit %d", args, code)
		}
	}
	if code, _, _ := h.run("calendar", "event", "respond", "eeee", "--yes"); code != ExitNotPermitted {
		t.Fatalf("a feed event: exit %d", code)
	}
	if len(f.writes()) != 0 {
		t.Fatalf("a refused command wrote: %v", f.writes())
	}
	// A subscription that allows it sends sendResponse false.
	f.events = strings.Replace(guestEventsJSON, `"silent":false`, `"silent":true`, 1)
	h.mustRun("calendar", "event", "respond", "aaaa", "--no", "--no-tell")
	if _, body := lastBody(t, f); body["sendResponse"] != false {
		t.Fatalf("body %v", body)
	}
}

func TestCalendarEventRespondServerErrors(t *testing.T) {
	h, f := guestHarness(t, "full")
	f.status, f.code = 422, "event_not_editable"
	if code, _, errb := h.run("calendar", "event", "respond", "aaaa", "--yes"); code != ExitServer || !strings.Contains(errb, "event_not_editable") {
		t.Fatalf("cannot_respond: exit %d %q", code, errb)
	}
	f.status, f.code = 409, "event_changed"
	if code, _, _ := h.run("calendar", "event", "respond", "aaaa", "--yes"); code != ExitServer {
		t.Fatalf("event_changed: exit %d", code)
	}
}

func TestCalendarEventRespondNeedsAFullToken(t *testing.T) {
	h, f := guestHarness(t, "read")
	if code, _, _ := h.run("calendar", "event", "respond", "aaaa", "--yes"); code != ExitNotPermitted {
		t.Fatalf("read token: exit %d", code)
	}
	if len(f.writes()) != 0 {
		t.Fatalf("a read token wrote: %v", f.writes())
	}
}

// Changing an event that tells its guests needs --notify-guests, and the command says so
// before it sends anything.
func TestCalendarEventEditNeedsNotifyGuests(t *testing.T) {
	h, f := guestHarness(t, "full")
	code, _, errb := h.run("calendar", "event", "edit", "bbbb", "--title", "Planning")
	if code != ExitUsage || !strings.Contains(errb, "--notify-guests") || !strings.Contains(errb, "tells its guests") {
		t.Fatalf("exit %d %q", code, errb)
	}
	if len(f.writes()) != 0 {
		t.Fatalf("a refused command wrote: %v", f.writes())
	}
	h.mustRun("calendar", "event", "edit", "bbbb", "--title", "Planning", "--notify-guests")
	req, body := lastBody(t, f)
	if !strings.HasPrefix(req, "PATCH ") || body["notifyGuests"] != true || body["summary"] != "Planning" || body["rev"] != "r2" {
		t.Fatalf("%s %v", req, body)
	}
	// An event that tells nobody needs no flag, and sends none.
	h.mustRun("calendar", "event", "edit", "cccc", "--title", "Alone")
	if _, body := lastBody(t, f); body["notifyGuests"] != nil {
		t.Fatalf("notifyGuests sent unasked: %v", body)
	}
}

func TestCalendarEventEditGuests(t *testing.T) {
	h, f := guestHarness(t, "full")
	if code, _, errb := h.run("calendar", "event", "edit", "cccc", "--guest", "ben@example.com"); code != ExitUsage || !strings.Contains(errb, "--notify-guests") {
		t.Fatalf("inviting without the flag: exit %d %q", code, errb)
	}
	if code, _, _ := h.run("calendar", "event", "edit", "cccc", "--guest", "not-an-address", "--notify-guests"); code != ExitUsage {
		t.Fatalf("a bad address: exit %d", code)
	}
	if code, _, _ := h.run("calendar", "event", "edit", "cccc", "--guest", "ben@example.com:maybe", "--notify-guests"); code != ExitUsage {
		t.Fatalf("a bad role: exit %d", code)
	}
	if len(f.writes()) != 0 {
		t.Fatalf("a refused command wrote: %v", f.writes())
	}
	h.mustRun("calendar", "event", "edit", "bbbb", "--guest", "ben@example.com", "--guest", "cy@example.com:optional", "--no-guest", "ana@example.com", "--online-meeting", "--notify-guests")
	_, body := lastBody(t, f)
	g, _ := body["guests"].(map[string]any)
	add, _ := g["add"].([]any)
	rem, _ := g["remove"].([]any)
	if len(add) != 2 || add[0].(map[string]any)["email"] != "ben@example.com" || add[0].(map[string]any)["role"] != "required" ||
		add[1].(map[string]any)["role"] != "optional" || len(rem) != 1 || rem[0] != "ana@example.com" ||
		body["notifyGuests"] != true || body["onlineMeeting"] != true {
		t.Fatalf("body %v", body)
	}
	// Taking a guest off, and --remove-guest, count as a change by themselves.
	h.mustRun("calendar", "event", "edit", "bbbb", "--remove-guest", "ana@example.com", "--notify-guests")
	if _, body := lastBody(t, f); body["guests"].(map[string]any)["remove"] == nil {
		t.Fatalf("body %v", body)
	}
}

func TestCalendarEventAddWithGuests(t *testing.T) {
	h, f := guestHarness(t, "full")
	args := []string{"calendar", "event", "add", "Kickoff", "--start", "2026-10-20T10:00", "--for", "30m", "--guest", "ben@example.com"}
	if code, _, errb := h.run(args...); code != ExitUsage || !strings.Contains(errb, "--notify-guests") {
		t.Fatalf("exit %d %q", code, errb)
	}
	if len(f.writes()) != 0 {
		t.Fatalf("a refused command wrote: %v", f.writes())
	}
	h.mustRun(append(args, "--online-meeting", "--notify-guests")...)
	req, body := lastBody(t, f)
	g, _ := body["guests"].([]any)
	if !strings.HasPrefix(req, "POST ") || len(g) != 1 || g[0].(map[string]any)["email"] != "ben@example.com" || body["notifyGuests"] != true || body["onlineMeeting"] != true {
		t.Fatalf("%s %v", req, body)
	}
	// A plain add sends no guest member.
	h.mustRun("calendar", "event", "add", "Dentist", "--start", "2026-10-21T10:00", "--for", "30m")
	if _, body := lastBody(t, f); body["guests"] != nil || body["notifyGuests"] != nil || body["onlineMeeting"] != nil {
		t.Fatalf("body %v", body)
	}
}

func TestCalendarEventDeleteNeedsNotifyGuests(t *testing.T) {
	h, f := guestHarness(t, "full")
	code, _, errb := h.run("calendar", "event", "delete", "bbbb", "--yes")
	if code != ExitUsage || !strings.Contains(errb, "--notify-guests") {
		t.Fatalf("exit %d %q", code, errb)
	}
	if len(f.writes()) != 0 {
		t.Fatalf("a refused command wrote: %v", f.writes())
	}
	h.mustRun("calendar", "event", "delete", "bbbb", "--yes", "--notify-guests")
	if w := f.writes(); len(w) != 1 || !strings.Contains(w[0], "?rev=r2&notifyGuests=true") {
		t.Fatalf("requests %v", w)
	}
	// An event that tells nobody deletes as before.
	h.mustRun("calendar", "event", "delete", "cccc", "--yes")
	if w := f.writes(); strings.Contains(w[len(w)-1], "notifyGuests") {
		t.Fatalf("notifyGuests sent unasked: %v", w)
	}
}
