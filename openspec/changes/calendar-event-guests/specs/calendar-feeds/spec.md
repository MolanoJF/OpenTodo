## ADDED Requirements

### Requirement: Guest lists are read from the server's cache
`GET /api/v1/calendar-feeds/events/{eventId}/guests` SHALL answer, for an event of one of the caller's connected calendars, `{organizer, guests, count, truncated}` from the server's cached copy of the calendar, without a request to the calendar: the organiser (name, address, whether it is the user), each guest (name, address, `role` `required`, `optional` or `room`, `response` `accepted`, `tentative`, `declined` or `needs-action`, whether it is the user), the number of attendees other than the user and whether the list was cut at 100. The answer SHALL carry `Cache-Control: no-store`. The route SHALL work for events that cannot be changed and while "Edit events in OpenTodo" is off, SHALL answer 404 `not_found` for an event that is not the caller's, is not cached or has no guests, 404 `calendar_sync_disabled` while the owner's switch is off, 403 `demo_restricted` on the public demo and 401 `unauthorized` without credentials, and SHALL be open to read tokens. A device SHALL request it when a popover's guest list is opened, SHALL keep the answer in memory only and SHALL discard it when the popover closes. An ICS feed event SHALL have no guest list.

#### Scenario: Open the list
- **WHEN** a user expands the guests of a meeting with three guests
- **THEN** the device requests the guests route once, the response lists the organiser and three guests with their answers, and the calendar server receives no request

#### Scenario: Another user's event
- **WHEN** a user requests the guests of an event id of a different account
- **THEN** the response is a 404 `not_found` error

#### Scenario: Event without guests
- **WHEN** a user requests the guests of an event that has none
- **THEN** the response is a 404 `not_found` error

#### Scenario: Unauthenticated
- **WHEN** the route is requested without credentials
- **THEN** the response is a 401 `unauthorized` error

#### Scenario: Not stored on the device
- **WHEN** the popover closes
- **THEN** the guest list is dropped from memory and the device's database never held it

## MODIFIED Requirements

### Requirement: Events served to devices
`GET /api/v1/calendar-feeds/events?from=YYYY-MM-DD&to=YYYY-MM-DD` SHALL return the caller's occurrences that overlap the range, at most 92 days wide, with start and end as `{date, time?, tz?}` values like a due, the subscription id and when the subscription was last refreshed. For an event of a connected calendar it SHALL also return `rev`, an opaque string that changes whenever the event's remote version does, and `access`, `{update, delete, reason?, notifies?}`, which says whether OpenTodo may change or delete the event and, when it may not, why; an occurrence of a repeating event SHALL also carry `series`, `{id, rrule, repeat, custom, first, rev?}`: a stable `id` shared by the occurrences of the series, the calendar's rule text, the structured `repeat` (null when the rule is custom), whether this is the series' first occurrence, and for a CalDAV series the `rev` its occurrences share; an event with other attendees or an answer of the user's SHALL carry `guests`, `{count, you?, organizer, canRespond}`, never a name or an address, and an event with a join address SHALL carry `conference`, `{url, provider}`; the subscription of such an event SHALL carry `connectionId`, `calendarUrl`, `caps` (with `onlineMeeting` when known), `editEvents`, `maxReminders` and `guests`, `{respond, silent, edit}`, what the provider lets OpenTodo do with guests. Events of subscribed (ICS) calendars SHALL carry neither `rev` nor `access`. The route SHALL answer 400 `invalid_input` for a malformed or wider range.

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

#### Scenario: Event with guests
- **WHEN** a Microsoft calendar holds a meeting the user organises, with three guests, one of whom has accepted
- **THEN** the event is served with `guests: {"count": 3, "organizer": true, "canRespond": false}`, `access.notifies: true` and no name or address anywhere in the response

#### Scenario: Invitation the user can answer
- **WHEN** a Microsoft calendar holds a meeting someone else organises and the user has not answered
- **THEN** the event is served with `guests.you: "needs-action"`, `guests.canRespond: true` and `access.reason: "not_organizer"`

#### Scenario: Event with a join link
- **WHEN** a Google event has `X-GOOGLE-CONFERENCE:https://meet.google.com/abc-defg-hij`
- **THEN** it is served with `conference: {"url": "https://meet.google.com/abc-defg-hij", "provider": "meet"}`

#### Scenario: A link that is not https
- **WHEN** an event's description holds `http://zoom.us/j/123` or `https://zoom.us@evil.example/j/123`
- **THEN** the event is served with no `conference`
