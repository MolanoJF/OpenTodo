package agenttools

import "context"

// Calendar events for agents (OpenSpec change calendar-event-edits, design D8):
// one read tool and two write tools over the events of the calendars the user
// connected through calendar sync. The edits are not change sets: the planner
// validates and hands Call an exec that goes through the EventEditor, the same
// shared function the REST routes use, so checks, locks, audit and wake-ups
// are identical. The tools live in read_calendar_events.go and
// write_calendar_events.go.

// EventTime is one end of an event as the events route serves and the PATCH
// route takes it: a date, with a wall time and a zone for a timed event (no
// zone: floating).
type EventTime struct {
	Date string  `json:"date"`
	Time *string `json:"time,omitempty"`
	TZ   *string `json:"tz,omitempty"`
}

// EventAccess says what the user may do to an event (design D3).
type EventAccess struct {
	Update bool   `json:"update"`
	Delete bool   `json:"delete"`
	Reason string `json:"reason,omitempty"`
}

// CalendarEvent is one event of the calendar view. ConnectionID, Rev and
// Access are set only for an event of a connected calendar.
type CalendarEvent struct {
	ID           string       `json:"id"`
	ConnectionID string       `json:"connection_id,omitempty"`
	Rev          string       `json:"rev,omitempty"`
	Start        EventTime    `json:"start"`
	End          EventTime    `json:"end"`
	AllDay       bool         `json:"all_day"`
	Free         bool         `json:"free"`
	Access       *EventAccess `json:"access,omitempty"`
	// Title and Location are text from the remote calendar.
	Title    string `json:"-"`
	Location string `json:"-"`
}

// EventUpdate is the change update_calendar_event asks for.
type EventUpdate struct {
	ConnectionID string
	EventID      string
	Rev          string
	Summary      *string
	Start, End   *EventTime
	AllDay       *bool
	// Location and Description: "" clears; Description replaces the whole text.
	Location, Description *string
	Free                  *bool
}

// EventCreate is the change create_calendar_event asks for (calendar-event-editor
// D13). ClientID is the idempotency key: a repeat returns the first event.
type EventCreate struct {
	ConnectionID, CalendarURL string
	ClientID                  string
	Summary                   string
	Start, End                EventTime
	AllDay                    *bool
	Location, Description     string
	Free                      bool
}

// EventCalendar is a calendar an event can be created in.
type EventCalendar struct {
	ConnectionID string `json:"connection_id"`
	CalendarURL  string `json:"calendar_url"`
	Name         string `json:"name"`
	// CanCreate is false while "Edit events in OpenTodo" is off for the connection.
	CanCreate bool `json:"can_create"`
}

// EventOutcome is the event as the calendar now holds it and the id it had.
type EventOutcome struct {
	Event    CalendarEvent
	Replaces string
	// Replayed: a create whose client id was used before; nothing was written.
	Replayed bool
}

// EventEditor is what the calendar event tools need from the server; the
// server installs it with Options.EventEditor. Its errors are *ToolError with
// the REST codes of design D4 (event_changed, event_edits_off, and so on) or
// *ArgError.
type EventEditor interface {
	// Events lists the events touching the days from..to ("YYYY-MM-DD").
	Events(ctx context.Context, userID, from, to string) ([]CalendarEvent, error)
	// Validate checks a change as the PATCH route does, without writing.
	Validate(u EventUpdate) error
	// Update changes one event (u.Rev is the version the caller saw).
	Update(ctx context.Context, userID string, u EventUpdate) (EventOutcome, error)
	// Calendars lists the writable calendars of the user's connections.
	Calendars(ctx context.Context, userID string) ([]EventCalendar, error)
	// ValidateCreate checks a new event as the POST route does, without writing.
	ValidateCreate(c EventCreate) error
	// Create makes one event (c.ClientID makes a retry safe).
	Create(ctx context.Context, userID string, c EventCreate) (EventOutcome, error)
	// Delete removes one event (rev is the version the caller saw).
	Delete(ctx context.Context, userID, connectionID, eventID, rev string) error
}

func calendarEventsOff() *ToolError {
	return toolErr("unavailable", "calendar events are not available on this server")
}
