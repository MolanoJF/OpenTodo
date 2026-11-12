package agenttools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
)

// update_calendar_event and delete_calendar_event (OpenSpec change
// calendar-event-edits, design D8). They are not change sets: planning
// validates the arguments and returns a Plan whose exec calls the EventEditor
// (the run_routine pattern), after the write gate, the per-token limits and
// the agent.write audit that Call applies. The editor records
// calendar_event.changed and wakes the user's devices.

var (
	connectionIDProp = map[string]any{"type": "string", "description": "The event's connection_id (see list_calendar_events)."}
	eventIDProp      = map[string]any{"type": "string", "description": "The event's id (see list_calendar_events)."}
	eventRevProp     = map[string]any{"type": "string", "description": "The event's rev from list_calendar_events. If the event changed in the calendar since, the call fails with event_changed and nothing is written: list again and redo the change."}
)

func eventPointProp(what string) map[string]any {
	return map[string]any{"type": "object", "additionalProperties": false, "required": []string{"date"},
		"description": what + " of the event: {date: YYYY-MM-DD, time: HH:MM, tz: IANA zone}. Timed events need time; tz keeps the event's own zone (omit it only for a floating event). An all-day event takes the date alone, its end being the day after the last day.",
		"properties": map[string]any{
			"date": map[string]any{"type": "string", "format": "date"},
			"time": map[string]any{"type": "string", "description": "HH:MM wall time."},
			"tz":   map[string]any{"type": "string", "description": "IANA time zone, e.g. Europe/Berlin."},
		}}
}

func init() {
	register(tool{def: func(Caller) Definition {
		return writeDef("update_calendar_event", "Change a calendar event",
			"Rename, move or resize one event of a connected calendar (see list_calendar_events for its connection_id, id and rev). Send summary, or start and end together (and all_day), or both. "+
				"It works only when the user turned on editing events for that connection and the event's access.update is true; recurring events, events with guests and events the user does not organise stay read-only. No invitation or update is ever sent to guests. "+
				"The answer holds the event as the calendar now holds it: a move changes its id, and replaces names the old one.",
			object([]string{"connection_id", "event_id", "rev"}, map[string]any{
				"connection_id": connectionIDProp, "event_id": eventIDProp, "rev": eventRevProp,
				"summary": map[string]any{"type": "string", "minLength": 1, "maxLength": 500, "description": "The new title."},
				"start":   eventPointProp("The new start"), "end": eventPointProp("The new end"),
				"all_day": map[string]any{"type": "boolean", "description": "true for an all-day event (start and end are dates); default: all-day when start and end have no time."},
			}), false, true, nil, false)
	}, plan: (*Registry).planUpdateCalendarEvent})
	register(tool{def: func(Caller) Definition {
		return writeDef("delete_calendar_event", "Delete a calendar event",
			"Delete one event from a connected calendar (see list_calendar_events for its connection_id, id and rev). It is removed from the user's calendar and can not be restored from here. It works only when the user turned on editing events for that connection and the event's access.delete is true.",
			object([]string{"connection_id", "event_id", "rev"}, map[string]any{
				"connection_id": connectionIDProp, "event_id": eventIDProp, "rev": eventRevProp,
			}), true, true, nil, false)
	}, plan: (*Registry).planDeleteCalendarEvent})
}

// eventTarget reads the three arguments every event write needs.
func (a args) eventTarget() (conn, id, rev string, err error) {
	for _, f := range []struct {
		name string
		to   *string
	}{{"connection_id", &conn}, {"event_id", &id}, {"rev", &rev}} {
		if *f.to, _, err = a.text(f.name, true, 200); err != nil {
			return
		}
	}
	return
}

// eventTime reads an optional {date, time, tz} object.
func (a args) eventTime(name string) (*EventTime, error) {
	if !a.present(name) {
		return nil, nil
	}
	dec := json.NewDecoder(bytes.NewReader(a[name]))
	dec.DisallowUnknownFields()
	var p EventTime
	if dec.Decode(&p) != nil || p.Date == "" {
		return nil, argErr(name + `: must be {"date": "YYYY-MM-DD", "time"?: "HH:MM", "tz"?: "Zone"}`)
	}
	return &p, nil
}

// eventUpdate reads update_calendar_event's arguments.
func (a args) eventUpdate() (EventUpdate, error) {
	if err := a.only("", "connection_id", "event_id", "rev", "summary", "start", "end", "all_day"); err != nil {
		return EventUpdate{}, err
	}
	conn, id, rev, err := a.eventTarget()
	if err != nil {
		return EventUpdate{}, err
	}
	u := EventUpdate{ConnectionID: conn, EventID: id, Rev: rev}
	if s, ok, err := a.text("summary", false, 500); err != nil {
		return u, err
	} else if ok {
		u.Summary = &s
	}
	if u.Start, err = a.eventTime("start"); err != nil {
		return u, err
	}
	if u.End, err = a.eventTime("end"); err != nil {
		return u, err
	}
	if a.present("all_day") {
		v, err := a.boolean("all_day")
		if err != nil {
			return u, err
		}
		u.AllDay = &v
	}
	return u, nil
}

func (r *Registry) planUpdateCalendarEvent(_ context.Context, c Caller, a args, _ ChangeSet) (Plan, error) {
	if r.eventEditor == nil {
		return Plan{}, calendarEventsOff()
	}
	u, err := a.eventUpdate()
	if err != nil {
		return Plan{}, err
	}
	if err := r.eventEditor.Validate(u); err != nil {
		return Plan{}, err
	}
	return Plan{Result: map[string]any{}, Count: 1, execWrites: true, exec: func(ctx context.Context, maxWrites int) (map[string]any, int, error) {
		if maxWrites < 1 {
			return nil, 0, rateLimited(0, "no changed items left today for this token")
		}
		res, err := r.eventEditor.Update(ctx, c.UserID, u)
		if err != nil {
			return nil, 0, eventToolError(err)
		}
		return map[string]any{"event": outEvent(res.Event), "replaces": res.Replaces}, 1, nil
	}}, nil
}

func (r *Registry) planDeleteCalendarEvent(_ context.Context, c Caller, a args, _ ChangeSet) (Plan, error) {
	if r.eventEditor == nil {
		return Plan{}, calendarEventsOff()
	}
	if err := a.only("", "connection_id", "event_id", "rev"); err != nil {
		return Plan{}, err
	}
	conn, id, rev, err := a.eventTarget()
	if err != nil {
		return Plan{}, err
	}
	return Plan{Result: map[string]any{}, Count: 1, execWrites: true, exec: func(ctx context.Context, maxWrites int) (map[string]any, int, error) {
		if maxWrites < 1 {
			return nil, 0, rateLimited(0, "no changed items left today for this token")
		}
		if err := r.eventEditor.Delete(ctx, c.UserID, conn, id, rev); err != nil {
			return nil, 0, eventToolError(err)
		}
		return map[string]any{"deleted": id}, 1, nil
	}}, nil
}

// eventToolError keeps the editor's coded errors and turns anything else into
// a plain internal error.
func eventToolError(err error) error {
	var te *ToolError
	var ae *ArgError
	if errors.As(err, &te) || errors.As(err, &ae) {
		return err
	}
	return toolErr("internal", "the calendar change failed on the server")
}
