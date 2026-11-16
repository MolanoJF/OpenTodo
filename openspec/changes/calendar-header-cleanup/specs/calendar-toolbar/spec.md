## ADDED Requirements

### Requirement: Compact calendar navigation
The calendar SHALL offer a checked view dropdown, a Number of days step for 2–6 consecutive days, a separate Today button and previous/next arrows. Custom day counts SHALL persist on the device, preserve the anchor date and page by the visible range. Normal Week SHALL honor the configured week start. Completed-task and other-calendar visibility SHALL appear in More actions at all viewport widths.

#### Scenario: Choose a custom range
- **WHEN** the user chooses 3 days from Number of days
- **THEN** three consecutive dates beginning at the anchor are shown, the trigger reads 3 days, and Next advances three days

### Requirement: Quiet time grid
Day and Week SHALL draw hour rules without quarter-hour rules or weekend shading. Day headings SHALL be centered without capacity summaries. Empty date-only lanes SHALL have no Unscheduled placeholder or dashed outline; existing date-only content and drag targets SHALL remain usable. Week headings SHALL show the month, year and ISO week number.

#### Scenario: Empty all-day region
- **WHEN** no date-only task or event is present
- **THEN** no Unscheduled placeholder is displayed and timed scheduling remains available
