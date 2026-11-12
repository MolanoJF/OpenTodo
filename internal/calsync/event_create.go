package calsync

import (
	"context"
	"errors"

	"github.com/OpenTodoApp/opentodo/internal/calfeed"
)

// Creating events and reading their detail (OpenSpec change
// calendar-event-editor, design D4 and D5): the Worker side shared by the REST
// routes, the agent tools and the CLI's routes.

// ValidClientID reports whether s is a client id: 8 to 64 of A-Za-z0-9_-.
func ValidClientID(s string) bool {
	if len(s) < 8 || len(s) > 64 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
		default:
			return false
		}
	}
	return true
}

// resultOf serves a cached occurrence with the access the user has to it.
func (w *Worker) resultOf(ctx context.Context, conn *Connection, sub calfeed.ManagedCalendar, uid, instance, replaces string) (*EventResult, error) {
	row, err := w.Feeds.EventRowOf(ctx, sub.SubscriptionID, uid, instance)
	if err != nil {
		return nil, err
	}
	a := AccessOf(conn, sub.CalendarURL, row)
	row.Access = &a
	return &EventResult{Event: row, Replaces: replaces, ConnectionID: conn.ID, Calendar: sub.CalendarURL, Provider: conn.Provider}, nil
}

// replay answers a create whose client id was created before: the cached
// event as it is now, or EventGone when it has been deleted since.
func (w *Worker) replay(ctx context.Context, conn *Connection, managed map[string]calfeed.ManagedCalendar, subID, uid string) (*EventResult, error) {
	var sub *calfeed.ManagedCalendar
	for _, m := range managed {
		if m.SubscriptionID == subID {
			m := m
			sub = &m
		}
	}
	if sub == nil {
		return nil, &EventError{Code: EventGone, Err: calfeed.ErrNotFound}
	}
	ref, err := w.Feeds.RefByUID(ctx, subID, uid)
	if errors.Is(err, calfeed.ErrNotFound) {
		return nil, &EventError{Code: EventGone, Err: err}
	}
	if err != nil {
		return nil, err
	}
	return w.resultOf(ctx, conn, *sub, ref.UID, ref.Instance, "")
}

// CreateEvent makes ev (UID is the client id) in one of the connection's shown
// calendars and caches it. A client id that was created before returns that
// event with Created false and writes nothing; if the event has been deleted
// since, it is EventGone. The errors are *EventError.
func (w *Worker) CreateEvent(ctx context.Context, userID, connID, calendarURL string, ev EventNew) (*EventResult, error) {
	l := w.lock(connID)
	l.Lock()
	defer l.Unlock()
	if !ValidClientID(ev.UID) {
		return nil, &EventError{Code: EventInvalid, Reason: "clientId"}
	}
	conn, err := w.Store.Get(ctx, userID, connID)
	if err != nil {
		return nil, &EventError{Code: EventNotFound, Err: err}
	}
	if !conn.EditEvents {
		return nil, &EventError{Code: EventEditsOff}
	}
	managed, err := w.Feeds.ManagedOf(ctx, connID)
	if err != nil {
		return nil, err
	}
	sub, shown := managed[calendarURL]
	if !shown {
		return nil, &EventError{Code: EventInvalid, Reason: "calendarUrl"}
	}
	if cal := conn.Calendar(calendarURL); cal == nil || !cal.EffectiveCaps().Create {
		return nil, &EventError{Code: EventReadOnly}
	}
	if subID, uid, ok, err := w.Feeds.CreatedBy(ctx, connID, ev.UID); err != nil {
		return nil, err
	} else if ok {
		return w.replay(ctx, conn, managed, subID, uid)
	}
	writer, err := w.EventWriter(ctx, conn)
	if err != nil {
		return nil, w.remoteError(ctx, &eventTarget{conn: conn, sub: sub}, err)
	}
	st, err := writer.Create(ctx, calendarURL, ev)
	created := err == nil
	switch {
	case errors.Is(err, ErrEventExists):
		err = nil // the account already holds it: cache it as a replay
	case errors.Is(err, ErrEventGone): // the object at the address vanished between two requests
		err = &Error{Reason: ReasonConnection}
	}
	if err != nil {
		return nil, w.remoteError(ctx, &eventTarget{conn: conn, sub: sub}, err)
	}
	occ := calfeed.OccurrenceOf(st.Event)
	if len(st.Wrote.Written) > 0 { // it repeats: the cache takes the occurrences of the window
		if occ, err = w.addSeries(ctx, sub, st, connID, ev.UID); err != nil {
			return nil, err
		}
	} else {
		remote := calfeed.EventRemote{Href: st.Ref.Href, ETag: st.Ref.ETag, Locked: st.Locked}
		if _, err := w.Feeds.AddEvent(ctx, sub.SubscriptionID, occ, remote, connID, ev.UID); err != nil {
			return nil, err
		}
	}
	res, err := w.resultOf(ctx, conn, sub, occ.UID, occ.Instance, "")
	if err != nil {
		return nil, err
	}
	res.Created = created
	w.notify(userID)
	return res, nil
}

// EventDetail reads one event from its calendar with its description. When the
// calendar holds a newer version than the cache, the cache takes it, the
// user's other devices are woken, and Replaces names the id it replaces. The
// event need not be editable. The errors are *EventError.
func (w *Worker) EventDetail(ctx context.Context, userID, connID, eventID string) (*EventDetail, error) {
	l := w.lock(connID)
	l.Lock()
	defer l.Unlock()
	t, err := w.find(ctx, userID, connID, eventID)
	if err != nil {
		return nil, err
	}
	if !t.conn.EditEvents {
		return nil, &EventError{Code: EventEditsOff}
	}
	writer, err := w.EventWriter(ctx, t.conn)
	if err != nil {
		return nil, w.remoteError(ctx, t, err)
	}
	st, err := writer.Detail(ctx, t.remoteRef())
	if err != nil {
		return nil, w.remoteError(ctx, t, err)
	}
	var res *EventResult
	switch {
	case st.Ref.ETag == t.ref.ETag:
		res, err = w.resultOf(ctx, t.conn, t.sub, t.ref.UID, t.ref.Instance, "")
	case len(st.Wrote.Written) > 0: // a series the cache is behind: it takes the object as read
		if res, err = w.cacheWrite(ctx, t, calfeed.ScopeAll, st.Wrote); err == nil && res.Gone {
			return nil, &EventError{Code: EventGone, Err: calfeed.ErrNotFound}
		}
	default:
		occ := calfeed.OccurrenceOf(st.Event)
		remote := calfeed.EventRemote{Href: firstOf(st.Ref.Href, t.ref.Href), ETag: st.Ref.ETag, Locked: st.Locked, Series: t.ref.Series}
		if _, err = w.Feeds.ReplaceEvent(ctx, t.sub.SubscriptionID, t.ref, &occ, remote); err == nil {
			w.notify(userID)
			res, err = w.resultOf(ctx, t.conn, t.sub, occ.UID, occ.Instance, t.ref.EventID)
		}
	}
	if err != nil {
		return nil, err
	}
	return &EventDetail{EventResult: *res, Description: st.Description, DescriptionEditable: !st.DescriptionLocked,
		SeriesDetail: st.Series, Reminders: st.Reminders, RemindersMax: st.RemindersMax, RemindersOther: st.RemindersOther}, nil
}
