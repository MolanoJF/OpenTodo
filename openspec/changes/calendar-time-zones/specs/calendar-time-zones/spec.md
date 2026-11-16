## ADDED Requirements

### Requirement: Second time zone in the Day and Week grid
The Day and Week grids SHALL show, when the device's "Second time zone" is set to a zone other than the device's own, a second hour column beside the existing one. Each row of the second column SHALL read the wall time, in the second zone, of the instant the same row names in the device zone on the first date shown, in the user's time format, so that half-hour and 45-minute offsets read as `14:30` and `14:45` and a daylight-saving change in either zone is labelled by the real instant. A short zone code, the first three letters of the zone's city in capitals (`UTC` for UTC), SHALL be shown at the top of each of the two columns while the second zone is on. The first column SHALL keep following the device zone. With no second zone, or the device's own, the grid SHALL be exactly as before. The column SHALL be drawn from local data and work offline. The phone's Week agenda, Month, Year and Plan SHALL NOT show it.

#### Scenario: Off by default
- **WHEN** a device never set a second zone and opens Day
- **THEN** the grid has one hour column and its sticky header reads exactly as before

#### Scenario: Bogotá beside Berlin
- **WHEN** the device zone is Europe/Berlin, the second zone is America/Bogota and Day shows 2026-11-13
- **THEN** a second column reads `03:00` beside the `09:00` row, `BOG` heads it and `BER` heads the first column

#### Scenario: Half-hour zone
- **WHEN** the device zone is UTC, the second zone is Asia/Kolkata and Day shows 2026-11-13
- **THEN** the `09:00` row reads `14:30` in the second column, and in 12-hour format `2:30 PM`

#### Scenario: Forty-five minute zone
- **WHEN** the device zone is UTC and the second zone is Asia/Kathmandu
- **THEN** the `09:00` row reads `14:45`

#### Scenario: Daylight saving in the second zone
- **WHEN** the device zone is UTC, the second zone is America/New_York and Day shows 2026-03-08, the day the United States moves its clocks forward at 07:00 UTC
- **THEN** the `06:00` row reads `01:00` and the `08:00` row reads `04:00`

#### Scenario: Daylight saving in the device zone
- **WHEN** the device zone is Europe/Berlin, the second zone is UTC and Day shows 2026-03-29, when 02:00 does not exist
- **THEN** the `01:00` row reads `00:00`, the `02:00` row reads `01:00` and the `03:00` row reads `01:00`

#### Scenario: Same zone as the device
- **WHEN** the second zone is the device's own zone
- **THEN** no second column is drawn

#### Scenario: Offline
- **WHEN** the device is offline
- **THEN** the second column is drawn from the device's stored preference and the clock

### Requirement: Second time zone is readable and fits the phone
The second column's labels SHALL be decorative like the first column's, hidden from assistive technology, and the second zone SHALL be stated once in text for assistive technology in the grid's header, naming the zone's city and its offset ("Times also shown in Bogota (UTC−5)"). The column SHALL use the same colour tokens as the first column, add no interactive element, and keep a fixed width so that at 360 px the Day grid keeps at least 260 px for its day column and the page does not scroll sideways.

#### Scenario: Screen reader
- **WHEN** a screen reader user moves through the Day header with a second zone on
- **THEN** one text states "Times also shown in Bogota (UTC−5)" and none of the 23 second-column labels is announced

#### Scenario: Phone width
- **WHEN** Day is opened at 360 px with a second zone on
- **THEN** the two hour columns take 96 px, the day column has the rest, and neither the page nor the grid scrolls sideways

### Requirement: Second time zone setting
Settings › Language and region › Calendar grid SHALL offer "Second time zone", off by default, chosen from the platform's time zones with the same picker as an event's time zone, with an "Off" choice listed first. The choice SHALL be a per-device grid preference, SHALL take effect at once on the device, SHALL NOT be synced and SHALL NOT create an operation. A stored zone the platform does not know SHALL read as off.

#### Scenario: Choose a zone
- **WHEN** the user opens the Second time zone picker, types "Bog" and chooses Bogotá
- **THEN** Day and Week on this device show the second column at once and other devices are unchanged

#### Scenario: Turn it off
- **WHEN** the user chooses Off
- **THEN** the second column disappears and the stored zone is empty

#### Scenario: Unknown stored zone
- **WHEN** the device holds a second zone of `Mars/Olympus`
- **THEN** the grid draws no second column and the stored value is read as off
