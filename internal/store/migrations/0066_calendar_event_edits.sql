-- Editing the events of a connected calendar (OpenSpec change
-- calendar-event-edits, design D2). Side tables, so an older binary never reads
-- them and calendar_events and calsync_connections keep their exact columns.
-- Server data, never an op.

-- Where each cached occurrence of a managed calendar lives on the remote
-- account, and the version last read or written.
CREATE TABLE IF NOT EXISTS calendar_event_remote (
  subscription_id TEXT NOT NULL REFERENCES calendar_subscriptions(id) ON DELETE CASCADE,
  uid             TEXT NOT NULL,
  instance        TEXT NOT NULL,
  event_id        TEXT NOT NULL,            -- the id the events route serves
  href            TEXT NOT NULL,            -- CalDAV object address, or the Graph event id
  etag            TEXT NOT NULL DEFAULT '', -- the version last read or written
  locked          TEXT NOT NULL DEFAULT '', -- '' editable, else recurring | has_guests | not_organizer | no_etag
  PRIMARY KEY (subscription_id, uid, instance)
);
CREATE UNIQUE INDEX IF NOT EXISTS calendar_event_remote_id ON calendar_event_remote(subscription_id, event_id);

-- A row means "Edit events in OpenTodo" is on for the connection.
CREATE TABLE IF NOT EXISTS calsync_event_edit (
  connection_id TEXT PRIMARY KEY REFERENCES calsync_connections(id) ON DELETE CASCADE
);

-- Managed calendars have no remote refs yet: the next cycle reads them once more.
UPDATE calendar_subscriptions SET etag = ''
  WHERE id IN (SELECT subscription_id FROM calsync_calendars);
