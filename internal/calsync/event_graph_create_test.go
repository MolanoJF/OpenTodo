package calsync

import (
	"errors"
	"testing"

	"github.com/OpenTodoApp/opentodo/internal/calsync/graphtest"
)

// Creating Graph events and reading their description (calendar-event-editor
// task 2.3).

func graphWriter(t *testing.T) (*harness, *graphtest.Server, EventWriter) {
	t.Helper()
	h := newHarness(t)
	ms, c := h.microsoft("W. Europe Standard Time")
	return h, ms, h.writer(c)
}

func graphEventByID(t *testing.T, ms *graphtest.Server, id string) graphtest.Event {
	t.Helper()
	for _, e := range ms.Events("cal-work") {
		if e.ID == id {
			return e
		}
	}
	t.Fatalf("no event %s", id)
	return graphtest.Event{}
}

func TestGraphCreate(t *testing.T) {
	h, ms, w := graphWriter(t)
	ev := EventNew{UID: "client-0001", Summary: "Call Ana", Location: "Room 4", Description: "Bring the draft.", Free: true,
		When: EventWhen{Start: zonedPoint(t, "2026-11-13", "14:00", "Europe/Berlin"), End: zonedPoint(t, "2026-11-13", "14:30", "Europe/Berlin")}}
	st, err := w.Create(h.ctx, "cal-work", ev)
	if err != nil {
		t.Fatal(err)
	}
	if st.Ref.Href == "" || st.Ref.ETag == "" || st.Event.UID != st.Ref.Href || st.Event.Summary != "Call Ana" || !st.Event.Free || st.Event.Location != "Room 4" {
		t.Fatalf("state = %+v", st)
	}
	got := graphEventByID(t, ms, st.Ref.Href)
	if got.Body != "Bring the draft." || got.BodyType != "text" || got.ShowAs != "free" || got.Location != "Room 4" ||
		got.Start.DateTime != "2026-11-13T14:00:00" || got.Start.TimeZone != "W. Europe Standard Time" || got.TransactionID != "opentodo-ev-client-0001" {
		t.Fatalf("stored = %+v", got)
	}
	d, err := w.Detail(h.ctx, st.Ref)
	if err != nil || d.Description != "Bring the draft." || d.DescriptionLocked {
		t.Fatalf("detail = %q locked %v %v", d.Description, d.DescriptionLocked, err)
	}
	// The same transaction id returns the first event: one event in the calendar.
	again, err := w.Create(h.ctx, "cal-work", ev)
	if err != nil || again.Ref.Href != st.Ref.Href || len(ms.Events("cal-work")) != 1 {
		t.Fatalf("repeat = %+v %v, %d events", again.Ref, err, len(ms.Events("cal-work")))
	}
}

func TestGraphCreateAllDayAndRefusals(t *testing.T) {
	h, ms, w := graphWriter(t)
	st, err := w.Create(h.ctx, "cal-work", EventNew{UID: "client-0002", Summary: "Holiday",
		When: EventWhen{Start: datePoint("2026-11-13"), End: datePoint("2026-11-14"), AllDay: true}})
	if err != nil {
		t.Fatal(err)
	}
	if got := graphEventByID(t, ms, st.Ref.Href); !got.IsAllDay || got.Start.DateTime != "2026-11-13T00:00:00" || got.ShowAs != "busy" || got.Body != "" {
		t.Fatalf("stored = %+v", got)
	}
	when := EventWhen{Start: floatingPoint("2026-11-13", "14:00"), End: floatingPoint("2026-11-13", "15:00")}
	var e *Error
	if _, err := w.Create(h.ctx, "no-such-calendar", EventNew{UID: "client-0003", Summary: "x", When: when}); !errors.As(err, &e) || e.Reason != ReasonCalendarMissing {
		t.Fatalf("missing calendar = %v", err)
	}
	ms.SetCanEdit("cal-work", false)
	if _, err := w.Create(h.ctx, "cal-work", EventNew{UID: "client-0004", Summary: "x", When: when}); !errors.Is(err, ErrCalendarRO) {
		t.Fatalf("read-only = %v", err)
	}
}

func TestGraphDescriptionTextEmptySkeletonAndRich(t *testing.T) {
	const skeleton = "<html><head></head><body><br></body></html>"
	const rich = "<html><body><p>Agenda: <b>one</b></p></body></html>"
	h, ms, c := func() (*harness, *graphtest.Server, *Connection) {
		h := newHarness(t)
		ms, c := h.microsoft("W. Europe Standard Time")
		return h, ms, c
	}()
	add := func(subject, typ, body string) string {
		return ms.Add(graphtest.Event{Calendar: "cal-work", Subject: subject, BodyType: typ, Body: body,
			Start: graphtest.DT{DateTime: "2026-10-21T10:00:00", TimeZone: "UTC"}, End: graphtest.DT{DateTime: "2026-10-21T10:30:00", TimeZone: "UTC"}})
	}
	textID, skelID, richID := add("Text", "", "Plain words"), add("Skeleton", "html", skeleton), add("Rich", "html", rich)
	h.cycle(c)
	w := h.writer(c)
	ref := func(id string) EventRef { return h.davRef(c, "cal-work", id) }

	d, err := w.Detail(h.ctx, ref(textID))
	if err != nil || d.Description != "Plain words" || d.DescriptionLocked {
		t.Fatalf("text = %q %v %v", d.Description, d.DescriptionLocked, err)
	}
	d, err = w.Detail(h.ctx, ref(skelID))
	if err != nil || d.Description != "" || d.DescriptionLocked {
		t.Fatalf("skeleton = %q %v %v", d.Description, d.DescriptionLocked, err)
	}
	d, err = w.Detail(h.ctx, ref(richID))
	if err != nil || d.Description != "Agenda: one" || !d.DescriptionLocked {
		t.Fatalf("rich = %q %v %v", d.Description, d.DescriptionLocked, err)
	}

	// A text body is editable; an empty skeleton too (it becomes a text body).
	desc := "Changed"
	if _, err := w.Update(h.ctx, ref(textID), EventEdit{Description: &desc}); err != nil {
		t.Fatal(err)
	}
	if got := graphEventByID(t, ms, textID); got.Body != "Changed" || got.BodyType != "text" {
		t.Fatalf("text event = %+v", got)
	}
	if _, err := w.Update(h.ctx, ref(skelID), EventEdit{Description: &desc}); err != nil {
		t.Fatal(err)
	}
	if got := graphEventByID(t, ms, skelID); got.Body != "Changed" || got.BodyType != "text" {
		t.Fatalf("skeleton event = %+v", got)
	}
	// A rich body is refused (and left alone), but its title can change.
	_, err = w.Update(h.ctx, ref(richID), EventEdit{Description: &desc})
	if !errors.Is(err, ErrEventLocked) || lockReason(err) != LockRichDescription {
		t.Fatalf("rich update = %v", err)
	}
	if got := graphEventByID(t, ms, richID); got.Body != rich || got.BodyType != "html" {
		t.Fatalf("rich body changed: %+v", got)
	}
	title := "Retitled"
	if _, err := w.Update(h.ctx, ref(richID), EventEdit{Summary: &title}); err != nil {
		t.Fatal(err)
	}
	if got := graphEventByID(t, ms, richID); got.Subject != title || got.Body != rich {
		t.Fatalf("rich after title = %+v", got)
	}
}

func TestGraphUpdateLocationFreeAndShowAsSurvives(t *testing.T) {
	h, ms, _, w, ref, id := graphSetup(t, func(e *graphtest.Event) { e.ShowAs = "tentative"; e.Location = "Old" })
	title := "New title"
	st, err := w.Update(h.ctx, ref, EventEdit{Summary: &title})
	if err != nil {
		t.Fatal(err)
	}
	if got := graphEventByID(t, ms, id); got.ShowAs != "tentative" || got.Location != "Old" {
		t.Fatalf("title edit changed showAs or location: %+v", got)
	}
	ref.ETag = st.Ref.ETag
	loc, free := "Room 9", true
	st, err = w.Update(h.ctx, ref, EventEdit{Location: &loc, Free: &free})
	if err != nil {
		t.Fatal(err)
	}
	if got := graphEventByID(t, ms, id); got.ShowAs != "free" || got.Location != "Room 9" || !st.Event.Free || st.Event.Location != "Room 9" {
		t.Fatalf("stored = %+v state = %+v", got, st.Event)
	}
	ref.ETag = st.Ref.ETag
	busy := false
	if _, err := w.Update(h.ctx, ref, EventEdit{Free: &busy}); err != nil {
		t.Fatal(err)
	}
	if got := graphEventByID(t, ms, id); got.ShowAs != "busy" {
		t.Fatalf("showAs = %q", got.ShowAs)
	}
}

func TestGraphDetailRefusals(t *testing.T) {
	h, ms, _, w, ref, id := graphSetup(t, nil)
	ms.Delete(id)
	if _, err := w.Detail(h.ctx, ref); !errors.Is(err, ErrEventGone) {
		t.Fatalf("deleted = %v", err)
	}
}
