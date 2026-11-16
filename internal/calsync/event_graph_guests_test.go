package calsync

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/OpenTodoApp/opentodo/internal/calfeed"
	"github.com/OpenTodoApp/opentodo/internal/calsync/graphtest"
)

// Replies, organiser edits and guests on Microsoft Graph (OpenSpec change
// calendar-event-guests, tasks 2.5 and 2.6, design D7, D8 and D18).

type guestEnv struct {
	h  *harness
	ms *graphtest.Server
	c  *Connection
}

// newGuestEnv is a Microsoft connection with edits on; teams makes the calendar list
// the Teams provider.
func newGuestEnv(t *testing.T, teams bool, events ...graphtest.Event) *guestEnv {
	t.Helper()
	h := newHarness(t)
	ms, c := h.microsoftWith(weHere, func(ms *graphtest.Server) {
		if teams {
			ms.AddCalendar(&graphtest.Calendar{ID: "cal-work", Name: "Calendar", Color: "#3366cc", CanEdit: true, OnlineProviders: []string{"teamsForBusiness"}})
		}
	})
	if _, err := h.w.Store.Update(h.ctx, "alice", c.ID, Patch{EditEvents: ptr(true)}); err != nil {
		t.Fatal(err)
	}
	for _, e := range events {
		ms.Add(e)
	}
	h.cycle(h.conn(c.ID))
	return &guestEnv{h: h, ms: ms, c: h.conn(c.ID)}
}

func gEvent(subject, day string, f func(e *graphtest.Event)) graphtest.Event {
	e := graphtest.Event{Calendar: "cal-work", Subject: subject,
		Start: graphtest.DT{DateTime: day + "T10:00:00", TimeZone: "UTC"}, End: graphtest.DT{DateTime: day + "T11:00:00", TimeZone: "UTC"},
		Guests: []graphtest.Attendee{
			{Address: "ben@example.com", Name: "Ben", Type: "required", Response: "accepted"},
			{Address: "room-4@example.com", Name: "Room 4", Type: "resource", Response: "accepted"},
			{Address: "cy@example.com", Name: "Cy", Type: "optional", Response: "notResponded"}}}
	if f != nil {
		f(&e)
	}
	return e
}

func invited(e *graphtest.Event) {
	e.NotOrganizer, e.Organizer = true, graphtest.Person{Name: "Ana", Address: "ana@example.com"}
	e.Guests = append(e.Guests, graphtest.Attendee{Address: "me@outlook.com", Name: "Me", Type: "required", Response: "notResponded"})
}

func (e *guestEnv) row(subject string) calfeed.EventRow {
	e.h.t.Helper()
	for _, r := range e.h.rows(e.c) {
		if r.Summary == subject {
			conn := e.h.conn(e.c.ID)
			Decorate(conn, "cal-work", &r)
			return r
		}
	}
	e.h.t.Fatalf("no event %q", subject)
	return calfeed.EventRow{}
}

func (e *guestEnv) edit(row calfeed.EventRow, edit EventEdit) (*EventResult, error) {
	return e.h.w.EditEvent(e.h.ctx, "alice", e.c.ID, row.ID, row.Rev, edit)
}

func (e *guestEnv) respond(row calfeed.EventRow, a Answer, scope calfeed.Scope, send bool) (*EventResult, error) {
	return e.h.w.RespondEvent(e.h.ctx, "alice", e.c.ID, row.ID, row.Rev, ReplyEdit{Response: a, Scope: scope, SendResponse: send})
}

func (e *guestEnv) writes() int {
	return e.ms.Count("PATCH") + e.ms.Count("POST") + e.ms.Count("DELETE")
}

// TestGraphReadsGuestsRolesAndAnswers: an organiser's meeting, an invitation and a
// meeting with a Teams link read the expected rows; the guest list holds the
// answers, rooms read-only, and the summary no address.
func TestGraphReadsGuestsRolesAndAnswers(t *testing.T) {
	e := newGuestEnv(t, true,
		gEvent("Mine", "2026-10-21", func(g *graphtest.Event) {
			g.JoinURL, g.IsOnlineMeeting = "https://teams.microsoft.com/l/meetup-join/abc", true
		}),
		gEvent("Invited", "2026-10-22", invited),
		gEvent("Alone", "2026-10-23", func(g *graphtest.Event) { g.Guests = nil }))
	mine, inv, alone := e.row("Mine"), e.row("Invited"), e.row("Alone")
	if mine.Guests == nil || !mine.Guests.Organizer || mine.Guests.Count != 3 || mine.Guests.You != "" || mine.Locked != "" {
		t.Fatalf("mine = %+v lock %q", mine.Guests, mine.Locked)
	}
	if mine.Conference == nil || mine.Conference.Provider != "teams" || mine.Access == nil || !mine.Access.Update || !mine.Access.Notifies {
		t.Fatalf("mine conference %+v access %+v", mine.Conference, mine.Access)
	}
	if inv.Guests == nil || inv.Guests.Organizer || inv.Guests.You != calfeed.ResponseNeedsAction || inv.Locked != LockNotOrganizer || !inv.Guests.CanRespond || inv.Access.Notifies {
		t.Fatalf("invited = %+v lock %q access %+v", inv.Guests, inv.Locked, inv.Access)
	}
	if alone.Guests != nil || alone.Access.Notifies || alone.Locked != "" {
		t.Fatalf("alone = %+v", alone)
	}
	got, ok, err := e.h.w.Feeds.GuestsOf(e.h.ctx, "alice", mine.ID)
	if err != nil || !ok || got.Info.Organizer == nil || got.Info.Organizer.Email != "me@outlook.com" || len(got.Info.Guests) != 3 {
		t.Fatalf("guest row = %+v %v %v", got, ok, err)
	}
	roles := map[string]string{}
	for _, g := range got.Info.Guests {
		roles[g.Email] = g.Role + "/" + g.Response
	}
	if roles["room-4@example.com"] != "room/accepted" || roles["cy@example.com"] != "optional/needs-action" || roles["ben@example.com"] != "required/accepted" {
		t.Fatalf("roles = %v", roles)
	}
	cals := e.h.conn(e.c.ID).Calendars
	if len(cals) == 0 || cals[0].Caps == nil || !cals[0].Caps.OnlineMeeting {
		t.Fatalf("calendar caps = %+v", cals)
	}
}

// TestGraphReplyHonoursSendResponse: an answer reaches the organiser when asked to and
// stays silent when not, an organiser is refused with nothing sent, and a stale
// version is refused before any request.
func TestGraphReplyHonoursSendResponse(t *testing.T) {
	e := newGuestEnv(t, false, gEvent("Invited", "2026-10-22", invited), gEvent("Mine", "2026-10-21", nil))
	inv := e.row("Invited")
	res, err := e.respond(inv, ResponseAccepted, "", false)
	if err != nil {
		t.Fatal(err)
	}
	e.ms.AssertNoMessages(t)
	if res.Event.Guests.You != calfeed.ResponseAccepted || res.Event.Rev == inv.Rev {
		t.Fatalf("result = %+v", res.Event)
	}
	if e.ms.Count("POST /v1.0/me/events/") != 1 || !strings.Contains(strings.Join(e.ms.Requests(), "\n"), "/accept") {
		t.Fatalf("requests = %q", e.ms.Requests())
	}
	if _, err := e.respond(e.row("Invited"), ResponseTentative, "", true); err != nil {
		t.Fatal(err)
	}
	if m := e.ms.Messages(); len(m) != 1 || m[0] != (graphtest.Message{Kind: "reply", To: "ana@example.com"}) {
		t.Fatalf("messages = %+v", m)
	}
	posts := e.ms.Count("POST")
	_, err = e.respond(e.row("Mine"), ResponseAccepted, "", true)
	wantCode(t, err, EventNotEditable, ReasonCannotRespond)
	_, err = e.h.w.RespondEvent(e.h.ctx, "alice", e.c.ID, inv.ID, inv.Rev, ReplyEdit{Response: ResponseDeclined, SendResponse: true})
	wantCode(t, err, EventChanged, "")
	if e.ms.Count("POST") != posts {
		t.Fatal("a refused reply sent a request")
	}
	// The writer compares the version too, before the action.
	cur := e.row("Invited")
	stale := EventRef{Calendar: "cal-work", Href: gHref(t, e, "Invited"), ETag: `W/"stale"`}
	if _, err := e.h.writer(e.c).Reply(e.h.ctx, stale, ReplyEdit{Response: ResponseDeclined, SendResponse: true}); !errors.Is(err, ErrEventChanged) {
		t.Fatalf("stale reply = %v", err)
	}
	if e.ms.Count("POST") != posts || cur.Guests.You != calfeed.ResponseTentative {
		t.Fatal("a stale reply was sent")
	}
}

func gHref(t *testing.T, e *guestEnv, subject string) string {
	t.Helper()
	row := e.row(subject)
	r, err := e.h.w.Feeds.RefByUID(e.h.ctx, e.h.refSub(e.c), row.UID)
	if err != nil {
		t.Fatal(err)
	}
	return r.Href
}

// TestGraphReplyToASeries: this answers the occurrence, all the series master.
func TestGraphReplyToASeries(t *testing.T) {
	e := newGuestEnv(t, false)
	master := e.ms.Add(weeklyMaster(func(m *graphtest.Event) {
		invited(m)
		m.Guests = m.Guests[len(m.Guests)-1:]
		m.Start.TimeZone, m.End.TimeZone = "UTC", "UTC"
		m.Start.DateTime, m.End.DateTime = "2026-10-26T07:00:00", "2026-10-26T08:30:00"
	}))
	e.h.cycle(e.h.conn(e.c.ID))
	var row calfeed.EventRow
	for _, r := range e.h.rows(e.c) {
		if r.Series != nil && r.Start.Date == "2026-10-28" {
			row = r
		}
	}
	Decorate(e.h.conn(e.c.ID), "cal-work", &row)
	if row.Guests == nil || !row.Guests.CanRespond {
		t.Fatalf("row = %+v", row)
	}
	if _, err := e.respond(row, ResponseAccepted, "", true); err == nil {
		t.Fatal("an occurrence was answered without a scope")
	} else {
		wantCode(t, err, EventScopeRequired, "")
	}
	res, err := e.respond(row, ResponseDeclined, calfeed.ScopeThis, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Event.Guests.You != calfeed.ResponseDeclined || res.Series == nil || res.Refetch {
		t.Fatalf("this = %+v %+v refetch %v", res.Event.Guests, res.Series, res.Refetch)
	}
	for _, r := range e.ms.Requests() {
		if strings.Contains(r, "/decline") && strings.Contains(r, master) && !strings.Contains(r, master+"_") {
			t.Fatalf("an occurrence's answer went to the master: %s", r)
		}
	}
	for _, r := range e.h.rows(e.c) {
		if r.Series != nil && r.Start.Date == "2026-10-26" {
			row = r
		}
	}
	Decorate(e.h.conn(e.c.ID), "cal-work", &row)
	res, err = e.respond(row, ResponseAccepted, calfeed.ScopeAll, false)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Refetch || res.Event.Guests.You != calfeed.ResponseAccepted {
		t.Fatalf("all = %+v refetch %v", res.Event.Guests, res.Refetch)
	}
	found := false
	for _, r := range e.ms.Requests() {
		found = found || (strings.HasPrefix(r, "POST") && strings.HasSuffix(r, "/"+master+"/accept"))
	}
	if !found {
		t.Fatalf("no answer went to the master: %q", e.ms.Requests())
	}
	e.ms.AssertNoMessages(t)
}

// TestGraphOrganiserEditNeedsNotifyGuests: nothing is written, requested or sent
// without the caller's confirmation, whatever the change; with it exactly one
// update goes out.
func TestGraphOrganiserEditNeedsNotifyGuests(t *testing.T) {
	e := newGuestEnv(t, false, gEvent("Mine", "2026-10-21", nil))
	row := e.row("Mine")
	for name, edit := range map[string]EventEdit{
		"rename":   {Summary: ptr("Renamed")},
		"busy":     {Free: ptr(true)},
		"location": {Location: ptr("Room 9")},
		"guests":   {Guests: &GuestsEdit{Add: []GuestAdd{{Email: "new@example.com"}}}},
		"remove":   {Guests: &GuestsEdit{Remove: []string{"ben@example.com"}}},
	} {
		_, err := e.edit(row, edit)
		wantCode(t, err, EventInvalid, "notifyGuests")
		if e.writes() != 0 || len(e.ms.Messages()) != 0 {
			t.Fatalf("%s wrote %d or sent %+v without a confirmation", name, e.writes(), e.ms.Messages())
		}
	}
	// Deleting needs it too.
	_, err := e.h.w.DeleteEvent(e.h.ctx, "alice", e.c.ID, row.ID, row.Rev, "", false)
	wantCode(t, err, EventInvalid, "notifyGuests")
	if e.writes() != 0 {
		t.Fatal("a delete without confirmation wrote")
	}
	// The writer refuses too, before any request.
	if _, err := e.h.writer(e.c).Update(e.h.ctx, e.h.davRef(e.c, "cal-work", row.UID), EventEdit{Summary: ptr("x")}); !errors.Is(err, ErrEventInvalid) || invalidField(err) != "notifyGuests" {
		t.Fatalf("writer = %v", err)
	}
	if e.writes() != 0 {
		t.Fatal("the writer wrote without a confirmation")
	}
	res, err := e.edit(row, EventEdit{Summary: ptr("Renamed"), NotifyGuests: true})
	if err != nil {
		t.Fatal(err)
	}
	if e.ms.Count("PATCH") != 1 || res.Event.Summary != "Renamed" {
		t.Fatalf("patches = %d %+v", e.ms.Count("PATCH"), res.Event)
	}
	m := e.ms.Messages()
	if len(m) != 3 {
		t.Fatalf("one update to each guest expected: %+v", m)
	}
	for _, x := range m {
		if x.Kind != "update" {
			t.Fatalf("message %+v", x)
		}
	}
	if e.ms.Unconditional() != 0 {
		t.Fatal("a write without If-Match")
	}
}

// TestGraphGuestsAddedAndRemoved: the collection is rebuilt with the existing guests
// and rooms kept, only changed attendees are told, a room is not removed and the
// user's own and the organiser's address are refused.
func TestGraphGuestsAddedAndRemoved(t *testing.T) {
	e := newGuestEnv(t, false, gEvent("Mine", "2026-10-21", nil))
	row := e.row("Mine")
	res, err := e.edit(row, EventEdit{NotifyGuests: true, Guests: &GuestsEdit{
		Add:    []GuestAdd{{Email: "New@Example.com", Name: "New Guest", Optional: true}, {Email: "cy@example.com"}},
		Remove: []string{"ben@example.com", "room-4@example.com"}}})
	if err != nil {
		t.Fatal(err)
	}
	var ev graphtest.Event
	for _, x := range e.ms.Events("cal-work") {
		if x.Subject == "Mine" {
			ev = x
		}
	}
	got := map[string]string{}
	for _, a := range ev.Guests {
		got[a.Address] = a.Type + "/" + a.Response
	}
	want := map[string]string{"room-4@example.com": "resource/accepted", "cy@example.com": "required/notResponded", "new@example.com": "optional/notResponded"}
	if len(got) != len(want) || got["room-4@example.com"] != want["room-4@example.com"] || got["cy@example.com"] != want["cy@example.com"] || got["new@example.com"] != want["new@example.com"] {
		t.Fatalf("attendees = %v, want %v", got, want)
	}
	if res.Event.Guests == nil || res.Event.Guests.Count != 3 {
		t.Fatalf("answer = %+v", res.Event.Guests)
	}
	msgs := map[string]string{}
	for _, m := range e.ms.Messages() {
		msgs[m.To] = m.Kind
	}
	if msgs["new@example.com"] != "update" || msgs["cy@example.com"] != "update" || msgs["ben@example.com"] != "cancel" || len(msgs) != 3 {
		t.Fatalf("messages = %+v: only the changed attendees are told", e.ms.Messages())
	}
	// Refused: the user's own address and the organiser's; the error names the field, not the value.
	row = e.row("Mine")
	_, err = e.edit(row, EventEdit{NotifyGuests: true, Guests: &GuestsEdit{Add: []GuestAdd{{Email: "me@outlook.com"}}}})
	var ee *EventError
	if !errors.As(err, &ee) || ee.Code != EventInvalid || ee.Reason != "guests.add" {
		t.Fatalf("own address = %v", err)
	}
	if err != nil && strings.Contains(err.Error(), "outlook.com") {
		t.Fatalf("the error holds the address: %v", err)
	}
}

// TestGraphSeriesWithGuests: this is allowed (one update), all only without
// exceptions, following never; nothing is written for the refusals.
func TestGraphSeriesWithGuests(t *testing.T) {
	e := newGuestEnv(t, false)
	master := e.ms.Add(weeklyMaster(func(m *graphtest.Event) {
		m.Guests = []graphtest.Attendee{{Address: "ben@example.com", Name: "Ben", Type: "required", Response: "accepted"}}
	}))
	e.h.cycle(e.h.conn(e.c.ID))
	occ := func(date string) calfeed.EventRow {
		for _, r := range e.h.rows(e.c) {
			if r.Series != nil && r.Start.Date == date {
				Decorate(e.h.conn(e.c.ID), "cal-work", &r)
				return r
			}
		}
		t.Fatalf("no occurrence on %s", date)
		return calfeed.EventRow{}
	}
	row := occ("2026-10-28")
	if row.Locked != "" || row.Access == nil || !row.Access.Notifies {
		t.Fatalf("row = %+v", row)
	}
	_, err := e.edit(row, EventEdit{Summary: ptr("Once"), Scope: calfeed.ScopeFollowing, NotifyGuests: true})
	wantCode(t, err, EventNotEditable, ReasonFollowingUnsupported)
	_, err = e.h.w.DeleteEvent(e.h.ctx, "alice", e.c.ID, row.ID, row.Rev, calfeed.ScopeFollowing, true)
	wantCode(t, err, EventNotEditable, ReasonFollowingUnsupported)
	if e.writes() != 0 || len(e.ms.Messages()) != 0 {
		t.Fatal("a refused series change wrote or sent something")
	}
	if _, err := e.edit(row, EventEdit{Summary: ptr("Once"), Scope: calfeed.ScopeThis}); err == nil {
		t.Fatal("a change of an event with guests went through without a confirmation")
	} else {
		wantCode(t, err, EventInvalid, "notifyGuests")
	}
	if _, err := e.edit(row, EventEdit{Summary: ptr("Once"), Scope: calfeed.ScopeThis, NotifyGuests: true}); err != nil {
		t.Fatal(err)
	}
	if m := e.ms.Messages(); len(m) != 1 || m[0].Kind != "update" || m[0].To != "ben@example.com" {
		t.Fatalf("this = %+v", m)
	}
	// The master now has an exception: all is refused, nothing more is written.
	row = occ("2026-10-26")
	patches := e.ms.Count("PATCH")
	_, err = e.edit(row, EventEdit{Summary: ptr("Everything"), Scope: calfeed.ScopeAll, NotifyGuests: true})
	wantCode(t, err, EventNotEditable, LockSeriesUnsupported)
	if e.ms.Count("PATCH") != patches {
		t.Fatal("all with exceptions and guests was written")
	}
	_ = master
}

// TestGraphDeleteTellsGuestsOnce: the organiser's delete cancels the meeting for the
// guests after the confirmation; a guest's copy is never deleted from here.
func TestGraphDeleteTellsGuestsOnce(t *testing.T) {
	e := newGuestEnv(t, false, gEvent("Mine", "2026-10-21", nil), gEvent("Invited", "2026-10-22", invited))
	inv := e.row("Invited")
	_, err := e.h.w.DeleteEvent(e.h.ctx, "alice", e.c.ID, inv.ID, inv.Rev, "", true)
	wantCode(t, err, EventNotEditable, LockNotOrganizer)
	if e.writes() != 0 {
		t.Fatal("a guest's copy was deleted")
	}
	mine := e.row("Mine")
	if _, err := e.h.w.DeleteEvent(e.h.ctx, "alice", e.c.ID, mine.ID, mine.Rev, "", true); err != nil {
		t.Fatal(err)
	}
	if e.ms.Count("DELETE") != 1 {
		t.Fatalf("deletes = %d", e.ms.Count("DELETE"))
	}
	for _, m := range e.ms.Messages() {
		if m.Kind != "cancel" {
			t.Fatalf("messages = %+v", e.ms.Messages())
		}
	}
	if len(e.ms.Messages()) != 3 {
		t.Fatalf("cancellations = %+v", e.ms.Messages())
	}
}

// TestGraphOnlineMeetingAndCreate: a Teams meeting is offered only where the
// calendar lists the provider; a create with guests needs the confirmation and then
// invites once.
func TestGraphOnlineMeetingAndCreate(t *testing.T) {
	plain := newGuestEnv(t, false, gEvent("Mine", "2026-10-21", func(g *graphtest.Event) { g.Guests = nil }))
	row := plain.row("Mine")
	_, err := plain.edit(row, EventEdit{OnlineMeeting: true})
	wantCode(t, err, EventInvalid, "onlineMeeting")
	_, err = plain.h.w.CreateEvent(plain.h.ctx, "alice", plain.c.ID, "cal-work", EventNew{UID: "client-id-1", Summary: "Call", When: whenOf(t), OnlineMeeting: true})
	wantCode(t, err, EventInvalid, "onlineMeeting")
	if plain.writes() != 0 {
		t.Fatal("a refused online meeting was written")
	}
	// The writer maps Graph's own refusal of the provider.
	if _, err := plain.h.writer(plain.c).Update(plain.h.ctx, plain.h.davRef(plain.c, "cal-work", row.UID), EventEdit{OnlineMeeting: true}); !errors.Is(err, ErrEventInvalid) || invalidField(err) != "onlineMeeting" {
		t.Fatalf("graph's refusal = %v", err)
	}

	e := newGuestEnv(t, true)
	guests := []GuestAdd{{Email: "ben@example.com", Name: "Ben"}, {Email: "cy@example.com", Optional: true}}
	_, err = e.h.w.CreateEvent(e.h.ctx, "alice", e.c.ID, "cal-work", EventNew{UID: "client-id-2", Summary: "Kick-off", When: whenOf(t), Guests: guests})
	wantCode(t, err, EventInvalid, "notifyGuests")
	if e.writes() != 0 || len(e.ms.Messages()) != 0 {
		t.Fatal("a create with guests and no confirmation wrote")
	}
	res, err := e.h.w.CreateEvent(e.h.ctx, "alice", e.c.ID, "cal-work", EventNew{UID: "client-id-2", Summary: "Kick-off", When: whenOf(t), Guests: guests, NotifyGuests: true, OnlineMeeting: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Event.Conference == nil || res.Event.Conference.Provider != "teams" || res.Event.Guests == nil || res.Event.Guests.Count != 2 || !res.Event.Access.Notifies {
		t.Fatalf("created = %+v", res.Event)
	}
	if m := e.ms.Messages(); len(m) != 2 {
		t.Fatalf("invitations = %+v", m)
	}
	// A repeat of the create invites nobody again.
	if _, err := e.h.w.CreateEvent(e.h.ctx, "alice", e.c.ID, "cal-work", EventNew{UID: "client-id-2", Summary: "Kick-off", When: whenOf(t), Guests: guests, NotifyGuests: true}); err != nil {
		t.Fatal(err)
	}
	if len(e.ms.Messages()) != 2 || e.ms.Count("POST") != 1 {
		t.Fatalf("a replayed create sent more: %+v", e.ms.Messages())
	}
	d, err := e.h.w.EventDetail(e.h.ctx, "alice", e.c.ID, res.Event.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !d.OnlineMeeting.Available || !d.OnlineMeeting.On || !d.CanEdit || d.CanRespond || d.Guests.Count != 2 {
		t.Fatalf("detail = %+v", d)
	}
}

func whenOf(t *testing.T) EventWhen {
	t.Helper()
	return EventWhen{Start: zonedPoint(t, "2026-10-24", "14:00", "UTC"), End: zonedPoint(t, "2026-10-24", "15:00", "UTC")}
}

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// TestGuestPathsLogNothingPersonal (design D12, D18): through a reply, a guest change, a
// refused write, a read-only calendar and an unreachable one, no log line and no error
// text holds an address, a name or a link of the fixtures.
func TestGuestPathsLogNothingPersonal(t *testing.T) {
	logs := &syncBuffer{}
	logger := slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	prev := slog.Default()
	slog.SetDefault(logger)
	t.Cleanup(func() { slog.SetDefault(prev) })
	e := newGuestEnv(t, true,
		gEvent("Mine", "2026-10-21", func(g *graphtest.Event) {
			g.JoinURL, g.IsOnlineMeeting = "https://teams.microsoft.com/l/meetup-join/abc", true
		}),
		gEvent("Invited", "2026-10-22", invited))
	e.h.w.Log = logger
	var texts []string
	note := func(err error) {
		if err != nil {
			texts = append(texts, err.Error())
		}
	}
	inv, mine := e.row("Invited"), e.row("Mine")
	_, err := e.respond(inv, ResponseAccepted, "", true)
	note(err)
	_, err = e.respond(inv, ResponseDeclined, "", true) // stale
	note(err)
	_, err = e.edit(mine, EventEdit{Summary: ptr("x")}) // no confirmation
	note(err)
	_, err = e.edit(mine, EventEdit{NotifyGuests: true, Guests: &GuestsEdit{Add: []GuestAdd{{Email: "me@outlook.com"}, {Email: "new@example.com"}}}})
	note(err)
	_, err = e.edit(mine, EventEdit{NotifyGuests: true, Guests: &GuestsEdit{Add: []GuestAdd{{Email: "new@example.com", Name: "New Guest"}}, Remove: []string{"ben@example.com"}}})
	note(err)
	mine = e.row("Mine")
	_, err = e.h.w.CreateEvent(e.h.ctx, "alice", e.c.ID, "cal-work", EventNew{UID: "client-id-9", Summary: "x", When: whenOf(t), Guests: []GuestAdd{{Email: "cy@example.com"}}, NotifyGuests: true, OnlineMeeting: true})
	note(err)
	e.ms.SetCanEdit("cal-work", false) // the calendar turns read-only: the write is refused (403)
	_, err = e.edit(e.row("Mine"), EventEdit{Summary: ptr("y"), NotifyGuests: true})
	note(err)
	e.ms.SetCanEdit("cal-work", true)
	_ = mine
	e.ms.Close() // unreachable
	_, err = e.respond(e.row("Invited"), ResponseTentative, "", true)
	note(err)
	_, err = e.h.w.EventDetail(e.h.ctx, "alice", e.c.ID, e.row("Mine").ID)
	note(err)
	slog.Info("capture-probe")
	if !strings.Contains(logs.String(), "capture-probe") {
		t.Fatal("the log capture is not attached")
	}
	for _, text := range append(texts, logs.String()) {
		for _, s := range []string{"ben@example.com", "cy@example.com", "room-4@example.com", "ana@example.com", "new@example.com", "me@outlook.com", "teams.microsoft.com", "meetup-join", "New Guest"} {
			if strings.Contains(text, s) {
				t.Errorf("%q appears in %q", s, text)
			}
		}
	}
	if len(texts) < 5 {
		t.Fatalf("the paths did not fail as planned: %q", texts)
	}
}
