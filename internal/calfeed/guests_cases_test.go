package calfeed

import (
	"fmt"
	"strings"
)

// The inputs of the golden cases of testdata/guests (calendar-event-guests D18).
// `go test ./internal/calfeed -run Guests -update` writes them to
// testdata/guests/<case>/in.ics with their meta.json and the answers; a normal run
// reads the files only, so a change of an input is a visible diff.

type guestMeta struct {
	Kind     string `json:"kind"` // read, respond, lock, conference
	Owner    string `json:"owner,omitempty"`
	Instance string `json:"instance,omitempty"`
	Response string `json:"response,omitempty"`
	Scope    string `json:"scope,omitempty"`
	ETag     string `json:"etag,omitempty"`
	Error    string `json:"error,omitempty"`
}

type guestCase struct {
	name, in string
	meta     guestMeta
}

const icsHead = "BEGIN:VCALENDAR\nVERSION:2.0\nPRODID:-//Example//EN\n"
const icsFoot = "END:VCALENDAR\n"

func cal(events ...string) string { return icsHead + strings.Join(events, "") + icsFoot }

// d6 is the invitation of design D6, as Google serves it.
const d6 = `BEGIN:VEVENT
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
`

func vevent(lines ...string) string {
	return "BEGIN:VEVENT\n" + strings.Join(lines, "\n") + "\nEND:VEVENT\n"
}

// seriesMaster is a daily series the user is invited to; exceptions follow.
func seriesMaster(extra ...string) string {
	lines := []string{"UID:daily-1@google.com", "DTSTART:20261116T090000Z", "DTEND:20261116T093000Z", "DTSTAMP:20261110T091500Z",
		"RRULE:FREQ=DAILY;COUNT=5", "ORGANIZER;CN=Ana Silva:mailto:ana@example.com",
		"ATTENDEE;PARTSTAT=ACCEPTED;CN=Ana Silva:mailto:ana@example.com",
		"ATTENDEE;PARTSTAT=NEEDS-ACTION;CN=You:mailto:you@example.com", "SEQUENCE:1", "SUMMARY:Standup"}
	return vevent(append(lines, extra...)...)
}

func seriesException(day, partstat string) string {
	return vevent("UID:daily-1@google.com", "RECURRENCE-ID:2026111"+day+"T090000Z", "DTSTART:2026111"+day+"T100000Z", "DTEND:2026111"+day+"T103000Z",
		"DTSTAMP:20261110T091500Z", "ORGANIZER;CN=Ana Silva:mailto:ana@example.com",
		"ATTENDEE;PARTSTAT=ACCEPTED;CN=Ana Silva:mailto:ana@example.com",
		"ATTENDEE;PARTSTAT="+partstat+";CN=You:mailto:you@example.com", "SEQUENCE:1", "SUMMARY:Standup moved")
}

func manyGuests(n int) string {
	lines := []string{"UID:big-1@example.com", "DTSTART:20261116T090000Z", "DTEND:20261116T100000Z", "DTSTAMP:20261110T091500Z",
		"ORGANIZER;CN=Ana Silva:mailto:ana@example.com", "SUMMARY:All hands"}
	for i := 0; i < n; i++ {
		lines = append(lines, fmt.Sprintf("ATTENDEE;PARTSTAT=NEEDS-ACTION;CN=Guest %d:mailto:guest%d@example.com", i, i))
	}
	return vevent(lines...)
}

func confEvent(lines ...string) string {
	base := []string{"UID:conf-1@example.com", "DTSTART:20261116T090000Z", "DTEND:20261116T100000Z", "DTSTAMP:20261110T091500Z", "SUMMARY:Call"}
	return cal(vevent(append(base, lines...)...))
}

func guestCases() []guestCase {
	respond := func(name, in, owner, response, scope, errName string) guestCase {
		return guestCase{name: name, in: in, meta: guestMeta{Kind: "respond", Owner: owner, Response: response, Scope: scope, Error: errName}}
	}
	conf := func(name, in string) guestCase {
		return guestCase{name: name, in: in, meta: guestMeta{Kind: "conference"}}
	}
	you := "you@example.com"
	return []guestCase{
		respond("respond-accept-single", cal(d6), you, "accepted", "", ""),
		respond("respond-tentative", cal(d6), you, "tentative", "", ""),
		respond("respond-decline", cal(d6), you, "declined", "", ""),
		respond("respond-keeps-sequence-and-dtstamp", cal(strings.Replace(d6, "SEQUENCE:2\n", "SEQUENCE:7\nLAST-MODIFIED:20261111T080000Z\n", 1)), you, "accepted", "", ""),
		respond("respond-params-preserved", cal(vevent("UID:p-1@example.com", "DTSTART:20261116T140000Z", "DTEND:20261116T150000Z", "DTSTAMP:20261110T091500Z",
			`ORGANIZER;CN="Silva, Ana":MAILTO:ana@example.com`,
			`ATTENDEE;CUTYPE=INDIVIDUAL;ROLE=REQ-PARTICIPANT;PARTSTAT=NEEDS-ACTION;RSVP=TRUE;CN="Costa, You";X-NUM-GUESTS=0:MAILTO:You@Example.com`,
			"ATTENDEE;PARTSTAT=ACCEPTED;CN=A very long name that makes this line fold over the seventy-five octet limit of RFC 5545 for sure:mailto:long@example.com",
			"SEQUENCE:3", "SUMMARY:Params")), you, "accepted", "", ""),
		respond("respond-case-insensitive-address", cal(d6), "You@Example.COM", "accepted", "", ""),
		respond("respond-all-carries-to-overrides", cal(seriesMaster(), seriesException("7", "NEEDS-ACTION")), you, "accepted", "all", ""),
		respond("respond-all-keeps-own-override", cal(seriesMaster(), seriesException("7", "DECLINED")), you, "accepted", "all", ""),
		respond("respond-this-refused", cal(seriesMaster()), you, "accepted", "this", "ErrRespondThis"),
		respond("respond-organizer-refused", cal(d6), "ana@example.com", "accepted", "", "ErrIsOrganizer"),
		respond("respond-not-invited-refused", cal(d6), "someone@example.com", "accepted", "", "ErrNotInvited"),
		respond("respond-owner-unknown-refused", cal(d6), "", "accepted", "", "ErrOwnerUnknown"),
		respond("respond-series-unsupported-refused", cal(seriesMaster("EXRULE:FREQ=WEEKLY")), you, "accepted", "all", "ErrSeriesUnsupported"),
		{name: "lock-organizer-google", in: cal(vevent("UID:o-1@google.com", "DTSTART:20261116T140000Z", "DTEND:20261116T150000Z", "DTSTAMP:20261110T091500Z",
			"ORGANIZER;CN=You:mailto:you@example.com", "ATTENDEE;PARTSTAT=ACCEPTED:mailto:you@example.com", "ATTENDEE;PARTSTAT=NEEDS-ACTION:mailto:ben@example.com", "SUMMARY:Mine")),
			meta: guestMeta{Kind: "lock", Owner: you, ETag: `"e1"`}},
		{name: "lock-guest-known-owner", in: cal(d6), meta: guestMeta{Kind: "lock", Owner: you, ETag: `"e1"`}},
		{name: "lock-guest-unknown-owner", in: cal(d6), meta: guestMeta{Kind: "lock", ETag: `"e1"`}},
		{name: "lock-guest-in-exception", in: cal(vevent("UID:daily-1@google.com", "DTSTART:20261116T090000Z", "DTEND:20261116T093000Z", "DTSTAMP:20261110T091500Z", "RRULE:FREQ=DAILY;COUNT=5", "SUMMARY:Standup"),
			seriesException("7", "NEEDS-ACTION")), meta: guestMeta{Kind: "lock", Owner: you, ETag: `"e1"`}},
		{name: "read-guests-roles-and-rooms", in: cal(vevent("UID:r-1@example.com", "DTSTART:20261116T090000Z", "DTEND:20261116T100000Z", "DTSTAMP:20261110T091500Z",
			"ORGANIZER;CN=Ana Silva:mailto:ana@example.com",
			"ATTENDEE;ROLE=REQ-PARTICIPANT;PARTSTAT=ACCEPTED;CN=Ana Silva:mailto:ana@example.com",
			"ATTENDEE;ROLE=OPT-PARTICIPANT;PARTSTAT=TENTATIVE;CN=Ben Costa:MAILTO:Ben@Example.com",
			`ATTENDEE;CUTYPE=RESOURCE;ROLE=NON-PARTICIPANT;PARTSTAT=ACCEPTED;CN="Room 4, floor 2":mailto:room-4@example.com`,
			"ATTENDEE;PARTSTAT=NEEDS-ACTION;CN=You:mailto:you@example.com",
			"ATTENDEE;PARTSTAT=DECLINED:mailto:dora@example.com",
			"ATTENDEE:urn:uuid:not-an-address",
			"SUMMARY:Roles")), meta: guestMeta{Kind: "read", Owner: you}},
		{name: "read-guests-over-cap", in: cal(manyGuests(105)), meta: guestMeta{Kind: "read"}},
		{name: "read-organizer-absent", in: cal(vevent("UID:n-1@example.com", "DTSTART:20261116T090000Z", "DTEND:20261116T100000Z", "DTSTAMP:20261110T091500Z",
			"ATTENDEE;PARTSTAT=ACCEPTED:mailto:ben@example.com", "SUMMARY:No organizer")), meta: guestMeta{Kind: "read", Owner: you}},
		conf("conference-x-google", confEvent("X-GOOGLE-CONFERENCE:https://meet.google.com/abc-defg-hij")),
		conf("conference-url-property", confEvent("URL:https://acme.zoom.us/j/123456?pwd=abc")),
		conf("conference-description-meet", confEvent(`DESCRIPTION:Join with Google Meet: https://meet.google.com/xyz-abcd-efg\nSee you`)),
		conf("conference-teams-in-location", confEvent("LOCATION:https://teams.microsoft.com/l/meetup-join/19%3ameeting_abc/0")),
		conf("conference-http-refused", confEvent("LOCATION:http://meet.google.com/abc-defg-hij")),
		conf("conference-userinfo-refused", confEvent("LOCATION:https://zoom.us@evil.example/j/1")),
		conf("conference-lookalike-host-refused", confEvent("LOCATION:https://zoom.us.evil.example/j/1 or https://notzoom.us/j/1")),
		conf("conference-javascript-refused", confEvent("X-GOOGLE-CONFERENCE:javascript:alert(1)", "URL:javascript:alert(1)")),
		conf("conference-too-long-refused", confEvent("LOCATION:https://meet.google.com/"+strings.Repeat("a", 2100))),
	}
}
