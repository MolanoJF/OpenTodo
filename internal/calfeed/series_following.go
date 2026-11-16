package calfeed

import (
	"errors"
	"strings"
	"time"

	"github.com/OpenTodoApp/opentodo/internal/caldav"
)

// ErrNewUID is a following edit without the uid of the series it creates.
var ErrNewUID = errors.New("following edit needs a new uid")

// filterExdates keeps the excluded dates of a VEVENT for which keep says so.
func filterExdates(c *caldav.Component, keep func(Point) bool) error {
	var out []caldav.Prop
	for _, p := range c.Props {
		if p.Name != "EXDATE" {
			out = append(out, p)
			continue
		}
		raw := strings.Split(p.Value, ",")
		pts := parseList(p, nil, warnings{})
		if len(pts) != len(raw) {
			return ErrSeriesUnsupported
		}
		var kept []string
		for i, pt := range pts {
			if keep(pt) {
				kept = append(kept, strings.TrimSpace(raw[i]))
			}
		}
		if len(kept) > 0 {
			p.Value = strings.Join(kept, ",")
			out = append(out, p)
		}
	}
	c.Props = out
	return nil
}

// notBefore reports whether an instance key is at or after the split instance.
func notBefore(key, split string) bool { return !keyTime(key).Before(keyTime(split)) }

// truncateOld ends the old series before the split instance: UNTIL the instance
// before it, and the excluded dates and exceptions from the split on go.
func truncateOld(o *sobj, before, split string, e SeriesEdit) error {
	SetProp(o.m, caldav.Prop{Name: "RRULE", Value: before}, true)
	err := filterExdates(o.m, func(p Point) bool { return !notBefore(instanceKey(alignPoint(p, o.mev.Start)), split) })
	if err != nil {
		return err
	}
	var kept []*sexc
	for _, x := range o.exc {
		if notBefore(x.ev.RecurrenceID, split) {
			removeChild(o.root, x.c)
		} else {
			kept = append(kept, x)
		}
	}
	o.exc = kept
	bump(o.m, seqOf(o.m), e.Now)
	return nil
}

// buildNew makes the object of the series a following edit starts: a copy of
// the object whose master is the occurrence as displayed, with the later
// excluded dates and exceptions, a new uid and the edited fields.
func buildNew(o *sobj, l located, after string, e SeriesEdit) (SeriesObject, Point, error) {
	nroot := cloneComp(o.root)
	n, err := loadSeries(nroot, o.uid)
	if err != nil {
		return SeriesObject{}, Point{}, err
	}
	ch := e.Change
	nm := n.m
	if x := n.excOf(e.Instance); x != nil { // the occurrence's own values start the new master
		nm = x.c
		DropProp(nm, "RECURRENCE-ID")
		for _, p := range n.m.Props {
			if p.Name == "EXDATE" {
				nm.Props = append(nm.Props, p)
			}
		}
		removeChild(nroot, nm)
		for i, c := range nroot.Children {
			if c == n.m {
				nroot.Children[i] = nm
			}
		}
		n.exc = removeExc(n.exc, x)
	}
	n.m = nm
	var kept []*sexc
	for _, x := range n.exc {
		if notBefore(x.ev.RecurrenceID, e.Instance) && x.c != nm {
			kept = append(kept, x)
		} else {
			removeChild(nroot, x.c)
		}
	}
	n.exc = kept
	SetProp(nm, caldav.Prop{Name: "UID", Value: e.NewUID}, false)
	for _, x := range n.exc {
		SetProp(x.c, caldav.Prop{Name: "UID", Value: e.NewUID}, false)
	}
	if err := filterExdates(nm, func(p Point) bool { return notBefore(instanceKey(alignPoint(p, o.mev.Start)), e.Instance) }); err != nil {
		return SeriesObject{}, Point{}, err
	}
	start, err := shapeNew(o, n, nm, l, after, e)
	if err != nil {
		return SeriesObject{}, Point{}, err
	}
	placeExdates(nm)
	applyFields(nm, SeriesChange{Summary: ch.Summary, Location: ch.Location, Description: ch.Description, Free: ch.Free, Reminders: ch.Reminders})
	bump(nm, -1, e.Now)
	return SeriesObject{UID: e.NewUID, Root: nroot, New: true}, start, nil
}

func removeExc(list []*sexc, x *sexc) []*sexc {
	var out []*sexc
	for _, y := range list {
		if y != x {
			out = append(out, y)
		}
	}
	return out
}

// shapeNew gives the new master its times and rule: the rule that continues the
// old one (moved by what the edit moved), or the edit's own repeat.
func shapeNew(o, n *sobj, nm *caldav.Component, l located, after string, e SeriesEdit) (Point, error) {
	ch := e.Change
	start, end := l.start, l.end
	if w := ch.When; w != nil {
		if spanDur(w.Start, w.End) < 0 {
			return Point{}, ErrSpanInvalid
		}
		start, end = w.Start, w.End
	}
	dur := spanDur(start, end)
	if start.AllDay && dur < 24*time.Hour {
		dur = 24 * time.Hour
	}
	if ch.Repeat != nil {
		dropKeyed(n)
		DropProp(nm, "EXDATE")
		DropProp(nm, "RRULE")
		n.setTimes(nm, start, endFrom(start, dur))
		if err := setRepeat(n, nm, *ch.Repeat, start); err != nil {
			return Point{}, err
		}
		return start, nil
	}
	if nm.Prop("RRULE") != nil {
		SetProp(nm, caldav.Prop{Name: "RRULE", Value: after}, true)
	} else {
		insertAfter(nm, caldav.Prop{Name: "RRULE", Value: after}, "DTEND", "DURATION", "DTSTART")
	}
	sh := shifter{oldForm: o.mev.Start, newForm: start, d: wallDiff(start, l.slot), days: dayDiff(start, l.slot),
		oldDur: masterDuration(o.mev), newDur: dur, now: e.Now, zones: zonesOf(n.root)}
	return start, sh.retime(n, nm, start)
}

// applyFollowing splits the series at the occurrence: the new series first in
// the result, the old one ended before it. On the first occurrence it is all.
func applyFollowing(o *sobj, e SeriesEdit) (SeriesResult, error) {
	l, err := o.locate(e.Instance)
	if err != nil {
		return SeriesResult{}, err
	}
	if l.idx == 0 {
		return applyAll(o, e)
	}
	if e.NewUID == "" {
		return SeriesResult{}, ErrNewUID
	}
	before, after, err := SplitRule(o.mev.RRule, o.mev.Start, l.slot)
	if err != nil {
		return SeriesResult{}, err
	}
	nobj, start, err := buildNew(o, l, after, e)
	if err != nil {
		return SeriesResult{}, err
	}
	if err := truncateOld(o, before, e.Instance, e); err != nil {
		return SeriesResult{}, err
	}
	return SeriesResult{Objects: []SeriesObject{nobj, {UID: o.uid, Root: o.root}}, UID: e.NewUID, Instance: instanceKey(start), Refetch: true}, nil
}

// deleteFollowing ends the series before the occurrence; the object goes when
// nothing is left of it.
func deleteFollowing(o *sobj, e SeriesEdit) (SeriesResult, error) {
	l, err := o.locate(e.Instance)
	if err != nil {
		return SeriesResult{}, err
	}
	if l.idx == 0 {
		return SeriesResult{DeleteOrigin: true, UID: o.uid}, nil
	}
	before, _, err := SplitRule(o.mev.RRule, o.mev.Start, l.slot)
	if err != nil {
		return SeriesResult{}, err
	}
	if err := truncateOld(o, before, e.Instance, e); err != nil {
		return SeriesResult{}, err
	}
	res := SeriesResult{Objects: []SeriesObject{{UID: o.uid, Root: o.root}}, UID: o.uid}
	if again, err := loadSeries(o.root, o.uid); err == nil && again.liveCount(1) == 0 {
		res.Objects, res.DeleteOrigin = nil, true
	}
	return res, nil
}

// placeExdates puts a VEVENT's excluded dates right after its rule.
func placeExdates(c *caldav.Component) {
	var ex []caldav.Prop
	for _, p := range c.Props {
		if p.Name == "EXDATE" {
			ex = append(ex, p)
		}
	}
	if len(ex) == 0 {
		return
	}
	DropProp(c, "EXDATE")
	for i := len(ex) - 1; i >= 0; i-- {
		insertAfter(c, ex[i], "RRULE", "DTEND", "DURATION", "DTSTART")
	}
}
