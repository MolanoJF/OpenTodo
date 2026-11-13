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
	// Organizer, Guests and ConferenceUrl are someone else's names, addresses and a link:
	// they are here, and only with include_guests (calendar-event-guests D14).
	Organizer     *EventPerson  `json:"organizer,omitempty"`
	Guests        []EventPerson `json:"guests,omitempty"`
	ConferenceURL string        `json:"conference_url,omitempty"`
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
				"calendars lists the writable calendars (connection_id, calendar_url, name, can_create) that create_calendar_event can use; can_create is false while the user has editing events turned off for that connection. "+
				"To change one, pass connection_id, the id and the rev to update_calendar_event or delete_calendar_event. An event with access.update false cannot be changed, and the reason says why. "+
				"An event with guests or a join link has guests ({count, you (the user's own answer), organizer (true: the user organises it), can_respond}) and conference ({provider}), with no name or address: pass include_guests true to get the organiser, the guests and the join link, inside untrusted_text. access.notifies true means that changing or deleting the event makes the calendar tell its guests, so update_calendar_event and delete_calendar_event then need notify_guests true. respond_to_calendar_event answers an invitation (guests.can_respond true). "+
				"An event with a series ({id, repeats, first, custom, repeat}) is an occurrence of a repeating event: changing or deleting it needs scope (this, following or all) on update_calendar_event and delete_calendar_event; custom means it repeats in a way repeat cannot show.",
			object([]string{"from", "to"}, map[string]any{
				"from":           map[string]any{"type": "string", "format": "date", "description": "First day (YYYY-MM-DD)."},
				"to":             map[string]any{"type": "string", "format": "date", "description": "Last day (YYYY-MM-DD), not before from, at most 91 days after it."},
				"include_guests": map[string]any{"type": "boolean", "description": "true: add each event's organiser, guests (at most 50) and join link, inside untrusted_text. Default false."},
			})),
		read: (*Registry).listCalendarEvents,
	})
}

func (r *Registry) listCalendarEvents(ctx context.Context, userID string, a args) (map[string]any, error) {
	if r.eventEditor == nil {
		return nil, calendarEventsOff()
	}
	if err := a.only("", "from", "to", "include_guests"); err != nil {
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
	include, err := a.boolean("include_guests")
	if err != nil {
		return nil, err
	}
	events, err := r.eventEditor.Events(ctx, userID, from, to)
	if err != nil {
		return nil, err
	}
	truncated := len(events) > MaxItems
	if truncated {
		events = events[:MaxItems]
	}
	out, err := r.eventRows(ctx, userID, events, include)
	if err != nil {
		return nil, err
	}
	cals, err := r.eventEditor.Calendars(ctx, userID)
	if err != nil {
		return nil, err
	}
	if cals == nil {
		cals = []EventCalendar{}
	}
	return map[string]any{"events": out, "calendars": cals, "from": from, "to": to, "truncated": truncated, "server_time": r.serverTime()}, nil
}

// eventRows turns events into rows; with include, the people and join link of those that
// have any are read from the cache and put inside untrusted_text.
func (r *Registry) eventRows(ctx context.Context, userID string, events []CalendarEvent, include bool) ([]eventOut, error) {
	out := make([]eventOut, len(events))
	for i, e := range events {
		out[i] = outEvent(e)
	}
	if !include {
		return out, nil
	}
	var ids []string
	for _, e := range events {
		if e.Guests != nil || e.Conference != nil {
			ids = append(ids, e.ID)
		}
	}
	if len(ids) == 0 {
		return out, nil
	}
	lists, err := r.eventEditor.GuestLists(ctx, userID, ids)
	if err != nil {
		return nil, err
	}
	for i, e := range events {
		l, ok := lists[e.ID]
		if !ok {
			continue
		}
		if len(l.Guests) > MaxEventGuestsListed {
			l.Guests = l.Guests[:MaxEventGuestsListed]
		}
		out[i].Untrusted.Organizer, out[i].Untrusted.Guests, out[i].Untrusted.ConferenceURL = l.Organizer, l.Guests, l.ConferenceURL
	}
	return out, nil
}

// MaxEventGuestsListed is the most guests list_calendar_events shows per event.
const MaxEventGuestsListed = 50
