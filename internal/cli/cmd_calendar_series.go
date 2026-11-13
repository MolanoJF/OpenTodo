package cli

import (
	"flag"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// The scope, repeat and reminder flags of `ot calendar event add|edit|delete`
// (OpenSpec change calendar-event-series, design D13). --on is taken by the
// commands (the day to look for the event around), so the weekdays of a weekly
// repeat are --days. A repeat given to edit is the whole rule, as the REST
// route takes it.

const scopeList = "this, following or all"

// calSeries is what an event says about its series.
type calSeries struct {
	ID     string `json:"id"`
	Custom bool   `json:"custom"`
	First  bool   `json:"first"`
	Repeat *struct {
		Freq     string   `json:"freq"`
		Interval int      `json:"interval"`
		Days     []string `json:"days"`
	} `json:"repeat"`
}

// repeatText is the REPEATS cell: empty for a single event.
func (s *calSeries) repeatText() string {
	switch {
	case s == nil:
		return ""
	case s.Custom || s.Repeat == nil:
		return "custom"
	case s.Repeat.Interval > 1:
		unit := map[string]string{"daily": "days", "weekly": "weeks", "monthly": "months", "yearly": "years"}[s.Repeat.Freq]
		return fmt.Sprintf("every %d %s", s.Repeat.Interval, unit)
	}
	return s.Repeat.Freq
}

// seriesFlags are the flags read by add, edit and delete.
type seriesFlags struct {
	scope, repeat, days, by, until string
	every, count                   int
	noRepeat, noRemind             bool
	remind                         stringList
}

// bind registers the flags: scope only for edit and delete, --no-repeat only
// for edit.
func (f *seriesFlags) bind(fs *flag.FlagSet, scope, repeats, noRepeat bool) {
	if scope {
		fs.StringVar(&f.scope, "scope", "", "for an event of a repeating series: this, following or all (required there)")
	}
	if repeats {
		fs.StringVar(&f.repeat, "repeat", "", "repeat: daily, weekdays, weekly, monthly or yearly")
		fs.IntVar(&f.every, "every", 0, "repeat every N days, weeks, months or years (1 to 99)")
		fs.StringVar(&f.days, "days", "", "weekdays of a weekly repeat, such as mo,we,fr")
		fs.StringVar(&f.by, "by", "", "monthly: monthday (the same day number) or weekday (the same weekday of the month)")
		fs.StringVar(&f.until, "until", "", "the last day an occurrence may start on: DATE")
		fs.IntVar(&f.count, "count", 0, "stop after N occurrences")
		fs.Var(&f.remind, "remind", "a reminder before the start, such as 10m, 1h or 1d (repeatable)")
		fs.BoolVar(&f.noRemind, "no-remind", false, "remove the event reminders (edit)")
	}
	if noRepeat {
		fs.BoolVar(&f.noRepeat, "no-repeat", false, "stop repeating (needs --scope following)")
	}
}

var repeatDayNames = []string{"mo", "tu", "we", "th", "fr", "sa", "su"}

func (f *seriesFlags) repeatGiven() bool {
	return f.repeat != "" || f.every != 0 || f.days != "" || f.by != "" || f.until != "" || f.count != 0
}

// repeatBody builds the repeat object, or nil when no repeat flag is given.
func (a *App) repeatBody(f *seriesFlags) (map[string]any, error) {
	if !f.repeatGiven() {
		return nil, nil
	}
	if f.noRepeat {
		return nil, usageErr("give --repeat or --no-repeat, not both")
	}
	if f.repeat == "" {
		return nil, usageErr("--every, --days, --by, --until and --count go with --repeat daily|weekdays|weekly|monthly|yearly")
	}
	freq, days := f.repeat, []string{}
	switch f.repeat {
	case "daily", "weekly", "monthly", "yearly":
	case "weekdays":
		freq, days = "weekly", repeatDayNames[:5]
	default:
		return nil, usageErr("--repeat: %q is not daily, weekdays, weekly, monthly or yearly", f.repeat)
	}
	if f.every < 0 || f.every > 99 {
		return nil, usageErr("--every: 1 to 99")
	}
	if f.days != "" {
		if f.repeat != "weekly" {
			return nil, usageErr("--days goes with --repeat weekly")
		}
		var err error
		if days, err = parseRepeatDays(f.days); err != nil {
			return nil, err
		}
	}
	if f.by != "" && (f.repeat != "monthly" || (f.by != "monthday" && f.by != "weekday")) {
		return nil, usageErr("--by monthday|weekday goes with --repeat monthly")
	}
	ends := map[string]any{"type": "never"}
	switch {
	case f.until != "" && f.count != 0:
		return nil, usageErr("give --until or --count, not both")
	case f.until != "":
		d, err := resolveDate(f.until, a.Now())
		if err != nil {
			return nil, usageErr("--until: %q is not a date", f.until)
		}
		ends = map[string]any{"type": "on", "date": d}
	case f.count != 0:
		if f.count < 1 || f.count > 999 {
			return nil, usageErr("--count: 1 to 999")
		}
		ends = map[string]any{"type": "after", "count": f.count}
	}
	rule := map[string]any{"freq": freq, "interval": max(f.every, 1), "days": days, "ends": ends}
	if f.by != "" {
		rule["by"] = f.by
	}
	return rule, nil
}

func parseRepeatDays(v string) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	for _, d := range strings.Split(strings.ToLower(v), ",") {
		d = strings.TrimSpace(d)
		ok := false
		for _, w := range repeatDayNames {
			ok = ok || w == d
		}
		if !ok || seen[d] {
			return nil, usageErr("--days: %q is not a list of mo,tu,we,th,fr,sa,su, each once", v)
		}
		seen[d] = true
		out = append(out, d)
	}
	return out, nil
}

// remindersBody turns --remind and --no-remind into minutes before the start;
// nil when neither is given.
func remindersBody(f *seriesFlags) ([]int, error) {
	if f.noRemind && len(f.remind) > 0 {
		return nil, usageErr("give --remind or --no-remind, not both")
	}
	if f.noRemind {
		return []int{}, nil
	}
	if len(f.remind) == 0 {
		return nil, nil
	}
	out := make([]int, 0, len(f.remind))
	for _, v := range f.remind {
		m, err := remindMinutes(v)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}

// remindMinutes reads 0, 10m, 1h or 2d as minutes.
func remindMinutes(v string) (int, error) {
	s := strings.TrimSpace(strings.ToLower(v))
	unit := time.Minute
	switch {
	case strings.HasSuffix(s, "m"):
		s = s[:len(s)-1]
	case strings.HasSuffix(s, "h"):
		s, unit = s[:len(s)-1], time.Hour
	case strings.HasSuffix(s, "d"):
		s, unit = s[:len(s)-1], 24*time.Hour
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 || n*int(unit/time.Minute) > 40320 {
		return 0, usageErr("--remind: %q is not a time before the start such as 10m, 1h or 1d (at most 4 weeks)", v)
	}
	return n * int(unit/time.Minute), nil
}

// needScope checks --scope against the event: required for an occurrence,
// refused for any other event, and compatible with the repeat change.
func needScope(ev *calEvent, f *seriesFlags, repeatChange, stop bool) error {
	switch f.scope {
	case "", "this", "following", "all":
	default:
		return usageErr("--scope: %q is not %s", f.scope, scopeList)
	}
	if ev.Series == nil {
		if f.scope != "" {
			return usageErr("--scope is for an event of a repeating series; %q does not repeat", ev.name())
		}
		return nil
	}
	if f.scope == "" {
		return &exitError{code: ExitUsage, jsonCode: "scope_required", msg: fmt.Sprintf(
			"%q repeats: say which events the change is for with --scope %s (this: only that one; following: it and every later one; all: the whole series)", ev.name(), scopeList)}
	}
	if (repeatChange || stop) && f.scope == "this" {
		return usageErr("changing the repeat needs --scope following or all, not this")
	}
	if stop && f.scope != "following" {
		return usageErr("--no-repeat needs --scope following")
	}
	return nil
}

// apply sets scope, repeat and reminders on a PATCH body.
func (f *seriesFlags) apply(body map[string]any, rule map[string]any, reminders []int) {
	if f.scope != "" {
		body["scope"] = f.scope
	}
	switch {
	case rule != nil:
		body["repeat"] = rule
	case f.noRepeat:
		body["repeat"] = nil
	}
	if reminders != nil {
		body["reminders"] = reminders
	}
}

// query is the scope of a DELETE as a query suffix.
func (f *seriesFlags) query() string {
	if f.scope == "" {
		return ""
	}
	return "&scope=" + f.scope
}
