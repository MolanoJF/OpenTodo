package calsync

import (
	"context"
	"errors"

	"github.com/OpenTodoApp/opentodo/internal/calfeed"
)

// Answering an invitation on a CalDAV account (calendar-event-guests D6). Only Google
// replies, and only while googleRespond is true: generic servers differ in what they
// do with an attendee's write (sabre-based ones send iTIP, Radicale does nothing) and
// are never written to for a guest event. The write changes the user's own PARTSTAT
// and nothing else: no SEQUENCE or DTSTAMP, no Schedule-Reply header, no
// SCHEDULE-AGENT (nothing is known about how Google tells the organiser), and the
// object is read back so an answer the server did not keep is reported.

// Reply answers an invitation while the event still has ref.ETag.
func (e davEvents) Reply(ctx context.Context, ref EventRef, reply ReplyEdit) (EventState, error) {
	owner := e.ownerOf(ref.Calendar)
	if !e.caps.Respond || owner == "" || !reply.Response.Valid() {
		return EventState{}, ErrCannotRespond
	}
	if ref.ETag == "" {
		return EventState{}, locked(LockNoETag)
	}
	st, root, err := e.read(ctx, ref)
	if err != nil {
		return EventState{}, err
	}
	switch {
	case st.Ref.ETag != ref.ETag:
		return EventState{}, ErrEventChanged
	case st.Locked == LockSeriesUnsupported || st.Locked == LockNoETag:
		return EventState{}, locked(st.Locked)
	}
	res, err := calfeed.ApplyRespond(root, calfeed.Respond{Owner: owner, Response: string(reply.Response), Scope: reply.Scope, Instance: ref.Instance})
	if err != nil {
		return EventState{}, respondError(err)
	}
	if len(res.Changed) == 0 { // already answered so: nothing is written
		return keptState(st, reply)
	}
	if !calfeed.RoundTrips(root) { // never write what could not be read back unchanged
		return EventState{}, locked(LockSeriesUnsupported)
	}
	if _, err := e.putObject(ctx, ref.Href, ref.ETag, root.Encode()); err != nil {
		return EventState{}, err
	}
	status, etag, data, err := e.d.getObject(ctx, ref.Href)
	if err != nil {
		return EventState{}, err
	}
	if err := refusal(status); err != nil {
		return EventState{}, err
	}
	obj, err := e.writtenOf(EventRef{Calendar: ref.Calendar, Href: ref.Href, ETag: etag, Instance: ref.Instance}, data)
	if err != nil {
		return EventState{}, err
	}
	out := obj.state()
	if obj.Series != nil {
		out.Wrote = SeriesWrite{Written: []WrittenObject{obj}, UID: obj.Events[0].UID, Instance: ref.Instance}
	}
	if !holdsAnswer(out.Guests.You, reply.Response) {
		return out, ErrResponseNotKept
	}
	return out, nil
}

// keptState is the state of an event that already holds the answer.
func keptState(st EventState, reply ReplyEdit) (EventState, error) {
	if !holdsAnswer(st.Guests.You, reply.Response) {
		return st, ErrResponseNotKept
	}
	return st, nil
}

// holdsAnswer compares the user's answer as the calendar reads it with the one asked for.
func holdsAnswer(you string, want Answer) bool { return you == string(want) }

// respondError maps the engine's refusals onto the writer's errors.
func respondError(err error) error {
	switch {
	case errors.Is(err, calfeed.ErrNotInvited), errors.Is(err, calfeed.ErrIsOrganizer), errors.Is(err, calfeed.ErrOwnerUnknown):
		return ErrCannotRespond
	case errors.Is(err, calfeed.ErrRespondThis):
		return invalid("scope")
	case errors.Is(err, calfeed.ErrBadResponse):
		return invalid("response")
	case errors.Is(err, calfeed.ErrSeriesUnsupported):
		return locked(LockSeriesUnsupported)
	}
	return err
}
