## Implementation
- [x] Add stable account colors and offline overrides; verify identity, refresh preservation and independent choices with Vitest.
- [x] Add accessible account swatches in Calendar sync settings; verify selection, reset and reactive subscription reads with a DOM test.
- [x] Apply hue surfaces to event blocks and rows; add a multi-account/project story and visually inspect light and dark rendering.
- [x] Update user docs and changelog; run npm test, npm run build, make openspec-validate and make check; report any environment blockers accurately.
- [ ] Obtain a green full Linux check before upstream contribution; Windows checks have unrelated failures.

## Verification (2026-11-13)
- Focused suite: 7 files, 114 tests passed (account color state/control, calendar sync settings, events in all views, project color mapping and token contrast).
- Production build, TypeScript, changed-file ESLint and full frontend lint passed. Bundle: entry 143.4 KB gzip; entry plus static imports 692.2 KB / 696 KB budget.
- OpenSpec strict validation passed for this change. Light and dark Storybook week inspected with two account colors, multiple project colors, all-day and free events.
- Full Windows web suite: 797 files passed, 11 failed, 1 skipped; 9483 tests passed, 16 failed, 4 skipped. Failures include path-sensitive layering/import/a11y checks and UI timing tests outside this change.
- `make check` passed vet, typecheck and frontend lint, then stopped at the POSIX `GOTOOLCHAIN=...` invocation on Windows. i18n/ui scripts also have existing Windows path-separator failures. No full green CI claim.
- The same source's Linux web build container passed `i18n-check` (7622 messages, all shipped catalogs complete) and `ui-check`; this confirms the local catalog/UI errors were Windows path handling.
- Deployed to CT 206 as `local/opentodo:cal-colors-20261113` after a stopped-service full-volume backup copied to the laptop with matching SHA-256. Previous image and Compose file retained for rollback.
