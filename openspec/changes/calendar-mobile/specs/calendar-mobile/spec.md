## ADDED Requirements
### Requirement: Stable phone calendar and detail layouts
The phone shell SHALL hide list-only add and tab controls on event detail routes, retain Back navigation, and constrain content scrolling to its viewport without horizontal page movement. Phone task and event footers SHALL remain in normal document flow without covering fields. Calendar controls and previews SHALL fit narrow phone widths while preserving existing event permissions and actions. Desktop behavior SHALL be preserved.
#### Scenario: New event on a phone
- **WHEN** a phone opens a new event from the calendar
- **THEN** the editor offers Back and Add event without a floating add button or bottom tabs overlapping it
#### Scenario: Reading a long task
- **WHEN** a phone scrolls a task with long content
- **THEN** the page stays within its horizontal bounds and the composer does not cover the content
