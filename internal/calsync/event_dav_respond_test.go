package calsync

import (
	"errors"
	"strings"
	"testing"

	"github.com/OpenTodoApp/opentodo/internal/calfeed"
	"github.com/OpenTodoApp/opentodo/internal/calsync/calsynctest"
	"github.com/OpenTodoApp/opentodo/internal/config"
)

// Answering an invitation on CalDAV and Google (OpenSpec change
// calendar-event-guests, tasks 2.2 and 2.4, design D6 and D18).

const youAddr = "you@example.com"

// invitationICS is an invitation as Google serves it: the organiser is Ana, the
// user (you@example.com) and Ben are invited.
func invitationICS(extra ...string) string {
	lines := []string{"BEGIN:VCALENDAR", "VERSION:2.0", "PRODID:-//Fake//EN", "BEGIN:VEVENT", "UID:inv-1@google.com",
		"DTSTAMP:20261010T090000Z", "DTSTART:20261021T100000Z", "DTEND:20261021T110000Z", "SUMMARY:Planning",
		"ORGANIZER;CN=Ana:mailto:ana@example.com",
		"ATTENDEE;PARTSTAT=ACCEPTED;CN=Ana:mailto:ana@example.com",
		"ATTENDEE;PARTSTAT=NEEDS-ACTION;RSVP=TRUE;CN=You:mailto:you@example.com",
		"ATTENDEE;PARTSTAT=TENTATIVE;CN=Ben:mailto:ben@example.com", "SEQUENCE:2"}
	return strings.Join(append(append(lines, extra...), "END:VEVENT", "END:VCALENDAR", ""), "\r\n")
}

// googleEnv is a Google connection to the fake CalDAV server, whose address the
// principal names, with the simulated scheduling of Google.
type googleEnv struct {
	*harness
	c *Connection
}

func newGoogleEnv(t *testing.T, respond bool) *googleEnv {
	t.Helper()
	googleRespond = respond
	t.Cleanup(func() { googleRespond = false })
	h := newHarness(t)
	h.srv.Prefix = "/caldav/v2/" + youAddr
	h.srv.Scheduling, h.srv.Owner, h.srv.KeepsPartstat = calsynctest.SchedulingGoogle, youAddr, true
	tokens := newFakeTokens(t)
	h.w.Google.Env = config.GoogleConfig{ClientID: "cid", ClientSecret: "csecret"}
	h.w.Token.URL = tokens.URL
	h.w.GoogleBase = h.srv.URL + h.srv.Prefix + "/"
	h.srv.Bearer = "access-1"
	client, _ := h.w.Google.Client(h.ctx)
	tok, err := h.w.Token.Exchange(h.ctx, client, "the-code", "verifier-1234567890", "https://todo.example/cb")
	if err != nil {
		t.Fatal(err)
	}
	d, err := h.w.DiscoverGoogle(h.ctx, tok.Access)
	if err != nil {
		t.Fatal(err)
	}
	c, err := h.w.Store.Create(h.ctx, &Connection{UserID: "alice", Provider: ProviderGoogle, Name: "Google", ServerURL: GoogleCalDAV,
		PrincipalURL: d.Principal, HomeURL: d.Home, Calendars: d.Calendars})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.w.Secrets.Set(c.ID, tok.Refresh); err != nil {
		t.Fatal(err)
	}
	target, on := d.Calendars[0].URL, true
	if _, err := h.w.Store.Update(h.ctx, "alice", c.ID, Patch{TargetURL: &target, EditEvents: &on}); err != nil {
		t.Fatal(err)
	}
	return &googleEnv{harness: h, c: c}
}

func (e *googleEnv) row(summary string) calfeed.EventRow {
	e.t.Helper()
	rows, err := e.w.Feeds.EventsOf(e.ctx, "alice", "2026-10-19", "2026-10-30", false, true)
	if err != nil {
		e.t.Fatal(err)
	}
	for _, r := range rows {
		if r.Summary == summary {
			Decorate(e.conn(e.c.ID), e.srv.CalendarURL("work"), &r)
			return r
		}
	}
	e.t.Fatalf("no event %q", summary)
	return calfeed.EventRow{}
}

func (e *googleEnv) reply(row calfeed.EventRow, a Answer, scope calfeed.Scope) (*EventResult, error) {
	return e.w.RespondEvent(e.ctx, "alice", e.c.ID, row.ID, row.Rev, ReplyEdit{Response: a, Scope: scope, SendResponse: true})
}

// TestGoogleReplyIsOnePartstatWrite: the answer is one PUT with If-Match, read back,
// the object differs only in the user's PARTSTAT, the organiser gets one reply, and
// the cache serves the new answer.
func TestGoogleReplyIsOnePartstatWrite(t *testing.T) {
	e := newGoogleEnv(t, true)
	e.srv.Put("work", "inv.ics", invitationICS())
	e.cycle(e.conn(e.c.ID))
	row := e.row("Planning")
	if row.Guests == nil || row.Guests.You != calfeed.ResponseNeedsAction || row.Guests.Organizer || row.Guests.Count != 2 || !row.Guests.CanRespond {
		t.Fatalf("summary = %+v", row.Guests)
	}
	if row.Access == nil {
		Decorate(e.conn(e.c.ID), e.srv.CalendarURL("work"), &row)
	}
	if row.Locked != LockNotOrganizer {
		t.Fatalf("lock = %q, want not_organizer: the address is known", row.Locked)
	}
	before := e.srv.Get("work", "inv.ics")
	res, err := e.reply(row, ResponseAccepted, "")
	if err != nil {
		t.Fatal(err)
	}
	writes := e.srv.Writes()
	if len(writes) != 1 || !strings.HasSuffix(writes[0], " If-Match") || !strings.HasPrefix(writes[0], "PUT ") {
		t.Fatalf("writes = %q", writes)
	}
	if m := e.srv.Messages(); len(m) != 1 || m[0] != (calsynctest.Message{Kind: "reply", To: "ana@example.com"}) {
		t.Fatalf("messages = %+v", m)
	}
	after := e.srv.Get("work", "inv.ics")
	if strings.ReplaceAll(before, "PARTSTAT=NEEDS-ACTION;RSVP=TRUE;CN=You", "PARTSTAT=ACCEPTED;RSVP=TRUE;CN=You") != after {
		t.Fatalf("the object changed beyond the user's PARTSTAT:\n%s\n--\n%s", before, after)
	}
	if res.Event.Guests == nil || res.Event.Guests.You != calfeed.ResponseAccepted || res.Event.Rev == "" || res.Event.Rev == row.Rev {
		t.Fatalf("result = %+v", res.Event)
	}
	if again := e.row("Planning"); again.Guests.You != calfeed.ResponseAccepted || again.Rev != res.Event.Rev {
		t.Fatalf("cache = %+v", again)
	}
	// Answering the same again writes nothing.
	if _, err := e.reply(e.row("Planning"), ResponseAccepted, ""); err != nil {
		t.Fatal(err)
	}
	if len(e.srv.Writes()) != 1 {
		t.Fatalf("an answer already held was written again: %q", e.srv.Writes())
	}
}

// TestGoogleReplyNotKept: a server that rewrites the answer is reported, and the
// cache shows what the calendar really holds.
func TestGoogleReplyNotKept(t *testing.T) {
	e := newGoogleEnv(t, true)
	e.srv.KeepsPartstat = false
	e.srv.Put("work", "inv.ics", invitationICS())
	e.cycle(e.conn(e.c.ID))
	_, err := e.reply(e.row("Planning"), ResponseDeclined, "")
	wantCode(t, err, EventUnreachable, ReasonResponseNotKept)
	if got := e.row("Planning"); got.Guests.You != calfeed.ResponseNeedsAction {
		t.Fatalf("the cache shows %q after a write the calendar did not keep", got.Guests.You)
	}
}

// TestGoogleReplyRefusals: every refusal comes before any request.
func TestGoogleReplyRefusals(t *testing.T) {
	e := newGoogleEnv(t, true)
	e.srv.Put("work", "inv.ics", invitationICS())
	e.srv.Put("work", "mine.ics", strings.NewReplacer("UID:inv-1@google.com", "UID:mine-1@google.com", "SUMMARY:Planning", "SUMMARY:Mine",
		"ORGANIZER;CN=Ana:mailto:ana@example.com", "ORGANIZER;CN=You:mailto:you@example.com", "DTSTART:20261021", "DTSTART:20261022", "DTEND:20261021", "DTEND:20261022").Replace(invitationICS()))
	e.srv.Put("work", "other.ics", strings.NewReplacer("UID:inv-1@google.com", "UID:other-1@google.com", "SUMMARY:Planning", "SUMMARY:Other",
		"mailto:you@example.com", "mailto:someone@example.com", "DTSTART:20261021", "DTSTART:20261023", "DTEND:20261021", "DTEND:20261023").Replace(invitationICS()))
	e.cycle(e.conn(e.c.ID))
	inv, mine, other := e.row("Planning"), e.row("Mine"), e.row("Other")
	puts := e.srv.Count("GET") + e.srv.Count("PUT")
	check := func(name string, err error, code, reason string) {
		t.Helper()
		wantCode(t, err, code, reason)
		if got := e.srv.Count("GET") + e.srv.Count("PUT"); got != puts {
			t.Fatalf("%s sent a request", name)
		}
	}
	_, err := e.reply(mine, ResponseAccepted, "")
	check("organiser", err, EventNotEditable, ReasonCannotRespond)
	_, err = e.reply(other, ResponseAccepted, "")
	check("not invited", err, EventNotEditable, ReasonCannotRespond)
	_, err = e.reply(inv, ResponseAccepted, calfeed.ScopeThis)
	check("scope on a single event", err, EventInvalid, "scope")
	_, err = e.reply(inv, "maybe", "")
	check("answer", err, EventInvalid, "response")
	_, err = e.w.RespondEvent(e.ctx, "alice", e.c.ID, inv.ID, inv.Rev, ReplyEdit{Response: ResponseAccepted, SendResponse: false})
	check("silent", err, EventInvalid, "sendResponse")
	_, err = e.w.RespondEvent(e.ctx, "alice", e.c.ID, inv.ID, "stale", ReplyEdit{Response: ResponseAccepted, SendResponse: true})
	check("stale", err, EventChanged, "")
	if len(e.srv.Writes()) != 0 {
		t.Fatalf("writes = %q", e.srv.Writes())
	}
	e.srv.AssertNoMessages(t)
}

// TestGoogleReplyOffUntilSmoke: googleRespond is false by default and nothing is
// offered or written, and an unknown own address offers no reply either.
func TestGoogleReplyOffUntilSmoke(t *testing.T) {
	e := newGoogleEnv(t, false)
	e.srv.Put("work", "inv.ics", invitationICS())
	e.cycle(e.conn(e.c.ID))
	row := e.row("Planning")
	if row.Guests == nil || row.Guests.CanRespond {
		t.Fatalf("summary = %+v", row.Guests)
	}
	Decorate(e.conn(e.c.ID), e.srv.CalendarURL("work"), &row)
	if row.Guests.CanRespond {
		t.Fatal("CanRespond with googleRespond false")
	}
	_, err := e.reply(row, ResponseAccepted, "")
	wantCode(t, err, EventNotEditable, ReasonCannotRespond)
	if e.srv.Count("PUT") != 0 || len(e.srv.Writes()) != 0 {
		t.Fatal("a reply was written")
	}
	// The writer itself refuses too (the last line of defence).
	if _, err := e.writer(e.c).Reply(e.ctx, e.davRef(e.c, e.srv.CalendarURL("work"), "inv-1@google.com"), ReplyEdit{Response: ResponseAccepted, SendResponse: true}); !errors.Is(err, ErrCannotRespond) {
		t.Fatalf("writer = %v", err)
	}
}

// TestGoogleReplyForbiddenIsReadOnly: a 403 on the write is calendar_read_only.
func TestGoogleReplyForbiddenIsReadOnly(t *testing.T) {
	e := newGoogleEnv(t, true)
	e.srv.Put("work", "inv.ics", invitationICS())
	e.cycle(e.conn(e.c.ID))
	e.srv.SetReadOnly("work", true)
	_, err := e.reply(e.row("Planning"), ResponseAccepted, "")
	wantCode(t, err, EventReadOnly, "")
	e.srv.AssertNoMessages(t)
}

// TestGoogleReplyAllOnASeries: the answer reaches the master and the overrides that
// held the old one, in one write, and series.rev follows the object.
func TestGoogleReplyAllOnASeries(t *testing.T) {
	e := newGoogleEnv(t, true)
	series := strings.NewReplacer("DTSTART:20261021T100000Z", "DTSTART:20261021T100000Z\r\nRRULE:FREQ=DAILY;COUNT=3").Replace(invitationICS())
	e.srv.Put("work", "inv.ics", series)
	e.cycle(e.conn(e.c.ID))
	row := e.row("Planning")
	if row.Series == nil {
		t.Fatalf("not a series: %+v", row)
	}
	if _, err := e.reply(row, ResponseAccepted, ""); err == nil {
		t.Fatal("an occurrence was answered without a scope")
	} else {
		wantCode(t, err, EventScopeRequired, "")
	}
	if _, err := e.reply(row, ResponseAccepted, calfeed.ScopeThis); err == nil {
		t.Fatal("an occurrence's own answer was written to CalDAV")
	} else {
		wantCode(t, err, EventInvalid, "scope")
	}
	res, err := e.reply(row, ResponseDeclined, calfeed.ScopeAll)
	if err != nil {
		t.Fatal(err)
	}
	if res.Series == nil || res.Series.Rev == "" || res.Event.Guests.You != calfeed.ResponseDeclined {
		t.Fatalf("result = %+v %+v", res.Series, res.Event)
	}
	for _, r := range func() []calfeed.EventRow {
		rows, _ := e.w.Feeds.EventsOf(e.ctx, "alice", "2026-10-19", "2026-10-30", false, true)
		return rows
	}() {
		if r.Guests == nil || r.Guests.You != calfeed.ResponseDeclined || r.Series.Rev != res.Series.Rev {
			t.Fatalf("an occurrence was left behind: %+v", r)
		}
	}
}

// TestCalDAVNeverWritesToAGuestEvent: on a generic server with scheduling, every
// route with a guest event refuses before any write and no message is sent.
func TestCalDAVNeverWritesToAGuestEvent(t *testing.T) {
	e := newEditEnv(t, true)
	e.srv.Scheduling, e.srv.Owner = calsynctest.SchedulingAuto, youAddr
	e.srv.Put("work", "inv.ics", invitationICS())
	e.later(1)
	e.cycle(e.conn(e.c.ID))
	inv := e.row("Planning")
	if inv.Locked != LockGuests {
		t.Fatalf("lock = %q, want has_guests: a generic server never says who the user is", inv.Locked)
	}
	if inv.Guests == nil || inv.Guests.CanRespond || inv.Guests.You != "" {
		t.Fatalf("summary = %+v", inv.Guests)
	}
	gone := false
	attempts := map[string]error{}
	_, attempts["edit"] = e.w.EditEvent(e.ctx, "alice", e.c.ID, inv.ID, inv.Rev, EventEdit{Summary: ptr("x"), NotifyGuests: true})
	_, attempts["delete"] = e.w.DeleteEvent(e.ctx, "alice", e.c.ID, inv.ID, inv.Rev, "", true)
	_, attempts["guests"] = e.w.EditEvent(e.ctx, "alice", e.c.ID, inv.ID, inv.Rev, EventEdit{Guests: &GuestsEdit{Add: []GuestAdd{{Email: "new@example.com"}}}, NotifyGuests: true})
	_, attempts["reply"] = e.w.RespondEvent(e.ctx, "alice", e.c.ID, inv.ID, inv.Rev, ReplyEdit{Response: ResponseAccepted, SendResponse: true})
	for name, err := range attempts {
		var ee *EventError
		if !errors.As(err, &ee) || (ee.Code != EventNotEditable && ee.Code != EventInvalid) {
			t.Errorf("%s = %v", name, err)
		}
		gone = gone || err == nil
	}
	if gone {
		t.Fatal("an attempt succeeded")
	}
	if w := e.srv.Writes(); len(w) != 0 {
		t.Fatalf("writes = %q", w)
	}
	e.srv.AssertNoMessages(t)
	// The writers refuse too, with no request.
	st, err := e.writer(e.c).Read(e.ctx, e.davRef(e.c, e.srv.CalendarURL("work"), "inv-1@google.com"))
	if err != nil || st.Locked != LockGuests {
		t.Fatalf("read = %+v %v", st, err)
	}
	ref := e.davRef(e.c, e.srv.CalendarURL("work"), "inv-1@google.com")
	if _, err := e.writer(e.c).Update(e.ctx, ref, EventEdit{Summary: ptr("x")}); !errors.Is(err, ErrEventLocked) {
		t.Fatalf("writer update = %v", err)
	}
	if _, err := e.writer(e.c).Reply(e.ctx, ref, ReplyEdit{Response: ResponseAccepted, SendResponse: true}); !errors.Is(err, ErrCannotRespond) {
		t.Fatalf("writer reply = %v", err)
	}
	if w := e.srv.Writes(); len(w) != 0 {
		t.Fatalf("writes after the writers = %q", w)
	}
}

// TestGuestCapsAndOwnerAddress: the table of design D1.
func TestGuestCapsAndOwnerAddress(t *testing.T) {
	const cal = "https://apidata.googleusercontent.com/caldav/v2/you%40example.com/events/"
	caps := map[string]GuestCaps{
		ProviderMicrosoft: {Respond: true, Silent: true, Edit: true},
		ProviderGoogle:    {},
		ProviderCalDAV:    {},
	}
	for p, want := range caps {
		if got := (&Connection{Provider: p}).GuestCaps(); got != want {
			t.Errorf("%s caps = %+v, want %+v", p, got, want)
		}
	}
	googleRespond = true
	if got := (&Connection{Provider: ProviderGoogle}).GuestCaps(); got != (GuestCaps{Respond: true}) {
		t.Errorf("google with the switch on = %+v", got)
	}
	googleRespond = false
	cases := []struct {
		name string
		c    Connection
		cal  string
		want string
	}{
		{"google principal", Connection{Provider: ProviderGoogle, PrincipalURL: "https://apidata.googleusercontent.com/caldav/v2/You%40Example.com/user"}, cal, youAddr},
		{"google home only", Connection{Provider: ProviderGoogle, HomeURL: "https://apidata.googleusercontent.com/caldav/v2/you@example.com/"}, cal, youAddr},
		{"shared colleague calendar", Connection{Provider: ProviderGoogle, PrincipalURL: "https://apidata.googleusercontent.com/caldav/v2/you%40example.com/user"},
			"https://apidata.googleusercontent.com/caldav/v2/colleague%40example.com/events/", ""},
		{"principal without an address", Connection{Provider: ProviderGoogle, PrincipalURL: "https://apidata.googleusercontent.com/caldav/v2/abc123/user"},
			"https://apidata.googleusercontent.com/caldav/v2/abc123/events/", ""},
		{"no principal", Connection{Provider: ProviderGoogle}, cal, ""},
		{"microsoft", Connection{Provider: ProviderMicrosoft, Username: "Me@Outlook.com"}, "cal-work", "me@outlook.com"},
		{"microsoft without a mailbox", Connection{Provider: ProviderMicrosoft, Username: "me"}, "cal-work", ""},
		{"generic caldav", Connection{Provider: ProviderCalDAV, Username: youAddr}, cal, ""},
	}
	for _, tc := range cases {
		if got := ownerAddress(&tc.c, tc.cal); got != tc.want {
			t.Errorf("%s: ownerAddress = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// TestCalDAVReadsGuestsAndConference: a generic CalDAV calendar shows the guests, the
// organiser and a join link found in the description, with the lock of a server that
// does not say who the user is.
func TestCalDAVReadsGuestsAndConference(t *testing.T) {
	e := newEditEnv(t, true)
	e.srv.Put("work", "call.ics", strings.NewReplacer("UID:inv-1@google.com", "UID:call-1@example.com", "SUMMARY:Planning", "SUMMARY:Call",
		"DTSTART:20261021", "DTSTART:20261024", "DTEND:20261021", "DTEND:20261024").Replace(invitationICS(
		`DESCRIPTION:Join: https://meet.google.com/abc-defg-hij\nBring notes`,
		"ATTENDEE;CUTYPE=RESOURCE;PARTSTAT=ACCEPTED;CN=Room 4:mailto:room-4@example.com")))
	e.later(1)
	e.cycle(e.conn(e.c.ID))
	row := e.row("Call")
	if row.Guests == nil || row.Guests.Count != 4 || row.Guests.You != "" || row.Guests.Organizer || row.Locked != LockGuests {
		t.Fatalf("summary = %+v lock %q", row.Guests, row.Locked)
	}
	if row.Conference == nil || row.Conference.Provider != "meet" || row.Conference.URL != "https://meet.google.com/abc-defg-hij" {
		t.Fatalf("conference = %+v", row.Conference)
	}
	got, ok, err := e.w.Feeds.GuestsOf(e.ctx, "alice", row.ID)
	if err != nil || !ok || got.Info.Organizer == nil || got.Info.Organizer.Email != "ana@example.com" || len(got.Info.Guests) != 4 {
		t.Fatalf("guests = %+v %v %v", got.Info, ok, err)
	}
	rooms := 0
	for _, g := range got.Info.Guests {
		if g.Role == calfeed.RoleRoom {
			rooms++
		}
	}
	if rooms != 1 {
		t.Fatalf("rooms = %d", rooms)
	}
	if row.Access != nil && row.Access.Notifies {
		t.Fatal("a CalDAV event notifies")
	}
}
