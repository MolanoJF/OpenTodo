package assistant

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/OpenTodoApp/opentodo/internal/agenttools"
	"github.com/OpenTodoApp/opentodo/internal/freetime"
)

// calendar-event-guests task 6.4: the in-app assistant never sees an event's guests or
// join link, because it is offered none of the calendar event tools and the only
// calendar data it reads (free time) carries no guest member.

func TestEveryCalendarEventToolIsKeptFromTheApp(t *testing.T) {
	f := newFixture(t)
	seen := 0
	for _, d := range f.loop.Tools.Definitions(Caller("u1")) {
		if !strings.HasSuffix(d.Name, "_calendar_event") && !strings.HasSuffix(d.Name, "_calendar_events") {
			continue
		}
		seen++
		if !notInApp[d.Name] {
			t.Errorf("%s is offered to the in-app assistant", d.Name)
		}
	}
	// The registry offers them to an agent token only with writes on, so list them there too.
	agent := agenttools.Caller{UserID: "u1", TokenID: "t", Scope: agenttools.ScopeAgent, OwnerAllowsWrites: true, UserAllowsWrites: true}
	for _, d := range f.loop.Tools.Definitions(agent) {
		if strings.Contains(d.Name, "calendar_event") {
			seen++
			if !notInApp[d.Name] {
				t.Errorf("%s is offered to the in-app assistant", d.Name)
			}
		}
	}
	if seen < 5 || !notInApp["respond_to_calendar_event"] {
		t.Fatalf("calendar event tools seen: %d", seen)
	}
	// A call to one of them is refused as an unknown tool, with nothing planned.
	draft := agenttools.ChangeSet{}
	out, isErr := f.loop.runTool(context.Background(), Caller("u1"), nil, ToolCall{Name: "respond_to_calendar_event", Args: json.RawMessage(`{}`)}, &draft, nil)
	if !isErr || !strings.Contains(out, "unknown_tool") {
		t.Fatalf("runTool = %v %s", isErr, out)
	}
}

func TestInAppCalendarDataCarriesNoGuests(t *testing.T) {
	typ := reflect.TypeOf(freetime.Event{})
	for i := 0; i < typ.NumField(); i++ {
		name := strings.ToLower(typ.Field(i).Name)
		if strings.Contains(name, "guest") || strings.Contains(name, "conference") || strings.Contains(name, "attendee") {
			t.Fatalf("freetime.Event has %s: the in-app assistant would receive it", typ.Field(i).Name)
		}
	}
	f := newFixture(t)
	for _, d := range f.loop.Tools.Definitions(Caller("u1")) {
		if notInApp[d.Name] {
			continue
		}
		raw, _ := json.Marshal(d.InputSchema)
		if strings.Contains(string(raw), `"guests":{`) || strings.Contains(string(raw), `"conference":{`) {
			t.Errorf("%s takes guests or a conference in the app", d.Name)
		}
	}
}
