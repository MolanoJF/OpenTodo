package calsync

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
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
	return e.state(EventRef{Calendar: calendar}, &created)
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
	return st, nil
}
