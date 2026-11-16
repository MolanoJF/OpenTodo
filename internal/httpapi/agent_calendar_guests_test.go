package httpapi

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/OpenTodoApp/opentodo/internal/agenttools"
	"github.com/OpenTodoApp/opentodo/internal/audit"
)

// The agent tools' EventEditor with guests (OpenSpec change calendar-event-guests, task
// 6.2) over a Microsoft account: the notify rule through the tools' own words, a reply, a
// notified delete, the cache-only guest list and no address outside it.

func agentGuestEnv(t *testing.T) (*msGuestsEnv, calendarEventEditor, string) {
	t.Helper()
	v := newMSGuestsEnv(t)
	var uid string
	if err := v.e.db.QueryRow(`SELECT id FROM users LIMIT 1`).Scan(&uid); err != nil {
		t.Fatal(err)
	}
	return v, calendarEventEditor{v.srv}, uid
}

func agentRows(t *testing.T, v *msGuestsEnv, ed calendarEventEditor, uid string) map[string]agenttools.CalendarEvent {
	t.Helper()
	to := v.day
	rows, err := ed.Events(context.Background(), uid, v.day, to)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]agenttools.CalendarEvent{}
	for _, r := range rows {
		out[r.Title] = r
	}
	return out
}

func TestAgentEditorShowsGuestsAsSummaries(t *testing.T) {
	v, ed, uid := agentGuestEnv(t)
	rows := agentRows(t, v, ed, uid)
	mine, inv, alone := rows["Mine"], rows["Invited"], rows["Alone"]
	if mine.Guests == nil || mine.Guests.Count != 3 || !mine.Guests.Organizer || mine.Guests.CanRespond || mine.Access == nil || !mine.Access.Notifies {
		t.Fatalf("mine = %+v access %+v", mine.Guests, mine.Access)
	}
	if mine.Conference == nil || mine.Conference.Provider != "teams" {
		t.Fatalf("conference = %+v", mine.Conference)
	}
	if inv.Guests == nil || inv.Guests.You != "needs-action" || !inv.Guests.CanRespond {
		t.Fatalf("invited = %+v", inv.Guests)
	}
	if alone.Guests != nil || alone.Conference != nil {
		t.Fatalf("alone = %+v", alone)
	}
	lists, err := ed.GuestLists(context.Background(), uid, []string{mine.ID, alone.ID})
	if err != nil {
		t.Fatal(err)
	}
	l, ok := lists[mine.ID]
	if !ok || len(l.Guests) == 0 || !strings.HasPrefix(l.ConferenceURL, "https://teams.microsoft.com/") {
		t.Fatalf("list = %+v", l)
	}
	if _, ok := lists[alone.ID]; ok {
		t.Fatal("an event without guests has a list")
	}
}

func TestAgentEditorNotifyRule(t *testing.T) {
	v, ed, uid := agentGuestEnv(t)
	ctx, mine := context.Background(), agentRows(t, v, ed, uid)["Mine"]
	title := "Renamed"
	u := agenttools.EventUpdate{ConnectionID: v.id, EventID: mine.ID, Rev: mine.Rev, Summary: &title}
	_, err := ed.Update(ctx, uid, u)
	var te *agenttools.ToolError
	if !errors.As(err, &te) || te.Code != "invalid_input" || !strings.Contains(te.Message, "notify_guests") || !strings.Contains(te.Message, "cannot be turned off") {
		t.Fatalf("update without notify_guests = %v", err)
	}
	off := false
	u.NotifyGuests = &off
	if _, err := ed.Update(ctx, uid, u); err == nil {
		t.Fatal("notify_guests false was accepted")
	}
	if err := ed.Delete(ctx, uid, agenttools.EventDelete{ConnectionID: v.id, EventID: mine.ID, Rev: mine.Rev}); !errors.As(err, &te) || te.Code != "invalid_input" {
		t.Fatalf("delete without notify_guests = %v", err)
	}
	if v.writes() != 0 {
		t.Fatalf("%d writes without a confirmation", v.writes())
	}
	on := true
	u.NotifyGuests = &on
	out, err := ed.Update(ctx, uid, u)
	if err != nil || out.Event.Title != "Renamed" {
		t.Fatalf("confirmed update = %+v %v", out, err)
	}
	if e := auditWith(v.e, "updated"); e == nil || e.Details["event_notified"] != "true" {
		t.Fatalf("audit = %+v", e)
	}
	if err := ed.Delete(ctx, uid, agenttools.EventDelete{ConnectionID: v.id, EventID: out.Event.ID, Rev: out.Event.Rev, NotifyGuests: true}); err != nil {
		t.Fatalf("confirmed delete = %v", err)
	}
	if v.ms.Count("DELETE") != 1 {
		t.Fatalf("deletes = %d", v.ms.Count("DELETE"))
	}
}

func TestAgentEditorGuestsAndOnlineMeeting(t *testing.T) {
	v, ed, uid := agentGuestEnv(t)
	ctx := context.Background()
	alone := agentRows(t, v, ed, uid)["Alone"]
	yes := true
	// Inviting a first guest tells them: it needs the confirmation, and names the tool's field.
	u := agenttools.EventUpdate{ConnectionID: v.id, EventID: alone.ID, Rev: alone.Rev,
		Guests: &agenttools.EventGuestsChange{Add: []agenttools.EventGuest{{Email: "new@example.com", Name: "New", Optional: true}}}}
	if _, err := ed.Update(ctx, uid, u); err == nil || !strings.Contains(err.Error(), "notify_guests") {
		t.Fatalf("invite without notify_guests = %v", err)
	}
	if err := ed.Validate(agenttools.EventUpdate{Rev: "r", Guests: &agenttools.EventGuestsChange{Add: []agenttools.EventGuest{{Email: "x"}}}}); err == nil || !strings.Contains(err.Error(), "guests.add[0].email") {
		t.Fatalf("a bad address = %v", err)
	}
	u.NotifyGuests = &yes
	out, err := ed.Update(ctx, uid, u)
	if err != nil || out.Event.Guests == nil || out.Event.Guests.Count != 1 {
		t.Fatalf("invite = %+v %v", out, err)
	}
	// A create with guests and an online meeting.
	if err := ed.ValidateCreate(agenttools.EventCreate{Guests: []agenttools.EventGuest{{Email: "x"}}}); err == nil {
		t.Fatal("a create with a bad address validated")
	}
	no := false
	if _, err := ed.Update(ctx, uid, agenttools.EventUpdate{ConnectionID: v.id, EventID: out.Event.ID, Rev: out.Event.Rev, OnlineMeeting: &no}); err == nil {
		t.Fatal("online_meeting false was accepted")
	}
}

func TestAgentEditorRespond(t *testing.T) {
	v, ed, uid := agentGuestEnv(t)
	ctx, rows := context.Background(), agentRows(t, v, ed, uid)
	inv, mine := rows["Invited"], rows["Mine"]
	r := agenttools.EventReply{ConnectionID: v.id, EventID: inv.ID, Rev: inv.Rev, Response: "tentative"}
	if err := ed.ValidateRespond(agenttools.EventReply{Rev: "r", Response: "yes"}); err == nil || !strings.HasPrefix(err.Error(), "response") {
		t.Fatalf("bad answer = %v", err)
	}
	if err := ed.ValidateRespond(agenttools.EventReply{Rev: "r", Response: "accepted", Scope: "following"}); err == nil {
		t.Fatal("following validated")
	}
	if err := ed.ValidateRespond(r); err != nil {
		t.Fatal(err)
	}
	// The user organises this one: cannot_respond, nothing sent.
	var te *agenttools.ToolError
	if _, err := ed.Respond(ctx, uid, agenttools.EventReply{ConnectionID: v.id, EventID: mine.ID, Rev: mine.Rev, Response: "accepted"}); !errors.As(err, &te) || te.Code != "event_not_editable" || !strings.HasSuffix(te.Message, "cannot_respond") {
		t.Fatalf("organiser = %v", err)
	}
	out, err := ed.Respond(ctx, uid, r)
	if err != nil || out.Event.Guests == nil || out.Event.Guests.You != "tentative" {
		t.Fatalf("respond = %+v %v", out, err)
	}
	if e := auditWith(v.e, "responded"); e == nil || e.Details["event_response"] != "tentative" {
		t.Fatalf("audit = %+v", e)
	}
	if _, err := ed.Respond(ctx, uid, agenttools.EventReply{ConnectionID: v.id, EventID: inv.ID, Rev: "0000000000000000", Response: "accepted"}); !errors.As(err, &te) || te.Code != "event_changed" {
		t.Fatalf("stale rev = %v", err)
	}
}

// auditWith is the calendar_event.changed entry with that action, nil when there is none.
func auditWith(e *env, action string) *audit.Entry {
	for _, x := range e.events(audit.CalendarEventChangedName) {
		if x.Details["event_action"] == action {
			return &x
		}
	}
	return nil
}
