package calfeed

import (
	"net/mail"
	"strings"
	"unicode"

	"github.com/OpenTodoApp/opentodo/internal/caldav"
	"github.com/OpenTodoApp/opentodo/internal/textutil"
)

// Guests of an event (OpenSpec change calendar-event-guests, design D2 and D4):
// pure functions over a parsed calendar object, with no I/O and no clock. A guest's
// name and address are personal data of someone who is not a user of this server:
// nothing here logs them, and an error never carries one.

// Caps of what is read and stored (design D2).
const (
	MaxGuests        = 100
	MaxGuestName     = 120
	MaxGuestEmail    = 254
	MaxConferenceURL = 2048
)

// Roles and answers of a guest.
const (
	RoleRequired = "required"
	RoleOptional = "optional"
	RoleRoom     = "room"

	ResponseNeedsAction = "needs-action"
	ResponseAccepted    = "accepted"
	ResponseTentative   = "tentative"
	ResponseDeclined    = "declined"
)

// Guest is one person (or room) on an event as the calendar lists it.
type Guest struct {
	Name     string `json:"name"`
	Email    string `json:"email"` // lower case, no mailto:
	Role     string `json:"role"`  // required, optional, room
	Response string `json:"response"`
	You      bool   `json:"you"`
}

// GuestInfo is what an occurrence says about its guests.
type GuestInfo struct {
	Organizer   *Guest
	IsOrganizer bool    // the user organises it
	You         string  // the user's answer, "" when the user is not a guest or not known
	Guests      []Guest // at most MaxGuests, the user included, rooms included
	Count       int     // every attendee but the user
	Truncated   bool
}

// Empty reports whether the occurrence has nothing to say about guests.
func (g GuestInfo) Empty() bool {
	return g.Organizer == nil && !g.IsOrganizer && g.You == "" && len(g.Guests) == 0 && g.Count == 0
}

type guestErr string

func (e guestErr) Error() string { return string(e) }

const errBadEmail = guestErr("not an email address")

// NormalizeEmail is the one email check of every write path: a bare local@domain
// address (no display name, angle brackets, comma, space or control character),
// at most 254 characters, a domain with a dot. It returns the lower-case form.
func NormalizeEmail(s string) (string, error) {
	if strings.IndexFunc(s, func(r rune) bool { return r == 13 || r == 10 || r == 0 }) >= 0 {
		return "", errBadEmail
	}
	s = strings.TrimSpace(s)
	if s == "" || len(s) > MaxGuestEmail {
		return "", errBadEmail
	}
	for _, r := range s {
		if r < 0x21 || r > 0x7e || strings.ContainsRune("\",<>();:\\[]", r) || unicode.IsSpace(r) || unicode.IsControl(r) {
			return "", errBadEmail
		}
	}
	a, err := mail.ParseAddress(s)
	if err != nil || a.Name != "" || !strings.EqualFold(a.Address, s) {
		return "", errBadEmail
	}
	local, domain, ok := strings.Cut(strings.ToLower(a.Address), "@")
	if !ok || local == "" || strings.Contains(domain, "@") || !strings.Contains(domain, ".") ||
		strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") || strings.Contains(domain, "..") {
		return "", errBadEmail
	}
	return local + "@" + domain, nil
}

// addressOf reads the address of an ORGANIZER or ATTENDEE value ("" when it is not
// an email address).
func addressOf(value string) string {
	v := strings.TrimSpace(value)
	if len(v) >= 7 && strings.EqualFold(v[:7], "mailto:") {
		v = v[7:]
	}
	email, err := NormalizeEmail(v)
	if err != nil {
		return ""
	}
	return email
}

func guestName(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, strings.TrimSpace(s))
	return textutil.Truncate(s, MaxGuestName)
}

func roleOf(p caldav.Prop) string {
	switch cu := strings.ToUpper(p.Param("CUTYPE")); {
	case cu == "RESOURCE" || cu == "ROOM":
		return RoleRoom
	case strings.EqualFold(p.Param("ROLE"), "OPT-PARTICIPANT"):
		return RoleOptional
	}
	return RoleRequired
}

func responseOf(p caldav.Prop) string {
	switch strings.ToUpper(p.Param("PARTSTAT")) {
	case "ACCEPTED":
		return ResponseAccepted
	case "TENTATIVE":
		return ResponseTentative
	case "DECLINED":
		return ResponseDeclined
	}
	return ResponseNeedsAction
}

// guestVEvent is the VEVENT whose guests an occurrence shows: its exception when it
// has one and that carries guests, else the master.
func guestVEvent(root *caldav.Component, instance string) *caldav.Component {
	if v := OccurrenceVEvent(root, instance); v != nil && (v.Prop("ATTENDEE") != nil || v.Prop("ORGANIZER") != nil) {
		return v
	}
	return OccurrenceVEvent(root, "")
}

// ReadGuests reads ORGANIZER and ATTENDEE of the VEVENT that is the occurrence (the
// exception of instance when there is one, else the master). owner is the user's
// address ("" when unknown); an address is matched as the text after mailto:,
// case-insensitively.
func ReadGuests(root *caldav.Component, instance, owner string) GuestInfo {
	var info GuestInfo
	v := guestVEvent(root, instance)
	if v == nil {
		return info
	}
	owner = strings.ToLower(strings.TrimSpace(owner))
	if p := v.Prop("ORGANIZER"); p != nil {
		if email := addressOf(p.Value); email != "" {
			info.Organizer = &Guest{Name: guestName(caldav.UnescapeText(p.Param("CN"))), Email: email, Role: RoleRequired, Response: ResponseAccepted, You: owner != "" && email == owner}
			info.IsOrganizer = info.Organizer.You
		}
	}
	seen := map[string]bool{}
	for _, p := range v.Props {
		if p.Name != "ATTENDEE" {
			continue
		}
		email := addressOf(p.Value)
		if email == "" || seen[email] {
			continue
		}
		seen[email] = true
		g := Guest{Name: guestName(caldav.UnescapeText(p.Param("CN"))), Email: email, Role: roleOf(p), Response: responseOf(p), You: owner != "" && email == owner}
		if g.You {
			info.You = g.Response
		} else {
			info.Count++
		}
		if len(info.Guests) < MaxGuests {
			info.Guests = append(info.Guests, g)
		} else {
			info.Truncated = true
		}
	}
	return info
}

// GuestName cleans a name a calendar gave for a guest: control characters out, at
// most MaxGuestName bytes.
func GuestName(s string) string { return guestName(s) }
