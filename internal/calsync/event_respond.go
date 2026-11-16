package calsync

import (
	"context"
	"errors"

	"github.com/OpenTodoApp/opentodo/internal/calfeed"
)

// The worker's side of answering an invitation (calendar-event-guests D3, D6, D7): the
// one function the REST route, the agent tool and the CLI's route share. A reply is
// not a change of the event: its message is the one the user asked for, so it needs
// no confirmation, and nothing else is written.

// checkReply applies the refusals of a reply that need no request, in the order of
// design D3: edits on, the provider replies, the lock that matters for a reply, the
// user is a guest, the scope, the version.
func (w *Worker) checkReply(t *eventTarget, rev string, reply ReplyEdit) error {
	cal := t.conn.Calendar(t.sub.CalendarURL)
	row := calfeed.EventRow{Known: true, Locked: t.ref.Locked, Guests: t.guests}
	switch {
	case !t.conn.EditEvents:
		return &EventError{Code: EventEditsOff}
	case cal == nil || !cal.EffectiveCaps().Update:
		return &EventError{Code: EventReadOnly}
	case !reply.Response.Valid():
		return &EventError{Code: EventInvalid, Reason: "response"}
	case t.ref.Locked == lockRecurringLegacy:
		return &EventError{Code: EventNotEditable, Reason: ReasonRefreshPending}
	case t.ref.Locked == LockSeriesUnsupported || t.ref.Locked == LockNoETag:
		return &EventError{Code: EventNotEditable, Reason: t.ref.Locked}
	case !CanRespond(t.conn, t.sub.CalendarURL, row):
		return &EventError{Code: EventNotEditable, Reason: ReasonCannotRespond}
	}
	occurrence := t.ref.Series != nil
	switch {
	case occurrence && reply.Scope == "":
		return &EventError{Code: EventScopeRequired}
	case !occurrence && reply.Scope != "":
		return &EventError{Code: EventInvalid, Reason: "scope"}
	case occurrence && reply.Scope != calfeed.ScopeThis && reply.Scope != calfeed.ScopeAll:
		return &EventError{Code: EventInvalid, Reason: "scope"}
	case occurrence && reply.Scope == calfeed.ScopeThis && t.conn.Provider != ProviderMicrosoft:
		return &EventError{Code: EventInvalid, Reason: "scope"} // an occurrence's own answer is not written to CalDAV
	case !reply.SendResponse && !t.conn.GuestCaps().Silent:
		return &EventError{Code: EventInvalid, Reason: "sendResponse"}
	case calfeed.Rev(t.ref.ETag) != rev:
		return &EventError{Code: EventChanged}
	}
	return nil
}

// RespondEvent answers an invitation of one of the user's events (rev is the version
// the caller saw) and returns the event as the calendar now holds it. Only the user's
// own answer changes. ErrResponseNotKept, the calendar accepting the write and not
// holding the answer, is a 502 response_not_kept after the cache took what the
// calendar holds, so the real answer is shown. The errors are *EventError.
func (w *Worker) RespondEvent(ctx context.Context, userID, connID, eventID, rev string, reply ReplyEdit) (*EventResult, error) {
	l := w.lock(connID)
	l.Lock()
	defer l.Unlock()
	t, err := w.find(ctx, userID, connID, eventID)
	if err != nil {
		return nil, err
	}
	if err := w.checkReply(t, rev, reply); err != nil {
		return nil, err
	}
	writer, err := w.EventWriter(ctx, t.conn)
	if err != nil {
		return nil, w.remoteError(ctx, t, err)
	}
	st, err := writer.Reply(ctx, t.remoteRef(), reply)
	notKept := errors.Is(err, ErrResponseNotKept)
	if err != nil && !notKept {
		return nil, w.remoteError(ctx, t, err)
	}
	res, cerr := w.cacheReply(ctx, t, reply, st)
	if cerr != nil {
		return nil, cerr
	}
	w.notify(userID)
	if notKept {
		return nil, w.remoteError(ctx, t, ErrResponseNotKept)
	}
	res.Notified = reply.SendResponse
	return res, nil
}

// cacheReply records the event as the calendar holds it after the reply. A CalDAV
// object holds the whole series, so its occurrences are replaced together (and the
// version they share is served as series.rev); a Microsoft occurrence is its own
// event, and an answer for all events changed the others, which the next read heals.
func (w *Worker) cacheReply(ctx context.Context, t *eventTarget, reply ReplyEdit, st EventState) (*EventResult, error) {
	if len(st.Wrote.Written) > 0 {
		return w.cacheWrite(ctx, t, calfeed.ScopeAll, st.Wrote)
	}
	occ := calfeed.OccurrenceOf(st.Event)
	remote := remoteOf(t.ref.Href, st)
	remote.Series = t.ref.Series
	if _, err := w.Feeds.ReplaceEvent(ctx, t.sub.SubscriptionID, t.ref, &occ, remote); err != nil {
		return nil, err
	}
	res, err := w.resultOf(ctx, t.conn, t.sub, occ.UID, occ.Instance, t.ref.EventID)
	if err != nil {
		return nil, err
	}
	if t.ref.Series != nil {
		res.Series = &SeriesResult{ID: seriesIDOf(t.sub.SubscriptionID, t.ref.Series, occ.UID)}
		res.Refetch = reply.Scope == calfeed.ScopeAll
	}
	return res, nil
}

// fillGuests sets what the detail says the user may do with the guests: answer (a
// guest of an event the connection replies to) and change them (the organiser of an
// event the provider tells the guests of, or any event that has none yet).
func (d *EventDetail) fillGuests(c *Connection, calendarURL string) {
	cal := c.Calendar(calendarURL)
	if cal != nil {
		d.OnlineMeeting.Available = cal.EffectiveCaps().OnlineMeeting
	}
	row := d.Event
	if !d.Guests.Empty() || row.Guests != nil {
		g := &calfeed.GuestSummary{Count: d.Guests.Count, You: d.Guests.You, Organizer: d.Guests.IsOrganizer}
		row.Guests = g
		d.CanRespond = CanRespond(c, calendarURL, row)
	}
	d.CanEdit = c.GuestCaps().Edit && row.Access != nil && row.Access.Update && (d.Guests.IsOrganizer || d.Guests.Empty())
}

// createGuestRules are the cache-only refusals of a create with guests or an online
// meeting, before any remote call (D3, D8): only where the provider and the calendar
// can, and inviting people only with the caller's confirmation.
func createGuestRules(c *Connection, calendarURL string, ev EventNew) error {
	switch {
	case len(ev.Guests) > 0 && !c.GuestCaps().Edit:
		return &EventError{Code: EventInvalid, Reason: "guests"}
	case ev.OnlineMeeting:
		if cal := c.Calendar(calendarURL); cal == nil || !cal.EffectiveCaps().OnlineMeeting {
			return &EventError{Code: EventInvalid, Reason: "onlineMeeting"}
		}
	}
	if len(ev.Guests) > 0 && !ev.NotifyGuests {
		return &EventError{Code: EventInvalid, Reason: "notifyGuests"}
	}
	return nil
}
