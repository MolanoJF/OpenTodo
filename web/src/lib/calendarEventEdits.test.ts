import 'fake-indexeddb/auto'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { CalendarEvent } from './types'

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
import { cachedWindow, refreshEvents } from './calendarFeeds'
import { cancelEventDelete, DELETE_DELAY_MS, EDIT_SETTLE_MS, editEvent, eventAccessText, eventEditNoticeText, flushEventEdits, onEventEditNotice, type EventEditNotice } from './calendarEventEdits'
import { db, setMeta, useDatabase } from './db'

const at = (time: string) => ({ date: '2026-11-12', time, tz: 'Europe/Berlin' })
const base: CalendarEvent = { id: 'e1', subscriptionId: 's1', uid: 'u1', summary: 'Standup', location: '', start: at('09:00'), end: at('09:30'), allDay: false, free: false, rev: 'r1', access: { update: true, delete: true } }
const moved = (from: string, to: string) => ({ start: at(from), end: at(to), allDay: false })
const T0 = Date.now()
const SETTLED = T0 + EDIT_SETTLE_MS

let n = 0
beforeEach(async () => {
  useDatabase(`event-edits-${Date.now()}-${n++}`)
  updateCalendarEvent.mockReset()
  deleteCalendarEvent.mockReset()
  calendarEvents.mockReset()
  await db.calendarSubscriptions.put({ id: 's1', name: 'Work', color: '', enabled: true, lastSuccessAt: 1, managed: true, connectionId: 'c1' })
  await db.calendarEvents.put(base)
})

/** An edit, then the send it starts: the tests step through the outbox one move at a time. */
const edit = async (...a: Parameters<typeof editEvent>) => { const r = await editEvent(...a); await flushEventEdits(SETTLED); return r }
const refused = (status: number, code: string) => new ApiError(status, code, `server says ${code}`)

describe('editEvent: merge, optimistic patch, inverse', () => {
  it('patches the cached row at once and queues one command', async () => {
    updateCalendarEvent.mockRejectedValue(new TypeError('offline'))
    const inverse = await edit(base, moved('10:00', '10:30'), T0)
    expect(inverse).toEqual(moved('09:00', '09:30'))
    expect((await db.calendarEvents.get('e1'))!.start.time).toBe('10:00')
    const cmd = (await db.eventOutbox.get('c1:e1'))!
    expect(cmd).toMatchObject({ rev: 'r1', patch: moved('10:00', '10:30') })
    expect(cmd.base).toEqual(base)
  })

  it('coalesces two moves and a rename offline into one command, sent once when the device is back', async () => {
    updateCalendarEvent.mockRejectedValue(new TypeError('offline'))
    await edit(base, moved('10:00', '10:30'), T0)
    await edit(base, moved('11:00', '11:30'), T0)
    await edit(base, { summary: 'Planning' }, T0)
    expect(await db.eventOutbox.count()).toBe(1)
    expect((await db.eventOutbox.get('c1:e1'))!.patch).toEqual({ ...moved('11:00', '11:30'), summary: 'Planning' })
    // Offline: kept, with a backoff.
    const kept = (await db.eventOutbox.get('c1:e1'))!
    expect(kept.tries).toBeGreaterThan(0)
    expect(kept.notBefore).toBeGreaterThan(T0)
    // Online again: one request.
    updateCalendarEvent.mockReset()
    updateCalendarEvent.mockResolvedValue({ event: { ...base, id: 'e2', rev: 'r2', summary: 'Planning', start: at('11:00'), end: at('11:30') }, replaces: 'e1' })
    await flushEventEdits(T0 + 10 * 60_000)
    expect(updateCalendarEvent).toHaveBeenCalledTimes(1)
    expect(updateCalendarEvent).toHaveBeenCalledWith('c1', 'e1', { rev: 'r1', ...moved('11:00', '11:30'), summary: 'Planning' })
    expect(await db.eventOutbox.count()).toBe(0)
  })

  it('removes the command when the event is moved back before it is sent', async () => {
    updateCalendarEvent.mockRejectedValue(new TypeError('offline'))
    await edit(base, moved('10:00', '10:30'), T0)
    await edit(base, moved('09:00', '09:30'), T0)
    expect(await db.eventOutbox.count()).toBe(0)
    expect((await db.calendarEvents.get('e1'))!.start.time).toBe('09:00')
    updateCalendarEvent.mockReset()
    await flushEventEdits(T0 + 60_000)
    expect(updateCalendarEvent).not.toHaveBeenCalled()
  })

  it('is not an edit for an event without a connection or a rev', async () => {
    await db.calendarSubscriptions.put({ id: 's2', name: 'Feed', color: '', enabled: true, lastSuccessAt: 1 })
    const feedEvent = { ...base, id: 'f1', subscriptionId: 's2' }
    await db.calendarEvents.put(feedEvent)
    expect(await edit(feedEvent, { summary: 'x' }, T0)).toBeNull()
    const unversioned = { ...base, id: 'n1', uid: 'n1', rev: undefined }
    await db.calendarEvents.put(unversioned)
    expect(await edit(unversioned, { summary: 'x' }, T0)).toBeNull()
    expect(await db.eventOutbox.count()).toBe(0)
  })

  it('Undo is an edit with the inverse', async () => {
    updateCalendarEvent.mockRejectedValue(new TypeError('offline'))
    const inverse = await edit(base, moved('10:00', '10:30'), T0)
    await edit(base, inverse as never, T0)
    expect(await db.eventOutbox.count()).toBe(0)
    expect(await db.calendarEvents.get('e1')).toEqual(base)
  })
})

describe('delete', () => {
  it('waits for Undo, then sends; a delete replaces an edit and wins', async () => {
    updateCalendarEvent.mockRejectedValue(new TypeError('offline'))
    await edit(base, moved('10:00', '10:30'), T0)
    deleteCalendarEvent.mockResolvedValue(undefined)
    await edit(base, { delete: true }, T0)
    expect(await db.calendarEvents.get('e1')).toBeUndefined()
    await flushEventEdits(T0 + DELETE_DELAY_MS - 1)
    expect(deleteCalendarEvent).not.toHaveBeenCalled()
    await flushEventEdits(T0 + DELETE_DELAY_MS)
    expect(deleteCalendarEvent).toHaveBeenCalledWith('c1', 'e1', 'r1')
    expect(await db.eventOutbox.count()).toBe(0)
  })

  it('Undo within the delay removes the command, no request is made, the event is back', async () => {
    await edit(base, { delete: true }, T0)
    expect(await cancelEventDelete(base)).toBe(true)
    expect(await db.eventOutbox.count()).toBe(0)
    expect(await db.calendarEvents.get('e1')).toEqual(base)
    await flushEventEdits(T0 + 60_000)
    expect(deleteCalendarEvent).not.toHaveBeenCalled()
  })

  it('Undo of a delete that followed a queued move keeps the move queued', async () => {
    updateCalendarEvent.mockRejectedValue(new TypeError('offline'))
    await edit(base, moved('10:00', '10:30'), T0)
    const shown = (await db.calendarEvents.get('e1'))!
    await edit(shown, { delete: true }, T0)
    expect(await cancelEventDelete(shown)).toBe(true)
    expect((await db.eventOutbox.get('c1:e1'))!.patch).toEqual(moved('10:00', '10:30'))
    expect(await db.calendarEvents.get('e1')).toEqual(shown)
  })

  it('cannot be undone once sent', async () => {
    deleteCalendarEvent.mockResolvedValue(undefined)
    await edit(base, { delete: true }, T0)
    await flushEventEdits(T0 + DELETE_DELAY_MS)
    expect(await cancelEventDelete(base)).toBe(false)
  })
})

describe('flushEventEdits', () => {
  it('waits for the next key press: two quick edits are one request', async () => {
    updateCalendarEvent.mockResolvedValue({ event: { ...base, id: 'e2', rev: 'r2', start: at('09:30'), end: at('10:00') }, replaces: 'e1' })
    await editEvent(base, moved('09:15', '09:45'), T0)
    await editEvent(base, moved('09:30', '10:00'), T0 + 100)
    await flushEventEdits(T0 + EDIT_SETTLE_MS) // the first edit's wait is over, the second's is not
    expect(updateCalendarEvent).not.toHaveBeenCalled()
    await flushEventEdits(T0 + 100 + EDIT_SETTLE_MS)
    expect(updateCalendarEvent).toHaveBeenCalledTimes(1)
    expect(updateCalendarEvent).toHaveBeenCalledWith('c1', 'e1', { rev: 'r1', ...moved('09:30', '10:00') })
  })

  it('swaps the old row for the answer on success', async () => {
    const answer = { ...base, id: 'e2', rev: 'r2', start: at('10:00'), end: at('10:30') }
    updateCalendarEvent.mockResolvedValue({ event: answer, replaces: 'e1' })
    await edit(base, moved('10:00', '10:30'), T0)
    await flushEventEdits(SETTLED)
    expect(await db.calendarEvents.get('e1')).toBeUndefined()
    expect(await db.calendarEvents.get('e2')).toEqual(answer)
    expect(await db.eventOutbox.count()).toBe(0)
  })

  it('an edit made while one is in flight is sent after the swap, against the new id and rev', async () => {
    let finish!: (v: unknown) => void
    updateCalendarEvent.mockImplementationOnce(() => new Promise((resolve) => { finish = resolve }))
    await editEvent(base, moved('10:00', '10:30'), T0)
    void flushEventEdits(SETTLED)
    await vi.waitFor(() => expect(updateCalendarEvent).toHaveBeenCalledTimes(1))
    await vi.waitFor(async () => expect((await db.eventOutbox.get('c1:e1'))!.sending).toBe(true))
    // A rapid second drag on the same block, before the answer.
    await editEvent(base, moved('11:00', '11:30'), T0)
    expect((await db.eventOutbox.get('c1:e1'))!.next).toEqual(moved('11:00', '11:30'))
    expect((await db.calendarEvents.get('e1'))!.start.time).toBe('11:00')
    updateCalendarEvent.mockResolvedValueOnce({ event: { ...base, id: 'e3', rev: 'r3', start: at('11:00'), end: at('11:30') }, replaces: 'e2' })
    finish({ event: { ...base, id: 'e2', rev: 'r2', start: at('10:00'), end: at('10:30') }, replaces: 'e1' })
    await vi.waitFor(() => expect(updateCalendarEvent).toHaveBeenCalledTimes(2))
    expect(updateCalendarEvent).toHaveBeenLastCalledWith('c1', 'e2', { rev: 'r2', ...moved('11:00', '11:30') })
    await vi.waitFor(async () => expect(await db.eventOutbox.count()).toBe(0))
    expect(await db.calendarEvents.toArray()).toEqual([expect.objectContaining({ id: 'e3', rev: 'r3' })])
  })

  it('a delete asked while an edit is in flight is sent after the swap', async () => {
    let finish!: (v: unknown) => void
    updateCalendarEvent.mockImplementationOnce(() => new Promise((resolve) => { finish = resolve }))
    deleteCalendarEvent.mockResolvedValue(undefined)
    await editEvent(base, moved('10:00', '10:30'), T0)
    void flushEventEdits(SETTLED)
    await vi.waitFor(async () => expect((await db.eventOutbox.get('c1:e1'))?.sending).toBe(true))
    await editEvent(base, { delete: true }, T0)
    finish({ event: { ...base, id: 'e2', rev: 'r2', start: at('10:00'), end: at('10:30') }, replaces: 'e1' })
    await vi.waitFor(() => expect(deleteCalendarEvent).toHaveBeenCalledWith('c1', 'e2', 'r2'))
    await vi.waitFor(async () => expect(await db.eventOutbox.count()).toBe(0))
    expect(await db.calendarEvents.count()).toBe(0)
  })

  it('a refusal (409) restores the event, drops the command, refetches and says why; two devices converge on the first', async () => {
    const notices: EventEditNotice[] = []
    onEventEditNotice((x) => notices.push(x))
    await setMeta('calendar.events.window', { from: '2026-11-01', to: '2026-11-30', fetchedAt: 1 })
    // The first device already moved it to 11:00: the server's event has a new id and rev.
    const winner = { ...base, id: 'e9', rev: 'r9', start: at('11:00'), end: at('11:30') }
    calendarEvents.mockResolvedValue({ events: [winner], subscriptions: [{ id: 's1', name: 'Work', color: '', enabled: true, lastSuccessAt: 2, managed: true, connectionId: 'c1' }] })
    updateCalendarEvent.mockRejectedValue(refused(409, 'event_changed'))
    await edit(base, moved('10:00', '10:30'), T0)
    await flushEventEdits(SETTLED)
    await vi.waitFor(async () => expect((await db.calendarEvents.toArray()).map((e) => e.id)).toEqual(['e9']))
    expect(await db.eventOutbox.count()).toBe(0)
    expect(notices).toEqual([{ code: 'event_changed', title: 'Standup', message: 'server says event_changed' }])
    expect(eventEditNoticeText(notices[0])).toBe('Standup changed in your calendar; your edit was not applied.')
    expect(await cachedWindow()).not.toBeNull()
  })

  it.each([[404, 'event_gone'], [403, 'event_edits_off'], [422, 'event_not_editable'], [400, 'invalid_input']])('a %i %s refusal restores the row and drops the command', async (status, code) => {
    calendarEvents.mockRejectedValue(new TypeError('offline')) // the refetch is not the point here
    updateCalendarEvent.mockRejectedValue(refused(status, code))
    await edit(base, { summary: 'Planning' }, T0)
    await flushEventEdits(SETTLED)
    await vi.waitFor(async () => expect(await db.eventOutbox.count()).toBe(0))
    expect(await db.calendarEvents.get('e1')).toEqual(base)
  })

  it('keeps the command on a 5xx and retries with a growing backoff', async () => {
    updateCalendarEvent.mockRejectedValue(new ApiError(503, 'unavailable', 'down'))
    await edit(base, { summary: 'Planning' }, T0)
    const first = (await db.eventOutbox.get('c1:e1'))!
    expect(first.tries).toBe(1)
    await flushEventEdits(first.notBefore)
    const second = (await db.eventOutbox.get('c1:e1'))!
    expect(second.tries).toBe(2)
    expect(second.notBefore - first.notBefore).toBeGreaterThan(first.notBefore - T0)
  })
})

describe('refreshEvents keeps queued edits (4.5)', () => {
  const sub = { id: 's1', name: 'Work', color: '', enabled: true, lastSuccessAt: 2, managed: true, connectionId: 'c1' }
  it('draws a queued edit over the fetched row and hides a queued delete', async () => {
    updateCalendarEvent.mockRejectedValue(new TypeError('offline'))
    calendarEvents.mockResolvedValue({ events: [base, { ...base, id: 'e5', uid: 'u5' }], subscriptions: [sub] })
    await edit(base, moved('10:00', '10:30'), T0)
    await edit({ ...base, id: 'e5', uid: 'u5' }, { delete: true }, T0)
    await refreshEvents({ from: '2026-11-10', to: '2026-11-16' })
    const rows = await db.calendarEvents.toArray()
    expect(rows.map((e) => e.id)).toEqual(['e1'])
    expect(rows[0].start.time).toBe('10:00')
  })
})

describe('texts', () => {
  it('says why an event is locked', () => {
    expect(eventAccessText('recurring')).toBe('Repeating events are read-only for now')
    expect(eventAccessText('has_guests')).toBe('This event has guests')
    expect(eventAccessText('nothing-known')).toBe('')
  })
})
