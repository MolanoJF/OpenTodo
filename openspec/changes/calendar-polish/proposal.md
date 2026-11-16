# Calendar navigation and visual polish

## Why
Event details should navigate like task pages. The grid needs gaps between consecutive blocks, readable staggered overlaps, configurable off-hours shading, weekend distinction and discoverable week-start controls.

## What Changes
- Route event details through the app shell, preserving a return to the calendar and dirty navigation guards.
- Condense read-first previews and improve top-aligned titles and start/end time ranges.
- Give timed cards vertical gaps and staggered overlap widths, with hover/selection bringing a card forward.
- Turn off-hours shading off by default with a device preference; retain adjustable grid hours. Subtly shade weekend columns.
- Expose the existing synced week-start preference in calendar settings and the calendar menu; give color options icons and descriptions.

## Capabilities
### New Capabilities
- `calendar-polish`: routed event pages and calendar presentation.

## Out of scope
Provider changes, event/task storage changes, new dependencies and a different scheduling/capacity model. Advances the roadmap Calendar work; no breaking ops or environment variables.
