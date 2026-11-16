package calsync

import (
	"net/url"
	"strings"

	"github.com/OpenTodoApp/opentodo/internal/calfeed"
)

// What OpenTodo does with the guests of a connection's events (OpenSpec change
// calendar-event-guests, design D1): derived in code from the connection, never
// stored, served on managed subscriptions and the basis of every guest rule. A
// capability that is not known to be true is false.

// GuestCaps is what OpenTodo does with the guests of a connection's events.
type GuestCaps struct {
	Respond bool `json:"respond"` // answer an invitation
	Silent  bool `json:"silent"`  // the answer can be sent without telling the organiser (sendResponse false)
	Edit    bool `json:"edit"`    // an organiser's change, guests added or removed, a delete: the provider tells the guests itself
}

// googleRespond is false until the smoke test of calendar-event-guests task 7.5
// records that Google keeps a PARTSTAT-only write and tells the organiser (open
// question G3). It is a variable so a test can turn it on; nothing else sets it.
var googleRespond = false

// GuestCaps says what the connection's provider is known to do.
func (c *Connection) GuestCaps() GuestCaps {
	switch c.Provider {
	case ProviderMicrosoft:
		return GuestCaps{Respond: true, Silent: true, Edit: true}
	case ProviderGoogle:
		return GuestCaps{Respond: googleRespond} // Edit stays false: open question G2
	}
	return GuestCaps{} // CalDAV: guests are read, nothing is written
}

// googleCalDAVPath is what precedes the calendar id in Google's CalDAV addresses.
const googleCalDAVPath = "/caldav/v2/"

// googleID is the id segment of a Google CalDAV address ("" when there is none).
func googleID(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	_, rest, ok := strings.Cut(u.Path, googleCalDAVPath)
	if !ok {
		return ""
	}
	id, _, _ := strings.Cut(rest, "/")
	return id
}

// ownerAddress is the user's own address on a calendar of the connection, "" when it
// is not known. Microsoft keeps the mailbox address in Username. Google's is the id
// segment of the principal address (falling back to the calendar home), and only for
// the calendar whose id equals it: a calendar shared in from a colleague has the
// colleague's id, and answering there would be answering as someone else. Generic
// CalDAV servers do not say.
func ownerAddress(c *Connection, calendarURL string) string {
	switch c.Provider {
	case ProviderMicrosoft:
		if u := strings.ToLower(strings.TrimSpace(c.Username)); strings.Contains(u, "@") {
			return u
		}
	case ProviderGoogle:
		id := googleID(c.PrincipalURL)
		if id == "" {
			id = googleID(c.HomeURL)
		}
		if strings.Contains(id, "@") && strings.EqualFold(googleID(calendarURL), id) {
			return strings.ToLower(id)
		}
	}
	return ""
}

// state is the event state a written object leaves.
func (o WrittenObject) state() EventState {
	st := EventState{Ref: o.Ref, Event: o.Events[0], Locked: o.Locked, Conference: o.Conference}
	if o.Guests != nil {
		st.Guests = *o.Guests
	}
	return st
}

// remoteOf is the ref the cache keeps for an event state read or written.
func remoteOf(href string, st EventState) calfeed.EventRemote {
	r := calfeed.EventRemote{Href: firstOf(st.Ref.Href, href), ETag: st.Ref.ETag, Locked: st.Locked, Conference: st.Conference}
	if !st.Guests.Empty() {
		g := st.Guests
		r.Guests = &g
	}
	return r
}

// notifies says that changing or deleting an event tells its guests: the user
// organises it, others are invited (a room counts) and the provider tells them itself.
func notifies(c *Connection, g *calfeed.GuestSummary) bool {
	return c.GuestCaps().Edit && g != nil && g.Organizer && g.Count > 0
}

// Decorate fills what the events route serves of an event of a connected calendar:
// its access and, for an event with guests, whether the user can answer it.
func Decorate(c *Connection, calendarURL string, row *calfeed.EventRow) {
	a := AccessOf(c, calendarURL, *row)
	row.Access = &a
	if row.Guests != nil {
		row.Guests.CanRespond = CanRespond(c, calendarURL, *row)
	}
}

// CanRespond says that the user can answer the event from here: the connection
// replies, edits are on, the calendar takes writes, the user is a guest (not the
// organiser) and the object is not one OpenTodo can not write back unchanged. It
// works on an event that is locked for edits: a reply is the one write it allows.
func CanRespond(c *Connection, calendarURL string, row calfeed.EventRow) bool {
	cal := c.Calendar(calendarURL)
	switch {
	case !c.GuestCaps().Respond, !c.EditEvents, cal == nil, !cal.EffectiveCaps().Update:
		return false
	case row.Guests == nil || row.Guests.You == "" || row.Guests.Organizer:
		return false
	case !row.Known, row.Locked == LockSeriesUnsupported, row.Locked == LockNoETag, row.Locked == lockRecurringLegacy:
		return false
	}
	return true
}

// tellsPeople says that applying edit to the event tells its guests: the provider
// tells them of any change to an organiser's meeting, and of anyone invited.
func tellsPeople(t *eventTarget, edit EventEdit) bool {
	return notifies(t.conn, t.guests) || (t.conn.GuestCaps().Edit && edit.Guests != nil && len(edit.Guests.Add) > 0)
}
