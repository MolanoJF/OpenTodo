package calsync

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/OpenTodoApp/opentodo/internal/caldav"
	"github.com/OpenTodoApp/opentodo/internal/calfeed"
)

// Scoped changes of a repeating event in a CalDAV or Google calendar
// (calendar-event-series D6, D7): the series engine edits the parsed object, the
// writer writes what it leaves, new object first.

// changeOf is the engine's view of an edit.
func changeOf(edit EventEdit) calfeed.SeriesChange {
	ch := calfeed.SeriesChange{Summary: edit.Summary, Location: edit.Location, Description: edit.Description, Free: edit.Free, Reminders: edit.Reminders}
	if edit.When != nil {
		ch.When = &calfeed.Span{Start: edit.When.Start, End: edit.When.End, AllDay: edit.When.AllDay}
	}
	if edit.Repeat != nil {
		rule := edit.Repeat.Rule
		ch.Repeat = &rule
	}
	return ch
}

// engineError maps an engine refusal onto the writer's errors.
func engineError(err error) error {
	switch {
	case errors.Is(err, calfeed.ErrNoSuchInstance):
		return ErrEventChanged // the series is not what the caller saw
	case errors.Is(err, calfeed.ErrSeriesUnsupported):
		return locked(LockSeriesUnsupported)
	case errors.Is(err, calfeed.ErrShiftUnsupported):
		return invalid("start")
	case errors.Is(err, calfeed.ErrSpanInvalid):
		return invalid("end")
	case errors.Is(err, calfeed.ErrRepeatThis), errors.Is(err, calfeed.ErrRepeatStart), errors.Is(err, calfeed.ErrRepeatInvalid):
		return invalid("repeat")
	}
	return err
}

// seriesEdit builds the engine's edit for the occurrence ref names.
func (e davEvents) seriesEdit(ref EventRef, root *caldav.Component, scope calfeed.Scope) (calfeed.SeriesEdit, error) {
	v, err := calfeed.ReadSeries(root)
	if err != nil {
		return calfeed.SeriesEdit{}, locked(LockSeriesUnsupported)
	}
	return calfeed.SeriesEdit{Root: root, UID: v.UID, Instance: ref.Instance, Scope: scope, Now: e.now(),
		NewUID: calfeed.NewSeriesUID(v.UID, ref.Instance, ref.ETag)}, nil
}

// updateSeries applies a scoped edit of one occurrence.
func (e davEvents) updateSeries(ctx context.Context, ref EventRef, edit EventEdit, root *caldav.Component) (EventState, error) {
	if !edit.Scope.Valid() {
		return EventState{}, invalid("scope")
	}
	se, err := e.seriesEdit(ref, root, edit.Scope)
	if err != nil {
		return EventState{}, err
	}
	se.Change = changeOf(edit)
	res, err := calfeed.ApplySeries(se)
	if err != nil {
		return EventState{}, engineError(err)
	}
	wr, err := e.writeSeries(ctx, ref, res)
	if err != nil {
		return EventState{}, err
	}
	st := EventState{Wrote: wr}
	if len(wr.Written) > 0 {
		st.Ref, st.Event, st.Locked = wr.Written[0].Ref, wr.Written[0].Events[0], wr.Written[0].Locked
	}
	return st, nil
}

// DeleteScoped removes an occurrence for a scope.
func (e davEvents) DeleteScoped(ctx context.Context, ref EventRef, scope calfeed.Scope) (SeriesWrite, error) {
	_, root, err := e.checkRead(ctx, ref)
	if err != nil {
		return SeriesWrite{}, err
	}
	if !recurring(root) || ref.Instance == "" {
		return SeriesWrite{}, ErrEventChanged
	}
	if !scope.Valid() {
		return SeriesWrite{}, invalid("scope")
	}
	se, err := e.seriesEdit(ref, root, scope)
	if err != nil {
		return SeriesWrite{}, err
	}
	res, err := calfeed.DeleteSeries(se)
	if err != nil {
		return SeriesWrite{}, engineError(err)
	}
	return e.writeSeries(ctx, ref, res)
}

// objectHref is where a new object of a calendar goes: <calendar><uid>.ics.
func objectHref(calendar, uid string) string {
	return strings.TrimSuffix(calendar, "/") + "/" + url.PathEscape(uid) + ".ics"
}

// writeSeries writes what the engine left: the new object first (If-None-Match),
// then the old one (If-Match), with the rollback of the new one when the second
// write fails. A series' last occurrence, or all of it, deletes the object.
func (e davEvents) writeSeries(ctx context.Context, ref EventRef, res calfeed.SeriesResult) (SeriesWrite, error) {
	wr := SeriesWrite{UID: res.UID, Instance: res.Instance, Refetch: res.Refetch}
	if res.DeleteOrigin {
		status, err := e.d.del(ctx, ref.Href, ref.ETag)
		if err != nil {
			return SeriesWrite{}, err
		}
		if err := refusal(status); err != nil {
			return SeriesWrite{}, err
		}
		wr.Deleted = []string{ref.Href}
		return wr, nil
	}
	var fresh, origin *calfeed.SeriesObject
	for i := range res.Objects {
		if res.Objects[i].New {
			fresh = &res.Objects[i]
		} else {
			origin = &res.Objects[i]
		}
	}
	var newRef EventRef
	if fresh != nil {
		obj, err := e.createSplit(ctx, ref, *fresh)
		if err != nil {
			return SeriesWrite{}, err
		}
		newRef = obj.Ref
		wr.Written = append(wr.Written, obj)
	}
	if origin != nil {
		body := origin.Root.Encode()
		etag, err := e.putOrigin(ctx, ref, origin, body, fresh != nil)
		if err != nil {
			if fresh != nil {
				err = e.rollback(ctx, newRef, err)
			}
			return SeriesWrite{}, err
		}
		obj, err := writtenOf(EventRef{Calendar: ref.Calendar, Href: ref.Href, ETag: etag}, body)
		if err != nil {
			return SeriesWrite{}, err
		}
		wr.Written = append(wr.Written, obj)
	}
	return wr, nil
}

// createSplit writes the new object of a split at its own address with
// If-None-Match: *. An object of the same uid already there is the replay of a
// split that was half done: it is written again, conditionally on the version
// found, so it holds this change. Another uid at the address is href_taken.
func (e davEvents) createSplit(ctx context.Context, ref EventRef, ob calfeed.SeriesObject) (WrittenObject, error) {
	href := objectHref(ref.Calendar, ob.UID)
	body := ob.Root.Encode()
	status, etag, err := e.d.put(ctx, href, "*", body)
	if err != nil {
		return WrittenObject{}, err
	}
	switch status {
	case http.StatusPreconditionFailed:
		cur, _, gerr := e.read(ctx, EventRef{Calendar: ref.Calendar, Href: href})
		if gerr != nil {
			return WrittenObject{}, gerr
		}
		if cur.Event.UID != ob.UID {
			return WrittenObject{}, &Error{Reason: ReasonHrefTaken}
		}
		if cur.Ref.ETag == "" {
			return WrittenObject{}, ErrEventChanged // no version to write over: not writing blind
		}
		if etag, err = e.putObject(ctx, href, cur.Ref.ETag, body); err != nil {
			return WrittenObject{}, err
		}
	case http.StatusNotFound:
		return WrittenObject{}, &Error{Reason: ReasonCalendarMissing}
	default:
		if err := refusal(status); err != nil {
			return WrittenObject{}, err
		}
		if etag == "" {
			if gs, ge, _, gerr := e.d.getObject(ctx, href); gerr == nil && refusal(gs) == nil {
				etag = ge
			}
		}
	}
	return writtenOf(EventRef{Calendar: ref.Calendar, Href: href, ETag: etag}, body)
}

// putOrigin writes the old object of a split (or the only object of a this or
// all change) with If-Match. In a split, a failure that may have been applied (a
// transport error or a server error) is looked at before anything is rolled back:
// when the object holds the rule that was sent the write went through.
func (e davEvents) putOrigin(ctx context.Context, ref EventRef, ob *calfeed.SeriesObject, body []byte, split bool) (string, error) {
	status, etag, err := e.d.put(ctx, ref.Href, ref.ETag, body)
	failure := err
	if err == nil {
		failure = refusal(status)
	}
	if failure == nil {
		if etag == "" {
			if gs, ge, _, gerr := e.d.getObject(ctx, ref.Href); gerr == nil && refusal(gs) == nil {
				etag = ge
			}
		}
		return etag, nil
	}
	if (err == nil && status < 500) || !split {
		return "", failure // refused, or nothing to roll back: the next read shows what happened
	}
	return e.settle(ctx, ref, ob, failure)
}

// settle decides what an ambiguous failure of the second write was: applied (the
// object holds our rule: the version it has now), not applied (the version we
// wrote over is still there) or unknown (ErrPartialWrite: nothing is undone).
func (e davEvents) settle(ctx context.Context, ref EventRef, ob *calfeed.SeriesObject, failure error) (string, error) {
	status, etag, data, err := e.d.getObject(ctx, ref.Href)
	if err != nil || status != http.StatusOK {
		return "", ErrPartialWrite
	}
	if etag != "" && etag == ref.ETag {
		return "", failure // the write did not happen
	}
	_, root, perr := calfeed.ParseCalendarRoot(data)
	want, werr := calfeed.ReadSeries(ob.Root)
	if perr == nil && werr == nil {
		if got, gerr := calfeed.ReadSeries(root); gerr == nil && got.RRule == want.RRule && etag != "" {
			return etag, nil // it went through
		}
	}
	return "", ErrEventChanged // somebody else changed it meanwhile; ours did not apply
}

// rollback deletes the new object of a split after the old one could not be
// written, conditionally on the version the new one answered with. The error of
// the second write is returned when the rollback worked, ErrPartialWrite when it
// did not. An ambiguous or unknown state is never rolled back.
func (e davEvents) rollback(ctx context.Context, newRef EventRef, cause error) error {
	if errors.Is(cause, ErrPartialWrite) {
		return cause
	}
	etag := newRef.ETag
	if etag == "" {
		if gs, ge, _, gerr := e.d.getObject(ctx, newRef.Href); gerr == nil && refusal(gs) == nil {
			etag = ge
		}
	}
	if etag == "" {
		return ErrPartialWrite // nothing to be conditional on
	}
	status, err := e.d.del(ctx, newRef.Href, etag)
	if err != nil || (status != http.StatusNotFound && refusal(status) != nil) {
		return ErrPartialWrite
	}
	return cause
}

// Detail is Read plus the description, the repeat and the reminders of the event.
func (e davEvents) Detail(ctx context.Context, ref EventRef) (EventState, error) {
	st, root, err := e.read(ctx, ref)
	if err != nil {
		return EventState{}, err
	}
	vev := calfeed.OccurrenceVEvent(root, ref.Instance)
	if vev == nil {
		return EventState{}, &Error{Reason: ReasonInvalid}
	}
	if p := vev.Prop("DESCRIPTION"); p != nil {
		st.Description = caldav.UnescapeText(p.Value)
	}
	st.Reminders, st.RemindersOther = calfeed.ReadReminders(vev)
	st.RemindersMax = maxDAVReminders
	v, err := calfeed.ReadSeries(root)
	if err != nil || !v.Recurring {
		return st, nil
	}
	st.Series = &SeriesDetail{RRule: v.RRule, Repeat: v.Repeat, Custom: v.Custom, Exceptions: v.Exceptions}
	// The object as read, for a cache that is behind it.
	body := root.Encode()
	if obj, err := writtenOf(st.Ref, body); err == nil {
		st.Wrote = SeriesWrite{Written: []WrittenObject{obj}, UID: v.UID, Instance: ref.Instance}
	}
	return st, nil
}
