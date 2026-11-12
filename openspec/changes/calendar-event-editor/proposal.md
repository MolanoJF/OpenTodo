# Create and fully edit calendar events

## Why

Phase 1 (`calendar-event-edits`) lets you move, resize, rename and delete an existing event of a connected calendar. You still cannot make one: planning a day in OpenTodo means adding the 30-minute call as a task, then opening Google or Outlook to put it in the calendar the other person sees. The popover also edits only the title and the times of a timed, one-day event; location, notes, busy or free, all-day and the time zone need another app.

A calendar worth planning in lets you drag over 14:00 to 15:00, type "Call Ana", and have the event in the real calendar a second later, from a phone on a train too. This phase adds that: create events, a full editor, and the two moves that make events and tasks one flow (Duplicate, Convert to task).

## What Changes

- **Create events** in a calendar of a connection that has "Edit events in OpenTodo" on and a calendar that allows creating (`caps.create`). A double-click, a right-click or a long press on an empty slot, `q`, and, with a mouse or pen, a **drag over an empty stretch of the grid** offer **Task or Event**. The default is Task and the device remembers the last choice. Without a writable calendar nothing changes: no choice, no sweep, no new menu item.
- **A full event editor**, one form for creating and editing: title, start and end (date and time), all-day, time zone, calendar, location, description (plain text), busy or free, Delete, **Duplicate** and **Convert to task**. It is a side panel from 1024 px and a bottom sheet on a phone, loaded only when opened. The phase-1 popover keeps its quick fields (they stay the one-click alternative to dragging) and gains "All details".
- **Server**: `POST /api/v1/calendar-sync/connections/{id}/events` creates an event; `GET .../events/{eventId}` reads one event's description from the calendar when the editor opens; `PATCH` takes `location`, `description` and `free` too. `EventWriter` gains `Create` and `Detail` for CalDAV and Google (a new object, `If-None-Match: *`) and Microsoft Graph (`POST /me/calendars/{id}/events`).
- **Safe to retry.** The client invents an id for each new event (`clientId`). It is the CalDAV UID and file name, Graph's `transactionId`, and the key of a small record the server keeps (migration `0067`), so a create sent twice makes one event, and one that was deleted meanwhile does not come back.
- **Offline.** A queued create shows at once as a pending block; edits and a delete of it merge into the create command; the block swaps to the calendar's event when the server answers. A refused create hands the typed event back in a toast.
- **Command palette and keys**: "New event" (the next free half hour today, in the default writable calendar) and `Shift+Q` in Day and Week.
- **Agents and the CLI**: `create_calendar_event`, `update_calendar_event` with the new fields (behind the same write gates, never a routine step), `ot calendar event add`, and new flags on `edit`.
- **Default calendar** for new events: the one used last on this device, else the first writable calendar of the first connection.
- **Moving an event between calendars is deferred**, with the reason in design D8. Duplicate into another calendar covers the need.

## Out of scope

- Recurring events (creating them, editing a series, "this and following"): phase 3.
- Reminders and alarms: phase 3. Created events carry none.
- Guests, invitations, RSVP, conferencing links, rich-text descriptions: phase 4.
- Moving an existing event to another calendar (design D8).
- ICS feed events (read-only for good), and events of connections where "Edit events in OpenTodo" is off.
- Dragging a task-only sweep for people without a writable calendar (see Open questions in the design).
- Month, Year and Plan drags (their rows may open the editor; they do not drag).
- Putting events in the op log. Events stay server data with the calendar as the truth.
- New dependencies, new environment variables, new OAuth scopes.

This advances the roadmap's calendar sync line, after `calendar-event-edits`, with a new row `calendar-event-editor` to add to `ROADMAP.md` when the change is archived. Nothing is **BREAKING**: additive JSON fields and routes, one idempotent migration of a new table, no environment variable, no op field, no Dexie version.

## Capabilities

### New Capabilities

None. Everything extends existing capabilities.

### Modified Capabilities

- `calendar-event-edits`: creating events, the detail read, the extended `PATCH`, offline creates, the Task or Event choice, the full editor, Duplicate, Convert to task, the default calendar, the create alternatives for dragging, and the agent and CLI create. Two requirements are restated: the route requirement (new fields) and the agents requirement (the create tool, and a correction: the in-app assistant does not offer the calendar event tools).
- `calendar-sync`: events created in OpenTodo stay events and are never linked to tasks.
- `calendar-feeds`: event descriptions are read from the calendar when an editor opens, never cached or served in lists.
- `time-blocking`: "Add with the pointer in Day and Week" gains the Event choice and the sweep.
- `command-palette`: "New event".

## Impact

- **Server:** `internal/calsync` (`events.go`, `event_dav.go`, `event_graph.go`, new `event_create.go`, `event_edit.go`, `graph.go` select), `internal/calfeed` (`managed.go`, `store.go`), `internal/store/migrations/0067_calendar_event_created.sql`, `internal/httpapi` (`calsync_events.go`, new `calsync_event_create.go`, `calsync.go`, `agent_calendar_events.go`), `internal/audit`, `internal/agenttools`, `internal/cli`, `internal/assistant/loop.go` (the `notInApp` list), `internal/rules` (the routine-tool test).
- **Web:** `web/src/lib/{types,api,calendarEventEdits,calendarFeeds,eventDrop,eventCreate,shortcuts}.ts`, `web/src/app/{CalendarEvents,calendarDrag}.ts*`, `web/src/app/calendar/{addHere,AddOnDayDialog,DayColumn,EventEditor}.tsx` and new `EventPanel.tsx`, `EventForm.tsx`, `eventConvert.ts`, `useWritableCalendars.ts`, `web/src/app/palette/registry.ts`, `web/src/locales/*`.
- **Docs and checks:** `docs/{calendar-sync,calendar-views,calendar-feeds,api,mcp,cli,keyboard,accessibility}.md`, `scripts/ux-audit/{screens.json,drag-alternatives.json,layout-screens.mjs,seed.mjs}`, `ROADMAP.md`, `changelog.d/Added/calendar-event-editor.md`, `internal/httpapi/testdata/v1_surface.json`, the MCP tool counts in tests.
- **Data:** one new table (`calendar_event_created`); no column added to an existing table, so a downgraded binary ignores it.
