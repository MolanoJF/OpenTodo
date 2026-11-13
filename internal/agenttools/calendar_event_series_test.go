package agenttools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/OpenTodoApp/opentodo/internal/audit"
)

// 6.2: scope, repeat and reminders of the calendar event tools (calendar-event-series D13).

func seriesArgs(extra map[string]any) map[string]any {
	m := map[string]any{"connection_id": "c1", "event_id": "e1", "rev": "r1", "summary": "Standup"}
	for k, v := range extra {
		m[k] = v
	}
	return m
}

func TestScopeIsDocumentedForAgents(t *testing.T) {
	f, _ := eventFixture(t)
	found := 0
	for _, d := range f.reg.Definitions(agent("u1")) {
		switch d.Name {
		case "update_calendar_event", "delete_calendar_event":
			raw, _ := json.Marshal(d.InputSchema)
			for _, w := range []string{"`this` changes only that occurrence", "`following` changes that occurrence and every later one", "`all` changes every occurrence", `"enum":["this","following","all"]`} {
				if !strings.Contains(string(raw), w) {
					t.Fatalf("%s schema lacks %q: %s", d.Name, w, raw)
				}
			}
			found++
		case "create_calendar_event":
			raw, _ := json.Marshal(d.InputSchema)
			if strings.Contains(string(raw), `"scope"`) || !strings.Contains(string(raw), `"repeat"`) || !strings.Contains(string(raw), "calendar provider") {
				t.Fatalf("create schema: %s", raw)
			}
		}
	}
	if found != 2 {
		t.Fatalf("found %d tools", found)
	}
}

func TestUpdateCalendarEventPassesScopeRepeatReminders(t *testing.T) {
	f, ed := eventFixture(t)
	c := agent("u1")
	f.mustCall(c, "update_calendar_event", seriesArgs(map[string]any{"scope": "all",
		"repeat":    map[string]any{"freq": "weekly", "interval": 2, "days": []string{"mo", "we"}, "ends": map[string]any{"type": "after", "count": 10}},
		"reminders": []int{10, 1440}}))
	u := ed.updates[0]
	if u.Scope != "all" || !u.RepeatSet || u.Repeat == nil || u.Repeat.Freq != "weekly" || u.Repeat.Interval != 2 || u.Repeat.Ends.Count != 10 || u.Reminders == nil || len(*u.Reminders) != 2 {
		t.Fatalf("update: %+v", u)
	}
	// repeat: null stops the repeat (following only); [] clears the reminders.
	f.mustCall(c, "update_calendar_event", seriesArgs(map[string]any{"scope": "following", "repeat": nil, "reminders": []int{}}))
	u = ed.updates[1]
	if !u.RepeatSet || u.Repeat != nil || u.Reminders == nil || len(*u.Reminders) != 0 {
		t.Fatalf("stop: %+v", u)
	}
	f.mustCall(c, "update_calendar_event", seriesArgs(nil))
	if u = ed.updates[2]; u.RepeatSet || u.Scope != "" || u.Reminders != nil {
		t.Fatalf("omitted: %+v", u)
	}
	f.mustCall(c, "delete_calendar_event", map[string]any{"connection_id": "c1", "event_id": "e1", "rev": "r1", "scope": "following"})
	if len(ed.deleteScopes) != 1 || ed.deleteScopes[0] != "following" {
		t.Fatalf("delete scope: %v", ed.deleteScopes)
	}
}

func TestCalendarEventScopeArgumentsAreChecked(t *testing.T) {
	f, ed := eventFixture(t)
	c := agent("u1")
	rule := map[string]any{"freq": "daily"}
	for name, bad := range map[string]map[string]any{
		"unknown scope":       seriesArgs(map[string]any{"scope": "some"}),
		"repeat with this":    seriesArgs(map[string]any{"scope": "this", "repeat": rule}),
		"stop with all":       seriesArgs(map[string]any{"scope": "all", "repeat": nil}),
		"unknown repeat key":  seriesArgs(map[string]any{"scope": "all", "repeat": map[string]any{"freq": "daily", "x": 1}}),
		"reminders not ints":  seriesArgs(map[string]any{"reminders": []any{"soon"}}),
		"repeat without freq": seriesArgs(map[string]any{"scope": "all", "repeat": map[string]any{}}),
	} {
		_, err := f.call(c, "update_calendar_event", bad)
		var ae *ArgError
		if !asArg(err, &ae) {
			t.Fatalf("%s: %v", name, err)
		}
		if name == "unknown scope" && !strings.Contains(ae.Msg, "this, following or all") {
			t.Fatalf("scope message: %s", ae.Msg)
		}
	}
	if _, err := f.call(c, "delete_calendar_event", map[string]any{"connection_id": "c1", "event_id": "e1", "rev": "r1", "scope": "x"}); err == nil {
		t.Fatal("delete with a bad scope accepted")
	}
	if len(ed.updates)+len(ed.deletes) != 0 {
		t.Fatal("a refused call wrote")
	}
}

func TestScopeRequiredReachesTheAgent(t *testing.T) {
	f, ed := eventFixture(t)
	ed.err = &ToolError{Code: "scope_required", Message: "this event repeats: say which events with scope (this, following or all)"}
	if _, err := f.call(agent("u1"), "update_calendar_event", seriesArgs(nil)); toolCode0(t, err) != "scope_required" {
		t.Fatalf("%v", err)
	}
}

func TestCreateCalendarEventTakesRepeatAndReminders(t *testing.T) {
	f, ed := eventFixture(t)
	c := agent("u1")
	f.mustCall(c, "create_calendar_event", withArgs(map[string]any{"repeat": map[string]any{"freq": "weekly"}, "reminders": []int{15}}))
	got := ed.creates[0]
	if got.Repeat == nil || got.Repeat.Freq != "weekly" || got.Repeat.Ends.Type != "never" || len(got.Reminders) != 1 || got.Reminders[0] != 15 {
		t.Fatalf("create: %+v", got)
	}
	if _, err := f.call(c, "create_calendar_event", withArgs(map[string]any{"scope": "all"})); err == nil {
		t.Fatal("create takes no scope")
	}
	f.mustCall(c, "create_calendar_event", withArgs(map[string]any{"repeat": nil}))
	if ed.creates[1].Repeat != nil {
		t.Fatal("null repeat is none")
	}
}

func TestListCalendarEventsShowsSeries(t *testing.T) {
	f, ed := eventFixture(t)
	ed.events[0].Series = &EventSeries{ID: "s1", Repeats: true, First: true, Repeat: &EventRepeat{Freq: "daily", Ends: EventRepeatEnd{Type: "never"}}}
	read := agent("u1")
	read.Scope = "read"
	out := f.mustCall(read, "list_calendar_events", map[string]any{"from": "2026-11-12", "to": "2026-11-12"})
	raw, _ := json.Marshal(out)
	if !strings.Contains(string(raw), `"series":{"id":"s1","repeats":true,"first":true,"custom":false,"repeat":{"freq":"daily"`) {
		t.Fatalf("series: %s", raw)
	}
}

func TestSeriesWritesAreAuditedAsOneItem(t *testing.T) {
	f, _ := eventFixture(t)
	f.mustCall(agent("u1"), "update_calendar_event", seriesArgs(map[string]any{"scope": "all"}))
	evs, _, _ := f.rec.List(context.Background(), audit.Filter{Name: audit.AgentWriteName, Limit: 10})
	if len(evs) != 1 {
		t.Fatalf("agent.write events: %d", len(evs))
	}
}

func asArg(err error, target **ArgError) bool {
	ae, ok := err.(*ArgError)
	*target = ae
	return ok
}
