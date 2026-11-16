package httpapi

import (
	"context"
	"strings"

	"github.com/OpenTodoApp/opentodo/internal/agenttools"
	"github.com/OpenTodoApp/opentodo/internal/audit"
	"github.com/OpenTodoApp/opentodo/internal/calfeed"
	"github.com/OpenTodoApp/opentodo/internal/calsync"
)

// The guest, reply and join-link half of the agent tools' EventEditor (OpenSpec change
// calendar-event-guests, design D14): respond_to_calendar_event, the guests,
// notify_guests and online_meeting arguments, and the cache-only guest lists
// list_calendar_events shows inside untrusted text. They go through the same
// calsync.Worker functions and body rules as the routes.

// guestInputsOf is a tool's guests as the route's body form.
func guestInputsOf(list []agenttools.EventGuest) []guestInput {
	if len(list) == 0 {
		return nil
	}
	out := make([]guestInput, len(list))
	for i, g := range list {
		out[i] = guestInput{Email: g.Email, Name: g.Name}
		if g.Optional {
			out[i].Role = calfeed.RoleOptional
		}
	}
	return out
}

// guestsPatchOf is an update's guests as the route's body form.
func guestsPatchOf(g *agenttools.EventGuestsChange) *guestsPatch {
	if g == nil {
		return nil
	}
	return &guestsPatch{Add: guestInputsOf(g.Add), Remove: g.Remove}
}

// toolField names a route body field as the tools call it.
func toolField(field string) string {
	switch field {
	case "notifyGuests":
		return "notify_guests"
	case "onlineMeeting":
		return "online_meeting"
	case "sendResponse":
		return "send_response"
	}
	return field
}

// toolFieldMessage is "field: message" with the field named as the tools call it.
func toolFieldMessage(field, msg string) string {
	return strings.TrimSpace(toolField(field) + ": " + msg)
}

// toolInvalid is the invalid_input of a refused field, worded for the tools.
func toolInvalid(field string) error {
	switch field {
	case "notifyGuests":
		return toolCode("invalid_input", "notify_guests: this change tells the guests; the calendar sends the message itself and it cannot be turned off. Ask the user, and send notify_guests true only if they want the guests told")
	case "sendResponse":
		return toolCode("invalid_input", "send_response: this calendar always tells the organiser; send_response false is not available")
	case "onlineMeeting":
		return toolCode("invalid_input", "online_meeting: an online meeting can only be made, on a calendar that offers one, and can not be removed")
	}
	return toolCode("invalid_input", toolField(field)+": "+invalidReason(field))
}

// replyOfTool validates a tool's answer with the route's body rules.
func replyOfTool(r agenttools.EventReply) (calsync.ReplyEdit, string, string) {
	in := eventResponse{Rev: r.Rev, Response: r.Response, SendResponse: r.SendResponse}
	if r.Scope != "" {
		in.Scope = &r.Scope
	}
	return in.replyOf()
}

// ValidateRespond applies the response route's body rules.
func (calendarEventEditor) ValidateRespond(r agenttools.EventReply) error {
	if _, field, msg := replyOfTool(r); field != "" {
		return &agenttools.ArgError{Msg: toolFieldMessage(field, msg)}
	}
	return nil
}

// Respond answers an invitation through the same Worker.RespondEvent as the route.
func (e calendarEventEditor) Respond(ctx context.Context, uid string, r agenttools.EventReply) (agenttools.EventOutcome, error) {
	if err := e.on(ctx); err != nil {
		return agenttools.EventOutcome{}, err
	}
	reply, field, msg := replyOfTool(r)
	if field != "" {
		return agenttools.EventOutcome{}, &agenttools.ArgError{Msg: toolFieldMessage(field, msg)}
	}
	if !e.s.calsyncEventLimiter.allow(uid) {
		return agenttools.EventOutcome{}, toolCode("rate_limited", "too many calendar event changes; try again later")
	}
	res, err := e.s.calsync.RespondEvent(ctx, uid, r.ConnectionID, r.EventID, r.Rev, reply)
	if err != nil {
		return agenttools.EventOutcome{}, e.toolError(err)
	}
	e.s.audit.Record(ctx, audit.CalendarEventResponded(uid, r.ConnectionID, res.Provider, string(reply.Response), string(reply.Scope), reply.SendResponse))
	return agenttools.EventOutcome{Event: eventOfRow(res.Event, res.ConnectionID), Replaces: res.Replaces}, nil
}

func personOf(g calfeed.Guest) agenttools.EventPerson {
	return agenttools.EventPerson{Name: g.Name, Email: g.Email, Role: g.Role, Response: g.Response, You: g.You}
}

// GuestLists reads the people and join link of events from the cache, with no request to
// the calendar.
func (e calendarEventEditor) GuestLists(ctx context.Context, uid string, ids []string) (map[string]agenttools.EventGuestList, error) {
	out := map[string]agenttools.EventGuestList{}
	if e.s.calWorker == nil {
		return out, nil
	}
	for _, id := range ids {
		row, ok, err := e.s.calWorker.Store.GuestsOf(ctx, uid, id)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		var l agenttools.EventGuestList
		if o := row.Info.Organizer; o != nil {
			p := personOf(*o)
			l.Organizer = &p
		}
		for _, g := range row.Info.Guests {
			l.Guests = append(l.Guests, personOf(g))
		}
		if row.Conference != nil {
			l.ConferenceURL = row.Conference.URL
		}
		out[id] = l
	}
	return out, nil
}

// withGuestSummary adds the guest count and the join provider of a row to a tool's event.
func withGuestSummary(e *agenttools.CalendarEvent, row calfeed.EventRow) {
	if g := row.Guests; g != nil {
		e.Guests = &agenttools.EventGuestSummary{Count: g.Count, You: g.You, Organizer: g.Organizer, CanRespond: g.CanRespond}
	}
	if c := row.Conference; c != nil {
		e.Conference = &agenttools.EventConference{Provider: c.Provider}
	}
	if row.Access != nil && e.Access != nil {
		e.Access.Notifies = row.Access.Notifies
	}
}
