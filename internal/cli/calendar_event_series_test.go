package cli

import (
	"net/http"
	"strings"
	"testing"
)

// 6.3: scope, repeat and reminder flags (calendar-event-series D13).

const seriesEventsJSON = `{"events":[
 {"id":"aaaaaaaaaaaa111111111111","subscriptionId":"s1","summary":"Standup","start":{"date":"2026-10-12","time":"09:00","tz":"Europe/Berlin"},"end":{"date":"2026-10-12","time":"09:30","tz":"Europe/Berlin"},"allDay":false,"rev":"r1","access":{"update":true,"delete":true},
  "series":{"id":"ser1","first":false,"custom":false,"repeat":{"freq":"weekly","interval":1,"days":["mo"]}}},
 {"id":"bbbbbbbbbbbb222222222222","subscriptionId":"s1","summary":"Odd","start":{"date":"2026-10-13","time":"09:00"},"end":{"date":"2026-10-13","time":"10:00"},"allDay":false,"rev":"r2","access":{"update":false,"delete":false,"reason":"series_unsupported"},
  "series":{"id":"ser2","custom":true,"repeat":null}},
 {"id":"cccccccccccc333333333333","subscriptionId":"s1","summary":"Single","start":{"date":"2026-10-14","time":"10:00"},"end":{"date":"2026-10-14","time":"11:00"},"allDay":false,"rev":"r3","access":{"update":true,"delete":true}}
],"subscriptions":[{"id":"s1","name":"Work","managed":true,"connectionId":"conn1"}]}`

func newSeriesHarness(t *testing.T) (*harness, *calFake) {
	h, f := newCalHarness(t, "full")
	f.events = seriesEventsJSON
	return h, f
}

func TestCalendarEventsShowsRepeats(t *testing.T) {
	h, _ := newSeriesHarness(t)
	out := h.mustRun("calendar", "events", "--from", "2026-10-12", "--to", "2026-10-18")
	for _, want := range []string{"REPEATS", "weekly", "custom"} {
		if !strings.Contains(out, want) {
			t.Fatalf("table lacks %q:\n%s", want, out)
		}
	}
}

func TestCalendarEventScopeIsRequiredForAnOccurrence(t *testing.T) {
	h, f := newSeriesHarness(t)
	for _, args := range [][]string{
		{"calendar", "event", "edit", "aaaa", "--title", "x"},
		{"calendar", "event", "delete", "aaaa", "--yes"},
	} {
		code, _, errb := h.run(args...)
		if code != ExitUsage || !strings.Contains(errb, "this, following or all") {
			t.Fatalf("%v: exit %d %q", args, code, errb)
		}
	}
	if len(f.writes()) != 0 {
		t.Fatal("a request was sent without a scope")
	}
	if code, _, errb := h.run("calendar", "event", "edit", "cccc", "--title", "x", "--scope", "all"); code != ExitUsage || !strings.Contains(errb, "does not repeat") {
		t.Fatalf("scope on a single event: %d %q", code, errb)
	}
	if code, _, _ := h.run("calendar", "event", "edit", "aaaa", "--title", "x", "--scope", "some"); code != ExitUsage {
		t.Fatalf("bad scope: %d", code)
	}
	// The server's own scope_required is a usage error too.
	f.status, f.code = http.StatusBadRequest, "scope_required"
	if code, _, _ := h.run("calendar", "event", "edit", "cccc", "--title", "x"); code != ExitUsage {
		t.Fatalf("server scope_required: exit %d", code)
	}
}

func TestCalendarEventEditSendsScopeRepeatReminders(t *testing.T) {
	h, f := newSeriesHarness(t)
	h.mustRun("calendar", "event", "edit", "aaaa", "--scope", "following", "--repeat", "weekly", "--every", "2", "--days", "mo,we", "--count", "10", "--remind", "10m", "--remind", "1d")
	_, body := lastBody(t, f)
	rep, _ := body["repeat"].(map[string]any)
	ends, _ := rep["ends"].(map[string]any)
	rem, _ := body["reminders"].([]any)
	if body["scope"] != "following" || rep["freq"] != "weekly" || rep["interval"] != float64(2) || ends["type"] != "after" || ends["count"] != float64(10) || len(rem) != 2 || rem[0] != float64(10) || rem[1] != float64(1440) {
		t.Fatalf("body %v", body)
	}
	if _, has := body["summary"]; has {
		t.Fatal("a title nobody changed was sent")
	}
	// --days does not collide with --on: --on still finds the event.
	h.mustRun("calendar", "event", "edit", "aaaa", "--on", "2026-10-12", "--scope", "all", "--repeat", "weekdays", "--until", "2026-12-31")
	_, body = lastBody(t, f)
	rep, _ = body["repeat"].(map[string]any)
	if d, _ := rep["days"].([]any); len(d) != 5 || rep["freq"] != "weekly" || rep["ends"].(map[string]any)["date"] != "2026-12-31" {
		t.Fatalf("weekdays body %v", body)
	}
	h.mustRun("calendar", "event", "edit", "aaaa", "--scope", "following", "--no-repeat", "--no-remind")
	_, body = lastBody(t, f)
	if v, has := body["repeat"]; !has || v != nil {
		t.Fatalf("no-repeat body %v", body)
	}
	if r, _ := body["reminders"].([]any); r == nil || len(r) != 0 {
		t.Fatalf("no-remind body %v", body)
	}
	// A scope-only call changes nothing: it is a usage error.
	if code, _, _ := h.run("calendar", "event", "edit", "aaaa", "--scope", "all"); code != ExitUsage {
		t.Fatalf("scope alone: %d", code)
	}
}

func TestCalendarEventRepeatUsageErrors(t *testing.T) {
	h, f := newSeriesHarness(t)
	for name, args := range map[string][]string{
		"this with repeat":     {"edit", "aaaa", "--scope", "this", "--repeat", "daily"},
		"no-repeat with all":   {"edit", "aaaa", "--scope", "all", "--no-repeat"},
		"repeat and no-repeat": {"edit", "aaaa", "--scope", "following", "--repeat", "daily", "--no-repeat"},
		"every alone":          {"edit", "aaaa", "--scope", "all", "--every", "2"},
		"bad repeat":           {"edit", "aaaa", "--scope", "all", "--repeat", "hourly"},
		"until and count":      {"edit", "aaaa", "--scope", "all", "--repeat", "daily", "--until", "2026-12-01", "--count", "3"},
		"days on daily":        {"edit", "aaaa", "--scope", "all", "--repeat", "daily", "--days", "mo"},
		"bad day":              {"edit", "aaaa", "--scope", "all", "--repeat", "weekly", "--days", "mon"},
		"by on weekly":         {"edit", "aaaa", "--scope", "all", "--repeat", "weekly", "--by", "weekday"},
		"remind and none":      {"edit", "aaaa", "--scope", "all", "--remind", "5m", "--no-remind"},
		"bad remind":           {"edit", "aaaa", "--scope", "all", "--remind", "soon"},
	} {
		if code, _, errb := h.run(append([]string{"calendar", "event"}, args...)...); code != ExitUsage {
			t.Fatalf("%s: exit %d %q", name, code, errb)
		}
	}
	if len(f.writes()) != 0 {
		t.Fatalf("a refused command sent %v", f.writes())
	}
}

func TestCalendarEventDeleteSendsScope(t *testing.T) {
	h, f := newSeriesHarness(t)
	h.mustRun("calendar", "event", "delete", "aaaa", "--scope", "following", "--yes")
	w := f.writes()
	if len(w) != 1 || !strings.HasPrefix(w[0], "DELETE /api/v1/calendar-sync/connections/conn1/events/aaaaaaaaaaaa111111111111?rev=r1&scope=following") {
		t.Fatalf("requests %v", w)
	}
}

func TestCalendarEventAddRepeatAndReminders(t *testing.T) {
	h, f := newSeriesHarness(t)
	h.mustRun("calendar", "event", "add", "Gym", "--start", "2026-11-16T07:00", "--for", "1h", "--repeat", "monthly", "--by", "weekday", "--remind", "30m")
	_, body := lastBody(t, f)
	rep, _ := body["repeat"].(map[string]any)
	if rep["freq"] != "monthly" || rep["by"] != "weekday" || body["scope"] != nil {
		t.Fatalf("body %v", body)
	}
	if r, _ := body["reminders"].([]any); len(r) != 1 || r[0] != float64(30) {
		t.Fatalf("reminders %v", body["reminders"])
	}
	if code, _, _ := h.run("calendar", "event", "add", "Gym", "--start", "2026-11-16T07:00", "--for", "1h", "--scope", "all"); code != ExitUsage {
		t.Fatalf("add takes no --scope: %d", code)
	}
}
