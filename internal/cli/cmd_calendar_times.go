package cli

import (
	"strings"
	"time"
)

// The new times of `ot calendar event edit` (OpenSpec change
// calendar-event-edits, design D8). A date alone keeps the event's clock time,
// a time alone is not accepted, a new start alone keeps the event's length, and
// a timed event keeps its own time zone (a floating one stays floating).

// parseWhenFlag splits DATE[Thh:mm] ("T" or a space) into a date and a clock.
func (a *App) parseWhenFlag(flagName, v string) (date, clock string, err error) {
	if v == "" {
		return "", "", nil
	}
	day, clk, _ := strings.Cut(strings.Replace(strings.TrimSpace(v), " ", "T", 1), "T")
	if date, err = resolveDate(day, a.Now()); err != nil {
		return "", "", usageErr("--%s: %q is not DATE or DATEThh:mm (a date is YYYY-MM-DD, today, tomorrow or a weekday)", flagName, v)
	}
	if clk != "" {
		if err = validClock(clk); err != nil {
			return "", "", usageErr("--%s: %q is not DATEThh:mm", flagName, v)
		}
	}
	return date, clk, nil
}

// wall is a date and clock as a wall-clock time (zone ignored).
func wall(date, clock string) time.Time {
	if clock == "" {
		clock = "00:00"
	}
	t, _ := time.Parse("2006-01-02 15:04", date+" "+clock)
	return t
}

func clockOf(w calWhen) string {
	if w.Time == nil {
		return ""
	}
	return *w.Time
}

func pointOf(t time.Time, tz *string, allDay bool) map[string]any {
	if allDay {
		return map[string]any{"date": t.Format(time.DateOnly)}
	}
	p := map[string]any{"date": t.Format(time.DateOnly), "time": t.Format("15:04")}
	if tz != nil {
		p["tz"] = *tz
	}
	return p
}

// eventTimes builds the start, end and allDay of the PATCH body.
func (a *App) eventTimes(ev *calEvent, startF, endF string, allDayF bool) (map[string]any, map[string]any, bool, error) {
	sd, sc, err := a.parseWhenFlag("start", startF)
	if err != nil {
		return nil, nil, false, err
	}
	ed, ec, err := a.parseWhenFlag("end", endF)
	if err != nil {
		return nil, nil, false, err
	}
	timedGiven := sc != "" || ec != ""
	if allDayF && timedGiven {
		return nil, nil, false, usageErr("--all-day takes dates only: drop the times from --start and --end")
	}
	allDay := ev.AllDay
	if allDayF {
		allDay = true
	} else if timedGiven {
		allDay = false
	}
	evStart, evEnd := wall(ev.Start.Date, clockOf(ev.Start)), wall(ev.End.Date, clockOf(ev.End))
	var s, e time.Time
	if allDay {
		s, e, err = allDayRange(ev, evStart, evEnd, sd, ed)
	} else {
		s, e, err = timedRange(ev, evStart, evEnd, sd, sc, ed, ec)
	}
	if err != nil {
		return nil, nil, false, err
	}
	if !e.After(s) {
		return nil, nil, false, usageErr("the end must be after the start")
	}
	if allDay {
		return pointOf(s, nil, true), pointOf(e, nil, true), true, nil
	}
	return pointOf(s, ev.Start.TZ, false), pointOf(e, ev.End.TZ, false), false, nil
}

func allDayRange(ev *calEvent, evStart, evEnd time.Time, sd, ed string) (time.Time, time.Time, error) {
	s := evStart.Truncate(24 * time.Hour)
	if sd != "" {
		s = wall(sd, "")
	}
	switch {
	case ed != "":
		return s, wall(ed, ""), nil
	case ev.AllDay:
		return s, s.Add(evEnd.Sub(evStart)), nil
	}
	return s, s.AddDate(0, 0, 1), nil
}

func timedRange(ev *calEvent, evStart, evEnd time.Time, sd, sc, ed, ec string) (time.Time, time.Time, error) {
	if ev.AllDay {
		// An all-day event becomes a timed one: both ends need a time.
		if sc == "" || ec == "" {
			return time.Time{}, time.Time{}, usageErr("to make an all-day event timed, give --start and --end with times (DATEThh:mm)")
		}
		if sd == "" {
			sd = ev.Start.Date
		}
		if ed == "" {
			ed = sd
		}
		return wall(sd, sc), wall(ed, ec), nil
	}
	s := replaceWall(evStart, sd, sc)
	if ed == "" && ec == "" {
		// Only the start moves: the event keeps its length.
		return s, s.Add(evEnd.Sub(evStart)), nil
	}
	return s, replaceWall(evEnd, ed, ec), nil
}

// replaceWall changes the date and/or clock of t.
func replaceWall(t time.Time, date, clock string) time.Time {
	d, c := t.Format(time.DateOnly), t.Format("15:04")
	if date != "" {
		d = date
	}
	if clock != "" {
		c = clock
	}
	return wall(d, c)
}
