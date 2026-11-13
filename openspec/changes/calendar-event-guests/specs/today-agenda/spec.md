## ADDED Requirements

### Requirement: Join from Today's agenda
An event in Today's agenda that has a conference address (`calendar-event-edits`) and has not ended SHALL show a **Join** control at the end of its row. The control SHALL be a link that opens the address in a new tab with no referrer, SHALL be named by the service ("Join Google Meet") with the event's title as its description, SHALL be reachable with Tab and operable with Enter, and SHALL NOT make the row wider than its layout, change the row's popover or register a shortcut. An event that has ended and an event without a conference SHALL have none. The control SHALL need no request: it is drawn from the device's cached event, and it SHALL NOT be drawn for an address that is not `https`.

#### Scenario: Meeting in progress
- **WHEN** Today lists a meeting that started 10 minutes ago with a Zoom link
- **THEN** its row shows Join, and pressing it opens the link in a new tab

#### Scenario: Meeting over
- **WHEN** the meeting's end has passed
- **THEN** its row shows no Join

#### Scenario: Offline
- **WHEN** the device is offline
- **THEN** Join still shows for a cached event and opening it is the browser's business, with no request made by OpenTodo

#### Scenario: Keyboard
- **WHEN** the user tabs through Today's agenda
- **THEN** each Join control is reached after its row, announced as "Join Google Meet", and Enter opens it
