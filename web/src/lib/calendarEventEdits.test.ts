import 'fake-indexeddb/auto'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { CalendarEvent } from './types'

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
import { cachedWindow, refreshEvents } from './calendarFeeds'
import { cancelEventDelete, createBodyOf, currentEventId, DELETE_DELAY_MS, EDIT_SETTLE_MS, editEvent, eventAccessText, eventEditNoticeText, flushEventEdits, onEventEditNotice, type EventEditNotice } from './calendarEventEdits'
import { clearLocalData, db, setMeta, useDatabase } from './db'
import { createEvent, getEventCalendar } from './eventCreate'
import type { CalendarEventDraft } from './types'

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
  createCalendarEvent.mockReset()
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
    expect(eventAccessText('series_unsupported')).toBe('This repeat is too complex to change here')
    expect(eventAccessText('has_guests')).toBe('This event has guests')
    expect(eventAccessText('nothing-known')).toBe('')
  })
})

// ---- creating events (calendar-event-editor D3, D9) ----
const draftOf = (clientId: string, over: Partial<CalendarEventDraft> = {}): CalendarEventDraft => ({
  clientId, connectionId: 'c1', calendarUrl: 'https://dav.example.com/work/', subscriptionId: 's1', summary: 'Dentist',
  start: at('09:00'), end: at('09:30'), allDay: false, location: '', description: '', free: false, ...over,
})
const created = (clientId: string, over: Partial<CalendarEvent> = {}): CalendarEvent => ({ ...base, id: `real-${clientId}`, uid: clientId, summary: 'Dentist', rev: 'r1', ...over })
const body = createBodyOf

describe('create', () => {
  it('draws the pending row at once and sends one request when online; the answer replaces the row', async () => {
    createCalendarEvent.mockResolvedValue({ event: created('k1'), clientId: 'k1' })
    const row = await createEvent(draftOf('k1'), T0)
    expect(row).toMatchObject({ id: 'pending:k1', uid: 'k1', summary: 'Dentist', access: { update: true, delete: true } })
    expect(row.rev).toBeUndefined()
    expect(await getEventCalendar()).toBe('s1')
    await flushEventEdits(T0 + EDIT_SETTLE_MS - 1)
    expect(createCalendarEvent).not.toHaveBeenCalled()
    await flushEventEdits(SETTLED)
    expect(createCalendarEvent).toHaveBeenCalledTimes(1)
    expect(createCalendarEvent).toHaveBeenCalledWith('c1', body(draftOf('k1')))
    expect(await db.calendarEvents.get('pending:k1')).toBeUndefined()
    expect((await db.calendarEvents.get('real-k1'))!.rev).toBe('r1')
    expect(await db.eventOutbox.count()).toBe(0)
    expect(currentEventId('pending:k1')).toBe('real-k1')
  })

  it('offline: kept with a backoff, then one request once online', async () => {
    createCalendarEvent.mockRejectedValue(new TypeError('offline'))
    await createEvent(draftOf('k2'), T0)
    await flushEventEdits(SETTLED)
    const kept = (await db.eventOutbox.get('c1:pending:k2'))!
    expect(kept.tries).toBe(1)
    expect(await db.calendarEvents.get('pending:k2')).toBeDefined()
    createCalendarEvent.mockReset()
    createCalendarEvent.mockResolvedValue({ event: created('k2'), clientId: 'k2' })
    await flushEventEdits(kept.notBefore)
    expect(createCalendarEvent).toHaveBeenCalledTimes(1)
  })

  it('merges two edits and a title change before sending into one request carrying the final draft', async () => {
    createCalendarEvent.mockResolvedValue({ event: created('k3'), clientId: 'k3' })
    const row = await createEvent(draftOf('k3'), T0)
    await editEvent(row, moved('10:00', '10:30'), T0)
    await editEvent(row, { summary: 'Dentist 2', location: 'Room 4', description: 'Bring x-rays', free: true }, T0)
    await editEvent(row, moved('11:00', '11:30'), T0)
    expect(await db.eventOutbox.count()).toBe(1)
    expect((await db.calendarEvents.get('pending:k3'))).toMatchObject({ summary: 'Dentist 2', location: 'Room 4', free: true, start: at('11:00') })
    await flushEventEdits(SETTLED)
    expect(createCalendarEvent).toHaveBeenCalledTimes(1)
    expect(createCalendarEvent).toHaveBeenCalledWith('c1', body(draftOf('k3', { summary: 'Dentist 2', location: 'Room 4', description: 'Bring x-rays', free: true, start: at('11:00'), end: at('11:30') })))
    expect(updateCalendarEvent).not.toHaveBeenCalled()
  })

  it('a delete of an unsent pending event sends nothing; Undo of an edit works on the draft', async () => {
    const row = await createEvent(draftOf('k4'), T0)
    const inverse = await editEvent(row, { summary: 'Other' }, T0)
    expect(inverse).toEqual({ summary: 'Dentist' })
    await editEvent(row, inverse as never, T0)
    expect((await db.eventOutbox.get('c1:pending:k4'))!.patch).toEqual({ create: draftOf('k4') })
    expect(await editEvent(row, { delete: true }, T0)).toEqual({ delete: true })
    expect(await db.eventOutbox.count()).toBe(0)
    expect(await db.calendarEvents.count()).toBe(1) // only the seeded event
    await flushEventEdits(T0 + 60_000)
    expect(createCalendarEvent).not.toHaveBeenCalled()
    expect(deleteCalendarEvent).not.toHaveBeenCalled()
  })

  it('an edit made while the create is in flight is a PATCH against the new id and rev', async () => {
    let finish!: (v: unknown) => void
    createCalendarEvent.mockImplementationOnce(() => new Promise((resolve) => { finish = resolve }))
    updateCalendarEvent.mockResolvedValue({ event: created('k5', { id: 'real-k5b', rev: 'r2', summary: 'Renamed' }), replaces: 'real-k5' })
    const row = await createEvent(draftOf('k5'), T0)
    void flushEventEdits(SETTLED)
    await vi.waitFor(async () => expect((await db.eventOutbox.get('c1:pending:k5'))?.sending).toBe(true))
    await editEvent(row, { summary: 'Renamed' }, T0)
    expect((await db.calendarEvents.get('pending:k5'))!.summary).toBe('Renamed')
    finish({ event: created('k5'), clientId: 'k5' })
    await vi.waitFor(() => expect(updateCalendarEvent).toHaveBeenCalledWith('c1', 'real-k5', { rev: 'r1', summary: 'Renamed' }))
    await vi.waitFor(async () => expect(await db.eventOutbox.count()).toBe(0))
    expect(createCalendarEvent).toHaveBeenCalledTimes(1)
    expect(currentEventId('pending:k5')).toBe('real-k5b')
  })

  it('a description-only change made during the send is not lost', async () => {
    let finish!: (v: unknown) => void
    createCalendarEvent.mockImplementationOnce(() => new Promise((resolve) => { finish = resolve }))
    updateCalendarEvent.mockResolvedValue({ event: created('k6'), replaces: 'real-k6' })
    const row = await createEvent(draftOf('k6'), T0)
    void flushEventEdits(SETTLED)
    await vi.waitFor(async () => expect((await db.eventOutbox.get('c1:pending:k6'))?.sending).toBe(true))
    await editEvent(row, { description: 'Notes' }, T0)
    finish({ event: created('k6'), clientId: 'k6' })
    await vi.waitFor(() => expect(updateCalendarEvent).toHaveBeenCalledWith('c1', 'real-k6', { rev: 'r1', description: 'Notes' }))
  })

  it.each([[403, 'event_edits_off'], [404, 'event_gone'], [400, 'invalid_input'], [409, 'event_changed'], [422, 'event_not_editable']])('a %i %s refusal removes the pending row and hands back the draft', async (status, code) => {
    const notices: EventEditNotice[] = []
    onEventEditNotice((x) => notices.push(x))
    calendarEvents.mockRejectedValue(new TypeError('offline'))
    createCalendarEvent.mockRejectedValue(refused(status, code))
    const row = await createEvent(draftOf('k8'), T0)
    await editEvent(row, { location: 'Room 4' }, T0)
    await flushEventEdits(SETTLED)
    await vi.waitFor(async () => expect(await db.eventOutbox.count()).toBe(0))
    expect(await db.calendarEvents.get('pending:k8')).toBeUndefined()
    expect(notices).toEqual([{ code, title: 'Dentist', message: `server says ${code}`, draft: draftOf('k8', { location: 'Room 4' }) }])
  })

  it('two creates are sent in the order they were made', async () => {
    const order: string[] = []
    createCalendarEvent.mockImplementation(async (_c: string, b: { clientId: string }) => { order.push(b.clientId); return { event: created(b.clientId), clientId: b.clientId } })
    await createEvent(draftOf('zz-first'), T0)
    await createEvent(draftOf('aa-second'), T0)
    await flushEventEdits(T0 + 60_000)
    expect(order).toEqual(['zz-first', 'aa-second'])
  })

  it('a refresh keeps the pending event and a queued description edit', async () => {
    const sub = { id: 's1', name: 'Work', color: '', enabled: true, lastSuccessAt: 2, managed: true, connectionId: 'c1' }
    createCalendarEvent.mockRejectedValue(new TypeError('offline'))
    updateCalendarEvent.mockRejectedValue(new TypeError('offline'))
    calendarEvents.mockResolvedValue({ events: [base], subscriptions: [sub] })
    const row = await createEvent(draftOf('k9'), T0)
    await editEvent(row, { location: 'Room 4' }, T0)
    await editEvent(base, { description: 'Notes' }, T0)
    await refreshEvents({ from: '2026-11-10', to: '2026-11-16' })
    expect((await db.calendarEvents.toArray()).map((e) => e.id).sort()).toEqual(['e1', 'pending:k9'])
    expect((await db.calendarEvents.get('pending:k9'))!.location).toBe('Room 4')
    expect((await db.eventOutbox.get('c1:e1'))!.patch).toEqual({ description: 'Notes' })
  })

  it('Undo after the swap is the normal delete of the real event', async () => {
    createCalendarEvent.mockResolvedValue({ event: created('k10'), clientId: 'k10' })
    deleteCalendarEvent.mockResolvedValue(undefined)
    const row = await createEvent(draftOf('k10'), T0)
    await flushEventEdits(SETTLED)
    const real = (await db.calendarEvents.get(currentEventId(row.id)))!
    expect(await editEvent(real, { delete: true }, T0)).toEqual({ delete: true })
    await flushEventEdits(T0 + DELETE_DELAY_MS)
    expect(deleteCalendarEvent).toHaveBeenCalledWith('c1', 'real-k10', 'r1')
  })

  it('location, free and a description of an existing event are sent; the description is never on the row', async () => {
    updateCalendarEvent.mockRejectedValue(new TypeError('offline'))
    expect(await edit(base, { description: 'Notes', location: 'Room 4', free: true }, T0, { description: 'Old' })).toEqual({ location: '', free: false, description: 'Old' })
    expect(await db.eventOutbox.get('c1:e1')).toMatchObject({ patch: { description: 'Notes', location: 'Room 4', free: true } })
    expect(await db.calendarEvents.get('e1')).toMatchObject({ location: 'Room 4', free: true })
    // A command whose only change is a description does not return to base.
    updateCalendarEvent.mockRejectedValue(new TypeError('offline'))
    await edit({ ...base, id: 'e7', uid: 'u7' }, { description: 'x' }, T0)
    expect((await db.eventOutbox.get('c1:e7'))!.patch).toEqual({ description: 'x' })
  })

  it('sign-out deletes the outbox and the meta rows', async () => {
    createCalendarEvent.mockRejectedValue(new TypeError('offline'))
    await createEvent(draftOf('k11'), T0)
    await clearLocalData()
    expect(await db.eventOutbox.count()).toBe(0)
    expect(await getEventCalendar()).toBeNull()
  })
})
