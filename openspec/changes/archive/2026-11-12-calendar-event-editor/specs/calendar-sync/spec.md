## ADDED Requirements

### Requirement: Events created in OpenTodo stay events
An event created through the event editor, the agent tools or the CLI SHALL be an ordinary event of its calendar: it SHALL carry no OpenTodo task marker, SHALL NOT be linked to a task, and SHALL NOT be changed, moved or deleted by a sync cycle's task logic. A cycle SHALL read it like any event of a shown calendar. Events SHALL be created only in calendars the connection shows and whose `create` capability is true, and creating SHALL NOT change how tasks are written to the target calendar, including when the target calendar is the one the event is created in. The connection's "Edit events in OpenTodo" SHALL gate creating as it gates changing, and turning it off SHALL stop creates at once, a create already queued on a device being refused with `403 event_edits_off`.

#### Scenario: Created in the target calendar
- **WHEN** a user creates an event in the calendar that receives their tasks
- **THEN** the next cycle reads it as an event, writes no task for it, leaves it alone while it syncs tasks, and the task events beside it are unchanged

#### Scenario: Not a task
- **WHEN** a user deletes in another app an event created in OpenTodo
- **THEN** no task is unscheduled or changed, and the event leaves every device after the next refresh

#### Scenario: Edits turned off with a create queued
- **WHEN** the user turns "Edit events in OpenTodo" off while a device holds a queued create
- **THEN** the create is refused with `403 event_edits_off` when the device sends it, the device removes the pending event and says editing is off

#### Scenario: A calendar that is not shown
- **WHEN** a connection stops showing a calendar and a device sends a create for it
- **THEN** the response is a 400 `invalid_input` error naming `calendarUrl` and nothing is written
