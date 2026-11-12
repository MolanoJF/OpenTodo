package calfeed

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/OpenTodoApp/opentodo/internal/ids"
)

// Managed subscriptions (OpenSpec change calendar-two-way, design D10): the
// calendars a calendar sync connection shows in Day and Week are stored as
// subscriptions that calsync_calendars marks. Calendar feeds' routes, limit
// and fetcher never see them; their events are served with everyone else's.

// kindFilter limits a query on calendar_subscriptions (aliased by prefix) to
// the user's own subscriptions, the managed ones, or both.
func kindFilter(prefix string, feeds, managed bool) string {
	switch {
	case feeds && managed:
		return ""
	case feeds:
		return " AND " + prefix + "id NOT IN (SELECT subscription_id FROM calsync_calendars)"
	case managed:
		return " AND " + prefix + "id IN (SELECT subscription_id FROM calsync_calendars)"
	}
	return " AND 0"
}

// ManagedCalendar is one managed subscription: the calendar it shows and the
// connection it belongs to.
type ManagedCalendar struct {
	SubscriptionID string
	ConnectionID   string
	CalendarURL    string
	// Version is the calendar's CTag or sync token when its events were stored.
	Version string
}

// Managed lists the user's managed subscriptions (the events route names them).
// One sync cycle creates several in the same millisecond, so the name breaks the
// tie before the random id and the order is the same on every server.
func (s *Store) Managed(ctx context.Context, userID string) ([]Subscription, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+subColumns+` FROM calendar_subscriptions WHERE user_id = ?`+kindFilter("", false, true)+` ORDER BY created_at, name, id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Subscription{}
	for rows.Next() {
		sub, err := scanSub(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *sub)
	}
	return out, rows.Err()
}

// ManagedOf lists a connection's managed subscriptions by calendar URL.
func (s *Store) ManagedOf(ctx context.Context, connID string) (map[string]ManagedCalendar, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT m.subscription_id, m.connection_id, m.calendar_url, s.etag FROM calsync_calendars m
		JOIN calendar_subscriptions s ON s.id = m.subscription_id WHERE m.connection_id = ?`, connID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]ManagedCalendar{}
	for rows.Next() {
		var m ManagedCalendar
		if err := rows.Scan(&m.SubscriptionID, &m.ConnectionID, &m.CalendarURL, &m.Version); err != nil {
			return nil, err
		}
		out[m.CalendarURL] = m
	}
	return out, rows.Err()
}

// EnsureManaged returns the managed subscription of a connection's calendar,
// creating it when missing, and keeps its name and colour current.
func (s *Store) EnsureManaged(ctx context.Context, userID, connID, calendarURL, name, color string) (ManagedCalendar, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ManagedCalendar{}, err
	}
	defer tx.Rollback()
	now := s.now().UnixMilli()
	m := ManagedCalendar{ConnectionID: connID, CalendarURL: calendarURL}
	err = tx.QueryRowContext(ctx, `SELECT m.subscription_id, s.etag FROM calsync_calendars m JOIN calendar_subscriptions s ON s.id = m.subscription_id
		WHERE m.connection_id = ? AND m.calendar_url = ?`, connID, calendarURL).Scan(&m.SubscriptionID, &m.Version)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		m.SubscriptionID = ids.New()
		// The address is the calendar's; nothing fetches it (Due skips managed rows).
		if _, err := tx.ExecContext(ctx, `INSERT INTO calendar_subscriptions(id,user_id,url,name,color,enabled,interval_seconds,next_fetch_at,created_at,updated_at)
			VALUES(?,?,?,?,?,1,?,?,?,?)`, m.SubscriptionID, userID, calendarURL, name, color, int64(DefaultInterval/time.Second), now, now, now); err != nil {
			return m, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO calsync_calendars(subscription_id, connection_id, calendar_url) VALUES(?,?,?)`, m.SubscriptionID, connID, calendarURL); err != nil {
			return m, err
		}
	case err != nil:
		return m, err
	default:
		if _, err := tx.ExecContext(ctx, `UPDATE calendar_subscriptions SET name = ?, color = ?, updated_at = ? WHERE id = ? AND (name != ? OR color != ?)`,
			name, color, now, m.SubscriptionID, name, color); err != nil {
			return m, err
		}
	}
	return m, tx.Commit()
}

// DropManaged deletes a connection's managed subscriptions (and their events)
// whose calendar is not in keep; nil keep drops them all.
func (s *Store) DropManaged(ctx context.Context, connID string, keep map[string]bool) (int, error) {
	list, err := s.ManagedOf(ctx, connID)
	if err != nil {
		return 0, err
	}
	n := 0
	for url, m := range list {
		if keep[url] {
			continue
		}
		if _, err := s.db.ExecContext(ctx, `DELETE FROM calendar_subscriptions WHERE id = ?`, m.SubscriptionID); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// StoreManaged replaces a managed subscription's events with occurrences read
// from its calendar, recording the calendar's version (CTag or sync token).
func (s *Store) StoreManaged(ctx context.Context, subscriptionID, version string, occ []Occurrence, warnings []string) error {
	return s.Success(ctx, subscriptionID, &Result{Replaced: true, Occurrences: occ, Warnings: warnings, ETag: version})
}

// EventRemote is where one event of a managed calendar lives on the remote
// account (calendar-event-edits D2): the CalDAV object address or Graph event
// id, the version last read, and why it can not be edited ("" when it can).
type EventRemote struct {
	Href   string
	ETag   string
	Locked string
}

// EventRef is a cached occurrence with its remote address.
type EventRef struct {
	SubscriptionID string
	UID            string
	Instance       string
	EventID        string
	EventRemote
}

// StoreManagedRefs is StoreManaged that also replaces the subscription's
// remote refs, one per stored occurrence, in the same transaction. refs is
// keyed by UID; an occurrence without an entry gets none.
func (s *Store) StoreManagedRefs(ctx context.Context, subscriptionID, version string, occ []Occurrence, warnings []string, refs map[string]EventRemote) error {
	return s.Success(ctx, subscriptionID, &Result{Replaced: true, Occurrences: occ, Warnings: warnings, ETag: version, Refs: refs})
}

// storeRefs replaces a subscription's refs inside the transaction that
// replaced its events. Feeds have none (Refs nil), and the delete is harmless.
func storeRefs(ctx context.Context, tx *sql.Tx, id string, r *Result) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM calendar_event_remote WHERE subscription_id = ?`, id); err != nil {
		return err
	}
	if len(r.Refs) == 0 {
		return nil
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT OR REPLACE INTO calendar_event_remote(subscription_id,uid,instance,event_id,href,etag,locked) VALUES(?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, o := range r.Occurrences {
		ref, ok := r.Refs[o.UID]
		if !ok {
			continue
		}
		if _, err := stmt.ExecContext(ctx, id, o.UID, o.Instance, EventID(id, o.UID, o.Instance), ref.Href, ref.ETag, ref.Locked); err != nil {
			return err
		}
	}
	return nil
}

// Refs lists a subscription's refs.
func (s *Store) Refs(ctx context.Context, subscriptionID string) ([]EventRef, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT subscription_id,uid,instance,event_id,href,etag,locked FROM calendar_event_remote WHERE subscription_id = ? ORDER BY uid, instance`, subscriptionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EventRef
	for rows.Next() {
		var r EventRef
		if err := rows.Scan(&r.SubscriptionID, &r.UID, &r.Instance, &r.EventID, &r.Href, &r.ETag, &r.Locked); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// RefByEvent returns the ref of one event of a subscription by the id the
// events route serves; ErrNotFound when it is not cached.
func (s *Store) RefByEvent(ctx context.Context, subscriptionID, eventID string) (EventRef, error) {
	var r EventRef
	err := s.db.QueryRowContext(ctx, `SELECT subscription_id,uid,instance,event_id,href,etag,locked FROM calendar_event_remote WHERE subscription_id = ? AND event_id = ?`,
		subscriptionID, eventID).Scan(&r.SubscriptionID, &r.UID, &r.Instance, &r.EventID, &r.Href, &r.ETag, &r.Locked)
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	return r, err
}

// OccurrenceOf is the occurrence of a single (not repeating) event, as the
// shown calendars store it.
func OccurrenceOf(ev Event) Occurrence {
	o := occurrence(ev, ev.Start)
	o.Instance = instanceKey(ev.Start)
	return o
}

// EventRowOf returns one stored occurrence as the events route serves it;
// ErrNotFound when it is not cached.
func (s *Store) EventRowOf(ctx context.Context, subscriptionID, uid, instance string) (EventRow, error) {
	r, err := scanEventRow(s.db.QueryRowContext(ctx, eventSelect+` WHERE e.subscription_id = ? AND e.uid = ? AND e.instance = ?`, subscriptionID, uid, instance))
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	return r, err
}

// ReplaceEvent records an edit made on the remote account in one transaction:
// the cached occurrence old is replaced by occ (nil drops it) with the remote
// state now, so the new id and ETag are served at once, and the subscription's
// version is cleared so the next cycle reads the calendar again. It returns the
// new ref (zero when dropped).
func (s *Store) ReplaceEvent(ctx context.Context, subscriptionID string, old EventRef, occ *Occurrence, remote EventRemote) (EventRef, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return EventRef{}, err
	}
	defer tx.Rollback()
	for _, q := range []string{`DELETE FROM calendar_events WHERE subscription_id = ? AND uid = ? AND instance = ?`,
		`DELETE FROM calendar_event_remote WHERE subscription_id = ? AND uid = ? AND instance = ?`} {
		if _, err := tx.ExecContext(ctx, q, subscriptionID, old.UID, old.Instance); err != nil {
			return EventRef{}, err
		}
	}
	var ref EventRef
	if occ != nil {
		sd, st, sz := pointParts(occ.Start)
		ed, et, ez := pointParts(occ.End)
		if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO calendar_events(subscription_id,uid,instance,summary,location,start_date,start_time,start_tz,end_date,end_time,end_tz,all_day,free,start_ms,end_ms)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, subscriptionID, occ.UID, occ.Instance, occ.Summary, occ.Location, sd, st, sz, ed, et, ez, occ.AllDay, occ.Free, pointMs(occ.Start), pointMs(occ.End)); err != nil {
			return EventRef{}, err
		}
		ref = EventRef{SubscriptionID: subscriptionID, UID: occ.UID, Instance: occ.Instance, EventID: EventID(subscriptionID, occ.UID, occ.Instance), EventRemote: remote}
		if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO calendar_event_remote(subscription_id,uid,instance,event_id,href,etag,locked) VALUES(?,?,?,?,?,?,?)`,
			subscriptionID, ref.UID, ref.Instance, ref.EventID, remote.Href, remote.ETag, remote.Locked); err != nil {
			return EventRef{}, err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE calendar_subscriptions SET etag = '', updated_at = ?, event_count = (SELECT COUNT(*) FROM calendar_events WHERE subscription_id = ?) WHERE id = ?`,
		s.now().UnixMilli(), subscriptionID, subscriptionID); err != nil {
		return EventRef{}, err
	}
	return ref, tx.Commit()
}
