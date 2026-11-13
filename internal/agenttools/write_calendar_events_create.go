package agenttools

import (
	"context"
	"crypto/rand"
	"fmt"
)

// create_calendar_event (OpenSpec change calendar-event-editor, design D13):
// one new event in a writable calendar of a connected account. Like the
// update tool it is not a change set: planning validates and hands Call an
// exec that goes through the EventEditor, after the write gate, the per-token
// limits and the agent.write audit. The editor records calendar_event.changed
// (created) and wakes the user's devices.

func init() {
	register(tool{def: func(Caller) Definition {
		return writeDef("create_calendar_event", "Create a calendar event",
			"Add one event to a connected calendar. Choose the calendar from the calendars array of list_calendar_events (connection_id and calendar_url, can_create true). "+
				"start and end are {date, time, tz}; an all-day event takes dates only, its end being the day after its last day. Optional: location, description (plain text), free (true: the user shows as free), repeat (makes a repeating event) and reminders. A new event takes no scope. "+
				"It works only when the user turned on editing events for that connection; no invitation is ever sent. "+
				"client_id is an optional key of 8 to 64 letters, digits, - or _: reuse it when you retry, and the event is made once. The answer holds the event with its id and rev.",
			object([]string{"connection_id", "calendar_url", "title", "start", "end"}, map[string]any{
				"connection_id": map[string]any{"type": "string", "description": "From the calendars array of list_calendar_events."},
				"calendar_url":  map[string]any{"type": "string", "description": "From the calendars array of list_calendar_events."},
				"title":         map[string]any{"type": "string", "minLength": 1, "maxLength": 500},
				"start":         eventPointProp("The start"), "end": eventPointProp("The end"),
				"all_day":     map[string]any{"type": "boolean", "description": "true for an all-day event; default: all-day when start and end have no time."},
				"location":    map[string]any{"type": "string", "maxLength": 500},
				"description": map[string]any{"type": "string", "maxLength": 10000, "description": "Plain text."},
				"free":        map[string]any{"type": "boolean", "description": "true: shown as free; default busy."},
				"client_id":   map[string]any{"type": "string", "minLength": 8, "maxLength": 64, "description": "Idempotency key; reuse it when retrying."},
				"repeat":      repeatProp(false), "reminders": remindersProp,
			}), false, true, nil, false)
	}, plan: (*Registry).planCreateCalendarEvent})
}

// eventCreate reads create_calendar_event's arguments.
func (a args) eventCreate() (EventCreate, error) {
	var c EventCreate
	if err := a.only("", "connection_id", "calendar_url", "title", "start", "end", "all_day", "location", "description", "free", "client_id", "repeat", "reminders"); err != nil {
		return c, err
	}
	var err error
	if c.ConnectionID, _, err = a.text("connection_id", true, 200); err != nil {
		return c, err
	}
	if c.CalendarURL, _, err = a.text("calendar_url", true, 2048); err != nil {
		return c, err
	}
	if c.Summary, _, err = a.text("title", true, 500); err != nil {
		return c, err
	}
	for _, f := range []struct {
		name string
		to   *EventTime
	}{{"start", &c.Start}, {"end", &c.End}} {
		p, err := a.eventTime(f.name)
		if err != nil {
			return c, err
		}
		if p == nil {
			return c, argErr(f.name + ": is required")
		}
		*f.to = *p
	}
	if a.present("all_day") {
		v, err := a.boolean("all_day")
		if err != nil {
			return c, err
		}
		c.AllDay = &v
	}
	if c.Location, _, err = a.text("location", false, 500); err != nil {
		return c, err
	}
	if c.Description, _, err = a.text("description", false, 10000); err != nil {
		return c, err
	}
	if c.Free, err = a.boolean("free"); err != nil {
		return c, err
	}
	if c.ClientID, _, err = a.text("client_id", false, 64); err != nil {
		return c, err
	}
	if c.ClientID == "" {
		c.ClientID = newClientID()
	}
	var set bool
	if c.Repeat, set, err = a.eventRepeat(); err != nil {
		return c, err
	}
	_ = set // null means no repeat
	if r, err := a.eventReminders(); err != nil {
		return c, err
	} else if r != nil {
		c.Reminders = *r
	}
	return c, nil
}

// newClientID is a random UUID, used when the agent sends no client_id.
func newClientID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6], b[8] = b[6]&0x0f|0x40, b[8]&0x3f|0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

func (r *Registry) planCreateCalendarEvent(_ context.Context, c Caller, a args, _ ChangeSet) (Plan, error) {
	if r.eventEditor == nil {
		return Plan{}, calendarEventsOff()
	}
	ev, err := a.eventCreate()
	if err != nil {
		return Plan{}, err
	}
	if err := r.eventEditor.ValidateCreate(ev); err != nil {
		return Plan{}, err
	}
	return Plan{Result: map[string]any{}, Count: 1, execWrites: true, exec: func(ctx context.Context, maxWrites int) (map[string]any, int, error) {
		if maxWrites < 1 {
			return nil, 0, rateLimited(0, "no changed items left today for this token")
		}
		res, err := r.eventEditor.Create(ctx, c.UserID, ev)
		if err != nil {
			return nil, 0, eventToolError(err)
		}
		n := 1
		if res.Replayed {
			n = 0 // a repeat of a client id writes nothing
		}
		return map[string]any{"event": outEvent(res.Event), "client_id": ev.ClientID, "replayed": res.Replayed}, n, nil
	}}, nil
}
