// New event without a place on the grid (OpenSpec change calendar-event-editor D10, D12): the palette's "New event"
// and Shift+Q. The editor opens in create mode at the next free half hour of today, or at 09:00 on another day.
// Loaded on demand (the palette imports it dynamically), so the entry chunk holds the command and nothing else.
import { setShowEvents, showEvents } from '../../lib/calendarFeeds'
import { logicalToday, viewerZone } from '../../lib/dates'
import { eventBusyOn, nextHalfHour } from '../../lib/eventCreate'
import { busyOn } from '../../lib/slots'
import { navigate, type Route } from '../router'
import { type EventPlace, openEventEditor } from './eventEditorStore'

const DEFAULT_START = 9 * 60
const LENGTH = 30

/** Where a new event goes on `date`: today's next free half hour (09:00 tomorrow when none is left), else 09:00. */
export async function eventPlaceOn(date: string, now = new Date()): Promise<EventPlace> {
  const zone = viewerZone()
  if (date === logicalToday(undefined, now.getTime())) {
    const busy = [...(await busyOn(date, zone)), ...(await eventBusyOn(date, zone))]
    const next = nextHalfHour(now, busy)
    return { date: next.date, start: next.start, end: next.start + LENGTH }
  }
  return { date, start: DEFAULT_START, end: DEFAULT_START + LENGTH }
}

/** Shift+Q: the editor for the focused day. */
export async function newEventOn(date: string, now = new Date()): Promise<void> {
  openEventEditor({ mode: 'create', place: await eventPlaceOn(date, now) })
}

/** The palette's New event: shows calendars if this device hides them, the Day view unless Day or Week is on, then the editor. */
export async function newEventNow(route: Route, now = new Date()): Promise<void> {
  const place = await eventPlaceOn(logicalToday(undefined, now.getTime()), now)
  if (!(await showEvents())) await setShowEvents(true)
  if (route.view !== 'day' && route.view !== 'week') navigate({ view: 'day', ...(place.date !== logicalToday(undefined, now.getTime()) ? { date: place.date } : {}) })
  else if (place.date !== logicalToday(undefined, now.getTime())) navigate({ view: route.view, date: place.date })
  openEventEditor({ mode: 'create', place })
}

