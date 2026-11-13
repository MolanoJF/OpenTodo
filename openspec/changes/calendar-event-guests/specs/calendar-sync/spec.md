## ADDED Requirements

### Requirement: Invitations are answered and changed only where the provider is known to handle them
OpenTodo SHALL read the organiser, the guests, each guest's answer and the user's own answer of every event of a connected calendar, and SHALL decide what it may write for guests from the provider alone. **Microsoft:** the user's answer is the event's own response status, a meeting is the user's to organise when `isOrganizer` is true, answers use the accept, tentatively-accept and decline actions with `sendResponse`, and an organiser's change, guest change or delete is allowed after the confirmation and tells the guests through Outlook, with no new permission beyond `Calendars.ReadWrite`. **Google:** the user's address SHALL be taken from the account's principal and matched only for the account's own calendar; an answer SHALL change only the user's own `PARTSTAT` and SHALL be read back; an answer SHALL stay off until a read-back test against the owner's calendar has been recorded; organiser changes, guest changes and deletes of events with guests SHALL NOT be written, because Google's CalDAV documentation does not say whether guests are told. **Other CalDAV servers:** guests SHALL be read and nothing SHALL be written for them. A cycle that reads a calendar SHALL NOT change an event with guests, and a task's event SHALL carry no guests. Reading guests SHALL need no request beyond the one the cycle already makes.

#### Scenario: Microsoft invitation
- **WHEN** an Outlook calendar holds a meeting someone else organises with five attendees
- **THEN** the cache holds the organiser, the five guests with their answers and the user's own answer, and the user can answer it from OpenTodo

#### Scenario: Google invitation with answers off
- **WHEN** a Google calendar holds an invitation and the answer capability is off
- **THEN** the guests and the user's answer are shown, no reply buttons are offered, and no write is made to the calendar

#### Scenario: Shared Google calendar
- **WHEN** the user shows a colleague's Google calendar and opens an event the colleague was invited to
- **THEN** the guests are shown, the user is not marked as a guest and no reply is offered

#### Scenario: Radicale
- **WHEN** a Radicale calendar holds an event with `ORGANIZER` and three `ATTENDEE` properties
- **THEN** the guests are shown, the event is locked with reason `has_guests`, and no request other than a read reaches the server

#### Scenario: Task events have no guests
- **WHEN** a task is written to the target calendar
- **THEN** its event carries no attendee and no organiser

## MODIFIED Requirements

### Requirement: Calendar capabilities
Each calendar of a connection SHALL report `caps`, `{create, update, delete, onlineMeeting?}`, derived from the permissions the account grants on that calendar: for a CalDAV or Google calendar its `current-user-privilege-set` (`bind` for create, `write-content` for update, `unbind` for delete, `write` or `all` for all three), for a Microsoft calendar its `canEdit` for all three. For a Microsoft calendar `onlineMeeting` SHALL be true when the calendar's `allowedOnlineMeetingProviders` lists Microsoft Teams, false when it does not, and absent on a calendar saved before it until its next discovery. A calendar saved before capabilities existed SHALL be reported with all three equal to the opposite of `read_only` until it is discovered again. A calendar whose account refuses a write with 403 SHALL be reported with all three false until the next discovery. Capabilities SHALL be served in `GET /api/v1/calendar-sync/connections` and, for shown calendars, on the managed subscriptions of the events route.

#### Scenario: Read-only calendar
- **WHEN** a Google account shares a calendar with the user as "see all event details"
- **THEN** the calendar is reported with `caps` all false and none of its events is editable from OpenTodo

#### Scenario: Delete forbidden
- **WHEN** a CalDAV calendar's privilege set holds `write-content` and `bind` but not `unbind`
- **THEN** its caps are `{create: true, update: true, delete: false}`, its events can be moved and renamed but not deleted, and the popover has no Delete

#### Scenario: Calendar saved before capabilities
- **WHEN** a server upgrades and a calendar has `read_only: false` and no caps
- **THEN** it is reported as `{create: true, update: true, delete: true}` until its next discovery

#### Scenario: The server says no
- **WHEN** the remote answers 403 to a write on a calendar reported writable
- **THEN** the write fails with `403 calendar_read_only`, the calendar's caps become all false and its events stop being draggable on the next refresh

#### Scenario: Calendar that cannot make online meetings
- **WHEN** a personal Outlook calendar lists no online meeting provider
- **THEN** it is reported with `caps.onlineMeeting: false` and the editor offers no Teams meeting for its events
