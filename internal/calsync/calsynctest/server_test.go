package calsynctest

import (
	"net/http"
	"strings"
	"testing"
)

func put(t *testing.T, s *Server, url string, hdr map[string]string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPut, url, strings.NewReader("BEGIN:VCALENDAR\r\nEND:VCALENDAR\r\n"))
	req.SetBasicAuth(s.User, s.Password)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp
}

// TestNoETagSwitches: a PUT answers 2xx without an ETag under NoETagOnPut, a
// GET without one under NoETagOnGet, and the If-None-Match of each PUT is recorded.
func TestNoETagSwitches(t *testing.T) {
	s := New()
	defer s.Close()
	u := s.CalendarURL("work") + "a.ics"
	r := put(t, s, u, map[string]string{"If-None-Match": "*"})
	if r.StatusCode != http.StatusCreated || r.Header.Get("ETag") == "" {
		t.Fatalf("default PUT = %d etag %q", r.StatusCode, r.Header.Get("ETag"))
	}
	s.NoETagOnPut = true
	r = put(t, s, s.CalendarURL("work")+"b.ics", map[string]string{"If-None-Match": "*"})
	if r.StatusCode != http.StatusCreated || r.Header.Get("ETag") != "" {
		t.Fatalf("NoETagOnPut PUT = %d etag %q", r.StatusCode, r.Header.Get("ETag"))
	}
	get := func() *http.Response {
		req, _ := http.NewRequest(http.MethodGet, s.CalendarURL("work")+"b.ics", nil)
		req.SetBasicAuth(s.User, s.Password)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp
	}
	if get().Header.Get("ETag") == "" {
		t.Fatal("GET lost its ETag under NoETagOnPut")
	}
	s.NoETagOnGet = true
	if get().Header.Get("ETag") != "" {
		t.Fatal("NoETagOnGet still sent an ETag")
	}
	put(t, s, u, nil)
	got := s.IfNoneMatches()
	if len(got) != 3 || got[0] != "*" || got[1] != "*" || got[2] != "" {
		t.Fatalf("IfNoneMatches = %q", got)
	}
}
