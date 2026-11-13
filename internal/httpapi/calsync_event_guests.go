package httpapi

import (
	"fmt"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/OpenTodoApp/opentodo/internal/audit"
	"github.com/OpenTodoApp/opentodo/internal/calfeed"
	"github.com/OpenTodoApp/opentodo/internal/calsync"
)

// Guests, replies and join links of a connected calendar's events (OpenSpec change
// calendar-event-guests, design D3, D8 and D12). The body rules need nothing but the
// body; whether the provider can do it, and whether a change needs the caller's
// confirmation, are the Worker's. A guest's name and address are personal data of
// someone who is not a user of this server: an error names the field and the index,
// never the value, and nothing here logs one.

const (
	maxGuestEdits = 50 // entries in guests.add and in guests.remove
	maxGuestRole  = 16
)

// guestInput is a guest to add (or whose role to set) as the client sends it.
type guestInput struct {
	Email string `json:"email"`
	Name  string `json:"name"`
	Role  string `json:"role"` // required (default) or optional
}

// guestsPatch is the guests member of a PATCH.
type guestsPatch struct {
	Add    []guestInput `json:"add"`
	Remove []string     `json:"remove"`
}

// guestFieldError names a field of the body with its index, never the value.
func guestField(list string, i int, member string) string {
	return fmt.Sprintf("guests.%s[%d].%s", list, i, member)
}

// guestAdds validates guests to add: a bare address (calfeed.NormalizeEmail), a name
// of at most 120 characters without control characters, a role required or optional,
// no duplicate address. prefix is the body's path to the list.
func guestAdds(list []guestInput, prefix string) ([]calsync.GuestAdd, string, string) {
	if len(list) > maxGuestEdits {
		return nil, prefix, fmt.Sprintf("send at most %d guests at a time", maxGuestEdits)
	}
	out := make([]calsync.GuestAdd, 0, len(list))
	seen := map[string]bool{}
	for i, g := range list {
		field := func(member string) string {
			return strings.Replace(guestField("add", i, member), "guests.add", prefix, 1)
		}
		email, err := calfeed.NormalizeEmail(g.Email)
		switch {
		case err != nil:
			return nil, field("email"), "enter an email address: local@domain, no name, no comma, no angle brackets"
		case seen[email]:
			return nil, field("email"), "each guest is listed once"
		}
		seen[email] = true
		name := strings.TrimSpace(g.Name)
		if utf8.RuneCountInString(name) > calfeed.MaxGuestName || strings.IndexFunc(name, unicode.IsControl) >= 0 {
			return nil, field("name"), "enter a name of at most 120 characters"
		}
		role := g.Role
		if len(role) > maxGuestRole || (role != "" && role != calfeed.RoleRequired && role != calfeed.RoleOptional) {
			return nil, field("role"), "role is required or optional"
		}
		out = append(out, calsync.GuestAdd{Email: email, Name: name, Optional: role == calfeed.RoleOptional})
	}
	return out, "", ""
}

// guestsEditOf validates the guests member of a PATCH.
func guestsEditOf(in *guestsPatch) (*calsync.GuestsEdit, string, string) {
	if len(in.Add) == 0 && len(in.Remove) == 0 {
		return nil, "guests", "nothing to change: send guests to add or remove"
	}
	add, field, msg := guestAdds(in.Add, "guests.add")
	if field != "" {
		return nil, field, msg
	}
	if len(in.Remove) > maxGuestEdits {
		return nil, "guests.remove", fmt.Sprintf("send at most %d guests at a time", maxGuestEdits)
	}
	edit := &calsync.GuestsEdit{Add: add}
	seen := map[string]bool{}
	for _, a := range add {
		seen[a.Email] = true
	}
	for i, r := range in.Remove {
		email, err := calfeed.NormalizeEmail(r)
		switch {
		case err != nil:
			return nil, fmt.Sprintf("guests.remove[%d]", i), "enter an email address: local@domain"
		case seen[email]:
			return nil, fmt.Sprintf("guests.remove[%d]", i), "an address is added or removed, not both, and once"
		}
		seen[email] = true
		edit.Remove = append(edit.Remove, email)
	}
	return edit, "", ""
}

// onlineMeetingOf reads the onlineMeeting member: true asks for one, false is refused
// because an online meeting can not be turned off once made (Graph forbids it).
func onlineMeetingOf(v *bool) (bool, string, string) {
	switch {
	case v == nil:
		return false, "", ""
	case !*v:
		return false, "onlineMeeting", "an online meeting can not be removed once it is made"
	}
	return true, "", ""
}

// guestParts sets the guests, notifyGuests and onlineMeeting members of a PATCH on the
// edit; field and message name the first thing wrong.
func (in eventPatch) guestParts(edit *calsync.EventEdit) (string, string) {
	if in.Guests != nil {
		g, field, msg := guestsEditOf(in.Guests)
		if field != "" {
			return field, msg
		}
		edit.Guests = g
	}
	on, field, msg := onlineMeetingOf(in.OnlineMeeting)
	if field != "" {
		return field, msg
	}
	edit.OnlineMeeting = on
	edit.NotifyGuests = in.NotifyGuests != nil && *in.NotifyGuests
	return "", ""
}

// guestParts sets the guests, notifyGuests and onlineMeeting members of a create.
func (in eventCreate) guestParts(ev *calsync.EventNew) (string, string) {
	adds, field, msg := guestAdds(in.Guests, "guests")
	if field != "" {
		return field, msg
	}
	if len(adds) > calfeed.MaxGuests {
		return "guests", "an event holds at most 100 guests"
	}
	on, field, msg := onlineMeetingOf(in.OnlineMeeting)
	if field != "" {
		return field, msg
	}
	ev.Guests, ev.OnlineMeeting = adds, on
	ev.NotifyGuests = in.NotifyGuests != nil && *in.NotifyGuests
	return "", ""
}

// notifyParam reads notifyGuests of a DELETE: only "true" confirms.
func notifyParam(r *http.Request) (bool, string, string) {
	switch strings.TrimSpace(r.URL.Query().Get("notifyGuests")) {
	case "", "false":
		return false, "", ""
	case "true":
		return true, "", ""
	}
	return false, "notifyGuests", "notifyGuests is true or false"
}

// eventResponse is the body of POST .../events/{eventId}/response.
type eventResponse struct {
	Rev          string  `json:"rev"`
	Response     string  `json:"response"`
	Scope        *string `json:"scope"`
	SendResponse *bool   `json:"sendResponse"`
}

// replyOf validates a reply body; field and message name the first thing wrong.
func (in eventResponse) replyOf() (calsync.ReplyEdit, string, string) {
	reply := calsync.ReplyEdit{Response: calsync.Answer(in.Response), SendResponse: in.SendResponse == nil || *in.SendResponse}
	switch {
	case strings.TrimSpace(in.Rev) == "":
		return reply, "rev", "send the rev of the event you saw"
	case !reply.Response.Valid():
		return reply, "response", "response is accepted, tentative or declined"
	}
	if in.Scope != nil {
		switch sc := calfeed.Scope(*in.Scope); sc {
		case calfeed.ScopeThis, calfeed.ScopeAll:
			reply.Scope = sc
		default:
			return reply, "scope", "scope is this or all"
		}
	}
	return reply, "", ""
}

func (s *Server) handleRespondCalendarEvent(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), userFrom(r.Context()).ID
	var in eventResponse
	if err := readJSON(r, &in); err != nil {
		badBodyWith(w, err, "invalid_input", `body must be {"rev": "...", "response": "accepted", "scope"?: "all", "sendResponse"?: true}`)
		return
	}
	reply, field, msg := in.replyOf()
	if field != "" {
		writeFieldError(w, field, msg)
		return
	}
	if !s.calsyncEventLimiter.allow(uid) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many calendar event changes; try again later")
		return
	}
	connID := r.PathValue("id")
	res, err := s.calsync.RespondEvent(ctx, uid, connID, r.PathValue("eventId"), in.Rev, reply)
	if err != nil {
		s.eventFailure(w, err)
		return
	}
	s.audit.Record(ctx, audit.CalendarEventResponded(uid, connID, res.Provider, string(reply.Response), string(reply.Scope), reply.SendResponse))
	out := map[string]any{"event": res.Event, "replaces": res.Replaces}
	seriesAnswer(out, res)
	writeJSON(w, http.StatusOK, out)
}

// guestListOf is the guest list as the cache-only route serves it.
func guestListOf(row calfeed.GuestRow) map[string]any {
	g := row.Info
	guests := make([]map[string]any, 0, len(g.Guests))
	for _, x := range g.Guests {
		guests = append(guests, map[string]any{"name": x.Name, "email": x.Email, "role": x.Role, "response": x.Response, "you": x.You})
	}
	out := map[string]any{"guests": guests, "count": g.Count, "truncated": g.Truncated}
	if g.Organizer != nil {
		out["organizer"] = map[string]any{"name": g.Organizer.Name, "email": g.Organizer.Email, "you": g.Organizer.You}
	} else {
		out["organizer"] = nil
	}
	return out
}

// handleEventGuests serves one occurrence's guest list from the cache, with no request
// to the calendar. It is held in memory by the client and never stored there, so the
// response must not be cached either.
func (s *Server) handleEventGuests(w http.ResponseWriter, r *http.Request) {
	row, ok, err := s.calWorker.Store.GuestsOf(r.Context(), userFrom(r.Context()).ID, r.PathValue("eventId"))
	if err != nil {
		s.internal(w, err)
		return
	}
	if !ok || (row.Info.Organizer == nil && len(row.Info.Guests) == 0) {
		writeError(w, http.StatusNotFound, "not_found", "no guests are known for this event")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, guestListOf(row))
}

// detailGuests is the guests member of the detail route: the guest list as the calendar
// holds it now, the user's own answer and what the user may do with it.
func detailGuests(d *calsync.EventDetail) map[string]any {
	out := guestListOf(calfeed.GuestRow{Info: d.Guests})
	out["you"] = d.Guests.You
	out["canRespond"], out["canEdit"] = d.CanRespond, d.CanEdit
	return out
}

// guestReason explains an invalid_input about the guests members by its field; the
// fields name the member and the index, never a value.
func guestReason(field string) string {
	switch {
	case field == "notifyGuests":
		return "this change tells the guests, and the calendar sends the message itself: send notifyGuests true to confirm it"
	case field == "onlineMeeting":
		return "an online meeting can only be made, on a calendar that offers one, and can not be removed"
	case field == "response":
		return "response is accepted, tentative or declined"
	case field == "sendResponse":
		return "this calendar always tells the organiser: sendResponse false is not available"
	case field == "guests" || strings.HasPrefix(field, "guests."):
		return "the guests of this event can not be changed this way: the calendar does not allow it, an address is the user's own or the organiser's, or the event would hold more than 100 guests"
	}
	return ""
}
