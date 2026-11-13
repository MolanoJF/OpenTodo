## ADDED Requirements

### Requirement: Repeating events are changed by scope
A signed-in user SHALL change or delete an occurrence of a repeating event of a calendar of one of their connections for one of three scopes, sent as `scope` on the `PATCH` and `DELETE` routes: `this`, only that occurrence; `following`, that occurrence and every later one; `all`, every occurrence. `scope` SHALL be required for an occurrence of a series (400 `scope_required` without it) and refused for an event that does not repeat (400 `invalid_input`). For a CalDAV or Google calendar the server SHALL, under a precondition on the version of the whole calendar object, write `this` as an exception of that occurrence in the same object (a delete as an excluded date and the removal of its exception), `following` as the end of the old series just before the occurrence plus a new series, with a new identity, that starts at the occurrence with the edited values, and `all` as a change of the series itself. For a Microsoft calendar the server SHALL change the occurrence for `this`, the series master for `all`, and for `following` end the master before the occurrence and create a new series. `following` on the first occurrence of a series SHALL act as `all`, and a delete of the first occurrence for `following` or `all`, or of the last remaining occurrence, SHALL delete the event. A `following` change that needs two writes SHALL write the new series first and the old series' end second and, when the second fails, delete the new series, so a failure leaves the calendar as it was or with a visible duplicate and never with lost occurrences. The server SHALL refresh its cached copy of the whole series in one transaction, record the audit event `calendar_event.changed` without the title, and wake the user's other devices. A change SHALL be refused with 409 `event_changed` when the version the caller saw is no longer the calendar's, as for any event.

#### Scenario: This event only
- **WHEN** a user moves the 2026-11-12 occurrence of a daily 07:00 to 08:30 series to 07:30 to 09:00 with `scope: "this"`
- **THEN** the calendar object holds the series unchanged and one exception for that day at 07:30 to 09:00, every other occurrence is at 07:00 to 08:30, and the occurrence keeps its `id`

#### Scenario: Delete this event
- **WHEN** a user deletes the 2026-11-15 occurrence with `scope: "this"`
- **THEN** the series gains an excluded date for that day, a stored exception for that day is removed, and the other occurrences remain

#### Scenario: This and following
- **WHEN** a user changes the time of the 2026-11-16 occurrence to 08:00 to 09:30 with `scope: "following"`
- **THEN** the old series ends with its 2026-11-15 occurrence and keeps its earlier exceptions, a new series with a new identity starts on 2026-11-16 at 08:00 to 09:30 with the same title, reminders and unknown properties, and exceptions and excluded dates from 2026-11-16 on belong to the new series

#### Scenario: A series with a count
- **WHEN** a user splits a series of 60 occurrences at its 47th occurrence
- **THEN** the old series ends on its 46th occurrence and the new series has 14 occurrences

#### Scenario: All events
- **WHEN** a user moves one occurrence 30 minutes later with `scope: "all"`
- **THEN** every occurrence of the series is 30 minutes later, its excluded dates and exceptions stay attached to the same days, and an exception that had been moved by hand keeps its own time

#### Scenario: Following on the first occurrence
- **WHEN** a user changes the title of the first occurrence with `scope: "following"`
- **THEN** the whole series is renamed and no new series is made

#### Scenario: Second write fails
- **WHEN** the new series has been created and the calendar refuses the change to the old series with a 412
- **THEN** the new series is deleted again, the response is 409 `event_changed`, and the calendar holds only the series it held before

#### Scenario: Microsoft occurrence
- **WHEN** a user renames one occurrence of an Outlook series they organise with `scope: "this"`, and then another with `scope: "all"`
- **THEN** Graph receives a `PATCH` of the occurrence's id, and then a `PATCH` of the series master with `If-Match`, and the response carries the renamed occurrence

#### Scenario: Microsoft this and following
- **WHEN** a user changes the time of an Outlook occurrence with `scope: "following"`
- **THEN** a new series starting at that occurrence is created, the old master's recurrence ends on the day before, and an event with attachments or an online meeting is refused with 422 `event_not_editable` (reason `following_unsupported`) before anything is written

### Requirement: A series keeps what the editor does not change
A change to a series SHALL keep every property of the remote object it does not change: unknown properties, reminders, other components and the exceptions and excluded dates of the occurrences it does not touch. A change with scope `all` SHALL carry a changed title, location, notes or busy or free into the exceptions that still hold the series' previous value, and SHALL move a change of time by the same difference for the series' start and for the exception identities, excluded dates and the times of exceptions that were still at their own slot, while an exception moved by hand keeps its own time; a change of the repeat SHALL remove the exceptions and excluded dates it makes meaningless and the editor SHALL say so before saving. A change of time zone SHALL keep the wall-clock times. Guests SHALL never be added or removed, and no invitation or update SHALL be sent.

#### Scenario: Rename all with a renamed exception
- **WHEN** a series is titled "Morning routine", one occurrence was renamed "Morning routine (late)", and a user renames the series to "Routine" for all events
- **THEN** the series and the occurrences with the old title read "Routine" and the renamed occurrence still reads "Morning routine (late)"

#### Scenario: Reminders and unknown properties survive
- **WHEN** a series has a 10 minute reminder and an `X-` property and one occurrence is moved with `scope: "this"`
- **THEN** the calendar object still holds both on the series, and the new exception carries the reminder

#### Scenario: Zone change
- **WHEN** a user changes the zone of a 07:00 series from Europe/Berlin to Europe/Lisbon for all events
- **THEN** the series is at 07:00 in Lisbon, its excluded dates and exceptions are still on the same calendar days

### Requirement: Repeat can be set and changed
`repeat` SHALL be `null` or an object `{freq, interval, days?, by?, ends}`: `freq` `daily`, `weekly`, `monthly` or `yearly`; `interval` 1 to 99; `days` (`mo` to `su`) for weekly, empty meaning the start's weekday; `by` `monthday` or `weekday` for monthly, meaning the start's day of the month or its weekday of the month (the fifth being the last); `ends` `{type: "never"}`, `{type: "on", date}` or `{type: "after", count}` with a count of 1 to 999. The server SHALL write the rule as an `RRULE` for CalDAV and Google and as a `patternedRecurrence` for Microsoft, and SHALL serve a series' rule as `series.repeat` when this form says exactly what the rule says and as `series.repeat: null` with `custom: true` otherwise, always with the calendar's own rule text as `series.rrule`. A change of `repeat` SHALL be refused with 400 `invalid_input` for scope `this`. `repeat` on an event that does not repeat SHALL make it repeat from its own start. `repeat: null` SHALL be accepted only with scope `following` and SHALL make the occurrence a single event. A rule the form cannot represent SHALL be kept unchanged by every other change, and SHALL NOT be changed through `repeat`. Microsoft series SHALL NOT accept `repeat: null` in this change.

#### Scenario: Make an event repeat
- **WHEN** a user PATCHes a single Tuesday event with `repeat` `{"freq": "weekly", "interval": 1, "days": [], "ends": {"type": "after", "count": 10}}`
- **THEN** the calendar holds the event with `FREQ=WEEKLY;COUNT=10` for a CalDAV calendar, and a weekly pattern on Tuesday with ten occurrences for Microsoft, and the next ten Tuesdays are served

#### Scenario: Weekdays
- **WHEN** a user sets a series to repeat on `mo` to `fr` every week for all events
- **THEN** the rule is `FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR`, and the editor shows "Every weekday" for it

#### Scenario: Nth weekday
- **WHEN** a user sets a monthly repeat `by: "weekday"` on an event starting on the third Thursday
- **THEN** the rule is `FREQ=MONTHLY;BYDAY=3TH`

#### Scenario: Ends on a date
- **WHEN** a user sets `ends` `{"type": "on", "date": "2026-12-31"}` on a 07:00 Berlin series
- **THEN** the series' last occurrence is the one on 2026-12-31, and reading the rule back gives the same date

#### Scenario: Custom rule
- **WHEN** a series repeats on the last weekday of the month
- **THEN** it is served with `series.repeat: null` and `custom: true`, a change of its title for all events leaves the rule as it was, and a PATCH of `repeat` for it answers 400 `invalid_input`

#### Scenario: Repeat for this event only
- **WHEN** a user sends `repeat` with `scope: "this"`
- **THEN** the response is a 400 `invalid_input` error naming `repeat` and nothing is written

#### Scenario: Stop repeating from an occurrence
- **WHEN** a user PATCHes `repeat: null` with `scope: "following"` on the 2026-11-16 occurrence of a CalDAV series
- **THEN** the old series ends with its 2026-11-15 occurrence and a single event exists on 2026-11-16

### Requirement: Events can have reminders
`reminders` SHALL be an array of whole minutes before the start, at most five (one for a Microsoft calendar), without repeats, from -1439 to 40320, a negative value meaning after the start of the day for an all-day event. For CalDAV and Google the server SHALL write each as a display alarm with a relative trigger and SHALL keep alarms the form cannot represent (other actions, absolute or end-relative triggers, repeating alarms) and count them; for Microsoft it SHALL write `reminderMinutesBeforeStart` and `isReminderOn`. `[]` SHALL remove the reminders the form represents. The editor's detail read SHALL serve `reminders`, `remindersMax` and the count of kept others. Reminders SHALL be delivered by the calendar provider and its apps: OpenTodo SHALL NOT send them and SHALL NOT add a notification path for them.

#### Scenario: Two reminders
- **WHEN** a user sets `reminders: [10, 1440]` on a CalDAV event
- **THEN** the object holds two display alarms with triggers `-PT10M` and `-P1D`, and the detail read serves `[10, 1440]`

#### Scenario: An alarm the editor cannot show
- **WHEN** an event has an email alarm and a user sets `reminders: [10]`
- **THEN** the email alarm is kept, the detail read serves `[10]` and one other reminder, and the editor says "1 other reminder is kept"

#### Scenario: Microsoft takes one
- **WHEN** a user sends two reminders for an Outlook event
- **THEN** the response is a 400 `invalid_input` error naming `reminders`, and one reminder sets `reminderMinutesBeforeStart` with `isReminderOn: true`

#### Scenario: Not an OpenTodo notification
- **WHEN** a reminder falls due
- **THEN** OpenTodo sends no push or in-app notification for it, and the editor says the calendar app sends it

### Requirement: Series that need two writes or cannot be written
The server SHALL answer 422 `event_not_editable` with reason `following_unsupported` before writing anything when a Microsoft series cannot be copied without losing attachments or an online meeting. It SHALL refuse a `repeat` change on a Microsoft series whose pattern the form cannot represent, and a move of a series to another day for all events when its rule cannot be shifted (monthly and yearly rules) with 400 `invalid_input` naming `start`. A series whose resource has changed while the server prepared the write SHALL be answered with 409 `event_changed`.

#### Scenario: Move all to another day, weekly
- **WHEN** a user drags one occurrence of a Monday and Wednesday series to Tuesday for all events
- **THEN** the series repeats on Tuesday and Thursday

#### Scenario: Move all to another day, monthly
- **WHEN** a user drags one occurrence of a series that repeats on the 15th of each month to the 16th for all events
- **THEN** the response is a 400 `invalid_input` error naming `start`, and the menu offers that choice disabled with the reason

### Requirement: Series writes refresh every occurrence
After a series is written the server SHALL read the objects it wrote back, expand them over the cached window and replace that series' cached occurrences in one transaction, so the events route serves the new state at once; a change that moves occurrence identities SHALL say `refetch: true` and the device SHALL refetch the shown range. The `rev` of a CalDAV series is the version of its calendar object, so after any write the device SHALL give every cached occurrence of that series the `series.rev` the response carries. No series change SHALL be an operation.

#### Scenario: Other occurrences stay editable
- **WHEN** a user changes one occurrence with `scope: "this"` and then drags another occurrence of the same series
- **THEN** the second request carries the new `rev` and is not refused as stale

#### Scenario: Identities after a move for all
- **WHEN** a user moves a series 30 minutes later for all events
- **THEN** the response says `refetch: true`, the device shows every occurrence at the new time after the refetch, and the occurrence that was edited is found under its new `id`

#### Scenario: Not an operation
- **WHEN** a series is changed
- **THEN** no operation is written, and the export and a backup contain no trace of it

## MODIFIED Requirements

### Requirement: Events can be changed and deleted in the connected calendar
While calendar sync is on, a signed-in user SHALL move, resize, rename and delete an event of a calendar of one of their connections, and change its location, description and busy or free, through `PATCH` and `DELETE /api/v1/calendar-sync/connections/{id}/events/{eventId}`, when the connection's "Edit events in OpenTodo" is on, the calendar's capability allows the change and the event is not locked. The `PATCH` body SHALL carry the `rev` the caller saw and any of `summary`, `start` and `end` together, `allDay`, `location` (empty to clear), `description` (plain text, empty to clear), `free`, `repeat` and `reminders`, and `scope` (`this`, `following` or `all`) when the event is an occurrence of a repeating event; it SHALL answer 200 with the event as the calendar now holds it (with its new `id`, `rev` and `access`) and `replaces`, the id it had, and, for an occurrence of a repeating event, `series` (its `id` and the new `rev` the series' other occurrences now have) and `refetch`, true when other occurrences changed. A `description` SHALL be refused with 422 `event_not_editable` when the calendar holds a rich-text body that plain text would flatten. The `DELETE` SHALL carry `rev` and, for an occurrence of a repeating event, `scope`, and answer 204. The server SHALL write to the remote calendar with a precondition on the event's version, keep every property of the remote event it does not change, never send an invitation or update to guests, refresh its cached copy, record an audit event without the title and wake the user's other devices. The route SHALL answer 404 `calendar_sync_disabled` while the owner's switch is off, 403 `demo_restricted` on the public demo, 401 `unauthorized` without credentials, 403 `forbidden` to read and agent tokens, 400 `scope_required` for an occurrence sent without `scope`, 404 `not_found` for a connection or event that is not the caller's, 403 `event_edits_off`, 403 `calendar_read_only`, 422 `event_not_editable`, 400 `invalid_input`, 404 `event_gone`, 429 `rate_limited` over 600 changes an hour, and 502 `calendar_unreachable` when the calendar fails. An event edit SHALL NOT be an operation: it SHALL NOT appear in the op log, the export or a backup.

#### Scenario: Move an event
- **WHEN** a user PATCHes an event of 09:00 to 09:30 on 2026-11-12 with `start` 10:00 and `end` 10:30 the same day and its current `rev`
- **THEN** the calendar holds the event at 10:00 to 10:30, its alarms and other properties unchanged, the response carries the event with a new `id`, a new `rev` and `replaces` set to the old id, and the user's other devices show the new time without reloading

#### Scenario: Rename an event
- **WHEN** a user PATCHes `{"rev": "...", "summary": "Planning"}` on a CalDAV event
- **THEN** the calendar object is written back whole with `If-Match` and only the summary and the bookkeeping properties changed

#### Scenario: Change location, notes and busy or free
- **WHEN** a user PATCHes `{"rev": "...", "location": "Room 4", "description": "Agenda attached", "free": true}` on a CalDAV event that has alarms
- **THEN** the calendar object holds the new location and description and `TRANSP:TRANSPARENT`, its alarms and unknown properties are unchanged, and the response carries the event with `free: true`

#### Scenario: Clear the location
- **WHEN** a user PATCHes `{"rev": "...", "location": ""}`
- **THEN** the event has no location in the calendar and the response carries `location: ""`

#### Scenario: Delete an event
- **WHEN** a user sends `DELETE` with the event's `rev`
- **THEN** the calendar no longer holds the event, the response is 204, the cached copy is gone and other devices stop showing it

#### Scenario: Not an operation
- **WHEN** an event is moved
- **THEN** no operation is written, the export and a backup contain no trace of the edit, and a device that was offline learns the new time from the events route, not from sync

#### Scenario: Edits off
- **WHEN** a user PATCHes an event of a connection whose `edit_events` is off
- **THEN** the response is a 403 `event_edits_off` error and the calendar is not contacted

#### Scenario: Read-only calendar
- **WHEN** a user PATCHes an event of a calendar whose caps are false
- **THEN** the response is a 403 `calendar_read_only` error and nothing is written

#### Scenario: Invalid change
- **WHEN** a user sends `start` without `end`, an end before the start, an unknown time zone, an empty summary, a `location` over 500 characters, a `description` over 10,000 characters or no field to change
- **THEN** the response is a 400 `invalid_input` error naming the field and nothing is written

#### Scenario: Another user's event
- **WHEN** a user PATCHes an event id of a different account, or an id that is not cached
- **THEN** the response is a 404 `not_found` error

#### Scenario: Unauthenticated
- **WHEN** either route is requested without credentials
- **THEN** the response is a 401 `unauthorized` error

#### Scenario: Agent or read token
- **WHEN** a read token or an agent token calls either route
- **THEN** the response is a 403 `forbidden` error

#### Scenario: Calendar unreachable
- **WHEN** the calendar server times out during a write
- **THEN** the response is a 502 `calendar_unreachable` error naming the reason, nothing is changed in the cache and the connection's other settings are untouched

#### Scenario: Microsoft event
- **WHEN** a user moves a single Outlook event they organise and that has no attendees
- **THEN** the server sends Graph a `PATCH` with `If-Match` set to the event's `@odata.etag` and only `start` and `end`, and the response holds the event with Graph's new ETag's `rev`

#### Scenario: Scope is required for an occurrence
- **WHEN** a user PATCHes an occurrence of a daily series with a new `start` and `end` and no `scope`
- **THEN** the response is a 400 `scope_required` error and nothing is written

#### Scenario: Scope on a single event
- **WHEN** a user PATCHes `{"rev": "...", "summary": "Planning", "scope": "all"}` on an event that does not repeat
- **THEN** the response is a 400 `invalid_input` error naming `scope` and nothing is written

### Requirement: Which events can be changed
An event SHALL be changeable only when it belongs to a connected calendar, its connection's "Edit events in OpenTodo" is on, its calendar's capability for the change is true, and it is a single event or an occurrence of a repeating event whose series OpenTodo can represent (the repeat rules and exceptions it reads and writes back without loss), without guests in the event or any exception of its series, organised by the user for Microsoft calendars, and with a version the calendar reported. Events of subscribed (ICS) calendars SHALL never be changeable. An event OpenTodo wrote for a task SHALL keep behaving as a task and SHALL NOT be served as an editable event. For every event the server serves, `access` SHALL say whether it may be updated or deleted and give the first unmet condition as `reason`: `edits_off`, `calendar_read_only`, `series_unsupported`, `has_guests`, `not_organizer`, `no_etag` or `refresh_pending`. A series SHALL be `series_unsupported` when its CalDAV object holds a `RANGE=THISANDFUTURE` exception, an `RDATE` or an `EXRULE`, more than one `RRULE`, a repeat rule OpenTodo cannot expand, more than one master event or events of another UID, more than 400 events or more than 256 KiB; and when a Microsoft occurrence does not name its series master. A cache read before this change that says `recurring` SHALL be served as `refresh_pending`. The write routes SHALL check every condition again.

#### Scenario: Repeating event
- **WHEN** a calendar holds a weekly event
- **THEN** every occurrence is served with `access: {"update": true, "delete": true}` and a `series`, and a PATCH or DELETE of one needs a `scope`

#### Scenario: Event with guests
- **WHEN** a CalDAV event has an `ATTENDEE` property
- **THEN** it is served with `access.reason: "has_guests"`, a PATCH answers 422 `event_not_editable` and no invitation is sent

#### Scenario: Meeting organised by someone else
- **WHEN** an Outlook event's `isOrganizer` is false
- **THEN** it is served with `access.reason: "not_organizer"` and cannot be changed

#### Scenario: Subscribed calendar
- **WHEN** a user shows an ICS subscription
- **THEN** its events carry no `access` and cannot be dragged, renamed or deleted

#### Scenario: An event OpenTodo wrote
- **WHEN** a task's event is in the target calendar
- **THEN** it is not an event of this requirement: moving it in the calendar follows the task rules of `calendar-sync`, and OpenTodo shows the task, not an event

#### Scenario: Cache from before the upgrade
- **WHEN** a server has just upgraded and has not yet read a connected calendar again
- **THEN** its events are served with `access.reason: "refresh_pending"` and become editable after the next read of that calendar

#### Scenario: A series that cannot be represented
- **WHEN** a CalDAV series has an exception with `RANGE=THISANDFUTURE`, or an `RDATE`
- **THEN** its occurrences are served with `access.reason: "series_unsupported"`, a PATCH answers 422 `event_not_editable` with that reason, and the popover says the repeat is too complex to change here

#### Scenario: Guests in an exception
- **WHEN** one exception of an otherwise guest-free series has an `ATTENDEE`
- **THEN** every occurrence of the series is served with `access.reason: "has_guests"` and nothing about it can be changed

### Requirement: Edits are queued offline and converge on the calendar
A device SHALL apply an edit to its local copy of the event at once and queue one command per event in a local outbox, carrying the `scope` for an occurrence of a repeating event; later edits of the same event with the same scope SHALL be merged into that command, an edit with another scope SHALL NOT be merged and SHALL wait until the first command has been answered, a delete SHALL replace it, and an edit that returns the event to what it was SHALL remove it. A device SHALL send queued commands, one at a time and in order, when it is online, at start and when the calendar views refresh, and SHALL keep the user's pending edits visible when it refetches events. A device SHALL show a change with scope `this` at once, and a change with scope `following` or `all` only on the edited occurrence with a saving state until the server answers, and then refetch the shown range; the other occurrences SHALL NOT be moved on the device before the answer. A delete SHALL wait 8 seconds before it is sent, so Undo can cancel it. An edit the server refuses (409, 404, 403, 422 or 400) SHALL be removed from the outbox, the event restored and the reason shown; a network failure or a 5xx SHALL keep it for a retry. Signing out SHALL delete the outbox. Devices SHALL converge on the calendar's state: after a refusal or a success every device reads the event from the server.

#### Scenario: Edit offline
- **WHEN** the device is offline and the user moves an event twice and renames it
- **THEN** one command holds the final time and title, the block shows the new time at once, and when the device comes online it sends one request

#### Scenario: Refetch keeps a pending edit
- **WHEN** the events cache is refreshed while a command is queued
- **THEN** the event is drawn with the queued change

#### Scenario: Two devices offline
- **WHEN** two devices move the same event offline and reconnect one after the other
- **THEN** the first one's edit is written, the second gets 409 `event_changed`, restores the event and shows the first one's time

#### Scenario: Refused edit
- **WHEN** a queued edit reaches the server after the user turned "Edit events in OpenTodo" off
- **THEN** the device drops the command, restores the event and shows that editing is off for the connection

#### Scenario: Sign out
- **WHEN** the user signs out with edits queued
- **THEN** the queue is deleted with the rest of the local data

#### Scenario: Move then move back
- **WHEN** the user moves an event and, before it is sent, moves it back
- **THEN** the command is removed and no request is made

#### Scenario: Offline change for all occurrences
- **WHEN** the device is offline and the user moves an occurrence of a daily series for all events
- **THEN** the dragged occurrence shows the new time and a saving state, the other occurrences are unchanged, and when the device is online it sends one request with `scope: "all"` and then shows every occurrence at the new time

#### Scenario: Another scope while one is saving
- **WHEN** the user changes the title of an occurrence for all events and, before the answer, changes its time for this event only
- **THEN** the second change waits, is sent after the first has been answered against the occurrence's new id and `rev`, and is refused with the reason shown if that occurrence no longer exists

### Requirement: Events are edited in Day and Week
In Day and Week the blocks of events whose `access.update` is true SHALL be movable to another time or day, resizable by their upper and lower edge and droppable on the all-day lane and the hours, with the existing pointer, touch long-press and edge-scroll behaviour of task blocks, and Escape SHALL cancel a drag. A drop, a key press or a save on an occurrence of a repeating event SHALL first ask which events the change is for (this event, this and following events, all events), through a menu that Escape closes without changing anything; a drop on a single event SHALL NOT ask. A drop SHALL write the change as in the first requirement, keep the event's own time zone, keep a floating event floating, and offer Undo, which restores the previous values with an inverse change; the Undo of a change for this and following events SHALL NOT be offered, because it split the series. Event blocks SHALL NOT drop into a slot, SHALL NOT be completed, and SHALL NOT show drag affordances when they cannot be changed; a locked event SHALL say why in a tooltip and in its popover. The event popover SHALL let the user change the title, date and times and delete the event when allowed, without dragging, and SHALL show the same reasons when it cannot. A block with a queued change SHALL show it as saving. Month, Year, Plan and the all-day chips of other views SHALL stay read-only.

#### Scenario: Drag to a new time
- **WHEN** the user drags a 30-minute Google event from 09:00 to 14:00 in the Week
- **THEN** the block moves at once, a toast offers Undo, the calendar holds the event at 14:00 to 14:30 after the request, and the block ends in its final place with the id the server answered

#### Scenario: Resize
- **WHEN** the user drags the lower edge of an event from 10:00 to 11:00
- **THEN** the event ends at 11:00 and its start is unchanged

#### Scenario: Event in another zone
- **WHEN** a user in Lisbon drags an event kept in Europe/Berlin
- **THEN** the event is written in Europe/Berlin at the matching wall time, not converted to Lisbon time

#### Scenario: Locked event
- **WHEN** the user presses on an occurrence of a series that is `series_unsupported`
- **THEN** no drag starts, the tooltip and the popover say the repeat is too complex to change here, and a task block beside it can still be dragged

#### Scenario: Undo
- **WHEN** the user presses Undo on the toast after a move
- **THEN** the event is moved back with a second change and the calendar holds the original time

#### Scenario: Delete with Undo
- **WHEN** the user deletes an event and presses Undo within 8 seconds
- **THEN** no request is sent and the event reappears

#### Scenario: Delete not undone
- **WHEN** the user deletes an event and 8 seconds pass
- **THEN** the delete is sent, the toast ends, and the event is gone from the calendar

#### Scenario: Not in a slot
- **WHEN** the user drops an event on a calendar slot
- **THEN** the event is not added to the slot and lands on the grid under the pointer

#### Scenario: Drag an occurrence
- **WHEN** the user drags one occurrence of a daily 07:00 to 08:30 block to 09:00 and the menu opens
- **THEN** the block shows at 09:00 with a saving state, nothing is sent until the user picks This event, This and following events or All events, and Escape puts the block back with no request

#### Scenario: This event only
- **WHEN** the user picks This event
- **THEN** only that day's occurrence is at 09:00, the other days are unchanged, the block keeps its id, and a toast offers Undo

### Requirement: Event edits have keyboard parity and a click alternative
Every pointer edit of an event SHALL have a keyboard equivalent and a one-click alternative that needs no dragging. An event that can be changed SHALL join the calendar's selection order, and the calendar's arrow keys SHALL move it by 15 minutes (Up, Down) and by a day (Left, Right), Shift+Up and Shift+Down SHALL move its end, Alt+Shift+Up and Alt+Shift+Down its start, and Delete or Backspace SHALL delete it, each with the toast and Undo of a drop; for an occurrence of a repeating event the same menu of scopes SHALL open once the keys settle, reachable and operable with the keyboard alone (arrows, Enter, Escape), and the Delete key SHALL ask it too. The popover's fields and Delete button SHALL be the one-click alternative, and the inventory of drags in `scripts/ux-audit/drag-alternatives.json` SHALL list it. Read-only events SHALL stay reachable with Tab for reading.

#### Scenario: Move with the arrows
- **WHEN** the user selects a changeable event with `j` and presses Down twice
- **THEN** the event is 30 minutes later, requested once after the keys settle as one change

#### Scenario: Delete key
- **WHEN** the user selects a changeable event and presses Delete
- **THEN** the event is hidden with a toast offering Undo and deleted after 8 seconds

#### Scenario: Keys ignore a locked event
- **WHEN** the user tabs onto a weekly event and presses Down
- **THEN** nothing changes

#### Scenario: Popover instead of dragging
- **WHEN** the user opens a changeable event and sets its start to 15:00 and presses Save
- **THEN** the event moves with the same request a drag makes

#### Scenario: Scope menu from the keyboard
- **WHEN** the user selects an occurrence of a series with `j`, presses Down twice and the keys settle
- **THEN** a menu offers This event, This and following events and All events, focus is on the first item, Enter picks it, and Escape leaves the occurrence where it was

### Requirement: Agents and the command line can change events
The MCP server SHALL offer `list_calendar_events`, `create_calendar_event`, `update_calendar_event` and `delete_calendar_event` over the same rules as the routes: the read tool to every token that may read, the write tools only when the owner's `mcp.writes` switch and the user's switch are on and the caller uses an `agent` token, with the per-token write limits and the `agent.write` audit event. `list_calendar_events` SHALL also list the calendars an event can be created in. `create_calendar_event` SHALL take an optional `client_id` that makes a repeated call create one event. `update_calendar_event` and `delete_calendar_event` SHALL take `scope` (required for an occurrence, and their descriptions SHALL explain the three values), and `create_calendar_event` and `update_calendar_event` SHALL take `repeat` and `reminders`. Their errors SHALL use the route's codes, and the titles they return SHALL be marked as untrusted text. The in-app assistant SHALL NOT offer these tools, and routines SHALL NOT use the write tools. `ot calendar events`, `ot calendar event add`, `ot calendar event edit` and `ot calendar event delete` SHALL call the routes with the user's token. `ot calendar event edit` and `ot calendar event delete` SHALL take `--scope this|following|all`, and `add` and `edit` SHALL take `--repeat` and `--remind`. The `ot` commands that change events SHALL need a full token.

#### Scenario: Agent moves an event
- **WHEN** an agent token with writes allowed calls `update_calendar_event` with an event id, its `rev` and a new `start` and `end`
- **THEN** the event moves in the calendar, the result holds the new event, and the audit log holds `agent.write` and `calendar_event.changed`

#### Scenario: Agent creates an event
- **WHEN** an agent token with writes allowed calls `create_calendar_event` with a `connection_id` and `calendar_url` from `list_calendar_events`, a title, a start and an end
- **THEN** the event exists in that calendar, the result holds the event, and the audit log holds `agent.write` and `calendar_event.changed` with the action `created`

#### Scenario: Agent retries a create
- **WHEN** an agent calls `create_calendar_event` twice with the same `client_id`
- **THEN** the calendar holds one event and both results hold it

#### Scenario: Calendars to choose from
- **WHEN** an agent calls `list_calendar_events`
- **THEN** the result lists each writable calendar with its `connection_id` and `calendar_url`

#### Scenario: Writes disabled
- **WHEN** the owner's `mcp.writes` switch is off
- **THEN** `update_calendar_event` and `create_calendar_event` are not offered, and a direct call is refused with `writes_disabled_by_owner`

#### Scenario: Stale revision
- **WHEN** an agent sends a `rev` the event no longer has
- **THEN** the result is the error `event_changed` and nothing is written

#### Scenario: Not in routines or the in-app assistant
- **WHEN** a routine step or the in-app assistant lists the tools it may use
- **THEN** the calendar event write tools are not among them

#### Scenario: Command line
- **WHEN** a user runs `ot calendar event edit <id> --start 2026-11-12T14:00` with a full token
- **THEN** the event moves and the command prints the new time

#### Scenario: Command line create
- **WHEN** a user with one writable calendar runs `ot calendar event add "Call Ana" --start 2026-11-13T14:00 --for 30m` with a full token
- **THEN** the event exists in that calendar and the command prints its id, title and time

#### Scenario: Read token
- **WHEN** a read token runs `ot calendar event delete <id>`
- **THEN** the command is refused before anything is sent, exits 5, and nothing is deleted

#### Scenario: Unauthenticated
- **WHEN** `ot calendar events` runs without a stored login
- **THEN** it exits 2 asking the user to log in, and a stored token the server rejects exits 3 with the 401 `unauthorized` message

#### Scenario: Agent changes all occurrences
- **WHEN** an agent token with writes allowed calls `update_calendar_event` for an occurrence with `scope: "all"` and a new `start` and `end`
- **THEN** every occurrence of the series moves by that difference, and the audit log holds `agent.write` and `calendar_event.changed`

#### Scenario: Agent forgets the scope
- **WHEN** an agent calls `update_calendar_event` for an occurrence without `scope`
- **THEN** the result is the error `scope_required`, which says what the three values mean, and nothing is written

#### Scenario: Command line with a scope
- **WHEN** a user runs `ot calendar event delete <id> --scope following` with a full token
- **THEN** the series ends before that occurrence and the command prints what was deleted, and without `--scope` on an occurrence it exits 2 and lists the three values

### Requirement: Events can be created in a connected calendar
While calendar sync is on, a signed-in user SHALL create an event in a shown calendar of one of their connections through `POST /api/v1/calendar-sync/connections/{id}/events`, when the connection's "Edit events in OpenTodo" is on and the calendar's `create` capability is true. The body SHALL carry `clientId` (8 to 64 characters of letters, digits, `_` and `-`), `calendarUrl`, `summary` (1 to 500 characters), `start` and `end` as `{date, time?, tz?}` with the rules of the change route, and MAY carry `allDay`, `location` (at most 500 characters), `description` (plain text, at most 10,000 characters), `free`, `repeat` (the structured rule of the change route) and `reminders`. The server SHALL write a new event to the calendar that fails if the event already exists, with no guests, no organizer and only the reminders asked for, store it in its cache, record an audit event without the title and wake the user's other devices, and SHALL answer 201 with `{event, clientId}`, the event being as the events route serves it (its `id`, `rev` and `access`). The route SHALL answer 404 `calendar_sync_disabled` while the owner's switch is off, 403 `demo_restricted` on the public demo, 401 `unauthorized` without credentials, 403 `forbidden` to read and agent tokens, 404 `not_found` for a connection that is not the caller's, 403 `event_edits_off`, 403 `calendar_read_only` when the calendar does not allow creating or answers 403, 400 `invalid_input` for a malformed body, a bad `clientId`, the time rules of the change route, or a `calendarUrl` that is not a shown calendar of the connection, 429 `rate_limited` over the 600 changes an hour that all event writes share, and 502 `calendar_unreachable` when the calendar fails. A created event SHALL NOT be an operation: it SHALL NOT appear in the op log, the export or a backup.

#### Scenario: Create a timed event
- **WHEN** a user POSTs `{"clientId": "6b1f1c2e-8f3a-4d0b-9d58-0c5a4e1b7a21", "calendarUrl": "<a shown calendar>", "summary": "Call Ana", "start": {"date": "2026-11-13", "time": "14:00", "tz": "Europe/Lisbon"}, "end": {"date": "2026-11-13", "time": "14:30", "tz": "Europe/Lisbon"}}`
- **THEN** the calendar holds a new event "Call Ana" from 14:00 to 14:30 in Lisbon time, the response is 201 with the event, its `rev` and `access: {"update": true, "delete": true}`, the events route serves it, and the user's other devices show it without reloading

#### Scenario: All-day event with details
- **WHEN** a user POSTs an all-day event with `allDay: true`, dates for `start` and `end` (the end being the day after the last day), a `location`, a `description` and `free: true`
- **THEN** the calendar holds an all-day event with that place and text, shown as free, and the events route serves `allDay: true` and `free: true`

#### Scenario: Microsoft event
- **WHEN** a user creates an event in an Outlook calendar
- **THEN** the server sends Graph a `POST` to that calendar's events with the subject, times, location and a plain-text body, and the response holds the event with Graph's ETag's `rev`

#### Scenario: Edits off
- **WHEN** a user POSTs to a connection whose `edit_events` is off
- **THEN** the response is a 403 `event_edits_off` error and the calendar is not contacted

#### Scenario: Calendar that does not allow creating
- **WHEN** a user POSTs to a calendar whose `caps.create` is false
- **THEN** the response is a 403 `calendar_read_only` error and nothing is written

#### Scenario: A calendar that is not shown
- **WHEN** a user POSTs a `calendarUrl` that the connection does not show
- **THEN** the response is a 400 `invalid_input` error naming `calendarUrl` and nothing is written

#### Scenario: Invalid event
- **WHEN** a user POSTs an empty `summary`, an end that is not after the start, an unknown time zone, a `clientId` of 3 characters or a `description` of 10,001 characters
- **THEN** the response is a 400 `invalid_input` error naming the field and nothing is written

#### Scenario: Another user's connection
- **WHEN** a user POSTs to a connection id of a different account
- **THEN** the response is a 404 `not_found` error

#### Scenario: Unauthenticated
- **WHEN** the route is requested without credentials
- **THEN** the response is a 401 `unauthorized` error

#### Scenario: Agent or read token
- **WHEN** a read token or an agent token POSTs to the route
- **THEN** the response is a 403 `forbidden` error

#### Scenario: Calendar unreachable
- **WHEN** the calendar server times out during the create
- **THEN** the response is a 502 `calendar_unreachable` error naming the reason, nothing is cached, and the client keeps the event queued to try again

#### Scenario: Not an operation
- **WHEN** an event is created
- **THEN** no operation is written, the export and a backup contain no trace of it, and a device that was offline learns of it from the events route, not from sync

#### Scenario: Create a repeating event
- **WHEN** a user POSTs an event of 07:00 to 08:30 with `repeat` `{"freq": "daily", "interval": 1, "ends": {"type": "never"}}` and `reminders` `[10]`
- **THEN** the calendar holds one series with a daily rule and a 10-minute reminder, the response carries the first occurrence with its `series`, and the events route serves the occurrences of the shown range

### Requirement: The full event editor
An event SHALL be created and edited in an editor with a title, an all-day switch, start and end date and time, a time zone, a calendar, a location, a plain-text description and a busy or free choice, a repeat and reminders, plus Duplicate, Convert to task and Delete. It SHALL be a panel in the shell's side slot from 1024 px and a bottom sheet on a phone, SHALL load only when opened, and SHALL be reachable from the event popover's "All details", a double-click on an event block, an all-day chip or an event row, and the add flows. Times SHALL be shown and entered in the selected zone, which is the viewer's zone for a new event and the event's own zone for an existing one, except that a UTC event is edited in the viewer's zone and a floating event stays floating until a zone is picked; a second line SHALL show the time in the viewer's zone when it differs. Changing the zone SHALL keep the wall-clock times. An all-day event SHALL show its end date as the last day it covers. The calendar SHALL be chosen when creating and SHALL be read-only when editing, an event SHALL NOT be moved to another calendar, and the server SHALL NOT accept a calendar change. Save SHALL send only the fields the user changed. Saving or deleting an occurrence of a repeating event SHALL ask which events the change is for before anything is sent, offering only the scopes the change allows (a change of the repeat is for this and following events or all events). The repeat SHALL offer none, every day, every weekday, every week on chosen days, every month on a day or on a weekday of the month, every year, and a custom interval, ending never, on a date or after a number of times; a rule the editor cannot represent SHALL be shown as "Custom repeat", read-only, while the other fields stay editable. Reminders SHALL be a list of times before the start, one for a Microsoft calendar, and reminders the editor cannot represent SHALL be kept and counted. Closing the editor with unsaved changes SHALL ask first. Events whose `access.update` is false SHALL keep the read-only popover with its reason. The editor SHALL work offline except for loading the description.

#### Scenario: Edit every field
- **WHEN** the user opens a plain event, sets a location, a description and "Free", and presses Save
- **THEN** one change carrying those three fields is sent, the calendar holds them, and a second calendar app shows the event with them

#### Scenario: All-day
- **WHEN** the user switches a 14:00–15:00 event to All day on 2026-11-13
- **THEN** the event becomes an all-day event on that day, the editor shows 13 November as both its first and last day, and the request carries the dates with `allDay: true`

#### Scenario: Another zone
- **WHEN** a user in Lisbon opens an event kept in Europe/Berlin at 10:00
- **THEN** the editor shows Europe/Berlin and 10:00 with "09:00 in your time" beneath, and changing the zone to Europe/Lisbon keeps 10:00 and moves the event an hour later in time

#### Scenario: Only what changed
- **WHEN** the user changes only the title of an event and saves
- **THEN** the request carries `rev` and `summary` and neither times nor description

#### Scenario: Calendar is fixed when editing
- **WHEN** the user opens an existing event
- **THEN** its calendar is shown as a label, not a picker, and a PATCH with any calendar field is refused with 400 `invalid_input`

#### Scenario: Unsaved changes
- **WHEN** the user types a new location and presses Escape
- **THEN** the editor asks whether to discard the changes before it closes

#### Scenario: Phone
- **WHEN** the editor opens on a phone
- **THEN** it is a bottom sheet with the same fields, and it closes with its Close control or Escape

#### Scenario: Locked event
- **WHEN** the user opens an event that cannot be changed, such as one with guests
- **THEN** it opens in the read-only popover with the reason, not in the editor, and Duplicate and Convert to task remain available

#### Scenario: Edit an occurrence
- **WHEN** the user opens one occurrence of a weekly event, changes the location and presses Save
- **THEN** a menu asks which events the change is for, nothing is sent before an answer, and the request carries the chosen `scope`

#### Scenario: Change the repeat
- **WHEN** the user opens an occurrence, sets the repeat to every weekday and presses Save
- **THEN** the menu offers only This and following events and All events, and the request carries `repeat` with the weekly rule on Monday to Friday

#### Scenario: Custom repeat
- **WHEN** the user opens an occurrence of a series whose rule is "last weekday of the month"
- **THEN** the repeat field reads "Custom repeat" and cannot be changed, the other fields can, and saving them leaves the rule as it was

#### Scenario: Reminders
- **WHEN** the user adds a 10 minute and a 1 day reminder to a CalDAV event and saves
- **THEN** the request carries `reminders: [10, 1440]`, and for a Microsoft calendar the editor offers one reminder only

### Requirement: Convert an event to a task
The editor and the event popover SHALL offer "Convert to task…" for any event. It SHALL ask once, with a switch "Delete the event from <calendar>" that is on by default when the user may delete the event and off and disabled otherwise (for an occurrence of a repeating event it deletes that occurrence only), remembered per device, and with "Create task". Creating SHALL make a task through the normal operations from the event's values as shown in the editor: its title; its description followed by the location when there is one as notes; for a timed event its start as the due, with time and time zone, and its length as the estimate; for an all-day event its last day as the due and, when it covers several days, its first day as the start of a span. The task SHALL be unplaced, as any new task without a project. When the switch is on the event SHALL be deleted with the usual 8 second delay. One toast SHALL offer one Undo that moves the task to the Trash and cancels the delete, and after the 8 seconds the delete SHALL be final. The task SHALL work offline, and a queued delete SHALL wait in the outbox. On an encrypted account the task's content SHALL be encrypted as any task's.

#### Scenario: Convert and delete
- **WHEN** the user converts a 14:00–15:30 event "Review draft" with a description, leaving the switch on
- **THEN** a task "Review draft" due that day at 14:00 with a 1 hour 30 minute estimate and the description as notes appears, and the event is deleted from the calendar after 8 seconds

#### Scenario: One Undo
- **WHEN** the user presses Undo on the toast within 8 seconds
- **THEN** the task is in the Trash and the event is still in the calendar, and no delete request was sent

#### Scenario: Keep the event
- **WHEN** the user turns the switch off and presses Create task
- **THEN** the task is created and the event is unchanged

#### Scenario: An event that cannot be deleted
- **WHEN** the user converts an event with guests, or one of a calendar whose `delete` capability is false
- **THEN** the switch is off and disabled, and the task is created from the event shown

#### Scenario: All-day span
- **WHEN** the user converts an all-day event from 16 to 18 November
- **THEN** the task starts on 16 November and is due on 18 November as a span

#### Scenario: Offline
- **WHEN** the device is offline and the user converts an event with the switch on
- **THEN** the task exists at once and the delete is sent when the device is online, unless Undo cancelled it first

#### Scenario: Convert one occurrence
- **WHEN** the user converts one occurrence of a daily series with the switch on
- **THEN** the task is made from that occurrence, and after 8 seconds only that occurrence is deleted (`scope: "this"`), the rest of the series is unchanged
