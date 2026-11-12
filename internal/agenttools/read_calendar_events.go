package agenttools

import (
	"context"
	"fmt"
	"time"
)

// list_calendar_events (OpenSpec change calendar-event-edits, design D8): the
// events of the user's calendar days with the id, rev and access an edit
// needs. Titles are text from a remote calendar, so they travel in
// untrusted_text. Calendar events are server data the server already holds in
// the clear, so the tool works for an encrypted account too (design D9).

// MaxEventDays is the longest range list_calendar_events reads at once, as
// GET /api/v1/calendar-feeds/events.
const MaxEventDays = 92

type eventText struct {
	Title    string `json:"title"`
	Location string `json:"location,omitempty"`
}

type eventOut struct {
	CalendarEvent
	Untrusted eventText `json:"untrusted_text"`
}

func outEvent(e CalendarEvent) eventOut {
	return eventOut{CalendarEvent: e, Untrusted: eventText{Title: e.Title, Location: e.Location}}
}

func init() {
	register(tool{
		def: readDef("list_calendar_events", "List calendar events",
			"List the user's calendar events from..to (dates YYYY-MM-DD, at most 92 days, both inclusive): the events of their connected calendars and calendar feeds, as the calendar view shows them. "+
				"Each event has an id, start and end ({date, time, tz}; all-day events have a date only, the end exclusive) and, for a connected calendar's event, connection_id, rev and access ({update, delete, reason}). "+
				"To change one, pass connection_id, the id and the rev to update_calendar_event or delete_calendar_event. An event with access.update false cannot be changed, and the reason says why.",
			object([]string{"from", "to"}, map[string]any{
				"from": map[string]any{"type": "string", "format": "date", "description": "First day (YYYY-MM-DD)."},
				"to":   map[string]any{"type": "string", "format": "date", "description": "Last day (YYYY-MM-DD), not before from, at most 91 days after it."},
			})),
		read: (*Registry).listCalendarEvents,
	})
}

func (r *Registry) listCalendarEvents(ctx context.Context, userID string, a args) (map[string]any, error) {
	if r.eventEditor == nil {
		return nil, calendarEventsOff()
	}
	if err := a.only("", "from", "to"); err != nil {
		return nil, err
	}
	from, err := a.date("from")
	if err != nil {
		return nil, err
	}
	to, err := a.date("to")
	if err != nil {
		return nil, err
	}
	if from == "" || to == "" {
		return nil, argErr("from and to: are required dates in YYYY-MM-DD form")
	}
	f, _ := time.Parse(time.DateOnly, from)
	t, _ := time.Parse(time.DateOnly, to)
	if t.Before(f) {
		return nil, argErr("to: must not be before from")
	}
	if int(t.Sub(f).Hours()/24)+1 > MaxEventDays {
		return nil, argErr(fmt.Sprintf("to: at most %d days at a time", MaxEventDays))
	}
	events, err := r.eventEditor.Events(ctx, userID, from, to)
	if err != nil {
		return nil, err
	}
	truncated := len(events) > MaxItems
	if truncated {
		events = events[:MaxItems]
	}
	out := make([]eventOut, len(events))
	for i, e := range events {
		out[i] = outEvent(e)
	}
	return map[string]any{"events": out, "from": from, "to": to, "truncated": truncated, "server_time": r.serverTime()}, nil
}
