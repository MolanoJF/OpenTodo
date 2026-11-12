package calsync

import (
	"context"
	"errors"

	"github.com/OpenTodoApp/opentodo/internal/calfeed"
)

// Editing the events of a connected calendar (OpenSpec change
// calendar-event-edits, design D5): one interface over the remote account, a
// WebDAV implementation for CalDAV and Google and a Graph one for Microsoft.
// The calendar is the source of truth: every write is conditional on the
// version the caller read.

// EventRef names one event on the remote account at the version the caller read.
type EventRef struct {
	Calendar string // the calendar's URL (DAV) or id (Graph), as in Connection.Calendars
	Href     string // CalDAV object address, or the Graph event id
	ETag     string // never empty: every write is conditional
}

// EventEdit is a change; a nil field stays as it is.
type EventEdit struct {
	Summary *string
	When    *EventWhen
}

// EventWhen is a start and end as the event keeps them: a zoned point keeps its
// IANA zone, a point with an empty Zone is floating, an all-day pair holds dates
// (end exclusive).
type EventWhen struct {
	Start, End calfeed.Point
	AllDay     bool
}

// EventState is an event as the remote holds it after a read or a write.
type EventState struct {
	Ref    EventRef
	Event  calfeed.Event
	Locked string // "" or a reason of design D2
}

// EventWriter changes events of one connection's calendars. Every write is
// conditional on ref.ETag.
type EventWriter interface {
	// Read returns the event as the remote holds it now.
	Read(ctx context.Context, ref EventRef) (EventState, error)
	// Update applies edit while the event still has ref.ETag; ErrEventChanged
	// when it does not (HTTP 412, or an ETag that differs on the pre-read).
	Update(ctx context.Context, ref EventRef, edit EventEdit) (EventState, error)
	// Delete removes the event while it still has ref.ETag.
	Delete(ctx context.Context, ref EventRef) error
}

// Errors of an EventWriter; Worker.EditEvent maps them onto EventError.
var (
	ErrEventChanged = errors.New("calendar event changed") // 412
	ErrEventGone    = errors.New("calendar event gone")    // 404
	ErrEventLocked  = errors.New("calendar event locked")  // recurring, guests, not organizer
	ErrCalendarRO   = errors.New("calendar is read only")  // 403 on a write
)

// lockedError is ErrEventLocked with the reason of design D2.
type lockedError struct{ reason string }

func (e *lockedError) Error() string   { return "calendar event locked: " + e.reason }
func (e *lockedError) Is(t error) bool { return t == ErrEventLocked }

func locked(reason string) error { return &lockedError{reason} }

// lockReason is the reason inside an ErrEventLocked, "" for another error.
func lockReason(err error) string {
	var le *lockedError
	if errors.As(err, &le) {
		return le.reason
	}
	return ""
}

// EventWriter builds the event writer of a connection with its credentials:
// Graph for Microsoft, WebDAV for CalDAV and Google (reached through its
// CalDAV API).
func (w *Worker) EventWriter(ctx context.Context, c *Connection) (EventWriter, error) {
	if c.Provider == ProviderMicrosoft {
		g, err := w.graphFor(ctx, c)
		if err != nil {
			return nil, err
		}
		return graphEvents{g: g, zone: c.mailboxZone()}, nil
	}
	dav, err := w.Client(ctx, c)
	if err != nil {
		return nil, err
	}
	return davEvents{d: dav, now: w.clock}, nil
}

// refusal maps the status of a refused event request onto the writer errors;
// nil for a success.
func refusal(status int) error {
	switch {
	case status >= 200 && status <= 299:
		return nil
	case status == 404:
		return ErrEventGone
	case status == 412:
		return ErrEventChanged
	case status == 403:
		return ErrCalendarRO
	}
	return statusError(status)
}
