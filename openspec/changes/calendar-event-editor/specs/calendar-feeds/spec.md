## ADDED Requirements

### Requirement: Event descriptions are read when needed
The events route, the devices' event cache and the agent and CLI event lists SHALL NOT carry an event's description. A description SHALL be read from the connected calendar only when an editor opens for the event (`calendar-event-edits`), kept in memory by the editor and discarded with it. An ICS feed event's description SHALL NOT be read or shown. Location and busy or free SHALL continue to be served with every event.

#### Scenario: Lists carry no notes
- **WHEN** a device requests the events of a week in which an event has a long description
- **THEN** the response holds the event's title, location and free flag and no description, and the device's cache holds none

#### Scenario: Offline
- **WHEN** the device is offline and the user opens an event
- **THEN** the event's title, times, location and busy or free are shown from the cache and its description is unavailable until the device is online

#### Scenario: Subscribed calendar
- **WHEN** the user opens an event of a subscribed (ICS) calendar
- **THEN** no description is requested or shown
