// The full event editor's fields (OpenSpec change calendar-event-editor D11): pure conversions between an event
// and what the editor shows. Apart from eventDrop (the grid's drops and keys, read on first load) so the editor's
// code loads with the editor.
import { parseTime } from './calendar'
import { addDaysISO, localDueOf } from './dates'
import type { CalendarEvent, CalendarEventChange, CalendarWhen } from './types'

/** What the full editor shows of an event. Times are wall times in `zone` ('' = floating); an all-day event has dates only, its end date the last day it covers. */
export interface EventFull {
  title: string
  allDay: boolean
  startDate: string
  startTime: string
  endDate: string
  endTime: string
  zone: string
  location: string
  description: string
  free: boolean
}

const DATE = /^\d{4}-\d{2}-\d{2}$/

/** A point as wall time in `zone`: the viewer's zone for UTC (a Graph event, a `Z` time), the point's own zone otherwise, floating as it is. */
function inZone(p: CalendarWhen, zone: string): { date: string; time: string } {
  if (!p.time) return { date: p.date, time: '' }
  if (!p.tz || !zone) return { date: p.date, time: p.time }
  if (p.tz === zone) return { date: p.date, time: p.time }
  const l = localDueOf({ date: p.date, time: p.time, tz: p.tz }, zone)
  return { date: l.date, time: l.time ?? p.time }
}

/** The zone an event is edited in: its own, the viewer's for a UTC event or an all-day one, '' for a floating one. */
export function eventZoneOf(ev: CalendarEvent, viewerZone: string): string {
  if (ev.allDay) return viewerZone
  const tz = ev.start.tz
  return !tz ? '' : tz === 'UTC' ? viewerZone : tz
}

/** The editor's fields for an event; `description` is what the calendar gave when the editor opened. */
export function eventFullOf(ev: CalendarEvent, viewerZone: string, description = ''): EventFull {
  const zone = eventZoneOf(ev, viewerZone)
  const base = { title: ev.summary, zone, location: ev.location, description, free: ev.free }
  if (ev.allDay) {
    const last = addDaysISO(ev.end.date, -1)
    return { ...base, allDay: true, startDate: ev.start.date, startTime: '', endDate: last < ev.start.date ? ev.start.date : last, endTime: '' }
  }
  const start = inZone(ev.start, zone)
  const end = inZone(ev.end.time ? ev.end : ev.start, zone)
  return { ...base, allDay: false, startDate: start.date, startTime: start.time, endDate: end.date, endTime: end.time }
}

/** The all-day switch: on keeps the dates, off gives 09:00 to 10:00 on the start date (D11). */
export function switchAllDay(form: EventFull, on: boolean): EventFull {
  if (on) return { ...form, allDay: true, startTime: '', endTime: '', endDate: form.endDate < form.startDate ? form.startDate : form.endDate }
  return { ...form, allDay: false, startTime: '09:00', endTime: '10:00', endDate: form.startDate }
}

/** The points the fields mean, or 'invalid' (an unreadable date or time, an end that is not after the start). */
export function pointsOf(form: EventFull): { start: CalendarWhen; end: CalendarWhen; allDay: boolean } | 'invalid' {
  if (!DATE.test(form.startDate) || !DATE.test(form.endDate)) return 'invalid'
  if (form.allDay) {
    if (form.endDate < form.startDate) return 'invalid'
    return { start: { date: form.startDate }, end: { date: addDaysISO(form.endDate, 1) }, allDay: true }
  }
  if (parseTime(form.startTime) === null || parseTime(form.endTime) === null) return 'invalid'
  if (`${form.endDate} ${form.endTime}` <= `${form.startDate} ${form.startTime}`) return 'invalid'
  const tz = form.zone ? { tz: form.zone } : {}
  return { start: { date: form.startDate, time: form.startTime, ...tz }, end: { date: form.endDate, time: form.endTime, ...tz }, allDay: false }
}

/** How the start reads in the viewer's zone when the event is kept in another one, else null ("10:00 in your time"). */
export function startInViewerZone(form: EventFull, viewerZone: string): { date: string; time: string } | null {
  if (form.allDay || !form.zone || form.zone === viewerZone || parseTime(form.startTime) === null || !DATE.test(form.startDate)) return null
  const l = localDueOf({ date: form.startDate, time: form.startTime, tz: form.zone }, viewerZone)
  return l.time ? { date: l.date, time: l.time } : null
}

/**
 * The change Save sends for the fields of an open event: only what differs from `ev` as the editor
 * showed it. A description is sent only when `loaded` (the text the calendar gave) is a string, i.e.
 * it could be loaded and edited. Null when nothing changed, 'invalid' when the times make no event.
 */
export function eventFullChange(ev: CalendarEvent, form: EventFull, viewerZone: string, loaded: string | null): CalendarEventChange | null | 'invalid' {
  const before = eventFullOf(ev, viewerZone, loaded ?? '')
  const out: CalendarEventChange = {}
  if (form.title.trim() !== ev.summary) out.summary = form.title.trim()
  if (form.location !== before.location) out.location = form.location
  if (form.free !== before.free) out.free = form.free
  if (loaded !== null && form.description !== loaded) out.description = form.description
  const times: (keyof EventFull)[] = ['allDay', 'startDate', 'startTime', 'endDate', 'endTime']
  if (times.some((k) => form[k] !== before[k]) || (!form.allDay && form.zone !== before.zone)) {
    const when = pointsOf(form)
    if (when === 'invalid') return 'invalid'
    Object.assign(out, when)
  }
  return Object.keys(out).length ? out : null
}
