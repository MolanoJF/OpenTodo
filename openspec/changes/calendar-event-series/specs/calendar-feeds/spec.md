## MODIFIED Requirements

### Requirement: Events served to devices
`GET /api/v1/calendar-feeds/events?from=YYYY-MM-DD&to=YYYY-MM-DD` SHALL return the caller's occurrences that overlap the range, at most 92 days wide, with start and end as `{date, time?, tz?}` values like a due, the subscription id and when the subscription was last refreshed. For an event of a connected calendar it SHALL also return `rev`, an opaque string that changes whenever the event's remote version does, and `access`, `{update, delete, reason?}`, which says whether OpenTodo may change or delete the event and, when it may not, why; an occurrence of a repeating event SHALL also carry `series`, `{id, rrule, repeat, custom, first, rev?}`: a stable `id` shared by the occurrences of the series, the calendar's rule text, the structured `repeat` (null when the rule is custom), whether this is the series' first occurrence, and for a CalDAV series the `rev` its occurrences share; the subscription of such an event SHALL carry `connectionId`, `calendarUrl`, `caps`, `editEvents` and `maxReminders`. Events of subscribed (ICS) calendars SHALL carry neither `rev` nor `access`. The route SHALL answer 400 `invalid_input` for a malformed or wider range.

#### Scenario: Week of events
- **WHEN** a device requests `from=2026-10-12&to=2026-10-18`
- **THEN** the response lists every occurrence of the user's enabled subscriptions that overlaps those days, including all-day and multi-day ones, and nothing from disabled subscriptions or other users

#### Scenario: Range too wide
- **WHEN** a device requests a range of 100 days
- **THEN** the response is a 400 `invalid_input` error naming `to`

#### Scenario: Unauthenticated
- **WHEN** the events route is requested without credentials
- **THEN** the response is a 401 `unauthorized` error

#### Scenario: Editable event of a connected calendar
- **WHEN** a connection with "Edit events in OpenTodo" on shows a writable calendar holding a single event without guests
- **THEN** the event is served with a `rev` and `access: {"update": true, "delete": true}`

#### Scenario: Locked event says why
- **WHEN** the same calendar holds an event with guests
- **THEN** that event is served with `access: {"update": false, "delete": false, "reason": "has_guests"}`

#### Scenario: Subscribed calendar event
- **WHEN** the response holds an event of an ICS subscription
- **THEN** it has no `rev` and no `access`

#### Scenario: Occurrence of a series
- **WHEN** a calendar holds a daily event with one exception and one excluded date
- **THEN** each served occurrence has the same `series.id`, `series.rrule` is `FREQ=DAILY`, `series.repeat` is `{"freq": "daily", "interval": 1, "ends": {"type": "never"}}`, the excluded date is absent, the exception is served with its own values, and all have `access.update: true`
