## MODIFIED Requirements

### Requirement: Add with the pointer in Day and Week
A double-click on an empty part of the Day or Week grid (no block, event, deadline chip or frame title under the pointer) SHALL open the add dialog with that date and the slot's 15-minute start, and a double-click in a day's unscheduled lane SHALL open it with the date only. A right-click on the same places, or a long press of 500 ms on touch, SHALL open a menu at that place with Add task here… and the entries other calendar features add there, including Add event here… while the user has a writable calendar (`calendar-event-edits`). A fine pointer SHALL see a faint highlight of the 15-minute slot under it. While the user has a writable calendar, a press and drag over an empty part of a day column with a mouse or pen SHALL size a new item from 15 minutes up and, on release, open the add dialog (or the event editor, when the device's last choice was an event) with the start and the length as the task's estimate; without a writable calendar a drag over an empty place SHALL do nothing. `q` in Day and Week SHALL open the same dialog for the focused day without a time, so no addition needs a pointer. The task SHALL be created locally with no network round trip and synced like any new task.

#### Scenario: Double-click a slot
- **WHEN** the user double-clicks Tuesday 14:30 in the Week, types "Call the bank" and presses Enter
- **THEN** a task "Call the bank" due Tuesday at 14:30 appears as a block there at once

#### Scenario: Context menu
- **WHEN** the user right-clicks Wednesday 09:00
- **THEN** a menu opens at the pointer with Add task here…, and choosing it opens the dialog with Wednesday 09:00

#### Scenario: Long press on a phone
- **WHEN** on a touch screen the user long-presses an empty slot of the Day view
- **THEN** the same menu opens as a bottom sheet, while a long press on a task block keeps its existing behaviour

#### Scenario: Keyboard
- **WHEN** a Week is shown with a block on Thursday selected and the user presses `q`
- **THEN** the add dialog opens for Thursday without a time, and a typed "at 4pm" sets 16:00

#### Scenario: Offline
- **WHEN** the device is offline and the user double-clicks a slot and adds a task
- **THEN** the block appears at once and the task is uploaded on the next sync

#### Scenario: Menu with a writable calendar
- **WHEN** the user has a writable calendar and right-clicks Wednesday 09:00
- **THEN** the menu lists Add task here… and Add event here…, and Add event here… opens the event editor with Wednesday 09:00

#### Scenario: Sweep a task
- **WHEN** the user has a writable calendar, the device's last choice was a task, and the user drags from 14:00 to 15:30 on an empty part of Wednesday
- **THEN** the add dialog opens due Wednesday at 14:00 and a typed title creates a task with an estimate of 1 hour 30 minutes

#### Scenario: No writable calendar
- **WHEN** the user has no writable calendar and drags over an empty part of the grid
- **THEN** nothing opens and the menu, the dialog and the double-click are exactly as before
