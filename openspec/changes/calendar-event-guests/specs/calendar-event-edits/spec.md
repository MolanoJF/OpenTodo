## ADDED Requirements

### Requirement: Guests are shown on an event
The event popover and the editor SHALL show an event's organiser and guests: each guest's name or address, whether they are required or optional (a room as a room), and their answer (accepted, maybe, declined or no answer), with the user's own answer named, in words and never by colour alone. The events route and the devices' cache SHALL carry only a summary, `guests` (`count`, the user's answer, whether the user organises the event, whether the user can reply), and the list SHALL be loaded when a popover opens and held in memory only (`calendar-feeds`). A guest list SHALL be shown for events of connected calendars on every provider, including those that cannot be changed, and SHALL be cut at 100 guests with the real count. Offline, the popover SHALL show the summary and say the list needs a connection. Devices SHALL converge on the calendar: the summary and the list are read from the server's copy of the calendar and replaced whenever the event is read again.

#### Scenario: Popover of an invitation
- **WHEN** the user opens the popover of a meeting with the organiser Ana and guests Ben (optional, accepted) and the user (no answer yet)
- **THEN** it shows "Ana Silva · 2 guests, 1 accepted", expands to the list with Ben "optional, accepted", and shows the user's own answer as "No answer yet"

#### Scenario: Large meeting
- **WHEN** an event has 140 guests
- **THEN** the list shows 100 of them with the count "140 guests" and a line saying the list is cut

#### Scenario: Offline
- **WHEN** the device is offline and the user opens an event with guests
- **THEN** the summary is shown from the cache and the list says it needs a connection

#### Scenario: Room
- **WHEN** an Outlook meeting has a room as an attendee
- **THEN** the room is listed as a room, has no Remove button in the editor, and counts as an attendee when the user is asked whether to tell the guests

### Requirement: An invited user can answer
A signed-in user SHALL answer an invitation, accepted, maybe or declined, through `POST /api/v1/calendar-sync/connections/{id}/events/{eventId}/response`, when the connection's "Edit events in OpenTodo" is on, the calendar's update capability is true, the user is a guest and not the organiser, the provider answers invitations for them, and the event has a version. The body SHALL carry the `rev` the caller saw, `response` (`accepted`, `tentative` or `declined`) and, for an occurrence of a repeating event, `scope` (`this` or `all`), and MAY carry `sendResponse` (default true; `false` only where the provider honours it). On a Microsoft calendar the server SHALL call the event's accept, tentatively-accept or decline action with `sendResponse`, `this` on the occurrence and `all` on the series master. On a Google calendar the server SHALL change only the user's own `PARTSTAT`, on the master and the exceptions that held the previous answer, with `If-Match` and with no other change, no new `SEQUENCE`, no new `DTSTAMP` and no scheduling parameter, then SHALL read the event back and answer 502 `calendar_unreachable` with reason `response_not_kept` when the calendar does not hold the answer, showing the calendar's real answer. The server SHALL NOT answer invitations on a generic CalDAV server, SHALL NOT answer for a calendar it cannot prove to be the user's own, and SHALL refuse with 422 `event_not_editable` (reason `cannot_respond`) when the user is the organiser or not invited, their address is not known, the provider does not answer for them or edits are off. The route SHALL answer 200 with `{event, replaces?, series?, refetch}`, record `calendar_event.changed` with the action `responded` and the answer, without a name or address, count against the 600 changes an hour that all event writes share, and wake the user's other devices. A reply SHALL NOT be an operation, SHALL NOT be queued by a device and SHALL need a connection. The route SHALL answer 404 `calendar_sync_disabled` while the owner's switch is off, 403 `demo_restricted` on the public demo, 401 `unauthorized` without credentials, 403 `forbidden` to read and agent tokens, 404 `not_found` for a connection or event that is not the caller's, 403 `event_edits_off`, 403 `calendar_read_only`, 400 `scope_required` for an occurrence without `scope`, 400 `invalid_input`, 409 `event_changed`, 404 `event_gone`, 429 `rate_limited` and 502 `calendar_unreachable`.

#### Scenario: Accept on Outlook
- **WHEN** a user POSTs `{"rev": "...", "response": "accepted"}` for a meeting someone else organises on an Outlook calendar
- **THEN** the server sends Graph one `accept` request with `sendResponse: true`, the response holds the event with `guests.you: "accepted"` and a new `rev`, and the organiser receives the answer

#### Scenario: Answer without telling the organiser
- **WHEN** the user turns off "Tell the organizer" and chooses No for an Outlook meeting
- **THEN** the request carries `sendResponse: false`, Graph sends the organiser nothing, and the event shows the user as declined

#### Scenario: Reply for a series
- **WHEN** a user answers Yes for all events of an Outlook series
- **THEN** the answer is sent for the series master, the response carries `refetch: true`, and an occurrence the user answered on its own keeps its answer

#### Scenario: Reply on Google
- **WHEN** a user answers Maybe to an invitation on their own Google calendar
- **THEN** the object is written back with `If-Match` and only their `PARTSTAT` changed to `TENTATIVE`, then read back, and the response carries the event with `guests.you: "tentative"`

#### Scenario: Google did not keep the answer
- **WHEN** Google's copy still says `NEEDS-ACTION` after the write
- **THEN** the response is a 502 `calendar_unreachable` error with reason `response_not_kept`, the cache holds Google's answer, and the device shows "Google didn't keep your answer"

#### Scenario: Generic CalDAV server
- **WHEN** a user answers an invitation on a Radicale or Nextcloud calendar
- **THEN** the response is a 422 `event_not_editable` error with reason `cannot_respond`, nothing is written, and the popover offers no reply buttons

#### Scenario: The organiser
- **WHEN** a user answers a meeting they organise
- **THEN** the response is a 422 `event_not_editable` error with reason `cannot_respond`

#### Scenario: Another user's event
- **WHEN** a user answers an event id of a different account
- **THEN** the response is a 404 `not_found` error

#### Scenario: Unauthenticated
- **WHEN** the route is requested without credentials
- **THEN** the response is a 401 `unauthorized` error

#### Scenario: Agent or read token
- **WHEN** a read token or an agent token calls the route
- **THEN** the response is a 403 `forbidden` error

#### Scenario: Stale event
- **WHEN** the user answers from a `rev` the event no longer has
- **THEN** the response is a 409 `event_changed` error and nothing is written

### Requirement: Nothing tells a person without a confirmation
OpenTodo SHALL NOT cause a message to a person who is not a user of the server unless the user has just confirmed it and the provider is known to send it. A change, delete or create that the calendar answers with a message to guests SHALL be confirmed in a dialog that says how many guests are told, says that the calendar sends the update itself, and offers only what the provider honours (Microsoft: Save and tell guests, or Cancel; there SHALL be no "don't send" option, because Graph cannot skip the message). The dialog SHALL come after the choice of scope for a repeating event, SHALL write nothing before the answer, SHALL move focus to Cancel, SHALL close on Escape as Cancel and SHALL be announced to assistive technology. A change SHALL NOT be offered where the provider's behaviour is not known to be safe: organiser changes, guest changes and deletes on Google and on CalDAV servers SHALL stay locked, with a text that says the calendar may tell the guests and OpenTodo cannot say. A delete SHALL need the confirmation and then the 8-second wait of phase 1.

#### Scenario: Confirmation words
- **WHEN** the user changes the time of an Outlook meeting they organise with three guests
- **THEN** the dialog reads "Tell 3 guests?", says "Outlook sends them this update. You can't turn that off.", offers Save and tell guests and Cancel, and focus is on Cancel

#### Scenario: Cancel
- **WHEN** the user presses Cancel or Escape
- **THEN** the block returns to its place, no request is made and no message is sent

#### Scenario: Locked on Google with the reason
- **WHEN** the user opens a Google event they organise that has guests
- **THEN** the popover says the event has guests, that Google may tell them about a change and that OpenTodo can't say, and offers no edit

#### Scenario: Create with guests
- **WHEN** the user presses Create on a new Outlook event with two guests
- **THEN** a dialog asks "Invite 2 guests?" with Invite and save and Cancel, and nothing is sent before the answer

### Requirement: Events can carry a join link and an online meeting
The server SHALL read a conference address for an event of a connected calendar and cache it with the event: the Google `X-GOOGLE-CONFERENCE` property, a Microsoft event's `onlineMeeting.joinUrl`, and, in this order, an address in the `URL`, `LOCATION` and `DESCRIPTION` properties. A conference address SHALL be accepted only when its scheme is `https`, it has no user information and no control character, is at most 2,048 characters, and, unless a provider named it, its host is Google Meet, Zoom, Zoom for Government, Microsoft Teams or Webex. The server SHALL serve it as `conference` with its provider, SHALL NOT request it, preview it or follow it, and SHALL NOT log it. The popover, the editor and Today's agenda SHALL show a **Join** control for it, a link that opens the address in a new tab with no referrer, named by the service and showing its host, and SHALL NOT draw a control for an address that is not `https`. Events of subscribed (ICS) calendars SHALL have none. On a Microsoft calendar whose capabilities list Microsoft Teams, the editor and the create route SHALL be able to add an online meeting (`onlineMeeting: true` with the Teams provider), which SHALL NOT be offered to remove because Outlook ignores that change, and a refusal by the calendar SHALL be answered 400 `invalid_input` naming `onlineMeeting` with no event created. Google Meet SHALL NOT be created by OpenTodo. Offline, an existing link SHALL still open and adding a meeting SHALL be unavailable.

#### Scenario: Google Meet link
- **WHEN** a Google event carries `X-GOOGLE-CONFERENCE:https://meet.google.com/abc-defg-hij`
- **THEN** its popover shows "Join Google Meet" linking to that address, opening in a new tab, and the server made no request to it

#### Scenario: Zoom link in the location
- **WHEN** an event's location is "https://acme.zoom.us/j/123456789?pwd=abc"
- **THEN** the event is served with `conference.provider` "zoom" and the Join control shows the host

#### Scenario: Not allowed
- **WHEN** an event's description holds `http://zoom.us/j/1`, `https://zoom.us@evil.example/j/1`, `https://zoom.us.evil.example/j/1` or a `javascript:` address
- **THEN** none of them becomes a join link

#### Scenario: Join in Today's agenda
- **WHEN** Today lists a meeting that has not ended and has a Teams link
- **THEN** its row has a Join control, and a meeting that has ended has none

#### Scenario: Teams meeting
- **WHEN** a user creates an Outlook event with `onlineMeeting: true` and the calendar lists Microsoft Teams
- **THEN** the event holds a Teams meeting and the response carries its `conference`

### Requirement: Guest data is personal
The names and email addresses of an event's guests SHALL be held only in the server's calendar cache and the answers of the routes that need them. They SHALL NOT be written to the operation log, an export, a webhook, the activity feed, the audit log or the server log, SHALL NOT appear in an error message (which names a field and an index), SHALL NOT be stored on a device outside memory, and SHALL NOT be sent to the in-app assistant, to routines or to an assistant provider. An external agent SHALL receive them only as untrusted text. A backup, which copies the database, SHALL contain the cache as it contains event titles; the documentation SHALL say so. Guest data SHALL be deleted with the event, the calendar or the connection, and SHALL be read only for the user's own connections. The conference address SHALL be treated the same way, and SHALL NOT be logged.

#### Scenario: Not in logs
- **WHEN** a user answers an invitation, adds a guest with an invalid address and creates an event with guests
- **THEN** the server's log output and every error message contain none of the addresses or links involved

#### Scenario: Not on the device
- **WHEN** a popover with a guest list has been opened and closed
- **THEN** the device's database holds the guest summary of the event and no guest name or address

#### Scenario: Not for the assistant
- **WHEN** the in-app assistant plans a day with free-time and calendar tools
- **THEN** none of its tool results or provider requests contains a guest, an organiser or a conference

#### Scenario: Deleted with the connection
- **WHEN** a user deletes a connection
- **THEN** its cached guests are deleted with its events

## MODIFIED Requirements

### Requirement: Events can be changed and deleted in the connected calendar
While calendar sync is on, a signed-in user SHALL move, resize, rename and delete an event of a calendar of one of their connections, and change its location, description and busy or free, through `PATCH` and `DELETE /api/v1/calendar-sync/connections/{id}/events/{eventId}`, when the connection's "Edit events in OpenTodo" is on, the calendar's capability allows the change and the event is not locked. The `PATCH` body SHALL carry the `rev` the caller saw and any of `summary`, `start` and `end` together, `allDay`, `location` (empty to clear), `description` (plain text, empty to clear), `free`, `repeat`, `reminders`, `guests` (`{add, remove}`), `onlineMeeting` and `notifyGuests`, and `scope` (`this`, `following` or `all`) when the event is an occurrence of a repeating event; it SHALL answer 200 with the event as the calendar now holds it (with its new `id`, `rev` and `access`) and `replaces`, the id it had, and, for an occurrence of a repeating event, `series` (its `id` and the new `rev` the series' other occurrences now have) and `refetch`, true when other occurrences changed. A `description` SHALL be refused with 422 `event_not_editable` when the calendar holds a rich-text body that plain text would flatten. The `DELETE` SHALL carry `rev` and, for an occurrence of a repeating event, `scope`, and answer 204. A change or delete of an event whose `access.notifies` is true SHALL be refused with 400 `invalid_input` naming `notifyGuests` unless it carries `notifyGuests: true`, whatever fields it changes, and `notifyGuests: false` SHALL be refused because the provider cannot skip the message. Guests SHALL be added and removed only for such an event, with addresses validated on the server (`local@domain`, at most 254 characters, at most 100 guests, never the user's own or the organiser's), and an error SHALL name the field and the index and never the address. The server SHALL write to the remote calendar with a precondition on the event's version, keep every property of the remote event it does not change, never send an invitation or update to guests except as the confirmed organiser of a Microsoft meeting, whose calendar then tells the guests itself, refresh its cached copy, record an audit event without the title and wake the user's other devices. The route SHALL answer 404 `calendar_sync_disabled` while the owner's switch is off, 403 `demo_restricted` on the public demo, 401 `unauthorized` without credentials, 403 `forbidden` to read and agent tokens, 400 `scope_required` for an occurrence sent without `scope`, 404 `not_found` for a connection or event that is not the caller's, 403 `event_edits_off`, 403 `calendar_read_only`, 422 `event_not_editable`, 400 `invalid_input`, 404 `event_gone`, 429 `rate_limited` over 600 changes an hour, and 502 `calendar_unreachable` when the calendar fails. An event edit SHALL NOT be an operation: it SHALL NOT appear in the op log, the export or a backup.

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

#### Scenario: Change of a meeting the user organises, not confirmed
- **WHEN** a user PATCHes a new `start` and `end` for an Outlook meeting they organise that has three guests, without `notifyGuests`
- **THEN** the response is a 400 `invalid_input` error naming `notifyGuests`, nothing is written and no message is sent to the guests

#### Scenario: Change of a meeting the user organises, confirmed
- **WHEN** the same PATCH carries `"notifyGuests": true`
- **THEN** the server sends Graph one `PATCH` with `If-Match`, Outlook tells the three guests, the response holds the event with its new `rev`, and the audit event records `event_notified` and no name or address

#### Scenario: The calendar cannot skip the message
- **WHEN** a PATCH of such a meeting carries `"notifyGuests": false`
- **THEN** the response is a 400 `invalid_input` error naming `notifyGuests` and nothing is written

#### Scenario: Add and remove guests
- **WHEN** a user PATCHes `{"rev": "...", "notifyGuests": true, "guests": {"add": [{"email": "new@example.com", "role": "optional"}], "remove": ["old@example.com"]}}` on a meeting they organise
- **THEN** Graph receives the attendee collection with the existing guests and rooms kept, the new guest as optional and the removed guest left out, and the response carries the new guest count

#### Scenario: Invalid guest address
- **WHEN** a PATCH carries `"add": [{"email": "ana@example.com"}, {"email": "not an address"}]`
- **THEN** the response is a 400 `invalid_input` error naming `guests.add[1].email`, the message does not contain the address, and nothing is written

#### Scenario: Google organiser event
- **WHEN** a user PATCHes the title of a Google event they organise that has guests
- **THEN** the response is a 422 `event_not_editable` error with reason `has_guests`, no request reaches the calendar and no message is sent

### Requirement: Which events can be changed
An event SHALL be changeable only when it belongs to a connected calendar, its connection's "Edit events in OpenTodo" is on, its calendar's capability for the change is true, and it is a single event or an occurrence of a repeating event whose series OpenTodo can represent (the repeat rules and exceptions it reads and writes back without loss), without other attendees or, on a Microsoft calendar, a meeting the user organises (a CalDAV or Google event with guests, in the event or in any exception of its series, SHALL NOT be changeable), and with a version the calendar reported. Events of subscribed (ICS) calendars SHALL never be changeable. An event OpenTodo wrote for a task SHALL keep behaving as a task and SHALL NOT be served as an editable event. For every event the server serves, `access` SHALL say whether it may be updated or deleted and give the first unmet condition as `reason`: `edits_off`, `calendar_read_only`, `series_unsupported`, `has_guests`, `not_organizer`, `no_etag` or `refresh_pending`, and `notifies`, true when changing or deleting the event would tell its guests. A user who is a guest SHALL be told `not_organizer` before `has_guests` for every provider that says who the user is; `has_guests` SHALL be the reason for an event with guests on a Google or CalDAV calendar that the user organises or whose organiser or user's own address is not known. A Microsoft meeting the user organises SHALL be changeable and deletable with `notifies: true`; such a series SHALL be changeable for this event, and for all events only while it has no exceptions (`series_unsupported`), and never for this and following events (`following_unsupported`). A series SHALL be `series_unsupported` when its CalDAV object holds a `RANGE=THISANDFUTURE` exception, an `RDATE` or an `EXRULE`, more than one `RRULE`, a repeat rule OpenTodo cannot expand, more than one master event or events of another UID, more than 400 events or more than 256 KiB; and when a Microsoft occurrence does not name its series master. A cache read before this change that says `recurring` SHALL be served as `refresh_pending`. The write routes SHALL check every condition again.

#### Scenario: Repeating event
- **WHEN** a calendar holds a weekly event
- **THEN** every occurrence is served with `access: {"update": true, "delete": true}` and a `series`, and a PATCH or DELETE of one needs a `scope`

#### Scenario: Event with guests
- **WHEN** a CalDAV or Google event the user organises has an `ATTENDEE` property
- **THEN** it is served with `access.reason: "has_guests"`, a PATCH answers 422 `event_not_editable` and no invitation or update is sent

#### Scenario: Meeting organised by someone else
- **WHEN** an Outlook meeting has attendees and its `isOrganizer` is false
- **THEN** it is served with `access.reason: "not_organizer"` and cannot be changed or deleted, and the user can still answer it

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
- **WHEN** one exception of an otherwise guest-free CalDAV series has an `ATTENDEE`
- **THEN** every occurrence of the series is served with `access.reason: "has_guests"` and nothing about it can be changed

#### Scenario: Microsoft meeting the user organises
- **WHEN** an Outlook meeting with two guests has `isOrganizer` true
- **THEN** it is served with `access: {"update": true, "delete": true, "notifies": true}` and a PATCH needs `notifyGuests: true`

#### Scenario: Series with guests, all events with exceptions
- **WHEN** a user changes all events of an Outlook series they organise, with guests, whose master has an exception
- **THEN** the response is a 422 `event_not_editable` error with reason `series_unsupported` and nothing is written

#### Scenario: Series with guests, this and following
- **WHEN** a user changes this and following events of an Outlook series they organise, with guests
- **THEN** the response is a 422 `event_not_editable` error with reason `following_unsupported` and nothing is written

### Requirement: Edits are queued offline and converge on the calendar
A device SHALL apply an edit to its local copy of the event at once and queue one command per event in a local outbox, carrying the `scope` for an occurrence of a repeating event; later edits of the same event with the same scope SHALL be merged into that command, an edit with another scope SHALL NOT be merged and SHALL wait until the first command has been answered, a delete SHALL replace it, and an edit that returns the event to what it was SHALL remove it. A device SHALL send queued commands, one at a time and in order, when it is online, at start and when the calendar views refresh, and SHALL keep the user's pending edits visible when it refetches events. A device SHALL show a change with scope `this` at once, and a change with scope `following` or `all` only on the edited occurrence with a saving state until the server answers, and then refetch the shown range; the other occurrences SHALL NOT be moved on the device before the answer. A delete SHALL wait 8 seconds before it is sent, so Undo can cancel it. A change, delete or create that tells people (an event whose `access.notifies` is true, a reply to an invitation, a create with guests) SHALL NOT be queued: it SHALL be sent only after the user's confirmation and only while the device is online, SHALL leave no command in the outbox, and its controls SHALL be disabled offline with the reason; a notified delete SHALL still wait 8 seconds and SHALL be restored with the reason when the device is offline as the time passes. An edit the server refuses (409, 404, 403, 422 or 400) SHALL be removed from the outbox, the event restored and the reason shown. A command that fails while the device is online (a 5xx, a 429, an unreachable calendar, or no answer) SHALL NOT be retried automatically: it SHALL stay in the outbox as a visible failed state with Retry and Discard. A command made while the device is offline SHALL wait, shown as pending, and SHALL be sent when the device is back online. Signing out SHALL delete the outbox. Devices SHALL converge on the calendar's state: after a refusal or a success every device reads the event from the server.

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

#### Scenario: Notified change offline
- **WHEN** the device is offline and the user opens an Outlook meeting they organise, with guests
- **THEN** its fields and Save are disabled with the reason "Guests are told when you're online", and no command is queued

#### Scenario: Reply offline
- **WHEN** the device is offline and the user opens an invitation
- **THEN** the reply buttons are disabled with the same reason and nothing is queued

### Requirement: Events are edited in Day and Week
In Day and Week the blocks of events whose `access.update` is true SHALL be movable to another time or day, resizable by their upper and lower edge and droppable on the all-day lane and the hours, with the existing pointer, touch long-press and edge-scroll behaviour of task blocks, and Escape SHALL cancel a drag. A drop, a key press or a save on an occurrence of a repeating event SHALL first ask which events the change is for (this event, this and following events, all events), through a menu that Escape closes without changing anything; a drop on a single event SHALL NOT ask. A drop SHALL write the change as in the first requirement, keep the event's own time zone, keep a floating event floating, and offer Undo, which restores the previous values with an inverse change; the Undo of a change for this and following events SHALL NOT be offered, because it split the series. A drop, key press or save on an event whose `access.notifies` is true SHALL ask, after the scope when there is one, whether to tell its guests, in a dialog that says the calendar sends the update itself and offers only Save and tell guests or Cancel, SHALL write nothing before the answer, and SHALL NOT offer Undo afterwards, because Undo would tell the guests again. Event blocks SHALL NOT drop into a slot, SHALL NOT be completed, and SHALL NOT show drag affordances when they cannot be changed; a locked event SHALL say why in a tooltip and in its popover. The event popover SHALL let the user change the title, date and times and delete the event when allowed, without dragging, and SHALL show the same reasons when it cannot. A block with a queued change SHALL show it as saving. Month, Year, Plan and the all-day chips of other views SHALL stay read-only.

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

#### Scenario: Move a meeting with guests
- **WHEN** the user drags an Outlook meeting they organise, with three guests, to a new time
- **THEN** the block shows the new time with a saving state, a dialog asks "Tell 3 guests?" with Save and tell guests and Cancel, nothing is sent until the user chooses, Cancel puts the block back, and after Save and tell guests the toast has no Undo

#### Scenario: Delete a meeting with guests
- **WHEN** the user deletes a meeting they organise, with guests, and confirms
- **THEN** the event is hidden with a toast offering Undo for 8 seconds, nothing is sent until they pass, and the calendar then sends the cancellation

### Requirement: Agents and the command line can change events
The MCP server SHALL offer `list_calendar_events`, `create_calendar_event`, `update_calendar_event` and `delete_calendar_event` over the same rules as the routes: the read tool to every token that may read, the write tools only when the owner's `mcp.writes` switch and the user's switch are on and the caller uses an `agent` token, with the per-token write limits and the `agent.write` audit event. `list_calendar_events` SHALL also list the calendars an event can be created in. `create_calendar_event` SHALL take an optional `client_id` that makes a repeated call create one event. `update_calendar_event` and `delete_calendar_event` SHALL take `scope` (required for an occurrence, and their descriptions SHALL explain the three values), and `create_calendar_event` and `update_calendar_event` SHALL take `repeat`, `reminders`, `guests`, `notify_guests` and `online_meeting`. `respond_to_calendar_event` SHALL answer an invitation, and `list_calendar_events` SHALL list `guests` and `conference`, with names, addresses and links only inside untrusted text. Their errors SHALL use the route's codes, and the titles they return SHALL be marked as untrusted text. The in-app assistant SHALL NOT offer these tools, and routines SHALL NOT use the write tools; no result the in-app assistant receives SHALL carry a guest or a conference. `ot calendar events`, `ot calendar event add`, `ot calendar event edit` and `ot calendar event delete` SHALL call the routes with the user's token. `ot calendar event edit` and `ot calendar event delete` SHALL take `--scope this|following|all`, `add` and `edit` SHALL take `--repeat`, `--remind`, `--guest`, `--notify-guests` and `--online-meeting`, `edit` SHALL take `--no-guest`, and `ot calendar event respond` SHALL answer an invitation. The `ot` commands that change events SHALL need a full token.

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

#### Scenario: Agent answers an invitation
- **WHEN** an agent token with writes allowed calls `respond_to_calendar_event` with an event id, its `rev`, `response: "accepted"` and `scope: "all"` for a repeating invitation on a Microsoft calendar
- **THEN** the answer is sent for the whole series, the result holds the event with its new answer, and the audit log holds `agent.write` and `calendar_event.changed` with the action `responded`

#### Scenario: Agent changes a meeting without the flag
- **WHEN** an agent calls `update_calendar_event` for a meeting the user organises, with guests, without `notify_guests`
- **THEN** the result is the error `invalid_input` naming `notify_guests`, which says the calendar tells the guests and that it cannot be turned off, and nothing is written

#### Scenario: Guests are untrusted text
- **WHEN** an agent calls `list_calendar_events` with `include_guests`
- **THEN** each event's organiser, guests and join link are inside `untrusted_text` and the rest of the row holds only counts and answers

#### Scenario: Not for the in-app assistant
- **WHEN** the in-app assistant lists its tools
- **THEN** `respond_to_calendar_event` and the other calendar event tools are not among them

#### Scenario: Command line reply
- **WHEN** a user runs `ot calendar event respond <id> --maybe` with a full token
- **THEN** the answer is sent and the command prints the event and the new answer

#### Scenario: Command line without the flag
- **WHEN** a user runs `ot calendar event edit <id> --title "Planning"` for a meeting they organise, with guests, without `--notify-guests`
- **THEN** the command exits 2 saying the calendar will tell the guests, and sends nothing

### Requirement: Events can be created in a connected calendar
While calendar sync is on, a signed-in user SHALL create an event in a shown calendar of one of their connections through `POST /api/v1/calendar-sync/connections/{id}/events`, when the connection's "Edit events in OpenTodo" is on and the calendar's `create` capability is true. The body SHALL carry `clientId` (8 to 64 characters of letters, digits, `_` and `-`), `calendarUrl`, `summary` (1 to 500 characters), `start` and `end` as `{date, time?, tz?}` with the rules of the change route, and MAY carry `allDay`, `location` (at most 500 characters), `description` (plain text, at most 10,000 characters), `free`, `repeat` (the structured rule of the change route), `reminders`, `guests` (a list of `{email, role}`, accepted only on a calendar whose provider tells guests itself, with `notifyGuests: true`) and `onlineMeeting`. The server SHALL write a new event to the calendar that fails if the event already exists, with only the guests asked for (none unless `guests` is sent), no organizer written by OpenTodo and only the reminders asked for, store it in its cache, record an audit event without the title and wake the user's other devices, and SHALL answer 201 with `{event, clientId}`, the event being as the events route serves it (its `id`, `rev` and `access`). The route SHALL answer 404 `calendar_sync_disabled` while the owner's switch is off, 403 `demo_restricted` on the public demo, 401 `unauthorized` without credentials, 403 `forbidden` to read and agent tokens, 404 `not_found` for a connection that is not the caller's, 403 `event_edits_off`, 403 `calendar_read_only` when the calendar does not allow creating or answers 403, 400 `invalid_input` for a malformed body, a bad `clientId`, the time rules of the change route, or a `calendarUrl` that is not a shown calendar of the connection, 429 `rate_limited` over the 600 changes an hour that all event writes share, and 502 `calendar_unreachable` when the calendar fails. A created event SHALL NOT be an operation: it SHALL NOT appear in the op log, the export or a backup.

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

#### Scenario: Create with guests
- **WHEN** a user POSTs an event with `guests` `[{"email": "ben@example.com", "role": "required"}]`, `notifyGuests: true` and `onlineMeeting: true` to an Outlook calendar that lists Microsoft Teams
- **THEN** Graph receives one `POST` with the attendee and the online meeting, the invitation is sent by Outlook, and the response holds the event with its `conference`

#### Scenario: Create with guests on Google
- **WHEN** a user POSTs an event with `guests` to a Google calendar
- **THEN** the response is a 400 `invalid_input` error naming `guests`, nothing is written and no invitation is sent

#### Scenario: Online meeting a calendar cannot make
- **WHEN** a user POSTs `onlineMeeting: true` to an Outlook calendar that lists no provider, or whose `POST` Graph refuses for it
- **THEN** the response is a 400 `invalid_input` error naming `onlineMeeting` and no event is created

### Requirement: The full event editor
An event SHALL be created and edited in an editor with a title, an all-day switch, start and end date and time, a time zone, a calendar, a location, a plain-text description and a busy or free choice, a repeat and reminders, guests and a video call, plus Duplicate, Convert to task and Delete. It SHALL be a panel in the shell's side slot from 1024 px and a bottom sheet on a phone, SHALL load only when opened, and SHALL be reachable from the event popover's "All details", a double-click on an event block, an all-day chip or an event row, and the add flows. Times SHALL be shown and entered in the selected zone, which is the viewer's zone for a new event and the event's own zone for an existing one, except that a UTC event is edited in the viewer's zone and a floating event stays floating until a zone is picked; a second line SHALL show the time in the viewer's zone when it differs. Changing the zone SHALL keep the wall-clock times. An all-day event SHALL show its end date as the last day it covers. The calendar SHALL be chosen when creating and SHALL be read-only when editing, an event SHALL NOT be moved to another calendar, and the server SHALL NOT accept a calendar change. Save SHALL send only the fields the user changed. Saving or deleting an occurrence of a repeating event SHALL ask which events the change is for before anything is sent, offering only the scopes the change allows (a change of the repeat is for this and following events or all events). The repeat SHALL offer none, every day, every weekday, every week on chosen days, every month on a day or on a weekday of the month, every year, and a custom interval, ending never, on a date or after a number of times; a rule the editor cannot represent SHALL be shown as "Custom repeat", read-only, while the other fields stay editable. Reminders SHALL be a list of times before the start, one for a Microsoft calendar, and reminders the editor cannot represent SHALL be kept and counted. The editor SHALL show the organiser and the guests with their answers; where the user organises the event and the provider tells guests itself it SHALL add and remove guests by email (free text, validated) with a role, and otherwise the list SHALL be read-only. It SHALL offer an online meeting only when the calendar can make one and the event has none, and SHALL say it cannot be removed afterwards. Save SHALL ask whether to tell the guests when `access.notifies` is true, and Create SHALL ask before inviting guests. Closing the editor with unsaved changes SHALL ask first. Events whose `access.update` is false SHALL keep the read-only popover with its reason. The editor SHALL work offline except for loading the description.

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

#### Scenario: Guests in the editor
- **WHEN** the user opens a meeting they organise on Outlook, types "ben@example.com" in Add guest, chooses the create row and presses Save
- **THEN** a dialog asks "Tell 4 guests?", and after Save and tell guests the request carries `guests.add` with that address and `notifyGuests: true`

#### Scenario: Guests are read-only on Google
- **WHEN** the user opens a Google event with guests
- **THEN** it opens in the read-only popover with the guest list and the reason, and the editor offers no guest field

#### Scenario: Teams meeting
- **WHEN** the user switches on "Add a Teams meeting" in the editor of an Outlook event and saves
- **THEN** the request carries `onlineMeeting: true`, the event then has a Join button, and the switch is on and cannot be turned off

### Requirement: Event details are read when an editor opens
`GET /api/v1/calendar-sync/connections/{id}/events/{eventId}` SHALL read the event from the calendar and answer `{event, replaces?, description, descriptionEditable, guests?, conference?, onlineMeeting?}`; `guests` SHALL be read from the calendar now (the organiser, each guest with role and answer, the count, whether the list was cut at 100, and whether the user can reply and edit) and SHALL be absent for an event without other attendees. `description` SHALL be plain text and SHALL NOT appear in the events route, the devices' cache or the audit log. When the calendar holds a newer version than the server's cache the server SHALL store it, wake the user's other devices and answer with the current row and `replaces`. `descriptionEditable` SHALL be false for a Microsoft event whose body is rich text with content; such a description SHALL be shown as plain text and a change of it SHALL be refused with 422 `event_not_editable` (reason `description_rich`). The route SHALL need the connection's "Edit events in OpenTodo" on, SHALL work for locked events, and SHALL answer 401, 404 `calendar_sync_disabled`, 404 `not_found`, 403 `event_edits_off`, 404 `event_gone` (the calendar no longer has the event; the cached copy is dropped) and 502 `calendar_unreachable` as the other event routes do. A device SHALL NOT send a description it could not load.

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

#### Scenario: Guests with the details
- **WHEN** a user opens the editor of a meeting with three guests
- **THEN** the response carries the organiser and the three guests with their answers, read from the calendar, and the device holds them in memory only

### Requirement: Convert an event to a task
The editor and the event popover SHALL offer "Convert to task…" for any event. It SHALL ask once, with a switch "Delete the event from <calendar>" that is on by default when the user may delete the event and it has no other attendee, and off and disabled otherwise (for an occurrence of a repeating event it deletes that occurrence only), remembered per device, and with "Create task". Creating SHALL make a task through the normal operations from the event's values as shown in the editor: its title; its description followed by the location when there is one as notes; for a timed event its start as the due, with time and time zone, and its length as the estimate; for an all-day event its last day as the due and, when it covers several days, its first day as the start of a span. The task SHALL be unplaced, as any new task without a project. When the switch is on the event SHALL be deleted with the usual 8 second delay. One toast SHALL offer one Undo that moves the task to the Trash and cancels the delete, and after the 8 seconds the delete SHALL be final. The task SHALL work offline, and a queued delete SHALL wait in the outbox. On an encrypted account the task's content SHALL be encrypted as any task's.

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

#### Scenario: Meeting the user organises
- **WHEN** the user converts an Outlook meeting they organise, with guests
- **THEN** the switch is off and disabled, the task is created from the event shown, and the event is not deleted and no cancellation is sent
