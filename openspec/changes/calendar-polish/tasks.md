- [x] Add routed event pages and guarded return navigation; verify route round trips and dirty navigation tests.
- [x] Add shared block geometry, gap, wrapping and weekend/off-hours styles; verify geometry and calendar interactions.
- [x] Expose week-start/shading settings and improve color menu entries; verify preference/menu tests.
- [x] Condense previews, inspect desktop/phone/light/dark and update docs/catalogs; run focused tests, build, bundle and Linux checks.
- [x] Back up, deploy and verify health and HTTPS; record rollback details.

## Verification (2026-11-13)
- Focused regression suite: 19 files, 227 tests passed. Covers event routes, dirty navigation, descriptions/guests, save/delete/scope prompts, dragging/resizing, completed tasks, week-start ops, shading preferences and geometry. After adding the task-style phone Back action, the affected 33 tests and TypeScript passed again.
- Production build, full frontend ESLint and existing bundle budget passed: entry 143.4 KB gzip; first load 692.5 KB against 696 KB. No budget or dependency increase.
- Linux web build container: ui-check passed; i18n-check passed with 7632 messages and complete shipped catalogs. Strict change validation passed.
- Storybook inspected: wide staggered overlaps and weekends in light/dark, compact previews, and normal routed event pages on desktop/phone. No event-page modal overlay.
- make check passed vet, typecheck and frontend lint, then stopped at the Windows-incompatible POSIX GOTOOLCHAIN assignment. Full Linux CI and screenshot baselines remain upstream follow-up work.
- Deployed local/opentodo:cal-polish-20261113 to CT 206. Previous image local/opentodo:cal-details-20261113 and /opt/opentodo/compose.pre-polish-20261113.yaml retained for rollback.
- Pre-rollout stopped-service full-volume backup copied to C:/Users/juanp/home-lab-backups/opentodo/pre-polish-20261113.tar.gz, with a matching NAS SHA-256: cd009399f5c9a2a1c30fe2da44074730bf895365b58f5f20478562e768230a9c.
