package httpapi

import (
	"context"
	"errors"

	"github.com/OpenTodoApp/opentodo/internal/agenttools"
	"github.com/OpenTodoApp/opentodo/internal/audit"
	"github.com/OpenTodoApp/opentodo/internal/calfeed"
	"github.com/OpenTodoApp/opentodo/internal/calsync"
)

// The agent tools' EventEditor (OpenSpec change calendar-event-edits, design
// D8): list_calendar_events, update_calendar_event and delete_calendar_event
// over the same calsync.Worker.EditEvent and DeleteEvent the REST routes call,
// with the same body rules, rate limit, switch and audit event.
type calendarEventEditor struct{ s *Server }

func toolCode(code, msg string) *agenttools.ToolError {
	return &agenttools.ToolError{Code: code, Message: msg}
}

// on refuses the call while calendar sync is off, as the routes' 404 does.
func (e calendarEventEditor) on(ctx context.Context) error {
	if e.s.calsync == nil || !e.s.calsyncOn.On(ctx) {
		return toolCode("calendar_sync_disabled", "calendar sync is turned off on this server")
	}
	return nil
}

func eventPointOf(p *agenttools.EventTime) *eventPoint {
	if p == nil {
		return nil
	}
	return &eventPoint{Date: p.Date, Time: p.Time, TZ: p.TZ}
}

// calRepeatOf is a tool's repeat rule as the REST form.
func calRepeatOf(r *agenttools.EventRepeat) *calfeed.Repeat {
	if r == nil {
		return nil
	}
	return &calfeed.Repeat{Freq: r.Freq, Interval: r.Interval, Days: r.Days, By: r.By,
		Ends: calfeed.RepeatEnd{Type: r.Ends.Type, Date: r.Ends.Date, Count: r.Ends.Count}}
}

func eventPatchOf(u agenttools.EventUpdate) eventPatch {
	p := eventPatch{Rev: u.Rev, Summary: u.Summary, Start: eventPointOf(u.Start), End: eventPointOf(u.End), AllDay: u.AllDay,
		Location: u.Location, Description: u.Description, Free: u.Free, Reminders: u.Reminders}
	if u.Scope != "" {
		p.Scope = &u.Scope
	}
	p.Repeat = repeatField{Set: u.RepeatSet, Rule: calRepeatOf(u.Repeat)}
	return p
}

// Validate applies the PATCH route's body rules.
func (calendarEventEditor) Validate(u agenttools.EventUpdate) error {
	if _, field, msg := eventPatchOf(u).editOf(); field != "" {
		return &agenttools.ArgError{Msg: field + ": " + msg}
	}
	return nil
}

// toolError maps a failed edit onto the REST code of design D4.
func (calendarEventEditor) toolError(err error) error {
	var ee *calsync.EventError
	if !errors.As(err, &ee) {
		return err
	}
	switch ee.Code {
	case calsync.EventScopeRequired:
		return toolCode("scope_required", "this event repeats (it has a series in list_calendar_events): say which events the change is for with scope: this (only that occurrence), following (it and every later one) or all (every occurrence)")
	case calsync.EventInvalid:
		return toolCode("invalid_input", ee.Reason+": "+invalidReason(ee.Reason))
	case calsync.EventNotFound:
		return toolCode("not_found", "calendar event not found")
	case calsync.EventEditsOff:
		return toolCode("event_edits_off", "editing events is off for this connection; the user turns it on in Settings, Calendar sync")
	case calsync.EventReadOnly:
		return toolCode("calendar_read_only", "this calendar does not allow changing events")
	case calsync.EventNotEditable:
		return toolCode("event_not_editable", "this event can not be edited here: "+ee.Reason)
	case calsync.EventChanged:
		return toolCode("event_changed", "the event changed in the calendar; the edit was not applied: list the events again and redo the change")
	case calsync.EventGone:
		return toolCode("event_gone", "the event is no longer in the calendar")
	case calsync.EventUnreachable:
		return toolCode("calendar_unreachable", ee.Reason)
	}
	return err
}

func (e calendarEventEditor) Update(ctx context.Context, uid string, u agenttools.EventUpdate) (agenttools.EventOutcome, error) {
	if err := e.on(ctx); err != nil {
		return agenttools.EventOutcome{}, err
	}
	edit, field, msg := eventPatchOf(u).editOf()
	if field != "" {
		return agenttools.EventOutcome{}, &agenttools.ArgError{Msg: field + ": " + msg}
	}
	if !e.s.calsyncEventLimiter.allow(uid) {
		return agenttools.EventOutcome{}, toolCode("rate_limited", "too many calendar event changes; try again later")
	}
	res, err := e.s.calsync.EditEvent(ctx, uid, u.ConnectionID, u.EventID, u.Rev, edit)
	if err != nil {
		return agenttools.EventOutcome{}, e.toolError(err)
	}
	e.s.audit.Record(ctx, audit.CalendarEventChanged(uid, u.ConnectionID, res.Provider, "updated", string(edit.Scope)))
	out := eventOfRow(res.Event, res.ConnectionID)
	return agenttools.EventOutcome{Event: out, Replaces: res.Replaces}, nil
}

func (e calendarEventEditor) Delete(ctx context.Context, uid string, d agenttools.EventDelete) error {
	if err := e.on(ctx); err != nil {
		return err
	}
	if !e.s.calsyncEventLimiter.allow(uid) {
		return toolCode("rate_limited", "too many calendar event changes; try again later")
	}
	scope := calfeed.Scope(d.Scope)
	if d.Scope != "" && !scope.Valid() {
		return &agenttools.ArgError{Msg: "scope: " + scopeValues}
	}
	res, err := e.s.calsync.DeleteEvent(ctx, uid, d.ConnectionID, d.EventID, d.Rev, scope)
	if err != nil {
		return e.toolError(err)
	}
	e.s.audit.Record(ctx, audit.CalendarEventChanged(uid, d.ConnectionID, res.Provider, "deleted", d.Scope))
	return nil
}

// Events lists the events the calendar view would show, with the id, rev and
// access of the connected calendars' events.
func (e calendarEventEditor) Events(ctx context.Context, uid, from, to string) ([]agenttools.CalendarEvent, error) {
	s := e.s
	if s.calWorker == nil {
		return nil, nil
	}
	feeds, managed := s.calFeedsOn.On(ctx), s.calsyncOn.On(ctx)
	if !feeds && !managed {
		return nil, nil
	}
	rows, err := s.calWorker.Store.EventsOf(ctx, uid, from, to, feeds, managed)
	if err != nil {
		return nil, err
	}
	links, err := s.managedLinks(ctx, uid, managed)
	if err != nil {
		return nil, err
	}
	out := make([]agenttools.CalendarEvent, 0, len(rows))
	for _, row := range rows {
		conn := ""
		if l, ok := links[row.SubscriptionID]; ok && row.Managed {
			a := calsync.AccessOf(l.conn, l.calendar, row)
			row.Access, conn = &a, l.conn.ID
		}
		out = append(out, eventOfRow(row, conn))
	}
	return out, nil
}

func timeOf(w calfeed.When) agenttools.EventTime {
	return agenttools.EventTime{Date: w.Date, Time: w.Time, TZ: w.TZ}
}

// eventOfRow is a stored event as the tools show it.
func eventOfRow(row calfeed.EventRow, connID string) agenttools.CalendarEvent {
	e := agenttools.CalendarEvent{ID: row.ID, ConnectionID: connID, Rev: row.Rev, Start: timeOf(row.Start), End: timeOf(row.End),
		AllDay: row.AllDay, Free: row.Free, Title: row.Summary, Location: row.Location}
	if sv := row.Series; sv != nil {
		s := &agenttools.EventSeries{ID: sv.ID, Repeats: true, First: sv.First, Custom: sv.Custom}
		if sv.Repeat != nil {
			s.Repeat = &agenttools.EventRepeat{Freq: sv.Repeat.Freq, Interval: sv.Repeat.Interval, Days: sv.Repeat.Days, By: sv.Repeat.By,
				Ends: agenttools.EventRepeatEnd{Type: sv.Repeat.Ends.Type, Date: sv.Repeat.Ends.Date, Count: sv.Repeat.Ends.Count}}
		}
		e.Series = s
	}
	if row.Access != nil {
		e.Access = &agenttools.EventAccess{Update: row.Access.Update, Delete: row.Access.Delete, Reason: row.Access.Reason}
	}
	return e
}

func eventCreateOf(c agenttools.EventCreate) eventCreate {
	return eventCreate{ClientID: c.ClientID, CalendarURL: c.CalendarURL, Summary: c.Summary,
		Start: eventPointOf(&c.Start), End: eventPointOf(&c.End), AllDay: c.AllDay, Location: c.Location, Description: c.Description, Free: c.Free,
		Repeat: repeatField{Set: c.Repeat != nil, Rule: calRepeatOf(c.Repeat)}, Reminders: c.Reminders}
}

// ValidateCreate applies the POST route's body rules.
func (calendarEventEditor) ValidateCreate(c agenttools.EventCreate) error {
	if _, field, msg := eventCreateOf(c).eventOf(); field != "" {
		return &agenttools.ArgError{Msg: field + ": " + msg}
	}
	return nil
}

// Create makes one event through the same Worker.CreateEvent as the POST
// route; a repeated client id is a replay that writes and audits nothing.
func (e calendarEventEditor) Create(ctx context.Context, uid string, c agenttools.EventCreate) (agenttools.EventOutcome, error) {
	if err := e.on(ctx); err != nil {
		return agenttools.EventOutcome{}, err
	}
	ev, field, msg := eventCreateOf(c).eventOf()
	if field != "" {
		return agenttools.EventOutcome{}, &agenttools.ArgError{Msg: field + ": " + msg}
	}
	if !e.s.calsyncEventLimiter.allow(uid) {
		return agenttools.EventOutcome{}, toolCode("rate_limited", "too many calendar event changes; try again later")
	}
	res, err := e.s.calsync.CreateEvent(ctx, uid, c.ConnectionID, c.CalendarURL, ev)
	if err != nil {
		var ee *calsync.EventError
		if errors.As(err, &ee) && ee.Code == calsync.EventInvalid {
			return agenttools.EventOutcome{}, &agenttools.ArgError{Msg: ee.Reason + ": not valid for this connection; take it from the calendars of list_calendar_events"}
		}
		return agenttools.EventOutcome{}, e.toolError(err)
	}
	if res.Created {
		e.s.audit.Record(ctx, audit.CalendarEventChanged(uid, c.ConnectionID, res.Provider, "created", ""))
	}
	return agenttools.EventOutcome{Event: eventOfRow(res.Event, res.ConnectionID), Replayed: !res.Created}, nil
}

// Calendars lists the calendars an event can be created in: the shown,
// writable calendars of the user's connections. CanCreate follows the
// connection's "Edit events in OpenTodo" switch.
func (e calendarEventEditor) Calendars(ctx context.Context, uid string) ([]agenttools.EventCalendar, error) {
	if e.s.calsync == nil || !e.s.calsyncOn.On(ctx) {
		return nil, nil
	}
	conns, err := e.s.calsync.Store.List(ctx, uid)
	if err != nil {
		return nil, err
	}
	var out []agenttools.EventCalendar
	for _, c := range conns {
		shown, err := e.s.calsync.Feeds.ManagedOf(ctx, c.ID)
		if err != nil {
			return nil, err
		}
		for _, cal := range c.Calendars {
			if _, ok := shown[cal.URL]; ok && cal.EffectiveCaps().Create {
				out = append(out, agenttools.EventCalendar{ConnectionID: c.ID, CalendarURL: cal.URL, Name: cal.Name, CanCreate: c.EditEvents})
			}
		}
	}
	return out, nil
}
