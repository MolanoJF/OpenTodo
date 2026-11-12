import 'fake-indexeddb/auto'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const calendarEvents = vi.fn(async (_from: string, _to: string) => ({
  events: [{ id: 'e1', subscriptionId: 's1', uid: 'u', summary: 'Team sync', location: '', start: { date: '2026-10-19', time: '10:00', tz: 'Europe/Berlin' }, end: { date: '2026-10-19', time: '11:00', tz: 'Europe/Berlin' }, allDay: false, free: false }],
  subscriptions: [{ id: 's1', name: 'Work', color: '#3b82f6', enabled: true, lastSuccessAt: 1 }],
}))
const feedsSwitch = vi.fn(async () => ({ enabled: true }))
const syncSwitch = vi.fn(async () => ({ enabled: false }))
vi.mock('./api', () => ({
  api: {
    calendarEvents: (from: string, to: string) => calendarEvents(from, to),
    calendarFeedsSwitch: { get: () => feedsSwitch() },
    calendarSyncSwitch: { get: () => syncSwitch() },
  },
}))

import { applyEventPatch, cachedWindow, covers, ensureEventsFor, eventPatchBetween, eventsStatus, fetchRange, isNoChange, overlayQueued, pendingRowOf, refreshEvents, requestRanges, setShowEvents, showEvents } from './calendarFeeds'
import type { CalendarEvent, EventCommand } from './types'
import { clearLocalData, db, useDatabase } from './db'

let n = 0
beforeEach(() => {
  useDatabase(`calfeeds-${Date.now()}-${n++}`)
  calendarEvents.mockClear()
  feedsSwitch.mockResolvedValue({ enabled: true })
  syncSwitch.mockResolvedValue({ enabled: false })
})

describe('calendar events cache (5.1, 5.2)', () => {
  const week = { from: '2026-10-19', to: '2026-10-25' }

  it('fetches the range ± 28 days, stores rows and the window, and sign-out empties them', async () => {
    await ensureEventsFor(week)
    expect(calendarEvents).toHaveBeenCalledWith('2026-09-21', '2026-11-22')
    expect(await db.calendarEvents.count()).toBe(1)
    expect(await db.calendarSubscriptions.count()).toBe(1)
    expect(covers(await cachedWindow(), week)).toBe(true)
    await clearLocalData()
    expect(await db.calendarEvents.count()).toBe(0)
    expect(await db.calendarSubscriptions.count()).toBe(0)
  })

  it('makes no request inside a fresh window, refreshes a 10-minute-old one, fetches outside it', async () => {
    await ensureEventsFor(week)
    calendarEvents.mockClear()
    const win = (await cachedWindow())!
    await ensureEventsFor(week, win.fetchedAt + 60_000)
    expect(calendarEvents).not.toHaveBeenCalled()
    await ensureEventsFor(week, win.fetchedAt + 11 * 60_000)
    expect(calendarEvents).toHaveBeenCalledTimes(1)
    await ensureEventsFor({ from: '2027-03-01', to: '2027-03-07' })
    expect(calendarEvents).toHaveBeenLastCalledWith('2027-02-01', '2027-04-04')
  })

  it('keeps the cache when the API fails', async () => {
    await refreshEvents(week)
    calendarEvents.mockRejectedValueOnce(new TypeError('offline'))
    expect(await refreshEvents(week)).toBe(false)
    expect(await db.calendarEvents.count()).toBe(1)
  })

  it('asks nothing while feeds and sync are both off, empties the cache, and asks again once one is on', async () => {
    await refreshEvents(week)
    expect(await db.calendarEvents.count()).toBe(1)
    calendarEvents.mockClear()
    feedsSwitch.mockResolvedValue({ enabled: false })
    expect(await refreshEvents(week)).toBe(false)
    await ensureEventsFor(week)
    expect(calendarEvents).not.toHaveBeenCalled()
    expect(await db.calendarEvents.count()).toBe(0)
    expect(await db.calendarSubscriptions.count()).toBe(0)
    expect(await cachedWindow()).toBeNull()
    syncSwitch.mockResolvedValue({ enabled: true })
    await ensureEventsFor(week)
    expect(calendarEvents).toHaveBeenCalledTimes(1)
    expect(await db.calendarEvents.count()).toBe(1)
  })

  it('still asks for events when the switches cannot be read, and keeps the cache if that fails too', async () => {
    await refreshEvents(week)
    calendarEvents.mockClear()
    feedsSwitch.mockRejectedValueOnce(new TypeError('offline'))
    expect(await refreshEvents(week)).toBe(true)
    expect(calendarEvents).toHaveBeenCalledTimes(1)
    feedsSwitch.mockRejectedValueOnce(new TypeError('offline'))
    calendarEvents.mockRejectedValueOnce(new TypeError('offline'))
    expect(await refreshEvents(week)).toBe(false)
    expect(await db.calendarEvents.count()).toBe(1)
  })

  it('reports the status and keeps the toggle per device', async () => {
    expect(eventsStatus(null, week)).toBe('missing')
    expect(eventsStatus({ from: '2026-10-01', to: '2026-10-31', fetchedAt: 0 }, week, 60_000)).toBe('fresh')
    expect(eventsStatus({ from: '2026-10-01', to: '2026-10-31', fetchedAt: 0 }, week, 11 * 60_000)).toBe('stale')
    expect(fetchRange({ from: '2026-10-01', to: '2026-11-30' })).toEqual({ from: '2026-09-16', to: '2026-12-16' })
    expect(await showEvents()).toBe(true)
    await setShowEvents(false)
    expect(await showEvents()).toBe(false)
  })

  it('fetches a year (the Year view) in pieces the server accepts, keeping an event in two pieces once', async () => {
    const year = { from: '2026-01-01', to: '2026-12-31' }
    expect(fetchRange(year)).toEqual(year)
    expect(fetchRange({ from: '2026-01-01', to: '2027-06-30' })).toEqual({ from: '2026-01-01', to: '2027-01-08' })
    expect(requestRanges(year)).toEqual([
      { from: '2026-01-01', to: '2026-04-02' }, { from: '2026-04-03', to: '2026-07-03' },
      { from: '2026-07-04', to: '2026-10-03' }, { from: '2026-10-04', to: '2026-12-31' },
    ])
    await ensureEventsFor(year)
    expect(calendarEvents.mock.calls.map(([from, to]) => [from, to])).toEqual(requestRanges(year).map((r) => [r.from, r.to]))
    // The mock answers every piece with the same event: it is stored once.
    expect(await db.calendarEvents.count()).toBe(1)
    expect(covers(await cachedWindow(), year)).toBe(true)
  })
})

describe('queued creates and the new fields (calendar-event-editor D9)', () => {
  const draft = { clientId: 'k1', connectionId: 'c1', calendarUrl: 'u', subscriptionId: 's1', summary: 'Dentist', start: { date: '2026-10-19', time: '09:00' }, end: { date: '2026-10-19', time: '09:30' }, allDay: false, location: '', description: 'x', free: false }
  const cmd = (patch: EventCommand['patch'], next?: EventCommand['next']): EventCommand => ({ key: 'c1:pending:k1', connectionId: 'c1', eventId: 'pending:k1', rev: '', base: pendingRowOf(draft), patch, next, notBefore: 0, tries: 0 })

  it('draws a pending row for a queued create, with what waits in next, and none for a discarded one', () => {
    const events = new Map<string, CalendarEvent>()
    overlayQueued(events, [cmd({ create: draft })])
    expect(events.get('pending:k1')).toMatchObject({ summary: 'Dentist', uid: 'k1' })
    overlayQueued(events, [cmd({ create: draft }, { summary: 'Renamed', location: 'Room 4' })])
    expect(events.get('pending:k1')).toMatchObject({ summary: 'Renamed', location: 'Room 4' })
    const gone = new Map<string, CalendarEvent>()
    overlayQueued(gone, [cmd({ create: draft }, { delete: true })])
    expect(gone.size).toBe(0)
  })

  it('applies location and free to a row and finds them in a difference, but never a description', () => {
    const row = pendingRowOf(draft)
    const changed = applyEventPatch(row, { location: 'Room 4', free: true, description: 'x' })
    expect(changed).toMatchObject({ location: 'Room 4', free: true })
    expect('description' in changed).toBe(false)
    expect(eventPatchBetween(row, changed)).toEqual({ location: 'Room 4', free: true })
    expect(isNoChange(row, { description: 'x' })).toBe(false)
    expect(isNoChange(row, { location: '' })).toBe(true)
  })
})

// calendar-event-series D11: a refresh keeps a queued scoped change on its one edited occurrence.
describe('queued scoped edits over a refresh', () => {
  const at = (date: string, time: string) => ({ date, time, tz: 'Europe/Berlin' })
  const occ = (day: number): CalendarEvent => ({
    id: `o${day}`, subscriptionId: 's1', uid: 'routine', summary: 'Routine', location: '', allDay: false, free: false, rev: 'r1',
    start: at(`2026-11-${10 + day}`, '07:00'), end: at(`2026-11-${10 + day}`, '08:30'), series: { id: 'ser', rev: 'r1' },
  })
  const command = (patch: EventCommand['patch'], scope: EventCommand['scope']): EventCommand => ({ key: 'c1:o2', connectionId: 'c1', eventId: 'o2', rev: 'r1', base: occ(2), patch, notBefore: 0, tries: 0, scope })

  it.each(['following', 'all'] as const)('applies a queued %s change to its occurrence only', (scope) => {
    const events = new Map([1, 2, 3].map((d) => [`o${d}`, occ(d)]))
    overlayQueued(events, [command({ start: at('2026-11-12', '09:00'), end: at('2026-11-12', '10:30'), allDay: false }, scope)])
    expect([...events.values()].map((e) => e.start.time)).toEqual(['07:00', '09:00', '07:00'])
  })

  it('does not put repeat or reminders on the row', () => {
    const events = new Map([['o2', occ(2)]])
    overlayQueued(events, [command({ repeat: { freq: 'daily', interval: 1, ends: { type: 'never' } }, reminders: [10] }, 'all')])
    expect(events.get('o2')).toEqual(occ(2))
  })

  it('counts a repeat or reminders change as a change, though no field of the row moves', () => {
    expect(isNoChange(occ(2), { repeat: null })).toBe(false)
    expect(isNoChange(occ(2), { reminders: [] })).toBe(false)
    expect(isNoChange(occ(2), {})).toBe(true)
  })
})
