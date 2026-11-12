package calfeed

import (
	"strconv"
	"strings"
	"time"

	"github.com/OpenTodoApp/opentodo/internal/caldav"
)

// Wall-clock arithmetic and property writing of the series engine (OpenSpec
// change calendar-event-series, design D4). A series is defined by the master's
// wall time in its zone, so every shift is done on the wall clock and resolved
// in the zone afterwards; a point is re-written in the form it already had.

type timeKind int

const (
	kindDate timeKind = iota
	kindFloating
	kindUTC
	kindZoned
)

func kindOfPoint(p Point) timeKind {
	switch {
	case p.AllDay:
		return kindDate
	case p.Zone == "":
		return kindFloating
	case p.Zone == "UTC":
		return kindUTC
	}
	return kindZoned
}

// sameForm reports whether two points are written alike: the same kind and,
// for a zoned point, the same zone.
func sameForm(a, b Point) bool {
	ka, kb := kindOfPoint(a), kindOfPoint(b)
	return ka == kb && (ka != kindZoned || a.Zone == b.Zone)
}

// inZone is the point's time in its own zone.
func inZone(p Point) time.Time {
	switch kindOfPoint(p) {
	case kindZoned:
		if loc, ok := Resolve(p.Zone); ok {
			return p.T.In(loc)
		}
	case kindUTC:
		return p.T.UTC()
	}
	return p.T
}

// civil is the wall clock of a point read as a UTC time: differences of civil
// times are wall-clock differences.
func civil(p Point) time.Time {
	t := inZone(p)
	y, m, d := t.Date()
	h, mi, s := t.Clock()
	return time.Date(y, m, d, h, mi, s, 0, time.UTC)
}

// fromCivil reads a civil time in the form of form (all-day keeps the date).
func fromCivil(c time.Time, form Point) Point {
	y, m, d := c.Date()
	h, mi, s := c.Clock()
	switch kindOfPoint(form) {
	case kindDate:
		return Point{T: time.Date(y, m, d, 0, 0, 0, 0, time.UTC), AllDay: true}
	case kindFloating:
		return Point{T: time.Date(y, m, d, h, mi, s, 0, time.UTC)}
	case kindUTC:
		return Point{T: time.Date(y, m, d, h, mi, s, 0, time.UTC), Zone: "UTC"}
	}
	loc, ok := Resolve(form.Zone)
	if !ok {
		loc = time.UTC
	}
	return Point{T: time.Date(y, m, d, h, mi, s, 0, loc), Zone: form.Zone}
}

// addWall moves a point by a wall-clock duration in its own zone (an all-day
// point moves by whole days).
func addWall(p Point, d time.Duration) Point {
	if p.AllDay {
		return fromCivil(civil(p).AddDate(0, 0, int(d/(24*time.Hour))), p)
	}
	return fromCivil(civil(p).Add(d), p)
}

// wallDiff is a minus b on the wall clock.
func wallDiff(a, b Point) time.Duration { return civil(a).Sub(civil(b)) }

// dayDiff is the number of calendar days from b's date to a's.
func dayDiff(a, b Point) int {
	ca, cb := civil(a), civil(b)
	da := time.Date(ca.Year(), ca.Month(), ca.Day(), 0, 0, 0, 0, time.UTC)
	db := time.Date(cb.Year(), cb.Month(), cb.Day(), 0, 0, 0, 0, time.UTC)
	return int(da.Sub(db).Hours() / 24)
}

// pointOfKey reads an instance key back as a point in the form of form.
func pointOfKey(key string, form Point) (Point, bool) {
	switch {
	case len(key) == 8 && form.AllDay:
		t, err := time.Parse("20060102", key)
		return Point{T: t, AllDay: true}, err == nil
	case len(key) == 15 && form.Zone == "" && !form.AllDay:
		t, err := time.Parse("20060102T150405", key)
		return Point{T: t}, err == nil
	case len(key) == 16 && strings.HasSuffix(key, "Z") && form.Zone != "" && !form.AllDay:
		t, err := time.Parse("20060102T150405Z", key)
		if err != nil {
			return Point{}, false
		}
		return Point{T: inZone(Point{T: t.UTC(), Zone: form.Zone}), Zone: form.Zone}, true
	}
	return Point{}, false
}

// keyTime orders instance keys of one series: the key read as a time, wall
// times as if UTC.
func keyTime(key string) time.Time {
	for _, layout := range []string{"20060102T150405Z", "20060102T150405", "20060102"} {
		if t, err := time.Parse(layout, key); err == nil {
			return t
		}
	}
	return time.Time{}
}

// formatValue writes a point as an iCalendar value.
func formatValue(p Point) string {
	switch kindOfPoint(p) {
	case kindDate:
		return p.T.Format("20060102")
	case kindUTC:
		return p.T.UTC().Format("20060102T150405Z")
	}
	return inZone(p).Format("20060102T150405")
}

// withoutParams copies params without the named ones.
func withoutParams(params []caldav.Param, names ...string) []caldav.Param {
	var out []caldav.Param
next:
	for _, p := range params {
		for _, n := range names {
			if p.Name == n {
				continue next
			}
		}
		out = append(out, caldav.Param{Name: p.Name, Values: append([]string(nil), p.Values...)})
	}
	return out
}

// kindOfProp is how a DTSTART-like property is written.
func kindOfProp(p caldav.Prop) (timeKind, string) {
	switch {
	case strings.EqualFold(p.Param("VALUE"), "DATE") || len(strings.TrimSpace(p.Value)) == 8:
		return kindDate, ""
	case strings.HasSuffix(strings.TrimSpace(p.Value), "Z"):
		return kindUTC, ""
	case p.Param("TZID") != "":
		if loc, ok := Resolve(p.Param("TZID")); ok {
			return kindZoned, zoneName(loc)
		}
		return kindZoned, ""
	}
	return kindFloating, ""
}

// rewriteProp writes point p in the property tmpl: the same name and
// parameters, in the form p is in. A property that was UTC stays UTC for a
// zoned point (a Google exception keeps its RECURRENCE-ID in UTC), and a zoned
// property keeps the TZID text it used when p is in that zone.
func rewriteProp(tmpl caldav.Prop, p Point, zones map[string]string) caldav.Prop {
	tk, tzone := kindOfProp(tmpl)
	out := caldav.Prop{Name: tmpl.Name, Value: formatValue(p)}
	switch kindOfPoint(p) {
	case kindDate:
		out.Params = append(withoutParams(tmpl.Params, "TZID", "VALUE"), caldav.Param{Name: "VALUE", Values: []string{"DATE"}})
	case kindFloating:
		out.Params = withoutParams(tmpl.Params, "TZID", "VALUE")
	case kindUTC:
		out.Params = withoutParams(tmpl.Params, "TZID", "VALUE")
	default:
		if tk == kindUTC {
			out.Params = withoutParams(tmpl.Params, "TZID", "VALUE")
			out.Value = p.T.UTC().Format("20060102T150405Z")
			break
		}
		tzid := p.Zone
		if tk == kindZoned && tzone == p.Zone {
			tzid = tmpl.Param("TZID")
		} else if raw, ok := zones[p.Zone]; ok {
			tzid = raw
		}
		out.Params = append(withoutParams(tmpl.Params, "TZID", "VALUE"), caldav.Param{Name: "TZID", Values: []string{tzid}})
	}
	return out
}

// zonesOf maps each IANA zone the object's events name to the TZID text used.
func zonesOf(root *caldav.Component) map[string]string {
	zones := map[string]string{}
	for _, c := range root.Children {
		if c.Name != "VEVENT" {
			continue
		}
		for _, p := range c.Props {
			if tz := p.Param("TZID"); tz != "" {
				if loc, ok := Resolve(tz); ok {
					if _, seen := zones[loc.String()]; !seen {
						zones[loc.String()] = tz
					}
				}
			}
		}
	}
	return zones
}

// PointProp writes a point as DTSTART or DTEND: a date for all-day, UTC with
// Z, a wall time with TZID for a zoned point (the TZID text the event already
// used for that zone, if any) and a bare wall time for a floating one.
func PointProp(name string, p Point, zones map[string]string) caldav.Prop {
	return rewriteProp(caldav.Prop{Name: name}, p, zones)
}

// AddZone adds a VTIMEZONE for the TZID of a written point when the object has
// none for it.
func AddZone(root *caldav.Component, p caldav.Prop) {
	tz := p.Param("TZID")
	if tz == "" {
		return
	}
	for _, c := range root.Children {
		if c.Name == "VTIMEZONE" {
			if id := c.Prop("TZID"); id != nil && id.Value == tz {
				return
			}
		}
	}
	loc, ok := Resolve(tz)
	if !ok || loc.String() != tz {
		return // a vendor TZID without a definition of ours stays as it was written
	}
	date, clock := p.Value[:4]+"-"+p.Value[4:6]+"-"+p.Value[6:8], p.Value[9:11]+":"+p.Value[11:13]
	if vt := caldav.VTimezone(tz, date, clock); vt != nil {
		root.Children = append([]*caldav.Component{vt}, root.Children...)
	}
}

// ---- properties ----

// SetProp replaces the first property of that name, keeping its parameters
// when keepParams, or appends it.
func SetProp(c *caldav.Component, p caldav.Prop, keepParams bool) {
	for i := range c.Props {
		if c.Props[i].Name == p.Name {
			if keepParams {
				p.Params = c.Props[i].Params
			}
			c.Props[i] = p
			return
		}
	}
	c.Props = append(c.Props, p)
}

// DropProp removes every property of that name.
func DropProp(c *caldav.Component, name string) {
	out := make([]caldav.Prop, 0, len(c.Props))
	for _, p := range c.Props {
		if p.Name != name {
			out = append(out, p)
		}
	}
	c.Props = out
}

// SetOrDelete sets a text property, keeping its parameters, or removes it for "".
func SetOrDelete(c *caldav.Component, name, text string) {
	if text == "" {
		DropProp(c, name)
		return
	}
	SetProp(c, caldav.Prop{Name: name, Value: caldav.EscapeText(text)}, true)
}

// ReplaceTimes puts DTSTART and DTEND where DTSTART was and drops DTEND and
// DURATION (a DURATION becomes a DTEND).
func ReplaceTimes(c *caldav.Component, start, end caldav.Prop) {
	out := make([]caldav.Prop, 0, len(c.Props)+1)
	placed := false
	for _, p := range c.Props {
		switch p.Name {
		case "DTSTART", "DTEND", "DURATION":
			if !placed {
				out = append(out, start, end)
				placed = true
			}
		default:
			out = append(out, p)
		}
	}
	if !placed {
		out = append(out, start, end)
	}
	c.Props = out
}

// insertAfter puts p after the last property named one of names, or at the end.
func insertAfter(c *caldav.Component, p caldav.Prop, names ...string) {
	at := -1
	for i, q := range c.Props {
		for _, n := range names {
			if q.Name == n {
				at = i
			}
		}
	}
	if at < 0 {
		c.Props = append(c.Props, p)
		return
	}
	c.Props = append(c.Props[:at+1], append([]caldav.Prop{p}, c.Props[at+1:]...)...)
}

// bump records a change of a VEVENT: SEQUENCE one higher than seq, DTSTAMP now,
// LAST-MODIFIED when the VEVENT has one (a server sets its own otherwise).
func bump(c *caldav.Component, seq int, now time.Time) {
	stamp := now.UTC().Format("20060102T150405Z")
	SetProp(c, caldav.Prop{Name: "SEQUENCE", Value: strconv.Itoa(seq + 1)}, false)
	SetProp(c, caldav.Prop{Name: "DTSTAMP", Value: stamp}, false)
	if c.Prop("LAST-MODIFIED") != nil {
		SetProp(c, caldav.Prop{Name: "LAST-MODIFIED", Value: stamp}, false)
	}
}

func seqOf(c *caldav.Component) int {
	if p := c.Prop("SEQUENCE"); p != nil {
		n, _ := strconv.Atoi(strings.TrimSpace(p.Value))
		return n
	}
	return 0
}

// cloneComp deep-copies a component.
func cloneComp(c *caldav.Component) *caldav.Component {
	n := &caldav.Component{Name: c.Name, Props: cloneProps(c.Props)}
	for _, ch := range c.Children {
		n.Children = append(n.Children, cloneComp(ch))
	}
	return n
}

func cloneProps(props []caldav.Prop) []caldav.Prop {
	out := make([]caldav.Prop, len(props))
	for i, p := range props {
		out[i] = caldav.Prop{Name: p.Name, Value: p.Value, Params: withoutParams(p.Params)}
	}
	return out
}

// sameTree reports whether two component trees hold the same content lines.
func sameTree(a, b *caldav.Component) bool {
	if a.Name != b.Name || len(a.Props) != len(b.Props) || len(a.Children) != len(b.Children) {
		return false
	}
	for i := range a.Props {
		if a.Props[i].Line() != b.Props[i].Line() {
			return false
		}
	}
	for i := range a.Children {
		if !sameTree(a.Children[i], b.Children[i]) {
			return false
		}
	}
	return true
}

// RoundTrips reports whether a parsed object survives being written and read
// again unchanged: the engine never writes an object it could not read back
// losslessly.
func RoundTrips(root *caldav.Component) bool {
	back, err := caldav.Parse(root.Encode())
	return err == nil && sameTree(root, back)
}
