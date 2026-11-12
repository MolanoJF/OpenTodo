package calsync

import (
	"context"
	"errors"

	"github.com/OpenTodoApp/opentodo/internal/calfeed"
)

// Editing the events of a connected calendar (OpenSpec changes
// calendar-event-edits and calendar-event-series, design D5): one interface over
// the remote account, a WebDAV implementation for CalDAV and Google and a Graph
// one for Microsoft. The calendar is the source of truth: every write is
// conditional on the version the caller read.

// EventRef names one event on the remote account at the version the caller read.
type EventRef struct {
	Calendar string // the calendar's URL (DAV) or id (Graph), as in Connection.Calendars
	Href     string // CalDAV object address, or the Graph event id (an occurrence id for Microsoft series)
	ETag     string // never empty: every write is conditional
	Instance string // the occurrence's instance key; "" for a single event
	Series   string // Microsoft: the seriesMasterId; "" otherwise
}

// EventEdit is a change; a nil field stays as it is.
type EventEdit struct {
	Summary     *string
	When        *EventWhen
	Location    *string // "" clears it
	Description *string // plain text; "" clears it
	Free        *bool   // true: free (TRANSP:TRANSPARENT, showAs free); false: busy
	Scope       calfeed.Scope
	Repeat      *RepeatEdit // nil: unchanged
	Reminders   *[]int      // nil: unchanged; empty: remove the ones the form shows
}

// RepeatEdit sets the repeat; a nil Rule stops repeating.
type RepeatEdit struct{ Rule *calfeed.Repeat }

// EventNew is an event to create in a calendar (calendar-event-editor D5).
type EventNew struct {
	UID         string // the client id: the CalDAV UID and object name, the Graph transaction id
	Summary     string
	When        EventWhen
	Location    string
	Description string
	Free        bool
	Repeat      *calfeed.Repeat // nil: a single event
	Reminders   []int
}

// EventWhen is a start and end as the event keeps them: a zoned point keeps its
// IANA zone, a point with an empty Zone is floating, an all-day pair holds dates
// (end exclusive).
type EventWhen struct {
	Start, End calfeed.Point
	AllDay     bool
}

// WrittenObject is a calendar object a write left behind, as the writer parsed
// it from the body it sent, so the worker can re-expand it without another read.
type WrittenObject struct {
	Ref    EventRef
	Events []calfeed.Event
	Locked string
	Series *calfeed.SeriesInfo
}

// SeriesWrite lists what a scoped write changed on the account.
type SeriesWrite struct {
	Written  []WrittenObject
	Deleted  []string // hrefs (or Graph ids) removed
	UID      string   // the edited occurrence's uid afterwards ("" for a delete)
	Instance string   // and its instance key
	// Refetch is true when occurrences other than the edited one changed.
	Refetch bool
}

// SeriesDetail is a series as the editor shows it.
type SeriesDetail struct {
	RRule      string
	Repeat     *calfeed.Repeat // nil when the rule is custom or not known
	Custom     bool
	Exceptions int // exceptions and excluded dates, for the "resets" warning
}

// EventState is an event as the remote holds it after a read or a write.
type EventState struct {
	Ref    EventRef
	Event  calfeed.Event
	Locked string // "" or a reason of design D2
	// Description and DescriptionLocked are filled by Detail only. DescriptionLocked
	// is true when the description is a rich-text body that plain text would flatten.
	Description       string
	DescriptionLocked bool
	// Wrote is the series write behind an update that touched a series (or made
	// an event repeat), and the object a Detail read of a series.
	Wrote SeriesWrite
	// Detail only: the series, the reminders and what the editor shows of them.
	Series         *SeriesDetail
	Reminders      []int
	RemindersMax   int
	RemindersOther int
}

// EventWriter changes events of one connection's calendars. Every write is
// conditional on ref.ETag, and a create on the event not existing yet.
type EventWriter interface {
	// Read returns the event as the remote holds it now.
	Read(ctx context.Context, ref EventRef) (EventState, error)
	// Detail is Read with the description.
	Detail(ctx context.Context, ref EventRef) (EventState, error)
	// Create writes ev into the calendar (its URL, or the Graph calendar id) and
	// returns it as the account holds it. When ev.UID was created before it returns
	// the existing event's state together with ErrEventExists.
	Create(ctx context.Context, calendar string, ev EventNew) (EventState, error)
	// Update applies edit while the event still has ref.ETag; ErrEventChanged
	// when it does not (HTTP 412, or an ETag that differs on the pre-read).
	// It honours edit.Scope when ref names an occurrence.
	Update(ctx context.Context, ref EventRef, edit EventEdit) (EventState, error)
	// Delete removes the event while it still has ref.ETag.
	Delete(ctx context.Context, ref EventRef) error
	// DeleteScoped removes an occurrence for a scope (ref names an occurrence).
	DeleteScoped(ctx context.Context, ref EventRef, scope calfeed.Scope) (SeriesWrite, error)
}

// Errors of an EventWriter; Worker.EditEvent maps them onto EventError.
var (
	ErrEventChanged = errors.New("calendar event changed") // 412
	ErrEventGone    = errors.New("calendar event gone")    // 404
	ErrEventLocked  = errors.New("calendar event locked")  // guests, not organizer, series_unsupported
	ErrCalendarRO   = errors.New("calendar is read only")  // 403 on a write
	// ErrEventExists is a create that met the same UID: the state returned with it is the existing event.
	ErrEventExists = errors.New("calendar event exists")
	// ErrEventInvalid is a change the event's rule can not take (the Reason names the field).
	ErrEventInvalid = errors.New("calendar event change invalid")
	// ErrFollowingUnsupported is a following change that would lose data (Microsoft:
	// attachments, an online meeting, a pattern the editor does not write).
	ErrFollowingUnsupported = errors.New("following change would lose data")
	// ErrPartialWrite is a following change half written: the rollback failed too.
	ErrPartialWrite = errors.New("series change half written")
)

// lockedError is ErrEventLocked with the reason of design D2.
type lockedError struct{ reason string }

func (e *lockedError) Error() string   { return "calendar event locked: " + e.reason }
func (e *lockedError) Is(t error) bool { return t == ErrEventLocked }

func locked(reason string) error { return &lockedError{reason} }

// invalidError is ErrEventInvalid for one field of the change.
type invalidError struct{ field string }

func (e *invalidError) Error() string   { return "calendar event change invalid: " + e.field }
func (e *invalidError) Is(t error) bool { return t == ErrEventInvalid }

func invalid(field string) error { return &invalidError{field} }

// invalidField is the field inside an ErrEventInvalid, "" for another error.
func invalidField(err error) string {
	var ie *invalidError
	if errors.As(err, &ie) {
		return ie.field
	}
	return ""
}

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
		return graphEvents{g: g, zone: c.mailboxZone(), now: w.clock}, nil
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
