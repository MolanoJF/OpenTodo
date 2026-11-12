package calsync

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/OpenTodoApp/opentodo/internal/caldav"
	"github.com/OpenTodoApp/opentodo/internal/calfeed"
)

// davEvents edits events of a CalDAV account (and Google's CalDAV API,
// calendar-event-edits D5). An update reads the object, changes only SUMMARY,
// DTSTART and DTEND (plus the bookkeeping properties), and writes the whole
// calendar object back with If-Match, so unknown properties, alarms and other
// components survive.
type davEvents struct {
	d   *DAV
	now func() time.Time
}

// read fetches and parses an object; ErrEventGone when it is not there.
func (e davEvents) read(ctx context.Context, ref EventRef) (EventState, []byte, error) {
	status, etag, data, err := e.d.getObject(ctx, ref.Href)
	if err != nil {
		return EventState{}, nil, err
	}
	if err := refusal(status); err != nil {
		return EventState{}, nil, err
	}
	cal, err := calfeed.ParseCalendar(data)
	if err != nil || len(cal.Events) == 0 {
		return EventState{}, nil, &Error{Reason: ReasonInvalid}
	}
	st := EventState{Ref: EventRef{Calendar: ref.Calendar, Href: ref.Href, ETag: etag}, Event: cal.Events[0], Locked: davLocked(cal.Events, etag)}
	for _, ev := range cal.Events {
		if ev.RecurrenceID == "" {
			st.Event = ev
			break
		}
	}
	return st, data, nil
}

// Read returns the event as the account holds it now.
func (e davEvents) Read(ctx context.Context, ref EventRef) (EventState, error) {
	st, _, err := e.read(ctx, ref)
	return st, err
}

// Update changes the event while it still has ref.ETag.
func (e davEvents) Update(ctx context.Context, ref EventRef, edit EventEdit) (EventState, error) {
	if ref.ETag == "" {
		return EventState{}, locked(LockNoETag)
	}
	st, data, err := e.read(ctx, ref)
	if err != nil {
		return EventState{}, err
	}
	if st.Ref.ETag != ref.ETag {
		return EventState{}, ErrEventChanged
	}
	if st.Locked != "" {
		return EventState{}, locked(st.Locked)
	}
	root, err := caldav.Parse(data)
	if err != nil {
		return EventState{}, &Error{Reason: ReasonInvalid}
	}
	var events []*caldav.Component
	for _, c := range root.Children {
		if c.Name == "VEVENT" {
			events = append(events, c)
		}
	}
	if len(events) != 1 {
		return EventState{}, locked(LockRecurring)
	}
	editObject(root, events[0], edit, e.now())
	body := root.Encode()
	status, etag, err := e.d.put(ctx, ref.Href, ref.ETag, body)
	if err != nil {
		return EventState{}, err
	}
	if err := refusal(status); err != nil {
		return EventState{}, err
	}
	if etag == "" { // a server that does not send the new version: ask for it
		if status, etag, _, err = e.d.getObject(ctx, ref.Href); err != nil || refusal(status) != nil {
			etag = ""
		}
	}
	cal, err := calfeed.ParseCalendar(body)
	if err != nil || len(cal.Events) == 0 {
		return EventState{}, &Error{Reason: ReasonInvalid}
	}
	return EventState{Ref: EventRef{Calendar: ref.Calendar, Href: ref.Href, ETag: etag}, Event: cal.Events[0], Locked: davLocked(cal.Events, etag)}, nil
}

// Delete removes the event while it still has ref.ETag.
func (e davEvents) Delete(ctx context.Context, ref EventRef) error {
	if ref.ETag == "" {
		return locked(LockNoETag)
	}
	status, err := e.d.del(ctx, ref.Href, ref.ETag)
	if err != nil {
		return err
	}
	return refusal(status)
}

// editObject applies edit to the VEVENT of a calendar object and bumps its
// bookkeeping properties (SEQUENCE, DTSTAMP, LAST-MODIFIED).
func editObject(root, ev *caldav.Component, edit EventEdit, now time.Time) {
	zones := map[string]string{} // IANA name -> the TZID text the event already uses
	for _, p := range ev.Props {
		if tz := p.Param("TZID"); tz != "" {
			if loc, ok := calfeed.Resolve(tz); ok {
				zones[loc.String()] = tz
			}
		}
	}
	if edit.Summary != nil {
		setProp(ev, caldav.Prop{Name: "SUMMARY", Value: caldav.EscapeText(*edit.Summary)}, true)
	}
	if edit.When != nil {
		var start, end caldav.Prop
		start = pointProp("DTSTART", edit.When.Start, zones)
		end = pointProp("DTEND", edit.When.End, zones)
		replaceTimes(ev, start, end)
		for _, p := range []caldav.Prop{start, end} {
			addZone(root, p)
		}
	}
	seq := 0
	if p := ev.Prop("SEQUENCE"); p != nil {
		seq, _ = strconv.Atoi(strings.TrimSpace(p.Value))
	}
	stamp := now.UTC().Format("20060102T150405Z")
	setProp(ev, caldav.Prop{Name: "SEQUENCE", Value: strconv.Itoa(seq + 1)}, false)
	setProp(ev, caldav.Prop{Name: "DTSTAMP", Value: stamp}, false)
	setProp(ev, caldav.Prop{Name: "LAST-MODIFIED", Value: stamp}, false)
}

// setProp replaces the first property of that name, keeping its parameters
// when keepParams, or appends it.
func setProp(c *caldav.Component, p caldav.Prop, keepParams bool) {
	for i := range c.Props {
		if c.Props[i].Name == p.Name {
			if keepParams {
				p.Params = c.Props[i].Params
			}
			c.Props[i] = p
			return
		}
	}
	c.Props = append(c.Props, p)
}

// replaceTimes puts DTSTART and DTEND where DTSTART was and drops DTEND and
// DURATION (a DURATION becomes a DTEND).
func replaceTimes(c *caldav.Component, start, end caldav.Prop) {
	out := make([]caldav.Prop, 0, len(c.Props)+1)
	placed := false
	for _, p := range c.Props {
		switch p.Name {
		case "DTSTART", "DTEND", "DURATION":
			if !placed {
				out = append(out, start, end)
				placed = true
			}
		default:
			out = append(out, p)
		}
	}
	if !placed {
		out = append(out, start, end)
	}
	c.Props = out
}

// pointProp writes a point as DTSTART or DTEND: a date for all-day, UTC with
// Z, a wall time with TZID for a zoned point (the TZID text the event already
// used for that zone, if any) and a bare wall time for a floating one.
func pointProp(name string, p calfeed.Point, zones map[string]string) caldav.Prop {
	switch {
	case p.AllDay:
		return caldav.Prop{Name: name, Params: []caldav.Param{{Name: "VALUE", Values: []string{"DATE"}}}, Value: p.T.Format("20060102")}
	case p.Zone == "":
		return caldav.Prop{Name: name, Value: p.T.Format("20060102T150405")}
	case p.Zone == "UTC":
		return caldav.Prop{Name: name, Value: p.T.UTC().Format("20060102T150405Z")}
	}
	tzid := p.Zone
	if raw, ok := zones[p.Zone]; ok {
		tzid = raw
	}
	t := p.T
	if loc, ok := calfeed.Resolve(p.Zone); ok {
		t = t.In(loc)
	}
	return caldav.Prop{Name: name, Params: []caldav.Param{{Name: "TZID", Values: []string{tzid}}}, Value: t.Format("20060102T150405")}
}

// addZone adds a VTIMEZONE for the TZID of a written point when the object has
// none for it.
func addZone(root *caldav.Component, p caldav.Prop) {
	tz := p.Param("TZID")
	if tz == "" {
		return
	}
	for _, c := range root.Children {
		if c.Name == "VTIMEZONE" {
			if id := c.Prop("TZID"); id != nil && id.Value == tz {
				return
			}
		}
	}
	loc, ok := calfeed.Resolve(tz)
	if !ok || loc.String() != tz {
		return // a vendor TZID without a definition of ours stays as it was written
	}
	date, clock := p.Value[:4]+"-"+p.Value[4:6]+"-"+p.Value[6:8], p.Value[9:11]+":"+p.Value[11:13]
	if vt := caldav.VTimezone(tz, date, clock); vt != nil {
		root.Children = append([]*caldav.Component{vt}, root.Children...)
	}
}
