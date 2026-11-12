package calfeed

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/OpenTodoApp/opentodo/internal/caldav"
)

// The series engine (OpenSpec change calendar-event-series, design D4): pure
// functions over a parsed calendar object, with no I/O and no clock (callers
// pass Now). A change reads the object, edits the tree and hands the objects to
// write back, so unknown properties, X- properties, VALARMs and VTIMEZONEs
// survive.

// Scope is how much of a series a change reaches.
type Scope string

// The three scopes of a change to a repeating event.
const (
	ScopeThis      Scope = "this"
	ScopeFollowing Scope = "following"
	ScopeAll       Scope = "all"
)

// Valid reports whether s is one of the three scopes.
func (s Scope) Valid() bool { return s == ScopeThis || s == ScopeFollowing || s == ScopeAll }

// Span is an occurrence's start and end.
type Span struct {
	Start, End Point
	AllDay     bool
}

// SeriesChange is what a scoped edit changes; a nil field stays as it is.
type SeriesChange struct {
	Summary, Location, Description *string
	Free                           *bool
	When                           *Span    // the edited occurrence's new start and end, absolute
	Repeat                         **Repeat // nil: unchanged; non-nil pointing at nil: stop repeating
	Reminders                      *[]int
}

// SeriesEdit is one scoped write against a parsed calendar object.
type SeriesEdit struct {
	Root     *caldav.Component // the VCALENDAR as read; modified in place
	UID      string
	Instance string // instanceKey of the occurrence the edit started from
	Scope    Scope
	Change   SeriesChange
	NewUID   string // the uid of the series a following edit creates (see NewSeriesUID)
	Now      time.Time
}

// SeriesObject is a calendar object an edit leaves behind.
type SeriesObject struct {
	UID  string
	Root *caldav.Component
	New  bool // a new object (a following split)
}

// SeriesResult is what an edit leaves: the objects to write (a new object
// first), whether the original object is to be deleted instead, and the edited
// occurrence's identity afterwards.
type SeriesResult struct {
	Objects      []SeriesObject
	DeleteOrigin bool
	UID          string // the edited occurrence's uid afterwards
	Instance     string // and its instance key
	// Refetch is true when other occurrences changed identity or place.
	Refetch bool
}

// Errors of the engine.
var (
	ErrSeriesUnsupported = errors.New("series not supported") // a lock reason found late
	ErrNoSuchInstance    = errors.New("no such occurrence")   // Instance is not an occurrence of the rule
	ErrShiftUnsupported  = errors.New("rule cannot move to another day")
	ErrRepeatThis        = errors.New("repeat changes need following or all")
	ErrRepeatStart       = errors.New("repeat does not include the start") // a weekly rule that skips the start's weekday
)

// Limits of an object the engine edits (design D10).
const (
	MaxSeriesEvents = 400
	MaxSeriesBytes  = 256 << 10
)

// Reasons SeriesLocked answers with.
const (
	LockSeriesUnsupported = "series_unsupported"
	LockGuests            = "has_guests"
	LockNoETag            = "no_etag"
)

// sexc is an exception: a VEVENT with a RECURRENCE-ID.
type sexc struct {
	c  *caldav.Component
	ev Event
}

// sobj is a parsed series object.
type sobj struct {
	root *caldav.Component
	uid  string
	m    *caldav.Component
	mev  Event
	exc  []*sexc
}

// loadSeries splits a calendar object into its master and exceptions; the
// error says why it can not be edited. uid "" takes the master's.
func loadSeries(root *caldav.Component, uid string) (*sobj, error) {
	o := &sobj{root: root, uid: uid}
	var defLoc *time.Location
	if p := root.Prop("X-WR-TIMEZONE"); p != nil {
		defLoc, _ = Resolve(p.Value)
	}
	w := warnings{}
	for _, c := range root.Children {
		if c.Name != "VEVENT" {
			continue
		}
		ev, err := parseEvent(c, defLoc, w)
		if err != nil || (ev.RecurrenceID == "" && c.Prop("RECURRENCE-ID") != nil) {
			return nil, ErrSeriesUnsupported
		}
		if o.uid == "" {
			o.uid = ev.UID
		}
		if ev.UID != o.uid {
			return nil, ErrSeriesUnsupported
		}
		if ev.RecurrenceID != "" {
			o.exc = append(o.exc, &sexc{c: c, ev: ev})
			continue
		}
		if o.m != nil {
			return nil, ErrSeriesUnsupported
		}
		o.m, o.mev = c, ev
	}
	if o.m == nil {
		return nil, ErrSeriesUnsupported
	}
	return o, nil
}

// recurring reports whether the object is more than one single event.
func (o *sobj) recurring() bool {
	return o.mev.RRule != "" || o.mev.RRules > 0 || len(o.mev.RDates) > 0 || len(o.mev.ExDates) > 0 || o.mev.Exrule || len(o.exc) > 0
}

// excKey finds the exception of an instance key.
func (o *sobj) excOf(key string) *sexc {
	for _, x := range o.exc {
		if x.ev.RecurrenceID == key {
			return x
		}
	}
	return nil
}

// exKeys are the instance keys the master excludes.
func (o *sobj) exKeys() map[string]bool {
	out := map[string]bool{}
	for _, ex := range o.mev.ExDates {
		out[instanceKey(alignPoint(ex, o.mev.Start))] = true
	}
	return out
}

// rule parses the master's rule.
func (o *sobj) rule() (rule, bool) {
	r, _, ok := parseRule(o.mev.RRule, o.mev.Start)
	return r, ok
}

// instances lists the rule's instances (DTSTART first) that start at or before
// to, ignoring EXDATE; ok is false when the expansion hit its work bound.
func (o *sobj) instances(to Point) ([]Point, bool) {
	r, ok := o.rule()
	if !ok {
		return nil, false
	}
	x := &expansion{ctx: context.Background(), left: maxExpandWork}
	out := []Point{o.mev.Start}
	win := Window{From: o.mev.Start.T, To: to.T}
	for _, p := range expandRule(r, o.mev.Start, win, time.Time{}, x) {
		if !p.T.After(to.T) {
			out = append(out, p)
		}
	}
	return out, !x.stopped
}

// all lists every instance of a finite rule; ok is false for an endless rule.
func (o *sobj) all() ([]Point, bool) {
	r, ok := o.rule()
	if !ok || (r.count == 0 && r.until == nil) {
		return nil, false
	}
	far := Point{T: time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC), Zone: o.mev.Start.Zone, AllDay: o.mev.Start.AllDay}
	list, complete := o.instances(far)
	return list, complete
}

func indexOfKey(list []Point, key string) int {
	for i, p := range list {
		if instanceKey(p) == key {
			return i
		}
	}
	return -1
}

// liveCount counts the occurrences the object still has, up to max.
func (o *sobj) liveCount(max int) int {
	list, ok := o.all()
	if !ok {
		return max
	}
	ex := o.exKeys()
	n := 0
	keys := map[string]bool{}
	for _, x := range o.exc {
		keys[x.ev.RecurrenceID] = true
		if !x.ev.Cancelled {
			n++
		}
	}
	for _, p := range list {
		if k := instanceKey(p); !ex[k] && !keys[k] {
			n++
		}
	}
	return min(n, max)
}

// located is the occurrence an edit started from.
type located struct {
	slot       Point // the nominal slot, in the master's form
	exc        *sexc // its exception, nil when it has none
	start, end Point // as displayed
	idx        int   // its index in the rule's instances
	list       []Point
}

// locate finds an occurrence by instance key.
func (o *sobj) locate(inst string) (located, error) {
	l := located{}
	slot, ok := pointOfKey(inst, o.mev.Start)
	if !ok {
		return l, ErrNoSuchInstance
	}
	l.slot = slot
	l.exc = o.excOf(inst)
	if l.exc != nil && l.exc.ev.Cancelled {
		return l, ErrNoSuchInstance
	}
	list, complete := o.instances(slot)
	if !complete {
		return l, ErrSeriesUnsupported
	}
	l.list, l.idx = list, indexOfKey(list, inst)
	if l.idx < 0 || (l.exc == nil && o.exKeys()[inst]) {
		return l, ErrNoSuchInstance
	}
	if l.exc != nil {
		oc := occurrence(l.exc.ev, l.exc.ev.Start)
		l.start, l.end = oc.Start, oc.End
	} else {
		oc := occurrence(o.mev, slot)
		l.start, l.end = oc.Start, oc.End
	}
	return l, nil
}

// SeriesRead is what the editor and the grid learn about an object.
type SeriesRead struct {
	UID       string
	Recurring bool // the object repeats (a rule, an excluded date or an exception)
	RRule     string
	Repeat    *Repeat // nil when the rule is custom
	Custom    bool
	First     string // instance key of the series' DTSTART
	// Exceptions counts the exceptions and the excluded dates.
	Exceptions int
}

// ReadSeries reads the repeat of a calendar object; an error when its master
// can not be found.
func ReadSeries(root *caldav.Component) (SeriesRead, error) {
	o, err := loadSeries(root, "")
	if err != nil {
		return SeriesRead{}, err
	}
	v := SeriesRead{UID: o.uid, Recurring: o.recurring(), RRule: o.mev.RRule, First: instanceKey(o.mev.Start)}
	if o.mev.RRule != "" {
		if r, ok := RepeatOf(o.mev.RRule, o.mev.Start); ok {
			v.Repeat = &r
		} else {
			v.Custom = true
		}
	} else if v.Recurring {
		v.Custom = true
	}
	v.Exceptions = len(o.exc) + len(o.mev.ExDates)
	return v, nil
}

// SeriesLocked is why an object can not be edited as a series, "" when it can:
// series_unsupported (design D10), then has_guests, then no_etag. A single event
// has no series reason.
func SeriesLocked(root *caldav.Component, etag string) string {
	switch why := unsupportedWhy(root); {
	case why != "":
		return LockSeriesUnsupported
	case hasGuests(root):
		return LockGuests
	case etag == "":
		return LockNoETag
	}
	return ""
}

func hasGuests(root *caldav.Component) bool {
	for _, c := range root.Children {
		if c.Name == "VEVENT" && c.Prop("ATTENDEE") != nil {
			return true
		}
	}
	return false
}

// unsupportedWhy names the first series_unsupported condition of design D10
// the object meets, "" for none.
func unsupportedWhy(root *caldav.Component) string {
	n := 0
	for _, c := range root.Children {
		if c.Name == "VEVENT" {
			n++
		}
	}
	if n == 0 {
		return "no event"
	}
	if n > MaxSeriesEvents {
		return "too many events"
	}
	o, err := loadSeries(root, "")
	if err != nil {
		return "masters or uids"
	}
	if !o.recurring() {
		return ""
	}
	if why := o.recurringWhy(root); why != "" {
		return why
	}
	if len(root.Encode()) > MaxSeriesBytes {
		return "too big"
	}
	if !RoundTrips(root) {
		return "not lossless"
	}
	return ""
}

// recurringWhy checks a repeating object against the envelope.
func (o *sobj) recurringWhy(root *caldav.Component) string {
	all := append([]*caldav.Component{o.m}, func() []*caldav.Component {
		var cs []*caldav.Component
		for _, x := range o.exc {
			cs = append(cs, x.c)
		}
		return cs
	}()...)
	for _, c := range all {
		for _, p := range c.Props {
			switch p.Name {
			case "RDATE", "EXRULE":
				return p.Name
			case "DTSTART", "DTEND", "RECURRENCE-ID", "EXDATE":
				if tz := p.Param("TZID"); tz != "" {
					if _, ok := Resolve(tz); !ok {
						return "unknown time zone"
					}
				}
			}
		}
	}
	for _, x := range o.exc {
		if x.ev.Range != "" || x.ev.RRule != "" {
			return "exception range or rule"
		}
	}
	if o.mev.RRules != 1 || o.mev.RRule == "" {
		return "rules"
	}
	if _, ok := o.rule(); !ok {
		return "rule"
	}
	if root.Prop("X-WR-TIMEZONE") != nil && kindOfPoint(o.mev.Start) == kindFloating {
		// With a default zone a floating time reads as zoned, and writing it back
		// would change its form.
		return "default zone"
	}
	if len(o.exc) > 0 {
		return o.orphans()
	}
	return ""
}

// orphans says why an exception is not one of the rule's instances: such an
// exception is ignored by readers and would be rewritten blindly.
func (o *sobj) orphans() string {
	last := o.exc[0].ev.RecurrenceID
	for _, x := range o.exc {
		if keyTime(x.ev.RecurrenceID).After(keyTime(last)) {
			last = x.ev.RecurrenceID
		}
	}
	through, ok := pointOfKey(last, o.mev.Start)
	if !ok {
		return "exception form"
	}
	list, complete := o.instances(through)
	if !complete {
		return "rule too long"
	}
	for _, x := range o.exc {
		if indexOfKey(list, x.ev.RecurrenceID) < 0 {
			return "exception without instance"
		}
	}
	return ""
}

// NewSeriesUID is the uid of the series a following edit creates: deterministic,
// so a retry of the same split after a half failure writes the same object.
func NewSeriesUID(uid, instance, etag string) string {
	sum := sha256.Sum256([]byte(uid + "|" + instance + "|" + etag))
	return "ot-" + hex.EncodeToString(sum[:])[:20] + "@opentodo"
}

// SeriesID is the id the events route groups an object's occurrences by.
func SeriesID(subscriptionID, seriesKey string) string {
	sum := sha256.Sum256([]byte(subscriptionID + "\x00" + seriesKey))
	return hex.EncodeToString(sum[:8])
}

// masterDuration is the length of the master's occurrences.
func masterDuration(ev Event) time.Duration {
	oc := occurrence(ev, ev.Start)
	return oc.End.T.Sub(oc.Start.T)
}

// Info is the series row of an object for the cache, nil for a single event.
func (v SeriesRead) Info() *SeriesInfo {
	if !v.Recurring {
		return nil
	}
	info := &SeriesInfo{RRule: v.RRule, First: v.First}
	if v.Repeat != nil {
		if b, err := json.Marshal(v.Repeat); err == nil {
			info.Repeat = string(b)
		}
	}
	return info
}

// OccurrenceVEvent is the VEVENT that shows an occurrence: its exception when it
// has one, else the master; nil when the object has no master.
func OccurrenceVEvent(root *caldav.Component, instance string) *caldav.Component {
	o, err := loadSeries(root, "")
	if err != nil {
		return nil
	}
	if x := o.excOf(instance); x != nil && !x.ev.Cancelled {
		return x.c
	}
	return o.m
}
