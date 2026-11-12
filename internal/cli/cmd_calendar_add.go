package cli

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// `ot calendar event add` (OpenSpec change calendar-event-editor, design D13):
// one new event through POST /api/v1/calendar-sync/connections/{id}/events.
// The calendar comes from the connections the user shows: --calendar names it
// (a name or a url), a single writable calendar needs no flag. Every run makes
// a fresh client id, so a retry by hand never doubles an event; --client-id
// is for scripts that retry.

// writableCalendar is a shown calendar events can be created in.
type writableCalendar struct {
	ConnectionID, ConnectionName string
	URL, Name                    string
}

func (w writableCalendar) label() string {
	if w.ConnectionName == "" || w.ConnectionName == w.Name {
		return w.Name
	}
	return w.ConnectionName + " / " + w.Name
}

// writableCalendars lists the shown calendars of the user's connections that
// allow creating events.
func (c *client) writableCalendars(ctx context.Context) ([]writableCalendar, error) {
	var res struct {
		Connections []struct {
			ID        string   `json:"id"`
			Name      string   `json:"name"`
			Target    string   `json:"target"`
			Read      []string `json:"read"`
			Calendars []struct {
				URL  string `json:"url"`
				Name string `json:"name"`
				Caps *struct {
					Create bool `json:"create"`
				} `json:"caps"`
				ReadOnly bool `json:"read_only"`
			} `json:"calendars"`
		} `json:"connections"`
	}
	if err := c.getJSON(ctx, "/calendar-sync/connections", &res); err != nil {
		return nil, err
	}
	var out []writableCalendar
	for _, conn := range res.Connections {
		shown := map[string]bool{conn.Target: true}
		for _, u := range conn.Read {
			shown[u] = true
		}
		for _, cal := range conn.Calendars {
			create := !cal.ReadOnly
			if cal.Caps != nil {
				create = cal.Caps.Create
			}
			if shown[cal.URL] && create {
				out = append(out, writableCalendar{ConnectionID: conn.ID, ConnectionName: conn.Name, URL: cal.URL, Name: cal.Name})
			}
		}
	}
	return out, nil
}

// pickCalendar resolves --calendar among the writable calendars: an exact url
// or name (any case) first, then a part of a name. Anything but one match is
// a usage error that lists the choices.
func pickCalendar(list []writableCalendar, want string) (writableCalendar, error) {
	if len(list) == 0 {
		return writableCalendar{}, usageErr("no calendar of yours allows creating events: connect one in Settings, Calendar sync, and show it")
	}
	hits := list
	if want = strings.TrimSpace(want); want != "" {
		hits = nil
		for _, pass := range []func(writableCalendar) bool{
			func(w writableCalendar) bool { return w.URL == want || strings.EqualFold(w.Name, want) },
			func(w writableCalendar) bool {
				return strings.Contains(strings.ToLower(w.label()), strings.ToLower(want))
			},
		} {
			for _, w := range list {
				if pass(w) {
					hits = append(hits, w)
				}
			}
			if len(hits) > 0 {
				break
			}
		}
	}
	if len(hits) == 1 {
		return hits[0], nil
	}
	var b strings.Builder
	for _, w := range list {
		fmt.Fprintf(&b, "\n  %s  (%s)", w.label(), w.URL)
	}
	if len(hits) == 0 {
		return writableCalendar{}, usageErr("no writable calendar matches %q; choose one with --calendar:%s", want, b.String())
	}
	return writableCalendar{}, usageErr("more than one calendar can take the event; choose one with --calendar <name or url>:%s", b.String())
}

// newEventTimes builds the start, end and allDay of the POST body.
func (a *App) newEventTimes(startF, endF, forF string, allDay bool) (map[string]any, map[string]any, bool, error) {
	sd, sc, err := a.parseWhenFlag("start", startF)
	if err != nil {
		return nil, nil, false, err
	}
	ed, ec, err := a.parseWhenFlag("end", endF)
	if err != nil {
		return nil, nil, false, err
	}
	if sd == "" {
		return nil, nil, false, usageErr("--start is required: DATE or DATEThh:mm")
	}
	if endF != "" && forF != "" {
		return nil, nil, false, usageErr("give --end or --for, not both")
	}
	if allDay {
		return newAllDay(sd, sc, ed, ec, forF)
	}
	if sc == "" {
		return nil, nil, false, usageErr("--start needs a time (DATEThh:mm), or pass --all-day")
	}
	s := wall(sd, sc)
	var e time.Time
	switch {
	case forF != "":
		d, err := time.ParseDuration(forF)
		if err != nil || d <= 0 {
			return nil, nil, false, usageErr("--for: %q is not a length such as 45m or 1h30m", forF)
		}
		e = s.Add(d)
	case endF != "":
		if ec == "" {
			return nil, nil, false, usageErr("--end needs a time (DATEThh:mm)")
		}
		e = wall(ed, ec)
	default:
		return nil, nil, false, usageErr("give --end DATEThh:mm or --for <length> (for example --for 45m)")
	}
	if !e.After(s) {
		return nil, nil, false, usageErr("the end must be after the start")
	}
	tz := a.localZone()
	return pointOf(s, &tz, false), pointOf(e, &tz, false), false, nil
}

func newAllDay(sd, sc, ed, ec, forF string) (map[string]any, map[string]any, bool, error) {
	if sc != "" || ec != "" {
		return nil, nil, false, usageErr("--all-day takes dates only: drop the times from --start and --end")
	}
	if forF != "" {
		return nil, nil, false, usageErr("--for is for timed events; an all-day event takes --end, the day after its last day")
	}
	s := wall(sd, "")
	e := s.AddDate(0, 0, 1)
	if ed != "" {
		e = wall(ed, "")
	}
	if !e.After(s) {
		return nil, nil, false, usageErr("the end must be after the start")
	}
	return pointOf(s, nil, true), pointOf(e, nil, true), true, nil
}

func newClientID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6], b[8] = b[6]&0x0f|0x40, b[8]&0x3f|0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

func calendarEventAdd(a *App, ctx context.Context, g *globalFlags, args []string) error {
	fs := a.flagSet("calendar event add", g)
	start := fs.String("start", "", "start: DATE or DATE[Thh:mm]; DATE is YYYY-MM-DD, today, tomorrow or a weekday")
	end := fs.String("end", "", "end, same forms (for an all-day event the day after its last day)")
	dur := fs.String("for", "", "length instead of --end, such as 45m or 1h30m")
	allDay := fs.Bool("all-day", false, "an all-day event")
	cal := fs.String("calendar", "", "the calendar to add it to: a name or a url (not needed with one writable calendar)")
	loc := fs.String("location", "", "where it is")
	desc := fs.String("description", "", "its description, plain text")
	free := fs.Bool("free", false, "show you as free during it")
	clientID := fs.String("client-id", "", "idempotency key for scripts that retry: 8 to 64 letters, digits, - or _")
	pos, err := a.parse(g, fs, "calendar event add", args)
	if err != nil {
		return a.helpOr("calendar", err)
	}
	title := strings.TrimSpace(strings.Join(pos, " "))
	if title == "" {
		return usageErr("usage: ot calendar event add <title> --start DATE[Thh:mm] [--end DATE[Thh:mm] | --for 45m] [--all-day] [--calendar <name or url>] [--location T] [--description T] [--free]")
	}
	s, e, kind, err := a.newEventTimes(*start, *end, *dur, *allDay)
	if err != nil {
		return err
	}
	c, _, err := a.connect(g)
	if err != nil {
		return err
	}
	cals, err := c.writableCalendars(ctx)
	if err != nil {
		return err
	}
	target, err := pickCalendar(cals, *cal)
	if err != nil {
		return err
	}
	id := *clientID
	if id == "" {
		id = newClientID()
	}
	body := map[string]any{"clientId": id, "calendarUrl": target.URL, "summary": title, "start": s, "end": e, "allDay": kind, "free": *free}
	if *loc != "" {
		body["location"] = *loc
	}
	if *desc != "" {
		body["description"] = *desc
	}
	a.target(title, "", title, "created in "+target.label())
	var res struct {
		Event json.RawMessage `json:"event"`
	}
	if err := c.postJSON(ctx, "/calendar-sync/connections/"+url.PathEscape(target.ConnectionID)+"/events", body, &res); err != nil {
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
