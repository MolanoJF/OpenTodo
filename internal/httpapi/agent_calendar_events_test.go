package httpapi

import (
	"context"
	"errors"
	"testing"

	"github.com/OpenTodoApp/opentodo/internal/agenttools"
	"github.com/OpenTodoApp/opentodo/internal/audit"
)

// The agent tools' EventEditor over a real worker (OpenSpec change
// calendar-event-editor, task 6.1): create, the replay of a client id, the
// stale rev, the writable calendars and the audit event.

func agentCreate(v *eventsEnv, clientID string) agenttools.EventCreate {
	return agenttools.EventCreate{ConnectionID: v.connID, CalendarURL: v.calURL, ClientID: clientID, Summary: "Call Ana", Location: "Cafe",
		Start: agenttools.EventTime{Date: v.day, Time: ptr("14:00"), TZ: ptr("Europe/Lisbon")},
		End:   agenttools.EventTime{Date: v.day, Time: ptr("14:30"), TZ: ptr("Europe/Lisbon")}}
}

func ptr(s string) *string { return &s }

func TestAgentEditorCreateAndReplay(t *testing.T) {
	v := newEventsEnv(t)
	v.edits(true)
	var uid string
	if err := v.e.db.QueryRow(`SELECT id FROM users LIMIT 1`).Scan(&uid); err != nil {
		t.Fatal(err)
	}
	ed, ctx := calendarEventEditor{v.srv}, context.Background()

	cals, err := ed.Calendars(ctx, uid)
	if err != nil || len(cals) != 1 || cals[0].ConnectionID != v.connID || cals[0].CalendarURL != v.calURL || !cals[0].CanCreate {
		t.Fatalf("calendars = %+v %v", cals, err)
	}
	if err := ed.ValidateCreate(agenttools.EventCreate{ClientID: "x"}); err == nil {
		t.Fatal("a bad create validated")
	}

	out, err := ed.Create(ctx, uid, agentCreate(v, "agent-0001"))
	if err != nil || out.Replayed || out.Event.Title != "Call Ana" || out.Event.Rev == "" {
		t.Fatalf("create = %+v %v", out, err)
	}
	puts := v.cal.Count("PUT")
	again, err := ed.Create(ctx, uid, agentCreate(v, "agent-0001"))
	if err != nil || !again.Replayed || again.Event.ID != out.Event.ID || v.cal.Count("PUT") != puts {
		t.Fatalf("replay = %+v %v", again, err)
	}
	if list := v.e.events(audit.CalendarEventChangedName); len(list) != 1 || list[0].Details["event_action"] != "created" {
		t.Fatalf("audit = %+v", list)
	}

	// A stale rev is event_changed, and the new text fields reach the calendar.
	loc, desc := "Elsewhere", "New text"
	if _, err := ed.Update(ctx, uid, agenttools.EventUpdate{ConnectionID: v.connID, EventID: out.Event.ID, Rev: "stale", Location: &loc}); !isToolCode(err, "event_changed") {
		t.Fatalf("stale rev: %v", err)
	}
	up, err := ed.Update(ctx, uid, agenttools.EventUpdate{ConnectionID: v.connID, EventID: out.Event.ID, Rev: out.Event.Rev, Location: &loc, Description: &desc})
	if err != nil || up.Event.Location != "Elsewhere" {
		t.Fatalf("update = %+v %v", up, err)
	}

	// With the switch off the calendar is listed but can not take an event.
	v.edits(false)
	if cals, _ := ed.Calendars(ctx, uid); len(cals) != 1 || cals[0].CanCreate {
		t.Fatalf("calendars with edits off = %+v", cals)
	}
	if _, err := ed.Create(ctx, uid, agentCreate(v, "agent-0002")); !isToolCode(err, "event_edits_off") {
		t.Fatalf("edits off: %v", err)
	}
}

func isToolCode(err error, code string) bool {
	var te *agenttools.ToolError
	return errors.As(err, &te) && te.Code == code
}
