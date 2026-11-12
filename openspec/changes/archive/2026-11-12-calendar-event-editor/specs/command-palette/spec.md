## ADDED Requirements

### Requirement: New event from the palette
While the user has a writable calendar (a shown calendar of a connection with "Edit events in OpenTodo" on whose `create` capability is true), the palette SHALL list the command "New event". Running it SHALL open the event editor in create mode on today at the next half hour (`:00` or `:30`) that holds no task block or event, or on tomorrow at 09:00 when none is left before midnight, in the default calendar for new events, showing the Day view when the app is not in Day or Week and showing calendars when the device hides them. The command SHALL NOT be listed when there is no writable calendar. It SHALL work offline and SHALL NOT create anything until the user saves.

#### Scenario: New event at 10:07
- **WHEN** it is 10:07, nothing is planned at 10:30 and the user runs "New event"
- **THEN** the Day view shows today and the editor opens for 10:30 to 11:00 in the default calendar with the title focused

#### Scenario: Late in the day
- **WHEN** it is 23:40 and the user runs "New event"
- **THEN** the editor opens for tomorrow at 09:00

#### Scenario: Busy half hour
- **WHEN** an event runs from 10:30 to 11:00 and it is 10:07
- **THEN** the editor opens for 11:00

#### Scenario: No writable calendar
- **WHEN** the user has no connection that allows edits and types `new event`
- **THEN** the command is not listed
