# Design

## Context

Phase 1 (`calendar-event-edits`, archived 2026-11-12) is the base; this change reuses its decisions (the remote calendar is the truth, event writes are server commands with ETag preconditions and not ops, `rev` is a hash of the ETag, ids change on a move, a device-local outbox). What the code does today, and the facts this design had to work around:

- **What the cache holds.** `calendar_events` (`0031_calendar_feeds.sql`) already stores `summary`, `location`, `free`, the times and `all_day`; `EventRow` already serves `location` and `free`. **Descriptions are not parsed (`calfeed.Event` has no field), stored or served.** `calendar_event_remote` (`0066`) holds href, ETag and the lock reason.
- **Only shown calendars have a cache.** A calendar gets a managed subscription only when its connection shows it (`internal/calsync/shown.go:57`, `EnsureManaged`). A new event has to appear in the grid, so it can only be created in a shown calendar.
- **Creating is already a proven write.** `DAV.put` with etag `"*"` sends `If-None-Match: *` (`remote.go:195`) and writes task events at `<calendar><id>.ics` (`transport.go`, `davTransport.create`); Graph task events are created with `POST /me/calendars/{id}/events` and a `transactionId` (`graph_transport.go:208-215`). `caldav.VTimezone`, `addZone` and `pointProp` (`event_dav.go`) already write zoned, floating and all-day points.
- **Graph events are read in UTC.** `graphDateTime.point` returns `Zone: "UTC"` for every timed Graph event (`graph_events.go:85`), and Google's CalDAV answers often use `Z` times. So an event's served `tz` is often `UTC` without being the event's meaningful zone.
- **Graph UIDs are Graph ids.** `graphEvent.event()` sets `UID: e.ID`, so a created Graph event's cached `uid` is known only after the answer.
- **Phase 1 edits only timed, one-day events.** `eventEditable` returns false for all-day events (`web/src/lib/eventDrop.ts:135`); `EventChip` (the all-day strip) is read-only (`CalendarEvents.tsx:143`); the popover form has a title, one date and two times, no zone, no all-day switch; the Delete key ignores all-day events (`useEventEdits.ts:70`).
- **No drag-to-create exists.** `addHandlers` in `DayColumn.tsx` gives double-click, right-click and a 500 ms touch hold on an empty place (`isEmptyPlace`, `addHere.tsx:30`); the menu is `AddHereItems`; `AddOnDayDialog` creates the task.
- **The panel slot exists.** `openPanel(key, render)` puts a `custom` occupant in the shell's slot (`panel.ts:196`; the planning list and the Notes page use it) wrapped in `PanelFrame`, which is a right-hand column from 1024 px and a full-height drawer below, not a bottom sheet. The overlay module's `Dialog` is the bottom sheet on a phone.
- **Per-device settings are Dexie meta rows** (`getMeta`/`setMeta`, `db.ts:579`; `calendar.showEvents` is one). `EventCommand` is stored in `eventOutbox`, declared `'key, notBefore'` (`db.ts:474`); Dexie declares indexes only, so new command fields need no version.
- **Budgets and keys.** The entry chunk is within 0.1 KB of its limit (`web/bundle-budget.json`, raised 2026-11-12 for phase 1). `e` is taken in the calendar (`calendar.edit-estimate`, `shortcuts.ts:284`); `Shift+Q` is free.
- **Spec drift to fix.** The merged `calendar-event-edits` requirement "Agents and the command line can change events" says the in-app assistant offers the event tools; as built it does not (`notInApp`, `internal/assistant/loop.go:425`; phase 1 design D8, "As built"). This change restates that requirement correctly.

## Goals / Non-Goals

**Goals:** create events from the grid, the keyboard, the palette, the CLI and agents; one full editor for creating and editing; Duplicate and Convert to task; a retried or offline-queued create makes exactly one event; nothing changes for people without a writable calendar.

**Non-Goals:** recurring events, reminders, guests, conferencing, rich-text descriptions, moving an event between calendars (D8), ICS feeds, events as a synced entity.

## Decisions

### D1. Writable calendars and the default calendar

A **writable calendar** is a managed subscription whose connection has `editEvents` and whose `caps.create` is true. The events route already serves `connectionId`, `calendarUrl`, `caps` and `editEvents` on managed subscriptions, so the web needs no new request: `useWritableCalendars()` (`web/src/app/calendar/useWritableCalendars.ts`) is a live query over `db.calendarSubscriptions` (`connectionId && editEvents && caps?.create && enabled !== false`), in the order the server serves them (creation time, then name).

The Event choice appears only when this list is not empty. The **default calendar** is the subscription whose id is in the device's meta row `calendar.eventCalendar` (written when an event is created or the picker changes), else the first row of the list. A remembered id that is no longer writable is ignored. The last Task or Event choice is the meta row `calendar.newKind` (`'task' | 'event'`, default `'task'`); an `event` value is ignored while the list is empty. Both are per device and never synced (like `calendar.showEvents`).

Creating an event while this device hides calendars (`setShowEvents(false)`) turns them on first and says so in the toast, so a new event is never invisible.

### D2. Read path and migration `0067`

Location and busy or free are cached and served already: no change. **Descriptions stay out of the cache and out of every list.** They can be large (Graph bodies), they are free text from a foreign calendar, and a grid never needs them. They are read from the calendar when an editor opens (D4 `GET`), held in memory by the editor, and written back only when the person changed them. Consequence: offline, the description field is disabled with the reason and is not sent, so a queued or retried edit never overwrites a description the person did not see.

One new side table, idempotent, no `ALTER` (the pattern of `0066`; `0066_calendar_event_edits.sql` is the last migration, so the next number is **0067**):

```sql
-- 0067_calendar_event_created.sql
-- The events OpenTodo created for a client id, so a create sent twice makes one event and
-- an event deleted since is not made again. Server data, never an op. (calendar-event-editor D2)
CREATE TABLE IF NOT EXISTS calendar_event_created (
  connection_id   TEXT NOT NULL REFERENCES calsync_connections(id) ON DELETE CASCADE,
  client_id       TEXT NOT NULL,            -- the client's id for the event (8 to 64 of A-Za-z0-9_-)
  subscription_id TEXT NOT NULL,            -- the shown calendar it went into (no FK: a hidden calendar keeps the record)
  uid             TEXT NOT NULL,            -- the cached uid: the client id for CalDAV, the Graph id for Microsoft
  created_at      INTEGER NOT NULL,         -- Unix ms; rows older than 30 days are deleted on the next create
  PRIMARY KEY (connection_id, client_id)
);
```

`calendar_events` and `calendar_event_remote` keep their columns. A downgraded binary never reads the table. Retention: `AddEvent` deletes rows older than 30 days in its own transaction, so no job is needed; an outbox older than that is outside what the product promises.

### D3. Id, `rev` and the pending-id swap

- **The client id.** `crypto.randomUUID()` per new event, sent as `clientId`. It is not the id the grid uses.
- **On the server.** CalDAV: UID = `clientId`, object address `<calendarUrl><PathEscape(clientId)>.ics`. Graph: `transactionId` = `opentodo-ev-<clientId>`. Both providers: a row in `calendar_event_created`.
- **Event id and `rev`.** After a create the event is an ordinary managed event: `id = calfeed.EventID(subscription, uid, instance)` with `instance` the start (so a later move changes it, as in phase 1) and `rev = calfeed.Rev(etag)` of the ETag the calendar gave. If the calendar gave none (and a `GET` gives none) the event is created and served with `access.reason: "no_etag"`, read-only.
- **On the device.** A queued create is drawn as a **pending row** in `calendarEvents` with `id = "pending:" + clientId`, `uid = clientId`, no `rev`, `access: {update: true, delete: true}`, and a command in `eventOutbox`. The server never sees a pending id. When the server answers, `settle` deletes the pending row, puts the answer, and records `swapped["pending:<clientId>"] = event.id`, the mechanism phase 1 uses so a selection, an open popover and an Undo find the event under its new id (`currentEventId`).
- **Edits before the answer.** Not sent yet: merged into the create draft (D9). In flight: held in `next` and sent as a `PATCH` after the swap against the new id and `rev` (the phase-1 rule, unchanged).

### D4. REST contract

All routes sit behind the calendar sync switch (404 `calendar_sync_disabled`), are registered in `internal/httpapi/calsync.go` as `mux.handle(pattern, signedIn.DemoDenied(), gate(handler))`, and share the `calsync.event` limiter (600 an hour per user) with `PATCH` and `DELETE`. The new handlers live in `internal/httpapi/calsync_event_create.go`. Read and agent tokens are refused on `POST` and `PATCH` by the existing non-GET rule; `GET` takes any token that may read.

```
POST /api/v1/calendar-sync/connections/{id}/events
  body: {
    "clientId":    "6b1f1c2e-8f3a-4d0b-9d58-0c5a4e1b7a21",   // required; 8 to 64 of A-Za-z0-9_-
    "calendarUrl": "https://dav.example.com/cal/work/",      // required; a shown calendar of this connection
                                                             //   (Graph: the calendar id), caps.create true
    "summary":     "Call Ana",                               // required; 1 to 500 characters
    "start": {"date": "2026-11-13", "time": "14:00", "tz": "Europe/Lisbon"},   // required
    "end":   {"date": "2026-11-13", "time": "14:30", "tz": "Europe/Lisbon"},   // required
    "allDay":      false,                                    // optional; all-day: dates only, end exclusive
    "location":    "Café Central",                           // optional; at most 500 characters
    "description": "Bring the draft.",                       // optional; plain text, at most 10000 characters
    "free":        false                                     // optional; true: shown as free
  }
  201: {"event": <the event as the events route serves it: id, rev, access>, "clientId": "<echo>"}
  200: the same body, when this clientId was created before (the current state of that event)

GET /api/v1/calendar-sync/connections/{id}/events/{eventId}
  200: {"event": <row>,
        "replaces": "<old id>",        // only when the calendar had a newer version than the cache
        "description": "Bring the draft.",
        "descriptionEditable": true}   // false: a rich-text body the editor shows read-only

PATCH /api/v1/calendar-sync/connections/{id}/events/{eventId}   // the phase-1 body, plus:
  "location": "Room 4",        // optional; "" clears it
  "description": "…",          // optional; "" clears it; refused when descriptionEditable was false
  "free": true                 // optional
  // "nothing to change" now counts summary, start and end, location, description and free
```

`start`, `end` and `allDay` follow the phase-1 rules exactly (`eventPatch.editOf`, `calsync_events.go:79`): a point with a `tz` is zoned, without one floating, all-day points are dates. `GET` reads the calendar once. When the calendar's ETag differs from the stored one, the server stores the new state (the same `ReplaceEvent` a write ends in), wakes the user's other devices and answers with the fresh row and `replaces`; the editor swaps to it before the person types, so most stale edits end as a visible refresh instead of a 409. `GET` needs the connection's `edit_events` on (the description is shown only in the editor) and does not need the event to be unlocked, so Duplicate and Convert to task can read a locked event's description.

**Errors** reuse the phase-1 codes. Nothing new is added:

| Status | Code | When |
| --- | --- | --- |
| 401 | `unauthorized` | no credentials |
| 403 | `demo_restricted` | public demo |
| 403 | `forbidden` | a read or agent token on `POST` or `PATCH` |
| 404 | `calendar_sync_disabled` | the owner's switch is off |
| 404 | `not_found` | the connection is not the caller's; for `GET` and `PATCH` also an event that is not cached |
| 403 | `event_edits_off` | the connection's `edit_events` is off |
| 403 | `calendar_read_only` | `caps.create` (`POST`) or `caps.update` is false, or the calendar answered 403 (the caps are cleared as in phase 1) |
| 400 | `invalid_input` | malformed body; a bad `clientId`; a `calendarUrl` that is not a shown calendar of the connection (field `calendarUrl`); the phase-1 time rules; an over-long `location` or `description` |
| 422 | `event_not_editable` | `PATCH`: locked, or `description` on a body that is not plain text (reason `description_rich`) |
| 409 | `event_changed` | `PATCH`: stale `rev` or a remote 412 |
| 404 | `event_gone` | the calendar no longer has the event (`GET`, `PATCH`); `POST`: this `clientId` was created before and the event has been deleted since |
| 429 | `rate_limited` | over the shared limit |
| 502 | `calendar_unreachable` | the calendar failed; the message is the reason code (`calendar_missing` when the calendar is gone, `href_taken` for a CalDAV object address held by a different UID) |

Order of checks for `POST`, under `Worker.lock(connID)`: body rules (400) -> connection (404) -> `edit_events` (403) -> the calendar is shown (400 `calendarUrl`) -> `caps.create` (403) -> **replay** (the `calendar_event_created` record: cached event found gives 200, record found but event gone gives 404 `event_gone`) -> `EventWriter.Create` -> cache and record in one transaction -> `Notify` once. The handler records `calendar_event.changed` with `event_action` `created` (a new value of the existing detail key, never a title) only for a 201.

### D5. Go additions

`internal/calsync/events.go` (verbatim; the rest of the file is unchanged):

```go
// EventEdit is a change; a nil field stays as it is.
type EventEdit struct {
	Summary     *string
	When        *EventWhen
	Location    *string // "" clears it
	Description *string // plain text; "" clears it
	Free        *bool   // true: free (TRANSP:TRANSPARENT, showAs free); false: busy
}

// EventNew is an event to create in a calendar.
type EventNew struct {
	UID         string // the client id: the CalDAV UID and object name, the Graph transaction id
	Summary     string
	When        EventWhen
	Location    string
	Description string
	Free        bool
}

// EventState is an event as the remote holds it after a read or a write.
type EventState struct {
	Ref    EventRef
	Event  calfeed.Event
	Locked string // "" or a reason of design D2 of calendar-event-edits
	// Description and DescriptionLocked are filled by Detail only. DescriptionLocked
	// is true when the description is a rich-text body that plain text would flatten.
	Description       string
	DescriptionLocked bool
}

// EventWriter changes events of one connection's calendars. Every write is
// conditional on ref.ETag, and a create on the event not existing yet.
type EventWriter interface {
	Read(ctx context.Context, ref EventRef) (EventState, error)
	// Detail is Read with the description.
	Detail(ctx context.Context, ref EventRef) (EventState, error)
	Update(ctx context.Context, ref EventRef, edit EventEdit) (EventState, error)
	Delete(ctx context.Context, ref EventRef) error
	// Create writes ev into the calendar (its URL, or the Graph calendar id) and
	// returns it as the account holds it. When ev.UID was created before it returns
	// the existing event's state together with ErrEventExists.
	Create(ctx context.Context, calendar string, ev EventNew) (EventState, error)
}

var ErrEventExists = errors.New("calendar event exists") // a create met the same UID
```

`internal/calsync/event_edit.go`:

```go
// Added to the phase-1 sets.
const (
	LockRichDescription = "description_rich" // an EventError reason for EventNotEditable
	EventInvalid        = "invalid_input"    // an EventError code; Reason names the field
)

// EventResult gains whether the call created the event (false for a replay).
type EventResult struct {
	Event        calfeed.EventRow
	Replaces     string
	ConnectionID string
	Calendar     string
	Provider     string
	Created      bool
}

// EventDetail is an event with the description the editor shows.
type EventDetail struct {
	EventResult
	Description         string
	DescriptionEditable bool
}

func (w *Worker) CreateEvent(ctx context.Context, userID, connID, calendarURL string, ev EventNew) (*EventResult, error)
func (w *Worker) EventDetail(ctx context.Context, userID, connID, eventID string) (*EventDetail, error)
```

`internal/calfeed/managed.go`:

```go
// AddEvent stores a created event and its remote ref, and records that connectionID
// created it for clientID, in one transaction; it deletes records older than 30 days.
func (s *Store) AddEvent(ctx context.Context, subscriptionID string, occ Occurrence, remote EventRemote, connectionID, clientID string) (EventRef, error)

// CreatedBy returns the subscription and uid recorded for clientID.
func (s *Store) CreatedBy(ctx context.Context, connectionID, clientID string) (subscriptionID, uid string, ok bool, err error)

// RefByUID returns the ref of the single event of a subscription with that uid;
// ErrNotFound when it is gone.
func (s *Store) RefByUID(ctx context.Context, subscriptionID, uid string) (EventRef, error)
```

`Worker.EditEvent` keeps its signature; `check` is shared. `EventResult` and `EventDetail` are what the REST handlers, the agent editor and the CLI's routes use, as in phase 1. Files: new `event_create.go` (the Worker side, about 150 lines); `event_dav.go` and `event_graph.go` grow by `Create`, `Detail` and the new fields and are split into `event_dav_create.go` / `event_graph_create.go` if either passes about 350 lines (the `internal/codesize` limits are 150 lines a function, 1,100 a file). Every fake `EventWriter` in tests gains the two methods.

### D6. CalDAV and Google (`davEvents`)

- **Create.** Build one VCALENDAR (`VERSION`, `PRODID: caldav.ProdID`), a `VTIMEZONE` through `addZone` for a zoned point, and one `VEVENT`: `UID`, `DTSTAMP`, `CREATED`, `LAST-MODIFIED`, `SEQUENCE:0`, `SUMMARY`, `DTSTART`, `DTEND` (`pointProp`), `LOCATION` and `DESCRIPTION` only when not empty, `TRANSP:TRANSPARENT` for free and `TRANSP:OPAQUE` otherwise. No `VALARM`, no `ATTENDEE`, no `ORGANIZER`. `PUT <calendar><uid>.ics` with `If-None-Match: *` through `DAV.put(..., "*", ...)`. A 2xx is success; the ETag comes from the answer, else from one `GET`. A server that gives none leaves the event read-only (`no_etag`).
- **412.** The object exists: `GET` it. A `VEVENT` with the same UID is a replay (return its state with `ErrEventExists`); any other UID is a different event at that address: `*Error{Reason: "href_taken"}`. 403 is `ErrCalendarRO`; 404 on the PUT is `ReasonCalendarMissing`.
- **Update** gains `LOCATION`, `DESCRIPTION` and `TRANSP` through `setProp` (keeping parameters such as `LANGUAGE`; an empty value deletes the property). When the description changes, `X-ALT-DESC` is deleted so a client does not keep showing the old HTML. Everything else survives as in phase 1.
- **Detail** is `read` plus `DESCRIPTION` from the parsed component; `DescriptionLocked` is false.

### D7. Microsoft Graph (`graphEvents`)

- **Create.** `POST /me/calendars/{id}/events` (`calendarPath(cal)+"/events"`) with `subject`, `start`, `end`, `isAllDay` (the existing `point` helper), `location: {displayName}`, `body: {contentType: "text", content}`, `showAs: "free" | "busy"` and `transactionId: "opentodo-ev-<uid>"`. A 201 returns the event; its id becomes the cached `uid`. The record of D2 is the idempotency guard we can test; `transactionId` is the remote-side second guard (whether Graph returns the original event or an error on a repeat is unverified: open question 2). 403 is `ErrCalendarRO`, 404 `ReasonCalendarMissing`.
- **Update** gains `location`, `body` and `showAs` members, sent only when changed. Because Graph events are read in UTC (`graph_events.go:85`), a move writes `TimeZone` as the editor's zone (a Windows id) or UTC, as phase 1 does.
- **Detail and the description.** `GET /me/events/{id}` with `$select` = `eventSelect` plus `body`. A `text` body is the description and is editable. An `html` body is read again with `Prefer: outlook.body-content-type="text"`: if that text is empty (Outlook stores an empty body as an HTML skeleton) the description is `""` and editable, else it is shown as plain text and `DescriptionLocked` is true, because saving plain text would drop its formatting. `Update` with a description repeats the same check on the event it pre-reads, so a stale client cannot flatten a rich body (`ErrEventLocked`, reason `description_rich`).
- **Busy or free.** `free` maps to `showAs: "free"`; `false` to `"busy"`. Any other `showAs` (`tentative`, `oof`, `workingElsewhere`) reads as busy; the editor sends `free` only when the person changed it, so those values survive an edit of something else.

### D8. Moving an event between calendars: deferred

Not in this change, for three reasons. (1) There is no atomic move: CalDAV needs a `PUT` into the new calendar then a `DELETE` of the old one with ETags on both, and a failure between them leaves two events (or none, if the order is reversed). (2) Graph has no event move; create-and-delete would drop properties this product does not model (categories, reminders, online-meeting links, attachments, sensitivity), which is data loss. (3) It adds a second id swap, a second ETag precondition and the rollback of a half-done move to the outbox, for a need that **Duplicate** into another calendar followed by Delete already meets with every step visible and undoable. The editor's calendar control is a picker when creating or duplicating and a plain label when editing. `PATCH` does not accept a calendar field. A later phase can add a CalDAV-only move (copy the whole object, then delete) once the half-failure story is designed.

### D9. Web data layer (`calendarEventEdits.ts`, `types.ts`, `api.ts`, `calendarFeeds.ts`)

No Dexie version: **Dexie stays at 31** and `mobile/app/src/storage/surface.json` is unchanged (the surface lists tables and indexes; `EventCommand` rows are free-form beyond `key` and `notBefore`).

```ts
// types.ts
export interface CalendarEventChange {
  summary?: string
  start?: CalendarWhen
  end?: CalendarWhen
  allDay?: boolean
  location?: string
  /** Not part of the cached row: carried by the command only (calendar-event-editor D2). */
  description?: string
  free?: boolean
}

/** An event made on this device, not yet in the calendar. */
export interface CalendarEventDraft {
  clientId: string
  connectionId: string
  calendarUrl: string
  subscriptionId: string
  summary: string
  start: CalendarWhen
  end: CalendarWhen
  allDay: boolean
  location: string
  description: string
  free: boolean
}

export interface EventCommand {
  /* key, connectionId, eventId, rev, base, next, notBefore, sending, tries: as in phase 1 */
  patch: CalendarEventChange | { delete: true } | { create: CalendarEventDraft }
}
```

A create command has `eventId = "pending:" + clientId`, `key = connectionId + ":" + eventId`, `rev = ""`, and `base` = the pending row. `api.ts` gains `createCalendarEvent(connectionId, body)` and `calendarEventDetail(connectionId, eventId)`, and `updateCalendarEvent`'s body type takes the new fields.

- `createEvent(draft)` (new `web/src/lib/eventCreate.ts`, small, imported by the lazy editor and the palette): in one transaction puts the pending row and the command (`notBefore = now + EDIT_SETTLE_MS`), writes the two meta rows of D1, and returns the pending row.
- `editEvent` on a pending row merges the change into `draft` (later fields win); the draft keeps its `clientId`, so a retry after a merge is still the same event. `{delete: true}` on a pending row that has not been sent deletes the command and the row (no request, nothing to undo on the server). In flight, both go through `next` as in phase 1.
- `flushEventEdits` / `sendOne` handle `create`: `api.createCalendarEvent`; 200 and 201 both `settle` (a replay is a success). 400, 403, 404 (`event_gone`, `not_found`), 409 and 422 roll back: the pending row is deleted, the command removed, and the notice carries the `draft`, so the toast offers **Edit again**, which reopens the editor on it. Network errors and 5xx keep the command (backoff as in phase 1).
- `refreshEvents` re-applies pending creates after a fetch (puts each pending row back), as it re-applies queued patches. A refresh that was already in flight when a create settled is repaired by the `calendar` live event the create itself causes.
- `eventPatchBetween` and `applyEventPatch` learn `location` and `free`; `description` is never on a row, so a command whose only change is a description is never empty and never "returns to base". The inverse for an Undo of a description edit is built by the editor, which holds the text it loaded.
- Undo of a create: while the command is queued, remove the command and the pending row; after the swap, the normal delete of the real event (8 s window) with the toast "Event removed".
- The phase-1 rule "an edit the server refuses restores `base`" is unchanged for existing events.

### D10. Creating from the grid

Files: `addHere.tsx`, `AddOnDayDialog.tsx`, `DayColumn.tsx` (`addHandlers`), `calendarDrag.ts`.

- **Gestures.** Double-click, `q` (the existing `calendar.quick-add`, its description widened to "add a task or event") and the touch long-press menu open the **remembered kind**. The right-click and long-press menu lists both **Add task here…** and **Add event here…** (`data-add-here="event"`), the remembered kind first; with no writable calendar the menu is exactly today's.
- **The chooser.** `AddOnDayDialog` shows a two-option control **Task | Event** in its header only when the writable list is not empty; choosing Event closes the dialog and opens the event editor at the same place (the typed text is not carried: the task line is parsed text and the event title is a field). The editor in create mode has the same control and switches back by reopening the dialog. Every explicit choice writes `calendar.newKind`.
- **Drag to create (fine pointer, a writable calendar exists).** A press on an empty place that moves 6 px or more vertically draws a ghost block snapped to 15 minutes (at least 15 minutes, at most to midnight) with its time range; edge scroll and Escape work as for block drags (the sweep is a new `kind: 'create'` of `useCalendarDrag`, `entity: 'range'`). Releasing opens the remembered kind with that start and end (a task gets the start as its time and the length as its estimate). Touch keeps the long press and does not sweep (a vertical drag on touch scrolls). With no writable calendar a press does nothing new: there is no sweep for task-only users (open question 1).
- **Keyboard path and alternatives** (WCAG 2.5.7): `q` and **`Shift+Q`** (new `calendar.new-event`: "In Day and Week: add an event on the focused day", at the next free half hour of that day, or 09:00 for another day), the palette's **New event**, the menu item, and the editor's own date and time fields. The drag-alternatives inventory gets a `calendar-create` source (D16).

### D11. The full editor

A new lazy chunk: `web/src/app/calendar/EventPanel.tsx` (the shell) and `EventForm.tsx` (the fields), opened through `openEventEditor(request)` in `web/src/app/calendar/eventEditor.ts` (a small store: `{mode: 'create', place, draft?} | {mode: 'edit', eventId}`).

- **Placement.** From 1024 px (`SPLIT_QUERY`) `openPanel('event-editor', ...)` puts it in the shell's slot inside `PanelFrame`, replacing whatever custom occupant is there (the planning list stays remembered open and does not reopen by itself). Below 1024 px it is the overlay module's `Dialog`, a bottom sheet on a phone. One form, two frames.
- **Opening.** Enter or a click on a block still opens the phase-1 popover (its quick fields are the drag alternative and keep `data-event-edit`); the popover gains **All details**. A double-click on a block, an all-day chip or a Month or Plan row, and Enter on a chip, open the editor. All-day and multi-day events open only here (drags and the Delete key stay as they are). Month, Year and Plan rows still do not drag.
- **Fields**, top to bottom: title (focused); **All day** switch; start date and time, end date and time (native `date` and `time` inputs, as the popover); **Time zone** (the existing `ZoneControl` picker); **Calendar**; **Location**; **Description** (a textarea, plain text, 10,000 characters); **Show as** Busy | Free. Footer: **Add event** or **Save**, and an overflow menu with **Duplicate**, **Convert to task…** and **Delete**. Cmd or Ctrl+Enter saves; Escape closes (a dirty form asks "Discard changes?" first).
- **Time zone semantics.** The times are in the selected zone. New events default to the viewer's zone. An existing event shows its own zone when it has a real one, with a second line "10:00 in your time" when that differs. A UTC event (Graph, or a `Z` time) is shown and edited in the viewer's zone (the instant does not change); a floating event shows "Floating" and keeps it unless the person picks a zone. Changing the zone keeps the wall-clock numbers and moves the instant, and the "in your time" line shows the effect at once. Start and end always share the zone. Only changed values are sent: an untouched start and end are not in the `PATCH`.
- **All-day.** On: dates only, the end date shown inclusive (the wire end is the day after). Off: 09:00 to 10:00 on the start date. Turning it on or off sends `allDay` with both points, as phase 1's `PATCH` requires.
- **Calendar.** A picker (colour dot and name) over the writable list when creating or duplicating; a plain label when editing (D8). The choice becomes the default for next time (D1).
- **Description loading.** In edit mode the editor calls `GET .../events/{eventId}` on open; the field shows a skeleton, then the text. On failure, offline, or when `descriptionEditable` is false, it is read-only or disabled with the reason and left out of the `PATCH`. `replaces` in the answer swaps the cached row before the form binds `rev`.
- **Save.** Create: `createEvent(draft)`, close, toast "Event added" with Undo (D9). Edit: one `changeEvent` with the changed fields, the toast and inverse of phase 1 (plus the previous description in the inverse). A pending event's Save merges into its draft.
- **Duplicate.** Reopens the editor in create mode with the same title, times, zone, location, description and Busy or Free, in the same calendar (the default one when that is not writable), with a new `clientId`. Offered for any event when a writable calendar exists, including locked and ICS events (it only creates), from the editor's menu or the popover's overflow. It copies one occurrence as a single event; guests, reminders and links are not copied.
- **Convert to task.** A small confirm popover: **Create task**, and a switch **Delete the event from <calendar>**, on by default when `access.delete` is true (off and disabled otherwise, and for connections with edits off), remembered per device. The task is made with `addTasksUndoable` (ops, local-first) from the form's current values: title; description = the event's description followed by `Location: <place>` when set; for a timed event `due` = start with time and zone and `estimate_minutes` = its length (capped to the field's limit); for an all-day event `due` = the last day, with `start` = the first day and `span: true` when it covers several days. No project is given, so the task lands where an unplaced task lands (Inbox). If the switch is on, the event is deleted through `deleteEvent` (the 8 s outbox delay). **One Undo**, in one toast ("Task created"), reverses both: it moves the task to the Trash through its undo entry and calls `cancelEventDelete`, so within the 8 seconds the event is untouched; after them the delete has been sent and cannot be taken back (phase 1's rule: a sent delete is final, and re-creating from the fields we know would lose the ones we do not model). Default-on is justified by what "convert" means, and because a timed task left beside its event is written to the calendar a second time when task write-through is on; it is made safe by the confirm step, the visible switch label and the 8 s window. Convert is also in the popover's overflow for events that cannot be edited (it only reads; the switch is off there). Offline: the task is made now, the delete waits in the outbox.
- **Pending events** open the editor on their draft: edits merge, Delete discards, Duplicate works, the description is the draft's.

### D12. Command palette

`registry.ts` gets `{ id: 'new:event', label: t('New event'), icon: 'calendar-plus', keywords: 'event calendar meeting add create', run }`, present only when `PaletteContext.eventsWritable` is true (a boolean from `useWritableCalendars().length > 0`, set where the context is built). `run` is `() => import('../calendar/newEventNow').then((m) => m.newEventNow())`, so the entry chunk holds the command and nothing else. `newEventNow` computes the next free half hour today (the next `:00` or `:30` with no task block or event in it; 09:00 tomorrow when none is left before midnight), turns calendars on, navigates to the Day view when the app is not in Day or Week (as `newSlotNow` does), and opens the editor in create mode in the default calendar. The navigation commands are unchanged.

### D13. Agents and the CLI

- **MCP** (`internal/agenttools/write_calendar_events.go`, `calendar_events.go`): `create_calendar_event` `{connection_id, calendar_url, title, start, end, all_day?, location?, description?, free?, client_id?}`; `client_id` is an optional idempotency key an agent reuses when it retries (the tool makes a UUID when absent). `update_calendar_event` gains `location`, `description` and `free`; its description says `description` replaces the whole text. `list_calendar_events` gains a top-level `calendars` array `{connection_id, calendar_url, name, can_create}` so an agent can choose where to create. `EventEditor` gains:

  ```go
  Create(ctx context.Context, userID string, c EventCreate) (EventOutcome, error)
  ```

  with `EventCreate` the tool's arguments in agenttools types (the package still does not import `calsync`), and `EventUpdate` gains `Location, Description *string` and `Free *bool`. The same write gate (`Caller.WriteDenial`), per-token limits (one item), `agent.write` audit, 600 an hour limiter and `calendar_event.changed` (`created`) as phase 1. **Not a routine step**: the tool joins the exclusion list in `internal/rules/routine_tools_test.go` with the reason "an event in a connected calendar is the person's to make". The in-app assistant does not offer it (`notInApp`), like the other calendar event tools. The tool counts in `internal/httpapi/mcp_test.go` and `mcp_write_test.go` go up by one write tool.
- **CLI** (`internal/cli/cmd_calendar_events.go`): `ot calendar event add <title> --start DATE[Thh:mm] [--end DATE[Thh:mm] | --for 45m] [--all-day] [--calendar <name or url>] [--location T] [--description T] [--free]`; `ot calendar event edit` gains `--location`, `--description`, `--free` and `--busy` (a flag is applied when given, so `--location ""` clears). `--calendar` is matched against the writable calendars from `GET /calendar-sync/connections` (a single writable calendar needs no flag; an ambiguous name exits 2 and lists the choices). A full token is required; exit codes as phase 1 (`event_edits_off` and `calendar_read_only` exit 5). Each run of `add` makes a fresh `clientId`; `--client-id` is accepted for scripts that retry. Completion learns the new subcommand.

### D14. E2EE

Events are server data the server already holds in the clear. A created event's title, location and description go to the remote calendar as typed, as phase 1's rename does, so an encrypted account's server learns nothing it did not know, and no event field is a content field (`internal/e2ee/contentfields.go` is unchanged). **Convert to task** writes the task through the normal local path, so on an encrypted account its title and description are encrypted like any task's before they leave the device. `create_calendar_event`, like the update tool, has no content fields to rewrite (`internal/agenttools/encrypted.go`).

### D15. Size, interface budget and operations

- **Server.** No dependency. About 700 lines of Go outside tests, one small table. Nothing resident in memory.
- **Web.** The editor, form, convert action and the reuse of `ZoneControl` are one lazy chunk, expected 9 to 12 KB gzip, outside the first load. In the first-load entry: the palette command and its context field, the `Shift+Q` registry entry, `useWritableCalendars` and the dynamic imports, expected 0.4 to 0.7 KB gzip. The entry chunk has 0.1 KB of headroom, so the change raises `entryGzipKB` in `web/bundle-budget.json` by up to 0.7 KB **on purpose**, with a dated note as phase 1 wrote, after `make bundle-budget` reports the real delta. If `addHere` / `AddOnDayDialog` turn out to sit in the entry and not in the calendar chunk, the chooser is lazy-loaded first.
- **Interface budget** (`docs/interface-guide.md`): 0 new rows of chrome above the grid (the Task or Event control lives inside the existing add dialog; the editor is a panel or sheet, not chrome); 0 new task properties; 0 new sidebar destinations; at most 3 scroll regions (the grid, plus the panel body, which takes the slot the planning list uses); one new shortcut, `calendar.new-event` (`Shift+Q`), registered in `web/src/lib/shortcuts.ts` with no conflict (`docs/keyboard.md` is generated), and `q` keeps its id.
- **Rate limits and audit.** One shared limiter; `calendar_event.changed` with `event_action: created`.

### D16. Accessibility, screens and translation

- `scripts/ux-audit/drag-alternatives.json`: a new source `calendar-create` ("An empty stretch of the Day and Week grid: drag to size a new task or event") with the alternatives `{file: web/src/app/calendar/addHere.tsx, contains: 'data-add-here="event"'}` and `{file: web/src/app/calendar/EventForm.tsx, contains: 'data-event-form-start'}`; `web/src/lib/a11y/dragAlternatives.test.ts` checks the text exists. The existing `calendar-event` entry stays.
- `scripts/ux-audit/screens.json`: `event-panel` (route `day`, kind `overlay`, `frequency: "rare"`, `core: false`, `layoutCheck: "event-panel"`, `on: "day"`, story `Screens/Calendar`), with its handler in `layout-screens.mjs` (double-click the `layout-edit` event, wait for `[data-event-form]`) and `seed.mjs` / `tour.mjs` answering the `GET .../events/{id}` route; `event-editor` (the popover) stays. The three-width tour covers the sheet at phone width. `docs/accessibility.md`'s screen table gets the row.
- The Task or Event control is a radio group with visible labels; the sweep ghost is announced through the live region the block drags use ("New event, 14:00 to 15:00"); the pending block is `aria-busy`.
- **Translation.** Every new string goes through `t()` and into `web/src/locales/messages.json` (`node scripts/i18n/extract.mjs --write`) and **all six catalogs** (`de`, `es`, `fr`, `it`, `pt`, `zh`); `make i18n-check` fails on a missing entry. Translations are drafted for the pull request and reviewed by a speaker of each language (`docs/translating.md`); none is machine-made into a catalog unreviewed. Messages with a count or a calendar name use placeholders, never glued pieces.

### D17. Test plan

Go (`go test ./...`): `internal/store/calendar_event_created_migration_test.go` (runs twice, data kept, phase-1 queries pass); `internal/calfeed` tests for `AddEvent` (row, ref and record in one transaction, rollback leaves none, 30-day prune), `CreatedBy`, `RefByUID`; `internal/calsync/calsynctest` gains a switch to serve 2xx without an ETag, `graphtest` gains `POST .../events`, `transactionId`, `body` and `Prefer` support; `event_dav_test.go` and `event_graph_test.go` for Create (VCALENDAR content, `If-None-Match: *`, zoned, floating and all-day, `href_taken`, replay, 403, 404), Update of location, description and free (alarms and `X-` properties survive, `X-ALT-DESC` removed), Detail (HTML body empty and non-empty); `event_create_test.go` (every check in D4's order, replay 200, replay after delete 404 `event_gone`, one `Notify`, two goroutines on one connection make one event, an unshown calendar 400, caps false 403); `internal/httpapi/calsync_event_create_test.go` (every row of the table, tokens, demo, rate limit, audit `created`, `GET` with a newer remote version); `internal/agenttools` and `internal/cli` tests; `internal/httpapi/testdata/v1_surface.json` regenerated with `-update`.

Web (`npm test`, `npm run build` in `web/`): `eventCreate.test.ts` and an extended `calendarEventEdits.test.ts` (fake-indexeddb: create queued offline then online, two edits and a delete merged into a create, delete of a pending event sends nothing, in-flight create plus `next`, a 200 replay, a refusal returns the draft, a refresh keeps pending rows, Undo before and after the swap, sign-out clears); `eventDrop.test.ts` (all-day switch, a zone change keeps the wall clock, a UTC event edited in the viewer's zone); `EventPanel.test.tsx` and `EventForm.test.tsx` (fields, description load and failure, dirty-close confirm, Duplicate, Convert with one Undo that cancels the delete, the switch disabled for a locked event); `calendarAdd.test.tsx` (the chooser only with a writable calendar, remembered kind, the sweep opens the editor with start and end, no sweep without a writable calendar, Escape cancels); `palette.test.tsx` (New event present only when writable, the next half hour); the shortcut registry test for `calendar.new-event`; `dragAlternatives.test.ts`; stories for the panel states (the `projectedBlock.stories.tsx` pattern) and UI baselines regenerated through the workflow phase 1 used. Then `make ux-audit`, `make i18n-check`, `make bundle-budget`, `make check`.

## Risks / Open questions

1. **Drag-create for task-only users.** The brief says nothing may change for people without a writable calendar, so the sweep exists only with one. If that feels arbitrary in use, a task-only sweep (start and estimate) is a one-line gate removal and a spec change.
2. **Graph `transactionId` on a repeat** is unverified (the original event or an error). The D2 record answers a replay before Graph is called, so correctness does not depend on it; verify once against a real mailbox, as phase 1's manual run still is.
3. **Google's CalDAV** may normalise a created object (add a `VALARM` from default reminders, rewrite the UID). The post-create `GET` is what the cache stores, so the grid shows what Google kept; a rewritten UID cannot cause a duplicate on replay because the record keys the client id, not the UID.
4. **Rich-text Graph bodies** are shown but not edited; people will ask. Phase 4 can add rich text.
5. **UTC zones.** Editing the time of a `Z`-time event in the viewer's zone rewrites it as a `TZID` time on save. It is the same instant; a strict client could notice. Untouched times are never rewritten.
6. **One slot.** Opening the event panel replaces the planning list in the slot until it is closed.
7. **Convert default-on** deletes remotely after 8 seconds. If feedback says it surprises people, the default flips with no other change.

## As built (groups 1–3)

Server half, in the order of the tasks. Where this differs from the decisions above, this section wins.

- **Store (D2, D5).** `0067_calendar_event_created.sql` is the statement of D2 verbatim. `calfeed.Store.AddEvent`, `CreatedBy` and `RefByUID` are D5 verbatim; `ReplaceEvent` and `AddEvent` share one `insertEvent`. `RefByUID` returns the first instance by key when a uid has several (a created event has one).
- **Test servers (task 1.4).** `calsynctest.Server.NoETagOnPut` already existed; it gained `NoETagOnGet` (a server that never gives an ETag), `IfNoneMatches()` (the header of every PUT) and `SetReadOnly(cal, bool)` (a 403 on writes while the privilege set still claims write). `graphtest` already had `POST .../events` with `transactionId`; it gained `Event.BodyType`, `Location` and `ShowAs`, `location`, `showAs` and `body.contentType` in the request and the answer, and the `Prefer: outlook.body-content-type="text"` read of an html body (markup stripped, an Outlook skeleton reads as `""`). Each has a `server_test.go` self-test.
- **Writers (D6, D7).** `davEvents.Create` is in `event_dav_create.go`, `graphEvents.Create` and `Detail` in `event_graph_create.go`; the new members of `Update` are `editText` (DAV) and `editMembers` (Graph). `href_taken` is `calsync.ReasonHrefTaken`. A DAV create whose `PUT` and following `GET` give no ETag is returned with `Locked = no_etag` (read-only), not refused. A Graph create does not return `ErrEventExists`: Graph's repeat of a `transactionId` is unverified (open question 2) and the fake returns the first event with `201`; the `calendar_event_created` record in `Worker.CreateEvent` is the replay guard. A Graph `Update` with a description pre-reads the body (`Detail`) and answers `ErrEventLocked` with `description_rich` after the ETag and lock checks, so a stale client gets `event_changed` first.
- **Worker (D4, D5).** The `EventError` code `EventInvalid` (`invalid_input`) carries the field name in `Reason` (`clientId` or `calendarUrl`); `CreateEvent` checks the client id itself (`calsync.ValidClientID`) so the agent tools and CLI cannot bypass it. A `404` for the object between a `412` and the `GET` that follows is reported as `calendar_unreachable`, never as `event_gone`. `EventDetail` stores the remote's newer state and wakes devices only when the ETag changed; `Replaces` then names the old id, which can equal the new one (a rename keeps the start, so the id), and `DescriptionEditable` is `!DescriptionLocked` whatever the event's lock.
- **Routes (D4).** `calsync_event_create.go` holds `POST` and `GET`; `eventPatch.editOf` and `eventCreate.eventOf` share `whenOf`, `summaryOf` and `textOf`. `location` is trimmed, `description` is kept as typed; both refuse a NUL; limits are in characters (500 and 10,000). `GET` is not rate limited (the limiter counts `POST`, `PATCH` and `DELETE`). No new error code was needed, so `server-error-codes.json` and `errors.ts` are unchanged. The audit detail `event_action` is documented as `created | updated | deleted`; the web formatter for `created` is the web half's.
- **Docs touched early.** `docs/api.md` got a row each for `POST` and `GET` and the new `PATCH` members (the route-coverage test needs them); the full text, examples and error table stay in task 7.3.

## As built (group 6)

Agent tools and CLI. Where this differs from D13, this section wins.

- **`EventEditor`** gained three methods, not one: `Create`, `ValidateCreate` (the POST body rules without writing, used at planning like `Validate`) and `Calendars`. `EventOutcome` gained `Replayed`. `EventUpdate` has `Location`, `Description *string` and `Free *bool`.
- **`create_calendar_event`** is in `internal/agenttools/write_calendar_events_create.go`. The file name matters: tools list in `init` order, which is file name order, and a name before `list_*` and `read_*` put a write tool among the reads. Without `client_id` the tool makes a UUID. The answer is `{event, client_id, replayed}`; a replay counts as 0 changed items (nothing is written or audited), a first create as 1.
- **`calendars`** in `list_calendar_events` are the shown calendars of the user's connections with `caps.create`, `can_create` being the connection's "Edit events" switch (so an agent sees that the switch is off instead of the calendar missing). It is `[]` when calendar sync is off.
- **Errors.** A calendar url or client id the worker refuses (`EventInvalid`) is an `ArgError` naming the field and pointing at the `calendars` array; the other codes are those of `update_calendar_event`.
- **CLI.** `add` with a timed start needs `--end` or `--for` (no default length); `--start` needs a time unless `--all-day`; an all-day event takes `--end` (default: one day) and refuses `--for`. Timed events carry the local zone (`localZone()`). `--calendar` matches a url or name exactly (any case), then a part of the name, over the shown writable calendars of `GET /calendar-sync/connections`; no match or several exits 2 and lists the choices. `edit` applies `--location` and `--description` when given (so `""` clears), `--free` and `--busy` exclude each other.
- **Not changed.** `create_calendar_event` is in `notInApp` and the routine exclusion map; the disclosure test lists only read tools, so it needed no entry. Docs (`docs/mcp.md`, `docs/cli.md`) are group 7.

## As built (groups 4–5)

Web half, in the order of the tasks. Where this differs from D1, D3 and D9 to D12, this section wins.

- **Data layer (D1, D3, D9).** `CalendarEventChange`, `CalendarEventDraft` and the `create` member are D9 verbatim, with two more types: `CalendarEventCreateBody` (the draft without `connectionId` and `subscriptionId`, built by `createBodyOf`) and `CalendarEventDetail`. The create branch lives in `calendarEventEdits.ts`; `createEvent`, `draftFromFull`, `draftFromEvent`, `nextHalfHour` and `eventBusyOn` live in `lib/eventCreate.ts`, and the writable-calendar read, the two meta rows and their hooks in the small `lib/writableCalendars.ts` (first load; `eventCreate` re-exports it). The pending row (`pendingRowOf`, `PENDING_PREFIX`) and `applyDraftPatch` are in `calendarFeeds.ts` because `refreshEvents` needs them. New exports of `calendarEventEdits.ts`: `armTimer`, `adoptEvent` (the editor's swap for `replaces`, which can equal the new id), `queuedDraft`, `createBodyOf`, `isCreate`. A description is never on a row, so `isNoChange` (not `Object.keys(eventPatchBetween(...))`) decides whether a command is empty, in `editEvent` and in `settle`.
- **Create order.** `createEvent` gives each create a `notBefore` of at least one more than the last queued create, so two creates made in the same tick are sent in order (the outbox index sorts equal times by key).
- **Refused create.** `EventEditNotice` gained `draft` (the draft with what waited in `next` applied; absent when the person had already discarded it). `ToastActionHost` shows it as an Undo-style toast with **Edit again**, which goes to the Day view of the draft's date and reopens the editor in create mode. The editor gives every Save in create mode a fresh client id, so Edit again after a 404 `event_gone` is not refused by the old one. Client ids come from `uuid()` (`lib/ids.ts`), not `crypto.randomUUID`, so plain-http installs work (`plainHttp.test.ts`).
- **Writable calendars (D1).** Listed by name: the cache is a Dexie table keyed by id and does not keep the order the server served them in, so "the first of the first connection" is the first by name. Screens may not import `db` or `useLiveQuery` (`layering.test.ts`), so `useWritableCalendars.ts` in `app/calendar` re-exports the hooks of `lib/writableCalendars.ts`; the editor's detail read is `lib/eventDetail.ts` with a thin hook beside the editor.
- **Form helpers (D11).** In `lib/eventFull.ts`, not `eventDrop.ts`: they are only needed by the lazy editor and added 0.9 KB gzip to the first load there (`eventFullOf`, `eventFullChange`, `switchAllDay`, `pointsOf`, `startInViewerZone`). A floating event shows "Floating" and offers it among the zones; every other event picks from the platform's zones. The zone picker is a Combobox in `EventZoneField.tsx` over `allZones` and `zoneOffset` of `ZoneControl`, because `ZoneControl` itself writes a task's due and cannot edit a form value.
- **Editor files (D11).** The store is `eventEditorStore.ts`, not `eventEditor.ts`: the phase-1 popover form is `EventEditor.tsx` and the two names differ only in case, which a case-insensitive checkout and `forceConsistentCasingInFileNames` refuse. `EventEditorHost.tsx` (new, mounted by `useCalendarChrome().element`) puts the editor in the side slot from 1024 px and in a `Dialog` (`placement="sheet"`, a bottom sheet on a phone) below, and closes it when the calendar is left (Day and Week are one component; moving between two calendar views keeps a request opened in between, such as the palette's). The editor is `EventPanel.tsx` (frame, footer, overflow menu, discard confirm, Cmd or Ctrl+Enter), `EventForm.tsx`, `useEventEditing.ts` (the fields as an `edits` object over a `base`, so a description that arrives late does not disturb typing), `useEventDetail.ts`, `eventAdd.ts` (Add event and Undo of it) and `ConvertToTask.tsx` with `eventConvert.ts`; the popover's **All details**, Duplicate and Convert are `EventPopoverActions.tsx`, lazy with the popover. Duplicate copies the fields as the editor shows them (so an unsaved change is copied) into the event's own calendar when it is writable, else the default one.
- **Convert (D11).** The switch is remembered in `localStorage` (`opentodo.calendar.convertDelete`), on by default. One toast carries one Undo (`action`, not the task's `undoId`) that runs `undoBatch` and then the delete's restore; its window is the 8 seconds when a delete is queued, else the usual 5. A delete that replaced a queued edit that carried a description loses that description if the delete is undone (the restore re-derives the edit from the row, which has none).
- **Grid (D10).** `useAddHere` returns `add` (the remembered kind), `addTask` and `addEvent` (the menu's items, each remembered), `kind`, `eventsOn` and `newEvent` (Shift+Q); the dialog's **Task | Event** control is in its header `actions`, the editor's own control appears only for a new event that is not a Duplicate and sends Task back through `addTaskRequest`. A sweep is `kind: 'create'`, `entity: 'range'`, started by the day column's `onPointerDown` (so the column carries `data-drag-id="calendar-create"`), waits for 6 px of vertical movement, works with a mouse or a pen, never with touch, and is offered only through `gesturesOf` while a writable calendar exists. It starts in the slot under the press and ends at the pointer rounded to 15 minutes (an upward sweep includes the pressed slot). The task path passes the sweep's length as the dialog's `estimate`. `nextHalfHour` returns the current half hour when it is exactly `:00` or `:30` and free.
- **Palette (D12).** The icon is `calendar-clock`: there is no `calendar-plus` in the icon set. `newEventNow(route)` keeps the Week view when the app is in it.
- **Audit.** `auditFormat.ts` words `created`. The server writes `updated` and `deleted` (not `update` and `delete`, which phase 1's formatter compared with); both spellings are now worded.
- **Bundle (D15).** Entry 143.4 to 143.50 KB (+97 bytes), first load 696.3 to 698.0 KB (+1.7 KB, 164 static chunks), measured against a build of this branch's base commit. `entryGzipKB` 143.6 and `initialGzipKB` 698.1, with the note in `web/bundle-budget.json`. The editor chunk is 4.4 KB gzip, beside shared modules that Rollup split out (the outbox, 2.5 KB, which the Shell already loaded).
- **Not done here.** `docs/accessibility.md`'s screen table row, `docs/keyboard.md` (generated; `Shift+Q` is not in it until `make docs`), the user docs and `make ux-audit` / `make layout-check` runs (the `event-panel` handler, its seed answer and the tour route are written but were not run; `scripts/e2e-layout.mjs` and `scripts/e2e-safe-area.mjs` do not answer the detail route, so there the editor's description reads as unavailable); UI baselines for the new `Screens/Event editor` stories come from the baselines workflow.
