- [x] Compact the phone calendar header and extend its grid behind the add button.
- [x] Simplify phone event fields and move submit to the header.
- [x] Verify interaction, responsive layouts, typecheck, lint, build and bundle budgets.
- [x] Update guides, back up, deploy and verify health.

Validation: all 124 focused tests passed across 10 files, including the phone header submit, invalid time guard, unsaved-change prompt and one queued create. Typecheck, lint, local and Linux production builds, Linux UI/catalog checks (7641 messages), and all 170 OpenSpec validations passed. Unchanged budgets: 143.4 KB entry / 692.4 KB first load. Inspected 320/390/430 px, short viewport, light/dark, expanded native date/time fields and end-of-calendar clearance above the floating button. CT 206 deployed local/opentodo:cal-phone-20261115 and reports dev+236910861-phone; healthy, home/day/event routes return HTTP 200. Pre-phone backups on CT/NAS/laptop have matching SHA-256 f96510ae93e67bef1db36eb146e00f00d96746ebd66d0cd63fe2b56111dcbb52. Prior header image and Compose file retained. Full Linux CI and physical iPhone testing remain pending.
