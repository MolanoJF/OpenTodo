# Guests, replies and join links in the calendar editor

## Why

Phases 1 to 3 (`calendar-event-edits`, `calendar-event-editor`, `calendar-event-series`) make the events of a connected calendar editable, with one exception that matters for a working calendar: **every event that has guests is locked** (`has_guests`: `calfeed.SeriesLocked`, `internal/calfeed/series.go:323`; `graphEvent.locked`, `internal/calsync/graph_events.go:148`), and on Microsoft so is every meeting someone else organises. A calendar full of meetings therefore shows its meetings and cannot answer them. A person looking at "Planning, 14:00" cannot see who is coming, cannot say "Yes" or "No" from the app, and cannot join the call without leaving for Google Calendar or Outlook; the Today agenda lists the call and has no Join button.

The risk is also the reason this is its own phase. A wrong write to an event with guests sends email to real people. The owner's calendar is Google, reached through **Google CalDAV**, whose documentation says nothing about invitations, notifications or replies (design section "Provider research"). So this change reads everything, answers invitations where the provider is known to handle the answer, lets the organiser edit only where the provider tells the guests itself and OpenTodo can say so honestly (Microsoft Graph), and **keeps Google organiser edits locked** until a smoke test on the owner's calendar proves otherwise.

## What Changes

- **Read guests.** The popover and the editor show the organiser, each guest (name or email, required or optional, answer) and your own answer. A new side table `calendar_event_guests` (migration `0069`) caches the guest list on the server. The events route and the devices' cache carry only a summary (`guests: {count, you, organizer, canRespond}`); the full list is served by a cache-only route when a popover opens and is held in memory, never in the device's database.
- **Reply to an invitation** (you are a guest, not the organiser): Yes, Maybe, No for the event, and for a repeating event for **all events** (Microsoft also for **this event**). Microsoft uses the `accept`, `tentativelyAccept` and `decline` actions with `sendResponse` (default true; the editor offers "Tell the organizer" only because Graph honours `false`). Google CalDAV changes only your own `PARTSTAT`, with no change to `SEQUENCE` or any other property, and **reads the object back**: a server that did not keep the answer is reported, not hidden. Generic CalDAV servers do not offer replies (their scheduling behaviour is unknown and some, like Radicale, have none).
- **Organiser edits with guests (Microsoft only).** An event you organise on a Microsoft calendar is editable and deletable, one confirmation first: "Tell N guests?" says that Outlook sends the update itself and offers Save and tell, or Cancel, never a "don't send" it cannot honour. The server refuses such a change without `notifyGuests: true`. A series with guests allows **this event** and **all events** (not when the series has exceptions), never **this and following events**, because that split creates a second series and Graph would send new invitations.
- **Add and remove guests** (same Microsoft condition): an email field in the editor (Combobox with a create row for typed text), required or optional, server-side email validation. Also when creating an event.
- **Google and CalDAV organiser events stay locked** as `has_guests`, with a popover text that says why. They can be read, answered (Google only), converted to a task without deleting the event, and duplicated without guests.
- **Join link.** The server extracts a conference URL (Google `X-GOOGLE-CONFERENCE`, Teams `onlineMeeting.joinUrl`, and `https` Zoom, Google Meet, Teams and Webex addresses found in `URL`, `LOCATION` and `DESCRIPTION`) and caches it. A **Join** button opens it from the popover, the editor and the Today agenda. Opening a link is a click; nothing is fetched.
- **Create a Teams meeting** (Microsoft): `isOnlineMeeting` and `onlineMeetingProvider: teamsForBusiness`, offered only when the calendar lists that provider. Google Meet creation needs the Google Calendar REST API, which OpenTodo does not use; it is out of scope and this change does not add that API.
- **Offline.** Anything that tells a person (a reply, a change or delete of an event with guests, a create with guests) is **never queued**: it is sent when confirmed and online, and its controls are disabled offline with the reason. Changes to events without guests keep the outbox of phase 1.
- **Agents and the CLI.** `list_calendar_events` rows gain `guests` and `conference` (names, emails and links only as untrusted text); `update_calendar_event` and `create_calendar_event` gain `guests`, `notify_guests` and `online_meeting`; a new write tool `respond_to_calendar_event`; `ot calendar event respond`, `--guest`, `--no-guest`, `--notify-guests`, `--online-meeting`. The tools stay out of the in-app assistant and routines.

## Out of scope

- Creating a Google Meet (REST `conferenceData`), free/busy of guests, finding a time, scheduling links, ICS feed events (no guests, no Join there), delegated or shared calendars, proposing a new time, comments on a reply, removing an online meeting once made (Graph forbids it).
- Organiser edits and guest changes on Google and on generic CalDAV, until the smoke of task 7.5 proves how Google treats them (open questions G1 to G4).
- Replies for one occurrence on Google (`this`), `following` replies anywhere, forwarding.
- Delivering or reading invitation emails.

This advances the roadmap's calendar sync line after `calendar-event-series`; add a `calendar-event-guests` row to `ROADMAP.md` on archive. Nothing is **BREAKING**: additive JSON fields, one new route pair, one new tool (the tool count in `mcp_test.go` and the surface file move by one), one idempotent migration of a new table (`0069`), no environment variable, no op field, no Dexie version, no new OAuth scope (`Calendars.ReadWrite` covers the Graph actions; Google's `.../auth/calendar` is unchanged).

## Capabilities

### New Capabilities

None. Everything extends existing capabilities.

### Modified Capabilities

- `calendar-event-edits`: the change route and its locks for events with guests, offline and confirmation rules, Day and Week asking, agents and CLI, creating, the full editor, event details, Convert to task, plus new requirements for showing guests, replying, notifying guests, join links and the privacy of guest data.
- `calendar-feeds`: events are served with `guests`, `conference` and `access.notifies`; managed subscriptions carry `guests` capabilities; a cache-only guest list route.
- `calendar-sync`: calendar capabilities gain `onlineMeeting`; how each provider handles invitations.
- `today-agenda`: a Join button on an event with a conference.

## Impact

- **Server:** new `internal/calfeed/{guests.go,conference.go,respond.go}` (pure) with `testdata/guests/`; `internal/calfeed/{vevent.go,managed.go,store.go}`; `internal/calsync/{events.go,event_dav.go,event_dav_respond.go,event_graph.go,event_graph_guests.go,event_graph_respond.go,event_graph_create.go,event_edit.go,event_respond.go,graph_events.go,graph_transport.go,graph.go,transport.go,shown.go,calsync.go}`; `internal/calsync/{calsynctest,graphtest}` (scheduling simulation); `internal/httpapi/{calsync_events.go,calsync_event_create.go,calsync_event_respond.go,calendar_guests.go,agent_calendar_events.go,router.go,policy_test.go surface}`; `internal/store/migrations/0069_calendar_event_guests.sql`; `internal/audit`; `internal/agenttools`; `internal/assistant` (the exclusion list and its test); `internal/cli`.
- **Web:** `web/src/lib/{types,api,calendarEventEdits,calendarFeeds,eventFull,eventCreate,eventScope,eventGuests,eventConference,eventNotify}.ts`, `web/src/app/calendar/{EventGuests,EventRespond,GuestsField,NotifyGuestsDialog,JoinButton,EventPanel,EventForm,EventPopoverActions,EventLocked,ConvertToTask,TodayAgenda,notifyStore,eventActions}.tsx`, `web/src/app/CalendarEvents.tsx`, `web/src/locales/*`, `web/bundle-budget.json`.
- **Docs and checks:** `docs/{calendar-sync,calendar-views,calendar-feeds,add-calendar-events,api,mcp,cli,accessibility,assistant-privacy,ai-and-agents}.md`, `scripts/ux-audit/*`, `changelog.d/Added/calendar-event-guests.md`, `ROADMAP.md`, `internal/httpapi/testdata/v1_surface.json`, `internal/cli/testdata/schema.golden.json`.
- **Data:** one new table (`calendar_event_guests`); no column added to an existing table, so a downgraded binary ignores it. Personal data (guest names and emails) enters the database cache and therefore backup snapshots; see design D12.
