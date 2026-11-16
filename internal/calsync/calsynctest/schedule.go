package calsynctest

import (
	"net/http"
	"strings"
	"testing"

	"github.com/OpenTodoApp/opentodo/internal/caldav"
)

// Scheduling is what the fake server does about invitations (calendar-event-guests
// D18): the behaviour of real servers is not the same, and a test must show that
// OpenTodo never writes something that makes one of them send a message to a person.
type Scheduling int

const (
	// SchedulingNone is a server that does nothing (Radicale).
	SchedulingNone Scheduling = iota
	// SchedulingAuto is a sabre-like server: a PUT that changes only an attendee's own
	// PARTSTAT records a reply to the organiser, a PUT of the organiser that changes
	// attendees or a shared property records an update to every other attendee, and a
	// DELETE of the organiser's event a cancellation.
	SchedulingAuto
	// SchedulingGoogle is SchedulingAuto plus the two switches of Server:
	// KeepsPartstat (when off, the attendee's PARTSTAT is rewritten to NEEDS-ACTION on
	// write) and RejectsAttendeeEdits (an attendee's change other than PARTSTAT is a
	// 403 with the allowed-attendee-scheduling-object-change precondition).
	SchedulingGoogle
)

// Message is a message the simulated server sent to a person.
type Message struct {
	Kind string // reply, update or cancel
	To   string // the address, as the object holds it (lower case)
}

// Messages lists the messages the server sent, in order.
func (s *Server) Messages() []Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Message(nil), s.messages...)
}

// AssertNoMessages fails the test when the server sent any message.
func (s *Server) AssertNoMessages(t testing.TB) {
	t.Helper()
	if m := s.Messages(); len(m) > 0 {
		t.Errorf("the server sent %d message(s): %+v", len(m), m)
	}
}

const attendeeChangeDenied = `<?xml version="1.0"?><d:error xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav"><c:allowed-attendee-scheduling-object-change/></d:error>`

func addrOf(v string) string {
	v = strings.TrimSpace(v)
	if len(v) >= 7 && strings.EqualFold(v[:7], "mailto:") {
		v = v[7:]
	}
	return strings.ToLower(v)
}

func vevents(root *caldav.Component) []*caldav.Component {
	var out []*caldav.Component
	for _, c := range root.Children {
		if c.Name == "VEVENT" {
			out = append(out, c)
		}
	}
	return out
}

func organizerOf(root *caldav.Component) string {
	for _, v := range vevents(root) {
		if p := v.Prop("ORGANIZER"); p != nil {
			return addrOf(p.Value)
		}
	}
	return ""
}

func attendeesOf(root *caldav.Component) []string {
	var out []string
	seen := map[string]bool{}
	for _, v := range vevents(root) {
		for _, p := range v.Props {
			if p.Name == "ATTENDEE" && !seen[addrOf(p.Value)] {
				seen[addrOf(p.Value)] = true
				out = append(out, addrOf(p.Value))
			}
		}
	}
	return out
}

// shared lists the lines of an object with the owner's PARTSTAT left out, so two
// objects that differ only in it list the same.
func shared(root *caldav.Component, owner string) []string {
	var out []string
	for _, c := range root.Children {
		for _, p := range c.Props {
			if c.Name == "VEVENT" && p.Name == "ATTENDEE" && addrOf(p.Value) == owner {
				var keep []caldav.Param
				for _, pa := range p.Params {
					if !strings.EqualFold(pa.Name, "PARTSTAT") {
						keep = append(keep, pa)
					}
				}
				p.Params = keep
			}
			out = append(out, c.Name+" "+p.Line())
		}
	}
	return out
}

func sameLines(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (s *Server) send(kind string, to ...string) {
	for _, t := range to {
		s.messages = append(s.messages, Message{Kind: kind, To: t})
	}
}

func others(list []string, owner string) []string {
	var out []string
	for _, a := range list {
		if a != owner {
			out = append(out, a)
		}
	}
	return out
}

// schedule applies the server's scheduling to a PUT of data over old (nil for a new
// object): it records the messages it would send and returns the data it stores. A
// status other than 0 refuses the write with that status and the body.
func (s *Server) schedule(old *Object, data string) (stored string, status int, body string) {
	if s.Scheduling == SchedulingNone {
		return data, 0, ""
	}
	nroot, err := caldav.Parse([]byte(data))
	if err != nil {
		return data, 0, ""
	}
	owner := strings.ToLower(s.Owner)
	org := organizerOf(nroot)
	guest := org != "" && org != owner // the writer is an attendee, not the organiser
	var oroot *caldav.Component
	if old != nil {
		oroot, _ = caldav.Parse([]byte(old.Data))
	}
	if oroot == nil {
		if !guest {
			s.send("update", others(attendeesOf(nroot), owner)...)
		}
		return data, 0, ""
	}
	if string(oroot.Encode()) == string(nroot.Encode()) {
		return data, 0, ""
	}
	if sameLines(shared(oroot, owner), shared(nroot, owner)) { // only the owner's PARTSTAT
		s.send("reply", org)
		if s.Scheduling == SchedulingGoogle && !s.KeepsPartstat {
			for _, v := range vevents(nroot) {
				for i := range v.Props {
					if v.Props[i].Name == "ATTENDEE" && addrOf(v.Props[i].Value) == owner {
						for j := range v.Props[i].Params {
							if strings.EqualFold(v.Props[i].Params[j].Name, "PARTSTAT") {
								v.Props[i].Params[j].Values = []string{"NEEDS-ACTION"}
							}
						}
					}
				}
			}
			return string(nroot.Encode()), 0, ""
		}
		return data, 0, ""
	}
	if guest && s.Scheduling == SchedulingGoogle && s.RejectsAttendeeEdits {
		return "", http.StatusForbidden, attendeeChangeDenied
	}
	if !guest {
		s.send("update", others(attendeesOf(nroot), owner)...)
	}
	return data, 0, ""
}

// scheduleDelete records what deleting the object tells people.
func (s *Server) scheduleDelete(old *Object) {
	if s.Scheduling == SchedulingNone || old == nil {
		return
	}
	root, err := caldav.Parse([]byte(old.Data))
	if err != nil {
		return
	}
	owner := strings.ToLower(s.Owner)
	switch org := organizerOf(root); {
	case org == "" || len(attendeesOf(root)) == 0:
	case org == owner:
		s.send("cancel", others(attendeesOf(root), owner)...)
	default:
		s.send("reply", org)
	}
}
