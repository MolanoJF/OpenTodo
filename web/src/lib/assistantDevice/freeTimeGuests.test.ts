// Guests and join links never reach the assistant (OpenSpec change calendar-event-guests, task 4.5, design D12):
// the device twin of `find_free_time` reads start, end, all-day and free of a cached event and nothing else, proven
// by serialising what it passes to the free time code and what it answers, and failing on a guest or a link.
import 'fake-indexeddb/auto'
import { readFileSync } from 'node:fs'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { db, setMeta, useDatabase } from '../db'
import { setCurrentUserId } from '../sharing'
import type { CalendarEvent } from '../types'
import { loadSnapshot } from './data'

const seen: unknown[] = []
vi.mock('../freeTime', async (importOriginal) => {
  const real = await importOriginal<typeof import('../freeTime')>()
  return { ...real, freeTime: (input: Parameters<typeof real.freeTime>[0]) => { seen.push(input.events); return real.freeTime(input) } }
})
import { freeEventsOf } from '../freeTime'
import { findFreeTime } from './readFreeTime'

const fetchMock = vi.fn(async () => new Response('{}', { status: 404 }))
const meeting: CalendarEvent = {
  id: 'g1', subscriptionId: 's1', uid: 'u', summary: 'Board meeting', location: 'Room 4', allDay: false, free: false, rev: 'r1',
  start: { date: '2026-11-16', time: '10:00', tz: 'UTC' }, end: { date: '2026-11-16', time: '11:00', tz: 'UTC' },
  guests: { count: 3, you: 'accepted', organizer: false, canRespond: true },
  conference: { url: 'https://meet.google.com/abc-defg-hij?pwd=secret-passcode', provider: 'meet' },
  access: { update: false, delete: false, reason: 'not_organizer', notifies: false },
}

let n = 0
beforeEach(async () => {
  seen.length = 0
  vi.stubGlobal('fetch', fetchMock)
  useDatabase(`free-time-guests-${Date.now()}-${n++}`)
  await setCurrentUserId('u-1')
  await db.calendarEvents.put(meeting)
  await setMeta('calendar.grid', { startHour: 9, endHour: 17, dayLength: 480 })
})
afterEach(() => { vi.unstubAllGlobals() })

describe('the free time twin and calendar guests', () => {
  it('passes the free time code only the times of an event and answers without a guest or a link', async () => {
    const out = await findFreeTime(await loadSnapshot(), { from: '2026-11-16', to: '2026-11-16', timezone: 'UTC' }, Date.parse('2026-11-14T08:00:00Z'))
    const events = (seen[0] ?? []) as Record<string, unknown>[]
    expect(events).toHaveLength(1) // the meeting was read...
    expect(Object.keys(events[0]).sort()).toEqual(['allDay', 'end', 'free', 'start'])
    for (const text of [JSON.stringify(seen), JSON.stringify(out)]) {
      expect(text).not.toMatch(/guests|conference|meet\.google|secret-passcode|notifies|Board meeting/)
    }
    // ...and it blocked the hour it covers.
    expect(out.days[0].free.map((x) => [x.start, x.end])).toEqual([['09:00', '10:00'], ['11:00', '17:00']])
  })

  it('freeEventsOf drops every other member of a row', () => {
    expect(freeEventsOf([meeting])).toEqual([{ start: meeting.start, end: meeting.end, allDay: false, free: false }])
  })

  it('the block reader of the in-app assistant strips events the same way', () => {
    const src = readFileSync(new URL('../assistantBlockData.ts', import.meta.url), 'utf8')
    expect(src).toMatch(/events: freeEventsOf\(events\)/)
  })
})
