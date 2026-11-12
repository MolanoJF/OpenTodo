package calsync

import (
	"context"
	"errors"

	"github.com/OpenTodoApp/opentodo/internal/calfeed"
)

// The worker's side of editing a connected calendar's events (OpenSpec change
// calendar-event-edits, design D4 and D5): the one function the REST routes,
// the agent tools and the CLI's routes share, so checks, locks and wake-ups
// are identical.

// Codes of an EventError (the REST error codes of design D4).
const (
	EventNotFound    = "not_found"
	EventEditsOff    = "event_edits_off"
	EventReadOnly    = "calendar_read_only"
	EventNotEditable = "event_not_editable"
	EventChanged     = "event_changed"
	EventGone        = "event_gone"
	EventUnreachable = "calendar_unreachable"
)

// ReasonUnauthorizedRemote is the reason of a refused credential.
const ReasonUnauthorizedRemote = "unauthorized_remote"

// EventError is a refused or failed event edit. Reason names the lock for
// EventNotEditable and the remote's failure for EventUnreachable.
type EventError struct {
	Code   string
	Reason string
	Err    error
}

func (e *EventError) Error() string {
	if e.Reason != "" {
		return "calendar event: " + e.Code + " (" + e.Reason + ")"
	}
	return "calendar event: " + e.Code
}

func (e *EventError) Unwrap() error { return e.Err }

// EventResult is the outcome of an edit: the event as the events route serves
// it now and, when the id changed (a move changes it), the id it replaces.
type EventResult struct {
	Event        calfeed.EventRow
	Replaces     string
	ConnectionID string
	// Calendar is the URL of the calendar holding the event.
	Calendar string
	Provider string
}

// AccessOf is what the user may do to a connected calendar's event (design
// D3): update and delete are true only when the connection allows edits, the
// calendar's caps allow them and the event is not locked. The reason is the
// first unmet condition.
func AccessOf(c *Connection, calendarURL string, row calfeed.EventRow) calfeed.Access {
	caps := Caps{}
	if cal := c.Calendar(calendarURL); cal != nil {
		caps = cal.EffectiveCaps()
	}
	a := calfeed.Access{Update: c.EditEvents && caps.Update && row.Locked == "" && row.Known,
		Delete: c.EditEvents && caps.Delete && row.Locked == "" && row.Known}
	switch {
	case !c.EditEvents:
		a.Reason = EventEditsOffReason
	case !caps.Update && !caps.Delete:
		a.Reason = EventReadOnly
	case row.Locked != "":
		a.Reason = row.Locked
	case !row.Known:
		a.Reason = ReasonRefreshPending
	case !caps.Update || !caps.Delete:
		a.Reason = EventReadOnly
	}
	return a
}

// Reasons of Access that are not locks.
const (
	EventEditsOffReason  = "edits_off"
	ReasonRefreshPending = "refresh_pending"
)

// target is an event found for an edit.
type eventTarget struct {
	conn *Connection
	sub  calfeed.ManagedCalendar
	ref  calfeed.EventRef
}

// find resolves an event of one of the user's connections through its managed
// subscriptions; anything not theirs is EventNotFound.
func (w *Worker) find(ctx context.Context, userID, connID, eventID string) (*eventTarget, error) {
	conn, err := w.Store.Get(ctx, userID, connID)
	if err != nil {
		return nil, &EventError{Code: EventNotFound, Err: err}
	}
	managed, err := w.Feeds.ManagedOf(ctx, connID)
	if err != nil {
		return nil, err
	}
	for _, m := range managed {
		ref, err := w.Feeds.RefByEvent(ctx, m.SubscriptionID, eventID)
		if errors.Is(err, calfeed.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		return &eventTarget{conn: conn, sub: m, ref: ref}, nil
	}
	return nil, &EventError{Code: EventNotFound, Err: calfeed.ErrNotFound}
}

// check applies the refusals that need no request, in the order of design D4.
func (w *Worker) check(t *eventTarget, rev string, need func(Caps) bool) error {
	cal := t.conn.Calendar(t.sub.CalendarURL)
	switch {
	case !t.conn.EditEvents:
		return &EventError{Code: EventEditsOff}
	case cal == nil || !need(cal.EffectiveCaps()):
		return &EventError{Code: EventReadOnly}
	case t.ref.Locked != "":
		return &EventError{Code: EventNotEditable, Reason: t.ref.Locked}
	case calfeed.Rev(t.ref.ETag) != rev:
		return &EventError{Code: EventChanged}
	}
	return nil
}

// remoteError maps a failed remote call onto an EventError, learning from what
// it says: a refused write clears the calendar's caps, a vanished event leaves
// the cache.
func (w *Worker) remoteError(ctx context.Context, t *eventTarget, err error) error {
	switch {
	case errors.Is(err, ErrEventChanged):
		return &EventError{Code: EventChanged, Err: err}
	case errors.Is(err, ErrEventGone):
		if _, derr := w.Feeds.ReplaceEvent(ctx, t.sub.SubscriptionID, t.ref, nil, calfeed.EventRemote{}); derr != nil {
			w.log().Error("calendar sync: drop gone event", "connection", t.conn.ID, "err", derr)
		}
		w.notify(t.conn.UserID)
		return &EventError{Code: EventGone, Err: err}
	case errors.Is(err, ErrEventLocked):
		return &EventError{Code: EventNotEditable, Reason: lockReason(err), Err: err}
	case errors.Is(err, ErrCalendarRO):
		if cerr := w.Store.SetCaps(ctx, t.conn.ID, t.sub.CalendarURL, Caps{}); cerr != nil {
			w.log().Error("calendar sync: record caps", "connection", t.conn.ID, "err", cerr)
		}
		return &EventError{Code: EventReadOnly, Err: err}
	}
	reason := reasonOf(err)
	if reason == ReasonUnauthorized {
		reason = ReasonUnauthorizedRemote
	}
	return &EventError{Code: EventUnreachable, Reason: reason, Err: err}
}

func (w *Worker) notify(userID string) {
	if w.Notify != nil {
		w.Notify(userID)
	}
}

// EditEvent changes one event of a connection's calendar (rev is the version
// the caller saw) and returns it as the calendar now holds it. Nothing is
// written when a check fails; the errors are *EventError.
func (w *Worker) EditEvent(ctx context.Context, userID, connID, eventID, rev string, edit EventEdit) (*EventResult, error) {
	l := w.lock(connID)
	l.Lock()
	defer l.Unlock()
	t, err := w.find(ctx, userID, connID, eventID)
	if err != nil {
		return nil, err
	}
	if err := w.check(t, rev, func(c Caps) bool { return c.Update }); err != nil {
		return nil, err
	}
	writer, err := w.EventWriter(ctx, t.conn)
	if err != nil {
		return nil, w.remoteError(ctx, t, err)
	}
	st, err := writer.Update(ctx, EventRef{Calendar: t.sub.CalendarURL, Href: t.ref.Href, ETag: t.ref.ETag}, edit)
	if err != nil {
		return nil, w.remoteError(ctx, t, err)
	}
	occ := calfeed.OccurrenceOf(st.Event)
	remote := calfeed.EventRemote{Href: firstOf(st.Ref.Href, t.ref.Href), ETag: st.Ref.ETag, Locked: st.Locked}
	if _, err := w.Feeds.ReplaceEvent(ctx, t.sub.SubscriptionID, t.ref, &occ, remote); err != nil {
		return nil, err
	}
	row, err := w.Feeds.EventRowOf(ctx, t.sub.SubscriptionID, occ.UID, occ.Instance)
	if err != nil {
		return nil, err
	}
	a := AccessOf(t.conn, t.sub.CalendarURL, row)
	row.Access = &a
	w.notify(userID)
	return &EventResult{Event: row, Replaces: t.ref.EventID, ConnectionID: connID, Calendar: t.sub.CalendarURL, Provider: t.conn.Provider}, nil
}

// DeleteEvent removes one event from a connection's calendar (rev is the
// version the caller saw) and from the cache. The errors are *EventError.
func (w *Worker) DeleteEvent(ctx context.Context, userID, connID, eventID, rev string) (*EventResult, error) {
	l := w.lock(connID)
	l.Lock()
	defer l.Unlock()
	t, err := w.find(ctx, userID, connID, eventID)
	if err != nil {
		return nil, err
	}
	if err := w.check(t, rev, func(c Caps) bool { return c.Delete }); err != nil {
		return nil, err
	}
	writer, err := w.EventWriter(ctx, t.conn)
	if err != nil {
		return nil, w.remoteError(ctx, t, err)
	}
	if err := writer.Delete(ctx, EventRef{Calendar: t.sub.CalendarURL, Href: t.ref.Href, ETag: t.ref.ETag}); err != nil {
		return nil, w.remoteError(ctx, t, err)
	}
	if _, err := w.Feeds.ReplaceEvent(ctx, t.sub.SubscriptionID, t.ref, nil, calfeed.EventRemote{}); err != nil {
		return nil, err
	}
	w.notify(userID)
	return &EventResult{Replaces: t.ref.EventID, ConnectionID: connID, Calendar: t.sub.CalendarURL, Provider: t.conn.Provider}, nil
}
