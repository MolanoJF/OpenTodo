package calsync

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/OpenTodoApp/opentodo/internal/calfeed"
)

// Creating Graph events and reading their description (calendar-event-editor D7).

// transactionPrefix marks the transactionId of an event the editor created.
const transactionPrefix = "opentodo-ev-"

// showAs is Graph's showAs for the busy or free switch.
func showAs(free bool) string {
	if free {
		return "free"
	}
	return "busy"
}

// textBody is the plain-text body member of a write.
func textBody(s string) map[string]string {
	return map[string]string{"contentType": "text", "content": s}
}

// Create posts the event with a transactionId; the Graph id becomes the uid.
func (e graphEvents) Create(ctx context.Context, calendar string, ev EventNew) (EventState, error) {
	body := map[string]any{
		"subject":       ev.Summary,
		"start":         e.point(ev.When.Start),
		"end":           e.point(ev.When.End),
		"isAllDay":      ev.When.AllDay,
		"location":      map[string]string{"displayName": ev.Location},
		"showAs":        showAs(ev.Free),
		"transactionId": transactionPrefix + ev.UID,
	}
	if ev.Description != "" {
		body["body"] = textBody(ev.Description)
	}
	if err := e.reminderMembers(body, EventEdit{Reminders: &ev.Reminders}); err != nil && len(ev.Reminders) > 0 {
		return EventState{}, err
	}
	if ev.Repeat != nil {
		rec, err := e.recurrenceForSingle(EventState{Event: calfeed.Event{Start: ev.When.Start}}, EventEdit{Repeat: &RepeatEdit{Rule: ev.Repeat}})
		if err != nil {
			return EventState{}, err
		}
		body["recurrence"] = rec
	}
	resp, err := e.g.do(ctx, http.MethodPost, calendarPath(calendar)+"/events", nil, body)
	if err != nil {
		return EventState{}, err
	}
	if resp.Status < 200 || resp.Status > 299 {
		if err := graphStatus(resp.Status); err != nil {
			if reasonOf(err) == ReasonCalendarReadOnly {
				return EventState{}, ErrCalendarRO
			}
			return EventState{}, err
		}
	}
	var created graphEvent
	if json.Unmarshal(resp.Body, &created) != nil || created.ID == "" {
		return EventState{}, &Error{Reason: ReasonInvalid}
	}
	st, err := e.state(EventRef{Calendar: calendar}, &created)
	if err != nil || ev.Repeat == nil {
		return st, err
	}
	written, err := e.instancesOf(ctx, calendar, created.ID) // a series: the occurrences are what the cache holds
	if err != nil {
		return EventState{}, err
	}
	st.Wrote = SeriesWrite{Written: written, UID: created.ID}
	if len(written) > 0 {
		st.Wrote.UID, st.Wrote.Instance = written[0].Events[0].UID, calfeed.OccurrenceOf(written[0].Events[0]).Instance
		st.Ref, st.Event, st.Locked = written[0].Ref, written[0].Events[0], written[0].Locked
	}
	return st, nil
}

// bodyText is the description of an event read with its body: a text body is
// the description; an html body is read again as text, and is locked unless
// that text is empty (Outlook stores an empty body as an HTML skeleton).
func (e graphEvents) bodyText(ctx context.Context, href string, ev *graphEvent) (text string, rich bool, err error) {
	if ev.Body == nil {
		return "", false, nil
	}
	if !strings.EqualFold(ev.Body.ContentType, "html") {
		return ev.Body.Content, false, nil
	}
	var plain graphEvent
	err = e.g.getWith(ctx, eventPath(href)+"?"+query(url.Values{"$select": {"id,body"}}),
		map[string]string{"Prefer": `outlook.body-content-type="text"`}, &plain)
	if err != nil {
		return "", false, e.mapRead(err)
	}
	if plain.Body == nil {
		return "", false, nil
	}
	text = strings.TrimSpace(plain.Body.Content)
	return text, text != "", nil
}

// mapRead maps the errors of a read of one event onto the writer's errors.
func (e graphEvents) mapRead(err error) error {
	switch reasonOf(err) {
	case ReasonCalendarMissing:
		return ErrEventGone
	case ReasonCalendarReadOnly:
		return ErrCalendarRO
	}
	return err
}

// Detail is Read with the description.
func (e graphEvents) Detail(ctx context.Context, ref EventRef) (EventState, error) {
	var ev graphEvent
	err := e.g.get(ctx, eventPath(ref.Href)+"?"+query(url.Values{"$select": {eventSelect + ",body"}}), &ev)
	if err != nil {
		return EventState{}, e.mapRead(err)
	}
	st, err := e.state(ref, &ev)
	if err != nil {
		return EventState{}, err
	}
	text, rich, err := e.bodyText(ctx, st.Ref.Href, &ev)
	if err != nil {
		return EventState{}, err
	}
	st.Description, st.DescriptionLocked = text, rich
	st.RemindersMax = 1
	if ev.IsReminderOn && ev.ReminderMinutes != nil {
		st.Reminders = []int{*ev.ReminderMinutes}
	}
	if ev.inSeries() && ev.SeriesMasterID != "" {
		detail, err := e.seriesDetail(ctx, ev.SeriesMasterID)
		if err != nil {
			return EventState{}, err
		}
		st.Series = detail
	}
	return st, nil
}

// seriesDetail reads the master of a series for what the editor shows: the repeat
// in its form (nil and custom when it has none) and how many exceptions the
// master has.
func (e graphEvents) seriesDetail(ctx context.Context, masterID string) (*SeriesDetail, error) {
	var m graphEvent
	err := e.g.get(ctx, eventPath(masterID)+"?"+query(url.Values{"$select": {"id,type,recurrence"}, "$expand": {"exceptionOccurrences($select=id)"}}), &m)
	if err != nil {
		return nil, e.mapRead(err)
	}
	d := &SeriesDetail{Exceptions: len(m.Exceptions)}
	if r, ok := repeatOfRecurrence(m.Recurrence); ok {
		d.Repeat = &r
	} else {
		d.Custom = true
	}
	return d, nil
}
