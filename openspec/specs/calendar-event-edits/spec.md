# calendar-event-edits Specification

## Purpose
Lets a user move, resize, rename and delete the events of a connected calendar from OpenTodo's Day and Week grids, the agent tools and the CLI, with the remote calendar as the source of truth. It is off until turned on per connection, and repeating events, events with guests and events the user does not organise stay read-only.

## Requirements

### Requirement: Events can be changed and deleted in the connected calendar
While calendar sync is on, a signed-in user SHALL move, resize, rename and delete an event of a calendar of one of their connections, and change its location, description and busy or free, through `PATCH` and `DELETE /api/v1/calendar-sync/connections/{id}/events/{eventId}`, when the connection's "Edit events in OpenTodo" is on, the calendar's capability allows the change and the event is not locked. The `PATCH` body SHALL carry the `rev` the caller saw and any of `summary`, `start` and `end` together, `allDay`, `location` (empty to clear), `description` (plain text, empty to clear) and `free`; it SHALL answer 200 with the event as the calendar now holds it (with its new `id`, `rev` and `access`) and `replaces`, the id it had. A `description` SHALL be refused with 422 `event_not_editable` when the calendar holds a rich-text body that plain text would flatten. The `DELETE` SHALL carry `rev` and answer 204. The server SHALL write to the remote calendar with a precondition on the event's version, keep every property of the remote event it does not change, never send an invitation or update to guests, refresh its cached copy, record an audit event without the title and wake the user's other devices. The route SHALL answer 404 `calendar_sync_disabled` while the owner's switch is off, 403 `demo_restricted` on the public demo, 401 `unauthorized` without credentials, 403 `forbidden` to read and agent tokens, 404 `not_found` for a connection or event that is not the caller's, 403 `event_edits_off`, 403 `calendar_read_only`, 422 `event_not_editable`, 400 `invalid_input`, 404 `event_gone`, 429 `rate_limited` over 600 changes an hour, and 502 `calendar_unreachable` when the calendar fails. An event edit SHALL NOT be an operation: it SHALL NOT appear in the op log, the export or a backup.

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

### Requirement: Which events can be changed
An event SHALL be changeable only when it belongs to a connected calendar, its connection's "Edit events in OpenTodo" is on, its calendar's capability for the change is true, and it is a single event: not repeating, not an occurrence or exception of a series, without guests, organised by the user for Microsoft calendars, and with a version the calendar reported. Events of subscribed (ICS) calendars SHALL never be changeable. An event OpenTodo wrote for a task SHALL keep behaving as a task and SHALL NOT be served as an editable event. For every event the server serves, `access` SHALL say whether it may be updated or deleted and give the first unmet condition as `reason`: `edits_off`, `calendar_read_only`, `recurring`, `has_guests`, `not_organizer`, `no_etag` or `refresh_pending`. The write routes SHALL check every condition again.

#### Scenario: Repeating event
- **WHEN** a calendar holds a weekly event
- **THEN** every occurrence is served with `access.reason: "recurring"`, a PATCH or DELETE of one answers 422 `event_not_editable`, and the popover says repeating events are read-only for now

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

### Requirement: The calendar is the source of truth
A change SHALL be conditional on the version of the event the user saw. The server SHALL answer 409 `event_changed` and write nothing when the `rev` sent is not the one it holds, or when the calendar refuses the write because the event changed (HTTP 412). A device that receives it SHALL restore the event as it was, refetch the visible range and tell the user the event changed in their calendar and the edit was not applied. When the calendar answers that the event no longer exists the server SHALL answer 404 `event_gone`, drop its cached copy and wake the user's devices. Neither side SHALL ever overwrite a version the user did not see, and no merge SHALL be attempted.

#### Scenario: Changed in another app
- **WHEN** the user moves an event in OpenTodo after the same event was moved in their phone's calendar app and OpenTodo has not read the calendar yet
- **THEN** the calendar refuses the write with a 412, the response is 409 `event_changed`, the block returns to the time the calendar holds and a toast says "changed in your calendar; your edit was not applied"

#### Scenario: Stale device
- **WHEN** a second device sends an edit for a `rev` the first device's edit has already replaced
- **THEN** the response is 409 `event_changed` without contacting the calendar and the second device shows the first device's result

#### Scenario: Deleted elsewhere
- **WHEN** the user renames an event that was deleted in the calendar app
- **THEN** the response is 404 `event_gone`, the event disappears from every device and a toast says so

#### Scenario: Unknown properties survive
- **WHEN** a CalDAV event has alarms, a `X-` property and a description and is moved in OpenTodo
- **THEN** the calendar's copy still has the alarms, the `X-` property and the description

### Requirement: Edits are queued offline and converge on the calendar
A device SHALL apply an edit to its local copy of the event at once and queue one command per event in a local outbox; later edits of the same event SHALL be merged into that command, a delete SHALL replace it, and an edit that returns the event to what it was SHALL remove it. A device SHALL send queued commands, one at a time and in order, when it is online, at start and when the calendar views refresh, and SHALL keep the user's pending edits visible when it refetches events. A delete SHALL wait 8 seconds before it is sent, so Undo can cancel it. An edit the server refuses (409, 404, 403, 422 or 400) SHALL be removed from the outbox, the event restored and the reason shown; a network failure or a 5xx SHALL keep it for a retry. Signing out SHALL delete the outbox. Devices SHALL converge on the calendar's state: after a refusal or a success every device reads the event from the server.

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

### Requirement: Events are edited in Day and Week
In Day and Week the blocks of events whose `access.update` is true SHALL be movable to another time or day, resizable by their upper and lower edge and droppable on the all-day lane and the hours, with the existing pointer, touch long-press and edge-scroll behaviour of task blocks, and Escape SHALL cancel a drag. A drop SHALL write the change as in the first requirement, keep the event's own time zone, keep a floating event floating, and offer Undo, which restores the previous values with an inverse change. Event blocks SHALL NOT drop into a slot, SHALL NOT be completed, and SHALL NOT show drag affordances when they cannot be changed; a locked event SHALL say why in a tooltip and in its popover. The event popover SHALL let the user change the title, date and times and delete the event when allowed, without dragging, and SHALL show the same reasons when it cannot. A block with a queued change SHALL show it as saving. Month, Year, Plan and the all-day chips of other views SHALL stay read-only.

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
- **WHEN** the user presses on a weekly event
- **THEN** no drag starts, the tooltip and the popover say repeating events are read-only for now, and a task block beside it can still be dragged

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

### Requirement: Event edits have keyboard parity and a click alternative
Every pointer edit of an event SHALL have a keyboard equivalent and a one-click alternative that needs no dragging. An event that can be changed SHALL join the calendar's selection order, and the calendar's arrow keys SHALL move it by 15 minutes (Up, Down) and by a day (Left, Right), Shift+Up and Shift+Down SHALL move its end, Alt+Shift+Up and Alt+Shift+Down its start, and Delete or Backspace SHALL delete it, each with the toast and Undo of a drop. The popover's fields and Delete button SHALL be the one-click alternative, and the inventory of drags in `scripts/ux-audit/drag-alternatives.json` SHALL list it. Read-only events SHALL stay reachable with Tab for reading.

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

### Requirement: Agents and the command line can change events
The MCP server SHALL offer `list_calendar_events`, `create_calendar_event`, `update_calendar_event` and `delete_calendar_event` over the same rules as the routes: the read tool to every token that may read, the write tools only when the owner's `mcp.writes` switch and the user's switch are on and the caller uses an `agent` token, with the per-token write limits and the `agent.write` audit event. `list_calendar_events` SHALL also list the calendars an event can be created in. `create_calendar_event` SHALL take an optional `client_id` that makes a repeated call create one event. Their errors SHALL use the route's codes, and the titles they return SHALL be marked as untrusted text. The in-app assistant SHALL NOT offer these tools, and routines SHALL NOT use the write tools. `ot calendar events`, `ot calendar event add`, `ot calendar event edit` and `ot calendar event delete` SHALL call the routes with the user's token. The `ot` commands that change events SHALL need a full token.

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

### Requirement: Events can be created in a connected calendar
While calendar sync is on, a signed-in user SHALL create an event in a shown calendar of one of their connections through `POST /api/v1/calendar-sync/connections/{id}/events`, when the connection's "Edit events in OpenTodo" is on and the calendar's `create` capability is true. The body SHALL carry `clientId` (8 to 64 characters of letters, digits, `_` and `-`), `calendarUrl`, `summary` (1 to 500 characters), `start` and `end` as `{date, time?, tz?}` with the rules of the change route, and MAY carry `allDay`, `location` (at most 500 characters), `description` (plain text, at most 10,000 characters) and `free`. The server SHALL write a new event to the calendar that fails if the event already exists, with no guests, no organizer and no reminders, store it in its cache, record an audit event without the title and wake the user's other devices, and SHALL answer 201 with `{event, clientId}`, the event being as the events route serves it (its `id`, `rev` and `access`). The route SHALL answer 404 `calendar_sync_disabled` while the owner's switch is off, 403 `demo_restricted` on the public demo, 401 `unauthorized` without credentials, 403 `forbidden` to read and agent tokens, 404 `not_found` for a connection that is not the caller's, 403 `event_edits_off`, 403 `calendar_read_only` when the calendar does not allow creating or answers 403, 400 `invalid_input` for a malformed body, a bad `clientId`, the time rules of the change route, or a `calendarUrl` that is not a shown calendar of the connection, 429 `rate_limited` over the 600 changes an hour that all event writes share, and 502 `calendar_unreachable` when the calendar fails. A created event SHALL NOT be an operation: it SHALL NOT appear in the op log, the export or a backup.

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

### Requirement: A created event is made once
The server SHALL keep, for each event it creates, a record of the connection, the `clientId` and the cached event, and SHALL delete records older than 30 days when it creates another. A `POST` whose `clientId` has a record for the connection SHALL NOT write to the calendar: when the cached event is still there it SHALL answer 200 with the event as it is now, and when the event has been deleted since it SHALL answer 404 `event_gone`. For a CalDAV calendar the object SHALL be named from the `clientId`, so a second create that reaches the calendar meets the existing object and is answered with that event. The record SHALL be kept per connection and SHALL be deleted with it.

#### Scenario: The answer was lost
- **WHEN** a device's create is received and the calendar write succeeds but the response never reaches the device, and the device sends the same `clientId` again
- **THEN** the calendar holds one event, the second response is 200 with that event and no second event is made

#### Scenario: Deleted in the meantime
- **WHEN** a create succeeds, the event is deleted in another app, and an old queued copy of the same create arrives
- **THEN** the response is 404 `event_gone`, nothing is written to the calendar, and the device drops the pending event and says it was deleted

#### Scenario: Two requests at once
- **WHEN** two requests with one `clientId` reach the server for the same connection at the same time
- **THEN** one writes the event and the other answers with it, and the calendar holds one event

#### Scenario: Records leave with the connection
- **WHEN** a user deletes a connection
- **THEN** its creation records are deleted with it

### Requirement: Event details are read when an editor opens
`GET /api/v1/calendar-sync/connections/{id}/events/{eventId}` SHALL read the event from the calendar and answer `{event, replaces?, description, descriptionEditable}`. `description` SHALL be plain text and SHALL NOT appear in the events route, the devices' cache or the audit log. When the calendar holds a newer version than the server's cache the server SHALL store it, wake the user's other devices and answer with the current row and `replaces`. `descriptionEditable` SHALL be false for a Microsoft event whose body is rich text with content; such a description SHALL be shown as plain text and a change of it SHALL be refused with 422 `event_not_editable` (reason `description_rich`). The route SHALL need the connection's "Edit events in OpenTodo" on, SHALL work for locked events, and SHALL answer 401, 404 `calendar_sync_disabled`, 404 `not_found`, 403 `event_edits_off`, 404 `event_gone` (the calendar no longer has the event; the cached copy is dropped) and 502 `calendar_unreachable` as the other event routes do. A device SHALL NOT send a description it could not load.

#### Scenario: Open the editor
- **WHEN** a user opens the editor of an event that has a description
- **THEN** the editor shows the description read from the calendar, and the events route for the same days carries no description

#### Scenario: Changed since the last refresh
- **WHEN** the calendar holds a newer version of the event than the server cached
- **THEN** the response carries the current event and `replaces`, the editor shows the current values, and a save uses the current `rev`

#### Scenario: Rich-text body
- **WHEN** an Outlook event has a formatted body with text in it
- **THEN** the description is shown read-only as plain text, `descriptionEditable` is false, and a PATCH of `description` for it answers 422 `event_not_editable`

#### Scenario: Offline
- **WHEN** the editor opens while the device is offline
- **THEN** the description field is disabled with the reason, the other fields work, and the save leaves the description out

#### Scenario: Unauthenticated
- **WHEN** the route is requested without credentials
- **THEN** the response is a 401 `unauthorized` error

#### Scenario: Deleted elsewhere
- **WHEN** the event was deleted in another app
- **THEN** the response is 404 `event_gone`, the event disappears from every device, and the editor closes with a message

### Requirement: Created events are queued offline and shown at once
A device SHALL draw a new event at once as a pending block with a saving state and queue one create command in its outbox; edits of the pending event SHALL be merged into that command, a delete of it before it is sent SHALL remove the command and the block with no request, and Duplicate and Convert to task SHALL work on it. When the server answers 200 or 201 the device SHALL replace the pending block with the calendar's event and keep selections, popovers and Undo pointing at it. A create the server refuses (400, 403, 404, 409 or 422) SHALL remove the pending block and the command, tell the user why, and offer to reopen the editor with what they typed. A network failure or a 5xx SHALL keep the command for a retry with the same `clientId`. Refreshing the events SHALL keep pending blocks. Signing out SHALL delete the outbox. An Undo of a created event SHALL remove an unsent create, and SHALL delete the event once it exists. Devices SHALL converge on the calendar: after the create every device reads the event from the server.

#### Scenario: Create offline
- **WHEN** the device is offline and the user adds "Dentist" at 09:00, then renames it and moves it to 10:00 before reconnecting
- **THEN** the block shows at 10:00 as "Dentist" at once, one command holds the final event, and when the device is online it sends one request

#### Scenario: Delete before sending
- **WHEN** the user deletes the pending event before the device is online
- **THEN** the command and the block are removed and no request is ever made

#### Scenario: Edit while it is being created
- **WHEN** the user changes the title of a pending event while its create is in flight
- **THEN** the change is sent as a change of the new event after the answer, and the block shows the new title throughout

#### Scenario: Refused create
- **WHEN** a queued create reaches the server after the user turned "Edit events in OpenTodo" off
- **THEN** the block disappears, a toast says editing events is off for the connection and offers to reopen the editor with the typed event

#### Scenario: Refresh keeps it
- **WHEN** the events cache is refreshed while a create is queued
- **THEN** the pending block is still drawn

#### Scenario: Undo
- **WHEN** the user presses Undo on the "Event added" toast
- **THEN** an unsent create is removed without a request, and a created event is deleted from the calendar with the same 8 second Undo as any delete

#### Scenario: Sign out
- **WHEN** the user signs out with a create queued
- **THEN** the queue is deleted with the rest of the local data

### Requirement: Task or event when adding on the calendar
The device SHALL remember, for itself, whether the user last chose a task or an event when adding on the Day and Week grids, defaulting to a task, and SHALL offer the event choice only while the user has at least one writable calendar: a shown calendar of a connection with "Edit events in OpenTodo" on whose `create` capability is true. A double-click or `q` on the grid SHALL open the remembered kind. The add menu (right-click, or a long press on touch) SHALL list "Add task here…" and "Add event here…", the remembered kind first. The add dialog SHALL carry a Task or Event control. A press and drag over an empty part of a day column with a mouse or pen SHALL size a new item from 15 minutes up, show its time range, and on release open the remembered kind with that start and end; Escape SHALL cancel it, and a touch screen SHALL keep the long press and not sweep. While the user has no writable calendar none of this SHALL be offered and the grid SHALL behave as before. Creating an event while the device hides calendars SHALL show them again. The choice is per device and never synced.

#### Scenario: Default stays a task
- **WHEN** a user with a writable calendar double-clicks an empty slot for the first time
- **THEN** the add task dialog opens with a Task or Event control showing Task

#### Scenario: Remembered
- **WHEN** the user picks Event in that control, adds an event, and later double-clicks another slot
- **THEN** the event editor opens at that slot

#### Scenario: No writable calendar
- **WHEN** a user with no connection that allows edits right-clicks an empty slot, double-clicks it or drags over it
- **THEN** the menu holds only the entries it has today, the dialog has no control, and the drag creates nothing

#### Scenario: Sweep for an event
- **WHEN** a user with a writable calendar drags from 14:00 to 15:30 on an empty part of Wednesday and the remembered kind is Event
- **THEN** a ghost shows "14:00 – 15:30" while dragging, and on release the editor opens with that start and end

#### Scenario: Sweep for a task
- **WHEN** the remembered kind is Task and the user sweeps 14:00 to 15:30
- **THEN** the add task dialog opens due Wednesday at 14:00 with an estimate of 1 hour 30 minutes

#### Scenario: Cancel the sweep
- **WHEN** the user presses Escape during the sweep
- **THEN** nothing opens and nothing is created

#### Scenario: Calendars hidden
- **WHEN** the device hides calendars and the user adds an event
- **THEN** calendars are shown again, the toast says so, and the new event is visible

### Requirement: Creating events has a keyboard path and a click alternative
Every pointer way to create an event SHALL have a keyboard equivalent and a one-click alternative that needs no dragging. `q` in Day and Week SHALL open the remembered kind for the focused day, and `Shift+Q` SHALL open the event editor for the focused day at its next free half hour (09:00 on a day other than today), both registered in the shortcut registry without a conflict. The editor's date and time fields and the add menu's "Add event here…" SHALL be the click alternative to the sweep, and the inventory of drags in `scripts/ux-audit/drag-alternatives.json` SHALL list them.

#### Scenario: Keyboard
- **WHEN** a Week is shown with a block on Thursday selected and the user presses `Shift+Q`
- **THEN** the event editor opens for Thursday at 09:00 with the title focused

#### Scenario: No drag needed
- **WHEN** a user cannot drag and wants an event from 14:00 to 15:30
- **THEN** they can right-click the slot, choose "Add event here…" and set the end time in the editor, or press `Shift+Q` and set both times

### Requirement: The full event editor
An event SHALL be created and edited in an editor with a title, an all-day switch, start and end date and time, a time zone, a calendar, a location, a plain-text description and a busy or free choice, plus Duplicate, Convert to task and Delete. It SHALL be a panel in the shell's side slot from 1024 px and a bottom sheet on a phone, SHALL load only when opened, and SHALL be reachable from the event popover's "All details", a double-click on an event block, an all-day chip or an event row, and the add flows. Times SHALL be shown and entered in the selected zone, which is the viewer's zone for a new event and the event's own zone for an existing one, except that a UTC event is edited in the viewer's zone and a floating event stays floating until a zone is picked; a second line SHALL show the time in the viewer's zone when it differs. Changing the zone SHALL keep the wall-clock times. An all-day event SHALL show its end date as the last day it covers. The calendar SHALL be chosen when creating and SHALL be read-only when editing, an event SHALL NOT be moved to another calendar, and the server SHALL NOT accept a calendar change. Save SHALL send only the fields the user changed. Closing the editor with unsaved changes SHALL ask first. Events whose `access.update` is false SHALL keep the read-only popover with its reason. The editor SHALL work offline except for loading the description.

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
- **WHEN** the user opens a weekly event
- **THEN** it opens in the read-only popover with the reason, not in the editor, and Duplicate and Convert to task remain available

### Requirement: Duplicate an event
The editor and the event popover SHALL offer Duplicate for any event of a connected or subscribed calendar whenever the user has a writable calendar. Duplicate SHALL open the editor in create mode with the event's title, times, time zone, location, description, and busy or free, in the same calendar when it is writable and else in the default calendar, with a new `clientId`; nothing SHALL be created until the user saves. A repeating event SHALL be copied as the single occurrence that was shown. Guests, reminders and links SHALL NOT be copied. Duplicate SHALL be the way to put an event in another calendar.

#### Scenario: Duplicate into another calendar
- **WHEN** the user duplicates a 10:00 event of calendar A, chooses calendar B in the picker and saves
- **THEN** calendar B holds a new event with the same details, calendar A's event is unchanged, and the two have different identities

#### Scenario: Duplicate a subscribed event
- **WHEN** the user duplicates an event of a subscribed (ICS) calendar
- **THEN** the editor opens in create mode in the default writable calendar and saving creates a normal event there

#### Scenario: Nothing until saved
- **WHEN** the user duplicates an event and closes the editor
- **THEN** no request is made

### Requirement: Convert an event to a task
The editor and the event popover SHALL offer "Convert to task…" for any event. It SHALL ask once, with a switch "Delete the event from <calendar>" that is on by default when the user may delete the event and off and disabled otherwise, remembered per device, and with "Create task". Creating SHALL make a task through the normal operations from the event's values as shown in the editor: its title; its description followed by the location when there is one as notes; for a timed event its start as the due, with time and time zone, and its length as the estimate; for an all-day event its last day as the due and, when it covers several days, its first day as the start of a span. The task SHALL be unplaced, as any new task without a project. When the switch is on the event SHALL be deleted with the usual 8 second delay. One toast SHALL offer one Undo that moves the task to the Trash and cancels the delete, and after the 8 seconds the delete SHALL be final. The task SHALL work offline, and a queued delete SHALL wait in the outbox. On an encrypted account the task's content SHALL be encrypted as any task's.

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
- **WHEN** the user converts a recurring event, or one of a calendar whose `delete` capability is false
- **THEN** the switch is off and disabled, and the task is created from the occurrence shown

#### Scenario: All-day span
- **WHEN** the user converts an all-day event from 16 to 18 November
- **THEN** the task starts on 16 November and is due on 18 November as a span

#### Scenario: Offline
- **WHEN** the device is offline and the user converts an event with the switch on
- **THEN** the task exists at once and the delete is sent when the device is online, unless Undo cancelled it first

### Requirement: Default calendar for new events
A device SHALL create new events in the calendar the user last created one in or chose in the editor, when that calendar is still writable, and otherwise in the first writable calendar of the first connection. The choice SHALL be per device, SHALL NOT be synced, and SHALL be deleted on sign-out.

#### Scenario: Remembered calendar
- **WHEN** the user creates an event in "Work" and later opens "New event" from the palette
- **THEN** the editor opens with "Work" selected

#### Scenario: Calendar no longer writable
- **WHEN** the remembered calendar's connection turns "Edit events in OpenTodo" off
- **THEN** the editor opens with the first writable calendar of the first connection, or no event choice is offered when there is none
