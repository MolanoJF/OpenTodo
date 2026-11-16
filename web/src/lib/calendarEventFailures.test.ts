// A calendar event write that failed is a visible state, never retried silently (OpenSpec change
// calendar-event-edits D6): the server answering with a failure while the device is online stops the
// command and marks it failed; only offline commands wait, and they are sent when the device is back.
import 'fake-indexeddb/auto'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { CalendarEvent, CalendarEventDraft } from './types'

const updateCalendarEvent = vi.fn()
const deleteCalendarEvent = vi.fn()
const createCalendarEvent = vi.fn()
const calendarEvents = vi.fn()
vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  api: {
    updateCalendarEvent: (...a: unknown[]) => updateCalendarEvent(...a),
    deleteCalendarEvent: (...a: unknown[]) => deleteCalendarEvent(...a),
    createCalendarEvent: (...a: unknown[]) => createCalendarEvent(...a),
    calendarEvents: (...a: unknown[]) => calendarEvents(...a),
    calendarFeedsSwitch: { get: async () => ({ enabled: false }) },
    calendarSyncSwitch: { get: async () => ({ enabled: true }) },
  },
}))

import { ApiError } from './api'
import { DELETE_DELAY_MS, discardEventWrite, EDIT_SETTLE_MS, editEvent, eventEditNoticeText, flushEventEdits, onEventEditNotice, retryEventWrite, writeStateOf, type EventEditNotice } from './calendarEventEdits'
import { db, useDatabase } from './db'
import { createEvent } from './eventCreate'
import { resetModuleState } from './moduleState'
import { platform } from './platform'

let online = true
vi.spyOn(platform.network, 'online').mockImplementation(() => online)

const at = (time: string) => ({ date: '2026-11-12', time, tz: 'Europe/Berlin' })
const base: CalendarEvent = { id: 'e1', subscriptionId: 's1', uid: 'u1', summary: 'Standup', location: '', start: at('09:00'), end: at('09:30'), allDay: false, free: false, rev: 'r1', access: { update: true, delete: true } }
const moved = { start: at('10:00'), end: at('10:30'), allDay: false }
const draft: CalendarEventDraft = { clientId: 'k1', connectionId: 'c1', calendarUrl: 'https://dav.example.com/work/', subscriptionId: 's1', summary: 'Dentist', start: at('09:00'), end: at('09:30'), allDay: false, location: '', description: '', free: false }
const T0 = Date.now()
const SETTLED = T0 + EDIT_SETTLE_MS
const HOUR = 60 * 60_000
const down = () => new ApiError(502, 'calendar_unreachable', 'timeout')

let notices: EventEditNotice[] = []
let n = 0
beforeEach(async () => {
  online = true
  useDatabase(`event-failures-${Date.now()}-${n++}`)
  for (const f of [updateCalendarEvent, deleteCalendarEvent, createCalendarEvent, calendarEvents]) f.mockReset()
  calendarEvents.mockRejectedValue(new TypeError('offline')) // the refetch after a rollback is not the point here
  await db.calendarSubscriptions.put({ id: 's1', name: 'Work', color: '', enabled: true, lastSuccessAt: 1, managed: true, connectionId: 'c1' })
  await db.calendarEvents.put(base)
  notices = []
  onEventEditNotice((x) => notices.push(x))
})
afterEach(() => { vi.useRealTimers() })

/** An edit and the send it starts. */
const edit = async (change: Parameters<typeof editEvent>[1], ev = base, scope?: Parameters<typeof editEvent>[4]) => { await editEvent(ev, change, T0, undefined, scope); await flushEventEdits(SETTLED) }

describe('a server failure while online', () => {
  it('stops the command, keeps the drawn change, says why with Retry and Discard, and never sends it again by itself', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    updateCalendarEvent.mockRejectedValue(down())
    await edit({ summary: 'Planning' })
    const cmd = (await db.eventOutbox.get('c1:e1'))!
    expect(cmd).toMatchObject({ sending: false, failed: { code: 'calendar_unreachable', status: 502 } })
    expect(writeStateOf(cmd)).toBe('failed')
    expect((await db.calendarEvents.get('e1'))!.summary).toBe('Planning')
    expect(notices).toHaveLength(1)
    expect(notices[0]).toMatchObject({ code: 'calendar_unreachable', failure: { key: 'c1:e1' } })
    expect(eventEditNoticeText(notices[0])).toContain('Standup was not saved')
    // Well past the old five minute backoff, and every trigger there is: nothing is sent again.
    await vi.advanceTimersByTimeAsync(30 * 60_000)
    await flushEventEdits(T0 + HOUR)
    await flushEventEdits(T0 + 2 * HOUR)
    expect(updateCalendarEvent).toHaveBeenCalledTimes(1)
    expect((await db.eventOutbox.get('c1:e1'))!.failed).toBeDefined()
  })

  it('treats no answer while the device is online as a failure, and a refused sign-in too', async () => {
    updateCalendarEvent.mockRejectedValueOnce(new TypeError('Failed to fetch'))
    await edit({ summary: 'Planning' })
    expect((await db.eventOutbox.get('c1:e1'))!.failed).toMatchObject({ code: 'no_answer' })
    await discardEventWrite('c1:e1')
    updateCalendarEvent.mockRejectedValueOnce(new ApiError(403, 'reauth_required', 'confirm it is you'))
    await edit({ summary: 'Planning' })
    expect((await db.eventOutbox.get('c1:e1'))!.failed).toMatchObject({ code: 'reauth_required', status: 403 })
    expect(await db.eventOutbox.count()).toBe(1)
  })

  it('Retry sends the same command once, with the same version', async () => {
    updateCalendarEvent.mockRejectedValueOnce(down())
    await edit({ summary: 'Planning' })
    updateCalendarEvent.mockResolvedValue({ event: { ...base, id: 'e2', rev: 'r2', summary: 'Planning' }, replaces: 'e1' })
    await retryEventWrite('c1:e1')
    expect(updateCalendarEvent).toHaveBeenCalledTimes(2)
    expect(updateCalendarEvent).toHaveBeenLastCalledWith('c1', 'e1', { rev: 'r1', summary: 'Planning' })
    expect(await db.eventOutbox.count()).toBe(0)
    expect((await db.calendarEvents.get('e2'))!.rev).toBe('r2')
  })

  it('Retry that meets a changed event (409) rolls back as any refusal does', async () => {
    updateCalendarEvent.mockRejectedValueOnce(down())
    await edit({ summary: 'Planning' })
    updateCalendarEvent.mockRejectedValue(new ApiError(409, 'event_changed', 'changed'))
    await retryEventWrite('c1:e1')
    expect(await db.eventOutbox.count()).toBe(0)
    expect(await db.calendarEvents.get('e1')).toEqual(base)
    expect(notices.at(-1)).toMatchObject({ code: 'event_changed' })
  })

  it('Discard rolls back without a notice and sends nothing', async () => {
    updateCalendarEvent.mockRejectedValue(down())
    await edit({ summary: 'Planning' })
    notices.length = 0
    await discardEventWrite('c1:e1')
    expect(await db.eventOutbox.count()).toBe(0)
    expect(await db.calendarEvents.get('e1')).toEqual(base)
    expect(notices).toEqual([])
    expect(updateCalendarEvent).toHaveBeenCalledTimes(1)
  })

  it('a 429 keeps its Retry-After in the text and still waits for Retry', async () => {
    updateCalendarEvent.mockRejectedValue(new ApiError(429, 'rate_limited', 'slow down', undefined, 30))
    await edit({ summary: 'Planning' })
    expect(notices[0].message).toBe('The server asks you to wait 30 seconds')
    expect((await db.eventOutbox.get('c1:e1'))!.failed).toMatchObject({ status: 429, retryAfter: 30 })
    await flushEventEdits(T0 + HOUR)
    expect(updateCalendarEvent).toHaveBeenCalledTimes(1)
  })

  it('a failed edit is not merged into: another change says to retry or discard first', async () => {
    updateCalendarEvent.mockRejectedValue(down())
    await edit({ summary: 'Planning' })
    notices.length = 0
    expect(await editEvent(base, moved, T0 + 1000)).toBeNull()
    expect(notices[0]).toMatchObject({ code: 'edit_blocked', message: 'Retry or discard the change that was not saved first' })
    expect((await db.eventOutbox.get('c1:e1'))!.patch).toEqual({ summary: 'Planning' })
  })

  it('keeps the scope of a series command through Retry', async () => {
    updateCalendarEvent.mockRejectedValueOnce(down())
    await edit({ summary: 'Planning' }, base, 'all')
    updateCalendarEvent.mockResolvedValue({ event: { ...base, rev: 'r2' }, replaces: 'e1' })
    await retryEventWrite('c1:e1')
    expect(updateCalendarEvent).toHaveBeenLastCalledWith('c1', 'e1', { rev: 'r1', summary: 'Planning', scope: 'all' })
  })

  it('a failure after a reload is still failed, and nothing is sent', async () => {
    updateCalendarEvent.mockRejectedValue(down())
    await edit({ summary: 'Planning' })
    resetModuleState() // as a fresh page load has it
    await flushEventEdits(T0 + HOUR)
    expect(updateCalendarEvent).toHaveBeenCalledTimes(1)
    expect(writeStateOf((await db.eventOutbox.get('c1:e1'))!)).toBe('failed')
  })
})

describe('a failed delete', () => {
  it('draws the event again, marked, and Retry deletes it', async () => {
    deleteCalendarEvent.mockRejectedValueOnce(down())
    await edit({ delete: true })
    await flushEventEdits(T0 + DELETE_DELAY_MS)
    expect(await db.eventOutbox.get('c1:e1')).toMatchObject({ failed: { code: 'calendar_unreachable' } })
    expect(await db.calendarEvents.get('e1')).toEqual(base)
    deleteCalendarEvent.mockResolvedValue(undefined)
    await retryEventWrite('c1:e1')
    expect(deleteCalendarEvent).toHaveBeenCalledTimes(2)
    expect(await db.calendarEvents.get('e1')).toBeUndefined()
    expect(await db.eventOutbox.count()).toBe(0)
  })

  it('Discard keeps the event and sends nothing more', async () => {
    deleteCalendarEvent.mockRejectedValue(down())
    await editEvent(base, { delete: true }, T0)
    await flushEventEdits(T0 + DELETE_DELAY_MS)
    await discardEventWrite('c1:e1')
    await flushEventEdits(T0 + HOUR)
    expect(deleteCalendarEvent).toHaveBeenCalledTimes(1)
    expect(await db.calendarEvents.get('e1')).toEqual(base)
    expect(await db.eventOutbox.count()).toBe(0)
  })
})

describe('a failed create', () => {
  it('keeps the pending block, Retry sends it, Discard removes it', async () => {
    createCalendarEvent.mockRejectedValueOnce(down())
    await createEvent(draft, T0)
    await flushEventEdits(SETTLED)
    expect(await db.eventOutbox.get('c1:pending:k1')).toMatchObject({ failed: { code: 'calendar_unreachable' } })
    expect(await db.calendarEvents.get('pending:k1')).toBeDefined()
    await flushEventEdits(T0 + HOUR)
    expect(createCalendarEvent).toHaveBeenCalledTimes(1)
    createCalendarEvent.mockResolvedValue({ event: { ...base, id: 'real', uid: 'k1', summary: 'Dentist' }, clientId: 'k1' })
    await retryEventWrite('c1:pending:k1')
    expect(createCalendarEvent).toHaveBeenCalledTimes(2)
    expect(await db.calendarEvents.get('pending:k1')).toBeUndefined()
    expect(await db.calendarEvents.get('real')).toBeDefined()

    createCalendarEvent.mockRejectedValue(down())
    await createEvent({ ...draft, clientId: 'k2' }, T0)
    await flushEventEdits(SETTLED)
    await discardEventWrite('c1:pending:k2')
    expect(await db.calendarEvents.get('pending:k2')).toBeUndefined()
    expect(await db.eventOutbox.count()).toBe(0)
  })
})

describe('offline', () => {
  it('waits visibly, sends nothing and no timer, and is sent when the device is back', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    online = false
    await edit({ summary: 'Planning' })
    expect(writeStateOf((await db.eventOutbox.get('c1:e1'))!)).toBe('waiting')
    expect(updateCalendarEvent).not.toHaveBeenCalled()
    expect(notices).toEqual([])
    await vi.advanceTimersByTimeAsync(30 * 60_000)
    expect(updateCalendarEvent).not.toHaveBeenCalled()
    online = true
    updateCalendarEvent.mockResolvedValue({ event: { ...base, id: 'e2', rev: 'r2', summary: 'Planning' }, replaces: 'e1' })
    await flushEventEdits(T0 + HOUR)
    expect(updateCalendarEvent).toHaveBeenCalledTimes(1)
    expect(await db.eventOutbox.count()).toBe(0)
  })

  it('goes offline during the request: waiting, not failed', async () => {
    updateCalendarEvent.mockImplementation(async () => { online = false; throw new TypeError('Failed to fetch') })
    await edit({ summary: 'Planning' })
    expect(writeStateOf((await db.eventOutbox.get('c1:e1'))!)).toBe('waiting')
    expect(notices).toEqual([])
  })

  it('a waiting command survives a reload and is sent at start', async () => {
    online = false
    await edit({ summary: 'Planning' })
    resetModuleState()
    online = true
    updateCalendarEvent.mockResolvedValue({ event: { ...base, id: 'e2', rev: 'r2', summary: 'Planning' }, replaces: 'e1' })
    await flushEventEdits(T0 + HOUR)
    expect(updateCalendarEvent).toHaveBeenCalledTimes(1)
  })
})
