// @vitest-environment happy-dom
import 'fake-indexeddb/auto'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { api, ApiError } from './api'
import { db, getMeta } from './db'
import { keepEventCopies, moveSingleEvent, type EventMoveReceipt } from './eventMove'
import type { CalendarEvent, CalendarEventDraft } from './types'

const source: CalendarEvent = { id: 'old', subscriptionId: 'work', uid: 'u1', summary: 'Review', location: 'Room 1', start: { date: '2026-11-13', time: '10:00', tz: 'America/Bogota' }, end: { date: '2026-11-13', time: '11:00', tz: 'America/Bogota' }, allDay: false, free: false, rev: 'r1', access: { update: true, delete: true } }
const draft: CalendarEventDraft = { connectionId: 'c-home', calendarUrl: 'https://dav/home/', subscriptionId: 'home', clientId: 'move-12345678', summary: source.summary, location: source.location ?? '', start: source.start, end: source.end, allDay: false, free: false, description: 'Agenda', reminders: [30] }
const created: CalendarEvent = { ...source, id: 'new', uid: draft.clientId, subscriptionId: 'home', rev: 'r2' }
const receipt = () => getMeta<EventMoveReceipt | null>('calendar.move.old', null)

beforeEach(async () => {
  await db.delete(); await db.open()
  await db.calendarEvents.put(source)
  vi.spyOn(api, 'createCalendarEvent').mockResolvedValue({ event: created, clientId: draft.clientId })
  vi.spyOn(api, 'deleteCalendarEvent').mockResolvedValue(undefined)
})
afterEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals() })

describe('single event calendar moves', () => {
  it('creates in the destination before revision-checked deletion of the source', async () => {
    vi.mocked(api.deleteCalendarEvent).mockImplementation(async () => {
      expect(api.createCalendarEvent).toHaveBeenCalledOnce()
      expect(await db.calendarEvents.get('old')).toBeTruthy()
      expect(await db.calendarEvents.get('new')).toEqual(created)
    })
    expect(await moveSingleEvent(source, 'c-work', draft)).toEqual(created)
    expect(api.createCalendarEvent).toHaveBeenCalledWith('c-home', expect.objectContaining({ clientId: draft.clientId, description: 'Agenda', reminders: [30] }))
    expect(api.createCalendarEvent).toHaveBeenCalledWith('c-home', expect.not.objectContaining({ connectionId: expect.anything(), subscriptionId: expect.anything() }))
    expect(api.deleteCalendarEvent).toHaveBeenCalledWith('c-work', 'old', 'r1')
    expect(await db.calendarEvents.get('old')).toBeUndefined()
    expect(await db.calendarEvents.get('new')).toEqual(created)
    expect(await receipt()).toBeNull()
  })

  it('never deletes the original when the destination refuses creation', async () => {
    vi.mocked(api.createCalendarEvent).mockRejectedValue(new ApiError(403, 'forbidden', 'Read only'))
    await expect(moveSingleEvent(source, 'c-work', draft)).rejects.toThrow('Read only')
    expect(api.deleteCalendarEvent).not.toHaveBeenCalled()
    expect(await db.calendarEvents.get('old')).toEqual(source)
    expect(await receipt()).toBeNull()
  })

  it('retries a lost create response with the same clientId even after re-opening', async () => {
    vi.mocked(api.createCalendarEvent).mockRejectedValueOnce(new Error('lost response'))
    await expect(moveSingleEvent(source, 'c-work', draft)).rejects.toThrow('lost response')
    expect(api.deleteCalendarEvent).not.toHaveBeenCalled()
    expect((await receipt())?.draft.clientId).toBe(draft.clientId)
    await moveSingleEvent(source, 'c-work', { ...draft, clientId: 'different' })
    expect(vi.mocked(api.createCalendarEvent).mock.calls.map(([, body]) => body.clientId)).toEqual([draft.clientId, draft.clientId])
  })

  it('keeps both copies on a source revision conflict, and retries deletion without another create', async () => {
    vi.mocked(api.deleteCalendarEvent).mockRejectedValueOnce(new ApiError(409, 'revision_conflict', 'Changed remotely'))
    await expect(moveSingleEvent(source, 'c-work', draft)).rejects.toThrow('Changed remotely')
    expect(await db.calendarEvents.get('old')).toEqual(source)
    expect((await receipt())?.created).toEqual(created)
    await moveSingleEvent(source, 'c-work', draft)
    expect(api.createCalendarEvent).toHaveBeenCalledOnce()
    expect(api.deleteCalendarEvent).toHaveBeenCalledTimes(2)
  })

  it('finishes safely if the previous deletion response was lost', async () => {
    vi.mocked(api.deleteCalendarEvent).mockRejectedValueOnce(new Error('lost response')).mockRejectedValueOnce(new ApiError(410, 'event_gone', 'Gone'))
    await expect(moveSingleEvent(source, 'c-work', draft)).rejects.toThrow()
    await db.calendarEvents.delete(source.id)
    await moveSingleEvent(source, 'c-work', draft)
    expect(api.createCalendarEvent).toHaveBeenCalledOnce()
    expect(await receipt()).toBeNull()
    expect(await db.calendarEvents.get('new')).toEqual(created)
  })

  it.each([
    { ...source, rev: undefined },
    { ...source, series: { repeat: { frequency: 'daily', interval: 1 } } },
    { ...source, guests: { count: 1 } },
    { ...source, access: { update: true, delete: false } },
  ])('rejects unsupported source %j before network writes', async (event) => {
    await expect(moveSingleEvent(event as CalendarEvent, 'c-work', draft)).rejects.toThrow('unsupported_move')
    expect(api.createCalendarEvent).not.toHaveBeenCalled()
    expect(api.deleteCalendarEvent).not.toHaveBeenCalled()
  })

  it('does not begin a move while offline', async () => {
    vi.stubGlobal('navigator', { onLine: false })
    await expect(moveSingleEvent(source, 'c-work', draft)).rejects.toThrow('offline')
    expect(api.createCalendarEvent).not.toHaveBeenCalled()
  })

  it('lets the person keep both copies after a source conflict', async () => {
    vi.mocked(api.deleteCalendarEvent).mockRejectedValue(new ApiError(409, 'revision_conflict', 'Changed remotely'))
    await expect(moveSingleEvent(source, 'c-work', draft)).rejects.toThrow()
    await keepEventCopies(source.id)
    expect(await receipt()).toBeNull()
    expect(await db.calendarEvents.get('old')).toEqual(source)
    expect(await db.calendarEvents.get('new')).toEqual(created)
    expect(api.deleteCalendarEvent).toHaveBeenCalledOnce()
  })

  it('blocks a move when a source edit is still queued', async () => {
    await db.eventOutbox.put({ key: 'c-work:old', connectionId: 'c-work', eventId: source.id, rev: 'r1', base: source, patch: { summary: 'Pending title' }, notBefore: 0, tries: 0 })
    await expect(moveSingleEvent(source, 'c-work', draft)).rejects.toThrow('pending_source_write')
    expect(api.createCalendarEvent).not.toHaveBeenCalled()
    expect(await receipt()).toBeNull()
  })
})
