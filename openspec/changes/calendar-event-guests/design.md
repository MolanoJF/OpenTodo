# Design

## Context

Phases 1 to 3 (`calendar-event-edits`, `calendar-event-editor`, archived 2026-11-12, and `calendar-event-series`, built and not yet archived) are the base: the remote calendar is the truth, an event write is a server command with an ETag precondition and not an op, `rev` is a hash of the ETag, a device queues one command per event in `eventOutbox`, the editor is a panel, repeating events are changed for a scope. This change reuses all of that. What the code does today, and the facts the design had to work around:

- **Guests are one bit.** `calfeed.Event.Guests` is a bool set by any `ATTENDEE` property (`internal/calfeed/vevent.go:51-53` and `166-167`); `ORGANIZER`, `PARTSTAT`, `CN`, `ROLE`, `URL`, `X-GOOGLE-CONFERENCE` and `DESCRIPTION` are not parsed at all (the description is read from the calendar only when an editor opens, `calendar-feeds` "Event descriptions are read when needed"). Graph reads attendees as `{type}` only (`internal/calsync/graph_events.go:34-36`).
- **Every event with guests is locked.** CalDAV: `SeriesLocked` returns `has_guests` when any `VEVENT` of the object, master or exception, has an `ATTENDEE` (`internal/calfeed/series.go:323` and `331`). Graph: `graphEvent.locked` checks attendees first and `isOrganizer` second (`graph_events.go:148-150`), so a meeting somebody else organises is reported `has_guests`, never `not_organizer`; `not_organizer` is reachable only for an event that has no attendees.
- **The whole-object write is wrong for a reply.** Phase 3's bookkeeping (`SEQUENCE+1`, `DTSTAMP`, `LAST-MODIFIED`, series design D6) is right for an edit and wrong for a reply: RFC 5546 reserves a higher `SEQUENCE` for an organiser's change, and a server may treat an attendee's copy with a new `SEQUENCE` as an organiser edit. A reply must change one parameter and nothing else.
- **Google's connection does not know the user's address.** The connection is created with `Name: "Google Calendar"`, no `Username` (`internal/httpapi/calsync_handlers.go:481`), and the OAuth scope is calendar only (`internal/calsync/google.go:26`), no `email`. Matching "you" among the attendees needs another source (D1, D6). Microsoft's connection keeps the mailbox address in `Username` (`calsync_microsoft.go:214`) and Graph says it directly (`isOrganizer`, `responseStatus`).
- **Graph reads leave out what this needs.** `windowSelect` (`graph_transport.go:289`) and `eventSelect` (`event_graph.go:25`) have no `organizer`, `responseStatus`, `onlineMeeting` or `isOnlineMeeting`; `graphEvent` declares `IsOnlineMeeting` and `HasAttachments` (`graph_events.go:51-52`) but nothing selects them. Calendars are listed with `$select=id,name,hexColor,canEdit` (`graph.go:202`).
- **The services say "never".** Phase 2's requirement "Events can be changed and deleted in the connected calendar" says the server SHALL "never send an invitation or update to guests"; `internal/agenttools/write_calendar_events.go:37` tells agents "No invitation or update is ever sent to guests"; `docs/calendar-sync.md:59` and `docs/api.md:1909` say nobody is notified; `docs/add-calendar-events.md:37` and `44` promise Duplicate copies no guests and guests are not part of the editor. All of these change in words, and Duplicate stays guest-free.
- **Descriptions and this data are not in the export, but are in a backup.** `internal/httpapi/export.go` has no calendar table; a backup is `VACUUM INTO` the whole database (`internal/backup/archive.go:96`), so a cache table is in a backup snapshot as `calendar_events` already is.
- **The assistant never sees events with text.** `list_calendar_events` and the three write tools are in the in-app assistant's `notInApp` map (`internal/assistant/loop.go:425-437`) and `internal/freetime.Event` is "without its text" (`freetime.go:43`); the device twin reads `db.calendarEvents.toArray()` whole rows (`web/src/lib/assistantDevice/readFreeTime.ts:256`) and must be proven to use times only (D12, task 5.7).

## Provider research

Everything the design assumes about how a provider treats guests is listed here with its source and status. "Verified" means read on the page on 2026-11-13; anything else is an **open question** (Risks section) and the design behaves conservatively for it.

### Google CalDAV (the owner's calendar)

| # | Claim | Source | Status |
| --- | --- | --- | --- |
| G0 | The CalDAV API needs OAuth 2.0; it implements `REPORT` (all reports but `free-busy-query`), `PUT`, `DELETE`, `If-Match`; no arbitrary WebDAV properties; no `MKCALENDAR`; no `VTODO`. | https://developers.google.com/workspace/calendar/caldav/v2/guide | Verified |
| G1 | It "supports a trivial inbox, which is always empty". "Invitations you receive are automatically delivered into your events collection." There is no mention of a scheduling outbox, `schedule-inbox`/`schedule-outbox` collections, `SCHEDULE-AGENT`, `calendar-user-address-set`, or any notification on a write. | same page | Verified (absence). RFC 6638 scheduling (https://datatracker.ietf.org/doc/html/rfc6638) is therefore **not assumed**: there is no outbox to POST an iTIP reply to, and `SCHEDULE-AGENT=NONE` is not known to be honoured. |
| G2 | Whether a `PUT` that adds, removes or edits attendees of an organiser's event makes Google email the guests. The guide says nothing. Google's data model sends updates (organiser changes "are propagated to the attendees", https://developers.google.com/workspace/calendar/api/concepts/inviting-attendees-to-events) and users without Google Calendar "will now always be sent an email when an event is created, updated or deleted" (https://workspaceupdates.googleblog.com/2018/10/non-google-calendar-users-receive-email-updates.html); the REST API controls this with `sendUpdates`, which CalDAV has no equivalent of. | pages above; a third-party issue claims Google's CalDAV path sends no invitation emails (https://github.com/FreeshardBase/calendar/issues/410, a project's note, not Google) | **Unverified; treated as "may email real people, cannot be turned off"** |
| G3 | What a `PUT` that changes only the user's own `PARTSTAT` on an invited event does: kept and propagated to the organiser, silently ignored, rewritten, or rejected. Google's model says "the only event change that is propagated from attendees back to the organizer is the attendee's response status" and that an attendee's other edits "are only reflected on their own copy" (same concepts page), which makes a `PARTSTAT`-only change the one write that is meant to propagate. An old SyncEvolution report says Google replaced `PARTSTAT=ACCEPTED` with `NEEDS-ACTION` (https://gitlab.freedesktop.org/SyncEvolution/syncevolution/issues/10, known from a search snippet only: the page was not readable). RFC 6638 section 3.2.2.1 lets an attendee change their own `PARTSTAT` and nothing else of substance, and a server that refuses answers 403 with the `allowed-attendee-scheduling-object-change` precondition (section 3.2.4.4). | pages above | **Unverified; design reads the object back and reports a kept or not-kept answer** |
| G4 | `X-GOOGLE-CONFERENCE` carries the Meet link in Google's `.ics` exports; whether the CalDAV resource carries it and `DESCRIPTION` also holds a Meet URL. | not in the guide | **Unverified; URL scan of `DESCRIPTION`, `LOCATION` and `URL` is the fallback** |
| G5 | Creating a Meet needs `conferenceData.createRequest` with `conferenceDataVersion=1` on the Calendar REST API (https://developers.google.com/workspace/calendar/api/guides/create-events). | not fetched | **Not needed: out of scope** |
| G6 | The account's own address: the primary calendar's id is the account email and appears as the calendar's path segment in `https://apidata.googleusercontent.com/caldav/v2/{calendarId}/events`; the principal found by `current-user-principal` is believed to contain it. | `GoogleCalDAV`, `internal/calsync/calsync.go:28`; no Google page read | **Unverified; no match means no "you", no reply, and the event stays `has_guests`** |

**Conclusion for Google.** Reading guests and the join link is safe. Replying (G3) is built, verified by a read back after every write and **shipped off** (`googleRespond = false`, D1) until the smoke test of task 7.5 flips it. Organiser edits, adding and removing guests, and deleting an organiser's event stay **locked as `has_guests`** (G2): the product cannot say honestly whether guests are told.

### Microsoft Graph

| # | Claim | Source | Status |
| --- | --- | --- | --- |
| M1 | `POST /me/events/{id}/accept`, `/tentativelyAccept`, `/decline` take a body with `comment` and `sendResponse` ("`true` if a response is to be sent to the organizer; otherwise, `false`. Optional. Default is `true`"), answer `202 Accepted` with no body, need `Calendars.ReadWrite` (delegated, work and personal). | https://learn.microsoft.com/en-us/graph/api/event-accept?view=graph-rest-1.0 | Verified |
| M2 | An event update that includes only `attendees` "sends a meeting update to only the attendees that have changed"; removing an attendee that came from a distribution list updates everyone; "If the event you're updating is the master event of a recurring series, contains multiple attendees, and has instances that have been updated separately, multiple notification emails will be sent out: one for the master series and one per instance that has been updated." There is no `sendUpdates` parameter. | https://learn.microsoft.com/en-us/graph/api/event-update?view=graph-rest-1.0 | Verified |
| M3 | "If the event is a meeting, deleting the event on the organizer's calendar sends a cancellation message to the meeting attendees." | https://learn.microsoft.com/en-us/graph/api/event-delete?view=graph-rest-1.0 | Verified |
| M4 | `isOrganizer` is true when the calendar owner organises; `responseStatus` is the type of response sent; `attendees` (up to 500) hold `emailAddress`, `type` (`required`, `optional`, `resource`) and `status.response`; `hideAttendees` shows each attendee only themselves; `onlineMeeting.joinUrl` is the join address (`onlineMeetingUrl` is to be deprecated); `isOnlineMeeting` and `onlineMeetingProvider` cannot be changed again once set. | https://learn.microsoft.com/en-us/graph/api/resources/event?view=graph-rest-1.0 | Verified |
| M5 | A calendar lists `allowedOnlineMeetingProviders` and `defaultOnlineMeetingProvider`; to make an online meeting set `isOnlineMeeting: true` and an `onlineMeetingProvider` the calendar supports; the join address is `onlineMeeting.joinUrl`. | https://learn.microsoft.com/en-us/graph/outlook-calendar-online-meetings | Verified |
| M6 | Whether personal Outlook.com calendars list `teamsForBusiness` (the enum offers `unknown`, `teamsForBusiness`, `skypeForBusiness`, `skypeForConsumer`). | M4, M5 | **Unverified; offered only when the calendar lists the provider, and the write error is mapped** |
| M7 | What `accept` on a **series master** does (the whole series is believed to be answered), and that an occurrence's answer becomes an exception. | not stated on the pages | **Unverified; smoke 7.5** |
| M8 | Whether an organiser's `PATCH` of `showAs`, reminders or categories alone sends an update. | not stated | **Unverified; every organiser write to an event with guests asks the confirmation (D8)** |

**Conclusion for Microsoft.** Graph tells guests itself on every organiser write and offers no way to stop it, so the only honest prompt is a confirmation. Replies honour `sendResponse`. No new OAuth scope.

### Generic CalDAV (Radicale, Nextcloud, iCloud, Fastmail)

Scheduling differs (sabre-based servers send iTIP and email themselves, Radicale does nothing), `calendar-user-address-set` is not read by discovery, and a reply could change only the user's copy while the organiser is never told. Guests and join links are read; **replies and organiser edits are not offered** (`GuestCaps{}` all false).

## Goals / Non-Goals

**Goals:** show who is invited and what each answered; answer an invitation from the app where the provider is known to deliver the answer; let the organiser of a Microsoft meeting change it and its guests with an honest confirmation; open a call in one click; create a Teams meeting; never send a message to a person the user did not confirm; never log, export or give an assistant a guest's address; stay downgrade-safe.

**Non-Goals:** the list of section "Out of scope" in the proposal.

## Decisions

### D1. Provider capabilities and the conservative rule

One small value, derived in code from the connection and never stored, says what each provider may do. It is served on managed subscriptions and decides every other rule.

```go
// GuestCaps is what OpenTodo does with the guests of a connection's events.
type GuestCaps struct {
	Respond bool // answer an invitation
	Silent  bool // the answer can be sent without telling the organiser (sendResponse false)
	Edit    bool // an organiser's change, guests added or removed, a delete: the provider tells the guests itself
}

func (c *Connection) GuestCaps() GuestCaps {
	switch c.Provider {
	case ProviderMicrosoft:
		return GuestCaps{Respond: true, Silent: true, Edit: true}
	case ProviderGoogle:
		return GuestCaps{Respond: googleRespond} // Edit stays false: open question G2
	}
	return GuestCaps{} // CalDAV: guests are read, nothing is written
}

// googleRespond is false until the smoke test of tasks 7.5 records that Google keeps a
// PARTSTAT-only write and tells the organiser (open question G3).
const googleRespond = false
```

- **"You".** The user's own address is known only where it can be read from the account, never guessed: Microsoft `Connection.Username`; Google the id segment of the principal (`current-user-principal` URL after `/caldav/v2/`), falling back to the calendar-home URL, and **only for the calendar whose id equals it** (a calendar shared in from a colleague has the colleague's id, so replies there are never offered: the user would answer as someone else); generic CalDAV none. `ownerAddress(c *Connection, calendarURL string) string` returns "" when unknown; with "" nothing is "you", `guests.you` is omitted and the event keeps its `has_guests` lock.
- **Conservative rule.** A capability that is not known to be true is false, and the UI hides what it cannot honour. A control is never shown with a promise (for example "Tell the organizer") the provider is not known to keep.

### D2. Migration `0077` and what the cache learns

Migration number **0077** (`0076_calendar_event_series.sql` is the last file). One idempotent side table keyed like `calendar_event_remote` (`0074`), no `ALTER`:

```sql
-- 0077_calendar_event_guests.sql
-- Who is invited to an occurrence of a managed calendar, how the user answered, and the
-- join link, so the events route can summarise them and a popover can list them without
-- asking the calendar. Server data, never an op. Personal data: guest names and
-- addresses (calendar-event-guests D12).
CREATE TABLE IF NOT EXISTS calendar_event_guests (
  subscription_id TEXT NOT NULL REFERENCES calendar_subscriptions(id) ON DELETE CASCADE,
  uid             TEXT NOT NULL,            -- as calendar_event_remote
  instance        TEXT NOT NULL,            -- as calendar_event_remote
  organizer       TEXT NOT NULL DEFAULT '', -- JSON {"name","email"}, '' when none is named
  is_organizer    INTEGER NOT NULL DEFAULT 0, -- the user organises it
  you             TEXT NOT NULL DEFAULT '', -- needs-action | accepted | tentative | declined | '' (not a guest, or unknown)
  guest_count     INTEGER NOT NULL DEFAULT 0, -- attendees other than the user, rooms included, uncapped
  attendees       TEXT NOT NULL DEFAULT '', -- JSON array of {name,email,role,response,you}, at most 100
  conference_url  TEXT NOT NULL DEFAULT '', -- https, validated (D10)
  conference_kind TEXT NOT NULL DEFAULT '', -- meet | teams | zoom | webex | other
  PRIMARY KEY (subscription_id, uid, instance)
);

-- Occurrences cached before this change have no guest rows and carry the old locks:
-- read every managed calendar once more.
UPDATE calendar_subscriptions SET etag = ''
  WHERE id IN (SELECT subscription_id FROM calsync_calendars);
```

- A row exists only for an occurrence that has guests, an organiser or a conference. `calfeed.EventRemote` gains `Guests *GuestInfo` and `Conference *Conference`; `storeRefs` writes the rows in the transaction that replaces the events and deletes the subscription's rows first (rows never outlive their events); `ReplaceSeries` and `DropSeries` handle them as they handle `calendar_event_series` rows. `Store.GuestsOf(ctx, userID, eventID)` reads one row for the cache-only route.
- **Caps.** At most 100 attendees are stored per occurrence (`guest_count` is the real number and `truncated` says so), a name at most 120 characters, an address at most 254, a conference URL at most 2,048. These bound the row at about 40 KB.
- **Downgrade.** An older binary ignores the table; the reset `etag` costs it one refetch, on which it writes its own refs with its own `has_guests` lock again.
- A cache row that predates this change says `has_guests` for every guest event; `refresh_pending` is not needed because the reset forces the read, and until it happens the old reason (the stricter one) stands.

### D3. REST contract

All routes keep the registration, gates, limiter (one unit per request) and tokens of phases 1 to 3.

```
GET /api/v1/calendar-feeds/events?from=&to=
  each occurrence of a connected calendar may gain:
    "guests": {"count": 3, "you": "needs-action", "organizer": false, "canRespond": true},
    "conference": {"url": "https://meet.google.com/abc-defg-hij", "provider": "meet"},
    "access": {"update": true, "delete": true, "notifies": true}      // notifies: a change or delete tells the guests
  guests is absent for an event with no other attendee and no answer of the user's;
  you is absent when the user is not a guest or their address is unknown.
  managed subscriptions[] gain:
    "guests": {"respond": true, "silent": true, "edit": true},
    "caps": {"create": true, "update": true, "delete": true, "onlineMeeting": true}

GET /api/v1/calendar-feeds/events/{eventId}/guests        // cache only, no request to the calendar
  200: {"organizer": {"name": "Ana Silva", "email": "ana@example.com", "you": false},
        "guests": [{"name": "Ben Costa", "email": "ben@example.com", "role": "required",
                    "response": "accepted", "you": false},
                   {"name": "", "email": "room-4@example.com", "role": "room", "response": "accepted", "you": false}],
        "count": 3, "truncated": false}
  404 not_found        // not the caller's, not cached, or no guests

GET /api/v1/calendar-sync/connections/{id}/events/{eventId}     // phase 3 answer, plus (read from the calendar now):
  {
    ...,
    "guests": {"organizer": {...}, "guests": [...], "count": 3, "truncated": false,
               "you": "needs-action", "canRespond": true, "canEdit": true},
    "conference": {"url": "https://teams.microsoft.com/l/meetup-join/...", "provider": "teams"},
    "onlineMeeting": {"available": true, "on": true}      // available: the calendar can make one; on: this event has one
  }

PATCH /api/v1/calendar-sync/connections/{id}/events/{eventId}   // phase 1 to 3 body, plus:
  {
    "rev": "a41c9d03e2b7f610",
    "guests": {"add": [{"email": "new@example.com", "name": "New Guest", "role": "optional"}],
               "remove": ["old@example.com"]},
    "onlineMeeting": true,
    "notifyGuests": true           // required for any change of an event whose access.notifies is true
  }
  200: as before; the event row carries the new guests summary

POST /api/v1/calendar-sync/connections/{id}/events          // phase 2 body, plus:
  {..., "guests": [{"email": "ben@example.com", "role": "required"}], "notifyGuests": true, "onlineMeeting": true}

DELETE /api/v1/calendar-sync/connections/{id}/events/{eventId}?rev=...&scope=...&notifyGuests=true
  204                               // notifyGuests required when access.notifies is true

POST /api/v1/calendar-sync/connections/{id}/events/{eventId}/response
  {"rev": "a41c9d03e2b7f610", "response": "accepted", "scope": "all", "sendResponse": true}
  200: {"event": <row as the events route serves it>, "replaces": "<id>",
        "series": {"id": "5e0c...", "rev": "9b7d..."},   // only for an occurrence; rev only for CalDAV
        "refetch": false}
```

- `role` is `required` (default) or `optional`; a room (`resource`) is read-only and kept. `response` is `accepted`, `tentative` or `declined`. `scope` on the response route is `this` or `all` and is required for an occurrence of a repeating event, refused for a single event. `sendResponse` defaults to true; `false` is accepted only where `guests.silent` is true.
- **Guests body rules.** `add` and `remove` each at most 50 entries; an address is trimmed, lower-cased for comparison, parsed with `net/mail.ParseAddress` (a bare `local@domain` only: no display name in the address, no angle brackets, no comma or newline), at most 254 characters, a domain with a dot; `name` at most 120 characters, no control characters; no duplicate within the body, the user's own address or the organiser's cannot be added or removed; the event may hold at most 100 guests after the change; removing an address that is not a guest is ignored; adding one that is already a guest only sets the role. A body that changes nothing is `invalid_input` (as phase 1's "nothing to change"). The error names the field and the index (`guests.add[1].email`), never the value.
- **Notify rule** (D8): a change, delete or create that tells people without `notifyGuests: true` is `400 invalid_input` field `notifyGuests`; `notifyGuests: false` where the provider cannot skip the message is the same error.

Errors: the earlier tables plus the rows below. **No new code** is added.

| Status | Code | When |
| --- | --- | --- |
| 400 | `invalid_input` | `notifyGuests` missing or false where access.notifies is true (field `notifyGuests`); a malformed `guests` body (field with index); `guests` or `onlineMeeting` on a calendar that cannot (field `guests`, `onlineMeeting`); `response` not one of three; `scope` refused for a single event, `following`, or `this` on Google; `sendResponse: false` where `guests.silent` is false; `onlineMeeting` when the calendar lists no provider and Graph refused it (field `onlineMeeting`) |
| 400 | `scope_required` | an occurrence's reply without `scope` |
| 422 | `event_not_editable` | reason `has_guests` (CalDAV and Google organiser event, any guest change), `not_organizer` (the user is a guest: no edit, no delete), `series_unsupported` (Graph `all` on a series with guests and exceptions), `following_unsupported` (Graph `following` on a series with guests), `cannot_respond` (reply refused: the user is the organiser, not invited, address unknown, the provider does not reply, or edits are off) |
| 403 | `calendar_read_only` | the calendar refuses (403 or the CalDAV `allowed-attendee-scheduling-object-change` precondition) |
| 409 | `event_changed` | stale `rev` |
| 502 | `calendar_unreachable` | reason `response_not_kept`: the write succeeded and the calendar's copy does not hold the answer; the cache is refreshed so the real answer is shown |

Order of checks on a guest write, under `Worker.lock(connID)`: body rules -> connection -> `edit_events` -> caps -> lock -> notify rule -> `scope` -> `rev` -> read the object and compare its ETag -> write -> read back (reply only) -> cache -> `Notify` once. The handler records `calendar_event.changed` with `event_action` `updated`, `deleted` or, new, `responded`, and the new detail keys `event_response` (`accepted|tentative|declined`) and `event_notified` (true when the write told people); never a name, an address or a link.

### D4. The pure engine (`internal/calfeed`)

```go
// Guest is one person on an event as the calendar lists it.
type Guest struct {
	Name     string
	Email    string // lower case, no mailto:
	Role     string // required, optional, room
	Response string // needs-action, accepted, tentative, declined
	You      bool
}

// GuestInfo is what an occurrence says about its guests.
type GuestInfo struct {
	Organizer   *Guest
	IsOrganizer bool    // the user organises it
	You         string  // the user's answer, "" when the user is not a guest or not known
	Guests      []Guest // at most MaxGuests, the user included, rooms included
	Count       int     // every attendee but the user
	Truncated   bool
}

// Conference is a join address and the service it belongs to.
type Conference struct {
	URL      string
	Provider string // meet, teams, zoom, webex, other
}

const (
	MaxGuests     = 100
	MaxGuestName  = 120
	MaxGuestEmail = 254
	MaxConferenceURL = 2048
)

// ReadGuests reads ORGANIZER and ATTENDEE of the VEVENT that is the occurrence
// (the exception of instance when there is one, else the master). owner is the
// user's address ("" when unknown); an address is matched as the text after mailto:,
// case-insensitively.
func ReadGuests(root *caldav.Component, instance, owner string) GuestInfo

// ConferenceOf finds the join address of an occurrence: X-GOOGLE-CONFERENCE, URL,
// then an address in LOCATION and DESCRIPTION; only https, only the services of D10.
func ConferenceOf(root *caldav.Component, instance string) *Conference

// ParseConference validates one address; trusted is true for a provider field
// (X-GOOGLE-CONFERENCE, Graph's joinUrl), whose host is not checked against the list.
func ParseConference(raw string, trusted bool) (Conference, bool)

// NormalizeEmail is the one email check of every write path.
func NormalizeEmail(s string) (string, error)

// Respond is the user's answer to an invitation.
type Respond struct {
	Owner    string // the user's address, never ""
	Response string // accepted, tentative, declined
	Scope    Scope  // this is refused here (ErrRespondThis); all or "" for a single event
	Instance string
}

// RespondResult says which VEVENTs changed (instance keys, "" for the master).
type RespondResult struct {
	Changed  []string
	Previous string
}

// ApplyRespond sets the owner's PARTSTAT, and nothing else, on the master and, for
// scope all, on every override whose own PARTSTAT equals the master's previous one.
// SEQUENCE, DTSTAMP and LAST-MODIFIED are not touched.
func ApplyRespond(root *caldav.Component, r Respond) (RespondResult, error)

var (
	ErrNotInvited    = errors.New("not invited")             // no ATTENDEE for the owner
	ErrIsOrganizer   = errors.New("user is the organizer")   // ORGANIZER is the owner
	ErrRespondThis   = errors.New("reply for one occurrence is not written")
	ErrOwnerUnknown  = errors.New("own address unknown")
)
```

`parseEvent` is unchanged except that `Event` gains `Organizer string` and `Attendees int`; the engine functions read the tree, as the series engine does. `SeriesLocked(root *caldav.Component, etag string)` gains a third parameter `owner string` and becomes the single place D9's CalDAV rows are decided.

### D5. EventWriter additions (Go, verbatim)

`internal/calsync/events.go` (the rest of the file is unchanged):

```go
// EventEdit gains the guests and the online meeting.
type EventEdit struct {
	// ... Summary, When, Location, Description, Free, Scope, Repeat, Reminders as before
	Guests        *GuestsEdit // nil: unchanged
	NotifyGuests  bool        // the caller confirmed that the provider tells the guests
	OnlineMeeting bool        // make it an online meeting (Microsoft); never turns one off
}

// GuestsEdit adds and removes guests (addresses already normalised).
type GuestsEdit struct {
	Add    []GuestAdd
	Remove []string
}

// GuestAdd is a guest to add or whose role to set.
type GuestAdd struct {
	Email    string
	Name     string
	Optional bool
}

// EventNew gains the guests and the online meeting.
type EventNew struct {
	// ... as before
	Guests        []GuestAdd
	NotifyGuests  bool
	OnlineMeeting bool
}

// Response is the user's answer to an invitation.
type Response string

const (
	ResponseAccepted  Response = "accepted"
	ResponseTentative Response = "tentative"
	ResponseDeclined  Response = "declined"
)

// ReplyEdit is one answer.
type ReplyEdit struct {
	Response     Response
	Scope        calfeed.Scope // this or all for an occurrence, "" for a single event
	SendResponse bool          // false only where GuestCaps.Silent
}

// EventState gains what a read of the guests and the conference gives.
type EventState struct {
	// ... as before
	Guests        calfeed.GuestInfo
	Conference    *calfeed.Conference
	OnlineMeeting OnlineMeeting // Detail only
}

// OnlineMeeting is whether the calendar can make an online meeting and whether the event has one.
type OnlineMeeting struct{ Available, On bool }

// EventWriter gains one method.
type EventWriter interface {
	// ... Read, Detail, Create, Update, Delete, DeleteScoped as before
	// Reply answers an invitation while the event still has ref.ETag, changing only
	// the user's own answer, and returns the event as the calendar holds it after
	// (read back). ErrResponseNotKept when the calendar's copy does not hold it.
	Reply(ctx context.Context, ref EventRef, reply ReplyEdit) (EventState, error)
}

var (
	ErrResponseNotKept = errors.New("calendar did not keep the answer") // 502 response_not_kept
	ErrCannotRespond   = errors.New("cannot respond")                   // 422 cannot_respond
)
```

`event_edit.go`: `EditEvent` and `DeleteEvent` gain the notify rule (`ErrNotifyRequired` is not an error value: the worker checks `access.notifies` and the flag before any remote call and returns `EventError{Code: "invalid_input", Field: "notifyGuests"}`); new `Worker.RespondEvent(ctx, userID, connID, eventID, rev string, reply ReplyEdit) (EventResult, error)` in `event_respond.go`, one unit of the limiter, one `Notify`. `EventResult` is unchanged (`Series`, `Refetch`). New reason strings `ReasonCannotRespond = "cannot_respond"` and `ReasonResponseNotKept = "response_not_kept"`; `LockGuests` and `LockNotOrganizer` are reused. `AccessOf` gains the `Notifies` member (`calfeed.Access{Update, Delete bool; Reason string; Notifies bool}`).

`internal/calfeed/managed.go`:

```go
// GuestRow is what the cache keeps of an occurrence's guests (migration 0077).
type GuestRow struct {
	Info       GuestInfo
	Conference *Conference
}

// EventRemote gains Guests *GuestInfo and Conference *Conference; storeRefs writes them.

// GuestsOf reads one occurrence's guest row for the cache-only route; ok is false when
// the event is not the user's, is not cached or has none.
func (s *Store) GuestsOf(ctx context.Context, userID, eventID string) (GuestRow, bool, error)
```

### D6. CalDAV and Google algorithms

**Reading.** `davRemote` runs `ReadGuests` and `ConferenceOf` for every occurrence of the object it reads, with the owner of the calendar (D1), and stores the rows. The `Guests bool` of `Event` is kept for the lock.

**A reply** (`Reply` for Google, behind `googleRespond`): read the object (`getObject`), compare the ETag with `ref.ETag`, `SeriesLocked`-style checks that matter for a reply only (`series_unsupported` still applies; `has_guests` and `not_organizer` do not: a guest is exactly who may reply), `ApplyRespond`, `RoundTrips`, `PUT` with `If-Match`, then **`GET` the object back** and read the user's `PARTSTAT` with `ReadGuests`. If it is not the requested one the answer is `ErrResponseNotKept` and the cache stores what the calendar holds. A 403 is `ErrCalendarRO` (and the `allowed-attendee-scheduling-object-change` precondition in the body is logged by name only). The write sends **no** `Schedule-Reply` header (RFC 6638 section 8.1 is not known to be honoured) and sets no `SCHEDULE-AGENT`: nothing in the request tries to steer how Google tells the organiser, because nothing about that is known (G1).

The change, for an invitation as Google serves it (the user is `you@example.com`, the calendar's owner):

```
BEGIN:VEVENT
UID:planning-77@google.com
DTSTART;TZID=Europe/Berlin:20261116T140000
DTEND;TZID=Europe/Berlin:20261116T150000
DTSTAMP:20261110T091500Z
ORGANIZER;CN=Ana Silva:mailto:ana@example.com
ATTENDEE;CUTYPE=INDIVIDUAL;ROLE=REQ-PARTICIPANT;PARTSTAT=ACCEPTED;CN=Ana Silva;X-NUM-GUESTS=0:mailto:ana@example.com
ATTENDEE;CUTYPE=INDIVIDUAL;ROLE=REQ-PARTICIPANT;PARTSTAT=NEEDS-ACTION;RSVP=TRUE;CN=You;X-NUM-GUESTS=0:mailto:you@example.com
ATTENDEE;CUTYPE=INDIVIDUAL;ROLE=OPT-PARTICIPANT;PARTSTAT=TENTATIVE;CN=Ben Costa:mailto:ben@example.com
SEQUENCE:2
SUMMARY:Planning
X-GOOGLE-CONFERENCE:https://meet.google.com/abc-defg-hij
END:VEVENT
```

becomes, for `accepted`, the same text with exactly one parameter changed on the user's line (`PARTSTAT=ACCEPTED`, `RSVP=TRUE` and every other parameter and the line's folding untouched); `SEQUENCE:2`, `DTSTAMP`, and absent `LAST-MODIFIED` stay as they are. A golden byte comparison pins this.

**A series reply** (scope `all`): the master's own `PARTSTAT` changes; every override `VEVENT` whose own `PARTSTAT` for the user equals the master's previous value changes with it (the carry rule of series design D6: an occurrence the user answered on its own keeps its answer); other `VEVENT`s are not touched. `this` is refused for CalDAV (`ErrRespondThis`, `400 invalid_input` field `scope`): whether Google honours an override `VEVENT` carrying only a `PARTSTAT`, and whether it creates the override at all, is open question G3. A series that is `series_unsupported` (D10 of phase 3) cannot be answered either.

**Never written for CalDAV in this change:** adding or removing `ATTENDEE`, `ORGANIZER`, `SEQUENCE` bumps for guest events, `SCHEDULE-AGENT`, `SCHEDULE-FORCE-SEND`, `Schedule-Reply`. The engine has no function for them. When a later change enables organiser edits for a server whose behaviour was verified, it adds `ApplyGuests` next to `ApplyRespond` and flips `GuestCaps.Edit`.

### D7. Microsoft Graph mapping

Reads: `windowSelect` and `eventSelect` add `organizer,responseStatus,isOnlineMeeting,onlineMeeting,hasAttachments`; `graphEvent` gains `Organizer`, `ResponseStatus`, `OnlineMeeting.JoinURL`, and its `Attendees` gain `EmailAddress{Name,Address}`, `Status.Response`, `Type`. Mapping: attendee `type` `required`/`optional`/`resource` to the roles `required`/`optional`/`room`; `status.response` `accepted`, `tentativelyAccepted`, `declined`, `notResponded`, `none`, `organizer` to `accepted`, `tentative`, `declined`, `needs-action`, `needs-action`, `accepted`. The user's answer is the event's own `responseStatus.response` (no address matching); the user's row in `attendees` is found by `Connection.Username` for `you: true`. `isOrganizer` decides `guests.organizer`.

```
POST /me/events/{id}/accept
{"sendResponse": true}
-> 202 Accepted           then GET /me/events/{id}?$select=<eventSelect> for the new ETag and answer

POST /me/events/{id}/tentativelyAccept     POST /me/events/{id}/decline      (same body)

PATCH /me/events/{id}           If-Match: <ETag read now>
{"attendees": [
   {"emailAddress": {"address": "ben@example.com", "name": "Ben Costa"}, "type": "required"},
   {"emailAddress": {"address": "new@example.com", "name": "New Guest"}, "type": "optional"}]}
-> 200 with the event      (the collection is replaced: the writer sends the current list, every
                            existing guest and room kept with its address, name and type, then
                            the additions and without the removals; status is not writable)

POST /me/calendars/{calendarId}/events
{"subject": "Planning", "start": {...}, "end": {...},
 "attendees": [{"emailAddress": {"address": "ben@example.com"}, "type": "required"}],
 "isOnlineMeeting": true, "onlineMeetingProvider": "teamsForBusiness",
 "transactionId": "opentodo-<clientId hash>"}
-> 201 with onlineMeeting.joinUrl
```

- **Reply scopes.** A single event: `accept` on its id. An occurrence: `this` on the occurrence id (Outlook records an exception), `all` on the `seriesMasterId` read from the occurrence at write time (M7: believed to answer the series; smoke 7.5). `sendResponse` is sent as the caller said.
- **Edits of an event with guests.** The `PATCH` of any change is sent only with `notifyGuests` (D8). An organiser's series edit: `this` patches the occurrence (one update); `all` patches the master and is **refused** (`422 event_not_editable`, reason `series_unsupported`) when the master has exceptions, cancelled or not (`exceptionOccurrences`, `cancelledOccurrences`: M2's "multiple notification emails"); `following` is refused (`following_unsupported`): it would `POST` a new series with the guests and invite everyone again.
- **Guests on delete.** `DELETE` on the organiser's meeting cancels it for the guests (M3), so it needs `notifyGuests` like any change; `scope` `this` deletes one occurrence (a cancellation of one), `all` the master, `following` refused as above. An attendee's event is never deleted from here (`not_organizer`): Outlook's behaviour for an attendee's delete is not documented, and "Decline" is the answer to offer.
- **Online meeting.** `onlineMeeting: true` on `PATCH` or `POST` sets `isOnlineMeeting: true` and `onlineMeetingProvider: teamsForBusiness` and is offered only when the calendar's `allowedOnlineMeetingProviders` contains it (`$select` of the calendar list gains `allowedOnlineMeetingProviders`; the result is stored as `caps.onlineMeeting` in the connection's calendars JSON, absent on older rows and then "unknown": offered, and a 4xx from Graph becomes `400 invalid_input` field `onlineMeeting` with the message "This calendar can't make online meetings", nothing created). The join address is read from the answer's `onlineMeeting.joinUrl`. Never turned off (M4: Outlook ignores it).
- **Cache.** After a reply, a guest change or a delete the writer reads the event back (`GET`, `eventSelect`) and the worker stores it as phase 1 does (`ReplaceEvent`); for `all` on a series it uses `DropSeries` and `instances` as phase 3 D9 does.

### D8. Notifying guests

The rule, in one sentence: **nothing that tells a person is sent without the user confirming it, and the confirmation offers only what the provider honours.**

- `access.notifies` is true for an event whose organiser is the user, that has at least one other attendee (a room counts) and whose provider has `GuestCaps.Edit` (today: Microsoft). For it, every `PATCH`, the `DELETE` and the guest changes need `notifyGuests: true` **whatever fields the change carries**: M8 says nobody knows which Graph writes stay silent, so none is assumed to. A reply is not a change of the event and needs no confirmation: its message is the one the user asked for, shown in the reply control.
- `notifyGuests: false` is refused for Microsoft with `400 invalid_input`: Graph cannot skip the message. A provider that could (a future `GuestCaps.Choice`) would honour `false`; none does, so the contract says only `true`.
- **The web confirmation** is a small dialog, never a menu with options the server refuses: "Tell 3 guests?" and, in one sentence, "Outlook sends them this update. You can't turn that off." with **Save and tell guests** and **Cancel** (**Delete and tell guests** for a delete; **Invite and save** for a create). It is asked once per change, after the scope menu for a repeating event, and Cancel puts the block back and sends nothing. The words come from `lib/eventNotify.ts`, which reads `access.notifies` and the guest count, so a future provider adds its second option there and nowhere else.
- **No Undo** for a change that told guests (Undo would be a second message); the toast says "Guests were told". A delete keeps the 8-second window of phase 1 after the confirmation: nothing is sent until it has passed.

### D9. Locks after phase 4

`access.reason` keeps its seven values. What changes is when they apply, per provider. "Organiser" means `ORGANIZER` (CalDAV) or `isOrganizer` (Graph) is the user; "guest" means the user is an attendee and not the organiser.

| Event | CalDAV (generic) | Google | Microsoft |
| --- | --- | --- | --- |
| No other attendee | as phase 3 | as phase 3 | as phase 3 |
| Organiser, guests | locked `has_guests`; read-only guests | locked `has_guests`; read-only guests | **editable and deletable** with `notifyGuests`; `access.notifies`; guests addable and removable; a series: `this`, `all` (no exceptions), never `following` |
| Guest | locked `has_guests` (own address unknown); no reply | locked `not_organizer` when the address matches, else `has_guests`; reply `all` or single when `googleRespond` | locked `not_organizer`; reply `this`, `all` |
| Organiser unknown (no `ORGANIZER`, guests present) | locked `has_guests` | locked `has_guests` | `has_guests` only if `isOrganizer` is absent |
| Guests in an exception of a series | locked `has_guests` for the whole series (phase 3 D10) | same | `this` allowed on that occurrence; `all` refused `series_unsupported` |
| `series_unsupported` object | locked, no reply | locked, no reply | locked |
| No ETag | `no_etag` | `no_etag` | `no_etag` |
| Read-only calendar or edits off | `calendar_read_only`, `edits_off` | same | same |

Order of reasons for one event is the phase 3 order with one change: for Graph and for CalDAV where the user's address is known, `not_organizer` is evaluated **before** `has_guests` (it fixes the order of `graph_events.go:148-150`, which hides `not_organizer` behind `has_guests`). `Convert to task` keeps its delete switch off and disabled for every event with guests, whatever the provider, so that converting never sends a cancellation. `Duplicate` copies no guests.

What stays locked, in one place: Google and CalDAV organiser events (G2); every guest's copy of an event for time, title, place and notes (guests never edit the organiser's event); Graph `following` with guests; Graph `all` with guests and exceptions; deleting an invited event; removing an online meeting.

### D10. Join links and online meetings

`ConferenceOf` order: `X-GOOGLE-CONFERENCE` (trusted), the `URL` property, `LOCATION`, `DESCRIPTION`; Graph: `onlineMeeting.joinUrl` (trusted), then `location.displayName`. For text fields the first address that passes `ParseConference` wins, scanning for `https://` and cutting at whitespace, `>`, `"`, `)` or `<`.

`ParseConference(raw, trusted)` accepts a value only if: `url.Parse` succeeds; the scheme is exactly `https` (not `http`, `javascript`, `data`, `intent`, `zoommtg`, `msteams`, `tel`); there is no userinfo (`https://zoom.us@evil.example` is refused); the host is non-empty, ASCII, lower-cased, without a port other than 443; the length is at most 2,048 with no control character; and, unless `trusted`, the host is one of `meet.google.com`, `zoom.us` or `*.zoom.us`, `*.zoomgov.com`, `teams.microsoft.com`, `teams.live.com`, `*.webex.com` (suffix match on a dot boundary). Provider fields are still `https` only and still checked for userinfo and length. `provider` is `meet`, `teams`, `zoom`, `webex` or, for a trusted address on another host, `other`.

The server never requests the address (no prefetch, no preview, no redirect following, no health check) and never logs it: it is a secret-like value (a passcode is often in the query). The client renders an `<a href target="_blank" rel="noopener noreferrer">` and re-checks the `https:` scheme before rendering, so a stale or hostile cached row cannot make a clickable `javascript:` link. The button shows the service name and the host ("Join Google Meet", title "meet.google.com") so the user sees where it goes. ICS feed events get no Join.

### D11. Web data layer

Types (`web/src/lib/types.ts`): `CalendarEvent` gains `guests?: {count: number; you?: GuestResponse; organizer: boolean; canRespond: boolean}` and `conference?: {url: string; provider: 'meet' | 'teams' | 'zoom' | 'webex' | 'other'}`; `CalendarEventAccess` gains `notifies?: boolean`; `CalendarSubscription` gains `guests?: {respond: boolean; silent: boolean; edit: boolean}` and `caps.onlineMeeting?: boolean`; `CalendarEventChange` gains `guests?: {add?: GuestInput[]; remove?: string[]}`, `onlineMeeting?: boolean`; `CalendarEventUpdate` is unchanged. New `EventGuestList` (the cache-only route and the detail's `guests`). **No Dexie version** (non-indexed members; `mobile/app/src/storage/surface.json` unchanged, verify with `git diff --stat`).

New pure modules, small and tested: `lib/eventGuests.ts` (`guestSummaryWords(ev)`: "3 guests, 2 accepted", `responseWords`, `validEmail` as a UX hint only), `lib/eventConference.ts` (`joinOf(ev)`: the link and its words, null unless the scheme is `https`), `lib/eventNotify.ts` (`notifyPrompt(ev, kind)`: the dialog's texts or null when the change tells nobody).

**Never queued.** `calendarEventEdits.ts` gains `sendNow(ev, request)`: a request that tells people (`ev.access.notifies`, a reply, a create with guests) calls the API directly, with the optimistic block drawn by `eventPreview` as the scope menu does, and no `eventOutbox` row, so nothing can be sent later from a queue. Offline (`useOnline`) these controls are disabled with the reason "Guests are told when you're online". On a refusal the block is restored and the reason shown (the phase 2 texts plus `has_guests` provider-aware: "Google doesn't say whether changing this would tell the guests, so it's read-only here", `cannot_respond`, `response_not_kept`: "Google didn't keep your answer"). A reply, once answered, writes the answer into the cached row's `guests.you` and the new `rev` (as `series.rev` is applied in phase 3) and, for `refetch`, runs `refetchEvents()`.

The guest list route result is held in component state and a short in-memory map keyed by event id; it is dropped when the popover closes and never written to Dexie, `localStorage` or the service worker cache. The `GET /api/v1/calendar-feeds/events/{eventId}/guests` request carries `Cache-Control: no-store`.

### D12. Privacy

Guest names and email addresses are **personal data of people who are not users of this server**. Rules:

- **Collected:** only what the calendar already shows the user, and only for events of the user's own connections. Never from ICS feeds.
- **Stored:** the server cache table of D2 (the full list, capped) and nothing else. The devices' cache holds only `guests: {count, you, organizer, canRespond}` and `conference`; the attendee list is requested when a popover opens and kept in memory. Cached rows leave with the event, the subscription or the connection (`ON DELETE CASCADE`, delete-first in `storeRefs`).
- **Not logged:** no `slog` call, error message, audit detail or `Error()` text includes a name, an address or a conference URL; validation errors carry a field path and an index. A test captures the log of the reply, guests and create paths and fails on any `@` from the fixture addresses (D18). The audit catalogue gains `event_response` and `event_notified` and nothing else.
- **Not exported:** `internal/httpapi/export.go` has no calendar table, and the op log, webhooks (derived from ops) and the activity feed never see event data. A **backup** is a `VACUUM INTO` of the database (`internal/backup/archive.go:96`) and therefore contains this table, as it contains `calendar_events` titles; `docs/backups.md` and `docs/calendar-sync.md` say so in one sentence. The table is a cache: restoring it is harmless and the next cycle rewrites it.
- **Not given to the in-app assistant:** `list_calendar_events`, the write tools and `respond_to_calendar_event` are in `notInApp`; the device free-time twin reads only start, end, all-day and free (proven by a test that serialises its input and fails on `guests` or `conference`, task 5.7); briefing, digest and routines never read events. An external agent with a read token sees the guests of `list_calendar_events` as `untrusted_text`, as it sees titles today: the user's own agent, behind the existing owner switches. A guest's name or an event's link is text a stranger wrote, so it is data and never an instruction (existing rule of the assistant prompt: text inside `untrusted_text`).
- **Joins and mail:** OpenTodo sends no email to guests itself; only the provider does, and only after the confirmation.
- **Demo instance:** the routes are `demoDenied` like every calendar route.

### D13. Web UI

Interface budget (`docs/interface-guide.md`): 0 new chrome rows; the popover gains one line (guests) and, for an invitation, one reply row; the Join button is in the popover's existing action row and the Today agenda row's trailing slot; the editor gains two fields (Guests, Video call) in the existing panel; one new overlay (the notify dialog); no new scroll region; **0 new shortcuts** (the registry and `docs/keyboard.md` are unchanged; every control is reachable with Tab).

- **Popover** (`CalendarEvents.tsx` `EventDetails`, lazy `EventPopoverActions`): after the title and time, the join button (if `conference`), the guests line from the summary ("Ana Silva · 3 guests, 2 accepted", words never colour alone) which expands (a disclosure button) into the list from the cache-only route, and for `guests.canRespond` the **reply row**: `Yes`, `Maybe`, `No` as a `SegmentedControl` of three, the current answer pressed, with "Tell the organizer" (a `Switch`, default on) only when `sub.guests.silent`. The reply row works when `access.update` is false: it is the one write a read-only event allows. Offline it is disabled with the reason.
- **Editor** (`EventForm.tsx`, `EventPanel.tsx`): new `GuestsField.tsx`: the organiser line, the guest list with a role text ("Optional") and a labelled **Remove** button per row, rows of rooms without it, and an **Add guest** Combobox whose create row reads "Add “ben@example.com”" (free text validated as an email; an invalid text shows the field error `Enter an email address`); a role switch per added row. The field is read-only text (list only) unless `guests.canEdit`. **Video call**: a `Switch` "Add a Teams meeting" shown when `onlineMeeting.available` and the event has none, and the Join button when it has; once on, it cannot be turned off and says so. Save on an event with `access.notifies`, and Create with guests, open the notify dialog first.
- **Notify dialog** (`NotifyGuestsDialog.tsx`, lazy, `ConfirmDialog` pattern, store `notifyStore.ts` like `eventScopeStore.ts`): title, the one sentence, the two buttons; focus on **Cancel**; Escape cancels; announced in the live region. Drops, key presses and the editor all end in `eventActions.changeEvent`/`deleteEvent`, which ask scope then notify, in that order.
- **Join in the Today agenda** (`TodayAgenda.tsx` row): a small `Join` link-button at the row's end when the event has a `conference` and has not ended; it is an anchor, the row's popover is unchanged.
- **Locked guest events** (`EventLocked.tsx`, `eventAccessText`): `has_guests` reads provider-aware (D11), `not_organizer` reads "You're a guest. Only the organizer can change this event". `Convert to task` and `Duplicate` stay.

### D14. Agents and the CLI

- **MCP.** `list_calendar_events` rows gain `guests: {count, you, organizer, can_respond}` and `conference: {provider}`; the names, emails and the link are in `untrusted_text` (`untrusted_text.organizer`, `untrusted_text.guests` only when the call passes `include_guests: true`, at most 50 per event, `untrusted_text.conference_url`). `update_calendar_event` gains `guests` (`{add: [{email, name?, optional?}], remove: [email]}`), `notify_guests` (boolean) and `online_meeting`; `create_calendar_event` gains `guests` (array of `{email, name?, optional?}`), `notify_guests` and `online_meeting`; new write tool `respond_to_calendar_event` with `connection_id`, `id`, `rev`, `response` (`accepted|tentative|declined`), `scope` (`this|all`, required for an occurrence), `send_response`. The `notify_guests` description says: *"notify_guests must be true when the event's access.notifies is true: the calendar then sends the guests an update itself and it cannot be turned off. Never set it without the user having asked you to change the event they organise."* The descriptions replace "No invitation or update is ever sent to guests" (`write_calendar_events.go:37`) by the real rule. Same gate, limits, `agent.write` audit, 600 an hour limiter and `calendar_event.changed`. The new tool is added to `notInApp` (`internal/assistant/loop.go`), is not a routine step, and moves the tool counts in `mcp_test.go`, the registry goldens and `docs/mcp.md` by one. `scope_required`, `invalid_input` and `event_not_editable` come through as coded `ToolError`s as in phase 3.
- **CLI** (`internal/cli/cmd_calendar_events.go`). `ot calendar event respond <id> --yes|--maybe|--no [--scope this|all] [--no-tell]` (`--no-tell` is sent as `sendResponse: false` and refused locally with exit 2 when the event's subscription says `silent` is false); `ot calendar event add|edit --guest ana@example.com[:optional]` (repeatable), `--no-guest ana@example.com` (edit, repeatable), `--notify-guests`, `--online-meeting`. Without `--notify-guests` on an event whose `access.notifies` is true the command exits 2 and says the calendar will tell the guests. `ot calendar events` gains a GUESTS column (`3`, `3 · maybe`) and `--json` carries `guests` and `conference`. Exit codes as phase 2: `cannot_respond`, `has_guests`, `not_organizer` exit 1.

### D15. E2EE and operations

Calendar events, guests and links are server data held in the clear by design (phase 1 D9): `internal/e2ee/contentfields.go` is unchanged and an encrypted account's tools work as before; the guests of an event are not an encrypted-account concern and not protected by it, and the docs say so next to the privacy paragraph. Rate limits: one unit per request, the 600 an hour of phase 1; replies share it. Credentials, hosts and `Worker.lock` as phases 1 to 3. No new outbound destination: Graph and the CalDAV server are already the connection's targets, and the join link is opened by the person's browser, not the server.

### D16. Size and cost

No dependency. **Server:** about 900 lines of Go outside tests (engine and conference 380, writers 300, store, routes and cache 220), a few KB of binary, nothing resident. **Web (estimate, to confirm with `make bundle-budget`):** the guest list, reply row and guests field with their words are about 4.5 KB gzip in the popover and editor lazy chunks; the notify dialog about 0.7 KB in its own chunk; `eventGuests.ts`, `eventConference.ts`, the Join anchor and the new fields of `calendarFeeds.ts` and `calendarEventEdits.ts`, drawn where the grid and the Today agenda are, about 0.6 KB gzip in the entry chunk, whose headroom is 0.1 KB (`entryGzipKB` 143.6, `initialGzipKB` 698.7, `web/bundle-budget.json`): the change **raises both by the measured delta on purpose** with a dated note, as phases 1 to 3 did. Go files and functions stay inside `internal/codesize`.

### D17. Accessibility and translation

The reply row is a labelled group of three toggle buttons (`aria-pressed`, visible text, not colour); the guest list is a list whose rows read "Ben Costa, optional, accepted" and a response icon is paired with its word; Remove buttons are named "Remove Ben Costa"; the notify dialog is a dialog from the overlay module with focus on Cancel and a labelled title; the Join link's name is "Join Google Meet" with the event title in `aria-describedby`, and an external-link icon with a visually hidden "opens in a new tab". `scripts/ux-audit/screens.json` gains an overlay entry `event-notify-guests` (`on: "day"`, handler in `layout-screens.mjs` that opens the popover of a seeded Microsoft meeting, changes the title and waits for `[data-notify-guests]`), and the seeds of `event-panel` and `event-editor` gain a seeded event with guests and a conference so the popover, the guests list and the Join button are in the three-width audit. `docs/accessibility.md` records the pattern. WCAG 2.5.7: nothing here is a drag. **Translation:** about 60 new strings through `t()` into `web/src/locales/messages.json` and all six catalogs (`node scripts/i18n/extract.mjs --write`; `make i18n-check`), drafted for review by a speaker of each language and never machine-made into a catalog unreviewed (`docs/translating.md`); counts and names use placeholders ("{n} guests", "{name}"), never glued pieces.

### D18. Test plan

Go (`go test ./...`):

- **Golden iCalendar fixtures**, `internal/calfeed/testdata/guests/<case>/{in.ics,change.json,out.ics}` with `go test ./internal/calfeed -run Guests -update` and a fixed clock: `respond-accept-single` (the D6 example, byte equal), `respond-tentative`, `respond-decline`, `respond-keeps-sequence-and-dtstamp`, `respond-params-preserved` (quoted `CN` with a comma, `RSVP=TRUE`, `X-NUM-GUESTS`, a folded line, upper-case `MAILTO:`), `respond-case-insensitive-address`, `respond-all-carries-to-overrides`, `respond-all-keeps-own-override`, `respond-this-refused`, `respond-organizer-refused`, `respond-not-invited-refused`, `respond-owner-unknown-refused`, `respond-series-unsupported-refused`, `lock-organizer-google`, `lock-guest-known-owner`, `lock-guest-unknown-owner`, `lock-guest-in-exception`, `read-guests-roles-and-rooms`, `read-guests-over-cap`, `read-organizer-absent`, `conference-x-google`, `conference-url-property`, `conference-description-meet`, `conference-teams-in-location`, `conference-http-refused`, `conference-userinfo-refused`, `conference-lookalike-host-refused`, `conference-javascript-refused`, `conference-too-long-refused`.
- **Property and fuzz tests**: `ApplyRespond` changes exactly one parameter value per touched line (diff the encoded object), is idempotent, and never alters a line that does not hold the owner's address; `ParseConference` never returns a non-`https` URL for any input (fuzz); `NormalizeEmail` accepts only `local@domain` forms and never returns a value with `\r`, `\n`, `,`, `<` or a space.
- **Fakes that simulate scheduling.** `internal/calsync/calsynctest`: a `Scheduling` mode per server, `SchedulingNone` (Radicale: nothing happens), `SchedulingAuto` (a sabre-like server: a `PUT` that changes only an attendee's own `PARTSTAT` records an outgoing reply message to the organiser; an organiser `PUT` that changes attendees or any shared property records an update message to every attendee), `SchedulingGoogle` (as auto, plus a switch `KeepsPartstat` that, when off, rewrites the attendee's `PARTSTAT` to `NEEDS-ACTION` on write, and a switch `RejectsAttendeeEdits` that answers 403 with the `allowed-attendee-scheduling-object-change` precondition to an attendee change other than `PARTSTAT`). `Messages()` lists `{kind: reply|update|cancel, to}` and `AssertNoMessages(t)` fails a test that wrote when it expected silence. `internal/calsync/graphtest`: `attendees`, `organizer`, `isOrganizer`, `responseStatus`, `onlineMeeting`, calendars with `allowedOnlineMeetingProviders`, `POST /accept|tentativelyAccept|decline` honouring `sendResponse`, a `PATCH` of attendees recording an update only to the changed ones, a `PATCH` of a master with exceptions recording one update per exception (M2), a `DELETE` of an organiser's meeting recording a cancellation, and `Messages()` as above.
- `event_dav_respond_test.go`: against `SchedulingGoogle` a reply writes once with `If-Match` and no other header, reads back, answers `ErrResponseNotKept` when `KeepsPartstat` is off and caches the real answer, maps the 403 to `ErrCalendarRO`, refuses an organiser, a non-invited user, an unknown owner, `this`, and does nothing when `googleRespond` is false; against `SchedulingNone` and generic CalDAV it is `cannot_respond` before any request. **No organiser write is ever made for CalDAV:** a test over every route with guest events asserts `Writes()` is empty and `Messages()` empty.
- `event_graph_guests_test.go` and `event_graph_respond_test.go`: every reply with `sendResponse` true and false; the series cases (occurrence `this`, master `all`); an organiser edit without `notifyGuests` writes nothing and sends no message; with it exactly one update; guests added and removed with the collection rebuilt (existing guests and rooms kept); `all` refused when the master has exceptions; `following` refused with nothing written; delete sends one cancellation; `onlineMeeting` offered only when the calendar lists the provider and mapped to `invalid_input` when Graph refuses; create with guests and Teams.
- `internal/store/calendar_event_guests_migration_test.go` (runs twice, older tables pass, the `etag` reset, cascade on subscription delete); `internal/calfeed` store tests for guest rows written, replaced and dropped with refs; `GuestsOf` is scoped to the user.
- `internal/httpapi`: every row of D3's table; the notify rule (missing, false, true); `guests` body validation with the field path and no value in the message; the reply route (scopes, authentication, tokens, demo, limiter); the cache-only route (not the caller's, no guests, `Cache-Control: no-store`); the served `guests`, `conference` and `access.notifies`; `calendar_event.changed` with `event_response` and `event_notified`; **a log-capture test** that fails on any fixture address or URL in the captured `slog` output and in every error message; `v1_surface.json` regenerated (additive plus the two routes).
- `internal/agenttools` and `internal/cli`: the new tool, the changed schemas and descriptions (the wording check of `TestRulesResource` applies), `notInApp` and routines exclusion tests, `include_guests`, guests only inside `untrusted_text`, `mcp_test.go` counts, CLI flags, exit codes, completion, the local `--no-tell` refusal.
- `internal/assistant`: a test that every calendar event tool is in `notInApp` and that no tool result the in-app assistant can produce carries `guests` or `conference` (task 6.4).

Web (`npm test`, `npm run build` in `web/`): `eventGuests.test.ts`, `eventConference.test.ts` (only `https` links, the host in the label), `eventNotify.test.ts` (no prompt when nobody is told; the dialog words per kind), `calendarEventEdits.test.ts` (a notified change is never in the outbox, offline it is refused with the reason, a reply writes `guests.you` and `rev`, `response_not_kept` restores and shows the real answer, no Undo after a notified change), `EventGuests.test.tsx` (the summary, the lazy list, never persisted: Dexie holds no address after the popover closes), `EventRespond.test.tsx` (three buttons, current answer pressed, "Tell the organizer" only for `silent`, works on a read-only event, disabled offline), `GuestsField.test.tsx` (add by create row, invalid text, role, remove, read-only when `canEdit` is false), `NotifyGuestsDialog.test.tsx` and `CalendarEvents.test.tsx` (drag then scope then notify, Cancel restores with no request, Escape), `EventForm.test.tsx` (Teams switch only when available, cannot be turned off, Save asks), `TodayAgenda` (Join on an event that has not ended, none after), `JoinButton.test.tsx` (a `javascript:` URL in a cached row renders no link), `dragAlternatives.test.ts` unchanged, stories for the dialog, the reply row, the guests field and the Join button. Then `make ux-audit`, `make i18n-check`, `make bundle-budget`, `make check`.

## Risks / Open questions

G-series are Google, M-series Microsoft, as in the research section.

1. **G2 Organiser edits on Google CalDAV.** Unknown whether Google emails guests on a `PUT`. They are locked; the smoke of 7.5 only observes (a second account's inbox), it never enables them in this change.
2. **G3 Google replies.** Shipped off. 7.5 sends an invitation from a second Google account to the owner's, replies through OpenTodo and records: was the `PARTSTAT` kept, did the organiser's calendar and inbox change, did Google rewrite other properties of the object. Only then is `googleRespond` set to true, in the commit that records the result here.
3. **G1/G4/G6 Own address and conference on Google.** If the principal URL does not contain the account address, there is no "you" and no reply; the fix is `calendar-user-address-set` in discovery (RFC 6638 section 2.4.1) stored with the connection, a later change. If `X-GOOGLE-CONFERENCE` is absent, the URL scan of `DESCRIPTION` carries Meet links; a Meet link only in a structure Google keeps off CalDAV would not show, and the popover then offers nothing rather than a wrong link.
4. **M7 Reply to a series master.** Believed to answer the whole series. If Outlook answers only the next occurrence, `all` is hidden for Microsoft (a constant) and `this` stays.
5. **M8 Which organiser writes stay silent.** Treated as none.
6. **M6 Personal Outlook.com Teams.** Not offered unless listed; the error is mapped.
7. **Reply is not atomic with the cache.** Between the pre-read and the reply a change can happen; the ETag compare is the guard, and Graph's action has no `If-Match` (as phase 3 open question 4).
8. **Guest lists in backups.** A backup holds third parties' addresses (D12). Accepted because titles are already there and the data is a rebuildable cache; the documentation says so.
9. **A lookalike host.** The allow-list checks the host, not the meeting: a stranger's invitation can carry a real Zoom link to a hostile meeting. The button shows the service and host; native calendars have the same exposure.
10. **Phase 3 is unverified on Google.** Its smoke (3's task 7.5) is still open; this change builds on the series writes it describes and does not touch them.

## As built (groups 1–3)

Deviations and decisions of the Go chain (engine, writers, routes). Where this differs from D2 to D9, this section wins.

- **Names.** `calsync.Answer` is D5's `Response` type (`Response` is the DAV answer type of `dav.go`); the constants are `ResponseAccepted`, `ResponseTentative` and `ResponseDeclined` as in D5. `googleRespond` is a package variable, not a constant, so a test can turn it on; nothing else sets it, and it ships `false`.
- **Guest cache keyed by uid.** `EventRemote.Guests` and `Conference` are per cached uid (the refs map is keyed by uid), so the occurrences of a CalDAV series all show the master's guests and conference; an exception that carries other guests is not shown separately. A series whose guests are only in an exception stays locked `has_guests` for the whole object (D9), so no write can reach that difference. `ReadGuests`/`ConferenceOf` still take an `instance` for the writers' read-back and for later use.
- **Rows.** A cache row exists for an occurrence that has guests, an organiser or a conference. A Graph event the user organises with nobody invited has none, and says nothing about guests (so a first guest can be added); its `canEdit` comes from the provider, the event's access and "no guests yet".
- **Locks.** `calfeed.SeriesLocked` takes the owner (`not_organizer` before `has_guests` only when the master names an organiser other than the owner and lists the owner as an attendee); `calfeed.LockNotOrganizer` is new and `calsync.LockNotOrganizer` aliases it. Graph: an event the user organises with attendees is now **unlocked** (`access.notifies` true), an invitation is `not_organizer`, `has_guests` stays for an event with attendees and no `isOrganizer`. The two phase 1 tests that asserted the old Graph lock for an organiser's attendee event (`TestGraphLocks`, `TestGraphLockedEventsAreRefused`) changed accordingly; nothing else of phases 1 to 3 changed, and the calsync package tests of phases 1 to 3 pass.
- **Notify rule, in two places.** The Worker refuses before any remote call from the cache (`access.notifies`, or guests added): `invalid_input` field `notifyGuests` for a change, a delete (new `DeleteEvent(..., scope, notify bool)`, `?notifyGuests=true` on the route) and a create with guests. The Graph writer re-checks from the event it reads (so a cache that is behind cannot send an update), and refuses an organiser's `PATCH` without `NotifyGuests` itself. `notifyGuests: false` is the same as missing: Graph cannot skip the message. A DELETE has no pre-read: it is guarded by the ETag, and the cache refuses it first. `EventResult.Notified` says the write told people (an update, a cancellation, an invitation, or a reply with `sendResponse`) and feeds the audit.
- **Guests body.** `guests.add`/`remove` at most 50 entries; the error names `guests.add[i].email`, `.name`, `.role`, `guests.remove[i]`, `guests[i].email` (create), never a value. An address added and removed in one body, or twice, is refused; `onlineMeeting: false` is `400` (an online meeting cannot be removed). The user's own address and the organiser's are refused by the Graph writer (`guests.add`/`guests.remove`), which alone knows them; rooms are kept when a removal names them; more than 100 guests after the change, or a guest list the cache truncated, is `invalid_input` field `guests`.
- **Reply.** Checks in the order of D3 plus the cache-only ones: edits on, calendar takes writes, lock `series_unsupported`/`no_etag`, `canRespond` (provider replies, the user is a guest, address known), scope (`this` only for Microsoft, `all` for both), `sendResponse: false` only where `silent`, `rev`. An answer the calendar already holds is not written (CalDAV) and is verified instead. The Graph writer compares the ETag on a read just before the action (the action has no precondition) and reads the occurrence back; an `all` answer sets `refetch`. A reply never carries `Schedule-Reply`, `SCHEDULE-AGENT` or a `SEQUENCE`/`DTSTAMP` bump; the fake `calsynctest.SchedulingGoogle` proves the write is one `PUT` with `If-Match`, the object differs only in the user's `PARTSTAT`, and one reply message reaches the organiser.
- **Graph reads.** `windowSelect`/`eventSelect` add `organizer,responseStatus,isOnlineMeeting,onlineMeeting`; the master read adds `cancelledOccurrences` and `$expand=exceptionOccurrences` (a series master with guests and any exception or cancelled occurrence refuses `all` with `series_unsupported`; `following` with guests is `following_unsupported`). Calendars are listed with `allowedOnlineMeetingProviders`: `Caps.OnlineMeeting` is true when the calendar is writable and lists `teamsForBusiness`; a connection discovered before this change gets it on its next discovery. None of these Graph members could be verified against the real service in this environment (M6 to M8 stay open).
- **HTTP.** `calsync.Decorate(conn, calendarURL, &row)` replaces `AccessOf` at the three serving sites (events route, results, agent rows keep `AccessOf`); it fills `access` (with `notifies`) and `guests.canRespond`. Subscriptions carry `guests: {respond, silent, edit}` and `caps.onlineMeeting`. The detail route always carries `guests` (`canRespond`, `canEdit`), `onlineMeeting` and, when there is one, `conference`, with `Cache-Control: no-store`. The agent editor only compiles against `DeleteEvent`'s new argument and passes `false`, so an agent can not delete a meeting that tells guests until group 6 adds `notify_guests`. No new error code and no new `server-error-codes.json` entry: the new reasons are `invalid_input` fields and the existing `event_not_editable` and `calendar_unreachable` reasons.
- **Audit.** `calendar_event.changed` gains `event_action` `responded` and the keys `event_response` and `event_notified` through `audit.CalendarEventResponded` and `audit.CalendarEventChangedNotified` (the old constructor is unchanged for the agent routes); only fixed keys and short values, no name, address or link.
- **Tests.** Goldens in `internal/calfeed/testdata/guests/<case>/` (`in.ics` and `meta.json` are written by `-update` from `guests_cases_test.go`, answers `out.ics`, `read.json`, `lock.txt`, `conference.json`, `result.json`); no golden depends on a VTIMEZONE. The log-capture tests are `TestGuestPathsLogNothingPersonal` (calsync, through a reply, guest changes, refused writes, a read-only and an unreachable calendar) and `TestGuestPathsLogAndAuditNoPersonalData` (httpapi, also the audit rows and every error answer): they fail on any fixture address or link in a log line, and assert the capture is attached. The `502 response_not_kept` mapping is unit-tested in httpapi (`TestEventFailureMapsResponseNotKept`); the Google flow that produces it is `TestGoogleReplyNotKept` in calsync, because a Google connection needs that package's token fake.
- **Not done here.** Groups 4 to 7; the `docs/api.md` rows are the minimum the doc-coverage test needs (the guests and reply routes, the new members, the audit keys): the full text is task 7.3. Smoke tests (7.5, 7.6) and the CI baselines (7.7) are open.

## As built (groups 4–5)

Web half. Where this differs from D3, D8, D11, D13 and D17, this section wins.

- **Types and API.** D11 verbatim, plus `GuestResponse`, `EventGuest`, `GuestInput`, `GuestCaps`, `CalendarEventReply`, `CalendarEventGuests`, `CalendarConference`, and `onlineMeeting` on `CalendarCaps`. `request` takes a `cache` option; the guest list route sends `no-store`. `deleteCalendarEvent` takes `notifyGuests` as a fifth argument. Dexie stays at 31, `mobile/app/src/storage/surface.json` is untouched.
- **Never queued.** `sendNow` and `startNotifiedDelete` live in `lib/eventSendNow.ts`, not in `calendarEventEdits.ts` (a lazy chunk, 1.5 KB gzip, loaded when a notified change, an answer or a create is made). `calendarEventEdits.editEvent` refuses an event whose `access.notifies` is true (notice `notify_required`), and `createEvent` throws for a draft with guests, so no path can queue a message. A request carries `notifyGuests: true` only when the person confirmed (`confirmed: true`); an answer never does. A notified delete is held in memory for the 8 seconds (not in the outbox): a reload in the pause sends nothing; offline at send time the event is restored with the reason.
- **Prompt.** `notifyPrompt(ev, kind, invited)` also covers guests added in the same change and the first guests of an event that has none (an invitation, "Invite N guests?", "Invite and save"). `askFirst` (scope then notify) is what the popover editor, the full editor and the keys call; Cancel keeps the editor open.
- **Reply.** `EventRespond` is a `SegmentedControl` (a radio group, `aria-checked`) not toggle buttons; the scope menu for an occurrence is `ScopeMenu` rendered by the row itself with a request of its own (`only: ['this','all']`), so it works in Today's popover where no `ScopeMenuHost` is mounted. `ScopeMenu` now answers through `req.resolve`.
- **Provider.** The server serves no provider name, so `providerOf` reads Microsoft from `maxReminders === 1` and Google from the calendar address's host. Texts that name Google (`has_guests`, `response_not_kept`) use it; any other calendar gets the generic text, and the `has_guests` default stays "This event has guests".
- **Guests field.** Shown where the calendar lets guests change (`sub.guests.edit`, or the detail's `canEdit`), read-only for an event that has guests otherwise, never for a pending (queued) create. The Add guest Combobox's create row reads "Create “…”" (the primitive's own text), not "Add “…”".
- **Join.** `JoinButton` is static (Today's agenda draws it); the host is in a `Tooltip` (the token lint refuses a native `title`). Only timed rows of Today's agenda get it. Popover guests, reply row and the Join button load with `EventPopoverActions` (2.8 KB gzip with them).
- **Assistant.** `freeEventsOf` in `lib/freeTime.ts` passes only start, end, all-day and free to the free time code in both twins (`readFreeTime.ts`, `assistantBlockData.ts`); `freeTimeGuests.test.ts` proves it.
- **Bundle.** Entry 143.51 to 143.5 KB, first load 698.58 to 701.7 KB (+3.1 KB, over the 0.6 KB of D16: the English message table and the shared words); `initialGzipKB` raised to 701.8 with a dated note.
- **Not done here.** The `event-notify-guests` entry in `scripts/ux-audit` and the seeds (task 7.1) and the story baselines (7.7).

## As built (group 6)

Agent tools and command line. Where this differs from D14, this section wins.

- **Argument names.** The event is `event_id` (not `id`) on `respond_to_calendar_event`, as on `update_calendar_event` and `delete_calendar_event`. `notify_guests`, `online_meeting` and `send_response` are the tool spellings of `notifyGuests`, `onlineMeeting` and `sendResponse`; the editor maps the route's field names back, so an error names `notify_guests` and never the route's spelling.
- **EventEditor.** Adds `ValidateRespond`, `Respond` and `GuestLists` (cache only, the same `Store.GuestsOf` as the guests route). `EventUpdate`, `EventCreate` carry `Guests`, `NotifyGuests *bool`, `OnlineMeeting *bool` as sent (nil is "not sent": the server decides what `false` means, so `notify_guests: false` and `online_meeting: false` are refused as the routes refuse them). `EventDelete` carries `NotifyGuests`, now passed to `Worker.DeleteEvent` instead of the fixed `false`.
- **Rows.** `list_calendar_events` rows gain `guests: {count, you, organizer, can_respond}`, `conference: {provider}` and `access.notifies`; the agent editor now uses `calsync.Decorate`, so the access is the one the events route serves. `include_guests: true` adds `untrusted_text.organizer`, `untrusted_text.guests` (at most 50) and `untrusted_text.conference_url`; without it no name, address or link is in the answer at all (D14 left the organiser and link ungated; the spec scenario gates all three). An event's id used by `GuestLists` is the row id, so no extra query reaches the calendar.
- **Errors.** `notify_guests` missing or false is `invalid_input` with a tool message ("the calendar sends the message itself and it cannot be turned off ... send notify_guests true only if they want the guests told"); the other guest errors keep the routes' reasons through `invalidReason`. `cannot_respond`, `has_guests` and `not_organizer` come through as `event_not_editable` with the reason in the message, as phase 3.
- **Audit.** The agent paths now record `calendar_event.changed` through `CalendarEventChangedNotified` (`event_notified`), and a reply through `CalendarEventResponded` (action `responded`, `event_response`), exactly as the routes.
- **Descriptions.** `notify_guests` carries D14's sentence verbatim (plus one sentence that inviting needs it too); "No invitation or update is ever sent to guests" and "no invitation is ever sent" are gone from the update and create tools, and the three tool texts avoid the words the definition test forbids.
- **Not in the app or routines.** `respond_to_calendar_event` is in `notInApp` and in `notAStep` (reason: an answer is the person's own and tells the organiser). The registry tool count is 58 (`mcp_test.go`, `mcp_write_test.go`); the goldens (`tools_*.golden.json`, the mcp `tools_list`, `schema.golden.json`) and `v1_surface.json` were regenerated. Task 6.4's test is `internal/assistant/calendar_event_tools_test.go`: every calendar event tool is in `notInApp`, a call is an `unknown_tool`, `freetime.Event` (the only calendar data the app reads) has no guest member.
- **CLI.** `--no-guest` is the flag of the spec and D14, `--remove-guest` is an alias of it (the task brief used that name). `ot calendar event respond <id> --yes|--maybe|--no [--scope this|all] [--no-tell] [--on]`: `--no-tell` is refused locally (exit 2) when the event's subscription has `guests.silent` false; `following` is refused locally; the command does not check `canRespond` itself (the server's `cannot_respond`, exit 1, is the single source). `--notify-guests` is checked before any write on `edit`, `delete` and `add` (exit 2, `access.notifies` or any `--guest`), and sent as `notifyGuests: true` only when given; `delete` sends `&notifyGuests=true`. GUESTS column: `3`, `3 · yes`, `3 · maybe`, `3 · no`, `3 · no answer`; `--json` carries the row as the route served it (`guests`, `conference`, `access.notifies`); `schema/calendar_event.json` describes them. The error table's `event_not_editable` meaning names the three new reasons.
- **Tests.** `agenttools/calendar_event_guests_test.go` (gate for the new tool, limits, arguments, delete flag, definitions, guests only in `untrusted_text`), `httpapi/agent_calendar_guests_test.go` (a Microsoft account: summaries, the notify rule in the tools' words, a notified update and delete with audit, invites, a reply with audit, a stale rev), `cli/calendar_event_guests_test.go` (bodies, the local refusals, exit codes, a read token).
- **Not done here.** `docs/mcp.md` and `docs/cli.md` text (task 7.3).
