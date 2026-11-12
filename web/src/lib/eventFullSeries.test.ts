// The editor's repeat and reminders as form fields (OpenSpec change calendar-event-series, task 4.4, design D11):
// they are read from the event and its detail, sent only when changed, and never for a custom rule.
import { describe, expect, it } from 'vitest'
import { draftFromFull } from './eventCreate'
import { eventFullChange, eventFullOf, type EventExtras } from './eventFull'
import type { CalendarEvent, CalendarRepeat } from './types'

const BERLIN = 'Europe/Berlin'
const never = { type: 'never' } as const
const daily: CalendarRepeat = { freq: 'daily', interval: 1, days: [], ends: never }
const weekdays: CalendarRepeat = { freq: 'weekly', interval: 1, days: ['mo', 'tu', 'we', 'th', 'fr'], ends: never }
const ev = (over: Partial<CalendarEvent> = {}): CalendarEvent => ({
  id: 'e1', subscriptionId: 's1', uid: 'u1', summary: 'Routine', location: '', allDay: false, free: false, rev: 'r1',
  start: { date: '2026-11-12', time: '07:00', tz: BERLIN }, end: { date: '2026-11-12', time: '08:30', tz: BERLIN }, ...over,
})
const series = (repeat: CalendarRepeat | null | undefined, extra: Partial<NonNullable<CalendarEvent['series']>> = {}): CalendarEvent => ev({ series: { id: 's', ...(repeat !== undefined ? { repeat } : {}), ...extra } })
const full = (e: CalendarEvent, extras?: EventExtras) => eventFullOf(e, BERLIN, '', extras)
const change = (e: CalendarEvent, form: ReturnType<typeof full>, extras?: EventExtras) => eventFullChange(e, form, BERLIN, '', extras)

describe('the repeat field', () => {
  it('reads a single event as not repeating and an occurrence from its series', () => {
    expect(full(ev()).repeat).toBeNull()
    expect(full(series(daily)).repeat).toEqual(daily)
  })

  it('takes the detail over the events route (Microsoft loads the rule on demand)', () => {
    expect(full(series(undefined), { repeat: weekdays }).repeat).toEqual(weekdays)
  })

  it('sends nothing when the repeat is untouched', () => {
    expect(change(series(daily), full(series(daily)))).toBeNull()
    expect(change(ev(), full(ev()))).toBeNull()
  })

  it('makes a single event repeat from its own start, in the REST form', () => {
    const e = ev()
    expect(change(e, { ...full(e), repeat: { freq: 'weekly', interval: 1, days: ['we', 'mo'], ends: { type: 'after', count: 10 } } })).toEqual({
      repeat: { freq: 'weekly', interval: 1, days: ['mo', 'we'], ends: { type: 'after', count: 10 } },
    })
  })

  it('stopping the repeat is repeat: null', () => {
    const e = series(daily)
    expect(change(e, { ...full(e), repeat: null })).toEqual({ repeat: null })
  })

  it('changing the repeat of a series sends the new rule next to a time change', () => {
    const e = series(daily)
    expect(change(e, { ...full(e), repeat: weekdays, startTime: '08:00', endTime: '09:30' })).toEqual({
      repeat: weekdays,
      start: { date: '2026-11-12', time: '08:00', tz: BERLIN }, end: { date: '2026-11-12', time: '09:30', tz: BERLIN }, allDay: false,
    })
  })

  it('a custom rule is never sent: the form shows it read-only and the other fields still save', () => {
    const e = series(null, { custom: true })
    const f = full(e)
    expect(f.repeat).toBeNull()
    expect(change(e, { ...f, title: 'Renamed' })).toEqual({ summary: 'Renamed' })
    expect(change(e, { ...f, repeat: daily, title: 'Renamed' })).toEqual({ summary: 'Renamed' })
  })

  it('leaves a rule it has not read alone (a Microsoft series before its detail arrives)', () => {
    const e = series(undefined)
    expect(change(e, { ...full(e), title: 'Renamed' })).toEqual({ summary: 'Renamed' })
    expect(change(e, { ...full(e), repeat: daily })).toBeNull()
  })
})

describe('the reminders field', () => {
  it('reads the reminders of the detail', () => {
    expect(full(ev(), { reminders: [10, 1440] }).reminders).toEqual([10, 1440])
    expect(full(ev()).reminders).toEqual([])
  })

  it('sends them only when changed and only when they were loaded', () => {
    const loaded: EventExtras = { reminders: [10] }
    expect(change(ev(), { ...full(ev(), loaded), title: 'x' }, loaded)).toEqual({ summary: 'x' })
    expect(change(ev(), { ...full(ev(), loaded), reminders: [10, 1440] }, loaded)).toEqual({ reminders: [10, 1440] })
    expect(change(ev(), { ...full(ev(), loaded), reminders: [] }, loaded)).toEqual({ reminders: [] })
    // Offline (the detail did not load): left out, so a queued edit never overwrites reminders the person did not see.
    expect(change(ev(), { ...full(ev()), reminders: [5] })).toBeNull()
  })

  it('compares by value and by order', () => {
    const loaded: EventExtras = { reminders: [10, 1440] }
    expect(change(ev(), full(ev(), loaded), loaded)).toBeNull()
    expect(change(ev(), { ...full(ev(), loaded), reminders: [1440, 10] }, loaded)).toEqual({ reminders: [1440, 10] })
  })
})

describe('a draft from the fields', () => {
  const cal = { id: 's1', connectionId: 'c1', calendarUrl: 'https://dav.example.com/work/' }
  it('carries the repeat and reminders of a new event, and no keys for none', () => {
    const e = ev()
    expect(draftFromFull({ ...full(e), repeat: daily, reminders: [10] }, cal, 'k1')).toMatchObject({ repeat: daily, reminders: [10] })
    const plain = draftFromFull(full(e), cal, 'k1')!
    expect('repeat' in plain).toBe(false)
    expect('reminders' in plain).toBe(false)
  })
})
