## ADDED Requirements

### Requirement: Full event detail page
Opening event details or editing SHALL show a full-width responsive page with a prominent title and description, a schedule/details column with existing icon vocabulary, and visible save/close actions. Narrow screens SHALL stack the content. Existing dirty-close confirmation, recurrence scope and guest notification checks SHALL remain effective. Read-only events SHALL never gain writes. Offline editing SHALL retain the existing event outbox behavior and convergence rules.

#### Scenario: Edit and return
- **WHEN** a person opens an editable event's full details from its calendar preview
- **THEN** the full page opens, edits use existing commands, and closing returns to the same calendar without changing its range or scroll position

#### Scenario: Unsaved close
- **WHEN** the person closes a changed event page
- **THEN** they must choose whether to discard or keep editing

### Requirement: Read-first calendar previews
A single click on a calendar event or task block SHALL show a clean preview without immediately entering an edit form. Event previews SHALL show available date/time, zone, recurrence, guest/call, location, description, calendar and reminder data with icons and an explicit full-detail action. Task previews SHALL show cached schedule, project and notes with a full-task action. Existing keyboard and double-click full-open behavior SHALL remain. Opening previews SHALL NOT write tasks or events. Cached facts SHALL work offline; missing remote details SHALL be labelled unavailable. Existing data convergence SHALL remain unchanged.

#### Scenario: Task preview
- **WHEN** a task block is clicked once
- **THEN** its preview opens and offers the full task page, without modifying the task

#### Scenario: Remote detail unavailable
- **WHEN** a preview cannot fetch a remote event description or reminders
- **THEN** cached facts remain visible and unavailable remote detail is clearly stated
