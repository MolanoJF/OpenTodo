package calsync

import (
	"errors"
	"strings"
	"testing"

	"github.com/OpenTodoApp/opentodo/internal/calfeed"
	"github.com/OpenTodoApp/opentodo/internal/calsync/calsynctest"
)

// Creating events in a CalDAV calendar and reading descriptions
// (calendar-event-editor task 2.2).

func davHarness(t *testing.T) (*harness, EventWriter, string) {
	t.Helper()
	h := newHarness(t)
	c := h.connect(false)
	return h, h.writer(c), h.srv.CalendarURL("work")
}

func TestDAVCreateRoundTrips(t *testing.T) {
	cases := []struct {
		name string
		when EventWhen
		want []string
	}{
		{"zoned", EventWhen{Start: zonedPoint(t, "2026-11-13", "14:00", "Europe/Lisbon"), End: zonedPoint(t, "2026-11-13", "14:30", "Europe/Lisbon")},
			[]string{"BEGIN:VTIMEZONE", "TZID:Europe/Lisbon", "DTSTART;TZID=Europe/Lisbon:20261113T140000", "DTEND;TZID=Europe/Lisbon:20261113T143000"}},
		{"floating", EventWhen{Start: floatingPoint("2026-11-13", "14:00"), End: floatingPoint("2026-11-13", "15:00")},
			[]string{"DTSTART:20261113T140000\r\n", "DTEND:20261113T150000\r\n"}},
		{"allday", EventWhen{Start: datePoint("2026-11-13"), End: datePoint("2026-11-14"), AllDay: true},
			[]string{"DTSTART;VALUE=DATE:20261113", "DTEND;VALUE=DATE:20261114"}},
		{"utc", EventWhen{Start: zonedPoint(t, "2026-11-13", "14:00", "UTC"), End: zonedPoint(t, "2026-11-13", "15:00", "UTC")},
			[]string{"DTSTART:20261113T140000Z", "DTEND:20261113T150000Z"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, w, cal := davHarness(t)
			ev := EventNew{UID: "client-" + tc.name, Summary: "Call, Ana; ok", When: tc.when, Location: "Cafe Central", Description: "Bring the draft.\nTwo lines"}
			st, err := w.Create(h.ctx, cal, ev)
			if err != nil {
				t.Fatal(err)
			}
			if st.Ref.Href != cal+ev.UID+".ics" || st.Ref.ETag == "" || st.Locked != "" || st.Event.UID != ev.UID {
				t.Fatalf("state = %+v", st)
			}
			obj := h.srv.Get("work", ev.UID+".ics")
			for _, want := range append(tc.want, "UID:"+ev.UID, "SEQUENCE:0", "TRANSP:OPAQUE", "SUMMARY:Call\\, Ana\\; ok", "DESCRIPTION:Bring the draft.\\nTwo lines") {
				if !strings.Contains(obj, want) {
					t.Errorf("object lacks %q:\n%s", want, obj)
				}
			}
			for _, bad := range []string{"VALARM", "ATTENDEE", "ORGANIZER"} {
				if strings.Contains(obj, bad) {
					t.Errorf("object has %s", bad)
				}
			}
			if got := h.srv.IfNoneMatches(); len(got) != 1 || got[0] != "*" {
				t.Fatalf("If-None-Match = %q", got)
			}
			// It parses back to the same event.
			cp, err := calfeed.ParseCalendar([]byte(obj))
			if err != nil || len(cp.Events) != 1 {
				t.Fatal(err)
			}
			got := cp.Events[0]
			if got.Summary != ev.Summary || got.Location != ev.Location || got.Start.AllDay != tc.when.AllDay ||
				got.Start.Zone != tc.when.Start.Zone || !got.Start.T.Equal(tc.when.Start.T) {
				t.Fatalf("parsed = %+v, want %+v", got, tc.when)
			}
			// And the detail has the description.
			d, err := w.Detail(h.ctx, st.Ref)
			if err != nil || d.Description != ev.Description || d.DescriptionLocked {
				t.Fatalf("detail = %q locked %v %v", d.Description, d.DescriptionLocked, err)
			}
		})
	}
}

func TestDAVCreateFreeAndEmptyFields(t *testing.T) {
	h, w, cal := davHarness(t)
	_, err := w.Create(h.ctx, cal, EventNew{UID: "client-free1", Summary: "Free", Free: true,
		When: EventWhen{Start: floatingPoint("2026-11-13", "14:00"), End: floatingPoint("2026-11-13", "15:00")}})
	if err != nil {
		t.Fatal(err)
	}
	obj := h.srv.Get("work", "client-free1.ics")
	if !strings.Contains(obj, "TRANSP:TRANSPARENT") || strings.Contains(obj, "LOCATION") || strings.Contains(obj, "DESCRIPTION") {
		t.Fatalf("object:\n%s", obj)
	}
}

func TestDAVCreateReplayAndHrefTaken(t *testing.T) {
	h, w, cal := davHarness(t)
	when := EventWhen{Start: floatingPoint("2026-11-13", "14:00"), End: floatingPoint("2026-11-13", "15:00")}
	ev := EventNew{UID: "client-same1", Summary: "Once", When: when}
	first, err := w.Create(h.ctx, cal, ev)
	if err != nil {
		t.Fatal(err)
	}
	again, err := w.Create(h.ctx, cal, ev)
	if !errors.Is(err, ErrEventExists) || again.Ref.ETag != first.Ref.ETag || again.Event.Summary != "Once" {
		t.Fatalf("replay = %+v %v", again, err)
	}
	if len(h.srv.Members("work")) != 1 {
		t.Fatalf("members = %v", h.srv.Members("work"))
	}
	// Another UID at the address.
	h.srv.Put("work", "client-other.ics", "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VEVENT\r\nUID:someone-else\r\nDTSTART:20261113T100000Z\r\nDTEND:20261113T110000Z\r\nSUMMARY:X\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n")
	_, err = w.Create(h.ctx, cal, EventNew{UID: "client-other", Summary: "Mine", When: when})
	var e *Error
	if !errors.As(err, &e) || e.Reason != ReasonHrefTaken {
		t.Fatalf("different uid = %v", err)
	}
}

func TestDAVCreateRefusals(t *testing.T) {
	h, w, cal := davHarness(t)
	when := EventWhen{Start: floatingPoint("2026-11-13", "14:00"), End: floatingPoint("2026-11-13", "15:00")}
	h.srv.AddCalendar("ro", &calsynctest.Calendar{Name: "RO", ReadOnly: true})
	if _, err := w.Create(h.ctx, h.srv.CalendarURL("ro"), EventNew{UID: "client-ro01", Summary: "x", When: when}); !errors.Is(err, ErrCalendarRO) {
		t.Fatalf("read-only = %v", err)
	}
	h.srv.RemoveCalendar("work")
	_, err := w.Create(h.ctx, cal, EventNew{UID: "client-gone", Summary: "x", When: when})
	var e *Error
	if !errors.As(err, &e) || e.Reason != ReasonCalendarMissing {
		t.Fatalf("missing calendar = %v", err)
	}
}

func TestDAVCreateWithoutETagIsReadOnly(t *testing.T) {
	h, w, cal := davHarness(t)
	when := EventWhen{Start: floatingPoint("2026-11-13", "14:00"), End: floatingPoint("2026-11-13", "15:00")}
	h.srv.NoETagOnPut, h.srv.NoETagOnGet = true, true
	st, err := w.Create(h.ctx, cal, EventNew{UID: "client-noet", Summary: "x", When: when})
	if err != nil || st.Ref.ETag != "" || st.Locked != LockNoETag {
		t.Fatalf("state = %+v %v", st, err)
	}
	// With only the PUT answer lacking it, one GET supplies it.
	h.srv.NoETagOnGet = false
	st, err = w.Create(h.ctx, cal, EventNew{UID: "client-getet", Summary: "x", When: when})
	if err != nil || st.Ref.ETag == "" || st.Locked != "" {
		t.Fatalf("state after GET = %+v %v", st, err)
	}
}

const alarmEvent = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//Fake//EN\r\n" +
	"BEGIN:VEVENT\r\nUID:rich\r\nDTSTAMP:20261001T080000Z\r\nSEQUENCE:1\r\nSUMMARY:Standup\r\nDTSTART:20261021T100000Z\r\nDTEND:20261021T103000Z\r\n" +
	"LOCATION;X-P=1:Room 4\r\nDESCRIPTION;LANGUAGE=en:Daily\r\nX-ALT-DESC;FMTTYPE=text/html:<b>Daily</b>\r\nTRANSP:OPAQUE\r\nX-CUSTOM:keep me\r\n" +
	"BEGIN:VALARM\r\nACTION:DISPLAY\r\nTRIGGER:-PT10M\r\nDESCRIPTION:Reminder\r\nEND:VALARM\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"

func TestDAVUpdateNewFieldsKeepTheRest(t *testing.T) {
	h, _, w, ref := setupDAV(t, alarmEvent)
	loc, desc, free := "Room 7", "New text, with comma", true
	st, err := w.Update(h.ctx, ref, EventEdit{Location: &loc, Description: &desc, Free: &free})
	if err != nil {
		t.Fatal(err)
	}
	got := h.srv.Get("work", "rich.ics")
	for _, want := range []string{"LOCATION;X-P=1:Room 7", "DESCRIPTION;LANGUAGE=en:New text\\, with comma", "TRANSP:TRANSPARENT", "X-CUSTOM:keep me", "BEGIN:VALARM", "TRIGGER:-PT10M"} {
		if !strings.Contains(got, want) {
			t.Errorf("object lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "X-ALT-DESC") || strings.Contains(got, "TRANSP:OPAQUE") {
		t.Errorf("stale properties survived:\n%s", got)
	}
	if st.Event.Location != "Room 7" || !st.Event.Free {
		t.Fatalf("state = %+v", st.Event)
	}
	// An empty value deletes; busy sets OPAQUE.
	ref.ETag = st.Ref.ETag
	empty, busy := "", false
	if _, err := w.Update(h.ctx, ref, EventEdit{Location: &empty, Description: &empty, Free: &busy}); err != nil {
		t.Fatal(err)
	}
	got = h.srv.Get("work", "rich.ics")
	if strings.Contains(got, "LOCATION") || strings.Contains(got, "New text") || !strings.Contains(got, "TRANSP:OPAQUE") {
		t.Fatalf("after clearing:\n%s", got)
	}
	if !strings.Contains(got, "DESCRIPTION:Reminder") { // the alarm keeps its own DESCRIPTION
		t.Fatalf("alarm description lost:\n%s", got)
	}
}

func TestDAVUpdateTitleLeavesAltDesc(t *testing.T) {
	h, _, w, ref := setupDAV(t, alarmEvent)
	title := "Renamed"
	if _, err := w.Update(h.ctx, ref, EventEdit{Summary: &title}); err != nil {
		t.Fatal(err)
	}
	if got := h.srv.Get("work", "rich.ics"); !strings.Contains(got, "X-ALT-DESC") || !strings.Contains(got, "DESCRIPTION;LANGUAGE=en:Daily") {
		t.Fatalf("object:\n%s", got)
	}
}
