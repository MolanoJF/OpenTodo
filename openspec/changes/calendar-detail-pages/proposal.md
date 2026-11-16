# Calendar detail pages and previews

## Why
Events open a narrow stacked form while tasks have a full detail page. Calendar task clicks only select a block. A consistent preview and detail flow makes both kinds easier to inspect without losing the week.

## What Changes
- Full-width event detail/edit page with the task page's title/content and facts-column layout, existing icons and controls.
- Clean read-first event preview with date/time, zone, recurrence, guests, conference, location, description, source and reminders; explicit Edit/All details action.
- One-click calendar task preview with schedule, project, notes and a full-task action. Double-click and keyboard full-open remain available.

## Capabilities
### New Capabilities
- `calendar-detail-pages`: full event page and consistent calendar previews.

## Out of scope
New provider capabilities, comments/attachments on remote events, task schema changes, provider color writes or a new router format. Advances the roadmap Calendar work. No breaking changes, environment variables or dependencies.

## Impact
Frontend only. Preserve the existing event write/scope/guest-confirmation path and task ops. Lazy-load preview/detail content within the current bundle budget.
