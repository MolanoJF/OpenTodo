package calfeed

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/OpenTodoApp/opentodo/internal/caldav"
)

// Reminders (design D6): the editor owns the VALARMs that show a message a
// whole number of minutes before the start (or after it, for an all-day event's
// "on the day at 09:00"); every other alarm is kept and counted.

// Range of a reminder in minutes before the start (negative: after it).
const (
	MinReminder = -1439
	MaxReminder = 40320
)

// ownedAlarm reads a VALARM the form owns: ACTION DISPLAY, a TRIGGER relative to
// the start in whole minutes, no repeat.
func ownedAlarm(a *caldav.Component) (int, bool) {
	if a.Name != "VALARM" || a.Prop("REPEAT") != nil || a.Prop("DURATION") != nil || len(a.Children) > 0 {
		return 0, false
	}
	act := a.Prop("ACTION")
	trig := a.Prop("TRIGGER")
	if act == nil || trig == nil || !strings.EqualFold(strings.TrimSpace(act.Value), "DISPLAY") {
		return 0, false
	}
	if v := trig.Param("VALUE"); v != "" && !strings.EqualFold(v, "DURATION") {
		return 0, false
	}
	if rel := trig.Param("RELATED"); rel != "" && !strings.EqualFold(rel, "START") {
		return 0, false
	}
	d, _, err := parseDuration(trig.Value)
	if err != nil || d%time.Minute != 0 {
		return 0, false
	}
	m := int(-d / time.Minute)
	if m < MinReminder || m > MaxReminder {
		return 0, false
	}
	return m, true
}

// ReadReminders lists the minutes of the reminders the form owns, in document
// order, and how many other alarms the event has.
func ReadReminders(c *caldav.Component) (owned []int, other int) {
	for _, ch := range c.Children {
		if ch.Name != "VALARM" {
			continue
		}
		if m, ok := ownedAlarm(ch); ok {
			owned = append(owned, m)
		} else {
			other++
		}
	}
	return owned, other
}

// SeriesReminders reads the reminders of the occurrence of an object: its
// exception's when it has one, else the master's.
func SeriesReminders(root *caldav.Component, uid, instance string) (owned []int, other int) {
	o, err := loadSeries(root, uid)
	if err != nil {
		return nil, 0
	}
	if x := o.excOf(instance); x != nil && !x.ev.Cancelled {
		return ReadReminders(x.c)
	}
	return ReadReminders(o.m)
}

// reminderTrigger writes minutes before the start as a TRIGGER duration.
func reminderTrigger(m int) string {
	sign, abs := "-", m
	if m < 0 {
		sign, abs = "", -m
	}
	switch {
	case abs > 0 && abs%1440 == 0:
		return fmt.Sprintf("%sP%dD", sign, abs/1440)
	case abs > 0 && abs%60 == 0:
		return fmt.Sprintf("%sPT%dH", sign, abs/60)
	}
	return fmt.Sprintf("%sPT%dM", sign, abs)
}

// SetReminders replaces the reminders the form owns with one display alarm per
// value; the other alarms stay where they are.
func SetReminders(c *caldav.Component, minutes []int) {
	kept := make([]*caldav.Component, 0, len(c.Children)+len(minutes))
	for _, ch := range c.Children {
		if _, owned := ownedAlarm(ch); !(ch.Name == "VALARM" && owned) {
			kept = append(kept, ch)
		}
	}
	for _, m := range minutes {
		kept = append(kept, &caldav.Component{Name: "VALARM", Props: []caldav.Prop{
			{Name: "ACTION", Value: "DISPLAY"},
			{Name: "TRIGGER", Value: reminderTrigger(m)},
			{Name: "DESCRIPTION", Value: "Reminder"}}})
	}
	c.Children = kept
}

// sameMinutes reports whether two reminder lists hold the same values.
func sameMinutes(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	x, y := append([]int(nil), a...), append([]int(nil), b...)
	sort.Ints(x)
	sort.Ints(y)
	for i := range x {
		if x[i] != y[i] {
			return false
		}
	}
	return true
}
