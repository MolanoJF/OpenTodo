## Implementation
- [x] Add full event page frame and responsive content/facts layout; verify existing save, dirty close and scope tests.
- [x] Replace inline event editing with read-first comprehensive preview; verify preview and full-page actions.
- [x] Add task block previews without interfering with dragging/double-click; verify interaction tests.
- [x] Add stories, inspect desktop/phone/light/dark, update docs and validate focused tests, build, bundle and Linux UI/catalog checks.
- [x] Deploy with a backup and verify service health/HTTPS.

## Verification (2026-11-13)
- Focused regression coverage: 172 tests across 13 files; the final keyboard-preview expectation adjustment passed on rerun (12 tests). Save, deletion, recurrence scope, guest notification, dirty close, completed tasks, spans and preview read-only behavior covered.
- TypeScript, full frontend ESLint and production build passed. Entry plus static imports: 692.3 KB gzip / 696 KB budget; entry 143.4 KB / 143.4 KB.
- Linux build container: i18n-check passed (7623 messages, all catalogs complete); ui-check passed. Strict OpenSpec validation passed.
- Desktop light and phone dark full-form layouts and desktop/phone previews inspected in Storybook. Preview sheets avoid duplicate mobile title bars.
- Deployed as local/opentodo:cal-details-20261113; binary healthcheck and tailnet HTTPS 200 passed. Previous color image and Compose file retained.
- Fresh stopped-service full-volume backup copied to C:/Users/juanp/home-lab-backups/opentodo/pre-details-20261113.tar.gz; SHA-256 matched NAS copy: a24689838bcc7ce2906e2cfd93d38f1ca9627fa4675bbe40a7a4949ce78f016d.
- Full Linux CI and screenshot baselines remain upstream follow-up work. The full Windows suite's existing unrelated failures and POSIX make-check limitations were documented in calendar-source-colors; no green full-CI claim.
