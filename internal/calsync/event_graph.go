package calsync

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"time"

	"github.com/OpenTodoApp/opentodo/internal/calfeed"
	"github.com/OpenTodoApp/opentodo/internal/zoned"
)

// graphEvents edits events of a Microsoft account (calendar-event-edits D5):
// a PATCH with If-Match carrying only the changed members.
type graphEvents struct {
	g *Graph
	// zone is the mailbox's zone (IANA, "" for UTC), the zone of all-day and
	// floating points.
	zone string
	now  func() time.Time
}

// eventSelect is what a read asks for (the fields the locks need).
const eventSelect = "id,subject,start,end,isAllDay,showAs,type,isOrganizer,attendees,location,isCancelled,lastModifiedDateTime,seriesMasterId,reminderMinutesBeforeStart,isReminderOn"

func (e graphEvents) read(ctx context.Context, ref EventRef) (EventState, error) {
	var ev graphEvent
	err := e.g.get(ctx, eventPath(ref.Href)+"?"+query(url.Values{"$select": {eventSelect}}), &ev)
	switch reasonOf(err) {
	case ReasonCalendarMissing:
		return EventState{}, ErrEventGone
	case ReasonCalendarReadOnly:
		return EventState{}, ErrCalendarRO
	}
	if err != nil {
		return EventState{}, err
	}
	return e.state(ref, &ev)
}

func (e graphEvents) state(ref EventRef, ev *graphEvent) (EventState, error) {
	r, ok := ev.remote()
	if !ok || len(r.Events) == 0 {
		return EventState{}, &Error{Reason: ReasonInvalid}
	}
	return EventState{Ref: EventRef{Calendar: ref.Calendar, Href: firstOf(ev.ID, ref.Href), ETag: ev.ETag}, Event: r.Events[0], Locked: r.Locked}, nil
}

// Read returns the event as the account holds it now.
func (e graphEvents) Read(ctx context.Context, ref EventRef) (EventState, error) {
	return e.read(ctx, ref)
}

// Update changes the event while it still has ref.ETag. The event is read
// first: an event with guests or one the user does not organise is refused
// (editing it would notify people), and a changed ETag is ErrEventChanged
// before anything is sent. An occurrence of a series is changed for the scope
// of the edit (event_graph_series.go).
func (e graphEvents) Update(ctx context.Context, ref EventRef, edit EventEdit) (EventState, error) {
	if ref.ETag == "" {
		return EventState{}, locked(LockNoETag)
	}
	if ref.Instance == "" {
		return e.updateOne(ctx, ref, edit, false)
	}
	switch edit.Scope {
	case calfeed.ScopeThis:
		return e.updateOne(ctx, ref, edit, true)
	case calfeed.ScopeAll, calfeed.ScopeFollowing:
		s, err := e.readSeries(ctx, ref)
		if err != nil {
			return EventState{}, err
		}
		if edit.Scope == calfeed.ScopeAll {
			return e.updateAll(ctx, ref, edit, s)
		}
		return e.updateFollowing(ctx, ref, edit, s)
	}
	return EventState{}, invalid("scope")
}

// updateOne patches one event (a single event, or one occurrence of a series,
// which Graph turns into an exception) with only the members that changed.
func (e graphEvents) updateOne(ctx context.Context, ref EventRef, edit EventEdit, occurrence bool) (EventState, error) {
	cur, err := e.current(ctx, ref, edit)
	if err != nil {
		return EventState{}, err
	}
	if cur.Ref.ETag != ref.ETag {
		return EventState{}, ErrEventChanged
	}
	if cur.Locked != "" {
		return EventState{}, locked(cur.Locked)
	}
	if edit.Description != nil && cur.DescriptionLocked {
		return EventState{}, locked(LockRichDescription)
	}
	body := map[string]any{}
	if edit.Summary != nil {
		body["subject"] = *edit.Summary
	}
	e.editMembers(body, edit)
	if err := e.reminderMembers(body, edit); err != nil {
		return EventState{}, err
	}
	if edit.When != nil {
		body["start"] = e.point(edit.When.Start)
		body["end"] = e.point(edit.When.End)
		body["isAllDay"] = edit.When.AllDay
	}
	if edit.Repeat != nil {
		rec, err := e.recurrenceForSingle(cur, edit)
		if err != nil {
			return EventState{}, err
		}
		body["recurrence"] = rec
	}
	resp, err := e.g.do(ctx, http.MethodPatch, eventPath(ref.Href), map[string]string{"If-Match": ref.ETag}, body)
	if err != nil {
		return EventState{}, err
	}
	if err := refusal(resp.Status); err != nil {
		return EventState{}, err
	}
	var ev graphEvent
	if json.Unmarshal(resp.Body, &ev) != nil || ev.ID == "" {
		return EventState{}, &Error{Reason: ReasonInvalid}
	}
	st, err := e.state(ref, &ev)
	if err != nil {
		return EventState{}, err
	}
	switch {
	case occurrence:
		r, _ := ev.remote()
		st.Wrote = SeriesWrite{Written: []WrittenObject{{Ref: st.Ref, Events: r.Events, Locked: r.Locked, Series: r.Series}}, UID: ev.ID, Instance: calfeed.OccurrenceOf(r.Events[0]).Instance}
	case edit.Repeat != nil: // the event is a series master now: its occurrences are what the cache holds
		written, err := e.instancesOf(ctx, ref.Calendar, ev.ID)
		if err != nil {
			return EventState{}, err
		}
		st.Wrote = SeriesWrite{Written: written, Refetch: true, UID: ev.ID}
		if len(written) > 0 {
			st.Wrote.UID, st.Wrote.Instance = written[0].Events[0].UID, calfeed.OccurrenceOf(written[0].Events[0]).Instance
			st.Ref, st.Event, st.Locked = written[0].Ref, written[0].Events[0], written[0].Locked
		}
	}
	return st, nil
}

// recurrenceForSingle is the recurrence a single event gets when it is made to
// repeat: from its own start, read in the mailbox zone.
func (e graphEvents) recurrenceForSingle(cur EventState, edit EventEdit) (*graphRecurrence, error) {
	if edit.Repeat.Rule == nil {
		return nil, invalid("repeat") // nothing to stop on an event that does not repeat
	}
	win, loc := mailboxWindows(e.zone)
	start := cur.Event.Start
	if edit.When != nil {
		start = edit.When.Start
	}
	var local time.Time
	switch {
	case start.AllDay || start.Zone == "":
		local = time.Date(start.T.Year(), start.T.Month(), start.T.Day(), 0, 0, 0, 0, time.UTC)
	default:
		local = start.T.In(loc)
	}
	rec, err := recurrenceOf(*edit.Repeat.Rule, time.Date(local.Year(), local.Month(), local.Day(), local.Hour(), local.Minute(), 0, 0, time.UTC), win)
	if err != nil {
		return nil, invalid("repeat")
	}
	return rec, nil
}

// Delete removes the event while it still has ref.ETag.
func (e graphEvents) Delete(ctx context.Context, ref EventRef) error {
	if ref.ETag == "" {
		return locked(LockNoETag)
	}
	resp, err := e.g.do(ctx, http.MethodDelete, eventPath(ref.Href), map[string]string{"If-Match": ref.ETag}, nil)
	if err != nil {
		return err
	}
	return refusal(resp.Status)
}

// point is a start or end as Graph wants it (the rules of graphTimes): an
// all-day date at midnight in the mailbox zone; a zoned point in its zone's
// Windows id, or as the same instant in UTC when the zone has none; a floating
// one at its wall time in the mailbox zone.
func (e graphEvents) point(p calfeed.Point) graphDateTime {
	const layout = "2006-01-02T15:04:05"
	if p.AllDay {
		zone, _ := mailboxWindows(e.zone)
		return graphDateTime{DateTime: p.T.Format("2006-01-02") + "T00:00:00", TimeZone: zone}
	}
	if p.Zone == "" {
		zone, loc := mailboxWindows(e.zone)
		return graphDateTime{DateTime: time.Date(p.T.Year(), p.T.Month(), p.T.Day(), p.T.Hour(), p.T.Minute(), p.T.Second(), 0, loc).Format(layout), TimeZone: zone}
	}
	loc, err := zoned.Load(p.Zone)
	if p.Zone == "UTC" || err != nil {
		return graphDateTime{DateTime: p.T.UTC().Format(layout), TimeZone: "UTC"}
	}
	if win, ok := zoned.WindowsName(loc.String()); ok {
		return graphDateTime{DateTime: p.T.In(loc).Format(layout), TimeZone: win}
	}
	return graphDateTime{DateTime: p.T.UTC().Format(layout), TimeZone: "UTC"}
}

// current reads the event before an update. An edit of the description reads
// the body too, so Update can refuse a rich-text body (plain text would flatten it).
func (e graphEvents) current(ctx context.Context, ref EventRef, edit EventEdit) (EventState, error) {
	if edit.Description == nil {
		return e.read(ctx, ref)
	}
	st, err := e.Detail(ctx, ref)
	if err != nil {
		return EventState{}, err
	}
	return st, nil
}

// editMembers adds the members of location, description and busy or free that
// the edit changes.
func (e graphEvents) editMembers(body map[string]any, edit EventEdit) {
	if edit.Location != nil {
		body["location"] = map[string]string{"displayName": *edit.Location}
	}
	if edit.Description != nil {
		body["body"] = textBody(*edit.Description)
	}
	if edit.Free != nil {
		body["showAs"] = showAs(*edit.Free)
	}
}
