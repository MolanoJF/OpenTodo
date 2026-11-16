# Tasks

All web; no Go change. Verify with `npm test` (the named file), `npm run typecheck`, `npm run lint`, `npm run build` and `make i18n-check` in `web/`. Run `make check` before ticking 4.4.

## 1. Preference and clocks (design D1, D3)

Touches: `web/src/lib/calendar.ts`, `web/src/lib/zoneGutter.ts`, their tests.

- [x] 1.1 `GridPrefs.secondZone` (default `''`), normalised by `normalizeGridPrefs` (unknown zone reads as off, stored value untouched). Verify with `web/src/lib/calendar.test.ts` cases: default, valid, unknown, wrong type.
- [x] 1.2 `web/src/lib/zoneGutter.ts`: `zoneCode`, `secondZoneOf`, `hourClocks(date, viewer, zone)`. Verify with `web/src/lib/zoneGutter.test.ts`: Kolkata, Kathmandu, Bogotá, 12-hour format, `UTC`, DST in the second zone (2026-03-08, New York), DST in the device zone (2026-03-29, Berlin), the repeated hour of 2026-10-25, the same zone gives null.

## 2. Grid and setting (design D2, D4, D5)

Touches: `web/src/app/calendar/{SecondZone.tsx,DayColumn.tsx}`, `web/src/app/CalendarView.tsx`, `web/src/app/CalendarSettings.tsx`, `web/src/lib/settingsCopy.ts`, `web/src/app/settingsIndex.ts`, locale catalogs.

- [x] 2.1 `SecondZone.tsx` (gutter and header cell) wired into `HourGutter` and the sticky header. Verify with `web/src/app/calendar/secondZone.test.tsx`: off draws one column, equal zone one, two columns with labels, `aria-hidden` labels, the zone stated once, fixed widths, Day and Week.
- [x] 2.2 The "Second time zone" row with the zone Combobox and Off. Verify with `web/src/app/calendarSecondZoneSettings.test.tsx`: off by default, choose, off again, saved through `saveGridPrefs`, other prefs kept.
- [x] 2.3 Strings through `t()`, English copied into the other catalogs. Verify with `make i18n-check`.
- [x] 2.4 A story of Day with Bogotá beside Berlin in `web/src/app/calendar/secondZone.stories.tsx`. Verify with `npm run typecheck` (the story compiles) (no baselines yet).

## 3. Docs and changelog

Touches: `docs/time-blocking.md`, `changelog.d/Added/calendar-time-zones.md`, `ROADMAP.md`.

- [x] 3.1 Document the setting and the column in `docs/time-blocking.md` within its word limit. Verify with `make docs-check` and `npm run help` in `web/`.
- [x] 3.2 Changelog fragment and the ROADMAP row. Verify with `make repo-check` and `make docs`.

## 4. Final

- [x] 4.1 `npm test`, `npm run typecheck`, `npm run lint`, `npm run build` and the bundle budget; report the gzip delta.
- [x] 4.2 `npx @fission-ai/openspec@1.13.2 validate calendar-time-zones --strict` and `--all --strict`.
- [x] 4.3 Swap is recorded as out of scope (design D6).
- [ ] 4.4 Linux CI and ui baselines — deferred until all phases are done
