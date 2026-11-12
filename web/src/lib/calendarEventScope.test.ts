// Scoped edits of repeating events' occurrences (OpenSpec change calendar-event-series, task 4.3, design D1, D11):
// scope travels in the outbox command and the request; this is optimistic, following and all show only the edited
// block until the answer and then refetch; series.rev lets a second edit of another occurrence pass.
import 'fake-indexeddb/auto'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { CalendarEvent, CalendarRepeat } from './types'

const updateCalendarEvent = vi.fn()
const deleteCalendarEvent = vi.fn()
const calendarEvents = vi.fn()
vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  api: {
    updateCalendarEvent: (...a: unknown[]) => updateCalendarEvent(...a),
    deleteCalendarEvent: (...a: unknown[]) => deleteCalendarEvent(...a),
    calendarEvents: (...a: unknown[]) => calendarEvents(...a),
    calendarFeedsSwitch: { get: async () => ({ enabled: false }) },
    calendarSyncSwitch: { get: async () => ({ enabled: true }) },
  },
}))

import { ApiError } from './api'
import { cachedWindow } from './calendarFeeds'
import { cancelEventDelete, currentEventId, DELETE_DELAY_MS, EDIT_SETTLE_MS, editEvent, eventAccessText, eventEditNoticeText, flushEventEdits, onEventEditNotice, type EventEditNotice } from './calendarEventEdits'
import { clearLocalData, db, setMeta, useDatabase } from './db'

const at = (time: string, date = '2026-11-12') => ({ date, time, tz: 'Europe/Berlin' })
const daily: CalendarRepeat = { freq: 'daily', interval: 1, days: [], ends: { type: 'never' } }
const occurrence = (n: number, over: Partial<CalendarEvent> = {}): CalendarEvent => ({
  id: `o${n}`, subscriptionId: 's1', uid: 'routine', summary: 'Routine', location: '', start: at('07:00', `2026-11-${10 + n}`), end: at('08:30', `2026-11-${10 + n}`), allDay: false, free: false, rev: 'r1',
  access: { update: true, delete: true }, series: { id: 'ser1', repeat: daily, rev: 'r1', first: false }, ...over,
})
const day2 = occurrence(2) // 2026-11-12
const day3 = occurrence(3) // 2026-11-13
const moved = (to: string, end: string, date = '2026-11-12') => ({ start: at(to, date), end: at(end, date), allDay: false })
const T0 = Date.now()
const SETTLED = T0 + EDIT_SETTLE_MS
const sub = { id: 's1', name: 'Work', color: '', enabled: true, lastSuccessAt: 2, managed: true, connectionId: 'c1' }

let n = 0
beforeEach(async () => {
  useDatabase(`event-scope-${Date.now()}-${n++}`)
  updateCalendarEvent.mockReset()
  deleteCalendarEvent.mockReset()
  calendarEvents.mockReset()
  await db.calendarSubscriptions.put({ ...sub })
  await db.calendarEvents.bulkPut([occurrence(1), day2, day3])
})

const edit = async (...a: Parameters<typeof editEvent>) => { const r = await editEvent(...a); await flushEventEdits(SETTLED); return r }

describe('the scope travels with the command', () => {
  it.each(['this', 'following', 'all'] as const)('sends scope %s in the request body', async (scope) => {
    updateCalendarEvent.mockResolvedValue({ event: { ...day2, id: 'o2', rev: 'r2' }, replaces: 'o2' })
    await edit(day2, moved('09:00', '10:30'), T0, undefined, scope)
    expect(updateCalendarEvent).toHaveBeenCalledWith('c1', 'o2', { rev: 'r1', scope, ...moved('09:00', '10:30') })
  })

  it('keeps the scope in the command (and nothing for a single event)', async () => {
    updateCalendarEvent.mockRejectedValue(new TypeError('offline'))
    await editEvent(day2, moved('09:00', '10:30'), T0, undefined, 'all')
    expect((await db.eventOutbox.get('c1:o2'))!.scope).toBe('all')
    const single = { ...day2, id: 'solo', series: undefined }
    await db.calendarEvents.put(single)
    await editEvent(single, { summary: 'x' }, T0)
    expect((await db.eventOutbox.get('c1:solo'))!.scope).toBeUndefined()
  })

  it('offline then online sends one request with the scope', async () => {
    updateCalendarEvent.mockRejectedValue(new TypeError('offline'))
    await edit(day2, moved('09:00', '10:30'), T0, undefined, 'all')
    await edit(day2, moved('10:00', '11:30'), T0, undefined, 'all')
    expect(await db.eventOutbox.count()).toBe(1)
    updateCalendarEvent.mockReset()
    updateCalendarEvent.mockResolvedValue({ event: { ...day2, rev: 'r2' }, replaces: 'o2', series: { id: 'ser1', rev: 'r2' } })
    await flushEventEdits(T0 + 10 * 60_000)
    expect(updateCalendarEvent).toHaveBeenCalledTimes(1)
    expect(updateCalendarEvent).toHaveBeenCalledWith('c1', 'o2', { rev: 'r1', scope: 'all', ...moved('10:00', '11:30') })
  })

  it('sends repeat and reminders with the patch and never treats them as "no change"', async () => {
    updateCalendarEvent.mockRejectedValue(new TypeError('offline'))
    const weekly: CalendarRepeat = { freq: 'weekly', interval: 1, days: ['mo'], ends: { type: 'never' } }
    await editEvent(day2, { repeat: weekly, reminders: [10] }, T0, undefined, 'all')
    expect((await db.eventOutbox.get('c1:o2'))!.patch).toEqual({ repeat: weekly, reminders: [10] })
    await editEvent(day2, { repeat: weekly, reminders: [10] }, T0, undefined, 'all') // a merge that changes no cached field stays queued
    expect(await db.eventOutbox.count()).toBe(1)
  })
})

describe('merging and waiting', () => {
  it('merges an edit with the same scope into the command', async () => {
    updateCalendarEvent.mockRejectedValue(new TypeError('offline'))
    await editEvent(day2, moved('09:00', '10:30'), T0, undefined, 'all')
    await editEvent(day2, { summary: 'Renamed' }, T0, undefined, 'all')
    expect((await db.eventOutbox.get('c1:o2'))!.patch).toEqual({ ...moved('09:00', '10:30'), summary: 'Renamed' })
    expect((await db.eventOutbox.get('c1:o2'))!.next).toBeUndefined()
  })

  it('an edit with another scope waits in next, carrying its scope, and is sent after the answer against the new id and rev', async () => {
    updateCalendarEvent.mockRejectedValueOnce(new TypeError('offline')) // keep the first one queued
    await edit(day2, { summary: 'Routine B' }, T0, undefined, 'all')
    await editEvent(day2, moved('09:00', '10:30'), T0, undefined, 'this')
    const cmd = (await db.eventOutbox.get('c1:o2'))!
    expect(cmd).toMatchObject({ scope: 'all', patch: { summary: 'Routine B' }, next: moved('09:00', '10:30'), nextScope: 'this' })
    expect((await db.calendarEvents.get('o2'))!.start.time).toBe('09:00') // drawn at once
    updateCalendarEvent.mockReset()
    updateCalendarEvent.mockResolvedValueOnce({ event: { ...day2, id: 'o2b', rev: 'r2', summary: 'Routine B' }, replaces: 'o2', series: { id: 'ser1', rev: 'r2' }, refetch: true })
    updateCalendarEvent.mockResolvedValueOnce({ event: { ...day2, id: 'o2b', rev: 'r3' }, replaces: 'o2b' })
    calendarEvents.mockRejectedValue(new TypeError('offline'))
    await flushEventEdits(T0 + 10 * 60_000)
    await vi.waitFor(() => expect(updateCalendarEvent).toHaveBeenCalledTimes(2))
    expect(updateCalendarEvent).toHaveBeenNthCalledWith(1, 'c1', 'o2', { rev: 'r1', scope: 'all', summary: 'Routine B' })
    expect(updateCalendarEvent).toHaveBeenNthCalledWith(2, 'c1', 'o2b', { rev: 'r2', scope: 'this', ...moved('09:00', '10:30') })
    expect(currentEventId('o2')).toBe('o2b')
  })

  it('a third scope while two wait is blocked with a toast and changes nothing', async () => {
    const notices: EventEditNotice[] = []
    onEventEditNotice((x) => notices.push(x))
    updateCalendarEvent.mockRejectedValue(new TypeError('offline'))
    await editEvent(day2, { summary: 'A' }, T0, undefined, 'all')
    await editEvent(day2, { location: 'Gym' }, T0, undefined, 'this')
    expect(await editEvent(day2, { free: true }, T0, undefined, 'following')).toBeNull()
    expect(notices).toHaveLength(1)
    expect(eventEditNoticeText(notices[0])).toBe('Saving this change first')
    const cmd = (await db.eventOutbox.get('c1:o2'))!
    expect(cmd.patch).toEqual({ summary: 'A' })
    expect(cmd.next).toEqual({ location: 'Gym' })
    expect((await db.calendarEvents.get('o2'))!.free).toBe(false)
  })

  it('a later edit with the scope of the waiting one merges into it', async () => {
    updateCalendarEvent.mockRejectedValue(new TypeError('offline'))
    await editEvent(day2, { summary: 'A' }, T0, undefined, 'all')
    await editEvent(day2, { location: 'Gym' }, T0, undefined, 'this')
    await editEvent(day2, { free: true }, T0, undefined, 'this')
    expect((await db.eventOutbox.get('c1:o2'))!.next).toEqual({ location: 'Gym', free: true })
  })

  it('an edit made while a following or all change is in flight waits for it too', async () => {
    let finish!: (v: unknown) => void
    updateCalendarEvent.mockImplementationOnce(() => new Promise((resolve) => { finish = resolve }))
    await editEvent(day2, moved('09:00', '10:30'), T0, undefined, 'following')
    void flushEventEdits(SETTLED)
    await vi.waitFor(async () => expect((await db.eventOutbox.get('c1:o2'))?.sending).toBe(true))
    await editEvent(day2, { summary: 'X' }, T0, undefined, 'this')
    expect((await db.eventOutbox.get('c1:o2'))).toMatchObject({ next: { summary: 'X' }, nextScope: 'this' })
    calendarEvents.mockRejectedValue(new TypeError('offline'))
    updateCalendarEvent.mockResolvedValueOnce({ event: { ...day2, id: 'o2c', rev: 'r5' }, replaces: 'o2c' })
    finish({ event: { ...day2, id: 'o2c', rev: 'r4', start: at('09:00'), end: at('10:30') }, replaces: 'o2', series: { id: 'ser1', rev: 'r4' }, refetch: true })
    await vi.waitFor(() => expect(updateCalendarEvent).toHaveBeenCalledTimes(2))
    expect(updateCalendarEvent).toHaveBeenLastCalledWith('c1', 'o2c', { rev: 'r4', scope: 'this', summary: 'X' })
  })
})

describe('following and all show only the edited block until the answer, then refetch', () => {
  it('leaves other occurrences untouched until the answer, then the refetch replaces them', async () => {
    await setMeta('calendar.events.window', { from: '2026-11-01', to: '2026-11-30', fetchedAt: 1 })
    let finish!: (v: unknown) => void
    updateCalendarEvent.mockImplementationOnce(() => new Promise((resolve) => { finish = resolve }))
    await editEvent(day2, moved('09:00', '10:30'), T0, undefined, 'all')
    void flushEventEdits(SETTLED)
    await vi.waitFor(() => expect(updateCalendarEvent).toHaveBeenCalledTimes(1))
    expect((await db.calendarEvents.get('o2'))!.start.time).toBe('09:00')
    expect((await db.calendarEvents.get('o1'))!.start.time).toBe('07:00')
    expect((await db.calendarEvents.get('o3'))!.start.time).toBe('07:00')
    expect(calendarEvents).not.toHaveBeenCalled()
    const fresh = [1, 2, 3].map((k) => occurrence(k, { id: `n${k}`, rev: 'r2', start: at('09:00', `2026-11-${10 + k}`), end: at('10:30', `2026-11-${10 + k}`), series: { id: 'ser1', repeat: daily, rev: 'r2', first: false } }))
    calendarEvents.mockResolvedValue({ events: fresh, subscriptions: [sub] })
    finish({ event: fresh[1], replaces: 'o2', series: { id: 'ser1', rev: 'r2' }, refetch: true })
    await vi.waitFor(async () => expect((await db.calendarEvents.toArray()).map((e) => e.id).sort()).toEqual(['n1', 'n2', 'n3']))
    expect(calendarEvents).toHaveBeenCalledTimes(1)
    expect(await cachedWindow()).not.toBeNull()
  })

  it('draws a queued following change on its one occurrence when a refresh runs', async () => {
    await setMeta('calendar.events.window', { from: '2026-11-01', to: '2026-11-30', fetchedAt: 1 })
    updateCalendarEvent.mockRejectedValue(new TypeError('offline'))
    calendarEvents.mockResolvedValue({ events: [occurrence(1), day2, day3], subscriptions: [sub] })
    await edit(day2, moved('09:00', '10:30'), T0, undefined, 'following')
    const { refreshEvents } = await import('./calendarFeeds')
    await refreshEvents({ from: '2026-11-10', to: '2026-11-16' })
    const rows = Object.fromEntries((await db.calendarEvents.toArray()).map((e) => [e.id, e.start.time]))
    expect(rows).toEqual({ o1: '07:00', o2: '09:00', o3: '07:00' })
  })

  it('does not refetch after a this change that moved nothing else', async () => {
    updateCalendarEvent.mockResolvedValue({ event: { ...day2, rev: 'r2', start: at('09:00'), end: at('10:30') }, replaces: 'o2', series: { id: 'ser1', rev: 'r2' } })
    await edit(day2, moved('09:00', '10:30'), T0, undefined, 'this')
    expect(calendarEvents).not.toHaveBeenCalled()
  })

  it('an answer without an event (it left the cached window) drops the row', async () => {
    updateCalendarEvent.mockResolvedValue({ event: null, replaces: 'o2', series: { id: 'ser1', rev: 'r2' } })
    await edit(day2, moved('09:00', '10:30'), T0, undefined, 'this')
    expect(await db.calendarEvents.get('o2')).toBeUndefined()
    expect(await db.eventOutbox.count()).toBe(0)
  })
})

describe('series.rev', () => {
  it('is applied to every cached occurrence of the series, so a second edit of another occurrence is not stale', async () => {
    updateCalendarEvent.mockResolvedValueOnce({ event: { ...day2, rev: 'r2', series: { id: 'ser1', repeat: daily, rev: 'r2', first: false } }, replaces: 'o2', series: { id: 'ser1', rev: 'r2' } })
    await edit(day2, { summary: 'Renamed' }, T0, undefined, 'this')
    for (const id of ['o1', 'o3']) {
      const row = (await db.calendarEvents.get(id))!
      expect([row.rev, row.series!.rev]).toEqual(['r2', 'r2'])
    }
    updateCalendarEvent.mockResolvedValueOnce({ event: { ...day3, rev: 'r3' }, replaces: 'o3', series: { id: 'ser1', rev: 'r3' } })
    await edit((await db.calendarEvents.get('o3'))!, { summary: 'Other' }, T0, undefined, 'this')
    expect(updateCalendarEvent).toHaveBeenLastCalledWith('c1', 'o3', { rev: 'r2', scope: 'this', summary: 'Other' })
  })

  it('leaves the rows of another series alone', async () => {
    const other = occurrence(1, { id: 'x1', series: { id: 'ser2', repeat: daily, rev: 'rX' }, rev: 'rX' })
    await db.calendarEvents.put(other)
    updateCalendarEvent.mockResolvedValueOnce({ event: { ...day2, rev: 'r2' }, replaces: 'o2', series: { id: 'ser1', rev: 'r2' } })
    await edit(day2, { summary: 'Renamed' }, T0, undefined, 'this')
    expect((await db.calendarEvents.get('x1'))!.rev).toBe('rX')
  })
})

describe('refusals and sign-out', () => {
  it.each([[400, 'scope_required'], [422, 'event_not_editable'], [409, 'event_changed']])('a %i %s refusal restores the occurrence and refetches', async (status, code) => {
    await setMeta('calendar.events.window', { from: '2026-11-01', to: '2026-11-30', fetchedAt: 1 })
    calendarEvents.mockResolvedValue({ events: [occurrence(1), day2, day3], subscriptions: [sub] })
    updateCalendarEvent.mockRejectedValue(new ApiError(status, code, `says ${code}`))
    await edit(day2, moved('09:00', '10:30'), T0, undefined, 'all')
    await vi.waitFor(async () => expect(await db.eventOutbox.count()).toBe(0))
    await vi.waitFor(() => expect(calendarEvents).toHaveBeenCalled())
    expect((await db.calendarEvents.get('o2'))!.start.time).toBe('07:00')
  })

  it('words the new refusals', () => {
    expect(eventEditNoticeText({ code: 'scope_required', title: 'Routine', message: 'm' })).toBe('Choose which events to change.')
    expect(eventAccessText('series_unsupported')).toBe('This repeat is too complex to change here')
    expect(eventAccessText('recurring')).toBe('')
  })

  it('words a 422 by the reason the server gave', () => {
    const err = new ApiError(422, 'event_not_editable', 'this event can not be edited here: following_unsupported')
    const notices: EventEditNotice[] = []
    onEventEditNotice((x) => notices.push(x))
    expect(eventEditNoticeText({ code: err.code, title: 'Routine', message: err.message, reason: 'series_unsupported' })).toBe('This repeat is too complex to change here')
    expect(eventEditNoticeText({ code: err.code, title: 'Routine', message: err.message, reason: 'following_unsupported' })).toBe('Splitting this series would lose its attachments or online meeting')
    expect(eventEditNoticeText({ code: err.code, title: 'Routine', message: 'generic' })).toBe('generic')
  })

  it('hands the reason of a refused edit to the notice', async () => {
    const notices: EventEditNotice[] = []
    onEventEditNotice((x) => notices.push(x))
    calendarEvents.mockRejectedValue(new TypeError('offline'))
    updateCalendarEvent.mockRejectedValue(new ApiError(422, 'event_not_editable', 'this event can not be edited here: series_unsupported'))
    await edit(day2, moved('09:00', '10:30'), T0, undefined, 'all')
    await vi.waitFor(() => expect(notices).toHaveLength(1))
    expect(notices[0]).toMatchObject({ code: 'event_not_editable', reason: 'series_unsupported' })
    expect(eventEditNoticeText(notices[0])).toBe('This repeat is too complex to change here')
  })

  it('sign-out deletes the outbox with the scope in it', async () => {
    updateCalendarEvent.mockRejectedValue(new TypeError('offline'))
    await editEvent(day2, moved('09:00', '10:30'), T0, undefined, 'all')
    await clearLocalData()
    expect(await db.eventOutbox.count()).toBe(0)
  })
})

describe('delete with a scope', () => {
  it.each(['this', 'following', 'all'] as const)('waits the 8 seconds and then sends scope %s', async (scope) => {
    deleteCalendarEvent.mockResolvedValue(undefined)
    await editEvent(day2, { delete: true }, T0, undefined, scope)
    expect((await db.calendarEvents.get('o2'))).toBeUndefined()
    await flushEventEdits(T0 + DELETE_DELAY_MS - 1)
    expect(deleteCalendarEvent).not.toHaveBeenCalled()
    await flushEventEdits(T0 + DELETE_DELAY_MS + 50)
    expect(deleteCalendarEvent).toHaveBeenCalledWith('c1', 'o2', 'r1', scope)
  })

  it('Undo within the delay cancels it for any scope', async () => {
    await editEvent(day2, { delete: true }, T0, undefined, 'all')
    expect(await cancelEventDelete(day2)).toBe(true)
    await flushEventEdits(T0 + DELETE_DELAY_MS + 50)
    expect(deleteCalendarEvent).not.toHaveBeenCalled()
    expect(await db.calendarEvents.get('o2')).toEqual(day2)
  })

  it('a delete after a queued edit of another scope waits for it instead of replacing it', async () => {
    updateCalendarEvent.mockRejectedValue(new TypeError('offline'))
    await editEvent(day2, { summary: 'A' }, T0, undefined, 'all')
    await editEvent(day2, { delete: true }, T0, undefined, 'this')
    const cmd = (await db.eventOutbox.get('c1:o2'))!
    expect(cmd.patch).toEqual({ summary: 'A' })
    expect(cmd.next).toEqual({ delete: true })
    expect(cmd.nextScope).toBe('this')
  })
})
