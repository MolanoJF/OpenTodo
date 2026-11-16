## ADDED Requirements

### Requirement: Account colors on this device
The calendar SHALL give every connected account a stable default palette color and SHALL render all of its events with that color. Calendar sync settings SHALL offer a keyboard-accessible account color choice and an Automatic reset. Independent ICS feeds SHALL retain their configured colors. The choice SHALL persist offline on this device, react immediately to local changes and survive a provider refresh. Device choices SHALL NOT sync across devices or write to a provider. Task colors SHALL continue to follow the existing Colour by preference, defaulting to the project's synced color.

#### Scenario: Shared account color
- **WHEN** two displayed calendars belong to the same connection
- **THEN** their events share the account's color in timed blocks, all-day chips and agenda/month rows

#### Scenario: Offline choice survives refresh
- **WHEN** the person chooses a color while offline and later refreshes calendar data
- **THEN** the device retains that account color without changing provider data or other devices

#### Scenario: Project tasks
- **WHEN** Colour by Project is selected and a project has a color
- **THEN** its calendar tasks share that project color and existing project ops synchronize changes normally

### Requirement: Colored event surfaces
Busy events SHALL use a pastel hue fill, matching readable text and a strong hatched leading edge in light and dark themes. Events SHALL retain their dot and edge texture to distinguish them from tasks without color. Free events SHALL retain their dashed outlines. Existing focus, selection and failure indicators SHALL remain visible.

#### Scenario: Color-independent distinction
- **WHEN** a busy event and a task appear side by side in the same hue
- **THEN** the event's hatched edge and dot distinguish it from the task's solid edge
