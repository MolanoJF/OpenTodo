package calsync

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/OpenTodoApp/opentodo/internal/caldav"
	"github.com/OpenTodoApp/opentodo/internal/calfeed"
)

// davEvents edits and creates events of a CalDAV account (and Google's CalDAV API,
// calendar-event-edits D5). An update reads the object, changes only what the edit
// names (plus the bookkeeping properties), and writes the whole calendar object
// back with If-Match, so unknown properties, alarms and other components survive.
// A repeating event goes through the series engine (event_dav_series.go).
type davEvents struct {
	d   *DAV
	now func() time.Time
}

// maxDAVReminders is how many reminders the form writes to a CalDAV event.
const maxDAVReminders = 5

// read fetches and parses an object; ErrEventGone when it is not there.
func (e davEvents) read(ctx context.Context, ref EventRef) (EventState, *caldav.Component, error) {
	status, etag, data, err := e.d.getObject(ctx, ref.Href)
	if err != nil {
		return EventState{}, nil, err
	}
	if err := refusal(status); err != nil {
		return EventState{}, nil, err
	}
	cal, root, err := calfeed.ParseCalendarRoot(data)
	if err != nil || len(cal.Events) == 0 {
		return EventState{}, nil, &Error{Reason: ReasonInvalid}
	}
	st := EventState{Ref: EventRef{Calendar: ref.Calendar, Href: ref.Href, ETag: etag, Instance: ref.Instance}, Event: cal.Events[0], Locked: lockedOf(root, etag)}
	for _, ev := range cal.Events {
		if ev.RecurrenceID == "" {
			st.Event = ev
			break
		}
	}
	return st, root, nil
}

// Read returns the event as the account holds it now.
func (e davEvents) Read(ctx context.Context, ref EventRef) (EventState, error) {
	st, _, err := e.read(ctx, ref)
	return st, err
}

// recurring reports whether an object is a series.
func recurring(root *caldav.Component) bool {
	v, err := calfeed.ReadSeries(root)
	return err == nil && v.Recurring
}

// checkRead is the pre-read of every write: the object as it is now, at the
// version the caller saw, and not locked.
func (e davEvents) checkRead(ctx context.Context, ref EventRef) (EventState, *caldav.Component, error) {
	if ref.ETag == "" {
		return EventState{}, nil, locked(LockNoETag)
	}
	st, root, err := e.read(ctx, ref)
	if err != nil {
		return EventState{}, nil, err
	}
	if st.Ref.ETag != ref.ETag {
		return EventState{}, nil, ErrEventChanged
	}
	if st.Locked != "" {
		return EventState{}, nil, locked(st.Locked)
	}
	return st, root, nil
}

// Update changes the event while it still has ref.ETag.
func (e davEvents) Update(ctx context.Context, ref EventRef, edit EventEdit) (EventState, error) {
	_, root, err := e.checkRead(ctx, ref)
	if err != nil {
		return EventState{}, err
	}
	switch isSeries := recurring(root); {
	case ref.Instance != "" && isSeries:
		return e.updateSeries(ctx, ref, edit, root)
	case ref.Instance != "" || isSeries:
		return EventState{}, ErrEventChanged // the cache is stale about whether it repeats
	}
	var vev *caldav.Component
	for _, c := range root.Children {
		if c.Name == "VEVENT" {
			vev = c
			break
		}
	}
	if vev == nil {
		return EventState{}, &Error{Reason: ReasonInvalid}
	}
	if err := editObject(root, vev, edit, e.now()); err != nil {
		return EventState{}, err
	}
	if !calfeed.RoundTrips(root) { // never write what could not be read back unchanged
		return EventState{}, locked(LockSeriesUnsupported)
	}
	body := root.Encode()
	etag, err := e.putObject(ctx, ref.Href, ref.ETag, body)
	if err != nil {
		return EventState{}, err
	}
	obj, err := writtenOf(EventRef{Calendar: ref.Calendar, Href: ref.Href, ETag: etag}, body)
	if err != nil {
		return EventState{}, err
	}
	st := EventState{Ref: obj.Ref, Event: obj.Events[0], Locked: obj.Locked}
	if edit.Repeat != nil { // the event now repeats: the cache learns it as a series
		st.Wrote = SeriesWrite{Written: []WrittenObject{obj}, UID: obj.Events[0].UID, Instance: instanceOf(obj.Events[0])}
	}
	return st, nil
}

// instanceOf is the instance key of an event's own start.
func instanceOf(ev calfeed.Event) string { return calfeed.OccurrenceOf(ev).Instance }

// putObject writes a calendar object conditionally (If-Match, or If-None-Match
// for etag "*") and returns the version the server now holds: the one it sent, or,
// for a server that does not, one asked for with a GET ("" when that fails too).
func (e davEvents) putObject(ctx context.Context, href, etag string, body []byte) (string, error) {
	status, newTag, err := e.d.put(ctx, href, etag, body)
	if err != nil {
		return "", err
	}
	if err := refusal(status); err != nil {
		return "", err
	}
	if newTag == "" { // a server that does not send the new version: ask for it
		if gs, ge, _, gerr := e.d.getObject(ctx, href); gerr == nil && refusal(gs) == nil {
			newTag = ge
		}
	}
	return newTag, nil
}

// writtenOf parses the body that was sent, as the cache will hold it.
func writtenOf(ref EventRef, body []byte) (WrittenObject, error) {
	cal, root, err := calfeed.ParseCalendarRoot(body)
	if err != nil || len(cal.Events) == 0 {
		return WrittenObject{}, &Error{Reason: ReasonInvalid}
	}
	obj := WrittenObject{Ref: ref, Events: cal.Events, Locked: lockedOf(root, ref.ETag)}
	if v, err := calfeed.ReadSeries(root); err == nil {
		obj.Series = v.Info()
	}
	return obj, nil
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
// bookkeeping properties (SEQUENCE, DTSTAMP, LAST-MODIFIED). A repeat makes the
// event repeat from its own start; the reminders replace the ones the form owns.
func editObject(root, ev *caldav.Component, edit EventEdit, now time.Time) error {
	zones := map[string]string{} // IANA name -> the TZID text the event already uses
	for _, p := range ev.Props {
		if tz := p.Param("TZID"); tz != "" {
			if loc, ok := calfeed.Resolve(tz); ok {
				zones[loc.String()] = tz
			}
		}
	}
	if edit.Summary != nil {
		calfeed.SetProp(ev, caldav.Prop{Name: "SUMMARY", Value: caldav.EscapeText(*edit.Summary)}, true)
	}
	editText(ev, edit)
	if edit.When != nil {
		start := calfeed.PointProp("DTSTART", edit.When.Start, zones)
		end := calfeed.PointProp("DTEND", edit.When.End, zones)
		calfeed.ReplaceTimes(ev, start, end)
		for _, p := range []caldav.Prop{start, end} {
			calfeed.AddZone(root, p)
		}
	}
	if edit.Reminders != nil {
		calfeed.SetReminders(ev, *edit.Reminders)
	}
	if edit.Repeat != nil {
		if err := setRepeatOf(ev, edit.Repeat); err != nil {
			return err
		}
	}
	seq := 0
	if p := ev.Prop("SEQUENCE"); p != nil {
		seq, _ = strconv.Atoi(strings.TrimSpace(p.Value))
	}
	stamp := now.UTC().Format("20060102T150405Z")
	calfeed.SetProp(ev, caldav.Prop{Name: "SEQUENCE", Value: strconv.Itoa(seq + 1)}, false)
	calfeed.SetProp(ev, caldav.Prop{Name: "DTSTAMP", Value: stamp}, false)
	calfeed.SetProp(ev, caldav.Prop{Name: "LAST-MODIFIED", Value: stamp}, false)
	return nil
}

// setRepeatOf adds the rule of a repeat to a single event, from its own start. A
// nil rule has nothing to stop on an event that does not repeat.
func setRepeatOf(ev *caldav.Component, rep *RepeatEdit) error {
	if rep.Rule == nil {
		return invalid("repeat")
	}
	cal, err := calfeed.ParseCalendar((&caldav.Component{Name: "VCALENDAR", Children: []*caldav.Component{ev}}).Encode())
	if err != nil || len(cal.Events) != 1 {
		return &Error{Reason: ReasonInvalid}
	}
	text, err := rep.Rule.RRule(cal.Events[0].Start)
	if err != nil {
		return invalid("repeat")
	}
	prop := caldav.Prop{Name: "RRULE", Value: text}
	for i, p := range ev.Props {
		if p.Name == "RRULE" {
			ev.Props[i] = prop
			return nil
		}
	}
	at := len(ev.Props)
	for i, p := range ev.Props {
		if p.Name == "DTEND" || p.Name == "DURATION" {
			at = i + 1
		}
	}
	ev.Props = append(ev.Props[:at], append([]caldav.Prop{prop}, ev.Props[at:]...)...)
	return nil
}

// editText applies the location, description and busy or free members of an
// edit (calendar-event-edits D6): an empty text deletes the property, and a
// changed description drops X-ALT-DESC so no client keeps showing the old HTML.
func editText(ev *caldav.Component, edit EventEdit) {
	if edit.Location != nil {
		calfeed.SetOrDelete(ev, "LOCATION", *edit.Location)
	}
	if edit.Description != nil {
		calfeed.SetOrDelete(ev, "DESCRIPTION", *edit.Description)
		calfeed.DropProp(ev, "X-ALT-DESC")
	}
	if edit.Free != nil {
		transp := "OPAQUE"
		if *edit.Free {
			transp = "TRANSPARENT"
		}
		calfeed.SetProp(ev, caldav.Prop{Name: "TRANSP", Value: transp}, true)
	}
}
