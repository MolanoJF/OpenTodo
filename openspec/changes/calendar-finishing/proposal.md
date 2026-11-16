# Calendar finishing touches
## Why
Weekend shading should be quieter, preview action tooltips should not cover content, event pages should share the preview's icon-led layout, and a single event should be movable to another connected calendar account.
## What Changes
- Reduce weekend tint and disable action tooltips in previews while retaining accessible names.
- Use the preview's clock, globe, repeat, bell, calendar, location and description icon vocabulary on the event page.
- Offer writable destination calendars for eligible single events. Move online by creating first and deleting the source only after a successful create, retaining a device receipt for idempotent retries.
## Capabilities
### New Capabilities
- `calendar-finishing`: quiet calendar styling and single-event calendar moves.
## Out of scope
Moving recurring series, guest meetings, online meetings, formatted descriptions or unsupported reminders. No new backend endpoint, op schema or dependency. Advances Calendar roadmap work.
