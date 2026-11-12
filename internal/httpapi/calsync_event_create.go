package httpapi

import (
	"net/http"
	"strings"

	"github.com/OpenTodoApp/opentodo/internal/audit"
	"github.com/OpenTodoApp/opentodo/internal/calsync"
)

// Creating an event in a connected calendar and reading one with its
// description (OpenSpec change calendar-event-editor, design D4). The checks,
// the replay rule, the remote write and the cache are
// calsync.Worker.CreateEvent and EventDetail; this file is the request, the
// body rules and the answer.

const maxCalendarURL = 2048

// eventCreate is the body of POST .../events.
type eventCreate struct {
	ClientID    string      `json:"clientId"`
	CalendarURL string      `json:"calendarUrl"`
	Summary     string      `json:"summary"`
	Start       *eventPoint `json:"start"`
	End         *eventPoint `json:"end"`
	AllDay      *bool       `json:"allDay"`
	Location    string      `json:"location"`
	Description string      `json:"description"`
	Free        bool        `json:"free"`
}

// eventOf validates a create body and returns the event to make; field and
// message name the first thing wrong.
func (in eventCreate) eventOf() (calsync.EventNew, string, string) {
	var ev calsync.EventNew
	if !calsync.ValidClientID(in.ClientID) {
		return ev, "clientId", "send a client id of 8 to 64 letters, digits, - or _"
	}
	if n := len(in.CalendarURL); n == 0 || n > maxCalendarURL {
		return ev, "calendarUrl", "name the calendar to create the event in"
	}
	summary, msg := summaryOf(in.Summary)
	if msg != "" {
		return ev, "summary", msg
	}
	location, ok := textOf(strings.TrimSpace(in.Location), maxEventLocation)
	if !ok {
		return ev, "location", "enter a location of at most 500 characters"
	}
	description, ok := textOf(in.Description, maxEventDescription)
	if !ok {
		return ev, "description", "enter a description of at most 10000 characters"
	}
	if in.Start == nil || in.End == nil {
		return ev, "start", "send a start and an end"
	}
	when, field, msg := whenOf(in.Start, in.End, in.AllDay)
	if field != "" {
		return ev, field, msg
	}
	return calsync.EventNew{UID: in.ClientID, Summary: summary, When: *when, Location: location, Description: description, Free: in.Free}, "", ""
}

func (s *Server) handleCreateCalendarEvent(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), userFrom(r.Context()).ID
	var in eventCreate
	if err := readJSON(r, &in); err != nil {
		badBodyWith(w, err, "invalid_input", `body must be {"clientId": "...", "calendarUrl": "...", "summary": "...", "start": {...}, "end": {...}, "allDay"?: false, "location"?: "...", "description"?: "...", "free"?: false}`)
		return
	}
	ev, field, msg := in.eventOf()
	if field != "" {
		writeFieldError(w, field, msg)
		return
	}
	if !s.calsyncEventLimiter.allow(uid) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many calendar event changes; try again later")
		return
	}
	connID := r.PathValue("id")
	res, err := s.calsync.CreateEvent(ctx, uid, connID, in.CalendarURL, ev)
	if err != nil {
		s.eventFailure(w, err)
		return
	}
	status := http.StatusOK // a replay
	if res.Created {
		status = http.StatusCreated
		s.audit.Record(ctx, audit.CalendarEventChanged(uid, connID, res.Provider, "created"))
	}
	writeJSON(w, status, map[string]any{"event": res.Event, "clientId": in.ClientID})
}

func (s *Server) handleGetCalendarEvent(w http.ResponseWriter, r *http.Request) {
	d, err := s.calsync.EventDetail(r.Context(), userFrom(r.Context()).ID, r.PathValue("id"), r.PathValue("eventId"))
	if err != nil {
		s.eventFailure(w, err)
		return
	}
	out := map[string]any{"event": d.Event, "description": d.Description, "descriptionEditable": d.DescriptionEditable}
	if d.Replaces != "" {
		out["replaces"] = d.Replaces
	}
	writeJSON(w, http.StatusOK, out)
}
