## ADDED Requirements

### Requirement: Phone calendar uses available space
The phone calendar SHALL place its title beside its view and navigation controls, and its scroller SHALL extend to the bottom navigation with the add button floating above it. Content SHALL remain reachable above the floating button and SHALL NOT overflow horizontally.

#### Scenario: View the end of the calendar
- **WHEN** the user scrolls a phone calendar to its end
- **THEN** the last calendar content can be reached above the add button without a permanent blank strip under the grid

### Requirement: Compact phone event editor
The phone event page SHALL show a compact title and schedule, expandable optional fields with current-value summaries, and a header submit action linked to the existing form. Validation and read-only states SHALL remain enforced, and the save region SHALL use the page background.

#### Scenario: Edit event times and save
- **WHEN** the user expands the schedule, changes valid date/time values and submits from the header
- **THEN** the existing save path receives those values and the normal provider confirmations and dirty-close guard still apply
