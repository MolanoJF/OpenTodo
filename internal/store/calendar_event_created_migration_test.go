package store

import (
	"context"
	"io/fs"
	"strconv"
	"strings"
	"testing"
)

// TestMigrationCalendarEventCreated opens a database from before
// calendar-event-editor: the table appears, the migration runs again without
// losing rows, the phase-1 tables keep working, and deleting a connection
// deletes its records.
func TestMigrationCalendarEventCreated(t *testing.T) {
	ctx := context.Background()
	var name string
	var version int
	entries, _ := fs.ReadDir(migrations, "migrations")
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), "_calendar_event_created.sql") {
			name = e.Name()
			version, _ = strconv.Atoi(strings.SplitN(name, "_", 2)[0])
		}
	}
	if name == "" {
		t.Fatal("no *_calendar_event_created.sql migration embedded")
	}
	dir := t.TempDir()
	db, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		"DROP TABLE calendar_event_created",
		"PRAGMA user_version = " + strconv.Itoa(version-1),
		`INSERT INTO users(id,email,name,password_hash,role,created_at) VALUES('u1','a@b.c','','x','member',1)`,
		`INSERT INTO calsync_connections(id,user_id,provider,server_url,next_sync_at,created_at,updated_at) VALUES('c1','u1','caldav','https://x.test/dav',0,1,1)`,
		`INSERT INTO calendar_subscriptions(id,user_id,url,name,color,enabled,interval_seconds,next_fetch_at,etag,created_at,updated_at) VALUES('m1','u1','https://x.test/cal','M','',1,900,0,'ctag-1',1,1)`,
		`INSERT INTO calsync_calendars(subscription_id,connection_id,calendar_url) VALUES('m1','c1','https://x.test/cal')`,
		`INSERT INTO calendar_event_remote(subscription_id,uid,instance,event_id,href) VALUES('m1','u','i','e1','/a.ics')`,
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
		t.Fatal("phase-1 rows lost")
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO calendar_event_created(connection_id,client_id,subscription_id,uid,created_at) VALUES('c1','client-0001','m1','client-0001',5)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO calendar_event_created(connection_id,client_id,subscription_id,uid,created_at) VALUES('c1','client-0001','m1','x',6)`); err == nil {
		t.Fatal("duplicate client id accepted")
	}
	body, _ := fs.ReadFile(migrations, "migrations/"+name)
	if _, err := db.ExecContext(ctx, string(body)); err != nil {
		t.Fatalf("second run: %v", err)
	}
	if count("calendar_event_created") != 1 {
		t.Fatal("second run lost rows")
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM calsync_connections WHERE id = 'c1'`); err != nil {
		t.Fatal(err)
	}
	if count("calendar_event_created") != 0 {
		t.Fatal("record survived the cascade")
	}
}
