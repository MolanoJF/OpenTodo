package graphtest

import "testing"

func meeting(s *Server, mut func(e *Event)) string {
	e := Event{Calendar: "cal-work", Subject: "Planning", Start: DT{"2026-11-16T14:00:00", "UTC"}, End: DT{"2026-11-16T15:00:00", "UTC"},
		Guests: []Attendee{{Address: "ben@example.com", Name: "Ben", Type: "required", Response: "accepted"}, {Address: "room@example.com", Name: "Room", Type: "resource", Response: "accepted"}}}
	if mut != nil {
		mut(&e)
	}
	return s.Add(e)
}

func newGraph(t *testing.T) *Server {
	t.Helper()
	s := New()
	t.Cleanup(s.Close)
	s.tokens["tok"] = true
	return s
}

// TestFakeRepliesHonourSendResponse: an answer reaches the organiser exactly when
// sendResponse is true or absent, and an organiser cannot answer (M1).
func TestFakeRepliesHonourSendResponse(t *testing.T) {
	s := newGraph(t)
	id := meeting(s, func(e *Event) { e.NotOrganizer, e.Organizer = true, Person{"Ana", "ana@example.com"} })
	if st, _ := call(t, s, "POST", "/me/events/"+id+"/accept", map[string]any{"sendResponse": false}, nil); st != 202 {
		t.Fatalf("accept = %d", st)
	}
	s.AssertNoMessages(t)
	if _, ev := call(t, s, "GET", "/me/events/"+id, nil, nil); ev["responseStatus"].(map[string]any)["response"] != "accepted" {
		t.Fatalf("response = %v", ev["responseStatus"])
	}
	call(t, s, "POST", "/me/events/"+id+"/tentativelyAccept", map[string]any{"sendResponse": true}, nil)
	call(t, s, "POST", "/me/events/"+id+"/decline", nil, nil)
	if m := s.Messages(); len(m) != 2 || m[0] != (Message{"reply", "ana@example.com"}) {
		t.Fatalf("messages = %+v", m)
	}
	mine := meeting(s, nil)
	if st, _ := call(t, s, "POST", "/me/events/"+mine+"/accept", nil, nil); st != 400 {
		t.Errorf("the organiser answered their own meeting: %d", st)
	}
}

// TestFakeOrganiserWritesTellGuests: attendee changes update only the changed
// attendees, any other change updates all of them, a master with surviving exceptions
// sends one more per exception, a delete cancels, and an attendee's copy sends nothing (M2, M3, M8).
func TestFakeOrganiserWritesTellGuests(t *testing.T) {
	s := newGraph(t)
	id := meeting(s, nil)
	_, ev := call(t, s, "GET", "/me/events/"+id, nil, nil)
	etag := ev["@odata.etag"].(string)
	body := map[string]any{"attendees": []map[string]any{
		{"emailAddress": map[string]string{"address": "ben@example.com"}, "type": "required"},
		{"emailAddress": map[string]string{"address": "room@example.com"}, "type": "resource"},
		{"emailAddress": map[string]string{"address": "new@example.com", "name": "New"}, "type": "optional"}}}
	if st, _ := call(t, s, "PATCH", "/me/events/"+id, body, map[string]string{"If-Match": etag}); st != 200 {
		t.Fatalf("patch = %d", st)
	}
	if m := s.Messages(); len(m) != 1 || m[0] != (Message{"update", "new@example.com"}) {
		t.Fatalf("attendee update went to %+v", m)
	}
	_, ev = call(t, s, "GET", "/me/events/"+id, nil, nil)
	for _, a := range ev["attendees"].([]any) {
		if a.(map[string]any)["emailAddress"].(map[string]any)["address"] == "ben@example.com" && a.(map[string]any)["status"].(map[string]any)["response"] != "accepted" {
			t.Error("a retained guest lost the answer")
		}
	}
	s.messages = nil
	call(t, s, "PATCH", "/me/events/"+id, map[string]any{"subject": "Renamed"}, map[string]string{"If-Match": ev["@odata.etag"].(string)})
	if m := s.Messages(); len(m) != 3 {
		t.Fatalf("a rename updated %+v, want every attendee", m)
	}
	s.messages = nil
	st, _ := call(t, s, "DELETE", "/me/events/"+id, nil, nil)
	if st != 204 || len(s.Messages()) != 3 || s.Messages()[0].Kind != "cancel" {
		t.Fatalf("delete = %d, messages %+v", st, s.Messages())
	}
	s.messages = nil
	theirs := meeting(s, func(e *Event) { e.NotOrganizer = true })
	call(t, s, "PATCH", "/me/events/"+theirs, map[string]any{"showAs": "free"}, nil)
	s.AssertNoMessages(t)
}

// TestFakeMasterWithExceptions: M2's one update per surviving exception.
func TestFakeMasterWithExceptions(t *testing.T) {
	s := newGraph(t)
	master := meeting(s, func(e *Event) {
		e.Type = "seriesMaster"
		e.Recurrence = &Recurrence{Pattern: Pattern{Type: "daily", Interval: 1}, Range: Range{Type: "numbered", StartDate: "2026-11-16", NumberOfOccurrences: 5}}
	})
	s.Add(Event{Calendar: "cal-work", Type: "exception", SeriesMasterID: master, Original: "2026-11-17", Subject: "Moved",
		Start: DT{"2026-11-17T16:00:00", "UTC"}, End: DT{"2026-11-17T17:00:00", "UTC"}})
	_, ev := call(t, s, "GET", "/me/events/"+master, nil, nil)
	call(t, s, "PATCH", "/me/events/"+master, map[string]any{"subject": "Renamed"}, map[string]string{"If-Match": ev["@odata.etag"].(string)})
	if m := s.Messages(); len(m) != 4 { // two attendees for the master, two more for the exception
		t.Fatalf("messages = %+v", m)
	}
}

// TestFakeOnlineMeeting: only a provider the calendar lists makes one.
func TestFakeOnlineMeeting(t *testing.T) {
	s := newGraph(t)
	in := map[string]any{"subject": "Call", "start": DT{"2026-11-13T14:00:00", "UTC"}, "end": DT{"2026-11-13T15:00:00", "UTC"},
		"isOnlineMeeting": true, "onlineMeetingProvider": "teamsForBusiness", "transactionId": "opentodo-ev-abc12345"}
	if st, _ := call(t, s, "POST", "/me/calendars/cal-work/events", in, nil); st != 400 {
		t.Fatalf("a calendar without the provider took it: %d", st)
	}
	if len(s.Events("cal-work")) != 0 {
		t.Fatal("a refused create stored an event")
	}
	s.AddCalendar(&Calendar{ID: "cal-work", Name: "Calendar", CanEdit: true, OnlineProviders: []string{"teamsForBusiness"}})
	st, ev := call(t, s, "POST", "/me/calendars/cal-work/events", in, nil)
	if st != 201 || ev["onlineMeeting"].(map[string]any)["joinUrl"] == "" || ev["isOnlineMeeting"] != true {
		t.Fatalf("create = %d %v", st, ev)
	}
}
