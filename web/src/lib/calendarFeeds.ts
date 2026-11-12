// Events of subscribed calendars on this device (OpenSpec change calendar-feeds,
// design D8). Server data, never ops: fetched for a window around what a calendar
// view shows (Day, Week, Plan, Month and Year, calendar-events-all-views), kept in
// Dexie for offline use, refreshed when stale, on reconnect and when the server says
// the calendars changed. A window wider than one request is fetched in pieces.
import { api } from './api'
import { addDaysISO, daysBetweenISO, logicalToday } from './dates'
import { db, getMeta, setMeta } from './db'
import { resettable } from './moduleState'
import type { CalendarEvent, CalendarEventChange, CalendarEventDraft, CalendarSubscription, EventCommand } from './types'

const WINDOW_KEY = 'calendar.events.window'
const SHOW_KEY = 'calendar.showEvents'
/** A cache older than this is refreshed when a view asks for it. */
export const STALE_MS = 10 * 60 * 1000
/** Days fetched either side of the asked range. */
export const PAD_DAYS = 28
/** The server's bound on one request. */
export const MAX_DAYS = 92
/** The widest window a device fetches (a year, as the Year view shows, and a week). */
export const MAX_WINDOW_DAYS = 373

export interface EventsWindow {
  from: string
  to: string
  fetchedAt: number
}

export interface DayRange {
  from: string
  to: string
}

// ---- queued event edits over fetched rows (OpenSpec change calendar-event-edits D6) ----

/** The event with an edit applied: the fields the change names, nothing else. */
export function applyEventPatch(ev: CalendarEvent, patch: CalendarEventChange): CalendarEvent {
  return {
    ...ev,
    ...(patch.summary !== undefined ? { summary: patch.summary } : {}),
    ...(patch.start && patch.end ? { start: patch.start, end: patch.end, allDay: patch.allDay ?? ev.allDay } : {}),
    ...(patch.location !== undefined ? { location: patch.location } : {}),
    ...(patch.free !== undefined ? { free: patch.free } : {}),
  }
}

/** The id of an event drawn before the calendar has it (calendar-event-editor D3). */
export const PENDING_PREFIX = 'pending:'
export const isPendingId = (id: string): boolean => id.startsWith(PENDING_PREFIX)

/** The row the grid draws for a queued create: no `rev`, editable and deletable until the calendar answers. */
export function pendingRowOf(d: CalendarEventDraft): CalendarEvent {
  return {
    id: PENDING_PREFIX + d.clientId, subscriptionId: d.subscriptionId, uid: d.clientId, summary: d.summary, location: d.location,
    start: d.start, end: d.end, allDay: d.allDay, free: d.free, access: { update: true, delete: true },
  }
}

/** The draft with a change merged in (later fields win); a change of the times always carries both points. */
export function applyDraftPatch(d: CalendarEventDraft, patch: CalendarEventChange): CalendarEventDraft {
  return {
    ...d,
    ...(patch.summary !== undefined ? { summary: patch.summary } : {}),
    ...(patch.start && patch.end ? { start: patch.start, end: patch.end, allDay: patch.allDay ?? d.allDay } : {}),
    ...(patch.location !== undefined ? { location: patch.location } : {}),
    ...(patch.description !== undefined ? { description: patch.description } : {}),
    ...(patch.free !== undefined ? { free: patch.free } : {}),
  }
}

/** Whether a change of `ev` changes nothing: a description is never on the row, so it always counts. */
export const isNoChange = (ev: CalendarEvent, patch: CalendarEventChange): boolean =>
  patch.description === undefined && Object.keys(eventPatchBetween(ev, applyEventPatch(ev, patch))).length === 0

const pointKey = (p: CalendarEvent['start']) => `${p.date}|${p.time ?? ''}|${p.tz ?? ''}`

/** The edit that turns `from` into `to` (empty when they are the same): the Undo of a change and the rest of a delete. */
export function eventPatchBetween(from: CalendarEvent, to: CalendarEvent): CalendarEventChange {
  const out: CalendarEventChange = {}
  if (from.summary !== to.summary) out.summary = to.summary
  if (from.location !== to.location) out.location = to.location
  if (from.free !== to.free) out.free = to.free
  if (pointKey(from.start) !== pointKey(to.start) || pointKey(from.end) !== pointKey(to.end) || from.allDay !== to.allDay) {
    out.start = to.start
    out.end = to.end
    out.allDay = to.allDay
  }
  return out
}

/** Draws the queued edits over fetched rows, matched by event id: a queued delete hides the event. */
export function overlayQueued(events: Map<string, CalendarEvent>, commands: readonly EventCommand[]): void {
  for (const cmd of commands) {
    if ('create' in cmd.patch) { // a queued create: the pending row, with what waits in `next`
      const row = pendingRowOf(cmd.patch.create)
      if (!cmd.next) events.set(row.id, row)
      else if (!('delete' in cmd.next)) events.set(row.id, applyEventPatch(row, cmd.next))
      continue
    }
    const ev = events.get(cmd.eventId)
    if (!ev) continue
    if ('delete' in cmd.patch) { events.delete(cmd.eventId); continue }
    const next = cmd.next
    if (next && 'delete' in next) { events.delete(cmd.eventId); continue }
    events.set(cmd.eventId, applyEventPatch(applyEventPatch(ev, cmd.patch), next ?? {}))
  }
}

const afterRefresh = new Set<() => void>()
resettable('calendarFeedsRefreshHooks', () => afterRefresh.clear())

/** Calls `fn` after every refresh that replaced the cache (the event outbox sends what waits then). */
export function onEventsRefreshed(fn: () => void): () => void {
  afterRefresh.add(fn)
  return () => { afterRefresh.delete(fn) }
}

export function cachedWindow(): Promise<EventsWindow | null> {
  return getMeta<EventsWindow | null>(WINDOW_KEY, null)
}

/** True when the cached window holds every day of the range. */
export function covers(win: EventsWindow | null, range: DayRange): boolean {
  return !!win && win.from <= range.from && win.to >= range.to
}

/**
 * The range to fetch: the asked range padded, within the server's bound; a range wider
 * than one request (stacked months, a year) is fetched as it is, up to MAX_WINDOW_DAYS.
 */
export function fetchRange(range: DayRange): DayRange {
  if (daysBetweenISO(range.from, range.to) + 1 > MAX_DAYS) {
    const last = addDaysISO(range.from, MAX_WINDOW_DAYS - 1)
    return { from: range.from, to: range.to < last ? range.to : last }
  }
  let from = addDaysISO(range.from, -PAD_DAYS)
  let to = addDaysISO(range.to, PAD_DAYS)
  if (daysBetweenISO(from, to) + 1 > MAX_DAYS) {
    const extra = Math.max(0, MAX_DAYS - (daysBetweenISO(range.from, range.to) + 1))
    from = addDaysISO(range.from, -Math.floor(extra / 2))
    to = addDaysISO(range.to, Math.ceil(extra / 2))
    if (daysBetweenISO(from, to) + 1 > MAX_DAYS) to = addDaysISO(from, MAX_DAYS - 1)
  }
  return { from, to }
}

/** A window as the requests that fetch it, each at most MAX_DAYS wide. */
export function requestRanges(win: DayRange): DayRange[] {
  const out: DayRange[] = []
  for (let from = win.from; from <= win.to; from = addDaysISO(from, MAX_DAYS)) {
    const last = addDaysISO(from, MAX_DAYS - 1)
    out.push({ from, to: last < win.to ? last : win.to })
  }
  return out
}

/**
 * The events route answers 404 feeds_disabled while both calendar feeds and calendar sync are
 * off (the default), so it is not asked then; the switches are plain 200 reads. When they
 * cannot be read the state is unknown and the events request is made anyway.
 */
async function calendarsOn(): Promise<boolean> {
  try {
    return (await api.calendarFeedsSwitch.get()).enabled || (await api.calendarSyncSwitch.get()).enabled
  } catch {
    return true // unknown: ask the events route, which fails by itself when offline
  }
}

let inflight: Promise<boolean> | null = null

/**
 * Fetch events for a range (padded) and replace the cache. Without a range, the
 * cached window is refreshed. Failures (offline, an older server) keep the cache and
 * resolve false. While calendars are off nothing is asked, the cache is emptied (no window
 * is kept, so turning them on is picked up by the next call) and it resolves false.
 */
export async function refreshEvents(range?: DayRange): Promise<boolean> {
  if (inflight) return inflight
  inflight = (async () => {
    try {
      const win = await cachedWindow()
      const target = range ? fetchRange(range) : win ? { from: win.from, to: win.to } : null
      if (!target) return false
      // Nothing is asked while calendars are off: the cache is emptied and no window kept, so
      // turning them on is picked up by the next call.
      if (!(await calendarsOn())) {
        await db.transaction('rw', [db.calendarEvents, db.calendarSubscriptions, db.meta], async () => {
          await db.calendarEvents.clear()
          await db.calendarSubscriptions.clear()
          await db.meta.delete(WINDOW_KEY)
        })
        return false
      }
      // One request per piece, in order; an event in two pieces is kept once (by id).
      const events = new Map<string, CalendarEvent>()
      const subs = new Map<string, CalendarSubscription>()
      for (const piece of requestRanges(target)) {
        const res = await api.calendarEvents(piece.from, piece.to)
        for (const ev of res.events) events.set(ev.id, ev)
        for (const sub of res.subscriptions) subs.set(sub.id, sub)
      }
      await db.transaction('rw', [db.calendarEvents, db.calendarSubscriptions, db.meta, db.eventOutbox], async () => {
        // A refresh in the middle of an edit keeps the edit (calendar-event-edits D6).
        overlayQueued(events, await db.eventOutbox.toArray())
        await db.calendarEvents.clear()
        await db.calendarSubscriptions.clear()
        await db.calendarEvents.bulkPut([...events.values()])
        await db.calendarSubscriptions.bulkPut([...subs.values()])
        await setMeta(WINDOW_KEY, { from: target.from, to: target.to, fetchedAt: Date.now() } satisfies EventsWindow)
      })
      for (const fn of [...afterRefresh]) fn()
      return true
    } catch {
      return false
    } finally {
      inflight = null
    }
  })()
  return inflight
}

/**
 * After the user changed their subscriptions (Settings): refresh the cached window, or
 * today when the device has none yet, so Today learns about a first calendar (today-agenda D3).
 */
export async function refreshAfterChange(): Promise<boolean> {
  const today = logicalToday()
  return refreshEvents((await cachedWindow()) ? undefined : { from: today, to: today })
}

/** Make sure the cache holds a fresh copy of the range (the calendar views call this on navigation). */
export async function ensureEventsFor(range: DayRange, now = Date.now()): Promise<void> {
  const win = await cachedWindow()
  if (covers(win, range) && now - win!.fetchedAt < STALE_MS) return
  await refreshEvents(range)
}

/** How the cached events relate to what is on screen, for the toolbar's status text. */
export function eventsStatus(win: EventsWindow | null, range: DayRange, now = Date.now()): 'fresh' | 'stale' | 'missing' {
  if (!covers(win, range)) return 'missing'
  return now - win!.fetchedAt < STALE_MS ? 'fresh' : 'stale'
}

/** The "Calendars" toggle (per device, on by default). */
export function showEvents(): Promise<boolean> {
  return getMeta<boolean>(SHOW_KEY, true)
}

export function setShowEvents(on: boolean): Promise<void> {
  return setMeta(SHOW_KEY, on)
}
