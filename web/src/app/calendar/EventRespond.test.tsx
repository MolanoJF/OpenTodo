// @vitest-environment happy-dom
// Answering an invitation (OpenSpec change calendar-event-guests, task 5.2, design D11, D13): three answers in a labelled
// group with the current one chosen, "Tell the organizer" only where the calendar can skip the message, the request
// body per choice, the row on a read-only event and absent when the user cannot answer, disabled offline with the reason.
import 'fake-indexeddb/auto'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { eventEditNoticeText, onEventEditNotice, type EventEditNotice } from '../../lib/calendarEventEdits'
import { db, setMeta, useDatabase } from '../../lib/db'
import type { CalendarEvent, CalendarSubscription } from '../../lib/types'
import { click, render, waitFor } from '../../ui/test-utils'
import { EventRespond } from './EventRespond'

const outlook: CalendarSubscription = { id: 's1', name: 'Work', color: '', enabled: true, lastSuccessAt: 1, managed: true, connectionId: 'c1', maxReminders: 1, guests: { respond: true, silent: true, edit: true } }
const google: CalendarSubscription = { id: 's2', name: 'Me', color: '', enabled: true, lastSuccessAt: 1, managed: true, connectionId: 'c2', calendarUrl: 'https://apidata.googleusercontent.com/caldav/v2/me@gmail.com/events/', guests: { respond: true, silent: false, edit: false } }
const invite: CalendarEvent = {
  id: 'ev1', subscriptionId: 's1', uid: 'u', summary: 'Design review', location: '', allDay: false, free: false, rev: 'r1',
  start: { date: '2026-11-13', time: '10:00' }, end: { date: '2026-11-13', time: '11:00' }, access: { update: false, delete: false, reason: 'not_organizer' },
  guests: { count: 3, you: 'needs-action', organizer: false, canRespond: true },
}

let requests: { method: string; url: string; body?: Record<string, unknown> }[]
let reply: () => Response
const json = (b: unknown, status = 200) => new Response(JSON.stringify(b), { status, headers: { 'Content-Type': 'application/json' } })
let n = 0
beforeEach(async () => {
  useDatabase(`event-respond-${Date.now()}-${n++}`)
  requests = []
  reply = () => json({ event: { ...invite, rev: 'r2', guests: { ...invite.guests!, you: 'accepted' } }, replaces: 'ev1' })
  vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
    requests.push({ method: init?.method ?? 'GET', url, body: init?.body ? JSON.parse(String(init.body)) : undefined })
    return String(url).endsWith('/response') ? reply() : json({}, 404)
  }))
  await db.calendarSubscriptions.bulkPut([outlook, google])
  await db.calendarEvents.put(invite)
  await setMeta('calendar.events.window', { from: '2026-09-01', to: '2026-12-30', fetchedAt: Date.now() })
})
afterEach(() => {
  vi.unstubAllGlobals()
  Object.defineProperty(navigator, 'onLine', { configurable: true, value: true })
})

const radio = (c: HTMLElement, id: string) => c.querySelector<HTMLButtonElement>(`[data-segment="${id}"]`)!
const posts = () => requests.filter((r) => r.method === 'POST')

describe('EventRespond', () => {
  it('shows Yes, Maybe and No as a labelled group, none chosen yet, and the answer in words', async () => {
    const r = await render(<EventRespond ev={invite} sub={outlook} />)
    const group = r.container.querySelector('[role="radiogroup"]')!
    expect(group.getAttribute('aria-label')).toBe('Your answer')
    expect([...group.querySelectorAll('[role="radio"]')].map((b) => [b.textContent, b.getAttribute('aria-checked')])).toEqual([['Yes', 'false'], ['Maybe', 'false'], ['No', 'false']])
    expect(r.container.querySelector('[data-event-respond-you]')!.textContent).toBe('Your answer: No answer yet')
    await r.unmount()
  })

  it('marks the current answer', async () => {
    const r = await render(<EventRespond ev={{ ...invite, guests: { ...invite.guests!, you: 'tentative' } }} sub={outlook} />)
    expect(radio(r.container, 'tentative').getAttribute('aria-checked')).toBe('true')
    expect(r.container.querySelector('[data-event-respond-you]')!.textContent).toBe('Your answer: Maybe')
    await r.unmount()
  })

  it.each(['accepted', 'tentative', 'declined'] as const)('%s sends one request with the rev and the answer, and no sendResponse', async (response) => {
    const r = await render(<EventRespond ev={invite} sub={outlook} />)
    await click(radio(r.container, response))
    await waitFor(() => posts().length === 1, 'the reply')
    expect(posts()[0].url).toBe('/api/v1/calendar-sync/connections/c1/events/ev1/response')
    expect(posts()[0].body).toEqual({ rev: 'r1', response })
    await waitFor(async () => (await db.calendarEvents.get('ev1'))?.guests?.you === 'accepted', 'the row holds the answer')
    expect(await db.eventOutbox.count()).toBe(0)
    await r.unmount()
  })

  it('offers "Tell the organizer" only where the calendar can skip it, and sends sendResponse false only from the switch', async () => {
    const r = await render(<EventRespond ev={invite} sub={outlook} />)
    const sw = r.container.querySelector<HTMLButtonElement>('[role="switch"]')!
    expect(sw.closest('label')!.textContent).toContain('Tell the organizer')
    expect(sw.getAttribute('aria-checked')).toBe('true')
    await click(sw)
    await click(radio(r.container, 'declined'))
    await waitFor(() => posts().length === 1)
    expect(posts()[0].body).toEqual({ rev: 'r1', response: 'declined', sendResponse: false })
    await r.unmount()
    const g = await render(<EventRespond ev={{ ...invite, subscriptionId: 's2' }} sub={google} />)
    expect(g.container.querySelector('[role="switch"]')).toBeNull()
    await g.unmount()
  })

  it('works on a read-only event and is absent when the user cannot answer', async () => {
    expect(invite.access!.update).toBe(false)
    const r = await render(<EventRespond ev={invite} sub={outlook} />)
    expect(r.container.querySelector('[data-event-respond]')).not.toBeNull()
    await r.unmount()
    for (const guests of [{ ...invite.guests!, canRespond: false }, { ...invite.guests!, organizer: true, canRespond: false }, undefined]) {
      const x = await render(<EventRespond ev={{ ...invite, guests }} sub={outlook} />)
      expect(x.container.innerHTML).toBe('')
      await x.unmount()
    }
  })

  it('is disabled offline with the reason, and sends nothing', async () => {
    Object.defineProperty(navigator, 'onLine', { configurable: true, value: false })
    const r = await render(<EventRespond ev={invite} sub={outlook} />)
    expect(radio(r.container, 'accepted').disabled).toBe(true)
    expect(r.container.querySelector('[data-event-respond-offline]')!.textContent).toBe("Guests are told when you're online")
    await click(radio(r.container, 'accepted'))
    expect(requests).toEqual([])
    await r.unmount()
  })

  it('asks This event or All events for an occurrence and sends the scope chosen', async () => {
    const occ = { ...invite, series: { id: 'sr', rev: 'r1' } }
    await db.calendarEvents.put(occ)
    const r = await render(<EventRespond ev={occ} sub={outlook} />)
    await click(radio(r.container, 'accepted'))
    await waitFor(() => document.querySelectorAll('[data-event-scope]').length === 2, 'the scope menu')
    expect([...document.querySelectorAll('[data-event-scope]')].map((i) => i.getAttribute('data-event-scope'))).toEqual(['this', 'all'])
    expect(document.querySelector('[role="menu"]')!.getAttribute('aria-label')).toBe('Answer for which events')
    expect(posts()).toEqual([])
    await click(document.querySelector('[data-event-scope="all"]')!)
    await waitFor(() => posts().length === 1)
    expect(posts()[0].body).toEqual({ rev: 'r1', response: 'accepted', scope: 'all' })
    await r.unmount()
  })

  it('on Google offers no This event: the answer is for all events and sent without asking', async () => {
    const occ = { ...invite, subscriptionId: 's2', series: { id: 'sr' } }
    await db.calendarEvents.put(occ)
    const r = await render(<EventRespond ev={occ} sub={google} />)
    await click(radio(r.container, 'tentative'))
    await waitFor(() => posts().length === 1)
    expect(posts()[0].url).toBe('/api/v1/calendar-sync/connections/c2/events/ev1/response')
    expect(posts()[0].body).toEqual({ rev: 'r1', response: 'tentative', scope: 'all' })
    expect(document.querySelector('[data-event-scope]')).toBeNull()
    await r.unmount()
  })

  it('says the calendar did not keep the answer and shows the real one', async () => {
    const notices: EventEditNotice[] = []
    const off = onEventEditNotice((x) => notices.push(x))
    reply = () => json({ error: { code: 'calendar_unreachable', message: 'calendar_unreachable: response_not_kept' } }, 502)
    await db.calendarEvents.put({ ...invite, subscriptionId: 's2' })
    const r = await render(<EventRespond ev={{ ...invite, subscriptionId: 's2' }} sub={google} />)
    await click(radio(r.container, 'accepted'))
    await waitFor(() => notices.length === 1, 'the notice')
    expect(eventEditNoticeText(notices[0])).toBe("Google didn't keep your answer")
    expect((await db.calendarEvents.get('ev1'))!.guests!.you).toBe('needs-action')
    off()
    await r.unmount()
  })
})
