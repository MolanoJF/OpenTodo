-- The events OpenTodo created for a client id, so a create sent twice makes one event and
-- an event deleted since is not made again. Server data, never an op. (calendar-event-editor D2)
CREATE TABLE IF NOT EXISTS calendar_event_created (
  connection_id   TEXT NOT NULL REFERENCES calsync_connections(id) ON DELETE CASCADE,
  client_id       TEXT NOT NULL,            -- the client's id for the event (8 to 64 of A-Za-z0-9_-)
  subscription_id TEXT NOT NULL,            -- the shown calendar it went into (no FK: a hidden calendar keeps the record)
  uid             TEXT NOT NULL,            -- the cached uid: the client id for CalDAV, the Graph id for Microsoft
  created_at      INTEGER NOT NULL,         -- Unix ms; rows older than 30 days are deleted on the next create
  PRIMARY KEY (connection_id, client_id)
);
