// @vitest-environment happy-dom
import { closeEventEditor } from './calendar/eventEditorStore'
import type { ReactNode } from 'react'
import { EventPageTestRoute } from './eventPage.testkit'
// Events with guests in Day (OpenSpec change calendar-event-guests, tasks 5.1 to 5.5): a drag of a meeting the user organises
// asks "Tell N guests?" after the scope menu, Cancel restores the block and sends nothing, Save sends `notifyGuests: true`
// once and never queues, with no Undo; the popover shows the guests line, the reply row and Join; a guest's event is locked
// with its reason; a delete asks, waits the 8 seconds and sends once.
import 'fake-indexeddb/auto'
import { act } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { db, setMeta } from '../lib/db'
import { resetDeviceId } from '../lib/ops'
import type { CalendarEvent, CalendarSubscription } from '../lib/types'
import { click, key, render as renderBase, waitFor } from '../ui/test-utils'
import { useBulkToast } from './bulk/toastStore'
import { CalendarView } from './CalendarView'
import { PX_PER_MIN } from './calendar/grid'
import { resetPanel } from './panel'

const render = (ui: ReactNode) => renderBase(<>{ui}<EventPageTestRoute /></>)
const DAY = '2026-10-14'
const y = (minutes: number) => minutes * PX_PER_MIN
const outlook: CalendarSubscription = { id: 'sub1', name: 'Work', color: '#2563eb', enabled: true, lastSuccessAt: Date.now(), managed: true, connectionId: 'c1', editEvents: true, maxReminders: 1, guests: { respond: true, silent: true, edit: true } }
const base: CalendarEvent = { id: 'ev1', subscriptionId: 'sub1', uid: 'u1', summary: 'Board', location: '', start: { date: DAY, time: '10:00' }, end: { date: DAY, time: '11:00' }, allDay: false, free: false, rev: 'r1' }
const mine: CalendarEvent = { ...base, access: { update: true, delete: true, notifies: true }, guests: { count: 3, organizer: true, canRespond: false }, conference: { url: 'https://teams.microsoft.com/l/meetup-join/x', provider: 'teams' } }
const invite: CalendarEvent = { ...base, id: 'ev2', uid: 'u2', summary: 'Offsite', start: { date: DAY, time: '13:00' }, end: { date: DAY, time: '14:00' }, access: { update: false, delete: false, reason: 'not_organizer' }, guests: { count: 5, you: 'needs-action', organizer: false, canRespond: true } }
const seriesMine: CalendarEvent = { ...mine, id: 'ev3', uid: 'u3', summary: 'Weekly', start: { date: DAY, time: '07:00' }, end: { date: DAY, time: '08:00' }, series: { id: 'ser1', first: false, rev: 'r1' }, conference: undefined }

let requests: { method: string; url: string; body?: Record<string, unknown> }[]
const json = (status: number, b: unknown) => new Response(JSON.stringify(b), { status, headers: { 'Content-Type': 'application/json' } })

beforeEach(async () => {
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(new Date(2026, 9, 14, 8, 0))
  await db.delete()
  await db.open()
  resetDeviceId()
  resetPanel()
  closeEventEditor()
  localStorage.clear()
  requests = []
  vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
    const body = init?.body ? JSON.parse(String(init.body)) as Record<string, unknown> : undefined
    requests.push({ method: init?.method ?? 'GET', url, body })
    if (init?.method === 'PATCH') return json(200, { event: { ...mine, ...body, id: 'ev1b', rev: 'r2' }, replaces: 'ev1' })
    if (init?.method === 'DELETE') return new Response(null, { status: 204 })
    if (init?.method === 'POST' && String(url).endsWith('/response')) return json(200, { event: { ...invite, rev: 'r8', guests: { ...invite.guests, you: body?.response } }, replaces: 'ev2' })
    if (String(url).includes('/guests')) return json(200, { organizer: { name: 'Ana Silva', email: 'ana@example.com', you: false }, guests: [{ name: 'Ben Costa', email: 'ben@example.com', role: 'optional', response: 'accepted', you: false }], count: 5, truncated: false })
    return json(404, {})
  }))
  await db.calendarSubscriptions.put(outlook)
  await db.calendarEvents.bulkPut([mine, invite, seriesMine])
  await setMeta('calendar.events.window', { from: '2026-09-01', to: '2026-11-30', fetchedAt: Date.now() })
})
afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
  resetPanel()
  closeEventEditor()
})

function layout() {
  const spans: Record<string, [number, number]> = { ev1: [10 * 60, 60], ev2: [13 * 60, 60], ev3: [7 * 60, 60] }
  vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function (this: HTMLElement) {
    const box = (top: number, height: number) => ({ top, bottom: top + height, height, left: 0, right: 100, width: 100, x: 0, y: top, toJSON() {} }) as DOMRect
    const id = this.getAttribute('data-event')
    if (id && spans[id]) return box(y(spans[id][0]), y(spans[id][1]))
    return box(0, y(24 * 60))
  })
  document.elementFromPoint = () => document.querySelector<HTMLElement>(`[data-drop-day="${DAY}"]`)
}
async function drag(el: Element, fromY: number, toY: number) {
  const init = { bubbles: true, button: 0, pointerId: 1, pointerType: 'mouse', clientX: 50 }
  await act(async () => { el.dispatchEvent(new PointerEvent('pointerdown', { ...init, clientY: fromY })) })
  await act(async () => { window.dispatchEvent(new PointerEvent('pointermove', { ...init, clientY: fromY + (toY > fromY ? 6 : -6) })) })
  await act(async () => { window.dispatchEvent(new PointerEvent('pointermove', { ...init, clientY: toY })) })
  await act(async () => { window.dispatchEvent(new PointerEvent('pointerup', { ...init, clientY: toY })) })
}
function Toast() {
  const toast = useBulkToast()
  return <p data-test-toast>{toast?.label ?? ''}{toast?.action && <button type="button" data-test-undo onClick={() => toast.action?.run()}>undo</button>}</p>
}
const rendered = async () => {
  const r = await render(<><CalendarView mode="day" date={DAY} /><Toast /></>)
  await waitFor(() => r.container.querySelector('[data-event="ev1"]') !== null)
  layout()
  return r
}
const dialog = () => document.querySelector('[data-notify-guests]')
const patches = () => requests.filter((q) => q.method === 'PATCH')
const toastText = () => document.querySelector('[data-test-toast]')!.firstChild?.textContent
const scopeItems = () => [...document.querySelectorAll<HTMLButtonElement>('[data-event-scope]')]
const at = (time: string) => ({ date: DAY, time })
const openPopover = async (r: { container: HTMLElement }, id: string) => {
  await click(r.container.querySelector(`[data-event="${id}"]`)!)
  await waitFor(() => !!document.querySelector('[data-event-details]'), 'the popover')
}

describe('a drag of a meeting the user organises', () => {
  it('asks "Tell 3 guests?"; Cancel puts the block back and sends nothing, not even later', async () => {
    const r = await rendered()
    await drag(r.container.querySelector('[data-event="ev1"]')!, y(10 * 60 + 30), y(14 * 60 + 30))
    await waitFor(() => !!dialog(), 'the dialog')
    expect(document.body.textContent).toContain('Tell 3 guests?')
    await waitFor(async () => (await db.calendarEvents.get('ev1'))!.start.time === '14:00', 'the block shows the move')
    expect(await db.eventOutbox.count()).toBe(0)
    await click(document.querySelector('[data-notify-cancel]')!)
    await waitFor(() => !dialog())
    await waitFor(async () => (await db.calendarEvents.get('ev1'))!.start.time === '10:00', 'the block back')
    expect(requests.filter((q) => q.method !== 'GET')).toEqual([])
    expect(await db.eventOutbox.count()).toBe(0)
    await r.unmount()
  })

  it('Escape cancels too', async () => {
    const r = await rendered()
    await drag(r.container.querySelector('[data-event="ev1"]')!, y(10 * 60 + 30), y(14 * 60 + 30))
    await waitFor(() => !!dialog())
    await key(document.activeElement!, 'Escape')
    await waitFor(() => !dialog())
    expect(requests.filter((q) => q.method !== 'GET')).toEqual([])
    await r.unmount()
  })

  it('Save and tell guests sends once with notifyGuests, never queues, and offers no Undo', async () => {
    const r = await rendered()
    await drag(r.container.querySelector('[data-event="ev1"]')!, y(10 * 60 + 30), y(14 * 60 + 30))
    await waitFor(() => !!dialog())
    await click(document.querySelector('[data-notify-confirm]')!)
    await waitFor(() => patches().length === 1, 'the request')
    expect(patches()[0]).toEqual({ method: 'PATCH', url: '/api/v1/calendar-sync/connections/c1/events/ev1', body: { rev: 'r1', start: at('14:00'), end: at('15:00'), allDay: false, notifyGuests: true } })
    await waitFor(async () => (await db.calendarEvents.get('ev1b')) !== undefined, 'the answer swapped in')
    expect(await db.eventOutbox.count()).toBe(0)
    await waitFor(() => toastText() === 'Guests were told')
    expect(document.querySelector('[data-test-undo]')).toBeNull()
    await r.unmount()
  })

  it('an occurrence asks the scope first, then the guests', async () => {
    const r = await rendered()
    await drag(r.container.querySelector('[data-event="ev3"]')!, y(7 * 60 + 30), y(9 * 60 + 30))
    await waitFor(() => scopeItems().length > 0, 'the scope menu')
    expect(dialog()).toBeNull()
    await act(async () => scopeItems().find((i) => i.getAttribute('data-event-scope') === 'this')!.click())
    await waitFor(() => !!dialog(), 'then the dialog')
    expect(patches()).toHaveLength(0)
    await click(document.querySelector('[data-notify-confirm]')!)
    await waitFor(() => patches().length === 1)
    expect(patches()[0].body).toMatchObject({ scope: 'this', notifyGuests: true })
    await r.unmount()
  })

  it('a meeting without guests never asks', async () => {
    await db.calendarEvents.put({ ...mine, access: { update: true, delete: true }, guests: undefined })
    const r = await rendered()
    await drag(r.container.querySelector('[data-event="ev1"]')!, y(10 * 60 + 30), y(14 * 60 + 30))
    await waitFor(async () => (await db.eventOutbox.count()) === 1, 'the queued command')
    expect(dialog()).toBeNull()
    await r.unmount()
  })
})

describe('deleting a meeting with guests', () => {
  const askDelete = async (r: { container: HTMLElement }) => {
    await openPopover(r, 'ev1')
    await click(await waitForEl('[data-event-open-editor]'))
    await click(await waitForEl('[data-event-form-menu]'))
    await click(await waitForEl('[data-event-form-delete]'))
    await waitFor(() => !!dialog(), 'the dialog')
  }
  const waitForEl = async (sel: string) => { await waitFor(() => !!document.querySelector(sel), sel); return document.querySelector(sel)! }

  it('asks, hides the block and offers Undo for the 8 second pause, sending nothing yet', async () => {
    const r = await rendered()
    await askDelete(r)
    expect(document.body.textContent).toContain('Outlook tells them this event is cancelled')
    expect(requests.filter((q) => q.method === 'DELETE')).toEqual([])
    await click(document.querySelector('[data-notify-confirm]')!)
    await waitFor(async () => (await db.calendarEvents.get('ev1')) === undefined, 'the block hidden')
    expect(requests.filter((q) => q.method === 'DELETE')).toEqual([])
    expect(await db.eventOutbox.count()).toBe(0)
    await waitFor(() => toastText() === 'Event deleted')
    await click(document.querySelector('[data-test-undo]')!)
    await waitFor(async () => (await db.calendarEvents.get('ev1')) !== undefined, 'the block back')
    expect(requests.filter((q) => q.method === 'DELETE')).toEqual([])
    await r.unmount()
  })

  it('Cancel on the question deletes nothing', async () => {
    const r = await rendered()
    await askDelete(r)
    await click(document.querySelector('[data-notify-cancel]')!)
    await waitFor(() => !dialog())
    expect(await db.calendarEvents.get('ev1')).toBeTruthy()
    expect(requests.filter((q) => q.method === 'DELETE')).toEqual([])
    await r.unmount()
  })
})

describe('the popover of an event with guests', () => {
  it('shows the guests line, expands the list, and has Join for the call', async () => {
    const r = await rendered()
    await openPopover(r, 'ev1')
    await waitFor(() => !!document.querySelector('[data-event-guests-toggle]'), 'the guests line')
    expect(document.querySelector('[data-event-guests-toggle]')!.textContent).toBe('3 guests')
    expect(requests.some((q) => q.url.includes('/guests'))).toBe(false)
    const join = document.querySelector<HTMLAnchorElement>('[data-event-join]')!
    expect(join.textContent).toContain('Join Teams')
    expect(join.getAttribute('href')).toBe('https://teams.microsoft.com/l/meetup-join/x')
    await click(document.querySelector('[data-event-guests-toggle]')!)
    await waitFor(() => !!document.querySelector('[data-guest]'), 'the list')
    expect(requests.filter((q) => q.url.includes('/guests'))).toHaveLength(1)
    await r.unmount()
  })

  it('shows the reply row on a read-only invitation and sends the answer', async () => {
    const r = await rendered()
    await openPopover(r, 'ev2')
    await waitFor(() => !!document.querySelector('[data-event-respond]'), 'the reply row')
    expect(document.querySelector('[data-event-locked]')!.textContent).toBe("You're a guest. Only the organizer can change this event")
    await click(document.querySelector('[data-segment="tentative"]')!)
    await waitFor(() => requests.some((q) => q.url.endsWith('/events/ev2/response')), 'the reply')
    expect(requests.find((q) => q.url.endsWith('/response'))!.body).toEqual({ rev: 'r1', response: 'tentative' })
    await waitFor(async () => (await db.calendarEvents.get('ev2'))?.guests?.you === 'tentative')
    expect(await db.eventOutbox.count()).toBe(0)
    await r.unmount()
  })

  it('never starts a drag on an invitation the user did not organise', async () => {
    const r = await rendered()
    const block = r.container.querySelector('[data-event="ev2"]')!
    expect(block.hasAttribute('data-editable')).toBe(false)
    await drag(block, y(13 * 60 + 30), y(16 * 60))
    expect(dialog()).toBeNull()
    expect(await db.eventOutbox.count()).toBe(0)
    await r.unmount()
  })
})
