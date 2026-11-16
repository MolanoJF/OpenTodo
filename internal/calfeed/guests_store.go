package calfeed

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

// The guest cache (calendar-event-guests D2, D12): one row per cached occurrence that
// has guests, an organiser or a conference, in calendar_event_guests. It is server
// data, never an op, and personal data of people who are not users of this server:
// nothing here logs a name, an address or a link.

// GuestRow is what the cache keeps of an occurrence's guests (migration 0077).
type GuestRow struct {
	Info       GuestInfo
	Conference *Conference
}

// GuestSummary is what the events route and the devices' cache carry of an
// occurrence's guests: counts and the user's own answer, never an address.
type GuestSummary struct {
	Count      int    `json:"count"`
	You        string `json:"you,omitempty"`
	Organizer  bool   `json:"organizer"`
	CanRespond bool   `json:"canRespond"`
}

type organizerJSON struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

// storeGuests writes the guest row of one cached occurrence; a remote with neither
// guests nor a conference leaves none. The caps of D2 are applied here as well.
func storeGuests(ctx context.Context, tx *sql.Tx, subscriptionID, uid, instance string, r EventRemote) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM calendar_event_guests WHERE subscription_id = ? AND uid = ? AND instance = ?`, subscriptionID, uid, instance); err != nil {
		return err
	}
	var g GuestInfo
	if r.Guests != nil {
		g = *r.Guests
	}
	conf := r.Conference
	if conf != nil && (len(conf.URL) > MaxConferenceURL || conf.URL == "") {
		conf = nil
	}
	if g.Empty() && conf == nil {
		return nil
	}
	if len(g.Guests) > MaxGuests {
		g.Guests = g.Guests[:MaxGuests]
	}
	org := ""
	if g.Organizer != nil {
		b, err := json.Marshal(organizerJSON{Name: g.Organizer.Name, Email: g.Organizer.Email})
		if err != nil {
			return err
		}
		org = string(b)
	}
	list := ""
	if len(g.Guests) > 0 {
		b, err := json.Marshal(g.Guests)
		if err != nil {
			return err
		}
		list = string(b)
	}
	curl, kind := "", ""
	if conf != nil {
		curl, kind = conf.URL, conf.Provider
	}
	_, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO calendar_event_guests(subscription_id,uid,instance,organizer,is_organizer,you,guest_count,attendees,conference_url,conference_kind)
		VALUES(?,?,?,?,?,?,?,?,?,?)`, subscriptionID, uid, instance, org, g.IsOrganizer, g.You, g.Count, list, curl, kind)
	return err
}

// GuestsOf reads one occurrence's guest row for the cache-only route; ok is false
// when the event is not the user's, is not cached or has none.
func (s *Store) GuestsOf(ctx context.Context, userID, eventID string) (GuestRow, bool, error) {
	var org, list, curl, kind, you string
	var isOrg bool
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT g.organizer, g.is_organizer, g.you, g.guest_count, g.attendees, g.conference_url, g.conference_kind
		FROM calendar_event_remote r JOIN calendar_subscriptions s ON s.id = r.subscription_id
		JOIN calendar_event_guests g ON g.subscription_id = r.subscription_id AND g.uid = r.uid AND g.instance = r.instance
		WHERE s.user_id = ? AND r.event_id = ?`, userID, eventID).Scan(&org, &isOrg, &you, &count, &list, &curl, &kind)
	if errors.Is(err, sql.ErrNoRows) {
		return GuestRow{}, false, nil
	}
	if err != nil {
		return GuestRow{}, false, err
	}
	row := GuestRow{Info: GuestInfo{IsOrganizer: isOrg, You: you, Count: count}}
	if org != "" {
		var o organizerJSON
		if json.Unmarshal([]byte(org), &o) == nil && o.Email != "" {
			row.Info.Organizer = &Guest{Name: o.Name, Email: o.Email, Role: RoleRequired, Response: ResponseAccepted, You: isOrg}
		}
	}
	if list != "" {
		if err := json.Unmarshal([]byte(list), &row.Info.Guests); err != nil {
			row.Info.Guests = nil
		}
	}
	if len(row.Info.Guests) > MaxGuests {
		row.Info.Guests = row.Info.Guests[:MaxGuests]
	}
	listed := len(row.Info.Guests)
	if you != "" {
		listed-- // the user's own line is not part of Count
	}
	row.Info.Truncated = listed < count
	if curl != "" {
		row.Conference = &Conference{URL: curl, Provider: kind}
	}
	return row, true, nil
}
