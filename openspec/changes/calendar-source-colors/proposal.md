# Calendar source colors

## Why
Calendar events currently have gray surfaces and only a tiny colored dot. Accounts are difficult to distinguish in a busy week. Project tasks already inherit a project's chosen color, but this relationship should remain clear beside colored events.

## What Changes
- Give events a soft source-colored fill, matching text and a strong leading edge, similar to Notion Calendar.
- Assign connected accounts a stable default hue and allow an account color override in Calendar sync settings on this device.
- Keep subscribed feeds' chosen colors, project task colors, free-event outlines and non-color markers.

## Capabilities
### New Capabilities
- `calendar-source-colors`: account color selection and consistent event surfaces.
### Modified Capabilities
- `calendar-feeds`: replace the neutral event surface with a hue fill while retaining texture and a dot to distinguish events from tasks.

## Out of scope
Provider color writes, synced account color preferences, changing task/project storage, changing the existing Colour by modes and calendar layout. Advances the roadmap's Calendar work; no breaking changes or environment variables.

## Impact
Frontend only: device meta, subscription read model, calendar event CSS and an account settings control. No dependency, API or op-schema change.
