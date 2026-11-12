import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { api } from './api'

let calls: { method: string; url: string; body: unknown }[]

beforeEach(() => {
  calls = []
  vi.stubGlobal('fetch', vi.fn(async (url: string, init: RequestInit) => {
    calls.push({ method: init.method ?? 'GET', url, body: init.body ? JSON.parse(String(init.body)) : undefined })
    if (init.method === 'DELETE') return new Response(null, { status: 204 })
    return new Response('{"event":{"id":"b"},"replaces":"a"}', { status: 200, headers: { 'Content-Type': 'application/json' } })
  }))
})
afterEach(() => vi.unstubAllGlobals())

describe('calendar event edit API client', () => {
  it('PATCHes the event of a connection with the rev and the change', async () => {
    const start = { date: '2026-11-12', time: '14:00', tz: 'Europe/Berlin' }
    const end = { date: '2026-11-12', time: '14:30', tz: 'Europe/Berlin' }
    const res = await api.updateCalendarEvent('c 1', 'ev1', { rev: 'r1', summary: 'Standup', start, end, allDay: false })
    expect(res).toEqual({ event: { id: 'b' }, replaces: 'a' })
    expect(calls).toEqual([{ method: 'PATCH', url: '/api/v1/calendar-sync/connections/c%201/events/ev1', body: { rev: 'r1', summary: 'Standup', start, end, allDay: false } }])
  })

  it('DELETEs the event with the rev in the query', async () => {
    await api.deleteCalendarEvent('c1', 'ev1', 'r1')
    expect(calls).toEqual([{ method: 'DELETE', url: '/api/v1/calendar-sync/connections/c1/events/ev1?rev=r1', body: undefined }])
  })

  it('POSTs a new event with its client id and calendar', async () => {
    const start = { date: '2026-11-13', time: '14:00', tz: 'Europe/Lisbon' }
    const end = { date: '2026-11-13', time: '14:30', tz: 'Europe/Lisbon' }
    const body = { clientId: 'abc12345', calendarUrl: 'https://dav.example.com/cal/', summary: 'Call', start, end, allDay: false, location: '', description: 'x', free: false }
    await api.createCalendarEvent('c 1', body)
    expect(calls).toEqual([{ method: 'POST', url: '/api/v1/calendar-sync/connections/c%201/events', body }])
  })

  it('GETs the details of one event', async () => {
    await api.calendarEventDetail('c1', 'ev 1')
    expect(calls).toEqual([{ method: 'GET', url: '/api/v1/calendar-sync/connections/c1/events/ev%201', body: undefined }])
  })

  it('PATCHes location, description and free with the rest', async () => {
    await api.updateCalendarEvent('c1', 'ev1', { rev: 'r1', location: '', description: 'd', free: true })
    expect(calls[0].body).toEqual({ rev: 'r1', location: '', description: 'd', free: true })
  })

  // calendar-event-series D3: scope, repeat and reminders ride on the same routes.
  it('PATCHes with a scope, a repeat and reminders', async () => {
    const repeat = { freq: 'weekly' as const, interval: 1, days: ['mo', 'we'], ends: { type: 'after' as const, count: 10 } }
    await api.updateCalendarEvent('c1', 'ev1', { rev: 'r1', scope: 'following', repeat, reminders: [10, 1440] })
    expect(calls[0].body).toEqual({ rev: 'r1', scope: 'following', repeat, reminders: [10, 1440] })
  })

  it('sends repeat: null (stop repeating) as null, not as an omitted field', async () => {
    await api.updateCalendarEvent('c1', 'ev1', { rev: 'r1', scope: 'following', repeat: null })
    expect(calls[0].body).toEqual({ rev: 'r1', scope: 'following', repeat: null })
  })

  it('answers with the series and refetch of an occurrence write', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response('{"event":null,"replaces":"a","series":{"id":"s1","rev":"r9"},"refetch":true}', { status: 200, headers: { 'Content-Type': 'application/json' } })))
    expect(await api.updateCalendarEvent('c1', 'ev1', { rev: 'r1', scope: 'all', summary: 'x' })).toEqual({ event: null, replaces: 'a', series: { id: 's1', rev: 'r9' }, refetch: true })
  })

  it('DELETEs with the scope in the query', async () => {
    await api.deleteCalendarEvent('c1', 'ev1', 'r1', 'this')
    expect(calls).toEqual([{ method: 'DELETE', url: '/api/v1/calendar-sync/connections/c1/events/ev1?rev=r1&scope=this', body: undefined }])
  })

  it('POSTs a new repeating event with its reminders and no scope', async () => {
    const start = { date: '2026-11-13', time: '14:00', tz: 'Europe/Lisbon' }
    const end = { date: '2026-11-13', time: '14:30', tz: 'Europe/Lisbon' }
    const body = { clientId: 'abc12345', calendarUrl: 'https://dav.example.com/cal/', summary: 'Call', start, end, allDay: false, location: '', description: '', free: false, repeat: { freq: 'daily' as const, interval: 1, ends: { type: 'never' as const } }, reminders: [10] }
    await api.createCalendarEvent('c1', body)
    expect(calls[0].body).toEqual(body)
  })
})
