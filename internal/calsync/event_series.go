package calsync

import (
	"context"
	"errors"

	"github.com/OpenTodoApp/opentodo/internal/calfeed"
)

// The worker's side of a series write (calendar-event-series D5): after the
// account changed, the cache takes what the writer left behind, re-expanded from
// the objects the writer parsed from the bodies it sent, in one transaction; the
// next cycle reads the calendar again and heals any difference.

// dropList is the cached uids a write replaces: the event's own, every uid of
// its series when the scope reaches beyond this occurrence, and the uids the
// writer wrote.
func (w *Worker) dropList(ctx context.Context, t *eventTarget, scope calfeed.Scope, wr SeriesWrite) ([]string, error) {
	drop := []string{t.ref.UID}
	if scope != calfeed.ScopeThis && t.ref.Series != nil {
		uids, err := w.Feeds.SeriesUIDs(ctx, t.sub.SubscriptionID, t.ref.Series.ID)
		if err != nil {
			return nil, err
		}
		drop = append(drop, uids...)
	}
	for _, o := range wr.Written {
		for _, ev := range o.Events {
			drop = append(drop, ev.UID)
		}
	}
	return dedupe(drop), nil
}

// expandWritten expands the objects a write left, over the window the cache keeps,
// with the refs and series rows the cycle would store for them.
func (w *Worker) expandWritten(ctx context.Context, wr SeriesWrite) ([]calfeed.Occurrence, map[string]calfeed.EventRemote) {
	var events []calfeed.Event
	refs := map[string]calfeed.EventRemote{}
	for _, o := range wr.Written {
		for _, ev := range o.Events {
			events = append(events, ev)
			refs[ev.UID] = calfeed.EventRemote{Href: o.Ref.Href, ETag: o.Ref.ETag, Locked: o.Locked, Series: o.Series}
		}
	}
	ectx, cancel := context.WithTimeout(ctx, calfeed.MaxExpandTime)
	defer cancel()
	occ, _ := calfeed.ExpandContext(ectx, events, calfeed.StoreWindow(w.clock()), calfeed.MaxOccurrences)
	return occ, refs
}

// cacheWrite records a scoped write in the cache and serves the edited
// occurrence as the calendar holds it now.
func (w *Worker) cacheWrite(ctx context.Context, t *eventTarget, scope calfeed.Scope, wr SeriesWrite) (*EventResult, error) {
	drop, err := w.dropList(ctx, t, scope, wr)
	if err != nil {
		return nil, err
	}
	occ, refs := w.expandWritten(ctx, wr)
	if _, err := w.Feeds.ReplaceSeries(ctx, t.sub.SubscriptionID, drop, occ, refs); err != nil {
		return nil, err
	}
	res := &EventResult{Replaces: t.ref.EventID, ConnectionID: t.conn.ID, Calendar: t.sub.CalendarURL, Provider: t.conn.Provider, Refetch: wr.Refetch}
	w.notify(t.conn.UserID)
	if wr.UID == "" {
		return res, nil // a delete
	}
	row, err := w.Feeds.EventRowOf(ctx, t.sub.SubscriptionID, wr.UID, wr.Instance)
	switch {
	case errors.Is(err, calfeed.ErrNotFound):
		res.Gone = true // it left the cached window
	case err != nil:
		return nil, err
	default:
		a := AccessOf(t.conn, t.sub.CalendarURL, row)
		row.Access = &a
		res.Event = row
	}
	for _, o := range wr.Written {
		if !holds(o, wr.UID) {
			continue
		}
		if o.Ref.ETag == "" {
			res.Refetch = true // a server that never says the version: the cache has none to give
		}
		if o.Series != nil {
			res.Series = &SeriesResult{ID: seriesIDOf(t.sub.SubscriptionID, o.Series, wr.UID)}
			if t.conn.Provider != ProviderMicrosoft {
				res.Series.Rev = calfeed.Rev(o.Ref.ETag)
			}
		}
	}
	return res, nil
}

func holds(o WrittenObject, uid string) bool {
	for _, ev := range o.Events {
		if ev.UID == uid {
			return true
		}
	}
	return false
}

// seriesIDOf is the id the cache groups a series' occurrences by.
func seriesIDOf(sub string, s *calfeed.SeriesInfo, uid string) string {
	if s.ID != "" {
		return s.ID
	}
	return calfeed.SeriesID(sub, firstOf(s.Key, uid))
}

// addSeries caches an event that was created repeating: the first occurrence
// with the created record (as for a single event), then the object's occurrences
// of the window in its place.
func (w *Worker) addSeries(ctx context.Context, sub calfeed.ManagedCalendar, st EventState, connID, clientID string) (calfeed.Occurrence, error) {
	first := calfeed.OccurrenceOf(st.Event)
	obj := st.Wrote.Written[0]
	remote := calfeed.EventRemote{Href: obj.Ref.Href, ETag: obj.Ref.ETag, Locked: obj.Locked, Series: obj.Series}
	if _, err := w.Feeds.AddEvent(ctx, sub.SubscriptionID, first, remote, connID, clientID); err != nil {
		return first, err
	}
	if occ, refs := w.expandWritten(ctx, st.Wrote); len(occ) > 0 {
		if _, err := w.Feeds.ReplaceSeries(ctx, sub.SubscriptionID, []string{first.UID}, occ, refs); err != nil {
			return first, err
		}
	}
	return first, nil
}
