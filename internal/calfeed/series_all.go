package calfeed

import (
	"strings"
	"time"

	"github.com/OpenTodoApp/opentodo/internal/caldav"
)

// shifter moves the instants a series keeps (the rule's UNTIL, excluded dates and
// the exceptions' RECURRENCE-IDs) by the same wall-clock difference in the
// master's zone, so each stays attached to the same logical day (design D6).
type shifter struct {
	oldForm, newForm Point
	d                time.Duration // wall-clock shift of every instant
	days             int           // calendar days the rule moves by
	oldDur, newDur   time.Duration
	now              time.Time
	zones            map[string]string
}

// point moves a point read from a property: the master-form point of the same
// instance, shifted, in the new form.
func (s shifter) point(p Point) Point {
	key := instanceKey(alignPoint(p, s.oldForm))
	old, ok := pointOfKey(key, s.oldForm)
	if !ok {
		old = p
	}
	return fromCivil(civil(old).Add(s.d), s.newForm)
}

// until moves the UNTIL of a rule: an exact instant moves with the instances, a
// date by the days.
func (s shifter) until(val string) string {
	p, err := parseValue(val, "", "", nil, warnings{})
	if err != nil {
		return val
	}
	switch {
	case p.AllDay:
		d := fromCivil(civil(p).AddDate(0, 0, s.days), s.newForm)
		if s.newForm.AllDay {
			return d.T.Format("20060102")
		}
		return endOfDay(d.T, s.newForm)
	case s.newForm.AllDay:
		inOld := p
		if p.Zone == "UTC" && kindOfPoint(s.oldForm) == kindZoned {
			inOld = Point{T: p.T, Zone: s.oldForm.Zone}
		}
		return fromCivil(civil(inOld).Add(s.d), s.newForm).T.Format("20060102")
	}
	inOld := p
	if p.Zone == "UTC" && kindOfPoint(s.oldForm) == kindZoned {
		inOld = Point{T: p.T, Zone: s.oldForm.Zone}
	}
	moved := fromCivil(civil(inOld).Add(s.d), s.newForm)
	return untilValue(moved)
}

// rule rewrites a rule's text for the shift: the days of the week rotate, the
// UNTIL moves. ErrShiftUnsupported when the instances would not move as a whole.
func (s shifter) rule(m *sobj, rrule string) (string, error) {
	r, _, ok := parseRule(rrule, m.mev.Start)
	if !ok {
		return "", ErrSeriesUnsupported
	}
	if !canRotate(r, m.mev.Start, s.days) {
		return "", ErrShiftUnsupported
	}
	parts := splitParts(rrule)
	rotateDays(parts, s.days)
	for i, p := range parts {
		if p[0] == "UNTIL" {
			parts[i][1] = s.until(p[1])
		}
	}
	return joinParts(parts), nil
}

// exdates re-keys the excluded dates of a VEVENT, value by value; the values of
// one property stay together while they are written alike.
func (s shifter) exdates(c *caldav.Component) error {
	var out []caldav.Prop
	for _, p := range c.Props {
		if p.Name != "EXDATE" {
			out = append(out, p)
			continue
		}
		pts := parseList(p, nil, warnings{})
		if len(pts) != len(strings.Split(p.Value, ",")) {
			return ErrSeriesUnsupported
		}
		var cur *caldav.Prop
		var vals []string
		flush := func() {
			if cur != nil {
				cur.Value = strings.Join(vals, ",")
				out = append(out, *cur)
				cur, vals = nil, nil
			}
		}
		for _, pt := range pts {
			np := rewriteProp(p, s.point(pt), s.zones)
			if cur != nil && np.Line()[:len(np.Line())-len(np.Value)] != cur.Line()[:len(cur.Line())-len(cur.Value)] {
				flush()
			}
			if cur == nil {
				cur = &np
			}
			vals = append(vals, np.Value)
		}
		flush()
	}
	c.Props = out
	return nil
}

// exception re-keys one exception: its RECURRENCE-ID always, its own times only
// when it sat at its slot (one moved by hand keeps its absolute time).
func (s shifter) exception(o *sobj, x *sexc) {
	key, ok := pointOfKey(x.ev.RecurrenceID, s.oldForm)
	if !ok {
		return
	}
	oc := occurrence(x.ev, x.ev.Start)
	atSlot := !x.ev.Cancelled && instanceKey(x.ev.Start) == x.ev.RecurrenceID && spanDur(oc.Start, oc.End) == s.oldDur
	if s.still() && (!atSlot || s.newDur == s.oldDur) {
		return // nothing about it changes
	}
	np := fromCivil(civil(key).Add(s.d), s.newForm)
	if rid := x.c.Prop("RECURRENCE-ID"); rid != nil {
		SetProp(x.c, rewriteProp(*rid, np, s.zones), false)
	}
	if atSlot {
		o.setTimes(x.c, np, endFrom(np, s.newDur))
	}
	bump(x.c, seqOf(x.c), s.now)
}

// still reports a shift that moves no instant: only a length may change.
func (s shifter) still() bool { return s.d == 0 && sameForm(s.oldForm, s.newForm) }

// retime moves a series: its master's DTSTART and DTEND, its rule and everything
// keyed to the old instants. The master is the VEVENT c of o.
func (s shifter) retime(o *sobj, c *caldav.Component, newStart Point) error {
	rr := c.Prop("RRULE")
	if rr != nil && !s.still() {
		text, err := s.rule(o, rr.Value)
		if err != nil {
			return err
		}
		SetProp(c, caldav.Prop{Name: "RRULE", Value: text}, true)
	}
	if !s.still() {
		if err := s.exdates(c); err != nil {
			return err
		}
	}
	for _, x := range o.exc {
		s.exception(o, x)
	}
	o.setTimes(c, newStart, endFrom(newStart, s.newDur))
	return nil
}

// dropKeyed removes the excluded dates and the exceptions a new rule voids.
func dropKeyed(o *sobj) {
	DropProp(o.m, "EXDATE")
	for _, x := range o.exc {
		removeChild(o.root, x.c)
	}
	o.exc = nil
}

// setRepeat applies a repeat change to the master VEVENT of o: a new rule (from
// the master's start) or none.
func setRepeat(o *sobj, c *caldav.Component, rep *Repeat, start Point) error {
	if rep == nil {
		DropProp(c, "RRULE")
		return nil
	}
	text, err := rep.RRule(start)
	if err != nil {
		return err
	}
	if c.Prop("RRULE") != nil {
		SetProp(c, caldav.Prop{Name: "RRULE", Value: text}, true)
		return nil
	}
	insertAfter(c, caldav.Prop{Name: "RRULE", Value: text}, "DTEND", "DURATION", "DTSTART")
	return nil
}

// carry sets a changed member on the exceptions that still hold the master's
// previous value: renaming "Study" renames Wednesday's moved session but not one
// that was renamed alone (design D6).
func carry(o *sobj, old fields, ch SeriesChange, now time.Time) {
	for _, x := range o.exc {
		if x.ev.Cancelled {
			continue
		}
		f := fieldsOf(x.c)
		var c2 SeriesChange
		if ch.Summary != nil && f.summary == old.summary {
			c2.Summary = ch.Summary
		}
		if ch.Location != nil && f.location == old.location {
			c2.Location = ch.Location
		}
		if ch.Description != nil && f.description == old.description {
			c2.Description = ch.Description
		}
		if ch.Free != nil && f.free == old.free {
			c2.Free = ch.Free
		}
		if ch.Reminders != nil && sameMinutes(f.reminders, old.reminders) {
			c2.Reminders = ch.Reminders
		}
		if c2 != (SeriesChange{}) {
			applyFields(x.c, c2)
			bump(x.c, seqOf(x.c), now)
		}
	}
}

// applyAll changes every occurrence of the series.
func applyAll(o *sobj, e SeriesEdit) (SeriesResult, error) {
	l, err := o.locate(e.Instance)
	if err != nil {
		return SeriesResult{}, err
	}
	ch := e.Change
	res := SeriesResult{Objects: []SeriesObject{{UID: o.uid, Root: o.root}}, UID: o.uid, Instance: e.Instance, Refetch: true}
	carry(o, fieldsOf(o.m), ch, e.Now)
	applyFields(o.m, SeriesChange{Summary: ch.Summary, Location: ch.Location, Description: ch.Description, Free: ch.Free, Reminders: ch.Reminders})
	var sh shifter
	moved := false
	if w := ch.When; w != nil {
		if spanDur(w.Start, w.End) < 0 {
			return SeriesResult{}, ErrSpanInvalid
		}
		sh = shiftFor(o, l, w, e.Now)
		moved = sh.d != 0 || !sameForm(w.Start, o.mev.Start) || sh.newDur != sh.oldDur
	}
	if ch.Repeat != nil {
		return finishRepeat(o, e, res, sh, moved, ch.When != nil)
	}
	if moved {
		newStart := fromCivil(civil(o.mev.Start).Add(sh.d), sh.newForm)
		if err := sh.retime(o, o.m, newStart); err != nil {
			return SeriesResult{}, err
		}
		if k, ok := pointOfKey(e.Instance, sh.oldForm); ok {
			res.Instance = instanceKey(fromCivil(civil(k).Add(sh.d), sh.newForm))
		}
	}
	bump(o.m, seqOf(o.m), e.Now)
	return res, nil
}

// shiftFor reads the deltas of an edit of the occurrence l: wall-clock and
// relative to the occurrence as displayed, so dragging a hand-moved exception
// shifts the series by what the person moved.
func shiftFor(o *sobj, l located, w *Span, now time.Time) shifter {
	oldDur := masterDuration(o.mev)
	sh := shifter{oldForm: o.mev.Start, newForm: w.Start, d: wallDiff(w.Start, l.start), days: dayDiff(w.Start, l.start),
		oldDur: oldDur, now: now, zones: zonesOf(o.root)}
	sh.newDur = oldDur + (spanDur(w.Start, w.End) - spanDur(l.start, l.end))
	if kindOfPoint(w.Start) != kindOfPoint(o.mev.Start) && (w.Start.AllDay || o.mev.Start.AllDay) {
		sh.newDur = spanDur(w.Start, w.End)
	}
	if w.Start.AllDay && sh.newDur < 24*time.Hour {
		sh.newDur = 24 * time.Hour
	}
	return sh
}

// finishRepeat applies an edit that carries a repeat to the master: the new
// rule replaces the old one and every excluded date and exception goes (their
// identities belong to the old rule).
func finishRepeat(o *sobj, e SeriesEdit, res SeriesResult, sh shifter, moved, hasTime bool) (SeriesResult, error) {
	start := o.mev.Start
	if hasTime && moved {
		start = fromCivil(civil(o.mev.Start).Add(sh.d), sh.newForm)
		o.setTimes(o.m, start, endFrom(start, sh.newDur))
	}
	dropKeyed(o)
	if err := setRepeat(o, o.m, *e.Change.Repeat, start); err != nil {
		return SeriesResult{}, err
	}
	bump(o.m, seqOf(o.m), e.Now)
	if k, ok := pointOfKey(e.Instance, o.mev.Start); ok && hasTime && moved {
		res.Instance = instanceKey(fromCivil(civil(k).Add(sh.d), sh.newForm))
	}
	return res, nil
}
