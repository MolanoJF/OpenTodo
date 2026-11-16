// What a drop, a key press or the popover's fields mean for a connected calendar's event
// (OpenSpec change calendar-event-edits D6, D7). Pure: the grid gives wall times in the
// viewer's zone, the event keeps its own zone (`eventPointIn`), floating points stay floating.
import { DAY_MINUTES, formatTime, parseTime, SLOT_MINUTES } from './calendar'
import { addDaysISO, daysBetweenISO, eventPointIn, localDueOf } from './dates'
import type { CalendarEvent, CalendarEventChange, CalendarWhen } from './types'

/** The targets of an event drag; `into-slot` is not one (an event never joins a slot). */
export type EventTarget =
  | { type: 'slot'; date: string; start: number }
  | { type: 'lane'; date: string }
  | { type: 'resize'; estimate: number }
  | { type: 'resize-start'; date: string; start: number; estimate: number }

/** A change of the event's end or start by keys: both by `minutes`, both by `days`, or one edge by `end` / `start` minutes. */
export type EventNudge = { minutes: number } | { days: number } | { end: number } | { start: number }

interface Wall { date: string; minutes: number }

/** A timed point as the viewer sees it. */
function wallOf(p: CalendarWhen, zone?: string): Wall | null {
  if (!p.time) return null
  const local = p.tz ? localDueOf({ date: p.date, time: p.time, tz: p.tz }, zone) : { date: p.date, time: p.time }
  const minutes = parseTime(local.time)
  return minutes === null ? null : { date: local.date, minutes }
}

function plus(w: Wall, delta: number): Wall {
  const total = w.minutes + delta
  const days = Math.floor(total / DAY_MINUTES)
  return { date: addDaysISO(w.date, days), minutes: total - days * DAY_MINUTES }
}

const between = (a: Wall, b: Wall) => daysBetweenISO(a.date, b.date) * DAY_MINUTES + b.minutes - a.minutes

/** A wall time back to the point the event keeps, in the zone of `like`. */
const pointOf = (like: CalendarWhen, w: Wall, zone?: string): CalendarWhen => eventPointIn(like.tz, { date: w.date, time: formatTime(w.minutes) }, zone)

/** The viewer-zone start and length of a timed event, or null for an all-day one. */
function spanOf(ev: CalendarEvent, zone?: string): { start: Wall; length: number } | null {
  if (ev.allDay) return null
  const start = wallOf(ev.start, zone)
  if (!start) return null
  const end = wallOf(ev.end, zone)
  return { start, length: Math.max(SLOT_MINUTES, end ? between(start, end) : SLOT_MINUTES) }
}

function timedChange(ev: CalendarEvent, start: Wall, end: Wall, zone?: string): CalendarEventChange {
  return { start: pointOf(ev.start, start, zone), end: pointOf(ev.end.time ? ev.end : ev.start, end, zone), allDay: false }
}

/** The change a drop makes, or null when the event does not move. Dropped on the lane it becomes all-day. */
export function eventDropChange(ev: CalendarEvent, target: EventTarget, zone?: string): CalendarEventChange | null {
  const span = spanOf(ev, zone)
  if (!span) return null
  const { start, length } = span
  if (target.type === 'lane') return { start: { date: target.date }, end: { date: addDaysISO(target.date, 1) }, allDay: true }
  const next = target.type === 'slot' ? { date: target.date, minutes: target.start }
    : target.type === 'resize-start' ? { date: target.date, minutes: target.start } : start
  const len = target.type === 'slot' ? length : Math.max(SLOT_MINUTES, target.estimate)
  const change = timedChange(ev, next, plus(next, len), zone)
  return same(ev, change) ? null : change
}

/** The change of an arrow key on a timed event, or null when it cannot (an edge would cross the other). */
export function eventNudgeChange(ev: CalendarEvent, nudge: EventNudge, zone?: string): CalendarEventChange | null {
  const span = spanOf(ev, zone)
  if (!span) return null
  const { start, length } = span
  const end = plus(start, length)
  if ('minutes' in nudge) return timedChange(ev, plus(start, nudge.minutes), plus(end, nudge.minutes), zone)
  if ('days' in nudge) return timedChange(ev, plus(start, nudge.days * DAY_MINUTES), plus(end, nudge.days * DAY_MINUTES), zone)
  if ('end' in nudge) return length + nudge.end < SLOT_MINUTES ? null : timedChange(ev, start, plus(end, nudge.end), zone)
  return length - nudge.start < SLOT_MINUTES ? null : timedChange(ev, plus(start, nudge.start), end, zone)
}

/** Whether a change leaves the event where it is. */
function same(ev: CalendarEvent, c: CalendarEventChange): boolean {
  const k = (p: CalendarWhen | undefined) => `${p?.date}|${p?.time ?? ''}|${p?.tz ?? ''}`
  return k(c.start) === k(ev.start) && k(c.end) === k(ev.end) && (c.allDay ?? ev.allDay) === ev.allDay
}

/** What the popover shows of an event, in the viewer's zone: `start` and `end` are "HH:MM", empty for an all-day event. */
export interface EventForm {
  title: string
  date: string
  start: string
  end: string
}

export function eventFormOf(ev: CalendarEvent, zone?: string): EventForm {
  const start = wallOf(ev.start, zone)
  const end = wallOf(ev.end, zone)
  if (ev.allDay || !start) return { title: ev.summary, date: ev.start.date, start: '', end: '' }
  return { title: ev.summary, date: start.date, start: formatTime(start.minutes), end: formatTime((end ?? start).minutes) }
}

/**
 * The change the popover's Save makes, `null` when nothing changed, `'invalid'` when the
 * times do not make an event (an end that is not after the start, an unreadable time).
 * A timed event keeps the number of days it spans; an all-day one stays all-day unless it is given times.
 */
export function eventFormChange(ev: CalendarEvent, form: EventForm, zone?: string): CalendarEventChange | null | 'invalid' {
  const before = eventFormOf(ev, zone)
  const change: CalendarEventChange = {}
  const title = form.title.trim()
  if (title !== ev.summary) change.summary = title
  if (form.date !== before.date || form.start !== before.start || form.end !== before.end) {
    const start = form.start ? parseTime(form.start) : null
    const end = form.end ? parseTime(form.end) : null
    if (!/^\d{4}-\d{2}-\d{2}$/.test(form.date)) return 'invalid'
    if (!form.start && !form.end) {
      if (!ev.allDay) return 'invalid' // a timed event keeps its times
      const days = Math.max(1, daysBetweenISO(ev.start.date, ev.end.date))
      change.start = { date: form.date }
      change.end = { date: addDaysISO(form.date, days) }
      change.allDay = true
    } else {
      if (start === null || end === null) return 'invalid'
      const span = ev.allDay ? 0 : daysBetweenISO(wallOf(ev.start, zone)?.date ?? ev.start.date, wallOf(ev.end, zone)?.date ?? ev.end.date)
      const from: Wall = { date: form.date, minutes: start }
      const to: Wall = { date: addDaysISO(form.date, span), minutes: end }
      if (between(from, to) <= 0) return 'invalid'
      Object.assign(change, timedChange(ev.allDay ? { ...ev, start: { date: form.date, time: form.start }, end: { date: form.date, time: form.end } } : ev, from, to, zone))
    }
  }
  return Object.keys(change).length ? change : null
}


/**
 * Whether the grid lets an event be dragged, selected with the keys and edited in place: the calendar
 * allows an update and it is a timed event on one day (a block clipped at midnight has no edge to drag).
 */
export function eventEditable(ev: CalendarEvent, zone?: string): boolean {
  if (!ev.access?.update || ev.allDay) return false
  const start = wallOf(ev.start, zone)
  const end = wallOf(ev.end, zone)
  return !!start && (!end || end.date === start.date || (end.minutes === 0 && end.date === addDaysISO(start.date, 1)))
}
