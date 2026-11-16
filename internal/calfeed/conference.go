package calfeed

import (
	"net/url"
	"strings"

	"github.com/OpenTodoApp/opentodo/internal/caldav"
)

// Join links (calendar-event-guests D10). The server never requests the address and
// never logs it: a passcode is often in the query.

// Conference is a join address and the service it belongs to.
type Conference struct {
	URL      string `json:"url"`
	Provider string `json:"provider"` // meet, teams, zoom, webex, other
}

// ParseConference validates one address; trusted is true for a provider field
// (X-GOOGLE-CONFERENCE, Graph's joinUrl), whose host is not checked against the
// list. Only https, no userinfo, no port but 443, an ASCII host, at most 2,048
// characters without a control character.
func ParseConference(raw string, trusted bool) (Conference, bool) {
	raw = strings.TrimSpace(raw)
	if len(raw) < len("https://a.b") || len(raw) > MaxConferenceURL || !strings.EqualFold(raw[:8], "https://") {
		return Conference{}, false
	}
	for _, r := range raw {
		if r < 0x21 || r > 0x7e {
			return Conference{}, false
		}
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Opaque != "" {
		return Conference{}, false
	}
	if p := u.Port(); p != "" && p != "443" {
		return Conference{}, false
	}
	host := strings.ToLower(u.Hostname())
	if !validHost(host) {
		return Conference{}, false
	}
	provider, listed := conferenceProvider(host)
	if !listed && !trusted {
		return Conference{}, false
	}
	if !listed {
		provider = "other"
	}
	u.Host = host
	return Conference{URL: u.String(), Provider: provider}, true
}

func validHost(h string) bool {
	if h == "" || strings.HasPrefix(h, ".") || strings.HasSuffix(h, ".") || strings.Contains(h, "..") || !strings.Contains(h, ".") {
		return false
	}
	for _, r := range h {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '.' || r == '-') {
			return false
		}
	}
	return true
}

// conferenceProvider names the service of a host on the list of D10.
func conferenceProvider(host string) (string, bool) {
	under := func(domain string) bool { return host == domain || strings.HasSuffix(host, "."+domain) }
	switch {
	case host == "meet.google.com":
		return "meet", true
	case host == "teams.microsoft.com" || host == "teams.live.com":
		return "teams", true
	case under("zoom.us") || under("zoomgov.com"):
		return "zoom", true
	case under("webex.com"):
		return "webex", true
	}
	return "", false
}

// firstConference finds the first address in free text that passes ParseConference.
func firstConference(text string) (Conference, bool) {
	lower := asciiLower(text)
	for from := 0; from < len(text); {
		i := strings.Index(lower[from:], "https://")
		if i < 0 {
			return Conference{}, false
		}
		start := from + i
		end := start
		for end < len(text) && !strings.ContainsRune(" \t\r\n>\"<)", rune(text[end])) {
			end++
		}
		cand := strings.TrimRight(text[start:end], ".,;")
		if c, ok := ParseConference(cand, false); ok {
			return c, true
		}
		from = start + len("https://")
	}
	return Conference{}, false
}

func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}

// ConferenceOf finds the join address of an occurrence: X-GOOGLE-CONFERENCE, the URL
// property, then an address in LOCATION and DESCRIPTION; only https, only the
// services of D10 (the provider field excepted). nil when there is none.
func ConferenceOf(root *caldav.Component, instance string) *Conference {
	v := OccurrenceVEvent(root, instance)
	if v == nil {
		return nil
	}
	if p := v.Prop("X-GOOGLE-CONFERENCE"); p != nil {
		if c, ok := ParseConference(p.Value, true); ok {
			return &c
		}
	}
	if p := v.Prop("URL"); p != nil {
		if c, ok := firstConference(p.Value); ok {
			return &c
		}
	}
	for _, name := range []string{"LOCATION", "DESCRIPTION"} {
		if p := v.Prop(name); p != nil {
			if c, ok := firstConference(caldav.UnescapeText(p.Value)); ok {
				return &c
			}
		}
	}
	return nil
}

// ConferenceIn finds the first join address in free text (a Graph location), with the
// same rules as the text fields of ConferenceOf.
func ConferenceIn(text string) (Conference, bool) { return firstConference(text) }
