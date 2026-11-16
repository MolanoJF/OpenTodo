# Repeating events in the calendar editor

## Why

The owner's Google calendar holds 1,769 cached events, and 1,722 of them are occurrences of repeating series: daily routine blocks such as 07:00 to 08:30, 13:00 to 14:00 and 23:30 to 00:00. Phases 1 and 2 (`calendar-event-edits`, `calendar-event-editor`) made single events editable, and every one of those 1,722 is locked with reason `recurring` (`davLocked`, `internal/calsync/transport.go:164`), so the calendar a person plans in is read-only for almost everything on it. A calendar worth planning in lets you move one Tuesday's block, end a routine from a date on, or shift the whole routine, the way Notion Calendar and Google Calendar do, and lets you say "every weekday" and "remind me 10 minutes before" when you make an event.

The pieces are in place: the remote calendar is the truth, writes are conditional on the version the person saw, ids and `rev` exist, the outbox queues commands, the editor is a panel, and the series expansion (`calfeed.Expand`) already reads rules, excluded dates and exceptions. What is missing is the write side of a series.

## What Changes

- **Change a repeating event for a scope.** Move, resize, rename and edit any editor field of an occurrence, and delete it, for **this event**, **this and following events** or **all events**. After a drag or a key press the grid asks with a small menu (the occurrence menu's pattern); the editor asks on Save and Delete. `PATCH` and `DELETE` take `scope`, required for an occurrence.
- **CalDAV and Google.** One calendar object holds the series and its exceptions. This event writes or updates an exception (a delete is an excluded date); this and following ends the old series just before the occurrence and creates a new series with a new identity (new series first, so a failure never loses occurrences); all events changes the series and keeps excluded dates and exceptions attached to the same days. Properties, reminders and exceptions the editor does not change survive.
- **Microsoft.** This event patches the occurrence, all events patches the series master, this and following ends the master and creates a new series. Graph drops exceptions when a master's times or pattern change; that is documented, not hidden.
- **Set and change the repeat** in the editor and in the create flow: none, every day, every weekday, every week on chosen days, every month by day or by weekday, every year, any interval, ending never, on a date or after a number of times. A rule the editor cannot represent is kept and shown read-only as "Custom repeat".
- **Reminders** on events: a list of "time before" values written as display alarms (one for Microsoft). The calendar provider delivers them; OpenTodo adds no notification path.
- **Locks.** The `recurring` lock is gone. What stays: guests, not the organiser, a read-only calendar, no ETag, and a new `series_unsupported` for series OpenTodo cannot write back without loss (exceptions with `RANGE=THISANDFUTURE`, `RDATE`, several rules, huge objects).
- **Offline and pending.** Scope travels in the outbox command. This event is shown at once; this and following and all events show the edited block saving and then refetch the range.
- **Agents and the CLI.** `scope`, `repeat` and `reminders` on the event tools, `--scope`, `--repeat` and `--remind` on `ot calendar event`.
- **A 23:30 to 00:00 block is editable.** Checked against the code: a timed event that ends exactly at midnight of the next day is already draggable, resizable and keyboard-movable (`eventEditable`); this change pins it with tests for series and singles and fixes nothing else.

## Out of scope

- Guests, invitations, RSVP and conferencing (phase 4): events with guests stay locked, in the event or in any exception.
- ICS feed events (read-only), tasks' own recurrence (unchanged), Month, Year and Plan drags.
- Moving a series to another calendar (phase 2 D8).
- Editing a custom rule, `RDATE` series and `RANGE=THISANDFUTURE` exceptions; they are locked as `series_unsupported` or kept as they are.
- Microsoft "stop repeating" on an existing series (`repeat: null`), and relative-yearly patterns: kept read-only until verified against a real mailbox.
- Delivering reminders from OpenTodo.

This advances the roadmap's calendar sync line after `calendar-event-editor`; add a `calendar-event-series` row to `ROADMAP.md` on archive. Nothing is **BREAKING**: additive JSON fields and one error code, one idempotent migration of a new table (`0068`), no environment variable, no op field, no Dexie version.

## Capabilities

### New Capabilities

None. Everything extends existing capabilities.

### Modified Capabilities

- `calendar-event-edits`: scopes for repeating events, what a series keeps, the repeat and reminder fields, the series lock rules, refresh of a series, and nine restated requirements (the change route, which events can change, the offline outbox, the Day and Week edits, keyboard parity, agents and CLI, creating, the full editor, Convert to task).
- `calendar-feeds`: events of a series are served with `series`; the `recurring` reason is replaced.
- `calendar-sync`: a series change never becomes a task event.

## Impact

- **Server:** new pure engine `internal/calfeed/series*.go` (and `testdata/series/` goldens); `internal/calfeed/{vevent.go,managed.go,store.go}`; `internal/calsync/{events.go,event_dav.go,event_dav_series.go,event_graph.go,event_graph_series.go,event_edit.go,event_series.go,transport.go,graph_events.go,graph_transport.go}`; `internal/httpapi/{calsync_events.go,calsync_event_create.go,agent_calendar_events.go}`; `internal/store/migrations/0068_calendar_event_series.sql`; `internal/audit`; `internal/agenttools`; `internal/cli`.
- **Web:** `web/src/lib/{types,api,calendarEventEdits,calendarFeeds,eventDrop,eventFull,eventScope,eventRepeat}.ts`, `web/src/app/calendar/{ScopeMenu,EventRepeatField,EventRemindersField,EventForm,EventPanel,useEventEditing,EventLocked}.tsx`, `web/src/app/{CalendarEvents,useEventEdits}`, `web/src/locales/*`, `web/bundle-budget.json`.
- **Docs and checks:** `docs/{calendar-sync,calendar-views,calendar-feeds,api,mcp,cli,keyboard,accessibility}.md`, `scripts/ux-audit/*`, `changelog.d/Added/calendar-event-series.md`, `ROADMAP.md`, `internal/httpapi/testdata/v1_surface.json`.
- **Data:** one new table (`calendar_event_series`); no column added to an existing table, so a downgraded binary ignores it.
