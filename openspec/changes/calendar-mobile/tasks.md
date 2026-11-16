- [x] Reproduce full-shell phone layouts and implement bounded scrolling and event-route chrome.
- [x] Verify task/event footers and narrow calendar fields, previews and controls.
- [x] Run focused regressions, responsive checks, typecheck, lint, production build and bundle checks.
- [x] Update documentation, back up, deploy and verify service health.

Validation: 110 focused tests, typecheck/lint and production build passed. Linux UI and catalog checks passed (7642 messages); entry 143.4 KB, first load 692.1 KB within unchanged budgets. Full-shell touch checks: 320/390/430 px widths, short viewport, Day/Week, task/event sheets, new event and long task scroll bounds. CT 206 deployed local/opentodo:cal-mobile-20261113, home and /event/new returned HTTP 200. Pre-mobile stopped-volume backups on CT/NAS/laptop have matching SHA-256, prior image and compose retained. Native-device testing and full Linux CI remain pending.
