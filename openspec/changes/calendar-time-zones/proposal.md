# A second time zone in Day and Week

## Why

People who plan across zones (a team in Bogotá, a client in Berlin, family in Kolkata) keep doing sums in their heads: "my 15:00 is 21:00 there". The calendar already knows both zones. Events and tasks carry a zone (`calendar-event-editor`), the editor says what a time is in the viewer's zone, and the Day and Week grid has one hour gutter. Notion Calendar shows a second hour column for exactly this, and nothing else changes. The grid should do the same, off until someone wants it.

## What Changes

- **A second hour column** beside the existing one in the Day and Week grids, with a short zone code ("BOG", "BER") at the top of each column. The first column keeps following the device zone as today; the second shows what time it is in the chosen zone at each row.
- **A setting, "Second time zone"**, off by default, in Settings > Language and region > Calendar grid, chosen with the same zone Combobox as the event editor and with an "Off" choice. It is a per-device grid preference like the grid hours (design D1): no op, no server change.
- **Correct clocks**, not offsets added to hours: half-hour and 45-minute zones (Asia/Kolkata, Asia/Kathmandu) read "14:30" and "14:45", and a daylight-saving change in either zone is labelled by the real instant (design D3).
- **Accessible and phone-safe**: the hour labels stay decorative like the first column's, the zone is stated once in text for screen readers, and at 360 px the page still does not scroll sideways (design D5).

## Out of scope

- **Swap** (making the second zone the one the grid is drawn in): every drag, drop, projection and "now" line of the grid is computed in the device zone, so a display zone would touch all of them (design D6). Listed for a later change if asked for.
- More than one extra zone, a world-clock panel, a per-day "+1" marker, and showing the second zone in Month, Year, Plan or the phone's Week agenda (none of them has an hour gutter).
- Syncing the choice across devices, changing how tasks or events are stored, and any server, op or API change.

This advances the roadmap's calendar line after `calendar-event-guests`; add a `calendar-time-zones` row to `ROADMAP.md` (proposed in #1233). Nothing is **BREAKING**: one optional key in a device-only preference, no environment variable, no op field, no dependency.

## Capabilities

### New Capabilities
- `calendar-time-zones`: the second hour column, its setting, its clocks and its accessibility.

### Modified Capabilities
- `time-blocking`: "Grid preferences" gains the second zone as a per-device preference.

## Impact

- **Web:** `web/src/lib/calendar.ts` (`GridPrefs.secondZone`), `web/src/lib/zoneGutter.ts` (new, pure), `web/src/app/calendar/SecondZone.tsx` (new), `web/src/app/calendar/DayColumn.tsx` (`HourGutter`), `web/src/app/CalendarView.tsx` (gutter widths), `web/src/app/CalendarSettings.tsx`, `web/src/lib/settingsCopy.ts`, `web/src/app/settingsIndex.ts`, locale catalogs, a story.
- **Docs:** `docs/time-blocking.md`, `changelog.d/Added/calendar-time-zones.md`, `ROADMAP.md`.
- **Data, server, API, ops:** none.
