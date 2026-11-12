package calsync

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/OpenTodoApp/opentodo/internal/calfeed"
	"github.com/OpenTodoApp/opentodo/internal/calsync/calsynctest"
	"github.com/OpenTodoApp/opentodo/internal/zoned"
)

// The DAV event writer against the in-process CalDAV server (OpenSpec change
// calendar-event-edits, task 2.2).

// davRef is the ref the shown calendar holds for an event uid.
func (h *harness) davRef(c *Connection, calURL, uid string) EventRef {
	h.t.Helper()
	r, ok := h.refsOf(c, calURL)[uid]
	if !ok {
		h.t.Fatalf("no ref for %s", uid)
	}
	return EventRef{Calendar: calURL, Href: r.Href, ETag: r.ETag}
}

func (h *harness) writer(c *Connection) EventWriter {
	h.t.Helper()
	w, err := h.w.EventWriter(h.ctx, h.conn(c.ID))
	if err != nil {
		h.t.Fatal(err)
	}
	return w
}

func zonedPoint(t *testing.T, date, clock, tz string) calfeed.Point {
	t.Helper()
	loc, err := zoned.Load(tz)
	if err != nil {
		t.Fatal(err)
	}
	at, err := zoned.Instant(date, clock, loc)
	if err != nil {
		t.Fatal(err)
	}
	if tz == "UTC" {
		return calfeed.Point{T: at.UTC(), Zone: "UTC"}
	}
	return calfeed.Point{T: at.In(loc), Zone: tz}
}

func floatingPoint(date, clock string) calfeed.Point {
	t, _ := time.Parse("2006-01-02 15:04", date+" "+clock)
	return calfeed.Point{T: t}
}

func datePoint(date string) calfeed.Point {
	t, _ := time.Parse("2006-01-02", date)
	return calfeed.Point{T: t, AllDay: true}
}

// richEvent has properties an edit must not lose.
const richEvent = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//Fake//EN\r\nX-WR-CALNAME:Work\r\n" +
	"BEGIN:VEVENT\r\nUID:rich\r\nDTSTAMP:20261001T080000Z\r\nSEQUENCE:4\r\nSUMMARY;LANGUAGE=en:Standup\r\n" +
	"DTSTART:20261021T100000Z\r\nDTEND:20261021T103000Z\r\nLOCATION:Room 4\r\nDESCRIPTION:Daily\\, short\r\nX-CUSTOM;X-P=1:keep me\r\n" +
	"BEGIN:VALARM\r\nACTION:DISPLAY\r\nTRIGGER:-PT10M\r\nDESCRIPTION:Reminder\r\nEND:VALARM\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"

func setupDAV(t *testing.T, data string) (*harness, *Connection, EventWriter, EventRef) {
	t.Helper()
	h := newHarness(t)
	h.srv.Put("work", "rich.ics", data)
	c := h.connect(false)
	h.cycle(c)
	cal := h.srv.CalendarURL("work")
	return h, c, h.writer(c), h.davRef(c, cal, "rich")
}

func TestDAVUpdateMoveKeepsUnknownProperties(t *testing.T) {
	h, _, w, ref := setupDAV(t, richEvent)
	when := &EventWhen{Start: zonedPoint(t, "2026-10-22", "14:00", "UTC"), End: zonedPoint(t, "2026-10-22", "14:45", "UTC")}
	title := "Standup (moved)"
	st, err := w.Update(h.ctx, ref, EventEdit{Summary: &title, When: when})
	if err != nil {
		t.Fatal(err)
	}
	if st.Ref.ETag == "" || st.Ref.ETag == ref.ETag || st.Event.Summary != title || !st.Event.Start.T.Equal(when.Start.T) || st.Locked != "" {
		t.Fatalf("state = %+v", st)
	}
	got := h.srv.Get("work", "rich.ics")
	for _, want := range []string{"SUMMARY;LANGUAGE=en:Standup (moved)", "DTSTART:20261022T140000Z", "DTEND:20261022T144500Z", "SEQUENCE:5",
		"LOCATION:Room 4", "DESCRIPTION:Daily\\, short", "X-CUSTOM;X-P=1:keep me", "BEGIN:VALARM", "TRIGGER:-PT10M", "X-WR-CALNAME:Work", "UID:rich",
		"DTSTAMP:20261020T120000Z", "LAST-MODIFIED:20261020T120000Z"} {
		if !strings.Contains(got, want) {
			t.Errorf("object lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "20261021T100000Z") || strings.Contains(got, "DTSTAMP:20261001") {
		t.Errorf("old times or stamp survived:\n%s", got)
	}
	if h.srv.Unconditional() != 0 {
		t.Fatalf("%d writes without a precondition", h.srv.Unconditional())
	}
}

func TestDAVUpdateRenameAndResizeOnly(t *testing.T) {
	h, _, w, ref := setupDAV(t, richEvent)
	title := "Only the title"
	st, err := w.Update(h.ctx, ref, EventEdit{Summary: &title})
	if err != nil {
		t.Fatal(err)
	}
	got := h.srv.Get("work", "rich.ics")
	if !strings.Contains(got, "DTSTART:20261021T100000Z") || !strings.Contains(got, "DTEND:20261021T103000Z") || !strings.Contains(got, ":Only the title") {
		t.Fatalf("rename changed times:\n%s", got)
	}
	// A resize: the same start, a later end.
	ref.ETag = st.Ref.ETag
	when := &EventWhen{Start: zonedPoint(t, "2026-10-21", "10:00", "UTC"), End: zonedPoint(t, "2026-10-21", "11:30", "UTC")}
	if _, err := w.Update(h.ctx, ref, EventEdit{When: when}); err != nil {
		t.Fatal(err)
	}
	got = h.srv.Get("work", "rich.ics")
	if !strings.Contains(got, "DTEND:20261021T113000Z") || !strings.Contains(got, ":Only the title") || !strings.Contains(got, "SEQUENCE:6") {
		t.Fatalf("resize:\n%s", got)
	}
}

func TestDAVUpdateAllDayAndTimedBothWays(t *testing.T) {
	h, _, w, ref := setupDAV(t, richEvent)
	st, err := w.Update(h.ctx, ref, EventEdit{When: &EventWhen{Start: datePoint("2026-10-23"), End: datePoint("2026-10-24"), AllDay: true}})
	if err != nil {
		t.Fatal(err)
	}
	got := h.srv.Get("work", "rich.ics")
	if !strings.Contains(got, "DTSTART;VALUE=DATE:20261023") || !strings.Contains(got, "DTEND;VALUE=DATE:20261024") || !st.Event.Start.AllDay {
		t.Fatalf("all-day:\n%s", got)
	}
	ref.ETag = st.Ref.ETag
	if _, err := w.Update(h.ctx, ref, EventEdit{When: &EventWhen{Start: zonedPoint(t, "2026-10-23", "09:00", "UTC"), End: zonedPoint(t, "2026-10-23", "10:00", "UTC")}}); err != nil {
		t.Fatal(err)
	}
	got = h.srv.Get("work", "rich.ics")
	if !strings.Contains(got, "DTSTART:20261023T090000Z") || strings.Contains(got, "VALUE=DATE") {
		t.Fatalf("timed again:\n%s", got)
	}
}

func TestDAVFloatingStaysFloatingAndNewZoneGetsVTimezone(t *testing.T) {
	floating := strings.Replace(strings.Replace(richEvent, "DTSTART:20261021T100000Z", "DTSTART:20261021T100000", 1), "DTEND:20261021T103000Z", "DTEND:20261021T103000", 1)
	h, _, w, ref := setupDAV(t, floating)
	st, err := w.Update(h.ctx, ref, EventEdit{When: &EventWhen{Start: floatingPoint("2026-10-22", "08:15"), End: floatingPoint("2026-10-22", "08:45")}})
	if err != nil {
		t.Fatal(err)
	}
	got := h.srv.Get("work", "rich.ics")
	if !strings.Contains(got, "DTSTART:20261022T081500\r\n") || strings.Contains(got, "TZID") || st.Event.Start.Zone != "" {
		t.Fatalf("floating:\n%s", got)
	}
	ref.ETag = st.Ref.ETag
	if _, err := w.Update(h.ctx, ref, EventEdit{When: &EventWhen{Start: zonedPoint(t, "2026-10-22", "08:15", "Europe/Berlin"), End: zonedPoint(t, "2026-10-22", "08:45", "Europe/Berlin")}}); err != nil {
		t.Fatal(err)
	}
	got = h.srv.Get("work", "rich.ics")
	if !strings.Contains(got, "DTSTART;TZID=Europe/Berlin:20261022T081500") || !strings.Contains(got, "BEGIN:VTIMEZONE") || !strings.Contains(got, "TZID:Europe/Berlin") {
		t.Fatalf("zoned:\n%s", got)
	}
}

func TestDAVZonedKeepsTheEventsOwnTZID(t *testing.T) {
	data := strings.Replace(strings.Replace(richEvent, "DTSTART:20261021T100000Z", "DTSTART;TZID=W. Europe Standard Time:20261021T120000", 1),
		"DTEND:20261021T103000Z", "DTEND;TZID=W. Europe Standard Time:20261021T123000", 1)
	h, _, w, ref := setupDAV(t, data)
	if _, err := w.Update(h.ctx, ref, EventEdit{When: &EventWhen{Start: zonedPoint(t, "2026-10-21", "13:00", "Europe/Berlin"), End: zonedPoint(t, "2026-10-21", "13:30", "Europe/Berlin")}}); err != nil {
		t.Fatal(err)
	}
	got := h.srv.Get("work", "rich.ics")
	if !strings.Contains(got, "DTSTART;TZID=W. Europe Standard Time:20261021T130000") {
		t.Fatalf("the Windows TZID was rewritten:\n%s", got)
	}
}

func TestDAVDurationBecomesDTEND(t *testing.T) {
	data := strings.Replace(richEvent, "DTEND:20261021T103000Z", "DURATION:PT30M", 1)
	h, _, w, ref := setupDAV(t, data)
	if _, err := w.Update(h.ctx, ref, EventEdit{When: &EventWhen{Start: zonedPoint(t, "2026-10-21", "10:00", "UTC"), End: zonedPoint(t, "2026-10-21", "12:00", "UTC")}}); err != nil {
		t.Fatal(err)
	}
	got := h.srv.Get("work", "rich.ics")
	if strings.Contains(got, "DURATION") || !strings.Contains(got, "DTEND:20261021T120000Z") {
		t.Fatalf("duration:\n%s", got)
	}
}

func TestDAVConflictsAndRefusals(t *testing.T) {
	h, c, w, ref := setupDAV(t, richEvent)
	title := "x"
	edit := EventEdit{Summary: &title}
	// Changed in the calendar app since it was read: 412 semantics, nothing sent.
	h.srv.Put("work", "rich.ics", strings.Replace(richEvent, "Standup", "Daily standup", 1))
	puts := h.srv.Count("PUT")
	if _, err := w.Update(h.ctx, ref, edit); !errors.Is(err, ErrEventChanged) {
		t.Fatalf("changed = %v", err)
	}
	if err := w.Delete(h.ctx, ref); !errors.Is(err, ErrEventChanged) {
		t.Fatalf("delete of a changed event = %v", err)
	}
	if h.srv.Count("PUT") != puts || !strings.Contains(h.srv.Get("work", "rich.ics"), "Daily standup") {
		t.Fatal("a refused change was written")
	}
	// Deleted in the calendar app: gone.
	h.srv.Delete("work", "rich.ics")
	if _, err := w.Update(h.ctx, ref, edit); !errors.Is(err, ErrEventGone) {
		t.Fatalf("gone = %v", err)
	}
	if err := w.Delete(h.ctx, ref); !errors.Is(err, ErrEventGone) {
		t.Fatalf("delete of a gone event = %v", err)
	}
	// An empty ETag never writes.
	if _, err := w.Update(h.ctx, EventRef{Calendar: ref.Calendar, Href: ref.Href}, edit); !errors.Is(err, ErrEventLocked) || lockReason(err) != LockNoETag {
		t.Fatalf("no etag = %v", err)
	}
	if h.srv.Unconditional() != 0 {
		t.Fatalf("%d writes without a precondition", h.srv.Unconditional())
	}
	_ = c
}

func TestDAVReadOnlyCalendarIsRefused(t *testing.T) {
	h := newHarness(t)
	h.srv.AddCalendar("holidays", &calsynctest.Calendar{Name: "Holidays", ReadOnly: true})
	h.srv.SetPrivileges("holidays", "write") // the privilege set claims more than the server allows
	h.srv.Put("holidays", "h.ics", strings.Replace(richEvent, "UID:rich", "UID:hol", 1))
	c := h.connect(false)
	reads := []string{h.srv.CalendarURL("holidays")}
	if _, err := h.w.Store.Update(h.ctx, "alice", c.ID, Patch{ReadURLs: &reads}); err != nil {
		t.Fatal(err)
	}
	h.cycle(h.conn(c.ID))
	ref := h.davRef(c, h.srv.CalendarURL("holidays"), "hol")
	w := h.writer(c)
	title := "x"
	if _, err := w.Update(h.ctx, ref, EventEdit{Summary: &title}); !errors.Is(err, ErrCalendarRO) {
		t.Fatalf("update = %v", err)
	}
	if err := w.Delete(h.ctx, ref); !errors.Is(err, ErrCalendarRO) {
		t.Fatalf("delete = %v", err)
	}
}

func TestDAVLockedObjectsAreRefused(t *testing.T) {
	for name, tc := range map[string]struct{ extra, reason string }{
		"guests":    {"ATTENDEE;CN=A:mailto:a@example.org\r\n", LockGuests},
		"recurring": {"RRULE:FREQ=DAILY;COUNT=3\r\n", LockRecurring},
		"exdate":    {"EXDATE:20261022T100000Z\r\n", LockRecurring},
	} {
		t.Run(name, func(t *testing.T) {
			h, _, w, ref := setupDAV(t, strings.Replace(richEvent, "BEGIN:VALARM", tc.extra+"BEGIN:VALARM", 1))
			title := "x"
			before := h.srv.Get("work", "rich.ics")
			if _, err := w.Update(h.ctx, ref, EventEdit{Summary: &title}); !errors.Is(err, ErrEventLocked) || lockReason(err) != tc.reason {
				t.Fatalf("update = %v", err)
			}
			if h.srv.Get("work", "rich.ics") != before || h.srv.Count("PUT") != 0 {
				t.Fatal("a locked object was written")
			}
		})
	}
	// An object holding an override is locked too.
	data := strings.Replace(richEvent, "END:VCALENDAR", "BEGIN:VEVENT\r\nUID:rich\r\nRECURRENCE-ID:20261022T100000Z\r\nDTSTART:20261022T110000Z\r\nSUMMARY:Override\r\nEND:VEVENT\r\nEND:VCALENDAR", 1)
	h, _, w, ref := setupDAV(t, data)
	title := "x"
	if _, err := w.Update(h.ctx, ref, EventEdit{Summary: &title}); !errors.Is(err, ErrEventLocked) {
		t.Fatalf("override object = %v", err)
	}
}

func TestDAVDeleteAndRead(t *testing.T) {
	h, _, w, ref := setupDAV(t, richEvent)
	st, err := w.Read(h.ctx, ref)
	if err != nil || st.Ref.ETag != ref.ETag || st.Event.Summary != "Standup" || st.Locked != "" {
		t.Fatalf("read = %+v %v", st, err)
	}
	if err := w.Delete(h.ctx, ref); err != nil {
		t.Fatal(err)
	}
	if got := h.srv.Get("work", "rich.ics"); got != "" {
		t.Fatalf("object survived: %s", got)
	}
	if h.srv.Unconditional() != 0 {
		t.Fatalf("%d deletes without If-Match", h.srv.Unconditional())
	}
}

func TestDAVServerWithoutETagOnPutIsReadAgain(t *testing.T) {
	h := newHarness(t)
	h.srv.NoETagOnPut = true
	h.srv.Put("work", "rich.ics", richEvent)
	c := h.connect(false)
	h.cycle(c)
	ref := h.davRef(c, h.srv.CalendarURL("work"), "rich")
	title := "x"
	st, err := h.writer(c).Update(h.ctx, ref, EventEdit{Summary: &title})
	if err != nil || st.Ref.ETag == "" || st.Ref.ETag == ref.ETag {
		t.Fatalf("state = %+v %v", st, err)
	}
}

func TestRefusalMapping(t *testing.T) {
	for status, want := range map[int]error{200: nil, 204: nil, 404: ErrEventGone, 412: ErrEventChanged, 403: ErrCalendarRO} {
		if got := refusal(status); !errors.Is(got, want) && !(got == nil && want == nil) {
			t.Errorf("refusal(%d) = %v, want %v", status, got, want)
		}
	}
	if got := refusal(401); reasonOf(got) != ReasonUnauthorized {
		t.Errorf("401 = %v", got)
	}
	if got := refusal(500); reasonOf(got) != "http_500" {
		t.Errorf("500 = %v", got)
	}
}
