package calsync

import (
	"context"
	"errors"

	"github.com/OpenTodoApp/opentodo/internal/calfeed"
)

// The worker's side of editing a connected calendar's events (OpenSpec changes
// calendar-event-edits and calendar-event-series, design D4 and D5): the one
// function the REST routes, the agent tools and the CLI's routes share, so
// checks, locks and wake-ups are identical.

// Codes of an EventError (the REST error codes of design D4).
const (
	EventNotFound      = "not_found"
	EventEditsOff      = "event_edits_off"
	EventReadOnly      = "calendar_read_only"
	EventNotEditable   = "event_not_editable"
	EventChanged       = "event_changed"
	EventGone          = "event_gone"
	EventUnreachable   = "calendar_unreachable"
	EventScopeRequired = "scope_required"
)

// Reasons of an EventError added by calendar-event-series.
const (
	ReasonFollowingUnsupported = "following_unsupported" // an EventNotEditable reason: the copy would lose data
	ReasonPartialWrite         = "partial_write"         // an EventUnreachable reason: a split is half written
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

// SeriesResult is the series an edited occurrence belongs to after the write:
// its id and, for CalDAV, the version every occurrence of the object shares.
type SeriesResult struct {
	ID  string
	Rev string
}

// EventResult is the outcome of an edit: the event as the events route serves
// it now and, when the id changed (a move changes it), the id it replaces.
type EventResult struct {
	Event        calfeed.EventRow
	Replaces     string
	ConnectionID string
	// Calendar is the URL of the calendar holding the event.
	Calendar string
	Provider string
	// Created is true when the call made the event, false for a replay of a create.
	Created bool
	// Series is set for an occurrence of a series, Refetch when other occurrences
	// changed, Gone when the edited occurrence left the cached window (Event is empty).
	Series  *SeriesResult
	Refetch bool
	Gone    bool
}

// AccessOf is what the user may do to a connected calendar's event (design
// D3): update and delete are true only when the connection allows edits, the
// calendar's caps allow them and the event is not locked. The reason is the
// first unmet condition. A row written before calendar-event-series that is
// locked as recurring is refresh_pending until the calendar is read again.
func AccessOf(c *Connection, calendarURL string, row calfeed.EventRow) calfeed.Access {
	caps := Caps{}
	if cal := c.Calendar(calendarURL); cal != nil {
		caps = cal.EffectiveCaps()
	}
	lock, known := row.Locked, row.Known
	if lock == lockRecurringLegacy {
		lock, known = "", false
	}
	a := calfeed.Access{Update: c.EditEvents && caps.Update && lock == "" && known,
		Delete: c.EditEvents && caps.Delete && lock == "" && known}
	switch {
	case !c.EditEvents:
		a.Reason = EventEditsOffReason
	case !caps.Update && !caps.Delete:
		a.Reason = EventReadOnly
	case lock != "":
		a.Reason = lock
	case !known:
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

// remoteRef is the reference a writer gets for a found event: its address and
// version and, for an occurrence of a series, its place in it.
func (t *eventTarget) remoteRef() EventRef {
	ref := EventRef{Calendar: t.sub.CalendarURL, Href: t.ref.Href, ETag: t.ref.ETag}
	if t.ref.Series != nil {
		ref.Instance = t.ref.Instance // Microsoft writers read the series master from the occurrence
	}
	return ref
}

// MaxReminders is how many reminders an event of the connection takes.
func MaxReminders(provider string) int {
	if provider == ProviderMicrosoft {
		return 1
	}
	return maxDAVReminders
}

// check applies the refusals that need no request, in the order of design D3 and D4.
func (w *Worker) check(t *eventTarget, rev string, need func(Caps) bool, edit EventEdit) error {
	cal := t.conn.Calendar(t.sub.CalendarURL)
	switch {
	case !t.conn.EditEvents:
		return &EventError{Code: EventEditsOff}
	case cal == nil || !need(cal.EffectiveCaps()):
		return &EventError{Code: EventReadOnly}
	case t.ref.Locked == lockRecurringLegacy:
		return &EventError{Code: EventNotEditable, Reason: ReasonRefreshPending}
	case t.ref.Locked != "":
		return &EventError{Code: EventNotEditable, Reason: t.ref.Locked}
	}
	if err := t.scopeRules(edit); err != nil {
		return err
	}
	if calfeed.Rev(t.ref.ETag) != rev {
		return &EventError{Code: EventChanged}
	}
	return nil
}

// scopeRules are the rules of design D1 and D3 that need only the cache: scope is
// required for an occurrence and refused otherwise, a repeat change needs
// following or all, and a custom rule cannot be changed through the form.
func (t *eventTarget) scopeRules(edit EventEdit) error {
	occurrence := t.ref.Series != nil
	switch {
	case occurrence && edit.Scope == "":
		return &EventError{Code: EventScopeRequired}
	case edit.Scope != "" && (!occurrence || !edit.Scope.Valid()):
		return &EventError{Code: EventInvalid, Reason: "scope"}
	}
	if r := edit.Repeat; r != nil {
		switch {
		case edit.Scope == calfeed.ScopeThis,
			r.Rule == nil && edit.Scope != calfeed.ScopeFollowing,
			occurrence && t.ref.Series.RRule != "" && t.ref.Series.Repeat == "":
			return &EventError{Code: EventInvalid, Reason: "repeat"}
		}
	}
	if edit.Reminders != nil && len(*edit.Reminders) > MaxReminders(t.conn.Provider) {
		return &EventError{Code: EventInvalid, Reason: "reminders"}
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
		w.dropGone(ctx, t)
		w.notify(t.conn.UserID)
		return &EventError{Code: EventGone, Err: err}
	case errors.Is(err, ErrEventLocked):
		return &EventError{Code: EventNotEditable, Reason: lockReason(err), Err: err}
	case errors.Is(err, ErrEventInvalid):
		return &EventError{Code: EventInvalid, Reason: invalidField(err), Err: err}
	case errors.Is(err, ErrFollowingUnsupported):
		return &EventError{Code: EventNotEditable, Reason: ReasonFollowingUnsupported, Err: err}
	case errors.Is(err, ErrPartialWrite):
		w.refreshAfterPartial(ctx, t)
		return &EventError{Code: EventUnreachable, Reason: ReasonPartialWrite, Err: err}
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

// dropGone takes an event that is no longer on the account out of the cache; a
// series that is gone goes with all its occurrences.
func (w *Worker) dropGone(ctx context.Context, t *eventTarget) {
	var err error
	if t.ref.Series != nil {
		var uids []string
		if uids, err = w.Feeds.SeriesUIDs(ctx, t.sub.SubscriptionID, t.ref.Series.ID); err == nil {
			err = w.Feeds.DropSeries(ctx, t.sub.SubscriptionID, append(uids, t.ref.UID))
		}
	} else {
		_, err = w.Feeds.ReplaceEvent(ctx, t.sub.SubscriptionID, t.ref, nil, calfeed.EventRemote{})
	}
	if err != nil {
		w.log().Error("calendar sync: drop gone event", "connection", t.conn.ID, "err", err)
	}
}

// refreshAfterPartial has the next cycle read the calendar, so a duplicate a
// half-written split left is visible, and wakes the devices.
func (w *Worker) refreshAfterPartial(ctx context.Context, t *eventTarget) {
	w.log().Error("calendar sync: a series change was half written", "connection", t.conn.ID)
	if err := w.Feeds.DropSeries(ctx, t.sub.SubscriptionID, nil); err != nil {
		w.log().Error("calendar sync: refresh after a half-written split", "connection", t.conn.ID, "err", err)
	}
	w.notify(t.conn.UserID)
}

func (w *Worker) notify(userID string) {
	if w.Notify != nil {
		w.Notify(userID)
	}
}

// EditEvent changes one event of a connection's calendar (rev is the version
// the caller saw) and returns it as the calendar now holds it. edit.Scope says
// how much of a series an occurrence's change reaches. Nothing is written when a
// check fails; the errors are *EventError.
func (w *Worker) EditEvent(ctx context.Context, userID, connID, eventID, rev string, edit EventEdit) (*EventResult, error) {
	l := w.lock(connID)
	l.Lock()
	defer l.Unlock()
	t, err := w.find(ctx, userID, connID, eventID)
	if err != nil {
		return nil, err
	}
	if err := w.check(t, rev, func(c Caps) bool { return c.Update }, edit); err != nil {
		return nil, err
	}
	writer, err := w.EventWriter(ctx, t.conn)
	if err != nil {
		return nil, w.remoteError(ctx, t, err)
	}
	st, err := writer.Update(ctx, t.remoteRef(), edit)
	if err != nil {
		return nil, w.remoteError(ctx, t, err)
	}
	if len(st.Wrote.Written) > 0 || st.Wrote.UID != "" {
		return w.cacheWrite(ctx, t, edit.Scope, st.Wrote)
	}
	occ := calfeed.OccurrenceOf(st.Event)
	remote := calfeed.EventRemote{Href: firstOf(st.Ref.Href, t.ref.Href), ETag: st.Ref.ETag, Locked: st.Locked}
	if _, err := w.Feeds.ReplaceEvent(ctx, t.sub.SubscriptionID, t.ref, &occ, remote); err != nil {
		return nil, err
	}
	res, err := w.resultOf(ctx, t.conn, t.sub, occ.UID, occ.Instance, t.ref.EventID)
	if err != nil {
		return nil, err
	}
	w.notify(userID)
	return res, nil
}

// DeleteEvent removes one event from a connection's calendar (rev is the
// version the caller saw) and from the cache; scope says how much of a series an
// occurrence's delete reaches. The errors are *EventError.
func (w *Worker) DeleteEvent(ctx context.Context, userID, connID, eventID, rev string, scope calfeed.Scope) (*EventResult, error) {
	l := w.lock(connID)
	l.Lock()
	defer l.Unlock()
	t, err := w.find(ctx, userID, connID, eventID)
	if err != nil {
		return nil, err
	}
	if err := w.check(t, rev, func(c Caps) bool { return c.Delete }, EventEdit{Scope: scope}); err != nil {
		return nil, err
	}
	writer, err := w.EventWriter(ctx, t.conn)
	if err != nil {
		return nil, w.remoteError(ctx, t, err)
	}
	if t.ref.Series != nil {
		wr, err := writer.DeleteScoped(ctx, t.remoteRef(), scope)
		if err != nil {
			return nil, w.remoteError(ctx, t, err)
		}
		return w.cacheWrite(ctx, t, scope, wr)
	}
	if err := writer.Delete(ctx, t.remoteRef()); err != nil {
		return nil, w.remoteError(ctx, t, err)
	}
	if _, err := w.Feeds.ReplaceEvent(ctx, t.sub.SubscriptionID, t.ref, nil, calfeed.EventRemote{}); err != nil {
		return nil, err
	}
	w.notify(userID)
	return &EventResult{Replaces: t.ref.EventID, ConnectionID: connID, Calendar: t.sub.CalendarURL, Provider: t.conn.Provider}, nil
}

// Added by calendar-event-editor to the phase-1 sets.
const (
	LockRichDescription = "description_rich" // an EventError reason for EventNotEditable
	EventInvalid        = "invalid_input"    // an EventError code; Reason names the field
	ReasonHrefTaken     = "href_taken"       // a CalDAV object address held by another UID
)

// EventDetail is an event with the description the editor shows, and for an
// event of a series or with reminders, what the editor shows of them.
type EventDetail struct {
	EventResult
	Description string
	// DescriptionEditable is false for a rich-text body the editor shows read-only.
	DescriptionEditable bool
	// Series is nil for a single event; Reminders are the minutes the form owns,
	// RemindersMax how many the calendar takes and RemindersOther how many other
	// alarms the event has that the form keeps.
	SeriesDetail   *SeriesDetail
	Reminders      []int
	RemindersMax   int
	RemindersOther int
}
