package agenttools

import (
	"bytes"
	"encoding/json"
)

// The scope, repeat and reminders arguments of the calendar event tools
// (OpenSpec change calendar-event-series, design D13). Their descriptions are
// part of the contract: an agent learns from them what a repeating event is
// and what each scope does.

const scopeText = "scope (this | following | all) says which events of a repeating series the change is for. " +
	"It is required when the event has a `series` in list_calendar_events and refused otherwise. " +
	"`this` changes only that occurrence. " +
	"`following` changes that occurrence and every later one: the series is split, a new series starts at that occurrence (on the first occurrence it equals `all`). " +
	"`all` changes every occurrence, past and future. " +
	"Changing the repeat needs `following` or `all`. Delete uses the same values."

const remindersText = "Reminders as minutes before the start (for example [10, 1440]; at most 5, 0 at the start, a negative number after it for an all-day event's morning). " +
	"They are sent by the calendar provider, not by OpenTodo, and a Microsoft calendar takes one. An empty list removes the event's reminders."

const repeatText = "How the event repeats: {freq: daily|weekly|monthly|yearly, interval: 1 to 99, days: [mo..su] (weekly only; a weekly repeat includes the start's weekday), by: monthday|weekday (monthly only), " +
	"ends: {type: never} | {type: on, date: YYYY-MM-DD} | {type: after, count: 1 to 999}}. "

// EventScopes are the values of scope.
var EventScopes = []string{"this", "following", "all"}

var scopeProp = map[string]any{"type": "string", "enum": EventScopes, "description": scopeText}

var remindersProp = map[string]any{"type": "array", "maxItems": 5, "items": map[string]any{"type": "integer"}, "description": remindersText}

func repeatProp(update bool) map[string]any {
	desc := repeatText
	var typ any = "object"
	if update {
		desc += "Changing it needs scope following or all (not this). null stops the repeat and is valid with following only (a Microsoft series cannot stop repeating). Omit it to leave the repeat as it is."
		typ = []string{"object", "null"}
	} else {
		desc += "Omit it for a single event."
	}
	return map[string]any{"type": typ, "additionalProperties": false, "required": []string{"freq"}, "description": desc,
		"properties": map[string]any{
			"freq":     map[string]any{"type": "string", "enum": []string{"daily", "weekly", "monthly", "yearly"}},
			"interval": map[string]any{"type": "integer", "minimum": 1, "maximum": 99},
			"days":     map[string]any{"type": "array", "maxItems": 7, "items": map[string]any{"type": "string", "enum": []string{"mo", "tu", "we", "th", "fr", "sa", "su"}}},
			"by":       map[string]any{"type": "string", "enum": []string{"monthday", "weekday"}},
			"ends": map[string]any{"type": "object", "additionalProperties": false, "required": []string{"type"}, "properties": map[string]any{
				"type":  map[string]any{"type": "string", "enum": []string{"never", "on", "after"}},
				"date":  map[string]any{"type": "string", "format": "date"},
				"count": map[string]any{"type": "integer", "minimum": 1, "maximum": 999},
			}},
		}}
}

// scope reads the optional scope argument.
func (a args) scope() (string, error) {
	v, ok, err := a.text("scope", false, 20)
	if err != nil || !ok {
		return "", err
	}
	for _, s := range EventScopes {
		if v == s {
			return v, nil
		}
	}
	return "", argErr("scope: must be this, following or all (this: only that occurrence; following: it and every later one; all: the whole series)")
}

// eventRepeat reads repeat: absent, an object or null (set true, rule nil).
func (a args) eventRepeat() (rule *EventRepeat, set bool, err error) {
	raw, ok := a["repeat"]
	if !ok {
		return nil, false, nil
	}
	if string(raw) == "null" {
		return nil, true, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var r EventRepeat
	if dec.Decode(&r) != nil || r.Freq == "" {
		return nil, false, argErr(`repeat: must be {"freq": "daily|weekly|monthly|yearly", "interval"?, "days"?, "by"?, "ends"?: {"type": "never|on|after", ...}} or null`)
	}
	if r.Ends.Type == "" {
		r.Ends.Type = "never"
	}
	return &r, true, nil
}

// eventReminders reads reminders: nil when absent.
func (a args) eventReminders() (*[]int, error) {
	if !a.present("reminders") {
		return nil, nil
	}
	var list []int
	if json.Unmarshal(a["reminders"], &list) != nil {
		return nil, argErr("reminders: must be an array of whole minutes before the start")
	}
	if list == nil {
		list = []int{}
	}
	return &list, nil
}

// withSeries reads scope, repeat and reminders of an update and applies the
// rules that need nothing but the arguments.
func (u EventUpdate) withSeries(a args) (EventUpdate, error) {
	var err error
	if u.Scope, err = a.scope(); err != nil {
		return u, err
	}
	if u.Repeat, u.RepeatSet, err = a.eventRepeat(); err != nil {
		return u, err
	}
	if u.Reminders, err = a.eventReminders(); err != nil {
		return u, err
	}
	if u.RepeatSet && u.Scope == "this" {
		return u, argErr("repeat: changing the repeat needs scope following or all, not this")
	}
	if u.RepeatSet && u.Repeat == nil && u.Scope != "" && u.Scope != "following" {
		return u, argErr("repeat: null (stop repeating) is valid with scope following only")
	}
	return u, nil
}
