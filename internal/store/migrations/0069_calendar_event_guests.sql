-- Who is invited to an occurrence of a managed calendar, how the user answered, and the
-- join link, so the events route can summarise them and a popover can list them without
-- asking the calendar. Server data, never an op. Personal data: guest names and
-- addresses (calendar-event-guests D12).
CREATE TABLE IF NOT EXISTS calendar_event_guests (
  subscription_id TEXT NOT NULL REFERENCES calendar_subscriptions(id) ON DELETE CASCADE,
  uid             TEXT NOT NULL,              -- as calendar_event_remote
  instance        TEXT NOT NULL,              -- as calendar_event_remote
  organizer       TEXT NOT NULL DEFAULT '',   -- JSON {"name","email"}, '' when none is named
  is_organizer    INTEGER NOT NULL DEFAULT 0, -- the user organises it
  you             TEXT NOT NULL DEFAULT '',   -- needs-action | accepted | tentative | declined | '' (not a guest, or unknown)
  guest_count     INTEGER NOT NULL DEFAULT 0, -- attendees other than the user, rooms included, uncapped
  attendees       TEXT NOT NULL DEFAULT '',   -- JSON array of {name,email,role,response,you}, at most 100
  conference_url  TEXT NOT NULL DEFAULT '',   -- https, validated (D10)
  conference_kind TEXT NOT NULL DEFAULT '',   -- meet | teams | zoom | webex | other
  PRIMARY KEY (subscription_id, uid, instance)
);

-- Occurrences cached before this change have no guest rows and carry the old locks:
-- read every managed calendar once more.
UPDATE calendar_subscriptions SET etag = ''
  WHERE id IN (SELECT subscription_id FROM calsync_calendars);
