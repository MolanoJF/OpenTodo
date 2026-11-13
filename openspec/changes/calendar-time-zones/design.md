# Design

## Context

- **One gutter today.** `HourGutter` (`web/src/app/calendar/DayColumn.tsx`) draws 24 labels (`formatHour`, so `09:00` or `9 AM` by the time-format preference) in a `w-12` column; the sticky header repeats a `w-12` gutter that says "all day". `CalendarGrid` (`web/src/app/CalendarView.tsx`) is the only caller. The gutter is `aria-hidden`: every block carries its own time in its text.
- **Where the grid is drawn.** Day, and Week at wide widths. The phone's Week is an agenda (`WeekAgenda`) with no hour column; Month, Year and Plan have none either. There is no 3-day grid.
- **Everything is in the viewer's zone.** `CalendarView` projects every due and event into `useViewerZone()` and the drag layer converts back (`eventPointIn`, `localDueOf`).
- **Grid preferences are per device.** `GridPrefs` (`web/src/lib/calendar.ts`) lives in the device meta key `calendar.grid`, saved by `saveGridPrefs` and read live with `useGridPrefs`; `time-blocking` "Grid preferences" says "SHALL not be synced". The Settings card is `scope: 'device'`.
- **The date and time preferences are synced**, written as ops on the user's personal data (`date-time-preferences`), which needs an op field.
- **Zone helpers exist:** `zonedInstant`, `viewerZone`, `isKnownZone`, `zoneCity`, `formatTime`, and the zone Combobox items of `EventZoneField` (`allZones`, `zoneOffset`).

## Goals / Non-Goals

**Goals:** a second, correct, readable hour column in Day and Week; one off-by-default setting; no data or server change; no horizontal page scroll at 360 px.

**Non-Goals:** Swap, several extra zones, Month/Year/Plan/agenda surfaces, syncing the choice.

## Decisions

### D1. The choice is per device, in the grid preferences

`GridPrefs` gains `secondZone: string`, `''` when off (the default), stored with the other grid fields under `calendar.grid`. A stored value that is not a known IANA zone reads as off and is kept as stored.

*Why per device, not synced.* (a) It sits next to the grid hours, which the spec already makes device-only, so "Calendar grid" stays one coherent card with one scope. (b) A synced preference needs an op field, which the scope forbids unless proven needed, and it is not: nothing server-side or on another device depends on it. (c) It is situational: a laptop at a desk may want Bogotá beside it while a phone wants nothing, and someone who travels changes it on one device. Rejected: a synced preference beside the date format (an op field and a conflict rule for a cosmetic choice).

### D2. Surfaces

The Day grid and the wide Week grid. `HourGutter` draws the second column when a second zone is set and differs from the device zone (equal zones draw nothing: a duplicate column says nothing). The phone Week agenda, Month, Year and Plan have no hour column and are untouched.

### D3. Labels are clocks, computed per row

For the first date on screen and each hour `h` of the device zone, the label is the wall time, in the second zone, of the instant `zonedInstant(date, "h:00", viewerZone)`, rendered by `formatTime` so the time-format preference applies (`09:00` or `9 AM`, `14:30` or `2:30 PM`). It is a pure function in `web/src/lib/zoneGutter.ts` over `Intl`, with no offset arithmetic, so:

- **Half-hour and 45-minute zones** (Asia/Kolkata +5:30, Asia/Kathmandu +5:45): with the device on UTC, the 09:00 row reads `14:30` and `14:45`.
- **DST in the second zone** (America/New_York on 2026-03-08, device on UTC): the second zone's clock is read at the same instants, so the 06:00 row reads `01:00` and the 08:00 row `04:00`; the skipped hour shows as the jump.
- **DST in the device zone** (Europe/Berlin on 2026-03-29, second zone UTC): the 02:00 row does not exist; `zonedInstant` resolves it to the moment the clocks jump, so it reads `01:00`, the same as the 03:00 row below it. On the repeated hour of 2026-10-25 the earlier instant is used.
- **A week** is labelled for its first day. If the second zone changes offset inside the week, later columns' labels are one hour off for those days; blocks carry their own times, so the grid stays readable. Rejected: a gutter per day (a wide grid of clocks, not calm).

The zone code is the first three letters of the zone's city, without accents and in capitals (`America/Bogota` is BOG, `Europe/Berlin` BER, `Asia/Kathmandu` KAT), and `UTC` stays `UTC`. Rejected: `Intl` abbreviations, which differ by language and are bare offsets ("GMT-5") for most zones in most locales.

### D4. The setting

A row "Second time zone" in `CalendarSettingsView`, after "Day length": the zone `Combobox` of `EventZoneField` over `allZones(viewerZone)` with city, region and offset, plus an "Off" first item. Choosing writes `secondZone` through `saveGridPrefs`. The settings search index gets an entry (`time zone`, `world clock`). No new page, no new shortcut.

**Interface budget** (`docs/interface-guide.md`): no row of chrome is added above the grid (0 of 2); no new task property, sidebar destination, scroll region or shortcut; the header's gutter cell grows from one line to two only when the setting is on, inside the day header's existing height.

### D5. Accessibility and the phone

- **Decorative, with the zone stated once.** The existing gutter is `aria-hidden` because each block states its own time. The second gutter's labels are `aria-hidden` for the same reason; the sticky header cell above them has the code (`aria-hidden`) and one visually hidden sentence, "Times also shown in Bogotá (UTC-5)", read once. The `accessibility-aa` spec needs no change: it requires axe-clean screens and 24 px targets, and these labels are neither interactive nor lower contrast than the first column's (same `text-faint` token).
- **360 px.** Week is an agenda there, so Day is the only grid. The two gutters are `w-12` each (96 px of 360), leaving 264 px for the day column, and the scroller has no fixed width, so the page does not scroll sideways. Hiding the second column on phones was rejected: the phone is where someone away from their desk checks the other clock. A unit test asserts both gutters are fixed-width and non-shrinking; the 360 px reflow is measured by the existing screen gate in CI.
- **Tokens only, `t()` strings**: "Second time zone", "Off", "Times also shown in {zone}"; English is copied into the other catalogs.

### D6. Not doing Swap

Swap would make the second zone the zone the grid is drawn in. `useViewerZone()` feeds `viewedTask`, `calendarDays`, capacity, the now line, `eventPointIn`, drag, drop and keyboard moves, and `localDueOf` for floating dues. Changing it for display only would make a drop write the wrong wall time. That is not cheap or safely separable, so it is out of scope.

### D7. Size and cost

About 1.5 KB gzip on first load: `zoneGutter.ts` (about 40 lines), the second gutter component and the key in `GridPrefs`, all drawn with the grid. The zone Combobox lives in the Settings page chunk, already lazy. `initialGzipKB` is re-measured after the build and moves only with a dated reason. No dependency, no runtime memory beyond 24 strings per render, computed once per date and zone.

### D8. Test plan

Vitest: `zoneGutter` vectors (Kolkata, Kathmandu, Bogotá, DST in each zone, 12-hour format, `UTC`), `normalizeGridPrefs` (default off, unknown zone off, kept value), the gutter (two columns, off draws one, equal zone draws one, labels `aria-hidden`, the zone stated once, fixed widths), the settings row (off by default, choose, off again, saved through `saveGridPrefs`). A Storybook story shows Day with Bogotá beside Berlin. Linux visual baselines come from CI at the end.

## Risks

- **Three-letter codes can collide or mislead** (`Asia/Kolkata` is KOL, `America/Indiana/Knox` is KNO); the visually hidden sentence gives the full city and offset, and the Combobox shows both.
- **A week with a DST change in the second zone** labels later days by the first day's offset (D3).
- **Syncing the choice later** needs a new key and op field; nothing here prevents it.
