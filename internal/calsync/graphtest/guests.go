package graphtest

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

// Guests, replies and online meetings of the fake (OpenSpec change
// calendar-event-guests, design D7 and D18). The fake sends a message exactly when
// the real service is documented to: a reply with sendResponse true, an update to
// the attendees an organiser's PATCH of attendees changes (M2), an update to every
// attendee for any other change of the organiser's meeting (M8 is unknown, so none
// is assumed silent), one update per surviving exception of a master (M2) and a
// cancellation on the organiser's delete (M3).

// Message is a message the simulated service sent to a person.
type Message struct {
	Kind string // reply, update or cancel
	To   string // the address, lower case
}

// Messages lists the messages sent, in order.
func (s *Server) Messages() []Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Message(nil), s.messages...)
}

// AssertNoMessages fails the test when any message was sent.
func (s *Server) AssertNoMessages(t testing.TB) {
	t.Helper()
	if m := s.Messages(); len(m) > 0 {
		t.Errorf("the service sent %d message(s): %+v", len(m), m)
	}
}

type inAttendee struct {
	EmailAddress struct {
		Address string `json:"address"`
		Name    string `json:"name"`
	} `json:"emailAddress"`
	Type string `json:"type"`
}

func (s *Server) send(kind string, to ...string) {
	for _, a := range to {
		s.messages = append(s.messages, Message{Kind: kind, To: strings.ToLower(a)})
	}
}

// guestList is every attendee of an event: the plain addresses first (required, no
// answer yet), then the guests.
func guestList(e *Event) []Attendee {
	var out []Attendee
	for _, a := range e.Attendees {
		out = append(out, Attendee{Address: a, Type: "required", Response: "notResponded"})
	}
	return append(out, e.Guests...)
}

func addresses(list []Attendee) []string {
	var out []string
	for _, a := range list {
		out = append(out, strings.ToLower(a.Address))
	}
	return out
}

func (s *Server) renderGuests(e *Event) []map[string]any {
	out := []map[string]any{}
	for _, a := range guestList(e) {
		ty, resp := a.Type, a.Response
		if ty == "" {
			ty = "required"
		}
		if resp == "" {
			resp = "notResponded"
		}
		out = append(out, map[string]any{"type": ty, "status": map[string]string{"response": resp},
			"emailAddress": map[string]string{"address": a.Address, "name": a.Name}})
	}
	return out
}

// renderMeeting adds the organiser, the user's own answer and the online meeting.
func (s *Server) renderMeeting(e *Event, out map[string]any) {
	org := Person{Name: "Me", Address: s.Address}
	resp := "organizer"
	if e.NotOrganizer {
		org = e.Organizer
		if org.Address == "" {
			org = Person{Name: "Organizer", Address: "organizer@example.com"}
		}
		resp = firstNonEmpty(e.ResponseStatus, "notResponded")
	}
	out["organizer"] = map[string]any{"emailAddress": map[string]string{"name": org.Name, "address": org.Address}}
	out["responseStatus"] = map[string]string{"response": resp}
	out["isOnlineMeeting"] = e.IsOnlineMeeting || e.JoinURL != ""
	if e.JoinURL != "" {
		out["onlineMeeting"] = map[string]string{"joinUrl": e.JoinURL}
		out["onlineMeetingProvider"] = firstNonEmpty(e.Provider, "teamsForBusiness")
	} else {
		out["onlineMeeting"] = nil
	}
}

// applyGuests replaces the attendee collection when the body has one (Graph keeps the
// answer of a retained attendee) and returns the addresses added or changed and
// removed.
func applyGuests(e *Event, in *input) (changed, removed []string) {
	if in.Attendees == nil {
		return nil, nil
	}
	before := map[string]Attendee{}
	for _, a := range guestList(e) {
		before[strings.ToLower(a.Address)] = a
	}
	e.Attendees = nil
	e.Guests = nil
	seen := map[string]bool{}
	for _, a := range *in.Attendees {
		addr := strings.ToLower(a.EmailAddress.Address)
		if addr == "" || seen[addr] {
			continue
		}
		seen[addr] = true
		g := Attendee{Address: a.EmailAddress.Address, Name: a.EmailAddress.Name, Type: firstNonEmpty(a.Type, "required"), Response: "notResponded"}
		prev, had := before[addr]
		if had {
			g.Response = firstNonEmpty(prev.Response, "notResponded")
			if g.Name == "" {
				g.Name = prev.Name
			}
		}
		if !had || firstNonEmpty(prev.Type, "required") != g.Type {
			changed = append(changed, addr)
		}
		e.Guests = append(e.Guests, g)
	}
	for addr := range before {
		if !seen[addr] {
			removed = append(removed, addr)
		}
	}
	return changed, removed
}

// meetingAllowed says whether a body that asks for an online meeting names a provider
// the calendar lists (anything else is a 400, checked before the event changes).
func meetingAllowed(in *input, cal *Calendar) bool {
	if in.OnlineMeeting == nil || !*in.OnlineMeeting {
		return true
	}
	provider := "teamsForBusiness"
	if in.Provider != nil {
		provider = *in.Provider
	}
	for _, p := range cal.OnlineProviders {
		if p == provider {
			return true
		}
	}
	return false
}

// applyMeeting turns the event into an online meeting when the body asks for it (the
// provider was checked by meetingAllowed).
func (s *Server) applyMeeting(e *Event, in *input) {
	if in.OnlineMeeting == nil || !*in.OnlineMeeting {
		return
	}
	provider := "teamsForBusiness"
	if in.Provider != nil {
		provider = *in.Provider
	}
	if e.JoinURL == "" {
		e.JoinURL = "https://teams.microsoft.com/l/meetup-join/meeting-" + strconv.Itoa(s.seq+1)
	}
	e.IsOnlineMeeting, e.Provider = true, provider
}

// exceptionsOf counts the exceptions a master keeps.
func (s *Server) exceptionsOf(masterID string) int {
	n := 0
	for _, x := range s.events {
		if x.SeriesMasterID == masterID && x.Type == "exception" {
			n++
		}
	}
	return n
}

// noteEdit records the messages an organiser's PATCH sends; before is the number of
// attendees the event had.
func (s *Server) noteEdit(e *Event, in *input, changed, removed []string) {
	if e.NotOrganizer {
		return
	}
	all := addresses(guestList(e))
	if len(all) == 0 && len(removed) == 0 {
		return
	}
	attendeesOnly := in.Attendees != nil && !in.changesAnythingButGuests()
	if attendeesOnly {
		s.send("update", changed...)
		s.send("cancel", removed...)
	} else {
		s.send("update", all...)
		s.send("cancel", removed...)
	}
	if e.Type == "seriesMaster" {
		for i := 0; i < s.exceptionsOf(e.ID); i++ {
			s.send("update", all...)
		}
	}
}

// changesAnythingButGuests says the body carries a member other than attendees.
func (in *input) changesAnythingButGuests() bool {
	return in.Subject != nil || in.ShowAs != nil || in.Location != nil || in.Body != nil || in.Start != nil || in.End != nil ||
		in.IsAllDay != nil || in.Recurrence != nil || in.ReminderMin != nil || in.ReminderOn != nil || in.Importance != nil ||
		in.Sensitivity != nil || in.Categories != nil || in.OnlineMeeting != nil
}

// respond is POST /me/events/{id}/accept, tentativelyAccept and decline.
func (s *Server) respond(action string) func(w http.ResponseWriter, r *http.Request) {
	answers := map[string]string{"accept": "accepted", "tentativelyAccept": "tentativelyAccepted", "decline": "declined"}
	return func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Comment      string `json:"comment"`
			SendResponse *bool  `json:"sendResponse"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		s.mu.Lock()
		defer s.mu.Unlock()
		e := s.find(r.PathValue("id"))
		switch {
		case e == nil:
			graphError(w, 404, "ErrorItemNotFound")
			return
		case !e.NotOrganizer: // the organiser cannot answer their own meeting
			graphError(w, 400, "ErrorInvalidRequest")
			return
		}
		if e.Type == "occurrence" { // an occurrence's answer is an exception of its series
			e.Type = "exception"
		}
		e.ResponseStatus = answers[action]
		e.Modified = s.now()
		s.store(e)
		if in.SendResponse == nil || *in.SendResponse {
			org := e.Organizer.Address
			if org == "" {
				org = "organizer@example.com"
			}
			s.send("reply", org)
		}
		w.WriteHeader(http.StatusAccepted)
	}
}

// ClearMessages forgets the messages sent so far.
func (s *Server) ClearMessages() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = nil
}
