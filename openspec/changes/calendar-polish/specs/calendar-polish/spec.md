## ADDED Requirements

### Requirement: Routed event details
Event details SHALL open in the app shell as a page with its own address, without a modal scrim or focus trap. Closing SHALL return to the originating calendar and its scroll position. Direct links SHALL read cached events. Dirty edits SHALL guard navigation. Existing write permissions, offline event outbox and convergence SHALL remain unchanged.

#### Scenario: Open and return
- **WHEN** an event opens from a calendar preview
- **THEN** the address becomes an event route and Close returns to the calendar without a modal

### Requirement: Readable calendar presentation
Consecutive timed cards SHALL have a visible gap without changing their stored times. Concurrent cards SHALL use staggered, overlapping widths and expose a card on hover or selection. Titles SHALL align at the top, and available height SHALL permit wrapping and an end time. Weekend columns SHALL be subtly shaded in both themes. Previews SHALL remain compact with a clear full-detail action.

#### Scenario: Consecutive and concurrent events
- **WHEN** one event ends when another starts and a third overlaps them
- **THEN** consecutive card edges have breathing room and concurrent cards use layered widths rather than only equal narrow columns

### Requirement: Calendar appearance preferences
The calendar SHALL expose Monday/Sunday week-start choices using the existing synced preference, with normal offline op convergence. Off-hours shading SHALL default off and be optional on this device, using the configured start/end hours without hiding times. Color options SHALL have icons and descriptions like other menu items.

#### Scenario: Off-hours shading
- **WHEN** shading is disabled
- **THEN** all hours remain unshaded except subtle weekend/background distinctions and no tasks or events are changed
