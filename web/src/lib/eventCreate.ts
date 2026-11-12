// Creating events in a connected calendar (OpenSpec change calendar-event-editor D1, D3, D9).
// A new event is drawn at once as a pending row and queued as one create command in the device's
// outbox (calendarEventEdits sends it); the calendar is the source of truth, nothing here is an op.
import { armTimer, EDIT_SETTLE_MS } from './calendarEventEdits'
import { pendingRowOf } from './calendarFeeds'
import { DAY_MINUTES } from './calendar'
import { addDaysISO, localDueOf, logicalToday, todayISO, viewerZone } from './dates'
import { db } from './db'
import { eventFullOf, pointsOf, type EventFull } from './eventFull'
import { restRepeat } from './eventRepeat'
import { uuid } from './ids'
import { setEventCalendar } from './writableCalendars'
import type { CalendarEvent, CalendarEventDraft, CalendarSubscription } from './types'

export { isPendingId, pendingRowOf } from './calendarFeeds'
export * from './writableCalendars'

/** A fresh client id: one per new event, sent so a retried create makes one event. */
export const newClientId = (): string => uuid()

/** A draft from the editor's fields; null when the times do not make an event. */
export function draftFromFull(form: EventFull, cal: Pick<CalendarSubscription, 'id' | 'connectionId' | 'calendarUrl'>, clientId = newClientId()): CalendarEventDraft | null {
  const when = pointsOf(form)
  if (when === 'invalid') return null
  return {
    clientId, connectionId: cal.connectionId!, calendarUrl: cal.calendarUrl!, subscriptionId: cal.id,
    summary: form.title.trim(), start: when.start, end: when.end, allDay: when.allDay,
    location: form.location, description: form.description, free: form.free,
    ...(form.repeat ? { repeat: restRepeat(form.repeat) } : {}), ...(form.reminders.length ? { reminders: form.reminders } : {}),
  }
}

/** Duplicate (D11): the event's own fields as a draft for `cal`, with a new client id. */
export function draftFromEvent(ev: CalendarEvent, description: string, cal: Pick<CalendarSubscription, 'id' | 'connectionId' | 'calendarUrl'>, viewerZone: string, clientId = newClientId()): CalendarEventDraft | null {
  return draftFromFull({ ...eventFullOf(ev, viewerZone, description), repeat: null }, cal, clientId) // one occurrence copies as a single event
}

/**
 * Draws a new event and queues its create (one command per event). The calendar the person
 * chose becomes the default for next time. Returns the pending row.
 */
export async function createEvent(draft: CalendarEventDraft, now = Date.now()): Promise<CalendarEvent> {
  const row = pendingRowOf(draft)
  await db.transaction('rw', [db.calendarEvents, db.eventOutbox, db.meta], async () => {
    // Creates are sent in the order they were made: each waits at least one tick after the last.
    const last = (await db.eventOutbox.toArray()).reduce((m, c) => ('create' in c.patch ? Math.max(m, c.notBefore) : m), 0)
    const notBefore = Math.max(now + EDIT_SETTLE_MS, last + 1)
    await db.calendarEvents.put(row)
    await db.eventOutbox.put({ key: `${draft.connectionId}:${row.id}`, connectionId: draft.connectionId, eventId: row.id, rev: '', base: row, patch: { create: draft }, notBefore, tries: 0 })
    await setEventCalendar(draft.subscriptionId)
  })
  void armTimer().catch(() => undefined)
  return row
}

/** A busy stretch of a day, in minutes from midnight. */
export interface BusyMinutes { start: number; end: number }

/**
 * The next `:00` or `:30` at or after `now` whose half hour holds nothing (design D12): today,
 * or 09:00 tomorrow when none is left before midnight. `busy` is today's task blocks and events.
 */
export function nextHalfHour(now: Date, busy: readonly BusyMinutes[]): { date: string; start: number } {
  const today = logicalToday(undefined, now.getTime())
  const from = todayISO(now) === today ? now.getHours() * 60 + now.getMinutes() : 0
  for (let s = Math.ceil(from / 30) * 30; s + 30 <= DAY_MINUTES; s += 30) {
    if (!busy.some((b) => b.start < s + 30 && b.end > s)) return { date: today, start: s }
  }
  return { date: addDaysISO(today, 1), start: 9 * 60 }
}

/** Minutes from midnight of a timed point on `date`, in the viewer's zone, or null when it is on another day. */
export function minutesOnDay(p: CalendarEvent['start'], date: string, zone?: string): number | null {
  if (!p.time) return null
  const local = p.tz ? localDueOf({ date: p.date, time: p.time, tz: p.tz }, zone) : { date: p.date, time: p.time }
  if (local.date !== date || !local.time) return null
  const [h, m] = local.time.split(':').map(Number)
  return h * 60 + m
}

/** The part of a timed event that falls on `date`, in the viewer's zone. */
function busyOfEvent(ev: CalendarEvent, date: string, zone: string): BusyMinutes | null {
  if (ev.allDay || !ev.start.time) return null
  const start = minutesOnDay(ev.start, date, zone)
  const end = ev.end.time ? minutesOnDay(ev.end, date, zone) : null
  if (start !== null) return { start, end: end ?? DAY_MINUTES }
  // Started on an earlier day and still going on this one.
  const from = ev.start.tz ? localDueOf({ date: ev.start.date, time: ev.start.time, tz: ev.start.tz }, zone).date : ev.start.date
  return from < date && end !== null && end > 0 ? { start: 0, end } : null
}

/** The stretches of `date` the cached events cover (timed ones; an all-day event is not busy time). */
export async function eventBusyOn(date: string, zone?: string): Promise<BusyMinutes[]> {
  return (await db.calendarEvents.toArray()).flatMap((ev) => busyOfEvent(ev, date, zone ?? viewerZone()) ?? [])
}
