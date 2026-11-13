package calsynctest

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

const invitation = "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nUID:i-1\r\nDTSTART:20261116T140000Z\r\nDTEND:20261116T150000Z\r\n" +
	"ORGANIZER:mailto:ana@example.com\r\n" +
	"ATTENDEE;PARTSTAT=ACCEPTED:mailto:ana@example.com\r\n" +
	"ATTENDEE;PARTSTAT=NEEDS-ACTION:mailto:you@example.com\r\n" +
	"ATTENDEE;PARTSTAT=NEEDS-ACTION:mailto:ben@example.com\r\n" +
	"SUMMARY:Planning\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"

func send(t *testing.T, s *Server, method, member, body string, hdr map[string]string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(method, s.CalendarURL("work")+member, strings.NewReader(body))
	req.SetBasicAuth(s.User, s.Password)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func scheduled(t *testing.T, mode Scheduling) *Server {
	t.Helper()
	s := New()
	t.Cleanup(s.Close)
	s.Scheduling, s.Owner = mode, "you@example.com"
	s.Put("work", "i.ics", invitation)
	return s
}

func answered(partstat string) string {
	return strings.Replace(invitation, "PARTSTAT=NEEDS-ACTION:mailto:you@", "PARTSTAT="+partstat+":mailto:you@", 1)
}

// TestSchedulingModes: each simulated server sends a message exactly when the real
// ones are documented to (design D18).
func TestSchedulingModes(t *testing.T) {
	etag := func(s *Server) string { return `"` + strings.Trim(s.objectETag("work", "i.ics"), `"`) + `"` }
	t.Run("none sends nothing", func(t *testing.T) {
		s := scheduled(t, SchedulingNone)
		send(t, s, http.MethodPut, "i.ics", answered("ACCEPTED"), map[string]string{"If-Match": etag(s)})
		s.AssertNoMessages(t)
	})
	t.Run("auto replies to the organiser for a PARTSTAT-only change", func(t *testing.T) {
		s := scheduled(t, SchedulingAuto)
		send(t, s, http.MethodPut, "i.ics", answered("ACCEPTED"), map[string]string{"If-Match": etag(s)})
		if m := s.Messages(); len(m) != 1 || m[0] != (Message{"reply", "ana@example.com"}) {
			t.Fatalf("messages = %+v", m)
		}
		if !strings.Contains(s.Get("work", "i.ics"), "PARTSTAT=ACCEPTED:mailto:you@") {
			t.Error("the answer was not kept")
		}
	})
	t.Run("auto keeps quiet for an identical write", func(t *testing.T) {
		s := scheduled(t, SchedulingAuto)
		send(t, s, http.MethodPut, "i.ics", invitation, map[string]string{"If-Match": etag(s)})
		s.AssertNoMessages(t)
	})
	t.Run("organiser edit updates every other attendee", func(t *testing.T) {
		s := scheduled(t, SchedulingAuto)
		s.Owner = "ana@example.com"
		send(t, s, http.MethodPut, "i.ics", strings.Replace(invitation, "SUMMARY:Planning", "SUMMARY:Moved", 1), map[string]string{"If-Match": etag(s)})
		if m := s.Messages(); len(m) != 2 || m[0].Kind != "update" || m[0].To != "you@example.com" || m[1].To != "ben@example.com" {
			t.Fatalf("messages = %+v", m)
		}
	})
	t.Run("organiser delete cancels", func(t *testing.T) {
		s := scheduled(t, SchedulingAuto)
		s.Owner = "ana@example.com"
		send(t, s, http.MethodDelete, "i.ics", "", map[string]string{"If-Match": etag(s)})
		if m := s.Messages(); len(m) != 2 || m[0].Kind != "cancel" {
			t.Fatalf("messages = %+v", m)
		}
	})
	t.Run("google without KeepsPartstat rewrites the answer", func(t *testing.T) {
		s := scheduled(t, SchedulingGoogle)
		send(t, s, http.MethodPut, "i.ics", answered("ACCEPTED"), map[string]string{"If-Match": etag(s)})
		if !strings.Contains(s.Get("work", "i.ics"), "PARTSTAT=NEEDS-ACTION:mailto:you@") {
			t.Errorf("the answer was kept:\n%s", s.Get("work", "i.ics"))
		}
		s.KeepsPartstat = true
		send(t, s, http.MethodPut, "i.ics", answered("TENTATIVE"), map[string]string{"If-Match": etag(s)})
		if !strings.Contains(s.Get("work", "i.ics"), "PARTSTAT=TENTATIVE:mailto:you@") {
			t.Error("KeepsPartstat did not keep the answer")
		}
	})
	t.Run("google rejects an attendee's other edit", func(t *testing.T) {
		s := scheduled(t, SchedulingGoogle)
		s.RejectsAttendeeEdits = true
		code, body := send(t, s, http.MethodPut, "i.ics", strings.Replace(invitation, "SUMMARY:Planning", "SUMMARY:Mine", 1), map[string]string{"If-Match": etag(s)})
		if code != http.StatusForbidden || !strings.Contains(body, "allowed-attendee-scheduling-object-change") {
			t.Fatalf("status %d body %q", code, body)
		}
		if strings.Contains(s.Get("work", "i.ics"), "Mine") {
			t.Error("the edit was applied")
		}
		s.AssertNoMessages(t)
	})
}

func (s *Server) objectETag(cal, member string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cals[cal].objects[member].ETag
}
