- [x] Implement the view dropdown, custom day counts and separated navigation.
- [x] Move visibility controls to More actions across calendar modes.
- [x] Simplify grid rules, weekends, titles and all-day lanes.
- [x] Verify regressions, responsive layouts, localization, lint, typecheck and build budgets.
- [x] Update docs, back up, deploy and verify health.

Validation: all 210 focused tests passed in the final serial run (17 files); responsive desktop and full-shell phone inspection at 320/390/430 px, including 320x500 menus; custom 3-day agenda and persisted range navigation; typecheck, lint, production image build, OpenSpec validation, Linux UI and catalog checks (7641 messages), and unchanged bundle budgets (143.4 KB entry / 692.6 KB first load). CT 206 runs local/opentodo:cal-header-20261113, healthy; home, /week and /event/new return HTTP 200. Pre-header stopped-volume backups have matching CT/NAS/laptop SHA-256 e01a566966b71f798c8965826c9b3b97159e3c3305dc03e1c5f8cfd0f9de82dd. Prior image and compose retained. Full Linux CI and physical-device testing remain pending.
