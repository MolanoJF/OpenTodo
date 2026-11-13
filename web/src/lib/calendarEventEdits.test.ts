import 'fake-indexeddb/auto'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { CalendarEvent } from './types'

const updateCalendarEvent = vi.fn()
const deleteCalendarEvent = vi.fn()
const createCalendarEvent = vi.fn()
const respondToCalendarEvent = vi.fn()
const calendarEvents = vi.fn()
vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  api: {
    updateCalendarEvent: (...a: unknown[]) => updateCalendarEvent(...a),
    deleteCalendarEvent: (...a: unknown[]) => deleteCalendarEvent(...a),
    createCalendarEvent: (...a: unknown[]) => createCalendarEvent(...a),
    respondToCalendarEvent: (...a: unknown[]) => respondToCalendarEvent(...a),
    calendarEvents: (...a: unknown[]) => calendarEvents(...a),
    calendarFeedsSwitch: { get: async () => ({ enabled: false }) },
    calendarSyncSwitch: { get: async () => ({ enabled: true }) },
  },
}))

import { ApiError } from './api'
import { cachedWindow, refreshEvents } from './calendarFeeds'
import { cancelEventDelete, createBodyOf, currentEventId, DELETE_DELAY_MS, EDIT_SETTLE_MS, editEvent, eventAccessText, eventEditNoticeText, flushEventEdits, onEventEditNotice, type EventEditNotice } from './calendarEventEdits'
import { clearLocalData, db, setMeta, useDatabase } from './db'
import { platform } from './platform'

let online = true
const setOnline = (v: boolean) => { online = v }
vi.spyOn(platform.network, 'online').mockImplementation(() => online)
import { createEvent, getEventCalendar } from './eventCreate'
import { sendNow, startNotifiedDelete } from './eventSendNow'
import type { CalendarEventDraft } from './types'

const at = (time: string) => ({ date: '2026-11-12', time, tz: 'Europe/Berlin' })
const base: CalendarEvent = { id: 'e1', subscriptionId: 's1', uid: 'u1', summary: 'Standup', location: '', start: at('09:00'), end: at('09:30'), allDay: false, free: false, rev: 'r1', access: { update: true, delete: true } }
const moved = (from: string, to: string) => ({ start: at(from), end: at(to), allDay: false })
const T0 = Date.now()
const SETTLED = T0 + EDIT_SETTLE_MS

let n = 0
beforeEach(async () => {
  useDatabase(`event-edits-${Date.now()}-${n++}`)
  updateCalendarEvent.mockReset(); setOnline(true)
  deleteCalendarEvent.mockReset()
  createCalendarEvent.mockReset(); setOnline(true)
  calendarEvents.mockReset()
  await db.calendarSubscriptions.put({ id: 's1', name: 'Work', color: '', enabled: true, lastSuccessAt: 1, managed: true, connectionId: 'c1' })
  await db.calendarEvents.put(base)
})

/** An edit, then the send it starts: the tests step through the outbox one move at a time. */
const edit = async (...a: Parameters<typeof editEvent>) => { const r = await editEvent(...a); await flushEventEdits(SETTLED); return r }
const refused = (status: number, code: string) => new ApiError(status, code, `server says ${code}`)

describe('editEvent: merge, optimistic patch, inverse', () => {
  it('patches the cached row at once and queues one command', async () => {
    updateCalendarEvent.mockRejectedValue(new TypeError('offline')); setOnline(false)
    const inverse = await edit(base, moved('10:00', '10:30'), T0)
    expect(inverse).toEqual(moved('09:00', '09:30'))
    expect((await db.calendarEvents.get('e1'))!.start.time).toBe('10:00')
    const cmd = (await db.eventOutbox.get('c1:e1'))!
    expect(cmd).toMatchObject({ rev: 'r1', patch: moved('10:00', '10:30') })
    expect(cmd.base).toEqual(base)
  })

  it('coalesces two moves and a rename offline into one command, sent once when the device is back', async () => {
    updateCalendarEvent.mockRejectedValue(new TypeError('offline')); setOnline(false)
    await edit(base, moved('10:00', '10:30'), T0)
    await edit(base, moved('11:00', '11:30'), T0)
    await edit(base, { summary: 'Planning' }, T0)
    expect(await db.eventOutbox.count()).toBe(1)
    expect((await db.eventOutbox.get('c1:e1'))!.patch).toEqual({ ...moved('11:00', '11:30'), summary: 'Planning' })
    // Offline: kept, waiting for the device (no timer).
    expect(await db.eventOutbox.get('c1:e1')).toMatchObject({ offline: true })
    // Online again: one request.
    updateCalendarEvent.mockReset(); setOnline(true)
    updateCalendarEvent.mockResolvedValue({ event: { ...base, id: 'e2', rev: 'r2', summary: 'Planning', start: at('11:00'), end: at('11:30') }, replaces: 'e1' })
    await flushEventEdits(T0 + 10 * 60_000)
    expect(updateCalendarEvent).toHaveBeenCalledTimes(1)
    expect(updateCalendarEvent).toHaveBeenCalledWith('c1', 'e1', { rev: 'r1', ...moved('11:00', '11:30'), summary: 'Planning' })
    expect(await db.eventOutbox.count()).toBe(0)
  })

  it('removes the command when the event is moved back before it is sent', async () => {
    updateCalendarEvent.mockRejectedValue(new TypeError('offline')); setOnline(false)
    await edit(base, moved('10:00', '10:30'), T0)
    await edit(base, moved('09:00', '09:30'), T0)
    expect(await db.eventOutbox.count()).toBe(0)
    expect((await db.calendarEvents.get('e1'))!.start.time).toBe('09:00')
    updateCalendarEvent.mockReset(); setOnline(true)
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
    updateCalendarEvent.mockRejectedValue(new TypeError('offline')); setOnline(false)
    const inverse = await edit(base, moved('10:00', '10:30'), T0)
    await edit(base, inverse as never, T0)
    expect(await db.eventOutbox.count()).toBe(0)
    expect(await db.calendarEvents.get('e1')).toEqual(base)
  })
})

describe('delete', () => {
  it('waits for Undo, then sends; a delete replaces an edit and wins', async () => {
    updateCalendarEvent.mockRejectedValue(new TypeError('offline')); setOnline(false)
    await edit(base, moved('10:00', '10:30'), T0)
    deleteCalendarEvent.mockResolvedValue(undefined)
    await edit(base, { delete: true }, T0)
    expect(await db.calendarEvents.get('e1')).toBeUndefined()
    await flushEventEdits(T0 + DELETE_DELAY_MS - 1)
    expect(deleteCalendarEvent).not.toHaveBeenCalled()
    setOnline(true)
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
    updateCalendarEvent.mockRejectedValue(new TypeError('offline')); setOnline(false)
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

})

describe('refreshEvents keeps queued edits (4.5)', () => {
  const sub = { id: 's1', name: 'Work', color: '', enabled: true, lastSuccessAt: 2, managed: true, connectionId: 'c1' }
  it('draws a queued edit over the fetched row and hides a queued delete', async () => {
    updateCalendarEvent.mockRejectedValue(new TypeError('offline')); setOnline(false)
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

  it('offline: kept as waiting, then one request once online', async () => {
    createCalendarEvent.mockRejectedValue(new TypeError('offline')); setOnline(false)
    await createEvent(draftOf('k2'), T0)
    await flushEventEdits(SETTLED)
    expect(await db.eventOutbox.get('c1:pending:k2')).toMatchObject({ offline: true })
    expect(await db.calendarEvents.get('pending:k2')).toBeDefined()
    createCalendarEvent.mockReset(); setOnline(true)
    createCalendarEvent.mockResolvedValue({ event: created('k2'), clientId: 'k2' })
    await flushEventEdits(T0 + 60_000)
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
    createCalendarEvent.mockRejectedValue(new TypeError('offline')); setOnline(false)
    updateCalendarEvent.mockRejectedValue(new TypeError('offline')); setOnline(false)
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
    updateCalendarEvent.mockRejectedValue(new TypeError('offline')); setOnline(false)
    expect(await edit(base, { description: 'Notes', location: 'Room 4', free: true }, T0, { description: 'Old' })).toEqual({ location: '', free: false, description: 'Old' })
    expect(await db.eventOutbox.get('c1:e1')).toMatchObject({ patch: { description: 'Notes', location: 'Room 4', free: true } })
    expect(await db.calendarEvents.get('e1')).toMatchObject({ location: 'Room 4', free: true })
    // A command whose only change is a description does not return to base.
    updateCalendarEvent.mockRejectedValue(new TypeError('offline')); setOnline(false)
    await edit({ ...base, id: 'e7', uid: 'u7' }, { description: 'x' }, T0)
    expect((await db.eventOutbox.get('c1:e7'))!.patch).toEqual({ description: 'x' })
  })

  it('sign-out deletes the outbox and the meta rows', async () => {
    createCalendarEvent.mockRejectedValue(new TypeError('offline')); setOnline(false)
    await createEvent(draftOf('k11'), T0)
    await clearLocalData()
    expect(await db.eventOutbox.count()).toBe(0)
    expect(await getEventCalendar()).toBeNull()
  })
})

// ---- guests: what tells people is sent now and never queued (calendar-event-guests D8, D11) ----
describe('sendNow, a notified change, a reply and a create with guests', () => {
  const guestRow: CalendarEvent = { ...base, rev: 'r1', access: { update: true, delete: true, notifies: true }, guests: { count: 3, organizer: true, canRespond: false } }
  const invite: CalendarEvent = { ...base, id: 'inv', rev: 'r7', access: { update: false, delete: false, reason: 'not_organizer' }, guests: { count: 4, you: 'needs-action', organizer: false, canRespond: true } }
  const notices: EventEditNotice[] = []
  const online = (on: boolean) => vi.stubGlobal('navigator', { onLine: on })

  beforeEach(async () => {
    notices.length = 0
    respondToCalendarEvent.mockReset()
    onEventEditNotice((n) => notices.push(n))
    await db.calendarEvents.bulkPut([guestRow, invite])
    online(true)
  })
  afterEach(() => { vi.unstubAllGlobals(); vi.useRealTimers() })

  it('sends a notified change with notifyGuests, draws it, swaps in the answer and leaves the outbox empty', async () => {
    updateCalendarEvent.mockResolvedValue({ event: { ...guestRow, id: 'e1b', rev: 'r2', summary: 'Planning' }, replaces: 'e1' })
    expect(await sendNow(guestRow, { kind: 'change', change: { summary: 'Planning' }, confirmed: true })).toBe(true)
    expect(updateCalendarEvent).toHaveBeenCalledWith('c1', 'e1', { rev: 'r1', summary: 'Planning', notifyGuests: true })
    expect(await db.eventOutbox.count()).toBe(0)
    expect(await db.calendarEvents.get('e1')).toBeUndefined()
    expect((await db.calendarEvents.get('e1b'))!.summary).toBe('Planning')
    expect(notices).toEqual([])
  })

  it('carries no notifyGuests unless the person confirmed (a message is never sent on a guess)', async () => {
    updateCalendarEvent.mockResolvedValue({ event: null, replaces: 'e1' })
    await sendNow(guestRow, { kind: 'change', change: { onlineMeeting: true } })
    expect(updateCalendarEvent).toHaveBeenCalledWith('c1', 'e1', { rev: 'r1', onlineMeeting: true })
  })

  it('carries the scope and the guest changes, and draws the change while it is on its way', async () => {
    let seen = ''
    updateCalendarEvent.mockImplementation(async () => { seen = (await db.calendarEvents.get('e1'))!.summary; return { event: null, replaces: 'e1' } })
    await sendNow({ ...guestRow, series: { id: 'sr' } }, { kind: 'change', change: { summary: 'Planning', guests: { add: [{ email: 'a@b.co' }], remove: ['c@d.co'] } }, scope: 'this', confirmed: true })
    expect(seen).toBe('Planning')
    expect(updateCalendarEvent).toHaveBeenCalledWith('c1', 'e1', { rev: 'r1', summary: 'Planning', guests: { add: [{ email: 'a@b.co' }], remove: ['c@d.co'] }, scope: 'this', notifyGuests: true })
  })

  it('sends nothing offline, says why and keeps the row as it was', async () => {
    online(false)
    expect(await sendNow(guestRow, { kind: 'change', change: { summary: 'Planning' } })).toBe(false)
    expect(updateCalendarEvent).not.toHaveBeenCalled()
    expect(notices).toMatchObject([{ code: 'offline_guests' }])
    expect(eventEditNoticeText(notices[0])).toBe("Guests are told when you're online")
    expect((await db.calendarEvents.get('e1'))!.summary).toBe('Standup')
    expect(await db.eventOutbox.count()).toBe(0)
  })

  it('restores the row and tells the reason when the calendar refuses, and never retries', async () => {
    updateCalendarEvent.mockRejectedValue(refused(409, 'event_changed'))
    expect(await sendNow(guestRow, { kind: 'change', change: { summary: 'Planning' } })).toBe(false)
    expect((await db.calendarEvents.get('e1'))!.summary).toBe('Standup')
    expect(notices).toMatchObject([{ code: 'event_changed', title: 'Standup' }])
    await flushEventEdits(T0 + 10 * 60_000)
    expect(updateCalendarEvent).toHaveBeenCalledTimes(1)
    expect(await db.eventOutbox.count()).toBe(0)
  })

  it('does not queue a change that could not reach the server: it is restored and never sent later', async () => {
    updateCalendarEvent.mockRejectedValue(new TypeError('offline')); setOnline(false)
    expect(await sendNow(guestRow, { kind: 'change', change: { summary: 'Planning' } })).toBe(false)
    expect(await db.eventOutbox.count()).toBe(0)
    expect((await db.calendarEvents.get('e1'))!.summary).toBe('Standup')
    expect(notices[0].code).toBe('offline_guests')
    await flushEventEdits(T0 + 10 * 60_000)
    expect(updateCalendarEvent).toHaveBeenCalledTimes(1)
  })

  it('refuses to queue a change of an event that tells its guests', async () => {
    expect(await editEvent(guestRow, { summary: 'Planning' }, T0)).toBeNull()
    expect(await editEvent(guestRow, { delete: true }, T0)).toBeNull()
    expect(await db.eventOutbox.count()).toBe(0)
    expect(notices[0].code).toBe('notify_required')
  })

  it('still queues a change of an event that has guests but tells nobody', async () => {
    const quiet = { ...guestRow, access: { update: true, delete: true } }
    await db.calendarEvents.put(quiet)
    updateCalendarEvent.mockRejectedValue(new TypeError('offline')); setOnline(false)
    expect(await edit(quiet, { summary: 'Planning' }, T0)).toEqual({ summary: 'Standup' })
    expect(await db.eventOutbox.count()).toBe(1)
  })

  it('answers an invitation: writes guests.you and the new rev from the calendar and sends one request', async () => {
    respondToCalendarEvent.mockResolvedValue({ event: { ...invite, rev: 'r8', guests: { ...invite.guests!, you: 'accepted' } }, replaces: 'inv' })
    expect(await sendNow(invite, { kind: 'reply', response: 'accepted', sendResponse: false })).toBe(true)
    expect(respondToCalendarEvent).toHaveBeenCalledWith('c1', 'inv', { rev: 'r7', response: 'accepted', sendResponse: false })
    const row = (await db.calendarEvents.get('inv'))!
    expect(row.rev).toBe('r8')
    expect(row.guests?.you).toBe('accepted')
    expect(await db.eventOutbox.count()).toBe(0)
  })

  it('draws the answer at once, applies the series version and reads the range again when the server says so', async () => {
    const occ = { ...invite, id: 'o1', series: { id: 'sr', rev: 'r7' } }
    const other = { ...invite, id: 'o2', series: { id: 'sr', rev: 'r7' } }
    await db.calendarEvents.bulkPut([occ, other])
    let drawn = ''
    respondToCalendarEvent.mockImplementation(async () => { drawn = (await db.calendarEvents.get('o1'))!.guests!.you!; return { event: null, replaces: 'o1', series: { id: 'sr', rev: 'r9' }, refetch: true } })
    calendarEvents.mockResolvedValue({ events: [], subscriptions: [] })
    await setMeta('calendar.events.window', { from: '2026-11-01', to: '2026-11-30', fetchedAt: Date.now() })
    await sendNow(occ, { kind: 'reply', response: 'tentative', scope: 'all' })
    expect(drawn).toBe('tentative')
    expect(respondToCalendarEvent).toHaveBeenCalledWith('c1', 'o1', { rev: 'r7', response: 'tentative', scope: 'all' })
    expect((await db.calendarEvents.get('o2'))!.series?.rev).toBe('r9')
    await vi.waitFor(() => expect(calendarEvents).toHaveBeenCalled())
  })

  it("shows the calendar's real answer and says so when it did not keep the reply", async () => {
    respondToCalendarEvent.mockRejectedValue(new ApiError(502, 'calendar_unreachable', 'calendar_unreachable: response_not_kept'))
    calendarEvents.mockResolvedValue({ events: [invite], subscriptions: [] })
    await setMeta('calendar.events.window', { from: '2026-11-01', to: '2026-11-30', fetchedAt: Date.now() })
    expect(await sendNow(invite, { kind: 'reply', response: 'accepted' })).toBe(false)
    expect((await db.calendarEvents.get('inv'))!.guests?.you).toBe('needs-action')
    expect(calendarEvents).toHaveBeenCalled()
    expect(notices).toMatchObject([{ code: 'response_not_kept' }])
    expect(eventEditNoticeText(notices[0])).toBe("The calendar didn't keep your answer")
    expect(eventEditNoticeText({ ...notices[0], provider: 'google' })).toBe("Google didn't keep your answer")
  })

  it('never queues a reply offline', async () => {
    online(false)
    expect(await sendNow(invite, { kind: 'reply', response: 'declined' })).toBe(false)
    expect(respondToCalendarEvent).not.toHaveBeenCalled()
    expect(await db.eventOutbox.count()).toBe(0)
  })

  it('creates an event with guests at once with notifyGuests, never in the outbox', async () => {
    const draft = draftOf('g1', { guests: [{ email: 'ben@example.com', role: 'optional' }], onlineMeeting: true })
    createCalendarEvent.mockResolvedValue({ event: created('g1'), clientId: 'g1' })
    expect(await sendNow(null, { kind: 'create', draft, confirmed: true })).toBe(true)
    expect(createCalendarEvent).toHaveBeenCalledWith('c1', { ...createBodyOf(draft), notifyGuests: true })
    expect(createBodyOf(draft)).toMatchObject({ guests: [{ email: 'ben@example.com', role: 'optional' }], onlineMeeting: true })
    expect(await db.eventOutbox.count()).toBe(0)
    expect(await db.calendarEvents.get('real-g1')).toBeTruthy()
    expect(await db.calendarEvents.get('pending:g1')).toBeUndefined()
  })

  it('a refused create leaves nothing and offers the draft back', async () => {
    const draft = draftOf('g2', { guests: [{ email: 'ben@example.com' }] })
    createCalendarEvent.mockRejectedValue(refused(400, 'invalid_input'))
    expect(await sendNow(null, { kind: 'create', draft })).toBe(false)
    expect(await db.calendarEvents.get('pending:g2')).toBeUndefined()
    expect(notices[0]).toMatchObject({ code: 'invalid_input', draft })
  })

  it('does not queue a create with guests: createEvent refuses it', async () => {
    await expect(createEvent(draftOf('g3', { guests: [{ email: 'ben@example.com' }] }))).rejects.toThrow()
    expect(await db.eventOutbox.count()).toBe(0)
  })

  describe('a notified delete', () => {
    it('hides the event, sends nothing before the delay and sends once with notifyGuests after it', async () => {
      vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
      deleteCalendarEvent.mockResolvedValue(undefined)
      await startNotifiedDelete(guestRow, 'all')
      expect(await db.calendarEvents.get('e1')).toBeUndefined()
      await vi.advanceTimersByTimeAsync(DELETE_DELAY_MS - 1)
      expect(deleteCalendarEvent).not.toHaveBeenCalled()
      await vi.advanceTimersByTimeAsync(2)
      expect(deleteCalendarEvent).toHaveBeenCalledWith('c1', 'e1', 'r1', 'all', true)
      expect(await db.eventOutbox.count()).toBe(0)
    })

    it('Undo inside the window brings the event back and sends nothing', async () => {
      vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
      const undo = (await startNotifiedDelete(guestRow))!
      expect(await undo()).toBe(true)
      expect((await db.calendarEvents.get('e1'))!.summary).toBe('Standup')
      await vi.advanceTimersByTimeAsync(DELETE_DELAY_MS * 2)
      expect(deleteCalendarEvent).not.toHaveBeenCalled()
      expect(await undo()).toBe(false)
    })

    it('restores the event and says why when the device is offline at send time', async () => {
      vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
      await startNotifiedDelete(guestRow)
      online(false)
      await vi.advanceTimersByTimeAsync(DELETE_DELAY_MS + 1)
      expect(deleteCalendarEvent).not.toHaveBeenCalled()
      expect(await db.calendarEvents.get('e1')).toBeTruthy()
      expect(notices[0].code).toBe('offline_guests')
    })

    it('restores the event and tells the reason when the calendar refuses', async () => {
      vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
      deleteCalendarEvent.mockRejectedValue(refused(409, 'event_changed'))
      await startNotifiedDelete(guestRow)
      await vi.advanceTimersByTimeAsync(DELETE_DELAY_MS + 1)
      expect(await db.calendarEvents.get('e1')).toBeTruthy()
      expect(notices[0].code).toBe('event_changed')
    })
  })

  it('says the locked reasons for a guest and for Google', () => {
    expect(eventAccessText('not_organizer')).toBe("You're a guest. Only the organizer can change this event")
    expect(eventAccessText('has_guests')).toBe('This event has guests')
    expect(eventAccessText('has_guests', 'google')).toBe("Google doesn't say whether changing this would tell the guests, so it's read-only here")
    expect(eventAccessText('cannot_respond')).toBe("You can't answer this invitation from here")
    expect(eventAccessText('response_not_kept', 'google')).toBe("Google didn't keep your answer")
  })
})
