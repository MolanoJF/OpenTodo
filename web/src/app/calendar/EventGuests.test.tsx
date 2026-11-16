// @vitest-environment happy-dom
// The guests of an event in its popover (OpenSpec change calendar-event-guests, task 5.1, design D11, D12, D13): the line comes from the
// cached summary, the list is requested when it is expanded and not when it renders, it is dropped when the popover
// closes, and the device's database holds no name or address at any point.
import 'fake-indexeddb/auto'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { db, useDatabase } from '../../lib/db'
import { peekGuestList } from '../../lib/eventGuests'
import type { CalendarEvent, EventGuestList } from '../../lib/types'
import { click, render, waitFor } from '../../ui/test-utils'
import { EventGuests } from './EventGuests'

const ev: CalendarEvent = {
  id: 'ev1', subscriptionId: 's1', uid: 'u', summary: 'Design review', location: '', allDay: false, free: false, rev: 'r1',
  start: { date: '2026-11-13', time: '10:00' }, end: { date: '2026-11-13', time: '11:00' }, access: { update: false, delete: false, reason: 'not_organizer' },
  guests: { count: 2, you: 'needs-action', organizer: false, canRespond: false },
}
const list: EventGuestList = {
  organizer: { name: 'Ana Silva', email: 'ana@example.com', you: false },
  guests: [
    { name: 'Ben Costa', email: 'ben@example.com', role: 'optional', response: 'accepted', you: false },
    { name: '', email: 'room-4@example.com', role: 'room', response: 'accepted', you: false },
    { name: 'Me', email: 'me@example.com', role: 'required', response: 'needs-action', you: true },
  ],
  count: 3, truncated: false,
}

let requests: { url: string; init?: RequestInit }[]
let answer: () => Response
const json = (b: unknown, status = 200) => new Response(JSON.stringify(b), { status, headers: { 'Content-Type': 'application/json' } })
let n = 0
beforeEach(() => {
  useDatabase(`event-guests-${Date.now()}-${n++}`)
  requests = []
  answer = () => json(list)
  vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => { requests.push({ url, init }); return answer() }))
})
afterEach(() => {
  vi.unstubAllGlobals()
  Object.defineProperty(navigator, 'onLine', { configurable: true, value: true })
})

const toggle = (c: HTMLElement) => c.querySelector<HTMLButtonElement>('[data-event-guests-toggle]')!
const dump = async () => JSON.stringify(await Promise.all(db.tables.map((t) => t.toArray())))

describe('EventGuests', () => {
  it('shows the line from the summary and requests nothing until the list is expanded', async () => {
    const r = await render(<EventGuests ev={ev} />)
    expect(toggle(r.container).textContent).toBe('2 guests')
    expect(toggle(r.container).getAttribute('aria-expanded')).toBe('false')
    expect(requests).toEqual([])
    await click(toggle(r.container))
    await waitFor(() => r.container.querySelectorAll('[data-guest]').length === 3, 'the list')
    expect(requests).toHaveLength(1)
    expect(requests[0].url).toBe('/api/v1/calendar-feeds/events/ev1/guests')
    expect(requests[0].init?.cache).toBe('no-store')
    await r.unmount()
  })

  it('reads each guest in words with the organiser, accepted count, rooms and the user named', async () => {
    const r = await render(<EventGuests ev={ev} />)
    await click(toggle(r.container))
    await waitFor(() => r.container.querySelectorAll('[data-guest]').length === 3)
    expect(toggle(r.container).textContent).toBe('Ana Silva · 2 guests, 2 accepted')
    expect(r.container.querySelector('[data-event-organizer]')!.textContent).toBe('Organizer: Ana Silva')
    const rows = [...r.container.querySelectorAll('[data-guest]')].map((x) => x.querySelector('.truncate')!.textContent)
    expect(rows).toEqual(['Ben Costa, optional, accepted', 'room-4@example.com, room, accepted', 'Me, no answer, you'])
    await r.unmount()
  })

  it('says the user\'s own answer when the event cannot be answered from here', async () => {
    const r = await render(<EventGuests ev={ev} />)
    expect(r.container.querySelector('[data-event-guests-you]')!.textContent).toBe('Your answer: No answer yet')
    await r.unmount()
  })

  it('says the list is cut with the real count', async () => {
    answer = () => json({ ...list, count: 140, truncated: true })
    const r = await render(<EventGuests ev={{ ...ev, guests: { ...ev.guests!, count: 140 } }} />)
    await click(toggle(r.container))
    await waitFor(() => !!r.container.querySelector('[data-event-guests-cut]'))
    expect(toggle(r.container).textContent).toContain('140 guests')
    expect(r.container.querySelector('[data-event-guests-cut]')!.textContent).toBe('Showing 3 of 140 guests.')
    await r.unmount()
  })

  it('says the list needs a connection offline, with the summary still shown, and makes no request', async () => {
    Object.defineProperty(navigator, 'onLine', { configurable: true, value: false })
    const r = await render(<EventGuests ev={ev} />)
    await click(toggle(r.container))
    await waitFor(() => !!r.container.querySelector('[data-event-guests-note]'))
    expect(r.container.querySelector('[data-event-guests-note]')!.textContent).toBe('The guest list needs a connection.')
    expect(toggle(r.container).textContent).toBe('2 guests')
    expect(requests).toEqual([])
    await r.unmount()
  })

  it('drops the list when the popover closes, and the device database never holds a name or an address', async () => {
    await db.calendarEvents.put(ev)
    const r = await render(<EventGuests ev={ev} />)
    await click(toggle(r.container))
    await waitFor(() => r.container.querySelectorAll('[data-guest]').length === 3)
    expect(peekGuestList('ev1')).toEqual(list)
    expect(await dump()).not.toMatch(/@|Ben Costa|Ana Silva/)
    expect(JSON.stringify({ ...localStorage })).not.toMatch(/@/)
    await r.unmount()
    expect(peekGuestList('ev1')).toBeUndefined()
    expect(await dump()).not.toMatch(/@|Ben Costa|Ana Silva/)
  })

  it('renders nothing for an event without guests', async () => {
    const r = await render(<EventGuests ev={{ ...ev, guests: undefined }} />)
    expect(r.container.innerHTML).toBe('')
    await r.unmount()
  })
})
