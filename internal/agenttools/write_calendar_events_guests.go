package agenttools

import (
	"bytes"
	"context"
	"encoding/json"
)

// The guests, notify_guests and online_meeting arguments of the calendar event tools and
// respond_to_calendar_event (OpenSpec change calendar-event-guests, design D14). The
// server decides what a provider can do and whether a change needs the confirmation
// (the routes' own checks); the tools only read, bound and pass the arguments on.

const notifyGuestsText = "notify_guests must be true when the event's access.notifies is true: the calendar then sends the guests an update itself and it cannot be turned off. " +
	"Never set it without the user having asked you to change the event they organise. " +
	"It is also needed to invite guests, which tells them. A call without it that would tell guests is refused with invalid_input and nothing is written."

const guestsText = "People to invite: {email, name?, optional?} each (at most 50 per call). Only a calendar that can tell guests takes them, and inviting needs notify_guests true. " +
	"A room cannot be added or removed here."

const onlineMeetingText = "true makes the event an online meeting with a join link, on a calendar that offers one. An online meeting cannot be removed once made, so false is refused."

var notifyGuestsProp = map[string]any{"type": "boolean", "description": notifyGuestsText}

var onlineMeetingProp = map[string]any{"type": "boolean", "description": onlineMeetingText}

func guestItemProp() map[string]any {
	return map[string]any{"type": "object", "additionalProperties": false, "required": []string{"email"}, "properties": map[string]any{
		"email":    map[string]any{"type": "string", "maxLength": 254, "description": "A bare address: local@domain."},
		"name":     map[string]any{"type": "string", "maxLength": 120},
		"optional": map[string]any{"type": "boolean", "description": "true: an optional attendee."},
	}}
}

// guestsAddProp is create_calendar_event's guests: an array of guests.
func guestsAddProp() map[string]any {
	return map[string]any{"type": "array", "maxItems": 50, "items": guestItemProp(), "description": guestsText}
}

// guestsChangeProp is update_calendar_event's guests: what to add and remove.
func guestsChangeProp() map[string]any {
	return map[string]any{"type": "object", "additionalProperties": false, "description": guestsText + " remove lists addresses of guests to take off.",
		"properties": map[string]any{
			"add":    map[string]any{"type": "array", "maxItems": 50, "items": guestItemProp()},
			"remove": map[string]any{"type": "array", "maxItems": 50, "items": map[string]any{"type": "string", "maxLength": 254}},
		}}
}

// maxToolGuests bounds the lists before the route's own checks.
const maxToolGuests = 50

func decodeStrict(raw json.RawMessage, to any) bool {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	return dec.Decode(to) == nil
}

// guestList reads the guests of a create.
func (a args) guestList() ([]EventGuest, error) {
	if !a.present("guests") {
		return nil, nil
	}
	var list []EventGuest
	if !decodeStrict(a["guests"], &list) || len(list) > maxToolGuests {
		return nil, argErr(`guests: must be an array of at most 50 {"email": "ana@example.com", "name"?: "...", "optional"?: true}`)
	}
	return list, nil
}

// guestsChange reads the guests of an update.
func (a args) guestsChange() (*EventGuestsChange, error) {
	if !a.present("guests") {
		return nil, nil
	}
	var in struct {
		Add    []EventGuest `json:"add"`
		Remove []string     `json:"remove"`
	}
	if !decodeStrict(a["guests"], &in) || len(in.Add) > maxToolGuests || len(in.Remove) > maxToolGuests {
		return nil, argErr(`guests: must be {"add"?: [{"email": "ana@example.com", "name"?: "...", "optional"?: true}], "remove"?: ["ben@example.com"]}, at most 50 of each`)
	}
	return &EventGuestsChange{Add: in.Add, Remove: in.Remove}, nil
}

// optBool reads an optional boolean: nil when absent.
func (a args) optBool(name string) (*bool, error) {
	if !a.present(name) {
		return nil, nil
	}
	v, err := a.boolean(name)
	if err != nil {
		return nil, err
	}
	return &v, nil
}

// withGuests reads guests, notify_guests and online_meeting of an update.
func (u EventUpdate) withGuests(a args) (EventUpdate, error) {
	var err error
	if u.Guests, err = a.guestsChange(); err != nil {
		return u, err
	}
	if u.NotifyGuests, err = a.optBool("notify_guests"); err != nil {
		return u, err
	}
	u.OnlineMeeting, err = a.optBool("online_meeting")
	return u, err
}

// withGuests reads guests, notify_guests and online_meeting of a create.
func (c EventCreate) withGuests(a args) (EventCreate, error) {
	var err error
	if c.Guests, err = a.guestList(); err != nil {
		return c, err
	}
	if c.NotifyGuests, err = a.optBool("notify_guests"); err != nil {
		return c, err
	}
	c.OnlineMeeting, err = a.optBool("online_meeting")
	return c, err
}

// respond_to_calendar_event answers an invitation. Like the other calendar event writes
// it is not a change set: planning validates and hands Call an exec that goes through the
// EventEditor, after the write gate, the per-token limits and the agent.write audit.
func init() {
	register(tool{def: func(Caller) Definition {
		return writeDef("respond_to_calendar_event", "Answer a calendar invitation",
			"Answer an invitation the user received: accepted, tentative or declined (see list_calendar_events: guests.can_respond is true, and the event has connection_id, an id and a rev). "+
				"The calendar tells the organiser the user's answer; send_response false keeps it from doing so only on a calendar that allows that, and is refused otherwise. "+
				"It works only when the user turned on editing events for that connection, and it changes nothing else about the event. "+
				"For an event with a series pass scope: this answers that occurrence (Microsoft calendars), all the whole series. A user who organises the event, or is not invited, cannot answer it (event_not_editable, reason cannot_respond). "+
				"The answer holds the event as the calendar now holds it.",
			object([]string{"connection_id", "event_id", "rev", "response"}, map[string]any{
				"connection_id": connectionIDProp, "event_id": eventIDProp, "rev": eventRevProp,
				"response":      map[string]any{"type": "string", "enum": []string{"accepted", "tentative", "declined"}, "description": "The user's answer."},
				"scope":         map[string]any{"type": "string", "enum": []string{"this", "all"}, "description": "For an occurrence of a repeating event: this occurrence or all of them. Refused for a single event."},
				"send_response": map[string]any{"type": "boolean", "description": "Default true. false: do not tell the organiser; refused where the calendar always does."},
			}), false, true, nil, false)
	}, plan: (*Registry).planRespondCalendarEvent})
}

// eventReply reads respond_to_calendar_event's arguments.
func (a args) eventReply() (EventReply, error) {
	var p EventReply
	if err := a.only("", "connection_id", "event_id", "rev", "response", "scope", "send_response"); err != nil {
		return p, err
	}
	var err error
	if p.ConnectionID, p.EventID, p.Rev, err = a.eventTarget(); err != nil {
		return p, err
	}
	if p.Response, _, err = a.text("response", true, 20); err != nil {
		return p, err
	}
	if p.Scope, _, err = a.text("scope", false, 20); err != nil {
		return p, err
	}
	p.SendResponse, err = a.optBool("send_response")
	return p, err
}

func (r *Registry) planRespondCalendarEvent(_ context.Context, c Caller, a args, _ ChangeSet) (Plan, error) {
	if r.eventEditor == nil {
		return Plan{}, calendarEventsOff()
	}
	p, err := a.eventReply()
	if err != nil {
		return Plan{}, err
	}
	if err := r.eventEditor.ValidateRespond(p); err != nil {
		return Plan{}, err
	}
	return Plan{Result: map[string]any{}, Count: 1, execWrites: true, exec: func(ctx context.Context, maxWrites int) (map[string]any, int, error) {
		if maxWrites < 1 {
			return nil, 0, rateLimited(0, "no changed items left today for this token")
		}
		res, err := r.eventEditor.Respond(ctx, c.UserID, p)
		if err != nil {
			return nil, 0, eventToolError(err)
		}
		return map[string]any{"event": outEvent(res.Event), "replaces": res.Replaces}, 1, nil
	}}, nil
}
