package store

import (
	"context"
	"io/fs"
	"strconv"
	"strings"
	"testing"
)

// TestMigrationCalendarEventSeries opens a database from before
// calendar-event-series: the table appears, the migration runs again without
// losing rows, the phase 1 and 2 tables and queries keep working, the managed
// calendars are read again, and deleting a subscription deletes its series rows.
func TestMigrationCalendarEventSeries(t *testing.T) {
	ctx := context.Background()
	var name string
	var version int
	entries, _ := fs.ReadDir(migrations, "migrations")
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), "_calendar_event_series.sql") {
			name = e.Name()
			version, _ = strconv.Atoi(strings.SplitN(name, "_", 2)[0])
		}
	}
	if name == "" {
		t.Fatal("no *_calendar_event_series.sql migration embedded")
	}
	dir := t.TempDir()
	db, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		"DROP TABLE calendar_event_series",
		"PRAGMA user_version = " + strconv.Itoa(version-1),
		`INSERT INTO users(id,email,name,password_hash,role,created_at) VALUES('u1','a@b.c','','x','member',1)`,
		`INSERT INTO calsync_connections(id,user_id,provider,server_url,next_sync_at,created_at,updated_at) VALUES('c1','u1','caldav','https://x.test/dav',0,1,1)`,
		`INSERT INTO calendar_subscriptions(id,user_id,url,name,color,enabled,interval_seconds,next_fetch_at,etag,created_at,updated_at) VALUES('m1','u1','https://x.test/cal','M','',1,900,0,'ctag-1',1,1)`,
		`INSERT INTO calendar_subscriptions(id,user_id,url,name,color,enabled,interval_seconds,next_fetch_at,etag,created_at,updated_at) VALUES('f1','u1','https://x.test/feed.ics','F','',1,900,0,'"feed-1"',1,1)`,
		`INSERT INTO calsync_calendars(subscription_id,connection_id,calendar_url) VALUES('m1','c1','https://x.test/cal')`,
		`INSERT INTO calendar_event_remote(subscription_id,uid,instance,event_id,href,locked) VALUES('m1','u','i','e1','/a.ics','recurring')`,
	} {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	db.Close()
	db, err = Open(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer db.Close()
	var v int
	if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&v); err != nil || v != LatestVersion() {
		t.Fatalf("user_version = %d, want %d (%v)", v, LatestVersion(), err)
	}
	count := func(table string) int {
		var n int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if count("calendar_event_remote") != 1 {
		t.Fatal("phase 1 rows lost")
	}
	etag := func(id string) string {
		var e string
		if err := db.QueryRowContext(ctx, `SELECT etag FROM calendar_subscriptions WHERE id = ?`, id).Scan(&e); err != nil {
			t.Fatal(err)
		}
		return e
	}
	if etag("m1") != "" {
		t.Errorf("the managed calendar keeps its version %q", etag("m1"))
	}
	if etag("f1") != `"feed-1"` {
		t.Errorf("a feed lost its version: %q", etag("f1"))
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO calendar_event_series(subscription_id,uid,series_id,rrule,repeat,first_instance) VALUES('m1','u','s1','FREQ=DAILY','{"freq":"daily"}','20261001T050000Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO calendar_event_series(subscription_id,uid,series_id) VALUES('m1','u','s2')`); err == nil {
		t.Fatal("duplicate uid accepted")
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO calendar_event_series(subscription_id,uid,series_id) VALUES('m1','w','s1')`); err != nil {
		t.Fatalf("defaults: %v", err)
	}
	// The migration is idempotent: a second run loses no row (and reads the calendars again).
	if _, err := db.ExecContext(ctx, `UPDATE calendar_subscriptions SET etag = 'ctag-2' WHERE id = 'm1'`); err != nil {
		t.Fatal(err)
	}
	body, _ := fs.ReadFile(migrations, "migrations/"+name)
	if _, err := db.ExecContext(ctx, string(body)); err != nil {
		t.Fatalf("second run: %v", err)
	}
	if count("calendar_event_series") != 2 {
		t.Fatal("second run lost rows")
	}
	if etag("m1") != "" {
		t.Errorf("second run keeps the version %q", etag("m1"))
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM calendar_subscriptions WHERE id = 'm1'`); err != nil {
		t.Fatal(err)
	}
	if count("calendar_event_series") != 0 || count("calendar_event_remote") != 0 {
		t.Fatal("rows survived the cascade")
	}
}
