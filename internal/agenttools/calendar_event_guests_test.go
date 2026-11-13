package agenttools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/OpenTodoApp/opentodo/internal/audit"
)

// calendar-event-guests group 6: the answer tool, the guests arguments, notify_guests,
// and guests only inside untrusted text.

var replyArgs = map[string]any{"connection_id": "c1", "event_id": "e1", "rev": "r1", "response": "accepted", "scope": "all"}

func TestRespondToCalendarEventNeedsTheGate(t *testing.T) {
	f, ed := eventFixture(t)
	for name, mod := range map[string]func(*Caller){
		DeniedByOwner: func(c *Caller) { c.OwnerAllowsWrites = false },
		DeniedByUser:  func(c *Caller) { c.UserAllowsWrites = false },
		DeniedByScope: func(c *Caller) { c.Scope = "full" },
	} {
		c := agent("u1")
		mod(&c)
		if _, err := f.call(c, "respond_to_calendar_event", replyArgs); toolCode0(t, err) != name {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if len(ed.replies) != 0 {
		t.Fatal("a denied call answered")
	}
}

func TestRespondToCalendarEvent(t *testing.T) {
	f, ed := eventFixture(t)
	out := f.mustCall(agent("u1"), "respond_to_calendar_event", replyArgs)
	if len(ed.replies) != 1 {
		t.Fatalf("replies: %+v", ed.replies)
	}
	r := ed.replies[0]
	if r.ConnectionID != "c1" || r.EventID != "e1" || r.Rev != "r1" || r.Response != "accepted" || r.Scope != "all" || r.SendResponse != nil {
		t.Fatalf("reply: %+v", r)
	}
	raw, _ := json.Marshal(out)
	if !strings.Contains(string(raw), `"replaces":"e1"`) || !strings.Contains(string(raw), `"untrusted_text":{"title":"Planning"}`) {
		t.Fatalf("result: %s", raw)
	}
	silent := map[string]any{"connection_id": "c1", "event_id": "e1", "rev": "r1", "response": "tentative", "send_response": false}
	f.mustCall(agent("u1"), "respond_to_calendar_event", silent)
	if s := ed.replies[1].SendResponse; s == nil || *s {
		t.Fatalf("send_response: %v", s)
	}
	evs, _, _ := f.rec.List(context.Background(), audit.Filter{Name: audit.AgentWriteName, Limit: 100})
	if len(evs) != 2 {
		t.Fatalf("agent.write events: %d", len(evs))
	}
	for _, bad := range []map[string]any{
		{"connection_id": "c1", "event_id": "e1", "rev": "r1"},
		{"connection_id": "c1", "event_id": "e1", "rev": "r1", "response": "accepted", "yes": true},
		{"connection_id": "c1", "event_id": "e1", "rev": "r1", "response": "accepted", "send_response": "no"},
	} {
		if _, err := f.call(agent("u1"), "respond_to_calendar_event", bad); err == nil {
			t.Fatalf("%v accepted", bad)
		}
	}
	for _, code := range []string{"event_not_editable", "event_changed", "scope_required", "invalid_input"} {
		ed.err = &ToolError{Code: code, Message: "m"}
		if _, err := f.call(agent("u1"), "respond_to_calendar_event", replyArgs); toolCode0(t, err) != code {
			t.Fatalf("%s: %v", code, err)
		}
	}
}

func TestRespondToCalendarEventIsLimited(t *testing.T) {
	f, ed := eventFixture(t)
	c := agent("u1")
	if ok, _ := f.reg.limits.Reserve(c.TokenID, PerDay); !ok {
		t.Fatal("reserve")
	}
	if _, err := f.call(c, "respond_to_calendar_event", replyArgs); toolCode0(t, err) != "rate_limited" || len(ed.replies) != 0 {
		t.Fatalf("past the daily items: %v", err)
	}
}

func TestUpdateAndCreateTakeGuests(t *testing.T) {
	f, ed := eventFixture(t)
	c := agent("u1")
	f.mustCall(c, "update_calendar_event", map[string]any{"connection_id": "c1", "event_id": "e1", "rev": "r1", "summary": "Planning",
		"guests":        map[string]any{"add": []any{map[string]any{"email": "ben@example.com", "name": "Ben", "optional": true}}, "remove": []string{"ana@example.com"}},
		"notify_guests": true, "online_meeting": true})
	u := ed.updates[0]
	if u.Guests == nil || len(u.Guests.Add) != 1 || u.Guests.Add[0] != (EventGuest{Email: "ben@example.com", Name: "Ben", Optional: true}) ||
		len(u.Guests.Remove) != 1 || u.NotifyGuests == nil || !*u.NotifyGuests || u.OnlineMeeting == nil || !*u.OnlineMeeting {
		t.Fatalf("update: %+v", u)
	}
	f.mustCall(c, "update_calendar_event", map[string]any{"connection_id": "c1", "event_id": "e1", "rev": "r1", "summary": "x"})
	if u := ed.updates[1]; u.Guests != nil || u.NotifyGuests != nil || u.OnlineMeeting != nil {
		t.Fatalf("unsent members must stay nil: %+v", u)
	}
	f.mustCall(c, "create_calendar_event", withArgs(map[string]any{"guests": []any{map[string]any{"email": "ben@example.com"}}, "notify_guests": true, "online_meeting": true}))
	if g := ed.creates[0]; len(g.Guests) != 1 || g.Guests[0].Email != "ben@example.com" || g.NotifyGuests == nil || !*g.NotifyGuests || g.OnlineMeeting == nil || !*g.OnlineMeeting {
		t.Fatalf("create: %+v", g)
	}
	tooMany := make([]any, 51)
	for i := range tooMany {
		tooMany[i] = map[string]any{"email": "a@example.com"}
	}
	for _, bad := range []map[string]any{
		{"guests": "ben@example.com"},
		{"guests": []any{map[string]any{"email": "a@example.com", "phone": "1"}}},
		{"guests": tooMany},
		{"notify_guests": "yes"},
	} {
		if _, err := f.call(c, "create_calendar_event", withArgs(bad)); err == nil {
			t.Fatalf("create %v accepted", bad)
		}
	}
	for _, bad := range []any{
		map[string]any{"add": "x"}, map[string]any{"drop": []string{"x"}}, []any{"x"},
	} {
		if _, err := f.call(c, "update_calendar_event", map[string]any{"connection_id": "c1", "event_id": "e1", "rev": "r1", "guests": bad}); err == nil {
			t.Fatalf("update %v accepted", bad)
		}
	}
}

func TestDeleteCalendarEventPassesNotifyGuests(t *testing.T) {
	f, ed := eventFixture(t)
	c := agent("u1")
	f.mustCall(c, "delete_calendar_event", map[string]any{"connection_id": "c1", "event_id": "e1", "rev": "r1"})
	f.mustCall(c, "delete_calendar_event", map[string]any{"connection_id": "c1", "event_id": "e1", "rev": "r1", "notify_guests": true})
	if len(ed.deleteNotify) != 2 || ed.deleteNotify[0] || !ed.deleteNotify[1] {
		t.Fatalf("notify: %v", ed.deleteNotify)
	}
	if _, err := f.call(c, "delete_calendar_event", map[string]any{"connection_id": "c1", "event_id": "e1", "rev": "r1", "notify_guests": 1}); err == nil {
		t.Fatal("a non-boolean notify_guests was accepted")
	}
}

// The notify_guests text is D14's verbatim, the old "never sent" sentence is gone and the
// new tool and arguments are in the registry.
func TestGuestToolDefinitions(t *testing.T) {
	f, _ := eventFixture(t)
	want := "notify_guests must be true when the event's access.notifies is true: the calendar then sends the guests an update itself and it cannot be turned off. Never set it without the user having asked you to change the event they organise."
	defs := map[string]Definition{}
	for _, d := range f.reg.Definitions(agent("u1")) {
		defs[d.Name] = d
	}
	for _, name := range []string{"update_calendar_event", "create_calendar_event", "delete_calendar_event"} {
		d := defs[name]
		if !strings.Contains(d.Description, want) || strings.Contains(d.Description, "No invitation or update is ever sent") || strings.Contains(d.Description, "no invitation is ever sent") {
			t.Fatalf("%s: %s", name, d.Description)
		}
	}
	d, ok := defs["respond_to_calendar_event"]
	if !ok || d.Kind != KindWrite {
		t.Fatalf("respond_to_calendar_event: %+v", d)
	}
	props, _ := d.InputSchema["properties"].(map[string]any)
	for _, k := range []string{"connection_id", "event_id", "rev", "response", "scope", "send_response"} {
		if _, ok := props[k]; !ok {
			t.Fatalf("respond_to_calendar_event lacks %s", k)
		}
	}
	for _, name := range []string{"update_calendar_event", "create_calendar_event"} {
		props, _ := defs[name].InputSchema["properties"].(map[string]any)
		for _, k := range []string{"guests", "notify_guests", "online_meeting"} {
			if _, ok := props[k]; !ok {
				t.Fatalf("%s lacks %s", name, k)
			}
		}
	}
}

func guestFixture(t *testing.T) (*fixture, *fakeEditor) {
	f, ed := eventFixture(t)
	ed.events[0].Guests = &EventGuestSummary{Count: 2, You: "needs-action", CanRespond: true}
	ed.events[0].Conference = &EventConference{Provider: "teams"}
	ed.events[0].Access.Notifies = true
	ed.people = map[string]EventGuestList{"e1": {
		Organizer:     &EventPerson{Name: "Ana Silva", Email: "ana@example.com"},
		Guests:        []EventPerson{{Name: "Ben", Email: "ben@example.com", Role: "required", Response: "accepted"}},
		ConferenceURL: "https://teams.example/join/1",
	}}
	return f, ed
}

// Names, addresses and the link sit in untrusted_text, and only with include_guests; the
// rest of the row holds counts and answers.
func TestListCalendarEventsShowsGuestsOnlyInUntrustedText(t *testing.T) {
	f, _ := guestFixture(t)
	read := agent("u1")
	read.Scope = "read"
	days := map[string]any{"from": "2026-11-12", "to": "2026-11-12"}

	plain, _ := json.Marshal(f.mustCall(read, "list_calendar_events", days))
	for _, leak := range []string{"ana@example.com", "ben@example.com", "Ana Silva", "teams.example"} {
		if strings.Contains(string(plain), leak) {
			t.Fatalf("%q without include_guests: %s", leak, plain)
		}
	}
	for _, want := range []string{`"guests":{"count":2,"you":"needs-action","organizer":false,"can_respond":true}`, `"conference":{"provider":"teams"}`, `"notifies":true`} {
		if !strings.Contains(string(plain), want) {
			t.Fatalf("missing %s: %s", want, plain)
		}
	}

	days["include_guests"] = true
	raw, _ := json.Marshal(f.mustCall(read, "list_calendar_events", days))
	var got struct {
		Events []map[string]json.RawMessage `json:"events"`
	}
	if err := json.Unmarshal(raw, &got); err != nil || len(got.Events) != 1 {
		t.Fatalf("%v: %s", err, raw)
	}
	ev := got.Events[0]
	for k, v := range ev {
		if k != "untrusted_text" && (strings.Contains(string(v), "@") || strings.Contains(string(v), "teams.example")) {
			t.Fatalf("%s holds a guest outside untrusted_text: %s", k, v)
		}
	}
	var ut struct {
		Organizer     struct{ Name, Email string }
		Guests        []struct{ Name, Email, Role, Response string }
		ConferenceURL string `json:"conference_url"`
	}
	if err := json.Unmarshal(ev["untrusted_text"], &ut); err != nil {
		t.Fatal(err)
	}
	if ut.Organizer.Email != "ana@example.com" || len(ut.Guests) != 1 || ut.Guests[0].Email != "ben@example.com" || ut.Guests[0].Response != "accepted" || ut.ConferenceURL != "https://teams.example/join/1" {
		t.Fatalf("untrusted_text: %s", ev["untrusted_text"])
	}
	// Encrypted accounts read it too: guests are server data (design D15).
	enc := read
	enc.Encrypted = true
	f.mustCall(enc, "list_calendar_events", days)
	if _, err := f.call(read, "list_calendar_events", map[string]any{"from": "2026-11-12", "to": "2026-11-12", "include_guests": "yes"}); err == nil {
		t.Fatal("a non-boolean include_guests was accepted")
	}
}

func TestListCalendarEventsCapsTheGuestList(t *testing.T) {
	f, ed := guestFixture(t)
	var many []EventPerson
	for i := 0; i < 80; i++ {
		many = append(many, EventPerson{Email: "g@example.com"})
	}
	l := ed.people["e1"]
	l.Guests = many
	ed.people["e1"] = l
	out := f.mustCall(agent("u1"), "list_calendar_events", map[string]any{"from": "2026-11-12", "to": "2026-11-12", "include_guests": true})
	raw, _ := json.Marshal(out)
	if n := strings.Count(string(raw), `"g@example.com"`); n != MaxEventGuestsListed {
		t.Fatalf("guests listed: %d", n)
	}
}
