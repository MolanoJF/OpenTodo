## MODIFIED Requirements

### Requirement: Grid preferences
Start hour, end hour, day length, "Count meetings in capacity" and "Second time zone" (OpenSpec `calendar-time-zones`) SHALL be per-device preferences editable in Settings › Calendar, with defaults that need no setup (the option off, no second zone), and SHALL not be synced. The option SHALL be shown only while the device knows at least one subscribed or connected calendar; a stored value SHALL be kept while it is hidden. A missing or invalid stored value SHALL read as its default.

#### Scenario: Change day length
- **WHEN** the user sets the day length to 6 hours in Settings
- **THEN** capacity markers on this device use 6 hours and other devices are unaffected

#### Scenario: Turn meetings on for this device
- **WHEN** the user turns on "Count meetings in capacity" on the laptop
- **THEN** the laptop's Day, Week and Month count busy event minutes and the phone's do not

#### Scenario: No calendar, no option
- **WHEN** the device knows no subscribed or connected calendar
- **THEN** Settings › Calendar shows no "Count meetings in capacity" row and capacity counts tasks only

#### Scenario: Second zone is per device
- **WHEN** the user sets a second time zone on the laptop
- **THEN** the laptop's Day and Week show it and the phone's grid is unchanged
