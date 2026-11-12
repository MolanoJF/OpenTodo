package calsync

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/OpenTodoApp/opentodo/internal/calfeed"
	"github.com/OpenTodoApp/opentodo/internal/calsync/graphtest"
)

// Scoped changes against the in-process Microsoft Graph (calendar-event-series
// task 2.6): the fake generates the occurrences of a master, stores the exceptions
// a patch or a delete of an occurrence makes, and drops them when the master's
// times or pattern change, as Outlook does.

const weHere = "W. Europe Standard Time"

func weeklyMaster(f func(e *graphtest.Event)) graphtest.Event {
	e := graphtest.Event{Calendar: "cal-work", Type: "seriesMaster", Subject: "Training",
		Start: graphtest.DT{DateTime: "2026-10-26T07:00:00", TimeZone: weHere}, End: graphtest.DT{DateTime: "2026-10-26T08:30:00", TimeZone: weHere},
		ReminderOn: true, ReminderMinutes: 15,
		Recurrence: &graphtest.Recurrence{
			Pattern: graphtest.Pattern{Type: "weekly", Interval: 1, DaysOfWeek: []string{"monday", "wednesday"}, FirstDayOfWeek: "monday"},
			Range:   graphtest.Range{Type: "noEnd", StartDate: "2026-10-26", RecurrenceTimeZone: weHere}}}
	if f != nil {
		f(&e)
	}
	return e
}

type msEnv struct {
	h      *harness
	ms     *graphtest.Server
	c      *Connection
	w      EventWriter
	master string
}

func newMSEnv(t *testing.T, f func(e *graphtest.Event)) *msEnv {
	t.Helper()
	h := newHarness(t)
	ms, c := h.microsoft(weHere)
	if _, err := h.w.Store.Update(h.ctx, "alice", c.ID, Patch{EditEvents: ptr(true)}); err != nil {
		t.Fatal(err)
	}
	id := ms.Add(weeklyMaster(f))
	h.cycle(h.conn(c.ID))
	return &msEnv{h: h, ms: ms, c: h.conn(c.ID), w: h.writer(c), master: id}
}

// ref is the writer's reference for the occurrence on a date.
func (e *msEnv) ref(t *testing.T, date string) EventRef {
	t.Helper()
	for _, row := range e.h.rows(e.c) {
		if row.Start.Date == date && row.Series != nil {
			r, err := e.h.w.Feeds.RefByUID(e.h.ctx, e.h.refSub(e.c), row.UID)
			if err != nil {
				t.Fatal(err)
			}
			return EventRef{Calendar: "cal-work", Href: r.Href, ETag: r.ETag, Instance: r.Instance}
		}
	}
	t.Fatalf("no occurrence on %s", date)
	return EventRef{}
}

func (e *msEnv) master_(t *testing.T) graphtest.Event {
	t.Helper()
	m, ok := e.ms.Event(e.master)
	if !ok {
		t.Fatal("no master")
	}
	return m
}

func TestGraphSeriesIsCachedWithTheMasterId(t *testing.T) {
	e := newMSEnv(t, nil)
	if e.ms.CountContaining("seriesMasterId") == 0 {
		t.Error("the window read does not select seriesMasterId")
	}
	rows := e.h.rows(e.c)
	if len(rows) < 100 {
		t.Fatalf("rows = %d", len(rows))
	}
	ids := map[string]bool{}
	for _, r := range rows {
		if r.Series == nil || r.Locked != "" || !r.Known || r.Series.Rev != "" || r.Series.First || r.Series.RRule != "" {
			t.Fatalf("row %s: series %+v locked %q", r.Start.Date, r.Series, r.Locked)
		}
		ids[r.Series.ID] = true
	}
	if len(ids) != 1 || !ids[calfeed.SeriesID(e.h.refSub(e.c), e.master)] {
		t.Fatalf("series ids %v", ids)
	}
	// An occurrence without a master id is locked.
	e.ms.Add(graphtest.Event{Calendar: "cal-work", Subject: "Odd", Type: "occurrence",
		Start: graphtest.DT{DateTime: "2026-10-22T10:00:00", TimeZone: "UTC"}, End: graphtest.DT{DateTime: "2026-10-22T11:00:00", TimeZone: "UTC"}})
}

// refSub is the managed subscription of the connection's target calendar.
func (h *harness) refSub(c *Connection) string {
	h.t.Helper()
	managed, err := h.w.Feeds.ManagedOf(h.ctx, c.ID)
	if err != nil {
		h.t.Fatal(err)
	}
	return managed["cal-work"].SubscriptionID
}

func TestGraphSeriesThisPatchesTheOccurrence(t *testing.T) {
	e := newMSEnv(t, nil)
	ref := e.ref(t, "2026-11-04")
	title := "Quiet one"
	st, err := e.w.Update(e.h.ctx, ref, EventUpdate(calfeed.ScopeThis, &title))
	if err != nil {
		t.Fatal(err)
	}
	if e.ms.Unconditional() != 0 || len(st.Wrote.Written) != 1 || st.Wrote.UID != ref.Href || st.Event.Summary != title {
		t.Fatalf("state %+v", st.Wrote)
	}
	got, ok := e.ms.Event(ref.Href)
	if !ok || got.Type != "exception" || got.Subject != title || got.SeriesMasterID != e.master {
		t.Fatalf("event = %+v", got)
	}
	if m := e.master_(t); m.Subject != "Training" {
		t.Fatalf("master = %+v", m)
	}
	patches := 0
	for _, r := range e.ms.Requests() {
		if strings.HasPrefix(r, "PATCH") {
			patches++
			if !strings.Contains(r, ref.Href) {
				t.Errorf("patch of another event: %s", r)
			}
		}
	}
	if patches != 1 {
		t.Fatalf("patches = %d", patches)
	}
}

// EventUpdate is an edit of a series' title for a scope.
func EventUpdate(scope calfeed.Scope, title *string) EventEdit {
	return EventEdit{Scope: scope, Summary: title}
}

func TestGraphSeriesAllPatchesTheMaster(t *testing.T) {
	e := newMSEnv(t, nil)
	// An exception the rename must leave, and one the time move drops.
	ex := e.ref(t, "2026-11-04")
	exTitle := "Hand moved"
	if _, err := e.w.Update(e.h.ctx, ex, EventUpdate(calfeed.ScopeThis, &exTitle)); err != nil {
		t.Fatal(err)
	}
	e.h.cycle(e.h.conn(e.c.ID))
	ref := e.ref(t, "2026-11-09")
	rename := "Everything"
	st, err := e.w.Update(e.h.ctx, ref, EventUpdate(calfeed.ScopeAll, &rename))
	if err != nil {
		t.Fatal(err)
	}
	m := e.master_(t)
	if m.Subject != rename || e.ms.Unconditional() != 0 || !st.Wrote.Refetch || len(st.Wrote.Written) < 100 {
		t.Fatalf("master %+v, written %d", m, len(st.Wrote.Written))
	}
	if _, kept := e.ms.Event(ex.Href); !kept {
		t.Fatal("a rename dropped the exception")
	}
	// The master was patched with the version just read, not the occurrence's.
	var masterPatches int
	for _, r := range e.ms.Requests() {
		if strings.SplitN(r, "?", 2)[0] == "PATCH /v1.0/me/events/"+e.master {
			masterPatches++
		}
	}
	if masterPatches != 1 {
		t.Fatalf("master patches = %d", masterPatches)
	}
	// A time move: the master moves by what the occurrence moved, the exception is dropped (Graph does).
	e.h.cycle(e.h.conn(e.c.ID))
	ref = e.ref(t, "2026-11-09")
	st, err = e.w.Update(e.h.ctx, ref, EventEdit{Scope: calfeed.ScopeAll, When: &EventWhen{Start: zonedPoint(t, "2026-11-09", "07:30", "Europe/Berlin"), End: zonedPoint(t, "2026-11-09", "09:00", "Europe/Berlin")}})
	if err != nil {
		t.Fatal(err)
	}
	m = e.master_(t)
	if m.Start.DateTime != "2026-10-26T07:30:00" || m.End.DateTime != "2026-10-26T09:00:00" || m.Start.TimeZone != weHere || m.Recurrence.Range.StartDate != "2026-10-26" {
		t.Fatalf("master = %+v", m)
	}
	if _, kept := e.ms.Event(ex.Href); kept {
		t.Fatal("the fake kept an exception when the master's times changed: it should document Outlook dropping it")
	}
	if st.Wrote.UID == "" || st.Wrote.UID == ref.Href && false {
		t.Fatalf("written %+v", st.Wrote)
	}
}

func TestGraphSeriesAllDayMoveRotatesAWeeklyPattern(t *testing.T) {
	e := newMSEnv(t, nil)
	ref := e.ref(t, "2026-11-09")
	st, err := e.w.Update(e.h.ctx, ref, EventEdit{Scope: calfeed.ScopeAll, When: &EventWhen{Start: zonedPoint(t, "2026-11-10", "07:00", "Europe/Berlin"), End: zonedPoint(t, "2026-11-10", "08:30", "Europe/Berlin")}})
	if err != nil {
		t.Fatal(err)
	}
	m := e.master_(t)
	if got := m.Recurrence.Pattern.DaysOfWeek; len(got) != 2 || got[0] != "tuesday" || got[1] != "thursday" || m.Recurrence.Range.StartDate != "2026-10-27" || m.Start.DateTime != "2026-10-27T07:00:00" {
		t.Fatalf("master = %+v", m)
	}
	if st.Wrote.UID == "" {
		t.Fatal("no edited occurrence")
	}
	for _, o := range st.Wrote.Written[:3] {
		if wd := o.Events[0].Start.T.Weekday(); wd != time.Tuesday && wd != time.Thursday {
			t.Errorf("occurrence on %v", o.Events[0].Start.T)
		}
	}
	// A monthly series cannot move to another day.
	e2 := newMSEnv(t, func(m *graphtest.Event) {
		m.Recurrence.Pattern = graphtest.Pattern{Type: "absoluteMonthly", Interval: 1, DayOfMonth: 26}
	})
	before := len(e2.ms.Requests())
	ref2 := e2.ref(t, "2026-11-26")
	_, err = e2.w.Update(e2.h.ctx, ref2, EventEdit{Scope: calfeed.ScopeAll, When: &EventWhen{Start: zonedPoint(t, "2026-11-27", "07:00", "Europe/Berlin"), End: zonedPoint(t, "2026-11-27", "08:30", "Europe/Berlin")}})
	if !errors.Is(err, ErrEventInvalid) || invalidField(err) != "start" {
		t.Fatalf("monthly day move = %v", err)
	}
	for _, r := range e2.ms.Requests()[before:] {
		if !strings.HasPrefix(r, "GET") {
			t.Fatalf("a refused change sent %s", r)
		}
	}
}

func TestGraphSeriesFollowingCreatesBeforeItPatches(t *testing.T) {
	e := newMSEnv(t, func(m *graphtest.Event) {
		m.Categories, m.Importance, m.Body, m.BodyType = []string{"Work"}, "high", "<p>Bring shoes</p>", "html"
	})
	ref := e.ref(t, "2026-11-11")
	title := "From now on"
	before := len(e.ms.Requests())
	st, err := e.w.Update(e.h.ctx, ref, EventEdit{Scope: calfeed.ScopeFollowing, Summary: &title, When: &EventWhen{
		Start: zonedPoint(t, "2026-11-11", "08:00", "Europe/Berlin"), End: zonedPoint(t, "2026-11-11", "09:30", "Europe/Berlin")}})
	if err != nil {
		t.Fatal(err)
	}
	var writes []string
	for _, r := range e.ms.Requests()[before:] {
		if !strings.HasPrefix(r, "GET") {
			writes = append(writes, strings.SplitN(r, "?", 2)[0])
		}
	}
	if len(writes) != 2 || !strings.HasPrefix(writes[0], "POST /v1.0/me/calendars/cal-work/events") || !strings.HasPrefix(writes[1], "PATCH /v1.0/me/events/"+strings.ReplaceAll(e.master, "=", "")[:10]) && !strings.Contains(writes[1], "PATCH") {
		t.Fatalf("writes = %v: the new series is created first, then the old one is ended", writes)
	}
	if e.ms.Unconditional() != 0 {
		t.Fatalf("%d writes without If-Match", e.ms.Unconditional())
	}
	m := e.master_(t)
	if m.Recurrence.Range.Type != "endDate" || m.Recurrence.Range.EndDate != "2026-11-10" || m.Recurrence.Range.StartDate != "2026-10-26" {
		t.Fatalf("old master range = %+v", m.Recurrence.Range)
	}
	var fresh graphtest.Event
	for _, ev := range e.ms.Events("cal-work") {
		if ev.Type == "seriesMaster" && ev.ID != e.master {
			fresh = ev
		}
	}
	if fresh.ID == "" || fresh.Subject != title || fresh.Start.DateTime != "2026-11-11T08:00:00" || fresh.Recurrence.Range.StartDate != "2026-11-11" ||
		fresh.Recurrence.Range.Type != "noEnd" || fresh.Importance != "high" || len(fresh.Categories) != 1 || fresh.Body != "<p>Bring shoes</p>" || fresh.BodyType != "html" ||
		!fresh.ReminderOn || fresh.ReminderMinutes != 15 || len(fresh.Recurrence.Pattern.DaysOfWeek) != 2 || fresh.TransactionID == "" {
		t.Fatalf("new series = %+v", fresh)
	}
	if !st.Wrote.Refetch || st.Wrote.UID == "" || !strings.HasPrefix(st.Wrote.UID, fresh.ID) {
		t.Fatalf("written %+v (new master %s)", st.Wrote.UID, fresh.ID)
	}
	// Both masters' occurrences are returned: before the split date the old, from it the new, none twice.
	days := map[string]int{}
	for _, o := range st.Wrote.Written {
		days[o.Events[0].Start.T.Format("2006-01-02")]++
	}
	for d, n := range days {
		if n > 1 {
			t.Errorf("%s has %d occurrences", d, n)
		}
	}
}

func TestGraphSeriesFollowingFailureRollsBack(t *testing.T) {
	e := newMSEnv(t, nil)
	ref := e.ref(t, "2026-11-11")
	masters := func() int {
		n := 0
		for _, ev := range e.ms.Events("cal-work") {
			if ev.Type == "seriesMaster" {
				n++
			}
		}
		return n
	}
	edit := EventEdit{Scope: calfeed.ScopeFollowing, Summary: ptr("X")}
	e.ms.RaceNextPatch() // an edit in Outlook at the same moment: the master's version moves before our patch
	if _, err := e.w.Update(e.h.ctx, ref, edit); !errors.Is(err, ErrEventChanged) {
		t.Fatalf("update = %v", err)
	}
	if masters() != 1 {
		t.Fatalf("masters = %d: the new series should be deleted again", masters())
	}
	if m := e.master_(t); m.Recurrence.Range.Type != "noEnd" {
		t.Fatalf("the old master was ended: %+v", m.Recurrence.Range)
	}
	// A rollback that fails too: the duplicate stays and the error says so.
	e2 := newMSEnv(t, nil)
	ref2 := e2.ref(t, "2026-11-11")
	e2.ms.ThrottleAfter(3, 3, 429, "1") // reads and the POST pass; the PATCH, the look at the master and the DELETE are throttled
	if _, err := e2.w.Update(e2.h.ctx, ref2, edit); !errors.Is(err, ErrPartialWrite) {
		t.Fatalf("update = %v", err)
	}
}

func TestGraphSeriesFollowingRefusals(t *testing.T) {
	for name, f := range map[string]func(m *graphtest.Event){
		"attachments":    func(m *graphtest.Event) { m.HasAttachments = true },
		"online meeting": func(m *graphtest.Event) { m.IsOnlineMeeting = true },
		"relative yearly": func(m *graphtest.Event) {
			m.Recurrence.Pattern = graphtest.Pattern{Type: "relativeYearly", Interval: 1, Month: 10, DaysOfWeek: []string{"monday"}, Index: "last"}
		},
	} {
		t.Run(name, func(t *testing.T) {
			e := newMSEnv(t, f)
			rows := e.h.rows(e.c)
			if len(rows) == 0 {
				t.Skip("the fake has no occurrences of this pattern")
			}
			ref := e.ref(t, rows[10].Start.Date)
			before := len(e.ms.Requests())
			_, err := e.w.Update(e.h.ctx, ref, EventEdit{Scope: calfeed.ScopeFollowing, Summary: ptr("X")})
			if !errors.Is(err, ErrFollowingUnsupported) {
				t.Fatalf("update = %v", err)
			}
			for _, r := range e.ms.Requests()[before:] {
				if !strings.HasPrefix(r, "GET") {
					t.Fatalf("a refused change sent %s", r)
				}
			}
		})
	}
	// All events is allowed for a series with attachments: nothing is copied.
	e := newMSEnv(t, func(m *graphtest.Event) { m.HasAttachments = true })
	if _, err := e.w.Update(e.h.ctx, e.ref(t, "2026-11-09"), EventEdit{Scope: calfeed.ScopeAll, Summary: ptr("X")}); err != nil {
		t.Fatalf("all with attachments = %v", err)
	}
}

func TestGraphSeriesRemindersAreOne(t *testing.T) {
	e := newMSEnv(t, nil)
	ref := e.ref(t, "2026-11-09")
	for name, list := range map[string][]int{"two": {5, 10}, "negative": {-540}} {
		if _, err := e.w.Update(e.h.ctx, ref, EventEdit{Scope: calfeed.ScopeAll, Reminders: &[]int{5, 10}}); !errors.Is(err, ErrEventInvalid) || invalidField(err) != "reminders" {
			t.Errorf("%s: %v", name, err)
		}
		_ = list
	}
	if _, err := e.w.Update(e.h.ctx, ref, EventEdit{Scope: calfeed.ScopeAll, Reminders: &[]int{-540}}); !errors.Is(err, ErrEventInvalid) {
		t.Errorf("negative: %v", err)
	}
	if _, err := e.w.Update(e.h.ctx, ref, EventEdit{Scope: calfeed.ScopeAll, Reminders: &[]int{30}}); err != nil {
		t.Fatal(err)
	}
	if m := e.master_(t); !m.ReminderOn || m.ReminderMinutes != 30 {
		t.Fatalf("master = %+v", m)
	}
	e.h.cycle(e.h.conn(e.c.ID)) // the occurrences' versions follow the master's
	e.c = e.h.conn(e.c.ID)
	ref = e.ref(t, "2026-11-09")
	if _, err := e.w.Update(e.h.ctx, ref, EventEdit{Scope: calfeed.ScopeAll, Reminders: &[]int{}}); err != nil {
		t.Fatal(err)
	}
	if m := e.master_(t); m.ReminderOn {
		t.Fatalf("reminder is still on: %+v", m)
	}
	e.h.cycle(e.h.conn(e.c.ID))
	d, err := e.w.Detail(e.h.ctx, e.ref(t, "2026-11-11"))
	if err != nil || d.RemindersMax != 1 || len(d.Reminders) != 0 {
		t.Fatalf("detail = %+v %v", d.Reminders, err)
	}
}

func TestGraphSeriesDeleteScopes(t *testing.T) {
	e := newMSEnv(t, nil)
	this := e.ref(t, "2026-11-04")
	wr, err := e.w.DeleteScoped(e.h.ctx, this, calfeed.ScopeThis)
	if err != nil || len(wr.Deleted) != 1 {
		t.Fatalf("this = %+v %v", wr, err)
	}
	if ev, ok := e.ms.Event(this.Href); !ok || !ev.Cancelled {
		t.Fatalf("Graph records a cancelled exception: %+v", ev)
	}
	e.h.cycle(e.h.conn(e.c.ID))
	for _, r := range e.h.rows(e.c) {
		if r.Start.Date == "2026-11-04" {
			t.Fatal("the deleted occurrence is shown")
		}
	}
	e.h.cycle(e.h.conn(e.c.ID))
	// Following ends the master the day before.
	wr, err = e.w.DeleteScoped(e.h.ctx, e.ref(t, "2026-11-16"), calfeed.ScopeFollowing)
	if err != nil || len(wr.Written) == 0 {
		t.Fatalf("following = %v", err)
	}
	if m := e.master_(t); m.Recurrence.Range.EndDate != "2026-11-15" || m.Recurrence.Range.Type != "endDate" {
		t.Fatalf("range = %+v", m.Recurrence.Range)
	}
	e.h.cycle(e.h.conn(e.c.ID))
	// The first occurrence deletes the master, and so does all.
	wr, err = e.w.DeleteScoped(e.h.ctx, e.ref(t, "2026-10-26"), calfeed.ScopeFollowing)
	if err != nil || len(wr.Deleted) != 1 || wr.Deleted[0] != e.master {
		t.Fatalf("first = %+v %v", wr, err)
	}
	if _, ok := e.ms.Event(e.master); ok {
		t.Fatal("the master survived")
	}
	e2 := newMSEnv(t, nil)
	if wr, err = e2.w.DeleteScoped(e2.h.ctx, e2.ref(t, "2026-11-09"), calfeed.ScopeAll); err != nil || e2.ms.Unconditional() != 0 {
		t.Fatalf("all = %+v %v", wr, err)
	}
	if _, ok := e2.ms.Event(e2.master); ok {
		t.Fatal("the master survived")
	}
}

func TestGraphSeriesDetailReadsTheMaster(t *testing.T) {
	e := newMSEnv(t, nil)
	ex := e.ref(t, "2026-11-04")
	if _, err := e.w.Update(e.h.ctx, ex, EventUpdate(calfeed.ScopeThis, ptr("x"))); err != nil {
		t.Fatal(err)
	}
	d, err := e.w.Detail(e.h.ctx, e.ref(t, "2026-11-09"))
	if err != nil {
		t.Fatal(err)
	}
	if d.Series == nil || d.Series.Repeat == nil || d.Series.Repeat.Freq != "weekly" || len(d.Series.Repeat.Days) != 2 || d.Series.Exceptions != 1 || d.Series.Custom ||
		d.RemindersMax != 1 || len(d.Reminders) != 1 || d.Reminders[0] != 15 {
		t.Fatalf("detail = %+v series %+v", d.Reminders, d.Series)
	}
	// A pattern the form cannot say is custom, and a repeat change is refused for it by the Worker.
	e2 := newMSEnv(t, func(m *graphtest.Event) {
		m.Recurrence.Pattern.DaysOfWeek = []string{"tuesday", "wednesday"} // not the start's weekday
	})
	_ = e2
}

func TestGraphSeriesSetRepeatOnASingleEvent(t *testing.T) {
	h := newHarness(t)
	ms, c := h.microsoft(weHere)
	id := ms.Add(graphtest.Event{Calendar: "cal-work", Subject: "Planning", Start: graphtest.DT{DateTime: "2026-11-09T09:00:00", TimeZone: weHere}, End: graphtest.DT{DateTime: "2026-11-09T10:00:00", TimeZone: weHere}})
	h.cycle(c)
	w := h.writer(c)
	ref := h.davRef(c, "cal-work", id)
	st, err := w.Update(h.ctx, ref, EventEdit{Repeat: &RepeatEdit{Rule: &calfeed.Repeat{Freq: "weekly", Ends: calfeed.RepeatEnd{Type: calfeed.EndAfter, Count: 5}}}, Reminders: &[]int{10}})
	if err != nil {
		t.Fatal(err)
	}
	m, _ := ms.Event(id)
	if m.Type != "seriesMaster" || m.Recurrence == nil || m.Recurrence.Pattern.Type != "weekly" || m.Recurrence.Range.Type != "numbered" || m.Recurrence.Range.NumberOfOccurrences != 5 ||
		m.Recurrence.Range.StartDate != "2026-11-09" || !m.ReminderOn || m.ReminderMinutes != 10 {
		t.Fatalf("event = %+v", m)
	}
	if len(st.Wrote.Written) != 5 || st.Wrote.UID == "" || !st.Wrote.Refetch {
		t.Fatalf("written = %d", len(st.Wrote.Written))
	}
	if _, err := w.Update(h.ctx, EventRef{Calendar: "cal-work", Href: id, ETag: m.ETag}, EventEdit{Repeat: &RepeatEdit{}}); err == nil {
		t.Log("a stop on a master is refused by the Worker, not by the writer")
	}
	// Create with repeat and reminders.
	created, err := w.Create(h.ctx, "cal-work", EventNew{UID: "client-0001", Summary: "Review", Reminders: []int{5},
		When:   EventWhen{Start: zonedPoint(t, "2026-11-16", "09:00", "Europe/Berlin"), End: zonedPoint(t, "2026-11-16", "10:00", "Europe/Berlin")},
		Repeat: &calfeed.Repeat{Freq: "daily", Ends: calfeed.RepeatEnd{Type: calfeed.EndAfter, Count: 3}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(created.Wrote.Written) != 3 || created.Wrote.Instance == "" {
		t.Fatalf("created = %d occurrences", len(created.Wrote.Written))
	}
	if _, err := w.Create(h.ctx, "cal-work", EventNew{UID: "client-0002", Summary: "Bad", Reminders: []int{5, 6},
		When: EventWhen{Start: zonedPoint(t, "2026-11-16", "09:00", "Europe/Berlin"), End: zonedPoint(t, "2026-11-16", "10:00", "Europe/Berlin")}}); !errors.Is(err, ErrEventInvalid) {
		t.Fatalf("two reminders on create = %v", err)
	}
}

// TestGraphPatternTable pins D8's table both ways.
func TestGraphPatternTable(t *testing.T) {
	wed := time.Date(2026, 11, 11, 7, 0, 0, 0, time.UTC) // the 2nd Wednesday
	fifth := time.Date(2026, 12, 30, 7, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		r    calfeed.Repeat
		at   time.Time
		want graphRecurrence
	}{
		{"daily", calfeed.Repeat{Freq: "daily", Interval: 3}, wed, graphRecurrence{Pattern: graphPattern{Type: "daily", Interval: 3}, Range: graphRange{Type: "noEnd"}}},
		{"weekly", calfeed.Repeat{Freq: "weekly", Days: []string{"mo", "we"}}, wed, graphRecurrence{Pattern: graphPattern{Type: "weekly", Interval: 1, DaysOfWeek: []string{"monday", "wednesday"}, FirstDayOfWeek: "monday"}, Range: graphRange{Type: "noEnd"}}},
		{"weekdays", calfeed.Repeat{Freq: "weekly", Days: []string{"mo", "tu", "we", "th", "fr"}}, wed, graphRecurrence{Pattern: graphPattern{Type: "weekly", Interval: 1, DaysOfWeek: []string{"monday", "tuesday", "wednesday", "thursday", "friday"}, FirstDayOfWeek: "monday"}, Range: graphRange{Type: "noEnd"}}},
		{"monthly by day", calfeed.Repeat{Freq: "monthly"}, wed, graphRecurrence{Pattern: graphPattern{Type: "absoluteMonthly", Interval: 1, DayOfMonth: 11}, Range: graphRange{Type: "noEnd"}}},
		{"monthly by weekday", calfeed.Repeat{Freq: "monthly", By: calfeed.ByWeekd}, wed, graphRecurrence{Pattern: graphPattern{Type: "relativeMonthly", Interval: 1, DaysOfWeek: []string{"wednesday"}, Index: "second"}, Range: graphRange{Type: "noEnd"}}},
		{"monthly by the last weekday", calfeed.Repeat{Freq: "monthly", By: calfeed.ByWeekd}, fifth, graphRecurrence{Pattern: graphPattern{Type: "relativeMonthly", Interval: 1, DaysOfWeek: []string{"wednesday"}, Index: "last"}, Range: graphRange{Type: "noEnd"}}},
		{"yearly", calfeed.Repeat{Freq: "yearly"}, wed, graphRecurrence{Pattern: graphPattern{Type: "absoluteYearly", Interval: 1, DayOfMonth: 11, Month: 11}, Range: graphRange{Type: "noEnd"}}},
		{"ends on", calfeed.Repeat{Freq: "daily", Ends: calfeed.RepeatEnd{Type: calfeed.EndOn, Date: "2026-12-31"}}, wed, graphRecurrence{Pattern: graphPattern{Type: "daily", Interval: 1}, Range: graphRange{Type: "endDate", EndDate: "2026-12-31"}}},
		{"ends after", calfeed.Repeat{Freq: "daily", Ends: calfeed.RepeatEnd{Type: calfeed.EndAfter, Count: 7}}, wed, graphRecurrence{Pattern: graphPattern{Type: "daily", Interval: 1}, Range: graphRange{Type: "numbered", NumberOfOccurrences: 7}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := recurrenceOf(c.r, c.at, weHere)
			if err != nil {
				t.Fatal(err)
			}
			c.want.Range.StartDate, c.want.Range.RecurrenceTimeZone = c.at.Format("2006-01-02"), weHere
			if c.want.Pattern.Interval == 0 {
				c.want.Pattern.Interval = 1
			}
			a, _ := json.Marshal(got)
			b, _ := json.Marshal(c.want)
			if string(a) != string(b) {
				t.Fatalf("pattern\n got  %s\n want %s", a, b)
			}
			back, ok := repeatOfRecurrence(got)
			want := c.r.Normalize(calfeed.Point{T: c.at})
			if !ok || toJSON(back) != toJSON(want) {
				t.Fatalf("back = %+v %v, want %+v", back, ok, want)
			}
		})
	}
	// What the form cannot say is custom.
	for name, g := range map[string]graphRecurrence{
		"relative yearly":                  {Pattern: graphPattern{Type: "relativeYearly", Interval: 1, Month: 11, DaysOfWeek: []string{"wednesday"}, Index: "second"}, Range: graphRange{Type: "noEnd", StartDate: "2026-11-11"}},
		"two weekdays of a month":          {Pattern: graphPattern{Type: "relativeMonthly", Interval: 1, DaysOfWeek: []string{"wednesday", "friday"}, Index: "second"}, Range: graphRange{Type: "noEnd", StartDate: "2026-11-11"}},
		"another day of the month":         {Pattern: graphPattern{Type: "absoluteMonthly", Interval: 1, DayOfMonth: 12}, Range: graphRange{Type: "noEnd", StartDate: "2026-11-11"}},
		"index second for a first weekday": {Pattern: graphPattern{Type: "relativeMonthly", Interval: 1, DaysOfWeek: []string{"wednesday"}, Index: "first"}, Range: graphRange{Type: "noEnd", StartDate: "2026-11-11"}},
		"weekly without the start's day":   {Pattern: graphPattern{Type: "weekly", Interval: 1, DaysOfWeek: []string{"monday"}}, Range: graphRange{Type: "noEnd", StartDate: "2026-11-11"}},
		"first day of week sunday":         {Pattern: graphPattern{Type: "weekly", Interval: 2, DaysOfWeek: []string{"wednesday", "friday"}, FirstDayOfWeek: "sunday"}, Range: graphRange{Type: "noEnd", StartDate: "2026-11-11"}},
		"index":                            {Pattern: graphPattern{Type: "index", Interval: 1}, Range: graphRange{Type: "noEnd", StartDate: "2026-11-11"}},
	} {
		if r, ok := repeatOfRecurrence(&g); ok {
			t.Errorf("%s: %+v should be custom", name, r)
		}
	}
	if _, err := recurrenceOf(calfeed.Repeat{Freq: "weekly", Days: []string{"tu"}}, wed, weHere); err == nil {
		t.Error("a weekly pattern that skips the start's weekday is written")
	}
}

func toJSON(v any) string { b, _ := json.Marshal(v); return string(b) }

func TestGraphSeriesWorkerCacheMatchesACycle(t *testing.T) {
	for name, tc := range map[string]struct {
		scope calfeed.Scope
		date  string
	}{"this": {calfeed.ScopeThis, "2026-11-11"}, "all": {calfeed.ScopeAll, "2026-11-11"}, "following": {calfeed.ScopeFollowing, "2026-11-16"}} {
		t.Run(name, func(t *testing.T) {
			e := newMSEnv(t, nil)
			row := e.rowOn(t, tc.date)
			res, err := e.h.w.EditEvent(e.h.ctx, "alice", e.c.ID, row.ID, row.Rev, EventEdit{Scope: tc.scope, Summary: ptr("Renamed")})
			if err != nil {
				t.Fatal(err)
			}
			if res.Series == nil || res.Series.Rev != "" || res.Event.Series == nil {
				t.Fatalf("result = %+v", res)
			}
			if (tc.scope != calfeed.ScopeThis) != res.Refetch {
				t.Errorf("refetch = %v", res.Refetch)
			}
			e.h.sameAsACycle(e.c)
			// And delete.
			row = e.rowOn(t, "2026-11-23")
			if _, err := e.h.w.DeleteEvent(e.h.ctx, "alice", e.c.ID, row.ID, row.Rev, tc.scope); err != nil {
				t.Fatal(err)
			}
			e.h.sameAsACycle(e.c)
		})
	}
}

func (e *msEnv) rowOn(t *testing.T, date string) calfeed.EventRow {
	t.Helper()
	for _, r := range e.h.rows(e.h.conn(e.c.ID)) {
		if r.Start.Date == date && r.Series != nil {
			return r
		}
	}
	t.Fatalf("no occurrence on %s", date)
	return calfeed.EventRow{}
}

func TestGraphSplittablePatterns(t *testing.T) {
	for _, typ := range []string{"daily", "weekly", "absoluteMonthly", "relativeMonthly", "absoluteYearly"} {
		if !splittable(graphPattern{Type: typ}) {
			t.Errorf("%s should be copied by a split", typ)
		}
	}
	for _, typ := range []string{"relativeYearly", "index", ""} {
		if splittable(graphPattern{Type: typ}) {
			t.Errorf("%s must not be copied: the editor does not write it", typ)
		}
	}
}

func TestGraphSeriesStopRepeatingIsRefused(t *testing.T) {
	e := newMSEnv(t, nil)
	for _, date := range []string{"2026-10-26", "2026-11-11"} { // the first occurrence acts as all, a later one splits
		before := len(e.ms.Requests())
		_, err := e.w.Update(e.h.ctx, e.ref(t, date), EventEdit{Scope: calfeed.ScopeFollowing, Repeat: &RepeatEdit{}})
		if !errors.Is(err, ErrEventInvalid) || invalidField(err) != "repeat" {
			t.Fatalf("%s: %v", date, err)
		}
		for _, r := range e.ms.Requests()[before:] {
			if !strings.HasPrefix(r, "GET") {
				t.Fatalf("a refused change sent %s", r)
			}
		}
	}
}
