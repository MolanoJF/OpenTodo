## ADDED Requirements

### Requirement: Series edits never touch task events
A change to a repeating event, including the new series a `following` change creates and the end it writes to the old one, SHALL be an ordinary event of its calendar: it SHALL carry no OpenTodo task marker, SHALL NOT be linked to a task, and a sync cycle SHALL read it like any event of a shown calendar and SHALL NOT change, move or delete it with its task logic, including in the calendar that receives the user's tasks.

#### Scenario: Split in the target calendar
- **WHEN** a user splits a repeating event in the calendar that receives their tasks
- **THEN** the next cycle reads both series as events, writes no task for them, and the task events beside them are unchanged

#### Scenario: Series moved elsewhere
- **WHEN** a repeating event is changed in another calendar app after OpenTodo cached it
- **THEN** the next cycle replaces its cached occurrences with the calendar's and no task is created or changed
