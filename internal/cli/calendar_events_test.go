package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// A fake server for `ot calendar`: the events route, /me for the token's scope
// and the two event routes, which answer what the test says.

type calFake struct {
	mu     sync.Mutex
	scope  string
	reqs   []string // "METHOD /path?query body"
	unauth bool     // every request is a 401
	status int      // status of PATCH, POST and DELETE (0: success)
	conns  string   // body of GET /calendar-sync/connections (default: one writable calendar)
	code   string
}

const eventsJSON = `{"events":[
 {"id":"aaaaaaaaaaaa111111111111","subscriptionId":"s1","summary":"Standup","start":{"date":"2026-10-12","time":"09:00","tz":"Europe/Berlin"},"end":{"date":"2026-10-12","time":"09:30","tz":"Europe/Berlin"},"allDay":false,"rev":"r1","access":{"update":true,"delete":true}},
 {"id":"bbbbbbbbbbbb222222222222","subscriptionId":"s1","summary":"Offsite","start":{"date":"2026-10-13"},"end":{"date":"2026-10-15"},"allDay":true,"rev":"r2","access":{"update":true,"delete":true}},
 {"id":"cccccccccccc333333333333","subscriptionId":"s1","summary":"Weekly sync","start":{"date":"2026-10-14","time":"10:00"},"end":{"date":"2026-10-14","time":"11:00"},"allDay":false,"rev":"r3","access":{"update":false,"delete":false,"reason":"recurring"}},
 {"id":"dddddddddddd444444444444","subscriptionId":"f1","summary":"Holiday","start":{"date":"2026-10-15"},"end":{"date":"2026-10-16"},"allDay":true}
],"subscriptions":[{"id":"s1","name":"Work","managed":true,"connectionId":"conn1"},{"id":"f1","name":"Holidays","managed":false}]}`

func newCalHarness(t *testing.T, scope string) (*harness, *calFake) {
	t.Helper()
	f := &calFake{scope: scope}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		if f.unauth {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"code":"unauthorized","message":"sign in"}}`))
			return
		}
		switch {
		case r.URL.Path == "/api/v1/me":
			_, _ = w.Write([]byte(`{"user":{"id":"u1","email":"a@b.c","name":"A","role":"member"},"auth":{"method":"token","scope":"` + f.scope + `"}}`))
			return
		case r.URL.Path == "/api/v1/calendar-sync/connections" && r.Method == http.MethodGet:
			body := f.conns
			if body == "" {
				body = oneCalendarJSON
			}
			_, _ = w.Write([]byte(body))
			return
		case r.URL.Path == "/api/v1/calendar-feeds/events":
			_, _ = w.Write([]byte(eventsJSON))
			return
		}
		f.mu.Lock()
		f.reqs = append(f.reqs, r.Method+" "+r.URL.RequestURI()+" "+string(body))
		f.mu.Unlock()
		if f.status != 0 {
			w.WriteHeader(f.status)
			_, _ = w.Write([]byte(`{"error":{"code":"` + f.code + `","message":"refused: ` + f.code + `"}}`))
			return
		}
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		_, _ = w.Write([]byte(`{"event":{"id":"eeeeeeeeeeee555555555555","subscriptionId":"s1","summary":"Standup","start":{"date":"2026-10-12","time":"14:00","tz":"Europe/Berlin"},"end":{"date":"2026-10-12","time":"14:30","tz":"Europe/Berlin"},"allDay":false,"rev":"r9"},"replaces":"aaaaaaaaaaaa111111111111"}`))
	}))
	t.Cleanup(srv.Close)
	h := &harness{t: t, srv: srv, configPath: t.TempDir() + "/cfg/config.json", env: map[string]string{}}
	h.setToken("ot_fake")
	return h, f
}

func (f *calFake) writes() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.reqs...)
}

func TestCalendarEventsTableAndJSON(t *testing.T) {
	h, _ := newCalHarness(t, "read")
	out := h.mustRun("calendar", "events", "--from", "2026-10-12", "--to", "2026-10-18")
	for _, want := range []string{"ID", "DATE", "TIME", "TITLE", "CALENDAR", "ACCESS", "aaaaaaaa", "09:00-09:30", "Standup", "Work", "edit", "all day", "read-only (recurring)", "Holidays"} {
		if !strings.Contains(out, want) {
			t.Fatalf("table lacks %q:\n%s", want, out)
		}
	}
	var list []map[string]any
	if err := json.Unmarshal([]byte(h.mustRun("calendar", "events", "--json")), &list); err != nil || len(list) != 4 || list[0]["rev"] != "r1" {
		t.Fatalf("json: %v %v", list, err)
	}
	// ot calendar alone lists the week too.
	if got := h.mustRun("calendar"); !strings.Contains(got, "Standup") {
		t.Fatalf("calendar: %q", got)
	}
}

func TestCalendarEventEditSendsTheRev(t *testing.T) {
	h, f := newCalHarness(t, "full")
	out := h.mustRun("calendar", "event", "edit", "aaaaaaaa", "--start", "2026-10-12T14:00")
	if !strings.Contains(out, "14:00") {
		t.Fatalf("output %q", out)
	}
	w := f.writes()
	if len(w) != 1 || !strings.HasPrefix(w[0], "PATCH /api/v1/calendar-sync/connections/conn1/events/aaaaaaaaaaaa111111111111 ") {
		t.Fatalf("requests %v", w)
	}
	var body map[string]any
	_ = json.Unmarshal([]byte(strings.SplitN(w[0], " ", 3)[2]), &body)
	start, end := body["start"].(map[string]any), body["end"].(map[string]any)
	// A new start alone keeps the 30 minute length and the event's zone.
	if body["rev"] != "r1" || start["time"] != "14:00" || end["time"] != "14:30" || start["tz"] != "Europe/Berlin" || body["allDay"] != false {
		t.Fatalf("body %v", body)
	}
	if _, has := body["summary"]; has {
		t.Fatal("a title nobody changed was sent")
	}
	// A rename sends only the title.
	h.mustRun("calendar", "event", "edit", "aaaa", "--title", "Daily")
	var rename map[string]any
	_ = json.Unmarshal([]byte(strings.SplitN(f.writes()[1], " ", 3)[2]), &rename)
	if rename["summary"] != "Daily" || rename["start"] != nil {
		t.Fatalf("rename body %v", rename)
	}
	// --json prints the event the server returned.
	var ev map[string]any
	if err := json.Unmarshal([]byte(h.mustRun("calendar", "event", "edit", "aaaa", "--end", "2026-10-12T10:00", "--json")), &ev); err != nil || ev["rev"] != "r9" {
		t.Fatalf("json %v %v", ev, err)
	}
	// An all-day event keeps its span when it moves.
	h.mustRun("calendar", "event", "edit", "bbbb", "--start", "2026-10-20")
	var allday map[string]any
	_ = json.Unmarshal([]byte(strings.SplitN(f.writes()[3], " ", 3)[2]), &allday)
	if allday["allDay"] != true || allday["start"].(map[string]any)["date"] != "2026-10-20" || allday["end"].(map[string]any)["date"] != "2026-10-22" {
		t.Fatalf("all-day body %v", allday)
	}
}

func TestCalendarEventEditUsageErrors(t *testing.T) {
	h, f := newCalHarness(t, "full")
	for _, args := range [][]string{
		{"calendar", "event", "edit", "aaaa"},
		{"calendar", "event", "edit", "zzzz", "--title", "x"},
		{"calendar", "event", "edit", "aaaa", "--start", "2026-10-12T25:00"},
		{"calendar", "event", "edit", "aaaa", "--end", "2026-10-12T08:00"},
		{"calendar", "event", "edit", "bbbb", "--start", "2026-10-20T10:00"},
		{"calendar", "event", "edit", "dddd", "--title", "x"},
	} {
		code, _, errb := h.run(args...)
		if code == 0 {
			t.Fatalf("%v accepted", args)
		}
		_ = errb
	}
	if code, _, _ := h.run("calendar", "event", "edit", "dddd", "--title", "x"); code != ExitNotPermitted {
		t.Fatalf("a feed event: exit %d", code)
	}
	if w := f.writes(); len(w) != 0 {
		t.Fatalf("a refused command wrote: %v", w)
	}
}

func TestCalendarEventDelete(t *testing.T) {
	h, f := newCalHarness(t, "full")
	// Off a terminal a destructive command needs --yes.
	if code, _, _ := h.run("calendar", "event", "delete", "aaaa"); code != ExitUsage {
		t.Fatalf("no --yes: exit %d", code)
	}
	if out := h.mustRun("calendar", "event", "delete", "aaaa", "--yes"); !strings.Contains(out, "deleted") {
		t.Fatalf("output %q", out)
	}
	w := f.writes()
	if len(w) != 1 || !strings.HasPrefix(w[0], "DELETE /api/v1/calendar-sync/connections/conn1/events/aaaaaaaaaaaa111111111111?rev=r1") {
		t.Fatalf("requests %v", w)
	}
}

// A dry run plans the request and sends nothing (the harness-backed dry-run
// test has no connected calendar to find an event in).
func TestCalendarEventDryRun(t *testing.T) {
	h, f := newCalHarness(t, "full")
	for _, args := range [][]string{
		{"calendar", "event", "edit", "aaaa", "--title", "Renamed", "--dry-run", "--json"},
		{"calendar", "event", "delete", "aaaa", "--dry-run", "--json"},
	} {
		out := h.mustRun(args...)
		if !strings.Contains(out, `"dry_run":true`) && !strings.Contains(out, `"dry_run": true`) || !strings.Contains(out, "/events/aaaaaaaaaaaa111111111111") {
			t.Fatalf("%v: %s", args, out)
		}
	}
	if w := f.writes(); len(w) != 0 {
		t.Fatalf("a dry run wrote: %v", w)
	}
}

// event_changed exits 1 with its message; a read token is refused before
// anything is sent (and the server's 403 is exit 5); no login is exit 3.
func TestCalendarEventErrorsAndExitCodes(t *testing.T) {
	h, f := newCalHarness(t, "full")
	f.status, f.code = http.StatusConflict, "event_changed"
	code, _, errb := h.run("calendar", "event", "edit", "aaaa", "--title", "x")
	if code != ExitServer || !strings.Contains(errb, "event_changed") {
		t.Fatalf("event_changed: exit %d %q", code, errb)
	}
	f.status, f.code = http.StatusForbidden, "event_edits_off"
	if code, _, _ := h.run("calendar", "event", "delete", "aaaa", "--yes"); code != ExitNotPermitted {
		t.Fatalf("event_edits_off: exit %d", code)
	}
	f.status, f.code = http.StatusUnprocessableEntity, "event_not_editable"
	if code, _, _ := h.run("calendar", "event", "edit", "aaaa", "--title", "x"); code != ExitServer {
		t.Fatalf("event_not_editable: exit %d", code)
	}

	hr, fr := newCalHarness(t, "read")
	if code, out, _ := hr.run("calendar", "events"); code != 0 || !strings.Contains(out, "Standup") {
		t.Fatalf("a read token lists: %d %q", code, out)
	}
	code, _, errb = hr.run("calendar", "event", "edit", "aaaa", "--title", "x")
	if code != ExitNotPermitted || !strings.Contains(errb, "read-only") {
		t.Fatalf("read token: exit %d %q", code, errb)
	}
	if len(fr.writes()) != 0 {
		t.Fatal("a read token reached the server")
	}

	// Without a valid login the server answers 401: exit 3. A missing token is
	// the usual usage error before any request.
	hn, fn := newCalHarness(t, "full")
	fn.unauth = true
	if code, _, _ := hn.run("calendar", "events"); code != ExitAuth {
		t.Fatalf("401: exit %d", code)
	}
	delete(hn.env, "OPENTODO_TOKEN")
	if code, _, _ := hn.run("calendar", "events"); code != ExitUsage {
		t.Fatalf("no token: exit %d", code)
	}
}

func TestCalendarCompletion(t *testing.T) {
	h, _ := newCalHarness(t, "full")
	_ = h
	st := scanWords([]string{"calendar", "event"})
	if st.unknown || st.spec != nil {
		t.Fatalf("calendar event: %+v", st)
	}
	var got []string
	for _, it := range subcommandCandidates(st.top) {
		got = append(got, it.value)
	}
	if strings.Join(got, ",") != "add,edit,delete,rm" {
		t.Fatalf("after calendar event: %v", got)
	}
	top := map[string]bool{}
	for _, it := range subcommandCandidates("calendar") {
		top[it.value] = true
	}
	if !top["events"] || !top["event"] || top["event edit"] {
		t.Fatalf("calendar candidates: %v", top)
	}
	if st := scanWords([]string{"calendar", "event", "edit"}); st.spec == nil || st.spec.Name != "calendar event edit" {
		t.Fatalf("edit: %+v", st)
	}
}

const oneCalendarJSON = `{"connections":[{"id":"conn1","name":"Work","edit_events":true,"target":"https://cal/work/","read":["https://cal/work/"],
 "calendars":[{"url":"https://cal/work/","name":"Work","read_only":false,"caps":{"create":true,"update":true,"delete":true}},
 {"url":"https://cal/ro/","name":"Holidays","read_only":true,"caps":{"create":false,"update":false,"delete":false}}]}]}`

const twoCalendarsJSON = `{"connections":[{"id":"conn1","name":"Work","edit_events":true,"target":"https://cal/work/","read":["https://cal/work/","https://cal/team/"],
 "calendars":[{"url":"https://cal/work/","name":"Work","caps":{"create":true}},{"url":"https://cal/team/","name":"Team","caps":{"create":true}}]}]}`

func lastBody(t *testing.T, f *calFake) (string, map[string]any) {
	t.Helper()
	w := f.writes()
	if len(w) == 0 {
		t.Fatal("no request was sent")
	}
	parts := strings.SplitN(w[len(w)-1], " ", 3)
	var body map[string]any
	_ = json.Unmarshal([]byte(parts[2]), &body)
	return parts[0] + " " + parts[1], body
}

func TestCalendarEventAddCreatesInTheOnlyWritableCalendar(t *testing.T) {
	h, f := newCalHarness(t, "full")
	h.env["TZ"] = "Europe/Berlin"
	out := h.mustRun("calendar", "event", "add", "Call", "Ana", "--start", "2026-11-13T14:00", "--for", "30m", "--location", "Zoom", "--free")
	if !strings.Contains(out, "Standup") || !strings.Contains(out, "eeeeeeee") {
		t.Fatalf("output %q", out)
	}
	req, body := lastBody(t, f)
	if req != "POST /api/v1/calendar-sync/connections/conn1/events" {
		t.Fatalf("request %s", req)
	}
	start, end := body["start"].(map[string]any), body["end"].(map[string]any)
	if body["summary"] != "Call Ana" || body["calendarUrl"] != "https://cal/work/" || body["allDay"] != false || body["free"] != true || body["location"] != "Zoom" ||
		start["time"] != "14:00" || end["time"] != "14:30" || start["tz"] == nil || len(body["clientId"].(string)) < 8 {
		t.Fatalf("body %v", body)
	}
	if _, has := body["description"]; has {
		t.Fatal("an empty description was sent")
	}
	// Every run makes a fresh client id; --client-id keeps one.
	h.mustRun("calendar", "event", "add", "Call Ana", "--start", "2026-11-13T14:00", "--for", "30m")
	_, again := lastBody(t, f)
	if again["clientId"] == body["clientId"] {
		t.Fatal("client id reused")
	}
	h.mustRun("calendar", "event", "add", "x", "--start", "2026-11-13T14:00", "--for", "30m", "--client-id", "script-key-1")
	if _, b := lastBody(t, f); b["clientId"] != "script-key-1" {
		t.Fatalf("client id %v", b["clientId"])
	}
	// --json prints the event; an all-day event takes dates.
	var ev map[string]any
	if err := json.Unmarshal([]byte(h.mustRun("calendar", "event", "add", "Offsite", "--start", "2026-11-20", "--all-day", "--json")), &ev); err != nil || ev["rev"] != "r9" {
		t.Fatalf("json %v %v", ev, err)
	}
	_, ad := lastBody(t, f)
	if ad["allDay"] != true || ad["end"].(map[string]any)["date"] != "2026-11-21" || ad["start"].(map[string]any)["time"] != nil {
		t.Fatalf("all-day body %v", ad)
	}
}

func TestCalendarEventAddCalendarChoice(t *testing.T) {
	h, f := newCalHarness(t, "full")
	f.conns = twoCalendarsJSON
	code, _, errb := h.run("calendar", "event", "add", "x", "--start", "2026-11-13T14:00", "--for", "30m")
	if code != ExitUsage || !strings.Contains(errb, "Work") || !strings.Contains(errb, "https://cal/team/") {
		t.Fatalf("ambiguous: exit %d %q", code, errb)
	}
	if code, _, errb = h.run("calendar", "event", "add", "x", "--start", "2026-11-13T14:00", "--for", "30m", "--calendar", "nope"); code != ExitUsage || !strings.Contains(errb, "nope") {
		t.Fatalf("unknown: exit %d %q", code, errb)
	}
	if len(f.writes()) != 0 {
		t.Fatal("an unresolved calendar reached the server")
	}
	h.mustRun("calendar", "event", "add", "x", "--start", "2026-11-13T14:00", "--for", "30m", "--calendar", "team")
	if _, b := lastBody(t, f); b["calendarUrl"] != "https://cal/team/" {
		t.Fatalf("body %v", b)
	}
	// A calendar that only reads is never offered.
	f.conns = `{"connections":[{"id":"c","name":"N","target":"https://cal/ro/","read":[],"calendars":[{"url":"https://cal/ro/","name":"RO","read_only":true}]}]}`
	if code, _, _ := h.run("calendar", "event", "add", "x", "--start", "2026-11-13T14:00", "--for", "30m"); code != ExitUsage {
		t.Fatalf("no writable calendar: exit %d", code)
	}
}

func TestCalendarEventAddUsageAndGates(t *testing.T) {
	h, f := newCalHarness(t, "full")
	for _, args := range [][]string{
		{"add"}, {"add", "x"}, {"add", "x", "--start", "2026-11-13"}, {"add", "x", "--start", "2026-11-13T14:00"},
		{"add", "x", "--start", "2026-11-13T14:00", "--for", "30m", "--end", "2026-11-13T15:00"},
		{"add", "x", "--start", "2026-11-13T14:00", "--for", "soon"}, {"add", "x", "--start", "2026-11-13T14:00", "--end", "2026-11-13T13:00"},
		{"add", "x", "--start", "2026-11-13T14:00", "--all-day"},
	} {
		if code, _, _ := h.run(append([]string{"calendar", "event"}, args...)...); code != ExitUsage {
			t.Fatalf("%v: exit %d", args, code)
		}
	}
	f.status, f.code = http.StatusForbidden, "event_edits_off"
	if code, _, errb := h.run("calendar", "event", "add", "x", "--start", "2026-11-13T14:00", "--for", "30m"); code != ExitNotPermitted || !strings.Contains(errb, "event_edits_off") {
		t.Fatalf("event_edits_off: exit %d %q", code, errb)
	}
	f.code = "calendar_read_only"
	if code, _, _ := h.run("calendar", "event", "add", "x", "--start", "2026-11-13T14:00", "--for", "30m"); code != ExitNotPermitted {
		t.Fatalf("calendar_read_only: exit %d", code)
	}
	hr, fr := newCalHarness(t, "read")
	if code, _, errb := hr.run("calendar", "event", "add", "x", "--start", "2026-11-13T14:00", "--for", "30m"); code != ExitNotPermitted || !strings.Contains(errb, "read-only") {
		t.Fatalf("read token: exit %d %q", code, errb)
	}
	if len(fr.writes()) != 0 {
		t.Fatal("a read token reached the server")
	}
	hn, fn := newCalHarness(t, "full")
	fn.unauth = true
	delete(hn.env, "OPENTODO_TOKEN")
	if code, _, _ := hn.run("calendar", "event", "add", "x", "--start", "2026-11-13T14:00", "--for", "30m"); code != ExitUsage {
		t.Fatalf("no login: exit %d", code)
	}
	// A dry run resolves the calendar and sends no POST.
	hd, fd := newCalHarness(t, "full")
	hd.mustRun("calendar", "event", "add", "x", "--start", "2026-11-13T14:00", "--for", "30m", "--dry-run")
	if len(fd.writes()) != 0 {
		t.Fatalf("dry run wrote: %v", fd.writes())
	}
}

func TestCalendarEventEditTextFlags(t *testing.T) {
	h, f := newCalHarness(t, "full")
	h.mustRun("calendar", "event", "edit", "aaaa", "--location", "")
	_, b := lastBody(t, f)
	if v, has := b["location"]; !has || v != "" || b["summary"] != nil || b["start"] != nil {
		t.Fatalf("clear location: %v", b)
	}
	h.mustRun("calendar", "event", "edit", "aaaa", "--description", "agenda", "--busy")
	_, b = lastBody(t, f)
	if b["description"] != "agenda" || b["free"] != false {
		t.Fatalf("description and busy: %v", b)
	}
	h.mustRun("calendar", "event", "edit", "aaaa", "--free")
	if _, b = lastBody(t, f); b["free"] != true {
		t.Fatalf("free: %v", b)
	}
	if code, _, _ := h.run("calendar", "event", "edit", "aaaa", "--free", "--busy"); code != ExitUsage {
		t.Fatalf("free and busy: exit %d", code)
	}
}

// The "Usage:" block `ot calendar event` prints lists every subcommand, add included.
func TestCalendarUsageListsEventAdd(t *testing.T) {
	c := findCommand("calendar")
	if c == nil {
		t.Fatal("no calendar command")
	}
	for _, want := range []string{"ot calendar event add <title>", "ot calendar event edit <id>", "ot calendar event delete <id>"} {
		if !strings.Contains(c.usage, want) {
			t.Errorf("usage lacks %q:\n%s", want, c.usage)
		}
	}
}
