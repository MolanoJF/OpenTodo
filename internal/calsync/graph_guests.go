package calsync

import (
	"strings"

	"github.com/OpenTodoApp/opentodo/internal/calfeed"
)

// Guests, replies and join links as Graph reads them (calendar-event-guests D7).

type graphAddress struct {
	Name    string `json:"name"`
	Address string `json:"address"`
}

type graphPerson struct {
	EmailAddress graphAddress `json:"emailAddress"`
}

type graphResponse struct {
	Response string `json:"response"`
}

type graphOnlineMeet struct {
	JoinURL string `json:"joinUrl"`
}

// graphAttendee is one attendee of an event.
type graphAttendee struct {
	Type         string        `json:"type"`
	Status       graphResponse `json:"status"`
	EmailAddress graphAddress  `json:"emailAddress"`
}

// graphAnswer maps a Graph response onto the answers of the API.
func graphAnswer(r string) string {
	switch strings.ToLower(r) {
	case "accepted", "organizer":
		return calfeed.ResponseAccepted
	case "tentativelyaccepted":
		return calfeed.ResponseTentative
	case "declined":
		return calfeed.ResponseDeclined
	}
	return calfeed.ResponseNeedsAction
}

func graphRole(t string) string {
	switch strings.ToLower(t) {
	case "optional":
		return calfeed.RoleOptional
	case "resource":
		return calfeed.RoleRoom
	}
	return calfeed.RoleRequired
}

// guests is what the event says of its guests; me is the mailbox address. The user's
// own answer is the event's responseStatus (no address matching); the user's row in
// attendees is found by me.
func (e *graphEvent) guests(me string) calfeed.GuestInfo {
	var g calfeed.GuestInfo
	me = strings.ToLower(strings.TrimSpace(me))
	g.IsOrganizer = e.IsOrganizer != nil && *e.IsOrganizer
	if g.IsOrganizer && len(e.Attendees) == 0 {
		return calfeed.GuestInfo{} // the user's own event with nobody invited says nothing about guests
	}
	if o := e.Organizer; o != nil {
		if email, err := calfeed.NormalizeEmail(o.EmailAddress.Address); err == nil {
			g.Organizer = &calfeed.Guest{Name: calfeed.GuestName(o.EmailAddress.Name), Email: email, Role: calfeed.RoleRequired,
				Response: calfeed.ResponseAccepted, You: g.IsOrganizer || (me != "" && email == me)}
		}
	}
	if e.IsOrganizer != nil && !*e.IsOrganizer && e.ResponseStatus != nil && strings.ToLower(e.ResponseStatus.Response) != "organizer" {
		g.You = graphAnswer(e.ResponseStatus.Response)
	}
	for _, a := range e.Attendees {
		email, err := calfeed.NormalizeEmail(a.EmailAddress.Address)
		if err != nil {
			continue
		}
		you := me != "" && email == me
		if !you {
			g.Count++
		}
		if len(g.Guests) < calfeed.MaxGuests {
			g.Guests = append(g.Guests, calfeed.Guest{Name: calfeed.GuestName(a.EmailAddress.Name), Email: email, Role: graphRole(a.Type),
				Response: graphAnswer(a.Status.Response), You: you})
		} else {
			g.Truncated = true
		}
	}
	return g
}

// conference is the join address: the online meeting's joinUrl (a provider field,
// trusted), then an address in the location.
func (e *graphEvent) conference() *calfeed.Conference {
	if m := e.OnlineMeeting; m != nil {
		if c, ok := calfeed.ParseConference(m.JoinURL, true); ok {
			return &c
		}
	}
	if e.Location != nil {
		if c, ok := calfeed.ConferenceIn(e.Location.DisplayName); ok {
			return &c
		}
	}
	return nil
}

// guestPtr is the cache form of what an event says of its guests, nil for nothing.
func guestPtr(g calfeed.GuestInfo) *calfeed.GuestInfo {
	if g.Empty() {
		return nil
	}
	return &g
}

// teamsProvider is the online meeting provider OpenTodo makes meetings with.
const teamsProvider = "teamsForBusiness"

// teamsListed reports whether a calendar lists the Teams provider (M5: a meeting is
// made only with a provider the calendar supports; personal calendars may list none, M6).
func teamsListed(providers []string) bool {
	for _, p := range providers {
		if strings.EqualFold(p, teamsProvider) {
			return true
		}
	}
	return false
}
