package calsync

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/OpenTodoApp/opentodo/internal/calfeed"
)

// Scoped changes against the in-process CalDAV server (calendar-event-series
// task 2.4): the golden inputs of internal/calfeed/testdata/series are the
// objects, the golden outputs what the server must hold afterwards.

const seriesFixtures = "../calfeed/testdata/series"

type whenJSON struct{ Date, Time, TZ string }

type caseJSON struct {
	Op        string
	Scope     calfeed.Scope
	Instance  string
	NewUID    string
	Summary   *string
	When      *struct{ Start, End whenJSON }
	Repeat    json.RawMessage
	Reminders *[]int
}

func (w whenJSON) point(t *testing.T) calfeed.Point {
	t.Helper()
	switch {
	case w.Time == "":
		return datePoint(w.Date)
	case w.TZ == "":
		return floatingPoint(w.Date, w.Time)
	}
	return zonedPoint(t, w.Date, w.Time, w.TZ)
}

func (c caseJSON) edit(t *testing.T) EventEdit {
	t.Helper()
	e := EventEdit{Scope: c.Scope, Summary: c.Summary, Reminders: c.Reminders}
	if c.When != nil {
		e.When = &EventWhen{Start: c.When.Start.point(t), End: c.When.End.point(t)}
		e.When.AllDay = e.When.Start.AllDay
	}
	if len(c.Repeat) > 0 {
		e.Repeat = &RepeatEdit{}
		if string(c.Repeat) != "null" {
			e.Repeat.Rule = &calfeed.Repeat{}
			if err := json.Unmarshal(c.Repeat, e.Repeat.Rule); err != nil {
				t.Fatal(err)
			}
		}
	}
	return e
}

func lf(s string) string { return strings.ReplaceAll(s, "\r\n", "\n") }

func readFixture(t *testing.T, name, file string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(seriesFixtures, name, file))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func loadCase(t *testing.T, name string) (string, caseJSON) {
	t.Helper()
	var c caseJSON
	if err := json.Unmarshal([]byte(readFixture(t, name, "change.json")), &c); err != nil {
		t.Fatal(err)
	}
	return readFixture(t, name, "in.ics"), c
}

// seriesHarness puts an object on the fake server and has the shown calendar read it.
func seriesHarness(t *testing.T, data string) (*harness, *Connection, davEvents) {
	t.Helper()
	h := newHarness(t)
	h.clock = time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
	h.srv.Put("work", "routine.ics", data)
	c := h.connect(false)
	if _, err := h.w.Store.Update(h.ctx, "alice", c.ID, Patch{EditEvents: ptr(true)}); err != nil {
		t.Fatal(err)
	}
	h.cycle(h.conn(c.ID))
	return h, h.conn(c.ID), h.writer(c).(davEvents)
}

func ptr[T any](v T) *T { return &v }

// refAt is the ref of the occurrence of uid at instance, as the cache holds it.
func (h *harness) refAt(c *Connection, uid, instance string) calfeed.EventRef {
	h.t.Helper()
	managed, err := h.w.Feeds.ManagedOf(h.ctx, c.ID)
	if err != nil {
		h.t.Fatal(err)
	}
	m := managed[h.srv.CalendarURL("work")]
	refs, err := h.w.Feeds.Refs(h.ctx, m.SubscriptionID)
	if err != nil {
		h.t.Fatal(err)
	}
	for _, r := range refs {
		if r.UID == uid && r.Instance == instance {
			return r
		}
	}
	h.t.Fatalf("no cached occurrence %s %s", uid, instance)
	return calfeed.EventRef{}
}

func writerRef(h *harness, r calfeed.EventRef) EventRef {
	return EventRef{Calendar: h.srv.CalendarURL("work"), Href: r.Href, ETag: r.ETag, Instance: r.Instance}
}

// uidOf reads the UID of the first VEVENT of an object.
func uidOf(data string) string {
	for _, l := range strings.Split(lf(data), "\n") {
		if strings.HasPrefix(l, "UID:") {
			return strings.TrimPrefix(l, "UID:")
		}
	}
	return ""
}

func TestDAVSeriesGoldens(t *testing.T) {
	apply := []string{"this-move", "this-rename-existing-exception", "all-shift", "all-rename-carry", "all-resize", "all-weekly-day-shift", "midnight-all", "midnight-this",
		"dst-crossing", "exdate-list-all", "reminders-add", "reminders-keep-other", "reminders-this-override", "x-props-kept-this", "x-props-kept-all", "all-new-repeat-drops-exceptions",
		"following-with-exceptions-after", "following-daily", "following-count", "following-consumes-exception", "following-new-repeat", "following-stop-repeating",
		"following-first-is-all", "exdate-list-following", "x-props-kept-following", "exception-before-master", "duration-master-this", "duration-master-all"}
	for _, name := range apply {
		t.Run(name, func(t *testing.T) {
			in, ch := loadCase(t, name)
			h, c, w := seriesHarness(t, in)
			uid := uidOf(in)
			ref := writerRef(h, h.refAt(c, uid, ch.Instance))
			st, err := w.Update(h.ctx, ref, ch.edit(t))
			if err != nil {
				t.Fatal(err)
			}
			if h.srv.Unconditional() != 0 {
				t.Fatalf("%d writes without a precondition", h.srv.Unconditional())
			}
			want := readFixture(t, name, "out.ics")
			got := h.srv.Get("work", "routine.ics")
			if lf(got) != lf(want) {
				t.Errorf("the object differs from the golden:\n--- got ---\n%s\n--- want ---\n%s", lf(got), lf(want))
			}
			members := h.srv.Members("work")
			if _, err := os.Stat(filepath.Join(seriesFixtures, name, "out_new.ics")); err == nil {
				nu := calfeed.NewSeriesUID(uid, ch.Instance, ref.ETag)
				if len(members) != 2 || h.srv.Get("work", nu+".ics") == "" {
					t.Fatalf("members = %v, want the original and %s", members, nu)
				}
				fresh := strings.ReplaceAll(h.srv.Get("work", nu+".ics"), nu, ch.NewUID)
				if lf(fresh) != lf(readFixture(t, name, "out_new.ics")) {
					t.Errorf("the new object differs from the golden:\n%s", lf(fresh))
				}
				writes := h.srv.Writes()
				if len(writes) != 2 || writes[0] != "PUT "+nu+".ics If-None-Match" || writes[1] != "PUT routine.ics If-Match" {
					t.Errorf("writes = %v: the new object goes first, both conditional", writes)
				}
			} else if len(members) != 1 {
				t.Errorf("members = %v", members)
			}
			if st.Ref.ETag == "" || len(st.Wrote.Written) == 0 {
				t.Errorf("state = %+v", st.Wrote)
			}
		})
	}
	for _, name := range []string{"delete-this", "delete-this-with-exception", "delete-following", "delete-all", "delete-following-first", "delete-this-last-remaining", "delete-this-not-last"} {
		t.Run(name, func(t *testing.T) {
			in, ch := loadCase(t, name)
			h, c, w := seriesHarness(t, in)
			uid := uidOf(in)
			wr, err := w.DeleteScoped(h.ctx, writerRef(h, h.refAt(c, uid, ch.Instance)), ch.Scope)
			if err != nil {
				t.Fatal(err)
			}
			if h.srv.Unconditional() != 0 {
				t.Fatalf("%d deletes without a precondition", h.srv.Unconditional())
			}
			if _, err := os.Stat(filepath.Join(seriesFixtures, name, "out.ics")); err != nil {
				if h.srv.Get("work", "routine.ics") != "" || len(wr.Deleted) != 1 || len(wr.Written) != 0 {
					t.Fatalf("the object should be deleted: %v %+v", h.srv.Members("work"), wr)
				}
				if w := h.srv.Writes(); len(w) != 1 || w[0] != "DELETE routine.ics If-Match" {
					t.Fatalf("writes = %v", w)
				}
				return
			}
			if lf(h.srv.Get("work", "routine.ics")) != lf(readFixture(t, name, "out.ics")) || len(wr.Written) != 1 || len(wr.Deleted) != 0 {
				t.Errorf("the object differs from the golden:\n%s", lf(h.srv.Get("work", "routine.ics")))
			}
		})
	}
}

func TestDAVSeriesSplitFailureLeavesTheOriginal(t *testing.T) {
	in, ch := loadCase(t, "following-daily")
	for name, tc := range map[string]struct {
		fail func(h *harness)
		want error
	}{
		"412 on the second write":      {func(h *harness) { h.srv.FailNext("PUT", "routine.ics", 412, 1) }, ErrEventChanged},
		"500 on the second write":      {func(h *harness) { h.srv.FailNext("PUT", "routine.ics", 500, 1) }, nil},
		"503 on the second write":      {func(h *harness) { h.srv.FailNext("PUT", "routine.ics", 503, 1) }, nil},
		"403 on the second write":      {func(h *harness) { h.srv.FailNext("PUT", "routine.ics", 403, 1) }, ErrCalendarRO},
		"the first write is refused":   {func(h *harness) { h.srv.FailNext("PUT", "ot-", 500, 1) }, nil},
		"the first write is forbidden": {func(h *harness) { h.srv.FailNext("PUT", "ot-", 403, 1) }, ErrCalendarRO},
	} {
		t.Run(name, func(t *testing.T) {
			h, c, w := seriesHarness(t, in)
			ref := writerRef(h, h.refAt(c, uidOf(in), ch.Instance))
			before := h.srv.Get("work", "routine.ics")
			tc.fail(h)
			if _, err := w.Update(h.ctx, ref, ch.edit(t)); err == nil || (tc.want != nil && !errors.Is(err, tc.want)) {
				t.Fatalf("update = %v, want %v", err, tc.want)
			} else if errors.Is(err, ErrPartialWrite) {
				t.Fatal("a rolled-back split is not a partial write")
			}
			if got := h.srv.Members("work"); len(got) != 1 || h.srv.Get("work", "routine.ics") != before {
				t.Fatalf("the calendar is not as it was: %v", got)
			}
			if h.srv.Unconditional() != 0 {
				t.Fatalf("%d writes without a precondition", h.srv.Unconditional())
			}
			writes := h.srv.Writes()
			if strings.HasPrefix(name, "the first") {
				if len(writes) != 1 {
					t.Fatalf("writes = %v", writes)
				}
				return
			}
			if len(writes) != 3 || !strings.HasPrefix(writes[2], "DELETE ot-") || !strings.HasSuffix(writes[2], " If-Match") {
				t.Fatalf("writes = %v: the new object is deleted with If-Match", writes)
			}
		})
	}
}

func TestDAVSeriesAnswerLostAfterBothWrites(t *testing.T) {
	in, ch := loadCase(t, "following-daily")
	h, c, w := seriesHarness(t, in)
	ref := writerRef(h, h.refAt(c, uidOf(in), ch.Instance))
	h.srv.FailNextAfterApply("PUT", "routine.ics", 500, 1)
	st, err := w.Update(h.ctx, ref, ch.edit(t))
	if err != nil {
		t.Fatalf("a write that went through is a success: %v", err)
	}
	if len(h.srv.Members("work")) != 2 || len(st.Wrote.Written) != 2 || st.Wrote.Written[1].Ref.ETag == "" {
		t.Fatalf("members %v, written %+v", h.srv.Members("work"), st.Wrote)
	}
	if lf(h.srv.Get("work", "routine.ics")) != lf(readFixture(t, "following-daily", "out.ics")) {
		t.Error("the old object is not the golden")
	}
}

func TestDAVSeriesFailedRollbackIsPartialWrite(t *testing.T) {
	in, ch := loadCase(t, "following-daily")
	h, c, w := seriesHarness(t, in)
	ref := writerRef(h, h.refAt(c, uidOf(in), ch.Instance))
	nu := calfeed.NewSeriesUID(uidOf(in), ch.Instance, ref.ETag)
	h.srv.FailNext("PUT", "routine.ics", 412, 1)
	h.srv.FailNext("DELETE", "ot-", 500, 1)
	if _, err := w.Update(h.ctx, ref, ch.edit(t)); !errors.Is(err, ErrPartialWrite) {
		t.Fatalf("update = %v, want a partial write", err)
	}
	if h.srv.Get("work", nu+".ics") == "" {
		t.Fatal("the new object should be left for the cache to show")
	}
}

func TestDAVSeriesReplayOfAHalfDoneSplit(t *testing.T) {
	in, ch := loadCase(t, "following-daily")
	h, c, w := seriesHarness(t, in)
	ref := writerRef(h, h.refAt(c, uidOf(in), ch.Instance))
	nu := calfeed.NewSeriesUID(uidOf(in), ch.Instance, ref.ETag)
	h.srv.FailNext("PUT", "routine.ics", 500, 1)
	h.srv.FailNext("DELETE", "ot-", 500, 1)
	if _, err := w.Update(h.ctx, ref, ch.edit(t)); !errors.Is(err, ErrPartialWrite) {
		t.Fatalf("first attempt = %v, want a partial write", err)
	}
	if got := h.srv.Members("work"); len(got) != 2 {
		t.Fatalf("members = %v", got)
	}
	// The origin is untouched, so a retry computes the same uid and finds its own object at the address.
	before := len(h.srv.Writes())
	st, err := w.Update(h.ctx, ref, ch.edit(t))
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	writes := h.srv.Writes()[before:]
	if len(writes) != 3 || writes[0] != "PUT "+nu+".ics If-None-Match" || writes[1] != "PUT "+nu+".ics If-Match" || writes[2] != "PUT routine.ics If-Match" {
		t.Fatalf("retry writes = %v", writes)
	}
	if len(h.srv.Members("work")) != 2 || len(st.Wrote.Written) != 2 {
		t.Fatalf("members = %v", h.srv.Members("work"))
	}
	fresh := strings.ReplaceAll(h.srv.Get("work", nu+".ics"), nu, ch.NewUID)
	if lf(fresh) != lf(readFixture(t, "following-daily", "out_new.ics")) {
		t.Errorf("the replayed object is not the golden:\n%s", lf(fresh))
	}
}

func TestDAVSeriesHrefTaken(t *testing.T) {
	in, ch := loadCase(t, "following-daily")
	h, c, w := seriesHarness(t, in)
	ref := writerRef(h, h.refAt(c, uidOf(in), ch.Instance))
	nu := calfeed.NewSeriesUID(uidOf(in), ch.Instance, ref.ETag)
	h.srv.Put("work", nu+".ics", strings.Replace(in, "UID:routine-1@google.com", "UID:somebody-else", 1))
	before := h.srv.Get("work", "routine.ics")
	_, err := w.Update(h.ctx, ref, ch.edit(t))
	var e *Error
	if !errors.As(err, &e) || e.Reason != ReasonHrefTaken {
		t.Fatalf("update = %v, want href_taken", err)
	}
	if h.srv.Get("work", "routine.ics") != before || h.srv.Count("DELETE") != 0 {
		t.Fatal("the calendar was written")
	}
}

func TestDAVSeriesRefusalsWriteNothing(t *testing.T) {
	in, ch := loadCase(t, "this-move")
	h, c, w := seriesHarness(t, in)
	ref := writerRef(h, h.refAt(c, uidOf(in), ch.Instance))
	stale := ref
	stale.ETag = `"stale"`
	if _, err := w.Update(h.ctx, stale, ch.edit(t)); !errors.Is(err, ErrEventChanged) {
		t.Fatalf("a stale version: %v", err)
	}
	noScope := ch.edit(t)
	noScope.Scope = "everything"
	if _, err := w.Update(h.ctx, ref, noScope); !errors.Is(err, ErrEventInvalid) || invalidField(err) != "scope" {
		t.Fatalf("an unknown scope: %v", err)
	}
	thisRepeat := ch.edit(t)
	thisRepeat.Repeat = &RepeatEdit{Rule: &calfeed.Repeat{Freq: "weekly"}}
	if _, err := w.Update(h.ctx, ref, thisRepeat); !errors.Is(err, ErrEventInvalid) || invalidField(err) != "repeat" {
		t.Fatalf("a repeat with this: %v", err)
	}
	// A day move of a monthly series is refused with the field.
	in2, ch2 := loadCase(t, "all-monthly-day-shift-refused")
	h2, c2, w2 := seriesHarness(t, in2)
	if _, err := w2.Update(h2.ctx, writerRef(h2, h2.refAt(c2, uidOf(in2), ch2.Instance)), ch2.edit(t)); !errors.Is(err, ErrEventInvalid) || invalidField(err) != "start" {
		t.Fatalf("a monthly day move: %v", err)
	}
	// The cache may be stale about a series: a ref without instance on a repeating object is a change.
	plain := ref
	plain.Instance = ""
	if _, err := w.Update(h.ctx, plain, EventEdit{Summary: ch.Summary}); !errors.Is(err, ErrEventChanged) {
		t.Fatalf("a single-event ref of a series: %v", err)
	}
	if h.srv.Count("PUT") != 0 || h.srv.Count("DELETE") != 0 {
		t.Fatalf("a refused change wrote: %v", h.srv.Writes())
	}
	// Objects outside the envelope are locked before any request is made.
	for _, name := range []string{"locked-rdate", "locked-thisandfuture", "locked-two-rrules", "locked-guest-in-exception"} {
		in, _ := loadCase2(t, name)
		h, _, w := seriesHarness(t, in)
		ref := EventRef{Calendar: h.srv.CalendarURL("work"), Href: h.srv.CalendarURL("work") + "routine.ics", ETag: etagOf(t, h), Instance: "20261112T060000Z"}
		_, err := w.Update(h.ctx, ref, EventEdit{Scope: calfeed.ScopeThis, Summary: ch.Summary})
		want := LockSeriesUnsupported
		if name == "locked-guest-in-exception" {
			want = LockGuests
		}
		if !errors.Is(err, ErrEventLocked) || lockReason(err) != want || h.srv.Count("PUT") != 0 {
			t.Errorf("%s: %v (writes %v)", name, err, h.srv.Writes())
		}
	}
}

func loadCase2(t *testing.T, name string) (string, bool) { return readFixture(t, name, "in.ics"), true }

// etagOf is the version the fake holds for the object.
func etagOf(t *testing.T, h *harness) string {
	t.Helper()
	d := davEvents{d: mustDAV(t, h), now: h.w.clock}
	status, etag, _, err := d.d.getObject(h.ctx, h.srv.CalendarURL("work")+"routine.ics")
	if err != nil || status != 200 {
		t.Fatalf("get: %d %v", status, err)
	}
	return etag
}

func mustDAV(t *testing.T, h *harness) *DAV {
	t.Helper()
	c, err := h.w.Store.List(h.ctx, "alice")
	if err != nil || len(c) == 0 {
		t.Fatal(err)
	}
	d, err := h.w.Client(h.ctx, &c[0])
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestDAVSeriesServerWithoutETagOnPut(t *testing.T) {
	in, ch := loadCase(t, "all-shift")
	h := newHarness(t)
	h.clock = time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
	h.srv.NoETagOnPut = true
	h.srv.Put("work", "routine.ics", in)
	c := h.connect(false)
	if _, err := h.w.Store.Update(h.ctx, "alice", c.ID, Patch{EditEvents: ptr(true)}); err != nil {
		t.Fatal(err)
	}
	h.cycle(h.conn(c.ID))
	w := h.writer(c).(davEvents)
	st, err := w.Update(h.ctx, writerRef(h, h.refAt(h.conn(c.ID), uidOf(in), ch.Instance)), ch.edit(t))
	if err != nil {
		t.Fatal(err)
	}
	if st.Ref.ETag == "" || st.Locked != "" {
		t.Fatalf("the version should be asked for with a GET: %+v", st.Ref)
	}
	// A server that never says the version of an object: the pre-read finds none to compare, and nothing is written.
	h2 := newHarness(t)
	h2.clock = h.clock
	h2.srv.NoETagOnPut, h2.srv.NoETagOnGet = true, true
	h2.srv.Put("work", "routine.ics", in)
	c2 := h2.connect(false)
	if _, err := h2.w.Store.Update(h2.ctx, "alice", c2.ID, Patch{EditEvents: ptr(true)}); err != nil {
		t.Fatal(err)
	}
	h2.cycle(h2.conn(c2.ID))
	r := h2.refAt(h2.conn(c2.ID), uidOf(in), ch.Instance)
	if _, err := h2.writer(c2).Update(h2.ctx, writerRef(h2, r), ch.edit(t)); !errors.Is(err, ErrEventChanged) || h2.srv.Count("PUT") != 0 {
		t.Fatalf("update = %v", err)
	}
}

func TestDAVSetRepeatOnASingleEventAndDetail(t *testing.T) {
	h := newHarness(t)
	h.clock = time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
	h.srv.Put("work", "rich.ics", richEvent)
	c := h.connect(false)
	if _, err := h.w.Store.Update(h.ctx, "alice", c.ID, Patch{EditEvents: ptr(true)}); err != nil {
		t.Fatal(err)
	}
	h.cycle(h.conn(c.ID))
	w := h.writer(c)
	ref := h.davRef(h.conn(c.ID), h.srv.CalendarURL("work"), "rich")
	rep := &RepeatEdit{Rule: &calfeed.Repeat{Freq: "weekly", Ends: calfeed.RepeatEnd{Type: calfeed.EndAfter, Count: 4}}}
	st, err := w.Update(h.ctx, ref, EventEdit{Repeat: rep, Reminders: &[]int{30, 1440}})
	if err != nil {
		t.Fatal(err)
	}
	got := h.srv.Get("work", "rich.ics")
	for _, want := range []string{"RRULE:FREQ=WEEKLY;COUNT=4", "TRIGGER:-PT30M", "TRIGGER:-P1D", "X-CUSTOM;X-P=1:keep me"} {
		if !strings.Contains(got, want) {
			t.Errorf("object lacks %q:\n%s", want, got)
		}
	}
	if len(st.Wrote.Written) != 1 || st.Wrote.Written[0].Series == nil || st.Wrote.Instance == "" {
		t.Fatalf("a repeating event is written as a series: %+v", st.Wrote)
	}
	// The single event refuses a "stop repeating" it has nothing to stop on.
	ref2 := EventRef{Calendar: ref.Calendar, Href: ref.Href, ETag: st.Ref.ETag}
	if _, err := w.Update(h.ctx, ref2, EventEdit{Summary: ptr("x")}); !errors.Is(err, ErrEventChanged) {
		t.Fatalf("a series object with a single-event ref: %v", err)
	}
	d, err := w.Detail(h.ctx, EventRef{Calendar: ref.Calendar, Href: ref.Href, ETag: st.Ref.ETag, Instance: st.Wrote.Instance})
	if err != nil {
		t.Fatal(err)
	}
	if d.Series == nil || d.Series.Repeat == nil || d.Series.Repeat.Freq != "weekly" || d.Series.Repeat.Ends.Count != 4 || d.Series.Exceptions != 0 || d.Series.Custom ||
		len(d.Reminders) != 2 || d.RemindersOther != 0 || d.RemindersMax != 5 || d.Description != "Daily, short" {
		t.Fatalf("detail = %+v series %+v", d, d.Series)
	}
}

func TestDAVCreateWithRepeatAndReminders(t *testing.T) {
	h := newHarness(t)
	h.clock = time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
	c := h.connect(false)
	if _, err := h.w.Store.Update(h.ctx, "alice", c.ID, Patch{EditEvents: ptr(true)}); err != nil {
		t.Fatal(err)
	}
	w := h.writer(c)
	ev := EventNew{UID: "client-0001", Summary: "Planning", When: EventWhen{Start: zonedPoint(t, "2026-11-16", "09:00", "Europe/Berlin"), End: zonedPoint(t, "2026-11-16", "10:00", "Europe/Berlin")},
		Repeat: &calfeed.Repeat{Freq: "weekly", Days: []string{"mo", "we"}}, Reminders: []int{10}}
	st, err := w.Create(h.ctx, h.srv.CalendarURL("work"), ev)
	if err != nil {
		t.Fatal(err)
	}
	got := h.srv.Get("work", "client-0001.ics")
	for _, want := range []string{"RRULE:FREQ=WEEKLY;BYDAY=MO,WE", "BEGIN:VALARM", "TRIGGER:-PT10M", "TZID:Europe/Berlin"} {
		if !strings.Contains(got, want) {
			t.Errorf("object lacks %q:\n%s", want, got)
		}
	}
	if st.Locked != "" || len(st.Wrote.Written) != 1 || st.Wrote.Instance != "20261116T080000Z" {
		t.Fatalf("state = %+v", st.Wrote)
	}
	// A weekly repeat that skips the start's weekday is refused before a request.
	bad := ev
	bad.UID = "client-0002"
	bad.Repeat = &calfeed.Repeat{Freq: "weekly", Days: []string{"tu"}}
	if _, err := w.Create(h.ctx, h.srv.CalendarURL("work"), bad); !errors.Is(err, ErrEventInvalid) || invalidField(err) != "repeat" {
		t.Fatalf("a repeat that skips the start: %v", err)
	}
	if h.srv.Count("PUT") != 1 {
		t.Fatalf("PUT count %d", h.srv.Count("PUT"))
	}
}
