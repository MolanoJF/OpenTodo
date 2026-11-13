package calfeed

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/OpenTodoApp/opentodo/internal/caldav"
)

// Golden cases of testdata/guests (calendar-event-guests D18): the inputs are in
// guests_cases_test.go and written out by -update.

var guestErrors = map[string]error{
	"ErrRespondThis":       ErrRespondThis,
	"ErrIsOrganizer":       ErrIsOrganizer,
	"ErrNotInvited":        ErrNotInvited,
	"ErrOwnerUnknown":      ErrOwnerUnknown,
	"ErrSeriesUnsupported": ErrSeriesUnsupported,
}

func writeCase(t *testing.T, dir string, c guestCase) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	meta, err := json.MarshalIndent(c.meta, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{"in.ics": []byte(c.in), "meta.json": append(meta, '\n')} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func jsonOf(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return append(b, '\n')
}

// readView is GuestInfo as a golden file shows it.
type readView struct {
	Organizer   *Guest  `json:"organizer"`
	IsOrganizer bool    `json:"isOrganizer"`
	You         string  `json:"you"`
	Guests      []Guest `json:"guests"`
	Count       int     `json:"count"`
	Truncated   bool    `json:"truncated"`
}

func TestGuestsGolden(t *testing.T) {
	for _, c := range guestCases() {
		t.Run(c.name, func(t *testing.T) {
			dir := filepath.Join("testdata", "guests", c.name)
			if *update {
				writeCase(t, dir, c)
			}
			in, err := os.ReadFile(filepath.Join(dir, "in.ics"))
			if err != nil {
				t.Fatalf("%v (run with -update)", err)
			}
			var meta guestMeta
			raw, err := os.ReadFile(filepath.Join(dir, "meta.json"))
			if err != nil || json.Unmarshal(raw, &meta) != nil {
				t.Fatalf("meta.json: %v", err)
			}
			root, err := caldav.Parse(in)
			if err != nil {
				t.Fatal(err)
			}
			switch meta.Kind {
			case "read":
				g := ReadGuests(root, meta.Instance, meta.Owner)
				check(t, filepath.Join(dir, "read.json"), jsonOf(t, readView(g)))
			case "lock":
				check(t, filepath.Join(dir, "lock.txt"), []byte(SeriesLocked(root, meta.ETag, meta.Owner)+"\n"))
			case "conference":
				check(t, filepath.Join(dir, "conference.json"), jsonOf(t, ConferenceOf(root, "")))
			case "respond":
				goldenRespond(t, dir, root, meta)
			default:
				t.Fatalf("unknown kind %q", meta.Kind)
			}
		})
	}
}

func goldenRespond(t *testing.T, dir string, root *caldav.Component, meta guestMeta) {
	t.Helper()
	before := string(root.Encode())
	res, err := ApplyRespond(root, Respond{Owner: meta.Owner, Response: meta.Response, Scope: Scope(meta.Scope), Instance: meta.Instance})
	if meta.Error != "" {
		if !errors.Is(err, guestErrors[meta.Error]) {
			t.Fatalf("error = %v, want %s", err, meta.Error)
		}
		if string(root.Encode()) != before {
			t.Error("a refused answer changed the object")
		}
		check(t, filepath.Join(dir, "result.json"), jsonOf(t, map[string]string{"error": meta.Error}))
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	check(t, filepath.Join(dir, "out.ics"), root.Encode())
	check(t, filepath.Join(dir, "result.json"), jsonOf(t, res))
	assertOnlyPartstat(t, before, string(root.Encode()), meta.Owner)
	again, err := ApplyRespond(root, Respond{Owner: meta.Owner, Response: meta.Response, Scope: Scope(meta.Scope)})
	if err != nil || len(again.Changed) != 0 {
		t.Errorf("a second application changed %v (%v)", again.Changed, err)
	}
}

// assertOnlyPartstat diffs two encodings line by line: every changed line is an
// ATTENDEE line of the owner, and only its PARTSTAT value differs.
func assertOnlyPartstat(t *testing.T, before, after, owner string) {
	t.Helper()
	b, a := unfolded(before), unfolded(after)
	if len(a) != len(b) {
		t.Fatalf("line count %d -> %d", len(b), len(a))
	}
	for i := range b {
		if a[i] == b[i] {
			continue
		}
		pb, err1 := caldav.ParseLine(b[i])
		pa, err2 := caldav.ParseLine(a[i])
		if err1 != nil || err2 != nil || pb.Name != "ATTENDEE" || pa.Name != "ATTENDEE" || pb.Value != pa.Value || !strings.EqualFold(strings.TrimPrefix(strings.ToLower(pb.Value), "mailto:"), strings.ToLower(owner)) {
			t.Fatalf("line %d changed beyond the owner's ATTENDEE: %q -> %q", i, b[i], a[i])
		}
		if len(pb.Params) != len(pa.Params) {
			t.Fatalf("parameter count changed on %q", b[i])
		}
		diff := 0
		for j := range pb.Params {
			if strings.Join(pb.Params[j].Values, ",") != strings.Join(pa.Params[j].Values, ",") {
				diff++
				if pb.Params[j].Name != "PARTSTAT" {
					t.Fatalf("parameter %s changed", pb.Params[j].Name)
				}
			}
		}
		if diff != 1 {
			t.Fatalf("%d parameter values changed on %q", diff, b[i])
		}
	}
}

func unfolded(s string) []string { return caldav.Unfold([]byte(s)) }

// TestGuestsRespondD6 pins the byte-for-byte example of design D6.
func TestGuestsRespondD6(t *testing.T) {
	root, err := caldav.Parse([]byte(cal(d6)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyRespond(root, Respond{Owner: "you@example.com", Response: "accepted"}); err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(cal(d6), "PARTSTAT=NEEDS-ACTION;RSVP=TRUE;CN=You", "PARTSTAT=ACCEPTED;RSVP=TRUE;CN=You", 1)
	want2, err := caldav.Parse([]byte(want))
	if err != nil {
		t.Fatal(err)
	}
	if lf(root.Encode()) != lf(want2.Encode()) {
		t.Errorf("got\n%s\nwant\n%s", lf(root.Encode()), lf(want2.Encode()))
	}
	for _, keep := range []string{"SEQUENCE:2", "DTSTAMP:20261110T091500Z"} {
		if !strings.Contains(string(root.Encode()), keep) {
			t.Errorf("%s was touched", keep)
		}
	}
	if strings.Contains(string(root.Encode()), "LAST-MODIFIED") {
		t.Error("LAST-MODIFIED was added")
	}
}

func TestGuestsNormalizeEmail(t *testing.T) {
	good := map[string]string{"ana@example.com": "ana@example.com", " Ana.Silva+x@Example.COM ": "ana.silva+x@example.com", "a@b.co": "a@b.co"}
	for in, want := range good {
		if got, err := NormalizeEmail(in); err != nil || got != want {
			t.Errorf("NormalizeEmail(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "ana", "ana@", "@example.com", "ana@example", "Ana <ana@example.com>", "<ana@example.com>", "ana@example.com\n", "ana@exa\nmple.com",
		"a,b@example.com", "ana @example.com", "ana@example.com, ben@example.com", "ana@.com", "ana@example..com", `"ana b"@example.com`, "ana@example.com\r", strings.Repeat("a", 250) + "@example.com", "ana@exam ple.com"} {
		if got, err := NormalizeEmail(in); err == nil {
			t.Errorf("NormalizeEmail(%q) = %q, want an error", in, got)
		}
	}
}

func FuzzGuestsNormalizeEmail(f *testing.F) {
	for _, s := range []string{"ana@example.com", "a b@c.d", "x@y.z\n", "\"a\"@b.co", "a@b@c.de", "<a@b.co>"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		got, err := NormalizeEmail(s)
		if err != nil {
			return
		}
		if strings.ContainsAny(got, "\r\n, <>\"") || strings.Count(got, "@") != 1 || len(got) > MaxGuestEmail || got != strings.ToLower(got) {
			t.Errorf("NormalizeEmail(%q) = %q", s, got)
		}
	})
}

func FuzzGuestsParseConference(f *testing.F) {
	for _, s := range []string{"https://meet.google.com/a-b-c", "http://zoom.us/j/1", "javascript:alert(1)", "https://zoom.us@evil.example", "HTTPS://TEAMS.MICROSOFT.COM/x", "https://a.b:444/", "https://a.b/\x00"} {
		f.Add(s, true)
		f.Add(s, false)
	}
	f.Fuzz(func(t *testing.T, s string, trusted bool) {
		c, ok := ParseConference(s, trusted)
		if !ok {
			return
		}
		if !strings.HasPrefix(c.URL, "https://") || strings.ContainsAny(c.URL, " \t\r\n\x00") || len(c.URL) > MaxConferenceURL || strings.Contains(strings.SplitN(c.URL[8:], "/", 2)[0], "@") {
			t.Errorf("ParseConference(%q) = %q", s, c.URL)
		}
	})
}

func TestGuestsConferenceTable(t *testing.T) {
	cases := []struct {
		in       string
		trusted  bool
		ok       bool
		provider string
	}{
		{"https://meet.google.com/abc-defg-hij", false, true, "meet"},
		{"https://teams.microsoft.com/l/meetup-join/x", false, true, "teams"},
		{"https://us02web.zoom.us/j/1?pwd=x", false, true, "zoom"},
		{"https://acme.webex.com/meet/a", false, true, "webex"},
		{"https://zoom.us:443/j/1", false, true, "zoom"},
		{"https://example.com/room", false, false, ""},
		{"https://example.com/room", true, true, "other"},
		{"https://zoom.us:8443/j/1", false, false, ""},
		{"http://zoom.us/j/1", true, false, ""},
		{"https://user:pw@zoom.us/j/1", true, false, ""},
		{"https://zoom.us.evil.example/", false, false, ""},
		{"https://xn--zoom-9d0a.us/", false, false, ""},
		{"https://zoom.us/а", true, false, ""},
	}
	for _, tc := range cases {
		c, ok := ParseConference(tc.in, tc.trusted)
		if ok != tc.ok || (ok && c.Provider != tc.provider) {
			t.Errorf("ParseConference(%q, %v) = %+v, %v; want ok %v %s", tc.in, tc.trusted, c, ok, tc.ok, tc.provider)
		}
	}
}

// TestGuestsReadNeverReturnsOverCap checks the cap and that "you" survives it.
func TestGuestsReadOverCapKeepsYou(t *testing.T) {
	in := cal(manyGuests(120))
	in = strings.Replace(in, "END:VEVENT", "ATTENDEE;PARTSTAT=TENTATIVE:mailto:you@example.com\nEND:VEVENT", 1)
	root, err := caldav.Parse([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	g := ReadGuests(root, "", "you@example.com")
	if len(g.Guests) != MaxGuests || !g.Truncated || g.Count != 120 || g.You != ResponseTentative {
		t.Errorf("guests %d truncated %v count %d you %q", len(g.Guests), g.Truncated, g.Count, g.You)
	}
}
