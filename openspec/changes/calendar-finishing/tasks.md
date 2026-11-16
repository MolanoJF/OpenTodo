- [x] Soften tint and remove preview action tooltips; verify accessible names and UI.
- [x] Align page fields and add eligible calendar selection; verify form and move state tests.
- [x] Implement persistent create-first move receipts and failure/retry tests.
- [x] Update docs/catalogs and inspect responsive layouts; run focused tests, build, bundle and Linux checks.
- [x] Back up and deploy; verify health and retain rollback.

Validation: 241 focused tests plus the final 45-test move/editor recovery pass; typecheck and lint; production image build; Linux ui-check and i18n-check (7642 messages); bundle 143.4 KB entry / 692.2 KB first load. Inspected desktop/phone pages, picker and light/dark weekends. Deployed local/opentodo:cal-finishing-20261113 to CT 206: healthy, home and /event/new HTTP 200. Consistent pre-finishing volume copies retained on CT/NAS/laptop with matching SHA-256; prior image and compose retained. Full Linux CI and real-account moves remain pending.
