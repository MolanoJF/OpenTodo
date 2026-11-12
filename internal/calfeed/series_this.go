package calfeed

import (
	"errors"
	"time"

	"github.com/OpenTodoApp/opentodo/internal/caldav"
)

// ErrSpanInvalid is a new start and end where the end is before the start.
var ErrSpanInvalid = errors.New("end before start")

// ApplySeries applies a scoped change to a parsed calendar object (design D6).
// The tree is modified in place; the result lists the objects to write.
func ApplySeries(e SeriesEdit) (SeriesResult, error) {
	if unsupportedWhy(e.Root) != "" || hasGuests(e.Root) {
		return SeriesResult{}, ErrSeriesUnsupported
	}
	o, err := loadSeries(e.Root, e.UID)
	if err != nil {
		return SeriesResult{}, err
	}
	var res SeriesResult
	switch e.Scope {
	case ScopeThis:
		res, err = applyThis(o, e)
	case ScopeAll:
		res, err = applyAll(o, e)
	case ScopeFollowing:
		res, err = applyFollowing(o, e)
	default:
		return SeriesResult{}, ErrNoSuchInstance
	}
	if err != nil {
		return SeriesResult{}, err
	}
	for _, ob := range res.Objects {
		if !RoundTrips(ob.Root) {
			return SeriesResult{}, ErrSeriesUnsupported
		}
	}
	return res, nil
}

// DeleteSeries removes an occurrence for a scope: an excluded date (this), the
// end of the series before the occurrence (following) or the whole object (all).
func DeleteSeries(e SeriesEdit) (SeriesResult, error) {
	if unsupportedWhy(e.Root) != "" || hasGuests(e.Root) {
		return SeriesResult{}, ErrSeriesUnsupported
	}
	o, err := loadSeries(e.Root, e.UID)
	if err != nil {
		return SeriesResult{}, err
	}
	switch e.Scope {
	case ScopeThis:
		return deleteThis(o, e)
	case ScopeFollowing:
		return deleteFollowing(o, e)
	case ScopeAll:
		if _, err := o.locate(e.Instance); err != nil {
			return SeriesResult{}, err
		}
		return SeriesResult{DeleteOrigin: true, UID: o.uid}, nil
	}
	return SeriesResult{}, ErrNoSuchInstance
}

// fields are the members of an event the editor changes.
type fields struct {
	summary, location, description string
	free                           bool
	reminders                      []int
}

func fieldsOf(c *caldav.Component) fields {
	f := fields{}
	text := func(name string) string {
		if p := c.Prop(name); p != nil {
			return caldav.UnescapeText(p.Value)
		}
		return ""
	}
	f.summary, f.location, f.description = text("SUMMARY"), text("LOCATION"), text("DESCRIPTION")
	if p := c.Prop("TRANSP"); p != nil {
		f.free = p.Value == "TRANSPARENT"
	}
	f.reminders, _ = ReadReminders(c)
	return f
}

// applyFields sets the text, busy or free and reminder members of a change.
func applyFields(c *caldav.Component, ch SeriesChange) {
	if ch.Summary != nil {
		SetProp(c, caldav.Prop{Name: "SUMMARY", Value: caldav.EscapeText(*ch.Summary)}, true)
	}
	if ch.Location != nil {
		SetOrDelete(c, "LOCATION", *ch.Location)
	}
	if ch.Description != nil {
		SetOrDelete(c, "DESCRIPTION", *ch.Description)
		DropProp(c, "X-ALT-DESC")
	}
	if ch.Free != nil {
		transp := "OPAQUE"
		if *ch.Free {
			transp = "TRANSPARENT"
		}
		SetProp(c, caldav.Prop{Name: "TRANSP", Value: transp}, true)
	}
	if ch.Reminders != nil {
		SetReminders(c, *ch.Reminders)
	}
}

// setTimes writes a start and end into a VEVENT, in the form of the points and
// with the TZID text the event already uses.
func (o *sobj) setTimes(c *caldav.Component, start, end Point) {
	tmplS := caldav.Prop{Name: "DTSTART"}
	if p := c.Prop("DTSTART"); p != nil {
		tmplS = *p
	}
	tmplE := tmplS
	if p := c.Prop("DTEND"); p != nil {
		tmplE = *p
	}
	tmplE.Name = "DTEND"
	zones := zonesOf(o.root)
	s, e := rewriteProp(tmplS, start, zones), rewriteProp(tmplE, end, zones)
	ReplaceTimes(c, s, e)
	AddZone(o.root, s)
	AddZone(o.root, e)
}

// spanDur is the length of a span: whole days for all-day, elapsed time otherwise.
func spanDur(s, e Point) time.Duration {
	if s.AllDay {
		return civil(e).Sub(civil(s))
	}
	return e.T.Sub(s.T)
}

// endFrom places a duration after a start.
func endFrom(start Point, d time.Duration) Point {
	if start.AllDay {
		return fromCivil(civil(start).Add(d), start)
	}
	return Point{T: start.T.Add(d), Zone: start.Zone}
}

// lastVEvent is the index of the last VEVENT child.
func lastVEvent(root *caldav.Component) int {
	at := -1
	for i, c := range root.Children {
		if c.Name == "VEVENT" {
			at = i
		}
	}
	return at
}

// removeChild takes a component out of the tree.
func removeChild(root, c *caldav.Component) {
	out := root.Children[:0]
	for _, ch := range root.Children {
		if ch != c {
			out = append(out, ch)
		}
	}
	root.Children = out
}

// newException copies the master into an exception of one instance: no rule or
// excluded dates, a RECURRENCE-ID in the master's DTSTART form, the master's
// other properties and alarms (design D6).
func (o *sobj) newException(l located, now time.Time) *caldav.Component {
	c := cloneComp(o.m)
	for _, n := range []string{"RRULE", "RDATE", "EXDATE", "EXRULE"} {
		DropProp(c, n)
	}
	tmpl := caldav.Prop{Name: "RECURRENCE-ID"}
	if p := o.m.Prop("DTSTART"); p != nil {
		tmpl.Params = p.Params
	}
	insertAfter(c, rewriteProp(tmpl, l.slot, zonesOf(o.root)), "UID")
	o.setTimes(c, l.slot, endFrom(l.slot, masterDuration(o.mev)))
	bump(c, seqOf(o.m), now)
	at := lastVEvent(o.root)
	o.root.Children = append(o.root.Children[:at+1], append([]*caldav.Component{c}, o.root.Children[at+1:]...)...)
	return c
}

// applyThis writes one occurrence: a new exception, or the existing one edited
// in place. The master stays as it is.
func applyThis(o *sobj, e SeriesEdit) (SeriesResult, error) {
	if e.Change.Repeat != nil {
		return SeriesResult{}, ErrRepeatThis
	}
	l, err := o.locate(e.Instance)
	if err != nil {
		return SeriesResult{}, err
	}
	var target *caldav.Component
	if l.exc != nil {
		target = l.exc.c
		bump(target, seqOf(target), e.Now)
	} else {
		target = o.newException(l, e.Now)
	}
	applyFields(target, e.Change)
	if w := e.Change.When; w != nil {
		if spanDur(w.Start, w.End) < 0 {
			return SeriesResult{}, ErrSpanInvalid
		}
		o.setTimes(target, w.Start, w.End)
	}
	return SeriesResult{Objects: []SeriesObject{{UID: o.uid, Root: o.root}}, UID: o.uid, Instance: e.Instance}, nil
}

// deleteThis excludes one occurrence (and drops its exception); the object is
// deleted when that was the last one a finite series had.
func deleteThis(o *sobj, e SeriesEdit) (SeriesResult, error) {
	l, err := o.locate(e.Instance)
	if err != nil {
		return SeriesResult{}, err
	}
	if l.exc != nil {
		removeChild(o.root, l.exc.c)
	}
	tmpl := caldav.Prop{Name: "EXDATE"}
	if p := o.m.Prop("DTSTART"); p != nil {
		tmpl.Params = p.Params
	}
	insertAfter(o.m, rewriteProp(tmpl, l.slot, zonesOf(o.root)), "EXDATE", "RRULE")
	bump(o.m, seqOf(o.m), e.Now)
	res := SeriesResult{Objects: []SeriesObject{{UID: o.uid, Root: o.root}}, UID: o.uid}
	if again, err := loadSeries(o.root, o.uid); err == nil && again.liveCount(1) == 0 {
		res.Objects, res.DeleteOrigin = nil, true
	}
	return res, nil
}
