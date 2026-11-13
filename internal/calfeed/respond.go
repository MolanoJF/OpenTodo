package calfeed

import (
	"errors"
	"strings"

	"github.com/OpenTodoApp/opentodo/internal/caldav"
)

// Answering an invitation in a calendar object (calendar-event-guests D6). The
// change is one parameter value on the user's own ATTENDEE line and nothing else:
// no SEQUENCE, DTSTAMP or LAST-MODIFIED bump (RFC 5546 reserves a higher SEQUENCE
// for an organiser's change, and a server may treat an attendee's copy with a new
// one as an organiser edit), no ORGANIZER, no other ATTENDEE, no SCHEDULE-AGENT.

// Respond is the user's answer to an invitation.
type Respond struct {
	Owner    string // the user's address, never ""
	Response string // accepted, tentative, declined
	Scope    Scope  // all, or "" for a single event; this and following are refused
	Instance string
}

// RespondResult says which VEVENTs changed (instance keys, "" for the master).
type RespondResult struct {
	Changed  []string
	Previous string // the master's PARTSTAT before, upper case
}

// Errors of ApplyRespond.
var (
	ErrNotInvited   = errors.New("not invited")                                     // no ATTENDEE for the owner
	ErrIsOrganizer  = errors.New("user is the organizer")                           // ORGANIZER is the owner
	ErrRespondThis  = errors.New("reply for one occurrence is not written")         // this or following
	ErrOwnerUnknown = errors.New("own address unknown")                             // Owner is ""
	ErrBadResponse  = errors.New("response is not accepted, tentative or declined") // Response is another word
)

var partstats = map[string]string{ResponseAccepted: "ACCEPTED", ResponseTentative: "TENTATIVE", ResponseDeclined: "DECLINED"}

// ownerAttendee is the ATTENDEE property of the owner in a VEVENT, nil when none.
func ownerAttendee(v *caldav.Component, owner string) *caldav.Prop {
	for i := range v.Props {
		if v.Props[i].Name == "ATTENDEE" && addressOf(v.Props[i].Value) == owner {
			return &v.Props[i]
		}
	}
	return nil
}

func partstatOf(p *caldav.Prop) string {
	return strings.ToUpper(strings.TrimSpace(p.Param("PARTSTAT")))
}

// setPartstat changes the PARTSTAT parameter value of a property, adding the
// parameter when the line has none.
func setPartstat(p *caldav.Prop, value string) {
	for i := range p.Params {
		if strings.EqualFold(p.Params[i].Name, "PARTSTAT") {
			p.Params[i].Values = []string{value}
			return
		}
	}
	p.Params = append(p.Params, caldav.Param{Name: "PARTSTAT", Values: []string{value}})
}

// ApplyRespond sets the owner's PARTSTAT, and nothing else, on the master and, for
// scope all, on every override whose own PARTSTAT equals the master's previous one
// (an occurrence the user answered on its own keeps its answer). SEQUENCE, DTSTAMP
// and LAST-MODIFIED are not touched. Applying the same answer again changes nothing.
func ApplyRespond(root *caldav.Component, r Respond) (RespondResult, error) {
	owner := strings.ToLower(strings.TrimSpace(r.Owner))
	want, ok := partstats[r.Response]
	switch {
	case owner == "":
		return RespondResult{}, ErrOwnerUnknown
	case !ok:
		return RespondResult{}, ErrBadResponse
	case r.Scope != "" && r.Scope != ScopeAll:
		return RespondResult{}, ErrRespondThis
	case unsupportedWhy(root) != "":
		return RespondResult{}, ErrSeriesUnsupported
	}
	o, err := loadSeries(root, "")
	if err != nil {
		return RespondResult{}, ErrSeriesUnsupported
	}
	if p := o.m.Prop("ORGANIZER"); p != nil && addressOf(p.Value) == owner {
		return RespondResult{}, ErrIsOrganizer
	}
	mine := ownerAttendee(o.m, owner)
	if mine == nil {
		return RespondResult{}, ErrNotInvited
	}
	res := RespondResult{Previous: partstatOf(mine)}
	if res.Previous != want {
		setPartstat(mine, want)
		res.Changed = append(res.Changed, "")
	}
	if r.Scope != ScopeAll {
		return res, nil
	}
	for _, x := range o.exc {
		if p := ownerAttendee(x.c, owner); p != nil && partstatOf(p) == res.Previous && res.Previous != want {
			setPartstat(p, want)
			res.Changed = append(res.Changed, x.ev.RecurrenceID)
		}
	}
	return res, nil
}
