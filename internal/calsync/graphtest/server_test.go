package graphtest

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
)

func call(t *testing.T, s *Server, method, path string, body any, hdr map[string]string) (int, map[string]any) {
	t.Helper()
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	req, _ := http.NewRequest(method, s.GraphBase()+path, rd)
	req.Header.Set("Authorization", "Bearer tok")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

// TestCreateEventFake: POST stores the event with its location, showAs and text
// body, a repeat of the transactionId returns the first event, and an html
// body is read as html, or as text with the Prefer header (an empty skeleton
// reads as an empty string).
func TestCreateEventFake(t *testing.T) {
	s := New()
	defer s.Close()
	s.tokens["tok"] = true
	in := map[string]any{"subject": "Call", "start": DT{"2026-11-13T14:00:00", "UTC"}, "end": DT{"2026-11-13T15:00:00", "UTC"},
		"location": map[string]string{"displayName": "Room 4"}, "showAs": "free", "transactionId": "opentodo-ev-abc12345",
		"body": map[string]string{"contentType": "text", "content": "Bring it"}}
	st, first := call(t, s, "POST", "/me/calendars/cal-work/events", in, nil)
	if st != 201 || first["id"] == "" {
		t.Fatalf("create = %d %v", st, first)
	}
	body := first["body"].(map[string]any)
	if body["contentType"] != "text" || body["content"] != "Bring it" || first["showAs"] != "free" || first["location"].(map[string]any)["displayName"] != "Room 4" {
		t.Fatalf("created event = %v", first)
	}
	st, again := call(t, s, "POST", "/me/calendars/cal-work/events", in, nil)
	if st != 201 || again["id"] != first["id"] || len(s.Events("cal-work")) != 1 {
		t.Fatalf("repeat = %d %v, events %d", st, again["id"], len(s.Events("cal-work")))
	}
	id := s.Add(Event{Calendar: "cal-work", Subject: "Rich", BodyType: "html", Body: "<html><body><p>Hello <b>you</b></p></body></html>",
		Start: DT{"2026-11-14T10:00:00", "UTC"}, End: DT{"2026-11-14T11:00:00", "UTC"}})
	skeleton := s.Add(Event{Calendar: "cal-work", Subject: "Empty", BodyType: "html", Body: "<html><head><style>p{}</style></head><body><br></body></html>",
		Start: DT{"2026-11-15T10:00:00", "UTC"}, End: DT{"2026-11-15T11:00:00", "UTC"}})
	_, rich := call(t, s, "GET", "/me/events/"+id, nil, nil)
	if rich["body"].(map[string]any)["contentType"] != "html" {
		t.Fatalf("html body = %v", rich["body"])
	}
	prefer := map[string]string{"Prefer": `outlook.body-content-type="text"`}
	_, txt := call(t, s, "GET", "/me/events/"+id, nil, prefer)
	if b := txt["body"].(map[string]any); b["contentType"] != "text" || b["content"] != "Hello you" {
		t.Fatalf("text view = %v", b)
	}
	_, empty := call(t, s, "GET", "/me/events/"+skeleton, nil, prefer)
	if empty["body"].(map[string]any)["content"] != "" {
		t.Fatalf("skeleton text = %v", empty["body"])
	}
	// PATCH changes location, showAs and body.
	st, patched := call(t, s, "PATCH", "/me/events/"+id, map[string]any{"showAs": "tentative", "location": map[string]string{"displayName": "X"}}, nil)
	if st != 200 || patched["showAs"] != "tentative" || patched["location"].(map[string]any)["displayName"] != "X" {
		t.Fatalf("patch = %d %v", st, patched)
	}
}
