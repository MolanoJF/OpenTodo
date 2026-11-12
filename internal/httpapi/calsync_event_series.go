package httpapi

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/OpenTodoApp/opentodo/internal/calfeed"
	"github.com/OpenTodoApp/opentodo/internal/calsync"
)

// The scope, repeat and reminder members of an event body (OpenSpec change
// calendar-event-series, design D3). The body rules need nothing but the body;
// the rules that need the event (scope required or refused, a custom rule, the
// reminder limit of a calendar) are the Worker's.

const maxReminderList = 5

// scopeValues names the three scopes in a message.
const scopeValues = "this, following or all"

// seriesParts validates the scope, repeat and reminders members and sets them on
// the edit; field and message name the first thing wrong.
func (in eventPatch) seriesParts(edit *calsync.EventEdit) (string, string) {
	if in.Scope != nil {
		sc := calfeed.Scope(*in.Scope)
		if !sc.Valid() {
			return "scope", "scope is " + scopeValues
		}
		edit.Scope = sc
	}
	if in.Repeat.Set {
		switch {
		case edit.Scope == calfeed.ScopeThis:
			return "repeat", "a change of the repeat needs scope following or all, not this"
		case in.Repeat.Rule == nil && edit.Scope != calfeed.ScopeFollowing:
			return "repeat", "stopping a repeat (null) needs scope following"
		case in.Repeat.Rule != nil:
			if msg := checkRepeat(*in.Repeat.Rule); msg != "" {
				return "repeat", msg
			}
		}
		edit.Repeat = &calsync.RepeatEdit{Rule: in.Repeat.Rule}
	}
	if in.Reminders != nil {
		if msg := checkReminders(*in.Reminders); msg != "" {
			return "reminders", msg
		}
		edit.Reminders = in.Reminders
	}
	return "", ""
}

// checkRepeat validates the form of a repeat; msg names the first thing wrong.
func checkRepeat(r calfeed.Repeat) string {
	switch r.Freq {
	case "daily", "weekly", "monthly", "yearly":
	default:
		return "freq is daily, weekly, monthly or yearly"
	}
	if r.Interval < 0 || r.Interval > 99 {
		return "interval is 1 to 99"
	}
	if len(r.Days) > 0 && r.Freq != "weekly" {
		return "days is for a weekly repeat only"
	}
	if len(r.Days) > 7 {
		return "days lists mo to su once each"
	}
	seen := map[string]bool{}
	for _, d := range r.Days {
		if !validDay(d) || seen[d] {
			return "days lists mo to su once each"
		}
		seen[d] = true
	}
	switch {
	case r.By != "" && r.Freq != "monthly":
		return "by is for a monthly repeat only"
	case r.By != "" && r.By != calfeed.ByDay && r.By != calfeed.ByWeekd:
		return "by is monthday or weekday"
	}
	switch r.Ends.Type {
	case "", calfeed.EndNever:
	case calfeed.EndOn:
		if _, err := time.Parse(time.DateOnly, r.Ends.Date); err != nil {
			return "ends.date must be YYYY-MM-DD"
		}
	case calfeed.EndAfter:
		if r.Ends.Count < 1 || r.Ends.Count > 999 {
			return "ends.count is 1 to 999"
		}
	default:
		return "ends.type is never, on or after"
	}
	return ""
}

func validDay(d string) bool {
	for _, c := range []string{"mo", "tu", "we", "th", "fr", "sa", "su"} {
		if d == c {
			return true
		}
	}
	return false
}

// checkReminders validates a list of reminders: at most five whole minutes in
// -1439..40320, each once.
func checkReminders(list []int) string {
	if len(list) > maxReminderList {
		return fmt.Sprintf("send at most %d reminders", maxReminderList)
	}
	seen := map[int]bool{}
	for _, m := range list {
		if m < calfeed.MinReminder || m > calfeed.MaxReminder {
			return fmt.Sprintf("a reminder is %d to %d minutes before the start", calfeed.MinReminder, calfeed.MaxReminder)
		}
		if seen[m] {
			return "a reminder is listed once"
		}
		seen[m] = true
	}
	return ""
}

// invalidReason explains an invalid_input of the Worker or a writer by its field.
func invalidReason(field string) string {
	switch field {
	case "scope":
		return "scope applies to an event of a repeating series only, and is " + scopeValues
	case "repeat":
		return "this repeat can not be set here: it needs scope following or all, a valid rule, the event's own weekday for a weekly repeat, and a series the form can read"
	case "reminders":
		return "too many reminders for this calendar, or one out of range"
	case "start":
		return "this repeating event can not move to another day for all events"
	case "end":
		return "the end must be after the start"
	}
	return "the calendar or client id is not valid for this connection"
}

// seriesAnswer adds the series members of a change to the answer.
func seriesAnswer(out map[string]any, res *calsync.EventResult) {
	if res.Gone {
		out["event"] = nil
	}
	if res.Series != nil {
		series := map[string]any{"id": res.Series.ID}
		if res.Series.Rev != "" {
			series["rev"] = res.Series.Rev
		}
		out["series"] = series
	}
	if res.Refetch {
		out["refetch"] = true
	}
}

// deleteScopeOf reads the scope of a DELETE; field and message name what is wrong.
func deleteScopeOf(r *http.Request) (calfeed.Scope, string, string) {
	v := strings.TrimSpace(r.URL.Query().Get("scope"))
	if v == "" {
		return "", "", ""
	}
	sc := calfeed.Scope(v)
	if !sc.Valid() {
		return "", "scope", "scope is " + scopeValues
	}
	return sc, "", ""
}
