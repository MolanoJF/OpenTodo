package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/OpenTodoApp/opentodo/internal/audit"
	"github.com/OpenTodoApp/opentodo/internal/calfeed"
	"github.com/OpenTodoApp/opentodo/internal/calsync"
	"github.com/OpenTodoApp/opentodo/internal/zoned"
)

// Editing the events of a connected calendar (OpenSpec change
// calendar-event-edits, design D4): PATCH and DELETE of one event. The checks,
// the remote write, the cache and the wake-up of other devices are
// calsync.Worker.EditEvent and DeleteEvent; this file is the request, the body
// rules and the error table.

const (
	calsyncEventsPerHr = 600
	maxEventSummary    = 500
)

// eventPoint is one end of an event as the client sends it.
type eventPoint struct {
	Date string  `json:"date"`
	Time *string `json:"time"`
	TZ   *string `json:"tz"`
}

// eventPatch is the body of PATCH .../events/{eventId}.
type eventPatch struct {
	Rev     string      `json:"rev"`
	Summary *string     `json:"summary"`
	Start   *eventPoint `json:"start"`
	End     *eventPoint `json:"end"`
	AllDay  *bool       `json:"allDay"`
}

// point turns a client point into the event's own form: a date for all-day, a
// wall time in its zone, or a floating wall time when it has no zone.
func (p *eventPoint) point(allDay bool) (calfeed.Point, string) {
	day, err := time.Parse(time.DateOnly, p.Date)
	if err != nil {
		return calfeed.Point{}, "date must be YYYY-MM-DD"
	}
	if allDay {
		if p.Time != nil || p.TZ != nil {
			return calfeed.Point{}, "an all-day point is a date alone"
		}
		return calfeed.Point{T: day, AllDay: true}, ""
	}
	if p.Time == nil {
		return calfeed.Point{}, "a timed point needs a time HH:MM"
	}
	clock, err := time.Parse("15:04", *p.Time)
	if err != nil {
		return calfeed.Point{}, "time must be HH:MM"
	}
	if p.TZ == nil || *p.TZ == "" {
		return calfeed.Point{T: time.Date(day.Year(), day.Month(), day.Day(), clock.Hour(), clock.Minute(), 0, 0, time.UTC)}, ""
	}
	loc, err := zoned.Load(*p.TZ)
	if err != nil {
		return calfeed.Point{}, "unknown time zone"
	}
	at, err := zoned.Instant(p.Date, *p.Time, loc)
	if err != nil {
		return calfeed.Point{}, "date and time are not valid in that zone"
	}
	return calfeed.Point{T: at.In(loc), Zone: loc.String()}, ""
}

// editOf validates a body and returns the edit; field and message name the
// first thing wrong.
func (in eventPatch) editOf() (calsync.EventEdit, string, string) {
	var edit calsync.EventEdit
	if strings.TrimSpace(in.Rev) == "" {
		return edit, "rev", "send the rev of the event you saw"
	}
	if in.Summary != nil {
		v := strings.TrimSpace(*in.Summary)
		if n := utf8.RuneCountInString(v); n < 1 || n > maxEventSummary {
			return edit, "summary", "enter a title of 1 to 500 characters"
		}
		edit.Summary = &v
	}
	if (in.Start == nil) != (in.End == nil) {
		return edit, "end", "start and end go together"
	}
	if in.AllDay != nil && in.Start == nil {
		return edit, "allDay", "allDay needs a start and an end"
	}
	if edit.Summary == nil && in.Start == nil {
		return edit, "summary", "nothing to change: send a summary, or a start and an end"
	}
	if in.Start == nil {
		return edit, "", ""
	}
	allDay := in.Start.Time == nil && in.End.Time == nil
	if in.AllDay != nil {
		allDay = *in.AllDay
	}
	start, msg := in.Start.point(allDay)
	if msg != "" {
		return edit, "start", msg
	}
	end, msg := in.End.point(allDay)
	if msg != "" {
		return edit, "end", msg
	}
	if (start.Zone == "") != (end.Zone == "") && !allDay {
		return edit, "end", "start and end must both have a time zone, or neither"
	}
	if !end.T.After(start.T) {
		return edit, "end", "the end must be after the start"
	}
	edit.When = &calsync.EventWhen{Start: start, End: end, AllDay: allDay}
	return edit, "", ""
}

// eventFailure answers a failed edit with the code and status of design D4.
func (s *Server) eventFailure(w http.ResponseWriter, err error) {
	var ee *calsync.EventError
	if !errors.As(err, &ee) {
		s.internal(w, err)
		return
	}
	switch ee.Code {
	case calsync.EventNotFound:
		writeError(w, http.StatusNotFound, "not_found", "calendar event not found")
	case calsync.EventEditsOff:
		writeError(w, http.StatusForbidden, "event_edits_off", "editing events is off for this connection")
	case calsync.EventReadOnly:
		writeError(w, http.StatusForbidden, "calendar_read_only", "this calendar does not allow changing events")
	case calsync.EventNotEditable:
		writeError(w, http.StatusUnprocessableEntity, "event_not_editable", "this event can not be edited here: "+ee.Reason)
	case calsync.EventChanged:
		writeError(w, http.StatusConflict, "event_changed", "the event changed in your calendar; your edit was not applied")
	case calsync.EventGone:
		writeError(w, http.StatusNotFound, "event_gone", "the event is no longer in your calendar")
	case calsync.EventUnreachable:
		writeError(w, http.StatusBadGateway, "calendar_unreachable", ee.Reason) // the reason code, as for discovery
	default:
		s.internal(w, err)
	}
}

func (s *Server) handlePatchCalendarEvent(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), userFrom(r.Context()).ID
	var in eventPatch
	if err := readJSON(r, &in); err != nil {
		badBodyWith(w, err, "invalid_input", `body must be {"rev": "...", "summary"?: "...", "start"?: {...}, "end"?: {...}, "allDay"?: false}`)
		return
	}
	edit, field, msg := in.editOf()
	if field != "" {
		writeFieldError(w, field, msg)
		return
	}
	if !s.calsyncEventLimiter.allow(uid) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many calendar event changes; try again later")
		return
	}
	connID := r.PathValue("id")
	res, err := s.calsync.EditEvent(ctx, uid, connID, r.PathValue("eventId"), in.Rev, edit)
	if err != nil {
		s.eventFailure(w, err)
		return
	}
	s.audit.Record(ctx, audit.CalendarEventChanged(uid, connID, res.Provider, "updated"))
	writeJSON(w, http.StatusOK, map[string]any{"event": res.Event, "replaces": res.Replaces})
}

func (s *Server) handleDeleteCalendarEvent(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), userFrom(r.Context()).ID
	rev := strings.TrimSpace(r.URL.Query().Get("rev"))
	if rev == "" {
		writeFieldError(w, "rev", "send the rev of the event you saw")
		return
	}
	if !s.calsyncEventLimiter.allow(uid) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many calendar event changes; try again later")
		return
	}
	connID := r.PathValue("id")
	res, err := s.calsync.DeleteEvent(ctx, uid, connID, r.PathValue("eventId"), rev)
	if err != nil {
		s.eventFailure(w, err)
		return
	}
	s.audit.Record(ctx, audit.CalendarEventChanged(uid, connID, res.Provider, "deleted"))
	w.WriteHeader(http.StatusNoContent)
}
