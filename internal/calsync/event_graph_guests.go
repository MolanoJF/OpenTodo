package calsync

import (
	"github.com/OpenTodoApp/opentodo/internal/calfeed"
)

// Changing the guests of a Microsoft event and making a Teams meeting
// (calendar-event-guests D7, D8). Graph tells the guests itself on every organiser
// write and offers no way to stop it, so nothing is sent unless the caller confirmed
// with NotifyGuests, and the check comes before any request.

// tellsGuests reports whether writing edit to the event cur tells people: the user
// organises it and others are invited, or the edit invites someone.
func tellsGuests(cur calfeed.GuestInfo, edit EventEdit) bool {
	return (cur.IsOrganizer && cur.Count > 0) || (edit.Guests != nil && len(edit.Guests.Add) > 0)
}

// requireNotify refuses a write that tells people without the caller's confirmation;
// nothing has been sent when it returns an error.
func requireNotify(cur calfeed.GuestInfo, edit EventEdit) error {
	if tellsGuests(cur, edit) && !edit.NotifyGuests {
		return invalid("notifyGuests")
	}
	return nil
}

// graphGuest is an attendee as Graph writes it.
type graphGuest struct {
	EmailAddress graphAddress `json:"emailAddress"`
	Type         string       `json:"type"`
}

func graphType(role string) string {
	switch role {
	case calfeed.RoleOptional:
		return "optional"
	case calfeed.RoleRoom:
		return "resource"
	}
	return "required"
}

// attendeeBody is the attendee collection the event has after edit: the current
// guests and rooms kept with their address, name and type, the removals gone, the
// additions in. Graph replaces the whole collection, so the current list is what is
// sent. An error says which field of the body is wrong, never the address.
func attendeeBody(cur calfeed.GuestInfo, me string, edit *GuestsEdit) ([]graphGuest, error) {
	if cur.Truncated {
		return nil, invalid("guests") // the list is longer than what is read, rebuilding it would drop guests
	}
	organizer := ""
	if cur.Organizer != nil {
		organizer = cur.Organizer.Email
	}
	remove := map[string]bool{}
	for i, r := range edit.Remove {
		a, err := calfeed.NormalizeEmail(r)
		if err != nil || a == me || a == organizer {
			return nil, invalid("guests.remove")
		}
		_ = i
		remove[a] = true
	}
	var out []graphGuest
	index := map[string]int{}
	for _, g := range cur.Guests {
		if remove[g.Email] && g.Role != calfeed.RoleRoom { // a room is read-only and kept
			continue
		}
		index[g.Email] = len(out)
		out = append(out, graphGuest{EmailAddress: graphAddress{Name: g.Name, Address: g.Email}, Type: graphType(g.Role)})
	}
	for _, a := range edit.Add {
		email, err := calfeed.NormalizeEmail(a.Email)
		if err != nil || email == me || email == organizer {
			return nil, invalid("guests.add")
		}
		role := calfeed.RoleRequired
		if a.Optional {
			role = calfeed.RoleOptional
		}
		if i, ok := index[email]; ok { // already a guest: only the role is set
			if out[i].Type != "resource" {
				out[i].Type = graphType(role)
			}
			continue
		}
		index[email] = len(out)
		out = append(out, graphGuest{EmailAddress: graphAddress{Name: calfeed.GuestName(a.Name), Address: email}, Type: graphType(role)})
	}
	if len(out) > calfeed.MaxGuests {
		return nil, invalid("guests")
	}
	return out, nil
}

// guestMembers adds the attendees and online meeting members an edit asks for.
func (e graphEvents) guestMembers(body map[string]any, cur EventState, edit EventEdit) error {
	if edit.Guests != nil {
		list, err := attendeeBody(cur.Guests, e.me, edit.Guests)
		if err != nil {
			return err
		}
		body["attendees"] = list
	}
	if edit.OnlineMeeting && !cur.OnlineMeeting.On {
		body["isOnlineMeeting"], body["onlineMeetingProvider"] = true, teamsProvider
	}
	return nil
}

// onlineRefusal maps the refusal of a write that asked for an online meeting onto the
// field of the body (a calendar that lists no provider, M6).
func onlineRefusal(err error, asked bool) error {
	if err != nil && asked && reasonOf(err) == "http_400" {
		return invalid("onlineMeeting")
	}
	return err
}

// createGuestMembers adds the guests and the online meeting of a new event. Inviting
// tells people, so it needs the caller's confirmation like any other change.
func (e graphEvents) createGuestMembers(body map[string]any, ev EventNew) error {
	if len(ev.Guests) > 0 {
		if !ev.NotifyGuests {
			return invalid("notifyGuests")
		}
		list, err := attendeeBody(calfeed.GuestInfo{}, e.me, &GuestsEdit{Add: ev.Guests})
		if err != nil {
			return err
		}
		body["attendees"] = list
	}
	if ev.OnlineMeeting {
		body["isOnlineMeeting"], body["onlineMeetingProvider"] = true, teamsProvider
	}
	return nil
}
