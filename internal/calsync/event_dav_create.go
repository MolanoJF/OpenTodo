package calsync

import (
	"context"
	"net/url"
	"strings"
	"time"

	"github.com/OpenTodoApp/opentodo/internal/caldav"
	"github.com/OpenTodoApp/opentodo/internal/calfeed"
)

// Creating events in a CalDAV calendar and reading their description
// (calendar-event-editor D6).

// newObject builds the calendar object of a new event: one VEVENT with no
// alarm, attendee or organizer, and a VTIMEZONE for a zoned point.
func newObject(ev EventNew, now time.Time) []byte {
	root := &caldav.Component{Name: "VCALENDAR", Props: []caldav.Prop{
		{Name: "VERSION", Value: "2.0"}, {Name: "PRODID", Value: caldav.ProdID}}}
	stamp := now.UTC().Format("20060102T150405Z")
	start, end := pointProp("DTSTART", ev.When.Start, nil), pointProp("DTEND", ev.When.End, nil)
	vev := &caldav.Component{Name: "VEVENT", Props: []caldav.Prop{
		{Name: "UID", Value: caldav.EscapeText(ev.UID)}, {Name: "DTSTAMP", Value: stamp},
		{Name: "CREATED", Value: stamp}, {Name: "LAST-MODIFIED", Value: stamp}, {Name: "SEQUENCE", Value: "0"},
		{Name: "SUMMARY", Value: caldav.EscapeText(ev.Summary)}, start, end}}
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
	root.Children = []*caldav.Component{vev}
	addZone(root, start)
	addZone(root, end)
	return root.Encode()
}

// Create writes ev into the calendar with If-None-Match: * at <calendar><uid>.ics.
// A 412 means the address is taken: the same UID is a replay (the existing
// event's state with ErrEventExists), another UID is href_taken.
func (e davEvents) Create(ctx context.Context, calendar string, ev EventNew) (EventState, error) {
	href := strings.TrimSuffix(calendar, "/") + "/" + url.PathEscape(ev.UID) + ".ics"
	body := newObject(ev, e.now())
	status, etag, err := e.d.put(ctx, href, "*", body)
	if err != nil {
		return EventState{}, err
	}
	switch status {
	case 412:
		return e.existing(ctx, calendar, href, ev.UID)
	case 404:
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
	cal, err := calfeed.ParseCalendar(body)
	if err != nil || len(cal.Events) == 0 {
		return EventState{}, &Error{Reason: ReasonInvalid}
	}
	return EventState{Ref: EventRef{Calendar: calendar, Href: href, ETag: etag}, Event: cal.Events[0], Locked: davLocked(cal.Events, etag)}, nil
}

// existing answers a create that met an object at its address.
func (e davEvents) existing(ctx context.Context, calendar, href, uid string) (EventState, error) {
	st, _, err := e.read(ctx, EventRef{Calendar: calendar, Href: href})
	if err != nil {
		return EventState{}, err
	}
	if st.Event.UID != uid {
		return EventState{}, &Error{Reason: ReasonHrefTaken}
	}
	return st, ErrEventExists
}

// Detail is Read plus the DESCRIPTION property of the event.
func (e davEvents) Detail(ctx context.Context, ref EventRef) (EventState, error) {
	st, data, err := e.read(ctx, ref)
	if err != nil {
		return EventState{}, err
	}
	root, err := caldav.Parse(data)
	if err != nil {
		return EventState{}, &Error{Reason: ReasonInvalid}
	}
	for _, c := range root.Children {
		if c.Name != "VEVENT" || c.Prop("RECURRENCE-ID") != nil {
			continue
		}
		if p := c.Prop("DESCRIPTION"); p != nil {
			st.Description = caldav.UnescapeText(p.Value)
		}
		break
	}
	return st, nil
}
