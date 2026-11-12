# Design

## Context

Phases 1 and 2 (`calendar-event-edits`, `calendar-event-editor`, archived 2026-11-12) are the base: the remote calendar is the truth, an event write is a server command with an ETag precondition and not an op, `rev` is a hash of the ETag, a device queues one command per event in `eventOutbox`, the editor is a panel. This change reuses all of that. What the code does today, and the facts the design had to work around:

- **Every occurrence is locked as `recurring`.** `davLocked` (`internal/calsync/transport.go:164`) locks an object whose events have `RRULE`, `RDATE`, `EXDATE` or `RECURRENCE-ID`; `graphEvent.locked` (`graph_events.go`) locks every `type` other than `singleInstance`. The refs table holds the lock per uid (`calendar_event_remote.locked`, `0066`).
- **The cache holds expanded occurrences, not series.** `readCalendars` (`internal/calsync/shown.go:57`) reads each shown calendar with a `calendar-query` time-range REPORT (`DAV.query`, `remote.go:174`, no server-side expand, so a master and its exceptions arrive together) and runs `calfeed.ExpandContext` over `StoreWindow` (62 days back, 400 forward). It stores one `calendar_events` row per occurrence and one `calendar_event_remote` row per occurrence, all sharing the object's `href` and `etag` (`refs[ev.UID]`, `shown.go:92`). The master, its rule and its exceptions are not cached; a write has to read the object.
- **Instance keys are stable for exceptions.** `instanceKey` (`internal/calfeed/vevent.go:301`) is the UTC instant of the original slot (`...Z`) for zoned and UTC points, the wall time for floating, the date for all-day. `Expand` stores an exception under its `RECURRENCE-ID` (`expand.go:112-115`, `o.Instance = ov.RecurrenceID`), not under its new start. So `id = EventID(subscription, uid, instance)` does **not** change when one occurrence is moved as an exception. The "ids change when moved" rule of phase 1 holds for single events only (`OccurrenceOf` uses the start as the instance) and for Microsoft occurrences.
- **`Expand` already understands series.** `parseRule` supports `DAILY`/`WEEKLY`/`MONTHLY`/`YEARLY`, `INTERVAL`, `COUNT`, `UNTIL`, `BYDAY` with ordinals, `BYMONTHDAY`, `BYMONTH`, `BYSETPOS`, `WKST` and rejects `BYHOUR`, `BYMINUTE`, `BYSECOND`, `BYYEARDAY`, `BYWEEKNO` (`expand.go:253`); `EXDATE`, `RDATE` and `RECURRENCE-ID` overrides are applied. Gaps that matter for writing: `parseEvent` keeps only the last `RRULE` (`vevent.go:133`) so several rules are invisible; `Range` is read but `THISANDFUTURE` is only approximated (`expand.go:99`); a cancelled override is dropped.
- **`internal/recurrence` is not reusable.** It is the task grammar: date-only floating arithmetic, completion-relative rules, a closed set of shapes (`kInterval`, `kWeekdays`, `kMonthDay`, `kNthWeekday`...), mirrored to `web/src/lib/recurrence.ts` by shared vectors (`recurrence.go:1-10`). It has no time zones, `EXDATE`, `RECURRENCE-ID` or `BYSETPOS` beyond one shape, and `FromRRULE` returns only task rules. Extending it would break the mirror contract. Events use `calfeed`'s parser and expander, which the grid already trusts.
- **Writing a calendar object exists.** `davEvents.Update` (`event_dav.go:53`) reads the object, edits `VEVENT` properties with `setProp`, `replaceTimes`, `pointProp`, `addZone`, re-encodes the whole `VCALENDAR` with `Component.Encode` and `PUT`s it with `If-Match`. It requires exactly one `VEVENT` (`event_dav.go:77`). Create uses `If-None-Match: *` (`event_dav_create.go`, `DAV.put` with etag `"*"`). `Delete` sends `If-Match`.
- **Graph occurrences are separate events.** The window read (`graphTransport.window`, `graph_transport.go:164`) uses `calendarView` with no `$select`; each occurrence arrives with its own `id` and `@odata.etag`, becomes `Event{UID: e.ID}` (`graph_events.go`) and is cached like a single event with instance = start. `graphEvent` reads `type` but not `seriesMasterId`, `recurrence`, `reminderMinutesBeforeStart`, `isReminderOn`, `hasAttachments` or `isOnlineMeeting`.
- **Midnight-ending events are already editable.** `eventEditable` (`web/src/lib/eventDrop.ts:135`) accepts a timed event whose end is `00:00` on the next day; `blockOn` (`web/src/lib/calendar.ts`) clips it to the day and `eventDropChange` and `plus()` carry an end past midnight to the next day (`eventDrop.test.ts:42`). The server has no same-day rule (`whenOf`, `calsync_events.go:84`). A series at 23:30 to 00:00 is therefore not locked by the grid; only `recurring` locked it.
- **Spec drift to fix.** `calendar-feeds` "Events served to devices" and `calendar-event-edits` "Which events can be changed" name `recurring` as a reason; the web says "Repeating events are read-only for now" (`web/src/lib/calendarEventEdits.ts:270`).

## Goals / Non-Goals

**Goals:** make occurrences of repeating events changeable for this, following and all; set and change the repeat and reminders in the editor and the create flow; keep everything the editor does not model; never lose occurrences on a failed two-write change; keep the remote calendar the truth; stay downgrade-safe.

**Non-Goals:** guests, RSVP and conferencing (phase 4); ICS feeds; tasks' recurrence; moving a series between calendars; editing custom rules, `RDATE` series or `THISANDFUTURE` exceptions; delivering reminders.

## Decisions

### D1. Scopes, identity and what the client sees

`scope` is `this`, `following` or `all`. An event is "an occurrence" when the cache says it belongs to a series (D2). Rules:

- `scope` is **required** for an occurrence (400 `scope_required`, a new code) and **refused** for an event that is not (400 `invalid_input`, field `scope`). An agent that always sends it learns the rule from the error; the strictness catches client bugs the way phase 1's "nothing to change" does.
- **`following` on the first occurrence acts as `all`** (the first is the series' DTSTART, `first_instance` in D2), so no empty old series is made. A **delete** of `following` on the first occurrence, a delete of `all`, and a delete of the last remaining occurrence of a finite series delete the calendar object (`DELETE` with `If-Match`).
- **Identity.** `this` keeps the event `id` on CalDAV (the instance is the `RECURRENCE-ID`); the answer still carries `replaces` (equal to `id`). `all` that moves times changes the instance of every occurrence, `following` moves the later ones to a new uid, and Microsoft `this` changes the id as in phase 1 (instance = start). Rather than a list of `replaces`, the answer says `refetch: true` when other ids changed and the device refetches the shown range (D12): `refreshEvents` already replaces the table wholesale, so no new client code reads a list. The edited occurrence's new row is still returned as `event` so the open popover, selection and Undo follow it (`adoptEvent`, phase 2).
- **`series` on every served occurrence:** `{id, rrule?, repeat, custom, first, rev?}`. `id` is `hash16(subscription, uid)` for CalDAV and `hash16(subscription, seriesMasterId)` for Microsoft, the same for all occurrences, never the uid. `rev` is present for CalDAV: every occurrence of an object shares the object's ETag, so after any write the device gives all cached occurrences of `series.id` the new `series.rev` (a Dexie `modify`, no network) and a second drag on another occurrence of the same series is not refused as stale.

### D2. Migration `0068` and what the cache learns

Migration number **0068** (`0067_calendar_event_created.sql` is the last). The brief preferred none; the cache has nowhere to say "this row is an occurrence of a series" (`calendar_events` has no such column and phase 1 chose side tables on purpose), and encoding it in `locked` would overload a column the write checks read. One idempotent side table, no `ALTER`, as `0066` and `0067`:

```sql
-- 0068_calendar_event_series.sql
-- The series an occurrence of a managed calendar belongs to, so the events route can say
-- "this repeats" and the write routes can ask for a scope. Server data, never an op.
-- (calendar-event-series D2)
CREATE TABLE IF NOT EXISTS calendar_event_series (
  subscription_id TEXT NOT NULL REFERENCES calendar_subscriptions(id) ON DELETE CASCADE,
  uid             TEXT NOT NULL,            -- the cached uid (the CalDAV UID, or a Microsoft occurrence id)
  series_id       TEXT NOT NULL,            -- hash16: what the client groups occurrences by
  rrule           TEXT NOT NULL DEFAULT '', -- the master's RRULE as read ('' for Microsoft)
  repeat          TEXT NOT NULL DEFAULT '', -- JSON of the structured repeat, '' when the rule is custom
  first_instance  TEXT NOT NULL DEFAULT '', -- instance key of the series' DTSTART ('' unknown)
  PRIMARY KEY (subscription_id, uid)
);
CREATE INDEX IF NOT EXISTS calendar_event_series_id ON calendar_event_series(subscription_id, series_id);

-- Occurrences cached before this change were locked as 'recurring' and have no series row:
-- read every managed calendar once more.
UPDATE calendar_subscriptions SET etag = ''
  WHERE id IN (SELECT subscription_id FROM calsync_calendars);
```

- `calfeed.EventRemote` gains `Series *SeriesInfo` (`{ID, RRule, Repeat string, First string}`); `storeRefs` writes one row per uid that has it, in the transaction that replaces the events, and deletes the subscription's rows first (rows never outlive their events, as refs). `EventRow` gains `Series *SeriesView` (never serialised by the store, filled by the route like `Access`). `Store.RefByEvent` joins the table so `find` knows the event is an occurrence.
- Until a calendar is re-read, an old row says `locked = 'recurring'`; `AccessOf` and `check` treat that legacy value as `refresh_pending` (the grid shows `refresh_pending` and unlocks after the next read, as `0066` did).
- For Microsoft the table has one row per occurrence uid (`series_id` from `seriesMasterId`); `rrule` and `repeat` stay empty and the editor reads the master's pattern with the detail call (D8).
- Downgrade: an older binary ignores the table; the reset `etag` costs it one refetch, on which it writes its own refs with its own `recurring` lock again.

### D3. REST contract

All routes keep the registration, gates, limiter (one unit per request, even for the two remote writes of `following`) and tokens of phase 1 and 2.

```
PATCH /api/v1/calendar-sync/connections/{id}/events/{eventId}      // phase 1 and 2 body, plus:
  {
    "rev": "a41c9d03e2b7f610",
    "scope": "following",                       // this | following | all; required for an occurrence
    "summary": "Morning routine",
    "start": {"date": "2026-11-16", "time": "08:00", "tz": "Europe/Berlin"},
    "end":   {"date": "2026-11-16", "time": "09:30", "tz": "Europe/Berlin"},
    "repeat": {"freq": "weekly", "interval": 1, "days": ["mo","we"],
               "ends": {"type": "after", "count": 10}},   // omitted: unchanged; null: stop repeating (following only)
    "reminders": [10, 1440]                     // minutes before the start; [] removes the ones the form shows
  }
  200: {"event": <row as the events route serves it, or null when it left the cached window>,
        "replaces": "<old id>",
        "series": {"id": "5e0c…", "rev": "9b7d…"},   // only for an occurrence; rev only for CalDAV
        "refetch": true}                              // other occurrences changed: refetch the shown range

DELETE /api/v1/calendar-sync/connections/{id}/events/{eventId}?rev=a41c9d03e2b7f610&scope=this
  204

GET /api/v1/calendar-sync/connections/{id}/events/{eventId}       // phase 2 answer, plus:
  {
    "event": {...}, "replaces": "...", "description": "...", "descriptionEditable": true,
    "series": {"rrule": "FREQ=DAILY", "repeat": {"freq":"daily","interval":1,"days":[],"ends":{"type":"never"}},
               "custom": false, "exceptions": 2},       // absent for a single event; repeat null + custom true when custom
    "reminders": [10], "remindersMax": 5, "remindersOther": 1
  }

POST /api/v1/calendar-sync/connections/{id}/events                // phase 2 body, plus "repeat" and "reminders"
  201: {"event": <first occurrence row>, "clientId": "..."}

GET /api/v1/calendar-feeds/events?from=&to=
  each occurrence gains: "series": {"id": "5e0c…", "rrule": "FREQ=DAILY",
        "repeat": {"freq":"daily","interval":1,"days":[],"ends":{"type":"never"}},   // null when custom
        "custom": false, "first": false, "rev": "9b7d…"}
  managed subscriptions[] gain: "maxReminders": 5     // 1 for Microsoft
```

`repeat` fields: `freq` `daily|weekly|monthly|yearly`; `interval` 1 to 99; `days` an array of `mo..su` (weekly; empty: the start's weekday); `by` `monthday|weekday` (monthly only; omitted otherwise: the start's day of the month, or its nth weekday of the month, the fifth being "last"); `ends` `{type: "never"}`, `{type: "on", date: "YYYY-MM-DD"}` (the last day an occurrence may start on, in the event's own zone) or `{type: "after", count: 1..999}`. "Every weekday" is `weekly` with `days` `mo..fr`; the editor words it. `reminders` is at most five whole minutes in -1439..40320, unique (one for Microsoft); negative means after midnight of an all-day event (all-day "on the day at 09:00" is -540). `series.rrule` is the calendar's own text, read-only echo.

Errors: the phase 1 and 2 table plus the rows below. No other code is added.

| Status | Code | When |
| --- | --- | --- |
| 400 | `scope_required` | an occurrence PATCH or DELETE without `scope` (the message lists the three values) |
| 400 | `invalid_input` | `scope` on a single event or not one of the three; `repeat` with `this`; `repeat: null` without `following`; a malformed `repeat` or `reminders` (field named); more reminders than `maxReminders`; `repeat` on a custom rule; `repeat: null` on a Microsoft series; a move to another day for `all` when the rule cannot shift (field `start`) |
| 422 | `event_not_editable` | reason `series_unsupported` (a lock, D10) or `following_unsupported` (Microsoft: the master has attachments or an online meeting, checked before any write) |
| 409 | `event_changed` | stale `rev`, a 412 on either write, a rolled-back `following` |
| 502 | `calendar_unreachable` | reason `partial_write` when the rollback of a `following` could not delete the new series (the cache is refreshed so the duplicate is visible) |

Order of checks on a series write, under `Worker.lock(connID)`: body rules -> connection -> `edit_events` -> caps -> lock (D10) -> `scope` (required or refused) -> `rev` -> read the object and compare its ETag -> engine -> write(s) -> cache -> `Notify` once. The handler records `calendar_event.changed` with `event_action` `updated` or `deleted` and a new detail key `event_scope` (`this|following|all`), never a title.

### D4. The series engine (pure Go, `internal/calfeed/series*.go`)

All iCalendar logic is pure functions over the parsed `*caldav.Component` tree, with no I/O and no clock (callers pass `now`), so every case is a golden file. It lives in `calfeed` because it needs `parseEvent`, `parseRule`, `expandRule`, `instanceKey` and `Resolve`, all unexported or already there.

```go
// Scope is how much of a series a change reaches.
type Scope string

const (
	ScopeThis      Scope = "this"
	ScopeFollowing Scope = "following"
	ScopeAll       Scope = "all"
)

// Repeat is a repeat rule in the form the editor edits (a subset of RRULE).
type Repeat struct {
	Freq     string // daily, weekly, monthly, yearly
	Interval int
	Days     []string // mo..su, weekly only; empty: the start's weekday
	By       string   // monthday or weekday, monthly only; "": the start's day of the month
	Ends     RepeatEnd
}

// RepeatEnd is never ({Type: "never"}), on a date ({Type: "on", Date}) or after a count.
type RepeatEnd struct {
	Type  string
	Date  string // YYYY-MM-DD, in the event's own zone
	Count int
}

// RepeatOf reads an RRULE in the editor's form; ok is false when the form cannot say
// exactly what the rule says (any other part, BYSETPOS, ordinals on weekly days, a
// WKST that changes a weekly interval, a BYMONTHDAY that is not the start's day...).
func RepeatOf(rrule string, start Point) (Repeat, bool)

// RRule writes a repeat as an RRULE value for an event that starts at start:
// an UNTIL is the end of its day in the start's zone, in UTC for a zoned start (RFC 5545 3.3.10).
func (r Repeat) RRule(start Point) (string, error)

// Span is an occurrence's start and end.
type Span struct {
	Start, End Point
	AllDay     bool
}

// SeriesChange is what a scoped edit changes; a nil field stays as it is.
type SeriesChange struct {
	Summary, Location, Description *string
	Free                           *bool
	When                           *Span         // the edited occurrence's new start and end, absolute
	Repeat                         **Repeat      // nil: unchanged; non-nil pointing at nil: stop repeating
	Reminders                      *[]int
}

// SeriesEdit is one scoped write against a parsed calendar object.
type SeriesEdit struct {
	Root     *caldav.Component // the VCALENDAR as read; modified in place
	UID      string
	Instance string // instanceKey of the occurrence the edit started from
	Scope    Scope
	Change   SeriesChange
	NewUID   string // the uid of the series a following edit creates (deterministic, D6)
	Now      time.Time
}

// SeriesObject is a calendar object an edit leaves behind.
type SeriesObject struct {
	UID  string
	Root *caldav.Component
	New  bool // a new object (a following split)
}

// SeriesResult is what an edit leaves: the objects to write, whether the original
// object is to be deleted instead, and the edited occurrence's identity afterwards.
type SeriesResult struct {
	Objects      []SeriesObject
	DeleteOrigin bool
	UID          string // the edited occurrence's uid afterwards
	Instance     string // and its instance key
}

func ApplySeries(e SeriesEdit) (SeriesResult, error)  // this, following, all
func DeleteSeries(e SeriesEdit) (SeriesResult, error) // this, following, all
func ReadSeries(root *caldav.Component) (SeriesView, error)
func SeriesLocked(root *caldav.Component, etag string) string // "", series_unsupported, has_guests, no_etag
func SplitRule(rrule string, start, at Point) (before, after string, err error)

var (
	ErrSeriesUnsupported = errors.New("series not supported") // a lock reason found late
	ErrNoSuchInstance    = errors.New("no such occurrence")   // Instance is not an occurrence of the rule
	ErrShiftUnsupported  = errors.New("rule cannot move to another day")
	ErrRepeatThis        = errors.New("repeat changes need following or all")
)
```

Files (each under the 1,100-line limit, each function under 150 lines): `series.go` (read, lock, locate the master and exceptions, the instance arithmetic), `series_this.go`, `series_all.go`, `series_following.go`, `series_repeat.go` (`RepeatOf`, `RRule`, `SplitRule`), `series_alarm.go` (reminders), `series_time.go` (wall-clock arithmetic in the master's zone, re-keying a point). `vevent.go` gains `Event.RRules int` (the number of `RRULE` properties) and `Event.Exrule bool` for the lock; `parseEvent` is otherwise unchanged.

Two helpers matter for correctness everywhere:

- **Instance arithmetic is wall-clock in the master's zone.** A series is defined by the master's wall time in its zone (`DTSTART;TZID=Europe/Berlin:...`), so shifting "30 minutes later" adds 30 minutes to the wall clock of every instance, and `time.Date` in the zone resolves DST; a UTC-form `DTSTART:...Z` series is shifted as UTC. An instance's `RECURRENCE-ID` or `EXDATE` is re-written in **the form it already had** (a Google exception with `RECURRENCE-ID:...Z` stays UTC, one with `TZID` stays `TZID`) so a server that matches textually still matches; the engine itself always matches by `instanceKey`.
- **The nominal slot.** An occurrence's slot is its `instance` read back as a point in the master's form. An exception whose start and duration equal its slot's (it was only renamed, say) is "at its slot"; one that differs was "moved by hand".

### D5. EventWriter additions (Go, verbatim)

`internal/calsync/events.go` (the rest of the file is unchanged):

```go
// EventRef gains the occurrence's place in its series.
type EventRef struct {
	Calendar string
	Href     string // CalDAV object address, or the Graph event id (an occurrence id for Microsoft series)
	ETag     string
	Instance string // the occurrence's instance key; "" for a single event
	Series   string // Microsoft: the seriesMasterId; "" otherwise
}

// EventEdit gains the scope, the repeat and the reminders.
type EventEdit struct {
	// ... Summary, When, Location, Description, Free as before
	Scope     calfeed.Scope  // required when ref names an occurrence
	Repeat    *RepeatEdit    // nil: unchanged
	Reminders *[]int         // nil: unchanged; empty: remove the ones the form shows
}

// RepeatEdit sets the repeat; a nil Rule stops repeating.
type RepeatEdit struct{ Rule *calfeed.Repeat }

// WrittenObject is a calendar object a write left behind, as the writer parsed
// it from the body it sent, so the worker can re-expand it without another read.
type WrittenObject struct {
	Ref    EventRef
	Events []calfeed.Event
	Locked string
	Series *calfeed.SeriesInfo
}

// SeriesWrite lists what a scoped write changed on the account.
type SeriesWrite struct {
	Written []WrittenObject
	Deleted []string // hrefs (or Graph ids) removed
	UID     string   // the edited occurrence's uid afterwards ("" for a delete)
	Instance string  // and its instance key
}

// EventState gains the series write and, for Detail, what the editor shows.
type EventState struct {
	// ... Ref, Event, Locked, Description, DescriptionLocked as before
	Wrote SeriesWrite
	// Detail only:
	Series         *SeriesDetail
	Reminders      []int
	RemindersMax   int
	RemindersOther int
}

// SeriesDetail is a series as the editor shows it.
type SeriesDetail struct {
	RRule      string
	Repeat     *calfeed.Repeat // nil when the rule is custom
	Custom     bool
	Exceptions int // exceptions and excluded dates, for the "resets" warning
}

// EventWriter gains one method; Update honours edit.Scope when ref names an occurrence.
type EventWriter interface {
	// ... Read, Detail, Update, Delete, Create as before
	// DeleteScoped removes an occurrence for a scope (ref names an occurrence).
	DeleteScoped(ctx context.Context, ref EventRef, scope calfeed.Scope) (SeriesWrite, error)
}

var (
	ErrFollowingUnsupported = errors.New("following change would lose data") // Microsoft: attachments, online meeting
	ErrPartialWrite         = errors.New("series change half written")       // the rollback failed
)
```

`internal/calsync/event_edit.go`: `DeleteEvent(ctx, userID, connID, eventID, rev string, scope calfeed.Scope)` gains the scope (REST, agent editor and CLI routes are its only callers and are updated); `EditEvent` keeps its signature. `EventResult` gains `Series *SeriesResult{ID, Rev string}` and `Refetch bool`. New error codes in the Go set: `EventScopeRequired = "scope_required"`, reasons `LockSeriesUnsupported = "series_unsupported"`, `ReasonFollowingUnsupported = "following_unsupported"`, `ReasonPartialWrite = "partial_write"`; `LockRecurring` is removed with its last user. `find` returns the occurrence's series row; `check` gains the scope rules of D1 and the legacy `recurring` treatment of D2.

`internal/calfeed/managed.go`:

```go
// ReplaceSeries records a scoped write in one transaction: the cached rows of every
// uid in drop are deleted (events, refs, series rows) and occ are stored with their
// refs and series rows; the subscription's version is cleared so the next cycle reads
// the calendar again. It returns the refs of the stored occurrences.
func (s *Store) ReplaceSeries(ctx context.Context, subscriptionID string, drop []string, occ []Occurrence, refs map[string]EventRemote) ([]EventRef, error)

// DropSeries is ReplaceSeries that stores nothing: the uids leave the cache.
func (s *Store) DropSeries(ctx context.Context, subscriptionID string, uids []string) error

// SeriesUIDs lists the cached uids of a series (Microsoft: one per occurrence).
func (s *Store) SeriesUIDs(ctx context.Context, subscriptionID, seriesID string) ([]string, error)
```

**Cache re-expansion (CalDAV).** `Worker` takes `SeriesWrite.Written`, runs `calfeed.ExpandContext` over `calfeed.StoreWindow(clock)` with `MaxExpandTime` on each written object's events (no second GET: the body we sent is what we parse, `calfeed.ParseCalendar(body)`, as phase 1's `Update` already does), and calls `ReplaceSeries` with `drop = [old uid, new uid]`. The edited occurrence's row is `EventRowOf(sub, UID, Instance)`; `series.rev` is `Rev(newETag)`. A server that answers 2xx without an ETag is read once for it (phase 1 rule); if it still has none the object is cached `locked = no_etag` and the answer carries `refetch: true`. The subscription's version is cleared, so the next cycle re-reads the calendar and heals any difference between what we expanded and what the server kept. **Microsoft:** `this` is `ReplaceEvent` as in phase 1; `all` and `following` call `DropSeries(SeriesUIDs(old series), SeriesUIDs(new))` and then read `GET /me/events/{master}/instances?startDateTime&endDateTime` for each affected master (D9), caching the occurrences it returns.

### D6. CalDAV and Google algorithms

The object is read (`getObject`), its ETag compared with `ref.ETag` (a mismatch is `ErrEventChanged` before any write), locked or unlocked by `SeriesLocked`, parsed with `caldav.Parse`, edited in the tree and written back whole, so unknown properties, `X-` properties, `VALARM`s, `VTIMEZONE`s and other components survive (phase 1 D5). Bookkeeping on every touched `VEVENT`: `SEQUENCE+1`, `DTSTAMP`, `LAST-MODIFIED`; the **master is not touched by `this`** except for an excluded date.

All examples use this resource, as Google serves it, with two Google quirks: the exception's `RECURRENCE-ID` is in UTC while the master uses `TZID`, and the exception sits after the master. 2026-11-12 is CET (UTC+1), summer time ended 2026-10-25.

```
BEGIN:VCALENDAR
VERSION:2.0
PRODID:-//Google Inc//Google Calendar 70.9054//EN
BEGIN:VTIMEZONE                       (Europe/Berlin, abbreviated here)
TZID:Europe/Berlin
...
END:VTIMEZONE
BEGIN:VEVENT
UID:routine-1@google.com
DTSTART;TZID=Europe/Berlin:20261001T070000
DTEND;TZID=Europe/Berlin:20261001T083000
RRULE:FREQ=DAILY
EXDATE;TZID=Europe/Berlin:20261114T070000
DTSTAMP:20261001T060000Z
SEQUENCE:0
SUMMARY:Morning routine
X-GOOGLE-CALENDAR-CONTENT-TITLE:keep me
BEGIN:VALARM
ACTION:DISPLAY
TRIGGER:-PT10M
DESCRIPTION:Reminder
END:VALARM
END:VEVENT
BEGIN:VEVENT
UID:routine-1@google.com
RECURRENCE-ID:20261113T060000Z
DTSTART;TZID=Europe/Berlin:20261113T090000
DTEND;TZID=Europe/Berlin:20261113T100000
SUMMARY:Morning routine (late)
DTSTAMP:20261101T090000Z
SEQUENCE:1
END:VEVENT
END:VCALENDAR
```

So the series is 07:00 to 08:30 daily from 2026-10-01, 2026-11-14 is excluded, and 2026-11-13 is an exception at 09:00 to 10:00 named "Morning routine (late)" (not at its slot: moved by hand).

**`this`, an occurrence without an exception.** Edit 2026-11-12 (instance `20261112T060000Z`) to 07:30 to 09:00. The engine copies the master `VEVENT` into a new `VEVENT` without `RRULE`, `RDATE`, `EXDATE` (and `EXRULE`), adds `RECURRENCE-ID` **in the master's `DTSTART` form** (`RECURRENCE-ID;TZID=Europe/Berlin:20261112T070000`), sets `DTSTART`, `DTEND` from the change, keeps `SUMMARY`, the unknown properties and the `VALARM` children, sets `SEQUENCE` to the master's plus one, and appends it after the last `VEVENT`:

```
BEGIN:VEVENT
UID:routine-1@google.com
RECURRENCE-ID;TZID=Europe/Berlin:20261112T070000
DTSTART;TZID=Europe/Berlin:20261112T073000
DTEND;TZID=Europe/Berlin:20261112T090000
DTSTAMP:20261112T100000Z
SEQUENCE:1
SUMMARY:Morning routine
X-GOOGLE-CALENDAR-CONTENT-TITLE:keep me
BEGIN:VALARM ... END:VALARM
END:VEVENT
```

The master is unchanged. After re-expansion the occurrence has the same `id` (instance `20261112T060000Z`) and new start and end.

**`this`, an occurrence that already has an exception** (2026-11-13, renamed again): the exception `VEVENT` found by instance key (its `RECURRENCE-ID` is UTC, matched by key) is edited in place, its `RECURRENCE-ID` text untouched. `STATUS:CANCELLED` exceptions are not occurrences and cannot be addressed.

**Delete `this`.** Instance 2026-11-15: append `EXDATE;TZID=Europe/Berlin:20261115T070000` to the master (a second `EXDATE` property; a server that wants one line gets one line per instance, valid RFC 5545). Instance 2026-11-13, which has an exception: the same `EXDATE` (`...20261113T070000`, the master's form) and the exception `VEVENT` is removed. The master's `SEQUENCE` is bumped (the series changed). If the series is finite and this was its last non-excluded occurrence, the object is deleted instead (D1).

**`following` (split).** Edit 2026-11-16 (instance `20261116T060000Z`) to 08:00 to 09:30. Let `S` be the occurrence, `P` the last rule instance strictly before `S` (computed with `expandRule`, ignoring `EXDATE`; none means `S` is the first and the edit is `all`).

1. **Old object.** `RRULE` gets `UNTIL=P` and loses `COUNT` (`SplitRule`): UTC `20261115T060000Z` here, because `DTSTART` has a `TZID` (RFC 5545 3.3.10: `UNTIL` must be UTC for a zoned or UTC `DTSTART`); a date for a `VALUE=DATE` start; floating for a floating start. `EXDATE`s at or after `S` and exceptions at or after `S` are removed (they move, below); earlier ones stay. `SEQUENCE+1`.
2. **New object**, a copy of the master and its `VTIMEZONE`s with: `UID` = `NewUID`; `DTSTART`/`DTEND` from the change; `RRULE` = `after` from `SplitRule` (no `UNTIL` unless the old rule had one, which it keeps; `COUNT` = old count minus the index of `S` in the rule set, so a 60-occurrence series split at its 47th occurrence leaves the old with 46 and the new with 14); `EXDATE`s at or after `S`; exceptions at or after `S`, with `UID` replaced; `SEQUENCE:0`; the edited fields; everything else as is.
3. If `S` itself had an exception (its displayed values), those values are the starting point of the new master and the exception is consumed (it appears in neither object).

Old after step 1:

```
RRULE:FREQ=DAILY;UNTIL=20261115T060000Z
EXDATE;TZID=Europe/Berlin:20261114T070000      (kept: before S)
... the 2026-11-13 exception stays ...
SEQUENCE:1
```

New object (`UID:ot-5e0c3a91b7d2f4a8c6e1@opentodo`):

```
DTSTART;TZID=Europe/Berlin:20261116T080000
DTEND;TZID=Europe/Berlin:20261116T093000
RRULE:FREQ=DAILY
SUMMARY:Morning routine
X-GOOGLE-CALENDAR-CONTENT-TITLE:keep me
BEGIN:VALARM ... TRIGGER:-PT10M ... END:VALARM
```

**Exceptions after the split point: they follow the new series.** Rationale: an exception at or after `S` is a customisation of a day the new series now owns (Google "keeps exceptions", the brief's rule); dropping it would silently undo the person's work, and leaving it on the old series would attach it to an occurrence that no longer exists (the old `UNTIL` ends before it), which every server reads as orphaned and may discard. Each moved exception is re-keyed with the same difference `delta` the new series' start has against the old slot of `S` (here `+1h`: 07:00 to 08:00): its `RECURRENCE-ID` shifts by `delta` in its own form, an `EXDATE` shifts in the master's form, and its own times shift too **only when it was at its slot** (an exception moved by hand keeps its absolute time). With a second exception on 2026-11-20 renamed "Review day" at its slot (`RECURRENCE-ID:20261120T060000Z`, 07:00 to 08:30) and an `EXDATE` on 2026-11-22:

```
RECURRENCE-ID:20261120T070000Z              (08:00 CET = 07:00Z)
DTSTART;TZID=Europe/Berlin:20261120T080000
DTEND;TZID=Europe/Berlin:20261120T093000
SUMMARY:Review day
...
EXDATE;TZID=Europe/Berlin:20261122T080000
```

If the change also carries a new `repeat`, exceptions and `EXDATE`s at or after `S` are dropped instead (they were keyed to the old rule; D8). `NewUID` is `ot-` + the first 20 hex characters of SHA-256 of `uid | instance | old ETag` + `@opentodo`: deterministic, so a retry of the same split after a half failure writes the same object.

**Write order and failure** (`following`): (1) `PUT` the new object at `<calendar><PathEscape(NewUID)>.ics` with `If-None-Match: *`; (2) `PUT` the old object with `If-Match: <etag read>`. New first: a failure leaves the calendar as it was or with a visible duplicate of future occurrences, **never with lost occurrences**; the reverse order would leave a gap if the second write failed. Failure handling: a 412 or any error on (1) writes nothing (a 412 whose body has `NewUID` is a replay of an earlier half-finished split: its ETag is used and the flow continues at (2); another uid at that address is `href_taken`). On any failure of (2), `DELETE` the new object with `If-Match` of the ETag (1) answered (read with `GET` when the server gave none), then return the error of (2) (412 gives `409 event_changed`). If that `DELETE` fails too, answer `502 calendar_unreachable` with reason `partial_write`, refresh the cache so the duplicate is visible, and log it. A lost answer after both writes is a retry that meets `409 event_changed` (the old object's ETag moved) and the device shows the result after its refetch.

**`all`.** Change the series; the edited occurrence `X` has displayed start `Xs` and end `Xe`, and `d_s = new start - Xs`, `d_e = new end - Xe` are taken in wall-clock minutes **relative to the occurrence as shown** (so dragging a hand-moved exception shifts the series by what the person moved, not by the distance from its slot):

- **Master time** = `DTSTART + d_s`, `DTEND = new start + (old duration + d_e - d_s)` (a `DURATION` becomes a `DTEND`, phase 1). A move to another day (`d_s` over a day) also moves the rule: `DAILY` as is, `WEEKLY` rotates `BYDAY` by the day difference (modulo 7), `MONTHLY` and `YEARLY` answer `ErrShiftUnsupported` (400 `invalid_input` field `start`; the client disables the choice from `series.repeat`, and for a custom rule leaves it to the server). An all-day switch or a zone change (a new `tz` with the same wall clock, phase 2) re-writes the master's start in the new form and every `EXDATE`/`RECURRENCE-ID` by **date** (same wall clock, new form).
- **Identity kept.** Every `EXDATE` and every exception's `RECURRENCE-ID` moves by the same wall-clock difference in the master's zone, so each stays attached to the same logical day; an exception at its slot also moves its own times; one moved by hand keeps its time. Moving the 07:00 series 30 minutes later:

```
DTSTART;TZID=Europe/Berlin:20261001T073000
DTEND;TZID=Europe/Berlin:20261001T090000
EXDATE;TZID=Europe/Berlin:20261114T073000
...
RECURRENCE-ID:20261113T063000Z            (07:30 CET = 06:30Z)
DTSTART;TZID=Europe/Berlin:20261113T090000   (moved by hand: unchanged)
DTEND;TZID=Europe/Berlin:20261113T100000
```

  This is what Google does ("Google keeps exceptions", the brief's rule) and it is the only choice that does not resurrect the occurrence the person had moved or deleted. Dropping overrides whose times were absolute was rejected: it deletes work.
- **Text fields carry.** A changed `SUMMARY`, `LOCATION`, `DESCRIPTION`, `TRANSP` or reminder set is set on the master **and on every exception that still holds the master's previous value** (the carry rule of `series-exceptions` D6, kept for the same reason: renaming "Study" renames Wednesday's moved session but not one that was renamed alone). Renaming "Morning routine" to "Routine" leaves "Morning routine (late)".
- **A new `repeat`** replaces `RRULE`, and **removes every `EXDATE` and exception** (their identities belong to the old rule; Outlook and Google do the same). The editor warns when `series.exceptions > 0` (`GET` detail) before Save.

**Reminders** (`series_alarm.go`): the form owns `VALARM` children with `ACTION:DISPLAY`, a relative `TRIGGER` (default `RELATED=START`), no `REPEAT`/`DURATION`, and a `TRIGGER` that parses as an integer number of minutes. Setting `reminders` deletes those and adds one `BEGIN:VALARM / ACTION:DISPLAY / TRIGGER:-PT10M / DESCRIPTION:Reminder / END:VALARM` per value (whole days as `-P1D`, whole hours as `-PT<n>H`, otherwise `-PT<n>M`; a negative value is a positive `TRIGGER` such as `PT9H`); all other alarms (email, audio, absolute `VALUE=DATE-TIME`, `RELATED=END`, repeating) are kept and counted as "other". An override created by `this` copies the master's alarms; a `reminders` edit under `all` is carried like the text fields.

**Quirks pinned by golden fixtures:** the exception before the master in the file; `RECURRENCE-ID` in UTC under a `TZID` master; `DTSTART` in UTC (`...Z`) with `UNTIL` in UTC; `VALUE=DATE` series with a date `UNTIL`; floating series; `STATUS:CANCELLED` exceptions kept as they are; `EXDATE` lists with several comma-separated values (re-keyed value by value); a `DURATION` master; a master without `SEQUENCE`; an `X-` property on master and exception; a `VTIMEZONE` that must be added for a new zone (`addZone`); `DTSTAMP`/`LAST-MODIFIED` set from the injected clock.

### D7. Delete scopes

`DeleteScoped`: `this` as above (an excluded date, the exception removed); `following` ends the old series at `P` and removes `EXDATE`s and exceptions at or after `S` (the first occurrence, or the last remaining one, deletes the object); `all` is `DELETE` with `If-Match`. Microsoft: `this` is `DELETE /me/events/{occurrenceId}` with `If-Match` (Graph records a cancelled exception), `all` deletes the master, `following` patches the master's range (D9). The web keeps the 8-second delay of phase 1 for every scope.

### D8. The repeat form, RRULE and Graph mapping

`RepeatOf(rrule, start)` returns `ok` only for rules `Repeat.RRule` regenerates exactly (same occurrence set); it is the single place "custom" is decided, and a golden table pins it:

| Form | RRULE | Graph `patternedRecurrence` |
| --- | --- | --- |
| daily, every N | `FREQ=DAILY[;INTERVAL=N]` | `daily`, `interval` |
| weekly on days, every N | `FREQ=WEEKLY[;INTERVAL=N];BYDAY=MO,WE` (no `BYDAY` for the start's weekday; `WKST=MO` written when `N>1` and several days) | `weekly`, `daysOfWeek`, `firstDayOfWeek: monday` |
| every weekday | weekly on `MO..FR` | weekly on `monday..friday` |
| monthly by day | `FREQ=MONTHLY;BYMONTHDAY=<start day>` (31 means "the 31st", months without it are skipped, as RFC 5545 says) | `absoluteMonthly`, `dayOfMonth` |
| monthly by weekday | `FREQ=MONTHLY;BYDAY=<n><DAY>` with `n` the start's ordinal, the fifth as `-1` | `relativeMonthly`, `daysOfWeek`, `index` `first..fourth|last` |
| yearly | `FREQ=YEARLY` (start's month and day) | `absoluteYearly`, `month`, `dayOfMonth` |
| ends on D | `UNTIL=` end of day D in the start's zone, UTC for a zoned start, a date for all-day, floating for floating | `range` `endDate` |
| ends after N | `COUNT=N` | `range` `numbered`, `numberOfOccurrences` |
| never | none | `range` `noEnd` |

Everything else (`BYSETPOS`, several weekday ordinals, `BYMONTHDAY` lists, `BYMONTH` other than the start's, `WKST` that changes a multi-day weekly interval, Graph `relativeYearly`, `index` `second` for weekly...) is **custom**. `Graph range.startDate` is the date of the start in `recurrenceTimeZone` (the zone's Windows id, phase 1's `mailboxWindows`).

Where the rule lives for Microsoft: the occurrence does not carry it, so the editor's `GET` detail reads the master (`GET /me/events/{seriesMasterId}?$select=recurrence,...`) and maps the pattern back with the same table. The grid's `series` for Microsoft has `id` and `first: false` only; `repeat` is loaded on demand.

**Rules of change.** A change of `repeat` is `all` or `following`, never `this` (400). On a single event it makes the event repeat from its own start (an edit of the master: add `RRULE`, or Graph `recurrence`). `repeat: null` (stop repeating) is accepted with `following` only: the occurrence becomes a single event and the old series ends before it; on the first occurrence the object is turned into a single event in place (`RRULE`, `EXDATE`, exceptions removed). It is refused for Microsoft series in this change (open question 2). A custom rule is kept untouched by every other change and cannot be changed through `repeat` (400); the editor shows "Custom repeat".

### D9. Microsoft Graph mapping

Reads: the window read gains `$select=id,subject,start,end,isAllDay,showAs,type,isOrganizer,attendees,location,isCancelled,lastModifiedDateTime,seriesMasterId` (with the existing `$expand`), because a `calendarView` without `$select` is not guaranteed to carry `seriesMasterId` (open question 3); `graphEvent` gains `SeriesMasterID`, `Recurrence`, `ReminderMinutesBeforeStart`, `IsReminderOn`, `HasAttachments`, `IsOnlineMeeting`. `type` `occurrence` or `exception` with a `seriesMasterId` is an occurrence of a series, `locked()` loses its `recurring` case and gains `series_unsupported` for an occurrence without a master id; the cached `uid` stays the occurrence id (an occurrence id is stable, and it is what `this` patches). The series row (`D2`) groups occurrences by `seriesMasterId`.

- **`this`:** `PATCH /me/events/{occurrenceId}` with `If-Match` the occurrence's ETag and only the changed members (phase 1 and 2 code, `Update` unchanged), reminders as `reminderMinutesBeforeStart` and `isReminderOn`. Graph turns the occurrence into an `exception`; the id is stable, the cache id changes with the start as for any Microsoft event (instance = start), so the answer's `replaces` is the old id. `DELETE /me/events/{occurrenceId}`.
- **`all`:** read the occurrence (its ETag must equal `ref.ETag`, else `ErrEventChanged`: the guard that stands in for the master's ETag, which `calendarView` does not give, open question 4) and the master (`GET /me/events/{seriesMasterId}` with `recurrence`, `start`, `end`), compute `d_s`, `d_e` as in D6, and `PATCH` the master with `If-Match` of the master's ETag just read: new `start`/`end` (master start + `d_s`, `point()` of phase 1), changed text, reminders, and a shifted pattern for a day move (`daysOfWeek` rotated, `range.startDate` moved; monthly and yearly patterns refuse as in D6). **Graph drops the series' exceptions when the master's times or pattern change** (documented behaviour of Outlook; the editor says so beside "All events" when `series.exceptions > 0`, and the occurrences it had moved by hand come back to the series' time). A rename or a change of reminder or location leaves the exceptions; it does not carry into exceptions that held the old value (Graph does not offer it), documented. `DELETE` the master for a delete.
- **`following`:** Graph has no split. (1) Read the master; refuse with `ErrFollowingUnsupported` (422 `following_unsupported`, nothing written) when `hasAttachments` or `isOnlineMeeting` is true or the pattern type is not representable (`relativeYearly`), because the copy would lose them (phase 2 D8's argument against moves). (2) `POST /me/calendars/{calendarId}/events` a new series from an allowlist of the master's members (`subject`, `body` as the master's content type, `location`, `categories`, `importance`, `sensitivity`, `showAs`, `isReminderOn`, `reminderMinutesBeforeStart`, `isAllDay`), the edited `start`/`end`, a `recurrence` whose pattern is the master's, `range.startDate` the occurrence's date, `endDate` or `numberOfOccurrences` kept (a `numbered` range becomes the remaining count), and `transactionId` `opentodo-sp-<NewUID hash>` (deterministic, as for creates). (3) `PATCH` the master `recurrence.range` to `endDate` = the day before the occurrence (`If-Match` the master's ETag read in (1)). On failure of (3) `DELETE` the new event (`If-Match` its ETag from (2)), as D6; a failed rollback is `partial_write`. Exceptions of the old master after the split date are dropped by Graph when the range shrinks; those of the part before stay.
- **Cache:** `ReplaceEvent` for `this`; for `all` and `following`, `DropSeries` of the affected series uids, then `GET /me/events/{master}/instances?startDateTime=&endDateTime=` over `StoreWindow` for the master (and the new one), cached as occurrences with series rows, the subscription's version cleared.
- **Locks** stay: `not_organizer`, `has_guests` (attendees on the occurrence), `no_etag`.

### D10. Locks and `series_unsupported`

`calendar_event_remote.locked` holds one reason per uid. After this change, in order, for a CalDAV object (`SeriesLocked`, evaluated for every object the cycle reads and again on every write):

1. `series_unsupported` when any of: an exception with `RANGE=THISANDFUTURE`; any `RDATE` (RDATE-only and RDATE-with-rule alike: instance arithmetic and splits over extra dates are outside what is verified); an `EXRULE`; more than one `RRULE`; an `RRULE` `parseRule` refuses (hourly, `BYHOUR`, `BYYEARDAY`...) or whose expansion hit the work bound; more than one master `VEVENT` for the uid, or `VEVENT`s of another uid in the object; more than 400 `VEVENT`s; an object over 256 KiB (the write limit is far below `MaxBodyBytes`, 1 MiB, so a normal long series is fine and a resource that is mostly attachments is not rewritten).
2. `has_guests`: an `ATTENDEE` in the master **or in any exception**. (Phase 1 had it for the first `VEVENT` with a guest; the series-wide rule matters because an exception write rewrites the whole object.)
3. `no_etag`.

Microsoft: `not_organizer`, `has_guests`, `no_etag`, and `series_unsupported` for an occurrence or exception without `seriesMasterId`. The `recurring` reason is **gone from the served set** (`access.reason`, the locked table, `eventAccessText`); a cache row that still says it is `refresh_pending` (D2). Two more write-time refusals are not locks because they depend on the change: `following_unsupported` (D9) and the `invalid_input` for a day move of a monthly or yearly series.

**Edge cases by series shape.** `COUNT` is converted to `UNTIL` for the old series and to a smaller `COUNT` for the new one (D6). A series whose DTSTART is earlier than the cache window is handled (the object holds the master). An exception whose `RECURRENCE-ID` matches no rule instance is kept and ignored by `Expand`; it is not an occurrence and cannot be addressed. A series with every occurrence excluded has no rows and nothing to edit.

### D11. Web data layer

Types (`web/src/lib/types.ts`): `CalendarEvent` gains `series?: {id: string; rrule?: string; repeat?: CalendarRepeat | null; custom?: boolean; first?: boolean; rev?: string}`; `CalendarSubscription` gains `maxReminders?: number`; `CalendarEventChange` gains `repeat?: CalendarRepeat | null` and `reminders?: number[]`; `EventCommand` gains `scope?: 'this' | 'following' | 'all'`; `CalendarRepeat` is the REST `repeat` object. **No Dexie version** (the outbox schema is `key, notBefore`; the surface file `mobile/app/src/storage/surface.json` is unchanged; verify with `git diff --stat`). `api.ts`: `updateCalendarEvent` and `deleteCalendarEvent` take `scope`; the answer type gains `series` and `refetch`.

New pure modules, small and tested: `web/src/lib/eventScope.ts` (`needsScope(ev)`, `scopesFor(ev, change)`: `this`, `following` (not on `series.first`), `all`; a change with `repeat` allows `following` and `all` only; a day move allows `all` only when `canShiftDays(series.repeat)`, which is true for daily and weekly and false for monthly and yearly, and true for `null` (a custom rule: the server decides)), and `web/src/lib/eventRepeat.ts` (the form's helpers: `repeatSummary(repeat)` for the words, presets, `endsLabel`, `defaultRepeat(start)`; no RRULE text on the client).

**Strategy per scope (optimistic or pending).**

- **`this`: optimistic**, exactly phase 1 and 2: patch the cached row at once (the id is stable on CalDAV), queue the command, roll back and refetch on a refusal, Undo with the inverse.
- **`following` and `all`: a pending state, then refetch.** The client does **not** expand series (that would need the rule, the exceptions and the zone arithmetic on the device, a second implementation to keep equal to the server's). The edited block alone shows its new place with the saving state (`aria-busy`, dimmed), exactly as a queued `this` edit does, so the drop does not snap back; every other occurrence stays where the cache has it until the answer. On `200` the device applies `series.rev` to the series' rows and, when `refetch` is true, runs `refreshEvents()` (the whole table is replaced from one request anyway) and `adoptEvent` on `event`. A series with a queued `following`/`all` command has other edits to the same event blocked with a toast ("Saving this change first"): an edit with the **same scope** merges into the command (the phase 1 rule); an edit with **another scope** waits in `next` (the in-flight slot of phase 1, now carrying its own `scope`), is sent after the answer against the new id and `rev`, and the id swap of `adoptEvent` finds the occurrence under its new id (`currentEventId`). If the occurrence no longer exists after the first change (its instance moved with `all`), `next` is refused with the reason, restored and shown.
- **Undo.** `this`: the inverse change. `all`: the inverse change with scope `all` (deltas are relative, so it restores the series, including what the carry changed). `following`: **no Undo** (it split the series and there is no merge); the toast says what happened without an action. Delete: the 8-second delay and cancel of phase 1 for every scope.
- **Outbox.** `EventCommand.scope` is set from the first edit and kept; `flushEventEdits` sends `scope` and `repeat`/`reminders` with the patch; the refusal handling is unchanged (400, 403, 404, 409, 422 restore `base`, refetch, toast) with new texts for `scope_required` (a bug) and `series_unsupported`; network errors keep the command. Convert to task on an occurrence deletes with scope `this`. Duplicate copies one occurrence as a single event (no `repeat`), as phase 2 says.
- **Refresh.** `refreshEvents` re-applies queued patches as before; a queued `following`/`all` patch is applied to its one edited occurrence only.

### D12. Web UI

**Scope menu** (`web/src/app/calendar/ScopeMenu.tsx`, lazy with the grid's edit hook; built on the overlay module's `Menu` with `open` controlled, as `occurrenceMenu.tsx` does for task occurrences, opened at the pointer for a drag and anchored to the block for a key press). Items: **This event**, **This and following events** (hidden on the first occurrence), **All events**; each is a `MenuItem` with `data-event-scope`; a choice that cannot apply is disabled with its reason as the item's `aria-describedby` ("This repeat can't move to another day for all events"). The change a drag made stays visible on the block with the saving state while the menu is open; Escape or a click outside restores the block and sends nothing. The store `eventScopeStore.ts` holds `{event, change, anchor, resolve}`; `useEventEdits.onDrop` and the key handlers call `askScope(ev, change)` for an occurrence and `editEvent` directly for a single event.

**Keyboard path.** An occurrence joins the selection order as a single event does. `calendar.nudge`, `calendar.shift-day`, `calendar.estimate`, `calendar.estimate-start` coalesce (the phase 1 settle of 300 ms) and, for an occurrence, open the scope menu after the keys settle with the focus on the first item, arrows to move, Enter to choose, Escape to cancel (the block returns to where it was); `calendar.delete` asks too. No new shortcut and no new registry id (`docs/keyboard.md` unchanged). **WCAG 2.5.7:** the popover's `data-event-edit` fields and Save remain the click alternative to every drag (`scripts/ux-audit/drag-alternatives.json` unchanged for `calendar-event`; its `alternatives` text now also covers the menu through the same Save path), and Save asks the same menu, anchored to the Save button. The menu is announced through the live region the block drags use ("Choose which events to change").

**Editor** (`EventForm.tsx`, `EventPanel.tsx`, `useEventEditing.ts`): new `EventRepeatField.tsx` and `EventRemindersField.tsx`, in the editor's lazy chunk.

- **Repeat:** a `Combobox` of presets (**Does not repeat**, **Every day**, **Every weekday**, **Every week on <weekday>**, **Every month on the <n>th**, **Every month on the <nth weekday>**, **Every year on <date>**, **Custom…**); Custom opens inline fields (`NumberInput` interval 1 to 99, unit, weekday toggles for weekly, **Day of the month | Weekday of the month** for monthly) and **Ends**: Never | On <date> | After <n> times. A series with a rule the form cannot represent shows **Custom repeat** as read-only text (the `custom` flag) and the other fields stay editable. An existing series shows its rule from `GET` detail (`series.repeat`); for Microsoft it is loaded from there as well.
- **Reminders:** a list of "<n> <unit> before" rows with Add and Remove, a preset menu (At the start, 5 minutes, 10, 15, 30 minutes, 1 hour, 1 day before, 2 days), `maxReminders` rows at most (one for Microsoft), and for an all-day event presets as "<n> days before at 09:00" and "On the day at 09:00" (not offered for Microsoft, which cannot take a negative value, open question 5); when `remindersOther > 0` a line "1 other reminder is kept" and a line "The calendar app sends reminders; OpenTodo does not." Loaded from `GET` detail; offline, the field is disabled with the reason and left out of the `PATCH` (the description rule of phase 2).
- **Save and Delete** on an occurrence open the scope menu first, offering only what the change allows (`scopesFor`); with a `repeat` change and `series.exceptions > 0` the menu item's description says "Changing the repeat resets events you changed individually". **Create** (`Add event`) sends `repeat` and `reminders` and no scope. A series' `Convert to task` and `Duplicate` are unchanged (D11 phase 2) except that Convert's delete switch deletes the occurrence (`this`).
- **Locked series:** an occurrence with `series_unsupported` opens the read-only popover with "This repeat is too complex to change here" (`eventAccessText`, replacing "Repeating events are read-only for now"); guests, read-only and edits-off keep their texts. Interface budget (`docs/interface-guide.md`): 0 new chrome rows; the menu is an overlay; the editor gains two fields in an existing panel; 0 new shortcuts; no new scroll region.

### D13. Agents and the CLI

- **MCP.** `update_calendar_event` and `delete_calendar_event` gain `scope` (`this|following|all`); `create_calendar_event` and `update_calendar_event` gain `repeat` (the REST object) and `reminders` (array of minutes). `EventEditor` (`internal/agenttools`) gains `Scope` on `EventUpdate` and `EventDelete`, `Repeat *EventRepeat` and `Reminders *[]int` on `EventUpdate` and `EventCreate`, and `Validate` runs the body rules of D3 (including the `scope_required` rule, which needs the event's series flag: `list_calendar_events` rows gain `series: {id, repeats: true, first, custom}`, so an agent knows before it writes). The tool description for `scope` is part of the contract: *"scope (this | following | all) says which events of a repeating series the change is for. It is required when the event has a `series` in `list_calendar_events` and refused otherwise. `this` changes only that occurrence. `following` changes that occurrence and every later one: the series is split, a new series starts at that occurrence (on the first occurrence it equals `all`). `all` changes every occurrence, past and future. Changing the repeat rule needs `following` or `all`. Delete uses the same values."* `repeat` and `reminders` descriptions say that reminders are sent by the calendar provider, that Microsoft calendars take one, and that `repeat: null` is valid with `following` only. Same gate, limits (one item per write), `agent.write` audit, 600 an hour limiter and `calendar_event.changed` with `event_scope`. The in-app assistant and routines still do not offer them (`notInApp`, `routine_tools_test.go`); counts in `mcp_test.go` do not change (no new tool).
- **CLI** (`internal/cli/cmd_calendar_events.go`). `ot calendar event edit <id> --scope this|following|all` and `ot calendar event delete <id> --scope ...` (without `--scope` on an occurrence the command exits 2 and lists the three; the event's `series` comes from the events route it already reads). `add` and `edit` gain `--repeat daily|weekdays|weekly|monthly|yearly` with `--every N`, `--days mo,we` (not `--on`: phase 1 already uses `--on <day>` for finding the event), `--by monthday|weekday`, and the end as `--until DATE` or `--count N`; `--no-repeat` (edit, with `--scope following`); `--remind 10m` (repeatable; `m`, `h`, `d`; all-day also `--remind-at 09:00-1d`) and `--no-remind`. A table column REPEATS appears in `ot calendar events`. Exit codes as phase 2: `scope_required` exits 2 (a usage error), `series_unsupported` and `following_unsupported` exit 1.

### D14. E2EE, privacy and operations

Events, repeats and reminders are server data the server already holds in the clear; the write sends them to the remote calendar as typed and nothing new is learned. No content field changes (`internal/e2ee/contentfields.go`). The audit log gets `event_scope`, never a title or a rule text. Reminders are **not** delivered by OpenTodo: no push, no inbox item, no new notification path, no new outbound call; the editor says so. Rate limits: one limiter unit per request. Series reads and writes stay inside the existing credential, host and `Worker.lock` rules.

### D15. Size and cost

No dependency. **Server:** about 1,100 lines of Go outside tests (engine 700, writers 250, store and routes 150), a few KB of binary, nothing resident. **Web (estimate, to confirm with `make bundle-budget`):** the repeat and reminder fields and their helpers are about 3.5 KB gzip in the editor's lazy chunk; the scope menu and `eventScope.ts` about 1.2 KB gzip in the calendar route chunk; the scope/series fields of `calendarEventEdits.ts` and `calendarFeeds.ts` about 0.3 KB gzip in the entry chunk, whose headroom is 0.1 KB (`entryGzipKB` 143.6, `web/bundle-budget.json`): the change **raises `entryGzipKB` and `initialGzipKB` by the measured delta on purpose**, with a dated note as phases 1 and 2 did. Go files and functions stay inside `internal/codesize` (150 lines per function, 1,100 per file), which is why the engine is split by scope.

### D16. Accessibility and translation

The scope menu is a menu from the overlay module (focus first item, arrows, Enter, Escape returns focus to the block, `role="menu"` and `menuitem`, labelled by "Change which events"); the repeat field is a Combobox plus labelled controls, and the weekday toggles are a `role="group"` of toggle buttons with visible text; the reminders list is a list with labelled Remove buttons; the saving state is `aria-busy` as in phase 1. `docs/accessibility.md` and `scripts/ux-audit/screens.json` gain an `event-scope-menu` overlay entry (`on: "week"`, handler in `layout-screens.mjs` that drags a series block and waits for `[data-event-scope]`). **Translation:** about 45 new strings through `t()` into `web/src/locales/messages.json` and **all six catalogs** (`node scripts/i18n/extract.mjs --write`; `make i18n-check`), drafted for review by a speaker of each language and never machine-made into a catalog unreviewed (`docs/translating.md`); counts and names use placeholders ("{n} events", "{weekday}"), never glued pieces; weekday and month names come from the platform's formatters.

### D17. Midnight-ending events

A series such as 23:30 to 00:00 ends at midnight of the next day. **Checked, not locked:** the grid treats an end of exactly `00:00` on the next day as same-day-ending (`eventEditable`), draws it to the bottom of the day (`blockOn`), keeps its 30-minute length through a move (`spanOf`/`plus`) and the server accepts an end after the start on any day. Nothing is fixed in code; the change pins it: web tests for a 23:30 to 00:00 occurrence (drag, resize of either edge, `calendar.nudge`, `calendar.shift-day`, the scope menu), a server golden for `this` and `all` on such a series (`DTEND` on the next day at `000000`; `all` +30 minutes gives `DTSTART 00:00`, `DTEND 00:30` the next day, i.e. the series moves a day later in the clock sense and its `BYDAY` rotates for a weekly rule), and a viewer-zone case where the end is not at midnight (not editable in the grid by the existing one-day rule; the editor and the popover remain the path).

### D18. Test plan

Go (`go test ./...`):

- **Golden iCalendar fixtures**, `internal/calfeed/testdata/series/<case>/{in.ics,change.json,out.ics,out_new.ics}` with `go test ./internal/calfeed -run Series -update` and a fixed clock, one directory per case: `this-move`, `this-rename-existing-exception`, `this-delete`, `this-delete-with-exception`, `following-daily`, `following-count` (60 occurrences split at the 47th), `following-with-exceptions-after` (the D6 example), `following-first-is-all`, `following-consumes-exception`, `following-new-repeat`, `following-stop-repeating`, `all-shift` (the D6 example), `all-rename-carry`, `all-zone-change`, `all-allday-toggle`, `all-weekly-day-shift`, `all-monthly-day-shift-refused`, `all-new-repeat-drops-exceptions`, `delete-*` for each scope, `reminders-add-keep-other`, `midnight-this`, `midnight-all`, `dst-crossing` (Europe/Berlin around 2027-03-28 for a daily series), and Google-like quirk cases: `google-utc-recurrence-id`, `google-cancelled-exception`, `google-date-series-date-until`, `google-utc-dtstart`, `exception-before-master`, `exdate-list`, `duration-master`, `floating`, `x-props-kept`. Locks: `locked-thisandfuture`, `locked-rdate`, `locked-exrule`, `locked-two-rrules`, `locked-unexpandable-rule`, `locked-two-masters`, `locked-guest-in-exception`, `locked-too-many-events`, `locked-too-big`.
- **Oracle property tests** next to the goldens: for each case, `Expand(out)` over a window equals the expected occurrence set computed from `Expand(in)` and the change (`this` differs in exactly one occurrence; `all` shifts every occurrence by the delta and keeps excluded days excluded; `following` has old before `S` and new from `S` with no overlap and no gap; a deleted occurrence is absent and the others unchanged; `RepeatOf(RRule(r)) == r` for the preset table and `Expand` of `RRule(r)` has the occurrences the form says).
- `internal/calsync/calsynctest`: `PUT` to a new href, `FailNextPut(n)`, a counter per method, objects built from the fixtures; `graphtest`: `seriesMasterId`, `type`, `recurrence`, `reminderMinutesBeforeStart`, `hasAttachments`, `isOnlineMeeting`, `GET .../instances`, `PATCH` of a master that drops its exceptions. `event_dav_series_test.go` and `event_graph_series_test.go`: each scope end to end against the fakes; **no write without a precondition**; the order of the two writes; the rollback when the second fails (412, 500, a timeout) and `partial_write` when the rollback fails; a replay of a half-done split reuses the new object; the cache after each write (rows, refs, series rows, `series.rev`); one `Notify` per write.
- `internal/store/calendar_event_series_migration_test.go` (runs twice, older tables and queries still pass, the `etag` reset); `internal/calfeed` store tests for `ReplaceSeries`, `DropSeries`, `SeriesUIDs`, `RefByEvent` with the series join.
- `internal/httpapi/calsync_events_test.go`: every row of D3's table, the scope rules (required, refused, `repeat` with `this`, `repeat: null` without `following`), the legacy `recurring` row served as `refresh_pending`, `series` on the events route, the audit `event_scope`, authentication and tokens as in phase 1.
- `internal/agenttools` and `internal/cli` tests for `scope`, `repeat`, `reminders`, the descriptions, exit codes.

Web (`npm test`, `npm run build` in `web/`): `eventScope.test.ts` (which scopes for which change and series), `eventRepeat.test.ts` (the preset words, the form to the REST object), `calendarEventEdits.test.ts` (scope in the command, same-scope merge, another scope waits in `next`, `following`/`all` patch only the edited block, `series.rev` applied to the series' rows, `refetch` calls `refreshEvents`, a refusal restores, no Undo for `following`, a delete with each scope, offline then online), `ScopeMenu.test.tsx` and `CalendarEvents.test.tsx` (drag then menu, Escape restores and sends nothing, the keyboard path, a single event does not ask), `EventForm.test.tsx` (repeat presets, custom read-only, reminders, the Microsoft limit of one, the warning about exceptions, Save asks the menu), `eventDrop.test.ts` for 23:30 to 00:00, `dragAlternatives.test.ts`, stories for the menu and the new fields. Then `make ux-audit`, `make i18n-check`, `make bundle-budget`, `make check`.

## Risks / Open questions

1. **Google's CalDAV.** Unverified against the real account: that it accepts a `PUT` of a whole object with a new exception `VEVENT` whose `RECURRENCE-ID` is in `TZID` form while its stored exceptions are UTC; that it keeps `X-` properties and `VALARM`s on re-serialisation; that a second recurring object in the same calendar is accepted at an arbitrary href; that it does not rewrite the `UID`. D6 sends back what it read, and the post-write read of the next cycle shows what it kept; run the manual smoke of task 7.5 on the owner's calendar before merging.
2. **Microsoft "stop repeating"** (`recurrence: null` on a master) and `relativeYearly` are refused until verified with a real mailbox.
3. **`calendarView` and `seriesMasterId`.** The default projection is believed to include it; the explicit `$select` of D9 makes that certain, at the cost of a longer request.
4. **Graph's master ETag is not in `calendarView`.** The guard is the occurrence's ETag plus the master's ETag read just before the write; a change to the master that does not touch the occurrence's ETag could be overwritten. Verify whether an occurrence's `@odata.etag` changes when its master does.
5. **Negative reminders for Microsoft** (all-day "on the day") are not offered; `reminderMinutesBeforeStart` is believed to be non-negative.
6. **Google's default reminders.** An event with no `VALARM` may still remind through the calendar's defaults; the editor shows no reminder and `reminders: []` cannot suppress them. The editor's line says the calendar app decides what it sends.
7. **Two-write failure leaves a duplicate** only when the rollback `DELETE` also fails; the cache then shows the duplicate and the toast says so. Acceptable for an event calendar; rejected alternative: old series first (a failed second write would lose the later occurrences).
8. **Exceptions after a split follow the new series.** If feedback says people expect them to stay, they can be dropped instead with a one-function change and a spec edit.
9. **A long series on a very large object.** The 256 KiB and 400-event limits lock it as `series_unsupported`; raise them if real calendars hit them.

## As built (groups 4–5)

Web half, in the order of the tasks. Where this differs from D1, D3, D11, D12 and D15, this section wins.

- **Types and API (D11).** `CalendarEvent.series`, `CalendarSubscription.maxReminders`, `CalendarRepeat`, `CalendarEventChange.repeat`/`reminders` and `EventCommand.scope` are D11 verbatim. Added: `EventScope`, `CalendarEventUpdate` (the `PATCH` answer; `event` can be `null` when the occurrence left the cached window), `EventCommand.nextScope`, the detail's `series`, `reminders`, `remindersMax` and `remindersOther`, and optional `repeat` and `reminders` on the draft and the create body. `CalendarEventReason` loses `recurring` and gains `series_unsupported`. `updateCalendarEvent` takes `scope` in the body, `deleteCalendarEvent` a fourth argument that goes in the query; a call without a scope sends exactly what phases 1 and 2 sent. Dexie stays at 31 and `mobile/app/src/storage/surface.json` is untouched (`dexie-surface.mjs --check`: up to date).
- **Outbox (D11).** `editEvent(ev, patch, now, before, scope)`. An edit with the command's scope merges; one with another scope, or one made while the command is in flight, waits in `next` with `nextScope` and is sent after the answer against the new id and `rev`; a third scope is refused with the toast "Saving this change first" (notice code `edit_blocked`). A delete for another scope than a queued edit waits in `next` too and is sent right after the answer, not after 8 seconds (as phase 1's delete asked while an edit is in flight); every other delete keeps the 8 second delay for every scope. `before` (`EditBefore`) carries what Undo puts back: the description, the repeat and the reminders. On `200` the answer's `series.rev` is written to `rev` and `series.rev` of every cached row with that `series.id`; `refetch: true` calls the new `refetchEvents()` in `calendarFeeds.ts`, which waits for a refresh already running (it may have read before the write) and starts another. An answer with `event: null` drops the row, and a waiting `next` is told the event changed. A 422 `event_not_editable` is worded by the reason its message ends with (`series_unsupported`, `following_unsupported`, the others of `eventAccessText`); `scope_required` has its own text.
- **`eventScope.ts`, `eventRepeat.ts`, `eventReminders.ts` (4.2).** `scopesFor(ev, change | 'delete')` returns `{scope, blocked?: 'day-shift'}[]`: `this` is left out for a repeat change, `following` on the first occurrence, and `repeat: null` is `following` only, also on the first occurrence (the server needs it to turn the object into a single event). `eventRepeat.ts` holds the presets (`repeatForPreset`, `presetOf`), `withFreq`, `restRepeat` (what a request carries: `days` always, `by` for monthly only), `sameRepeat`, the words (`repeatSummary`, `endsLabel`, one sentence per ordinal) and `isMicrosoftCalendar(sub)` (`maxReminders === 1`: the REST contract carries no provider flag, so this is how the editor knows a Microsoft series takes one reminder and cannot stop repeating). `eventReminders.ts` has the presets, the words (an all-day reminder reads as a time of the day, "1 day before at 9:00") and add/remove.
- **Where the scope is asked (D12).** Every path ends in `eventActions.changeEvent` and `deleteEvent`, so they ask (`scopeFor`) when the event has a `series` and no scope was passed; the editor's and the popover's Save ask before they close, anchored to the Save button, and cancelling the menu leaves them open. While the menu is open the change is drawn on the cached row by `lib/eventPreview.ts` (no command is queued before the answer, so nothing is held in the outbox) and put back when the menu closes; the block shows the saving state through `useEventEdits` (`aria-busy`). The menu opens at the bottom-left of the block (after a drag, where the block now is), not at the pointer. For the keys, `useEventEdits.nudge` draws each step on an occurrence's row and asks once 300 ms pass without another key (`EDIT_SETTLE_MS`). `ScopeMenuHost` is mounted by `useCalendarChrome` and loads `ScopeMenu` (0.8 KB gzip) only when a request is open; the store is `eventScopeStore.ts`.
- **Undo.** None for `following`, and none for a change whose repeat has no previous rule to put back (a single event that began to repeat); `all` undoes with scope `all`, `this` with `this`.
- **Editor (D12).** `EventFull` gained `repeat` and `reminders`; `EventExtras` (from the detail: `repeat`, `custom`, `exceptions`, `reminders`, `remindersMax`, `remindersOther`) is the optional fourth argument of `eventFullOf` and fifth of `eventFullChange`. The rule is read from the row's `series.repeat` and the detail; a custom rule is never sent, and a Microsoft series shows a skeleton (or, offline, "left as it is") until the detail gives its rule. Reminders are sent only when the detail loaded them and they changed; a server that does not answer with `reminders` shows no reminders field. The repeat field is a Combobox of presets, Custom fields (`NumberInput`, a `SegmentedControl` for the unit and for monthly "day or weekday", a `role="group"` of weekday toggles) and an Ends row shown whenever the event repeats; the note "Changing the repeat resets events you changed individually" is beside the field and on the menu items when `series.exceptions > 0`. The popover shows the rule's words (`data-event-repeat`) in its lazy actions. Duplicate copies an occurrence as a single event and Convert's delete is scope `this` (`queueDelete(ev, 'this')`).
- **Audit.** `auditFormat.ts` words `event_scope` ("this event", "this and following events", "all events"); an unknown value adds nothing.
- **Midnight (D17).** Nothing needed fixing: `eventDrop.test.ts` pins a 23:30 to 00:00 occurrence (editable, a drag keeps its 30 minutes, either edge, the keys, shift-day, a viewer zone where the end is not at midnight). No zone has a 15-minute offset that would split a 30-minute block across midnight, so the "not draggable" case is pinned with a one-hour variant (23:30 to 00:30), which ends after midnight in its own zone.
- **Bundle (D15).** Against a build of this branch's base commit: entry 146,962 to 146,947 bytes gzip (143.52 to 143.51 KB), first load 714,804 to 715,349 (698.05 to 698.58 KB, +0.53 KB, the same 165 chunks). The editor chunk grows from 4.3 to 6.4 KB gzip (D15 estimated 3.5 KB for the two fields), the scope menu is a 0.8 KB chunk. `initialGzipKB` 698.1 to 698.7 with a dated note; `entryGzipKB` stays 143.6.
- **i18n.** 47 new messages in the six catalogs with the English text as the placeholder for review (the four plural ones in `zh` keep only their `other` branch); the stale "Repeating events are read-only for now" is gone.
- **Not done here.** The scope menu's "Graph drops exceptions" note beside All events for Microsoft when a time changes is only given on the editor's Save (the grid's drag does not know `series.exceptions`); `scripts/ux-audit` inventories and `docs/` are group 7; UI baselines for the new stories come from the baselines workflow; the keyboard path for the menu is covered in happy-dom, not in a browser.

## As built (groups 1–3)

Deviations and decisions of the Go chain (engine, writers, routes):

- **Names.** `ReadSeries` returns `calfeed.SeriesRead` (with `.Info()` for the cache row); `calfeed.SeriesView` is the served `series` object of `EventRow`, filled by the store's join (not the route). `SeriesInfo` gained an unstored `Key` (Microsoft: the seriesMasterId the id derives from). `OccurrenceVEvent`, `SeriesReminders`, `ParseCalendarRoot`, `RoundTrips`, `NewSeriesUID`, `SeriesID` are exported helpers.
- **Bookkeeping.** `SEQUENCE` and `DTSTAMP` are always set; `LAST-MODIFIED` only when the VEVENT already has one, so the goldens equal D6's examples exactly.
- **Extra locks (`series_unsupported`).** An exception whose `RECURRENCE-ID` is not an instance of the rule (readers ignore it, a blind rewrite would not); an unresolvable `TZID`; a floating master under `X-WR-TIMEZONE`; an object that does not parse back unchanged (`RoundTrips`, also checked on every object before a write, for single events too).
- **Weekly repeat must include the start's weekday** (`ErrRepeatStart`, served 400 `invalid_input`, field `repeat`): otherwise the object would hold a DTSTART that is not an instance.
- **Day moves.** A weekly rule with interval above one only rotates when no day crosses the week boundary; otherwise `ErrShiftUnsupported`. `all` shifts by what the person moved relative to the occurrence as shown; a split's re-keying delta is relative to the split occurrence's nominal slot (D6).
- **Split writes.** The replay of a half-done split overwrites its own object with `If-Match` (so it holds the current change) instead of reusing it. A failure of the second write that may have been applied (transport error, 5xx) is first checked with a GET: applied is success, not applied rolls back, unknown is `partial_write` and nothing is undone. A rollback needs an ETag (never an unconditional or `*` delete), else `partial_write`.
- **Worker.** What a write replaces in the cache: `this` the uid only; other scopes every uid of the old series (`SeriesUIDs`). `EventResult` has `Gone` (edited occurrence left the window: answer `event: null`), `Series`, `Refetch`. The Microsoft series row cannot hold the master id (migration fixed by D2), so the writer reads `seriesMasterId` from the occurrence at write time. A cached `recurring` lock is `refresh_pending`.
- **Microsoft.** An all-day switch and "stop repeating" are refused (`invalid_input`); relative yearly, attachments and online meetings refuse `following` (`following_unsupported`); the POST of the new series has no precondition (Graph offers none; `transactionId` makes it idempotent). The fake models Outlook as assumed in open question 4 (an occurrence's ETag derives from its master's) and drops exceptions on a master time or pattern change.
- **HTTP.** `repeat` accepts omitted, object or null (create: null is none); unknown repeat members are 400. `calendar_event.changed` takes the scope as a fifth argument. The agent editor only compiles against the new signatures and maps `scope_required`/`invalid_input`; its tool fields are group 6. Fixtures: `internal/calfeed/testdata/series/` (inputs authored with their own VTIMEZONE so goldens are platform independent), `calsynctest` gained `FailNext`, `FailNextAfterApply`, `Writes`.
- **Open questions.** 1 (Google CalDAV) still needs the smoke of 7.5. 2, 3, 5 settled as designed. 4 is modelled, not verified. Not resolved: whether Graph keeps exceptions before a shrunk range (assumed yes).
