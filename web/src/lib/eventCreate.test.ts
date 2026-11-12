import 'fake-indexeddb/auto'
import { beforeEach, describe, expect, it } from 'vitest'
import { clearLocalData, db, useDatabase } from './db'
import { defaultEventCalendar, draftFromEvent, getEventCalendar, getNewKind, nextHalfHour, newKind, setEventCalendar, setNewKind, writableCalendars } from './eventCreate'
import type { CalendarEvent, CalendarSubscription } from './types'

const sub = (id: string, over: Partial<CalendarSubscription> = {}): CalendarSubscription => ({
  id, name: id, color: '', enabled: true, lastSuccessAt: 1, managed: true, connectionId: `c-${id}`, calendarUrl: `https://dav/${id}/`, editEvents: true, caps: { create: true, update: true, delete: true }, ...over,
})

let n = 0
beforeEach(() => useDatabase(`event-create-${Date.now()}-${n++}`))

describe('writable calendars (D1)', () => {
  it('lists only managed calendars with edits on and create allowed', () => {
    const list = writableCalendars([
      sub('b'), sub('a'), sub('off', { editEvents: false }), sub('ro', { caps: { create: false, update: true, delete: true } }),
      sub('ics', { connectionId: undefined, calendarUrl: undefined, managed: false }), sub('nocaps', { caps: undefined }), sub('disabled', { enabled: false }),
    ])
    expect(list.map((s) => s.id)).toEqual(['a', 'b'])
  })

  it('falls back to the first calendar when the remembered one stopped being writable', () => {
    const list = writableCalendars([sub('a'), sub('b')])
    expect(defaultEventCalendar(list, 'b')?.id).toBe('b')
    expect(defaultEventCalendar(list, 'gone')?.id).toBe('a')
    expect(defaultEventCalendar([], 'a')).toBeUndefined()
  })

  it('reads a remembered "event" as "task" while the list is empty', () => {
    expect(newKind([], 'event')).toBe('task')
    expect(newKind(writableCalendars([sub('a')]), 'event')).toBe('event')
    expect(newKind(writableCalendars([sub('a')]), null)).toBe('task')
  })

  it('keeps both choices per device and sign-out clears them', async () => {
    await setNewKind('event')
    await setEventCalendar('b')
    expect(await getNewKind()).toBe('event')
    expect(await getEventCalendar()).toBe('b')
    await clearLocalData()
    expect(await getNewKind()).toBeNull()
    expect(await getEventCalendar()).toBeNull()
    expect(await db.meta.count()).toBe(0)
  })
})

describe('nextHalfHour (D12)', () => {
  const at = (h: number, m: number) => new Date(2026, 10, 12, h, m)
  it.each([
    [10, 7, [], '2026-11-12', 10 * 60 + 30],
    [10, 30, [], '2026-11-12', 10 * 60 + 30],
    [10, 31, [], '2026-11-12', 11 * 60],
    [10, 7, [{ start: 10 * 60 + 30, end: 11 * 60 }], '2026-11-12', 11 * 60],
    [10, 7, [{ start: 10 * 60, end: 12 * 60 }], '2026-11-12', 12 * 60],
    [23, 40, [], '2026-11-13', 9 * 60],
    [23, 20, [], '2026-11-12', 23 * 60 + 30],
    [23, 20, [{ start: 23 * 60 + 30, end: 24 * 60 }], '2026-11-13', 9 * 60],
  ])('at %i:%i with %j gives %s %i', (h, m, busy, date, start) => {
    expect(nextHalfHour(at(h, m), busy)).toEqual({ date, start })
  })
})

describe('draftFromEvent (Duplicate)', () => {
  it('copies the fields into the chosen calendar with a new client id', () => {
    const ev: CalendarEvent = {
      id: 'e1', subscriptionId: 's1', uid: 'u', summary: 'Review', location: 'Room 4', allDay: false, free: true,
      start: { date: '2026-11-12', time: '10:00', tz: 'Europe/Berlin' }, end: { date: '2026-11-12', time: '11:00', tz: 'Europe/Berlin' },
    }
    const a = draftFromEvent(ev, 'Notes', sub('b'), 'Europe/Lisbon')!
    const b = draftFromEvent(ev, 'Notes', sub('b'), 'Europe/Lisbon')!
    expect(a).toMatchObject({ connectionId: 'c-b', calendarUrl: 'https://dav/b/', subscriptionId: 'b', summary: 'Review', location: 'Room 4', description: 'Notes', free: true, allDay: false, start: ev.start, end: ev.end })
    expect(a.clientId).not.toBe(b.clientId)
    expect(a.clientId).toMatch(/^[A-Za-z0-9_-]{8,64}$/)
  })
})
