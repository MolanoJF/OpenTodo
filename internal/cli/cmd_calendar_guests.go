package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"strings"
)

// Guests, replies and join links in `ot calendar event` (OpenSpec change
// calendar-event-guests, design D14): `respond` answers an invitation, and add, edit and
// delete take --guest, --no-guest, --notify-guests and --online-meeting. A change that
// tells people needs --notify-guests, and the command says so before anything is sent
// (exit 2), as the routes do (400 invalid_input, notifyGuests).

// calGuests is what an event row says of its guests: counts and the user's own answer.
type calGuests struct {
	Count      int    `json:"count"`
	You        string `json:"you"`
	Organizer  bool   `json:"organizer"`
	CanRespond bool   `json:"canRespond"`
}

// guestsText is the GUESTS cell: the count and the user's answer ("3", "3 · maybe"), or a
// dash for an event without guests.
func (e calEvent) guestsText() string {
	g := e.Guests
	if g == nil {
		return ""
	}
	n := fmt.Sprint(g.Count)
	switch g.You {
	case "accepted":
		return n + " · yes"
	case "tentative":
		return n + " · maybe"
	case "declined":
		return n + " · no"
	case "needs-action":
		return n + " · no answer"
	}
	return n
}

// guestFlags are --guest, --no-guest, --notify-guests and --online-meeting.
type guestFlags struct {
	guest, noGuest stringList
	notify, online bool
}

// bind registers the flags; edit alone takes --no-guest, and delete only --notify-guests.
func (f *guestFlags) bind(fs *flag.FlagSet, guests, remove, online bool) {
	if guests {
		fs.Var(&f.guest, "guest", "invite a guest: ana@example.com or ana@example.com:optional (repeatable; tells them, so it needs --notify-guests)")
	}
	if remove {
		fs.Var(&f.noGuest, "no-guest", "take a guest off the event by address (repeatable)")
		fs.Var(&f.noGuest, "remove-guest", "same as --no-guest")
	}
	if online {
		fs.BoolVar(&f.online, "online-meeting", false, "add an online meeting with a join link, on a calendar that offers one (it cannot be removed afterwards)")
	}
	fs.BoolVar(&f.notify, "notify-guests", false, "confirm that the calendar tells the guests: needed to invite guests, and to change or delete a meeting you organise that has guests; it cannot be turned off")
}

// given says a guest or online meeting change was asked for.
func (f *guestFlags) given() bool {
	return len(f.guest) > 0 || len(f.noGuest) > 0 || f.online
}

// guestEntry is one --guest value as the body takes it.
func guestEntry(v string) (map[string]any, error) {
	email, role := strings.TrimSpace(v), "required"
	if i := strings.LastIndex(email, ":"); i >= 0 {
		email, role = strings.TrimSpace(email[:i]), strings.TrimSpace(email[i+1:])
	}
	if email == "" || !strings.Contains(email, "@") || (role != "required" && role != "optional") {
		return nil, usageErr("--guest: %q is not an address such as ana@example.com or ana@example.com:optional", v)
	}
	return map[string]any{"email": email, "role": role}, nil
}

// adds is the --guest values as body entries.
func (f *guestFlags) adds() ([]map[string]any, error) {
	var out []map[string]any
	for _, v := range f.guest {
		g, err := guestEntry(v)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, nil
}

// needNotify refuses, before any write, a change that tells people without
// --notify-guests: an event whose access says it notifies, or guests invited.
func (f *guestFlags) needNotify(ev *calEvent, what string) error {
	if f.notify {
		return nil
	}
	switch {
	case ev != nil && ev.Access != nil && ev.Access.Notifies:
		return usageErr("%s %q tells its guests: the calendar sends them its own message and you cannot turn that off. Add --notify-guests to go ahead; nothing was changed", what, ev.name())
	case len(f.guest) > 0:
		return usageErr("inviting guests tells them: the calendar sends the invitation itself and you cannot turn that off. Add --notify-guests to go ahead; nothing was changed")
	}
	return nil
}

// editBody puts the guest members of an edit on the PATCH body.
func (f *guestFlags) editBody(body map[string]any) error {
	adds, err := f.adds()
	if err != nil {
		return err
	}
	if len(adds) > 0 || len(f.noGuest) > 0 {
		g := map[string]any{}
		if len(adds) > 0 {
			g["add"] = adds
		}
		if len(f.noGuest) > 0 {
			g["remove"] = []string(f.noGuest)
		}
		body["guests"] = g
	}
	f.flagsBody(body)
	return nil
}

// createBody puts the guest members of an add on the POST body.
func (f *guestFlags) createBody(body map[string]any) error {
	adds, err := f.adds()
	if err != nil {
		return err
	}
	if len(adds) > 0 {
		body["guests"] = adds
	}
	f.flagsBody(body)
	return nil
}

func (f *guestFlags) flagsBody(body map[string]any) {
	if f.notify {
		body["notifyGuests"] = true
	}
	if f.online {
		body["onlineMeeting"] = true
	}
}

// answerText is a stored answer in the words of the flags.
func answerText(v string) string {
	switch v {
	case "accepted":
		return "yes"
	case "tentative":
		return "maybe"
	case "declined":
		return "no"
	}
	return "no answer"
}

func calendarEventRespond(a *App, ctx context.Context, g *globalFlags, args []string) error {
	fs := a.flagSet("calendar event respond", g)
	yes := fs.Bool("yes", false, "accept the invitation")
	maybe := fs.Bool("maybe", false, "answer maybe (tentative)")
	no := fs.Bool("no", false, "decline the invitation")
	noTell := fs.Bool("no-tell", false, "do not tell the organiser (only on a calendar that allows it)")
	on := fs.String("on", "", "a day near the event, when it is more than 45 days from today")
	var sf seriesFlags
	sf.bind(fs, true, false, false)
	pos, err := a.parse(g, fs, "calendar event respond", args)
	if err != nil {
		return a.helpOr("calendar", err)
	}
	answer, err := chosenAnswer(*yes, *maybe, *no)
	if err != nil || len(pos) != 1 {
		if err == nil {
			err = usageErr("usage: ot calendar event respond <id> --yes | --maybe | --no [--scope this|all] [--no-tell]")
		}
		return err
	}
	c, _, err := a.connect(g)
	if err != nil {
		return err
	}
	ev, err := a.findEvent(ctx, c, pos[0], *on)
	if err != nil {
		return err
	}
	connID, err := connectionOf(ev)
	if err != nil {
		return err
	}
	if err := needReplyScope(ev, &sf); err != nil {
		return err
	}
	if *noTell && !ev.Silent {
		return usageErr("--no-tell: this calendar always tells the organiser; answer without it")
	}
	body := map[string]any{"rev": ev.Rev, "response": answer}
	if sf.scope != "" {
		body["scope"] = sf.scope
	}
	if *noTell {
		body["sendResponse"] = false
	}
	a.target(pos[0], ev.ID, ev.name(), "answered "+answerText(answer))
	var res struct {
		Event json.RawMessage `json:"event"`
	}
	if err := c.postJSON(ctx, eventPath(connID, ev.ID)+"/response", body, &res); err != nil {
		return err
	}
	if a.dryRun() {
		return nil
	}
	if g.json {
		return a.printJSON(res.Event)
	}
	var out calEvent
	_ = json.Unmarshal(res.Event, &out)
	fmt.Fprintf(a.Stdout, "%s  %s  %s %s  answer: %s\n", shortID(out.ID), out.name(), out.Start.Date, out.timeText(), answerText(answerOf(out, answer)))
	return nil
}

// answerOf is the user's answer the event now holds, or what was sent when the row does not say.
func answerOf(e calEvent, sent string) string {
	if e.Guests != nil && e.Guests.You != "" {
		return e.Guests.You
	}
	return sent
}

// chosenAnswer is the one of --yes, --maybe and --no that was given.
func chosenAnswer(yes, maybe, no bool) (string, error) {
	n, answer := 0, ""
	for _, c := range []struct {
		on  bool
		val string
	}{{yes, "accepted"}, {maybe, "tentative"}, {no, "declined"}} {
		if c.on {
			n, answer = n+1, c.val
		}
	}
	if n != 1 {
		return "", usageErr("give exactly one of --yes, --maybe or --no")
	}
	return answer, nil
}

// needReplyScope is needScope for an answer: this or all, never following.
func needReplyScope(ev *calEvent, f *seriesFlags) error {
	if f.scope == "following" {
		return usageErr("--scope: an answer is for this occurrence or all of them, not following")
	}
	return needScope(ev, f, false, false)
}

// query is --notify-guests as a DELETE query suffix.
func (f *guestFlags) query() string {
	if f.notify {
		return "&notifyGuests=true"
	}
	return ""
}
