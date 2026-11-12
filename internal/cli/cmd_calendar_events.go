package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// Calendar event commands (OpenSpec change calendar-event-edits, design D8):
// list the calendar's events with their ids and what may be changed, rename or
// move one, and delete one, through GET /api/v1/calendar-feeds/events and the
// two event routes of /api/v1/calendar-sync/connections/{id}/events/{eventId}.
// The connection comes from the event's subscription. A read token can list;
// changing needs a full token (the write gate refuses the others before
// anything is sent).

// calWhen is one end of an event: a date and, for a timed event, a wall time
// and zone.
type calWhen struct {
	Date string  `json:"date"`
	Time *string `json:"time,omitempty"`
	TZ   *string `json:"tz,omitempty"`
}

// calEvent is an event as the events route returns it.
type calEvent struct {
	ID             string  `json:"id"`
	SubscriptionID string  `json:"subscriptionId"`
	Summary        string  `json:"summary"`
	Start          calWhen `json:"start"`
	End            calWhen `json:"end"`
	AllDay         bool    `json:"allDay"`
	Rev            string  `json:"rev"`
	Access         *struct {
		Update bool   `json:"update"`
		Delete bool   `json:"delete"`
		Reason string `json:"reason"`
	} `json:"access"`
	// ConnectionID and Calendar come from the event's subscription.
	ConnectionID string          `json:"-"`
	Calendar     string          `json:"-"`
	Raw          json.RawMessage `json:"-"`
}

func (e calEvent) name() string {
	if e.Summary == "" {
		return "(no title)"
	}
	return e.Summary
}

func (e calEvent) timeText() string {
	if e.AllDay || e.Start.Time == nil {
		return "all day"
	}
	end := ""
	if e.End.Time != nil && e.End.Date == e.Start.Date {
		end = "-" + *e.End.Time
	}
	return *e.Start.Time + end
}

func (e calEvent) accessText() string {
	switch {
	case e.Access == nil:
		return "read-only"
	case e.Access.Update && e.Access.Delete:
		return "edit"
	case e.Access.Reason != "":
		return "read-only (" + e.Access.Reason + ")"
	}
	return "read-only"
}

// calEvents reads the events touching from..to with their subscriptions.
func (c *client) calEvents(ctx context.Context, from, to string) ([]calEvent, error) {
	q := url.Values{"from": {from}, "to": {to}}
	var page struct {
		Events        []json.RawMessage `json:"events"`
		Subscriptions []struct {
			ID           string `json:"id"`
			Name         string `json:"name"`
			ConnectionID string `json:"connectionId"`
		} `json:"subscriptions"`
	}
	if err := c.getJSON(ctx, "/calendar-feeds/events?"+q.Encode(), &page); err != nil {
		return nil, err
	}
	out := make([]calEvent, len(page.Events))
	for i, raw := range page.Events {
		_ = json.Unmarshal(raw, &out[i])
		out[i].Raw = raw
		for _, s := range page.Subscriptions {
			if s.ID == out[i].SubscriptionID {
				out[i].ConnectionID, out[i].Calendar = s.ConnectionID, s.Name
			}
		}
	}
	return out, nil
}

func cmdCalendar(a *App, ctx context.Context, g *globalFlags, args []string) error {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return calendarEvents(a, ctx, g, args)
	}
	switch args[0] {
	case "events":
		return calendarEvents(a, ctx, g, args[1:])
	case "event":
		if len(args) > 1 {
			switch args[1] {
			case "add":
				return calendarEventAdd(a, ctx, g, args[2:])
			case "edit":
				return calendarEventEdit(a, ctx, g, args[2:])
			case "delete", "rm":
				return calendarEventDelete(a, ctx, g, args[2:])
			}
		}
		return usageErr("usage: ot calendar event add <title> … | ot calendar event edit <id> … | ot calendar event delete <id>")
	}
	return usageErr("unknown calendar command %q (expected events or event)", args[0])
}

// dayRange resolves --from and --to (default: today and the six days after).
func (a *App) dayRange(from, to string) (string, string, error) {
	f, t := from, to
	var err error
	if f == "" {
		f = localDate(a.Now())
	} else if f, err = resolveDate(from, a.Now()); err != nil {
		return "", "", err
	}
	if t == "" {
		d, _ := time.Parse(time.DateOnly, f)
		t = d.AddDate(0, 0, 6).Format(time.DateOnly)
	} else if t, err = resolveDate(to, a.Now()); err != nil {
		return "", "", err
	}
	if t < f {
		return "", "", usageErr("--to must not be before --from")
	}
	return f, t, nil
}

func calendarEvents(a *App, ctx context.Context, g *globalFlags, args []string) error {
	fs := a.flagSet("calendar events", g)
	from := fs.String("from", "", "first day (YYYY-MM-DD, today, tomorrow or a weekday; default today)")
	to := fs.String("to", "", "last day (default: six days after --from; at most 92 days in all)")
	pos, err := a.parse(g, fs, "calendar events", args)
	if err != nil {
		return a.helpOr("calendar", err)
	}
	if len(pos) != 0 {
		return usageErr("usage: ot calendar events [--from <day>] [--to <day>]")
	}
	f, t, err := a.dayRange(*from, *to)
	if err != nil {
		return err
	}
	c, _, err := a.connect(g)
	if err != nil {
		return err
	}
	rows, err := c.calEvents(ctx, f, t)
	if err != nil {
		return err
	}
	if g.json {
		list := make([]json.RawMessage, len(rows))
		for i, r := range rows {
			list[i] = r.Raw
		}
		return a.printJSON(list)
	}
	if len(rows) == 0 {
		fmt.Fprintln(a.Stdout, "No events.")
		return nil
	}
	tbl := newTable("ID", "DATE", "TIME", "TITLE", "CALENDAR", "ACCESS")
	for _, r := range rows {
		tbl.add(cell{text: shortID(r.ID), style: styleCyan}, plain(r.Start.Date), plain(r.timeText()), plain(r.name()), plain(r.Calendar), plain(r.accessText()))
	}
	tbl.render(a.Stdout, a.color())
	return nil
}

// eventSearchDays is how far from today (or --on) a command looks for an id:
// the events route's 92 days at most.
const eventSearchDays = 45

// findEvent resolves an event by id or unique id prefix within eventSearchDays
// of the day on (default today).
func (a *App) findEvent(ctx context.Context, c *client, ref, on string) (*calEvent, error) {
	day := localDate(a.Now())
	if on != "" {
		var err error
		if day, err = resolveDate(on, a.Now()); err != nil {
			return nil, err
		}
	}
	d, _ := time.Parse(time.DateOnly, day)
	rows, err := c.calEvents(ctx, d.AddDate(0, 0, -eventSearchDays).Format(time.DateOnly), d.AddDate(0, 0, eventSearchDays).Format(time.DateOnly))
	if err != nil {
		return nil, err
	}
	var hit *calEvent
	for i := range rows {
		switch {
		case rows[i].ID == ref:
			return &rows[i], nil
		case strings.HasPrefix(rows[i].ID, ref):
			if hit != nil {
				return nil, usageErr("%q matches more than one event; use more of its id", ref)
			}
			hit = &rows[i]
		}
	}
	if hit == nil {
		return nil, usageErr("no event %q within %d days of %s (see `ot calendar events`; --on <day> looks around another day)", ref, eventSearchDays, day)
	}
	return hit, nil
}

// connectionOf is the connection an event can be changed through.
func connectionOf(e *calEvent) (string, error) {
	if e.ConnectionID == "" || e.Access == nil {
		return "", &exitError{code: ExitNotPermitted, jsonCode: "calendar_read_only", msg: fmt.Sprintf("%q is not in a connected calendar (a calendar feed is read-only)", e.name())}
	}
	return e.ConnectionID, nil
}

func eventPath(connID, eventID string) string {
	return "/calendar-sync/connections/" + url.PathEscape(connID) + "/events/" + url.PathEscape(eventID)
}

func calendarEventEdit(a *App, ctx context.Context, g *globalFlags, args []string) error {
	fs := a.flagSet("calendar event edit", g)
	title := fs.String("title", "", "new title")
	start := fs.String("start", "", "new start: DATE or DATE[Thh:mm]; DATE is YYYY-MM-DD, today, tomorrow or a weekday")
	end := fs.String("end", "", "new end, same forms (for an all-day event the day after its last day)")
	allDay := fs.Bool("all-day", false, "make the event an all-day event")
	on := fs.String("on", "", "a day near the event, when it is more than 45 days from today")
	loc := fs.String("location", "", "new location (\"\" clears it)")
	desc := fs.String("description", "", "new description, plain text; it replaces the whole text (\"\" clears it)")
	free := fs.Bool("free", false, "show you as free during the event")
	busy := fs.Bool("busy", false, "show you as busy during the event")
	pos, err := a.parse(g, fs, "calendar event edit", args)
	if err != nil {
		return a.helpOr("calendar", err)
	}
	given := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { given[f.Name] = true })
	changes := *title != "" || *start != "" || *end != "" || *allDay || given["location"] || given["description"] || *free || *busy
	if len(pos) != 1 || !changes {
		return usageErr("usage: ot calendar event edit <id> [--title <t>] [--start DATE[Thh:mm]] [--end DATE[Thh:mm]] [--all-day] [--location <t>] [--description <t>] [--free | --busy]")
	}
	if *free && *busy {
		return usageErr("give --free or --busy, not both")
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
	body := map[string]any{"rev": ev.Rev}
	if *title != "" {
		body["summary"] = *title
	}
	if *start != "" || *end != "" || *allDay {
		s, e, kind, err := a.eventTimes(ev, *start, *end, *allDay)
		if err != nil {
			return err
		}
		body["start"], body["end"], body["allDay"] = s, e, kind
	}
	if given["location"] {
		body["location"] = *loc
	}
	if given["description"] {
		body["description"] = *desc
	}
	if *free || *busy {
		body["free"] = *free
	}
	a.target(pos[0], ev.ID, ev.name(), "updated")
	var res struct {
		Event json.RawMessage `json:"event"`
	}
	if err := c.patchJSON(ctx, eventPath(connID, ev.ID), body, &res); err != nil {
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
	fmt.Fprintf(a.Stdout, "%s  %s  %s %s\n", shortID(out.ID), out.name(), out.Start.Date, out.timeText())
	return nil
}

func calendarEventDelete(a *App, ctx context.Context, g *globalFlags, args []string) error {
	fs := a.flagSet("calendar event delete", g)
	yes := fs.Bool("yes", false, "do not ask for confirmation")
	fs.BoolVar(yes, "y", false, "do not ask for confirmation")
	on := fs.String("on", "", "a day near the event, when it is more than 45 days from today")
	pos, err := a.parse(g, fs, "calendar event delete", args)
	if err != nil {
		return a.helpOr("calendar", err)
	}
	if len(pos) != 1 {
		return usageErr("usage: ot calendar event delete <id> [--yes]")
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
	a.target(pos[0], ev.ID, ev.name(), "deleted")
	if !*yes && !a.dryRun() {
		ok, err := a.confirm(fmt.Sprintf("Delete %q on %s from your calendar?", ev.name(), ev.Start.Date))
		if err != nil {
			return err
		}
		if !ok {
			return usageErr("aborted")
		}
	}
	if err := c.delete(ctx, eventPath(connID, ev.ID)+"?rev="+url.QueryEscape(ev.Rev)); err != nil {
		return err
	}
	if g.json {
		return a.printJSON(ev.Raw)
	}
	fmt.Fprintf(a.Stdout, "deleted  %s  %s\n", shortID(ev.ID), ev.name())
	return nil
}
