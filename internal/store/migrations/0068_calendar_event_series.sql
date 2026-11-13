-- The series an occurrence of a managed calendar belongs to, so the events route can say
-- "this repeats" and the write routes can ask for a scope. Server data, never an op.
-- (calendar-event-series D2)
CREATE TABLE IF NOT EXISTS calendar_event_series (
  subscription_id TEXT NOT NULL REFERENCES calendar_subscriptions(id) ON DELETE CASCADE,
  uid             TEXT NOT NULL,            -- the cached uid (the CalDAV UID, or a Microsoft occurrence id)
  series_id       TEXT NOT NULL,            -- hash16: what the client groups occurrences by
  rrule           TEXT NOT NULL DEFAULT '', -- the master's RRULE as read ('' for Microsoft)
  repeat          TEXT NOT NULL DEFAULT '', -- JSON of the structured repeat, '' when the rule is custom
  first_instance  TEXT NOT NULL DEFAULT '', -- instance key of the series' DTSTART ('' unknown)
  PRIMARY KEY (subscription_id, uid)
);
CREATE INDEX IF NOT EXISTS calendar_event_series_id ON calendar_event_series(subscription_id, series_id);

-- Occurrences cached before this change were locked as 'recurring' and have no series row:
-- read every managed calendar once more.
UPDATE calendar_subscriptions SET etag = ''
  WHERE id IN (SELECT subscription_id FROM calsync_calendars);
