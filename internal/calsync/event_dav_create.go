package calsync

import (
	"context"
	"net/http"
	"time"

	"github.com/OpenTodoApp/opentodo/internal/caldav"
	"github.com/OpenTodoApp/opentodo/internal/calfeed"
)

// Creating events in a CalDAV calendar (calendar-event-editor D6,
// calendar-event-series D8).

// newObject builds the calendar object of a new event: one VEVENT with its
// repeat and reminders if it has any and no attendee or organizer, and a
// VTIMEZONE for a zoned point.
func newObject(ev EventNew, now time.Time) ([]byte, error) {
	root := &caldav.Component{Name: "VCALENDAR", Props: []caldav.Prop{
		{Name: "VERSION", Value: "2.0"}, {Name: "PRODID", Value: caldav.ProdID}}}
	stamp := now.UTC().Format("20060102T150405Z")
	start, end := calfeed.PointProp("DTSTART", ev.When.Start, nil), calfeed.PointProp("DTEND", ev.When.End, nil)
	vev := &caldav.Component{Name: "VEVENT", Props: []caldav.Prop{
		{Name: "UID", Value: caldav.EscapeText(ev.UID)}, {Name: "DTSTAMP", Value: stamp},
		{Name: "CREATED", Value: stamp}, {Name: "LAST-MODIFIED", Value: stamp}, {Name: "SEQUENCE", Value: "0"},
		{Name: "SUMMARY", Value: caldav.EscapeText(ev.Summary)}, start, end}}
	if ev.Repeat != nil {
		rule, err := ev.Repeat.RRule(ev.When.Start)
		if err != nil {
			return nil, invalid("repeat")
		}
		vev.Props = append(vev.Props, caldav.Prop{Name: "RRULE", Value: rule})
	}
	if ev.Location != "" {
		vev.Props = append(vev.Props, caldav.Prop{Name: "LOCATION", Value: caldav.EscapeText(ev.Location)})
	}
	if ev.Description != "" {
		vev.Props = append(vev.Props, caldav.Prop{Name: "DESCRIPTION", Value: caldav.EscapeText(ev.Description)})
	}
	transp := "OPAQUE"
	if ev.Free {
		transp = "TRANSPARENT"
	}
	vev.Props = append(vev.Props, caldav.Prop{Name: "TRANSP", Value: transp})
	if len(ev.Reminders) > 0 {
		calfeed.SetReminders(vev, ev.Reminders)
	}
	root.Children = []*caldav.Component{vev}
	calfeed.AddZone(root, start)
	calfeed.AddZone(root, end)
	return root.Encode(), nil
}

// Create writes ev into the calendar with If-None-Match: * at <calendar><uid>.ics.
// A 412 means the address is taken: the same UID is a replay (the existing
// event's state with ErrEventExists), another UID is href_taken.
func (e davEvents) Create(ctx context.Context, calendar string, ev EventNew) (EventState, error) {
	href := objectHref(calendar, ev.UID)
	body, err := newObject(ev, e.now())
	if err != nil {
		return EventState{}, err
	}
	status, etag, err := e.d.put(ctx, href, "*", body)
	if err != nil {
		return EventState{}, err
	}
	switch status {
	case http.StatusPreconditionFailed:
		return e.existing(ctx, calendar, href, ev.UID)
	case http.StatusNotFound:
		return EventState{}, &Error{Reason: ReasonCalendarMissing}
	}
	if err := refusal(status); err != nil {
		return EventState{}, err
	}
	if etag == "" { // a server that does not send the version: ask for it once
		if gs, ge, _, gerr := e.d.getObject(ctx, href); gerr == nil && refusal(gs) == nil {
			etag = ge
		}
	}
	obj, err := writtenOf(EventRef{Calendar: calendar, Href: href, ETag: etag}, body)
	if err != nil {
		return EventState{}, err
	}
	st := EventState{Ref: obj.Ref, Event: obj.Events[0], Locked: obj.Locked}
	if obj.Series != nil {
		st.Wrote = SeriesWrite{Written: []WrittenObject{obj}, UID: obj.Events[0].UID, Instance: instanceOf(obj.Events[0])}
	}
	return st, nil
}

// existing answers a create that met an object at its address.
func (e davEvents) existing(ctx context.Context, calendar, href, uid string) (EventState, error) {
	st, root, err := e.read(ctx, EventRef{Calendar: calendar, Href: href})
	if err != nil {
		return EventState{}, err
	}
	if st.Event.UID != uid {
		return EventState{}, &Error{Reason: ReasonHrefTaken}
	}
	if v, err := calfeed.ReadSeries(root); err == nil && v.Recurring {
		if obj, err := writtenOf(st.Ref, root.Encode()); err == nil {
			st.Wrote = SeriesWrite{Written: []WrittenObject{obj}, UID: uid, Instance: instanceOf(obj.Events[0])}
		}
	}
	return st, ErrEventExists
}
