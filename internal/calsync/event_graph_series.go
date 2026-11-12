package calsync

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/OpenTodoApp/opentodo/internal/calfeed"
	"github.com/OpenTodoApp/opentodo/internal/zoned"
)

// Scoped changes of a repeating event in a Microsoft calendar (calendar-event-series
// D9): this event patches the occurrence, all events patches the series master,
// this and following ends the master and creates a new series. What Graph does to
// the exceptions of a master whose times or pattern change is Graph's (it drops
// them); this file documents it and does not hide it.

// masterSelect is what a series write reads of a master.
const masterSelect = eventSelect + ",recurrence,body,categories,importance,sensitivity"

// graphLayout is the dateTime text of a Graph time.
const graphLayout = "2006-01-02T15:04:05"

// fetch reads one event with the members sel names.
func (e graphEvents) fetch(ctx context.Context, id, sel string) (*graphEvent, error) {
	var ev graphEvent
	if err := e.g.get(ctx, eventPath(id)+"?"+query(url.Values{"$select": {sel}}), &ev); err != nil {
		return nil, e.mapRead(err)
	}
	return &ev, nil
}

// graphSeries is what a series write reads first: the occurrence the caller saw
// (at the version it saw), its master and the zone the series is defined in.
type graphSeries struct {
	occ, master *graphEvent
	win         string // the recurrence's zone as Graph names it
	loc         *time.Location
	mailbox     *time.Location
}

// readSeries reads the occurrence ref names and its master. An occurrence that
// changed since the caller read it is ErrEventChanged: the guard that stands in
// for the master's ETag, which calendarView does not give (design D9).
func (e graphEvents) readSeries(ctx context.Context, ref EventRef) (*graphSeries, error) {
	occ, err := e.fetch(ctx, ref.Href, eventSelect)
	if err != nil {
		return nil, err
	}
	switch {
	case occ.ETag != ref.ETag || !occ.inSeries():
		return nil, ErrEventChanged
	case occ.locked() != "":
		return nil, locked(occ.locked())
	}
	m, err := e.fetch(ctx, occ.SeriesMasterID, masterSelect)
	if err != nil {
		return nil, err
	}
	switch {
	case !strings.EqualFold(m.Type, "seriesMaster") || m.Recurrence == nil || m.Start == nil || m.End == nil:
		return nil, locked(LockSeriesUnsupported)
	case len(m.Attendees) > 0:
		return nil, locked(LockGuests)
	case m.IsOrganizer != nil && !*m.IsOrganizer:
		return nil, locked(LockNotOrganizer)
	case m.ETag == "":
		return nil, locked(LockNoETag)
	}
	win := m.Recurrence.Range.RecurrenceTimeZone
	if win == "" {
		win, _ = mailboxWindows(e.zone)
	}
	iana := ianaZone(win)
	if iana == "" {
		return nil, locked(LockSeriesUnsupported)
	}
	loc, err := zoned.Load(iana)
	if err != nil {
		return nil, locked(LockSeriesUnsupported)
	}
	_, mailbox := mailboxWindows(e.zone)
	return &graphSeries{occ: occ, master: m, win: win, loc: loc, mailbox: mailbox}, nil
}

// wall is a point read as a wall clock in the series' zone, as a UTC time: dates
// stay dates, a floating point is a wall time in the mailbox zone.
func (s *graphSeries) wall(p calfeed.Point) time.Time {
	t := p.T
	switch {
	case p.AllDay:
		return p.T
	case p.Zone == "":
		t = time.Date(p.T.Year(), p.T.Month(), p.T.Day(), p.T.Hour(), p.T.Minute(), p.T.Second(), 0, s.mailbox)
	}
	c := t.In(s.loc)
	return time.Date(c.Year(), c.Month(), c.Day(), c.Hour(), c.Minute(), c.Second(), 0, time.UTC)
}

func (s *graphSeries) pointOf(d *graphDateTime, allDay bool) (time.Time, bool) {
	p, ok := d.point(allDay)
	if !ok {
		return time.Time{}, false
	}
	return s.wall(p), true
}

func dateOf(t time.Time) string { return t.Format("2006-01-02") }

func daysBetween(a, b time.Time) int {
	da := time.Date(a.Year(), a.Month(), a.Day(), 0, 0, 0, 0, time.UTC)
	db := time.Date(b.Year(), b.Month(), b.Day(), 0, 0, 0, 0, time.UTC)
	return int(da.Sub(db).Hours() / 24)
}

// msShift is what an edit of the occurrence's times does to the master.
type msShift struct {
	changed          bool
	start, end       time.Time // the master's new start and end, wall clock in the series' zone
	dayShift         int
	occStart, occEnd time.Time // the occurrence as shown, wall clock
}

// shiftOf reads the edit of an occurrence's times relative to the occurrence as
// shown (so a hand-moved exception shifts the series by what the person moved).
func (s *graphSeries) shiftOf(edit EventEdit) (msShift, error) {
	var sh msShift
	ev, ok := s.occ.event()
	if !ok {
		return sh, locked(LockSeriesUnsupported)
	}
	sh.occStart = s.wall(ev.Start)
	sh.occEnd = sh.occStart
	if ev.End != nil {
		sh.occEnd = s.wall(*ev.End)
	}
	if edit.When == nil {
		return sh, nil
	}
	if edit.When.AllDay != s.master.IsAllDay {
		return sh, invalid("start") // an all-day switch of a Microsoft series is not written
	}
	ns, ne := s.wall(edit.When.Start), s.wall(edit.When.End)
	if !ne.After(ns) {
		return sh, invalid("end")
	}
	ms, mok := s.pointOf(s.master.Start, s.master.IsAllDay)
	me, eok := s.pointOf(s.master.End, s.master.IsAllDay)
	if !mok || !eok {
		return sh, locked(LockSeriesUnsupported)
	}
	ds, dur := ns.Sub(sh.occStart), (ne.Sub(ns))-(sh.occEnd.Sub(sh.occStart))
	sh.dayShift = daysBetween(ns, sh.occStart)
	sh.start, sh.end = ms.Add(ds), ms.Add(ds).Add(me.Sub(ms)+dur)
	sh.changed = ds != 0 || dur != 0
	return sh, nil
}

// dt writes a wall time of the series' zone as Graph's dateTimeTimeZone; an
// all-day time is a date at midnight in the mailbox zone.
func (s *graphSeries) dt(t time.Time, e graphEvents) graphDateTime {
	if s.master.IsAllDay {
		return e.point(calfeed.Point{T: time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC), AllDay: true})
	}
	return graphDateTime{DateTime: t.Format(graphLayout), TimeZone: s.win}
}

// recurrenceFor is the recurrence the master has after the edit: the edit's own
// repeat, or the master's moved by the days the edit moved it; nil when it stays.
func (s *graphSeries) recurrenceFor(edit EventEdit, sh msShift) (*graphRecurrence, error) {
	old := s.master.Recurrence
	if edit.Repeat != nil {
		if edit.Repeat.Rule == nil {
			return nil, invalid("repeat") // stopping a Microsoft series is not written
		}
		start := sh.start
		if !sh.changed {
			ms, _ := s.pointOf(s.master.Start, s.master.IsAllDay)
			start = ms
		}
		rec, err := recurrenceOf(*edit.Repeat.Rule, start, s.win)
		if err != nil {
			return nil, invalid("repeat")
		}
		return rec, nil
	}
	if sh.dayShift == 0 {
		return nil, nil
	}
	if !old.Pattern.shiftable(sh.dayShift) {
		return nil, invalid("start")
	}
	moved := *old
	moved.Pattern = old.Pattern.rotated(sh.dayShift)
	moved.Range.StartDate = dateOf(sh.start)
	if end, err := time.Parse("2006-01-02", old.Range.EndDate); err == nil && strings.EqualFold(old.Range.Type, "endDate") {
		moved.Range.EndDate = dateOf(end.AddDate(0, 0, sh.dayShift))
	}
	return &moved, nil
}

// updateAll patches the series master (design D9). Graph drops the exceptions when
// the master's times or pattern change; a rename or a change of reminder or
// location leaves them.
func (e graphEvents) updateAll(ctx context.Context, ref EventRef, edit EventEdit, s *graphSeries) (EventState, error) {
	sh, err := s.shiftOf(edit)
	if err != nil {
		return EventState{}, err
	}
	body := map[string]any{}
	if edit.Summary != nil {
		body["subject"] = *edit.Summary
	}
	e.editMembers(body, edit)
	if err := e.reminderMembers(body, edit); err != nil {
		return EventState{}, err
	}
	if edit.Description != nil {
		if _, rich, err := e.bodyText(ctx, s.master.ID, s.master); err != nil {
			return EventState{}, err
		} else if rich {
			return EventState{}, locked(LockRichDescription)
		}
	}
	if sh.changed {
		body["start"], body["end"] = s.dt(sh.start, e), s.dt(sh.end, e)
	}
	rec, err := s.recurrenceFor(edit, sh)
	if err != nil {
		return EventState{}, err
	}
	if rec != nil {
		body["recurrence"] = rec
	}
	if len(body) == 0 {
		return EventState{}, invalid("change")
	}
	if _, _, err := e.patch(ctx, s.master.ID, s.master.ETag, body); err != nil {
		return EventState{}, err
	}
	written, err := e.instancesOf(ctx, ref.Calendar, s.master.ID)
	if err != nil {
		return EventState{}, err
	}
	target := dateOf(s.wall(mustPoint(s.occ.Start, s.occ.IsAllDay)).AddDate(0, 0, sh.dayShift))
	return e.seriesState(ref, written, s, target, true), nil
}

func mustPoint(d *graphDateTime, allDay bool) calfeed.Point {
	p, _ := d.point(allDay)
	return p
}

// patch sends a PATCH with If-Match and returns the id and the new ETag.
func (e graphEvents) patch(ctx context.Context, id, etag string, body map[string]any) (string, string, error) {
	resp, err := e.g.do(ctx, http.MethodPatch, eventPath(id), map[string]string{"If-Match": etag}, body)
	if err != nil {
		return "", "", err
	}
	if err := refusal(resp.Status); err != nil {
		return "", "", err
	}
	var ev graphEvent
	if json.Unmarshal(resp.Body, &ev) != nil || ev.ID == "" {
		return "", "", &Error{Reason: ReasonInvalid}
	}
	return ev.ID, ev.ETag, nil
}

// instancesOf reads the occurrences of a master over the window the cache keeps.
func (e graphEvents) instancesOf(ctx context.Context, calendar, masterID string) ([]WrittenObject, error) {
	win := calfeed.StoreWindow(e.now())
	next := eventPath(masterID) + "/instances?" + query(url.Values{"startDateTime": {graphStamp(win.From)}, "endDateTime": {graphStamp(win.To)},
		"$top": {"250"}, "$select": {eventSelect}})
	var out []WrittenObject
	for page := 0; next != ""; page++ {
		if page >= maxGraphPages {
			return nil, &Error{Reason: ReasonTooLarge}
		}
		var p graphPage
		if err := e.g.get(ctx, next, &p); err != nil {
			return nil, e.mapRead(err)
		}
		for i := range p.Value {
			if r, ok := p.Value[i].remote(); ok {
				out = append(out, WrittenObject{Ref: EventRef{Calendar: calendar, Href: p.Value[i].ID, ETag: p.Value[i].ETag}, Events: r.Events, Locked: r.Locked, Series: r.Series})
			}
		}
		next = p.Next
	}
	return out, nil
}

// seriesState is the state a series write leaves: the occurrences read again and
// the edited one picked by its date (the first one when none has it).
func (e graphEvents) seriesState(ref EventRef, written []WrittenObject, s *graphSeries, date string, refetch bool) EventState {
	wr := SeriesWrite{Written: written, Refetch: refetch, UID: ref.Href}
	if len(written) > 0 {
		pick := written[0]
		for _, o := range written {
			if dateOf(s.wall(o.Events[0].Start)) == date {
				pick = o
				break
			}
		}
		wr.UID, wr.Instance = pick.Events[0].UID, calfeed.OccurrenceOf(pick.Events[0]).Instance
	}
	st := EventState{Wrote: wr}
	if len(written) > 0 {
		st.Ref, st.Event, st.Locked = written[0].Ref, written[0].Events[0], written[0].Locked
	}
	return st
}

// first reports whether the occurrence is the first the series has: its
// date is the range's start date.
func (s *graphSeries) first() bool {
	ev, ok := s.occ.event()
	return ok && dateOf(s.wall(ev.Start)) == s.master.Recurrence.Range.StartDate
}

// updateFollowing ends the master before the occurrence and creates a new series
// from it (Graph has no split): the new series first, so a failure never loses
// occurrences; a failure of the second write deletes the new one again.
func (e graphEvents) updateFollowing(ctx context.Context, ref EventRef, edit EventEdit, s *graphSeries) (EventState, error) {
	if s.first() {
		return e.updateAll(ctx, ref, edit, s)
	}
	m := s.master
	if m.HasAttachments || m.IsOnlineMeeting || !splittable(m.Recurrence.Pattern) {
		return EventState{}, ErrFollowingUnsupported // the copy would lose them
	}
	sh, err := s.shiftOf(edit)
	if err != nil {
		return EventState{}, err
	}
	start, end := sh.occStart, sh.occEnd
	if edit.When != nil {
		start, end = s.wall(edit.When.Start), s.wall(edit.When.End)
	}
	if err := e.reminderMembers(map[string]any{}, edit); err != nil {
		return EventState{}, err
	}
	split := dateOf(sh.occStart)
	rec, err := e.newRecurrence(ctx, ref, edit, s, start, daysBetween(start, sh.occStart))
	if err != nil {
		return EventState{}, err
	}
	body := e.copyOf(s, edit, start, end, rec, calfeed.NewSeriesUID(m.ID, split, m.ETag))
	created, err := e.createCopy(ctx, ref.Calendar, body)
	if err != nil {
		return EventState{}, err
	}
	endDate := mustDate(split).AddDate(0, 0, -1)
	old := *m.Recurrence
	old.Range = graphRange{Type: "endDate", StartDate: m.Recurrence.Range.StartDate, EndDate: dateOf(endDate), RecurrenceTimeZone: s.win}
	if _, _, err := e.patch(ctx, m.ID, m.ETag, map[string]any{"recurrence": old}); err != nil {
		if !e.endedAlready(ctx, m.ID, old.Range.EndDate, err) {
			return EventState{}, e.rollback(ctx, created, err)
		}
	}
	fresh, err := e.instancesOf(ctx, ref.Calendar, created.ID)
	if err != nil {
		return EventState{}, err
	}
	rest, err := e.instancesOf(ctx, ref.Calendar, m.ID)
	if err != nil {
		return EventState{}, err
	}
	st := e.seriesState(ref, append(fresh, rest...), s, dateOf(start), true)
	return st, nil
}

func mustDate(s string) time.Time {
	t, _ := time.Parse("2006-01-02", s)
	return t
}

// splittable reports whether a pattern is one the editor writes (a copy of any
// other would be guessed at).
func splittable(p graphPattern) bool {
	switch strings.ToLower(p.Type) {
	case "daily", "weekly", "absolutemonthly", "relativemonthly", "absoluteyearly":
		return true
	}
	return false
}

// newRecurrence is the recurrence of the new series: the edit's own repeat, or
// the master's pattern (moved by the days the edit moved it) from the occurrence
// on, with a numbered range lowered by the occurrences before it.
func (e graphEvents) newRecurrence(ctx context.Context, ref EventRef, edit EventEdit, s *graphSeries, start time.Time, dayShift int) (*graphRecurrence, error) {
	if edit.Repeat != nil {
		if edit.Repeat.Rule == nil {
			return nil, invalid("repeat")
		}
		rec, err := recurrenceOf(*edit.Repeat.Rule, start, s.win)
		if err != nil {
			return nil, invalid("repeat")
		}
		return rec, nil
	}
	old := s.master.Recurrence
	if !old.Pattern.shiftable(dayShift) {
		return nil, invalid("start")
	}
	rec := &graphRecurrence{Pattern: old.Pattern.rotated(dayShift), Range: graphRange{Type: old.Range.Type, StartDate: dateOf(start), RecurrenceTimeZone: s.win}}
	if dayShift == 0 {
		rec.Pattern = old.Pattern
	}
	switch strings.ToLower(old.Range.Type) {
	case "enddate":
		rec.Range.EndDate = dateOf(mustDate(old.Range.EndDate).AddDate(0, 0, dayShift))
	case "numbered":
		before, err := e.countBefore(ctx, s)
		if err != nil {
			return nil, err
		}
		rec.Range.NumberOfOccurrences = old.Range.NumberOfOccurrences - before
		if rec.Range.NumberOfOccurrences < 1 {
			return nil, ErrFollowingUnsupported
		}
	}
	return rec, nil
}

// countBefore counts the occurrences of the master before the edited one.
func (e graphEvents) countBefore(ctx context.Context, s *graphSeries) (int, error) {
	ev, _ := s.occ.event()
	from := mustDate(s.master.Recurrence.Range.StartDate).AddDate(0, 0, -1)
	next := eventPath(s.master.ID) + "/instances?" + query(url.Values{"startDateTime": {graphStamp(from)}, "endDateTime": {graphStamp(ev.Start.T)},
		"$top": {"250"}, "$select": {"id"}})
	n := 0
	for page := 0; next != ""; page++ {
		if page >= maxGraphPages {
			return 0, &Error{Reason: ReasonTooLarge}
		}
		var p graphPage
		if err := e.g.get(ctx, next, &p); err != nil {
			return 0, e.mapRead(err)
		}
		n += len(p.Value)
		next = p.Next
	}
	return n, nil
}

// copyOf is the new event of a split: an allowlist of the master's members, the
// edit's changes, the new times and the recurrence, with a transaction id derived
// from the split, so a retry after a lost answer makes no second series.
func (e graphEvents) copyOf(s *graphSeries, edit EventEdit, start, end time.Time, rec *graphRecurrence, key string) map[string]any {
	m := s.master
	body := map[string]any{"subject": m.Subject, "isAllDay": m.IsAllDay, "importance": firstOf(m.Importance, "normal"),
		"sensitivity": firstOf(m.Sensitivity, "normal"), "categories": append([]string{}, m.Categories...),
		"showAs": firstOf(m.ShowAs, "busy"), "isReminderOn": m.IsReminderOn, "recurrence": rec,
		"start": s.dt(start, e), "end": s.dt(end, e), "transactionId": transactionPrefix + "sp-" + hash16(key)}
	if m.ReminderMinutes != nil {
		body["reminderMinutesBeforeStart"] = *m.ReminderMinutes
	}
	if m.Body != nil {
		body["body"] = map[string]string{"contentType": m.Body.ContentType, "content": m.Body.Content}
	}
	if m.Location != nil {
		body["location"] = map[string]string{"displayName": m.Location.DisplayName}
	}
	if edit.Summary != nil {
		body["subject"] = *edit.Summary
	}
	e.editMembers(body, edit)
	_ = e.reminderMembers(body, edit) // validated before the first write
	return body
}

// createCopy posts the new series and reads its id and version.
func (e graphEvents) createCopy(ctx context.Context, calendar string, body map[string]any) (*graphEvent, error) {
	resp, err := e.g.do(ctx, http.MethodPost, calendarPath(calendar)+"/events", nil, body)
	if err != nil {
		return nil, err
	}
	if resp.Status == http.StatusNotFound {
		return nil, &Error{Reason: ReasonCalendarMissing}
	}
	if resp.Status < 200 || resp.Status > 299 {
		if err := graphStatus(resp.Status); err != nil {
			if reasonOf(err) == ReasonCalendarReadOnly {
				return nil, ErrCalendarRO
			}
			return nil, err
		}
	}
	var created graphEvent
	if json.Unmarshal(resp.Body, &created) != nil || created.ID == "" {
		return nil, &Error{Reason: ReasonInvalid}
	}
	return &created, nil
}

// endedAlready reports whether a master whose patch failed in a way that may have
// been applied holds the range that was sent.
func (e graphEvents) endedAlready(ctx context.Context, id, endDate string, cause error) bool {
	if refusalIsDefinite(cause) {
		return false
	}
	m, err := e.fetch(ctx, id, "id,recurrence")
	return err == nil && m.Recurrence != nil && m.Recurrence.Range.EndDate == endDate && strings.EqualFold(m.Recurrence.Range.Type, "endDate")
}

// refusalIsDefinite is true for an error that says the request was refused (and
// so not applied): a 412, a 404 or a 403.
func refusalIsDefinite(err error) bool {
	return err == ErrEventChanged || err == ErrEventGone || err == ErrCalendarRO
}

// rollback deletes the new series of a split after the master could not be ended,
// conditionally on the version it was created with. The error of the second write
// is returned when the rollback worked, ErrPartialWrite when it did not.
func (e graphEvents) rollback(ctx context.Context, created *graphEvent, cause error) error {
	if created.ETag == "" {
		return ErrPartialWrite
	}
	resp, err := e.g.do(ctx, http.MethodDelete, eventPath(created.ID), map[string]string{"If-Match": created.ETag}, nil)
	if err != nil || (resp.Status != http.StatusNotFound && refusal(resp.Status) != nil) {
		return ErrPartialWrite
	}
	return cause
}

// DeleteScoped removes an occurrence for a scope: this deletes it (Graph records a
// cancelled exception), all deletes the master, following ends the master before it.
func (e graphEvents) DeleteScoped(ctx context.Context, ref EventRef, scope calfeed.Scope) (SeriesWrite, error) {
	if ref.ETag == "" {
		return SeriesWrite{}, locked(LockNoETag)
	}
	if !scope.Valid() {
		return SeriesWrite{}, invalid("scope")
	}
	if scope == calfeed.ScopeThis {
		resp, err := e.g.do(ctx, http.MethodDelete, eventPath(ref.Href), map[string]string{"If-Match": ref.ETag}, nil)
		if err != nil {
			return SeriesWrite{}, err
		}
		if err := refusal(resp.Status); err != nil {
			return SeriesWrite{}, err
		}
		return SeriesWrite{Deleted: []string{ref.Href}}, nil
	}
	s, err := e.readSeries(ctx, ref)
	if err != nil {
		return SeriesWrite{}, err
	}
	if scope == calfeed.ScopeAll || s.first() {
		resp, err := e.g.do(ctx, http.MethodDelete, eventPath(s.master.ID), map[string]string{"If-Match": s.master.ETag}, nil)
		if err != nil {
			return SeriesWrite{}, err
		}
		if err := refusal(resp.Status); err != nil {
			return SeriesWrite{}, err
		}
		return SeriesWrite{Deleted: []string{s.master.ID}}, nil
	}
	ev, _ := s.occ.event()
	endDate := dateOf(s.wall(ev.Start).AddDate(0, 0, -1))
	rec := *s.master.Recurrence
	rec.Range = graphRange{Type: "endDate", StartDate: s.master.Recurrence.Range.StartDate, EndDate: endDate, RecurrenceTimeZone: s.win}
	if _, _, err := e.patch(ctx, s.master.ID, s.master.ETag, map[string]any{"recurrence": rec}); err != nil {
		return SeriesWrite{}, err
	}
	written, err := e.instancesOf(ctx, ref.Calendar, s.master.ID)
	if err != nil {
		return SeriesWrite{}, err
	}
	return SeriesWrite{Written: written, Refetch: true}, nil
}

// reminderMembers adds the reminder members of an edit: Microsoft takes one,
// counted in minutes before the start and not negative; none turns it off.
func (e graphEvents) reminderMembers(body map[string]any, edit EventEdit) error {
	if edit.Reminders == nil {
		return nil
	}
	switch list := *edit.Reminders; {
	case len(list) == 0:
		body["isReminderOn"] = false
	case len(list) == 1 && list[0] >= 0:
		body["isReminderOn"], body["reminderMinutesBeforeStart"] = true, list[0]
	default:
		return invalid("reminders")
	}
	return nil
}
