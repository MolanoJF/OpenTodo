// @vitest-environment happy-dom
// Where the full event editor opens from (OpenSpec change calendar-event-editor, task 5.4): the popover's All details,
// a double-click on a block, an all-day chip or an event row, Enter on a chip; the read-only popover for the rest
// with Duplicate and Convert; a pending event drawn at once, busy, swapped for the real one; calendars shown again.
import 'fake-indexeddb/auto'
import { act } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { EDIT_SETTLE_MS, flushEventEdits } from '../lib/calendarEventEdits'
import { setShowEvents, showEvents } from '../lib/calendarFeeds'
import { db, setMeta } from '../lib/db'
import { createEvent } from '../lib/eventCreate'
import { resetDeviceId } from '../lib/ops'
import type { CalendarEvent, CalendarEventDraft, CalendarSubscription } from '../lib/types'
import { click, key, render, waitFor } from '../ui/test-utils'
import { bulkToast, hideBulkToast } from './bulk/toastStore'
import { addEvent } from './calendar/eventAdd'
import { closeEventEditor, currentEventEditor } from './calendar/eventEditorStore'
import { useEventEdits } from './calendar/useEventEdits'
import { EventChip, EventRow } from './CalendarEvents'
import { CalendarView } from './CalendarView'
import { resetPanel } from './panel'
import { ToastActionHost } from './ToastActionHost'

const DAY = '2026-10-14'
const sub: CalendarSubscription = { id: 'sub1', name: 'Personal', color: '#2563eb', enabled: true, lastSuccessAt: Date.now(), managed: true, connectionId: 'c1', calendarUrl: 'https://dav/p/', editEvents: true, caps: { create: true, update: true, delete: true } }
const timed = (over: Partial<CalendarEvent>): CalendarEvent => ({ id: 'ev1', subscriptionId: 'sub1', uid: 'u1', summary: 'Coffee', location: '', start: { date: DAY, time: '10:00' }, end: { date: DAY, time: '11:00' }, allDay: false, free: false, rev: 'r1', access: { update: true, delete: true }, ...over })
const coffee = timed({})
const weekly = timed({ id: 'ev2', uid: 'u2', summary: 'Standup', start: { date: DAY, time: '13:00' }, end: { date: DAY, time: '13:30' }, access: { update: false, delete: false, reason: 'series_unsupported' } })
const offsite = timed({ id: 'ev3', uid: 'u3', summary: 'Offsite', allDay: true, start: { date: DAY }, end: { date: '2026-10-16' } })
const holiday = timed({ id: 'ev4', uid: 'u4', summary: 'Holiday', allDay: true, start: { date: DAY }, end: { date: '2026-10-15' }, access: { update: false, delete: false, reason: 'series_unsupported' } })

beforeEach(async () => {
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(new Date(2026, 9, 14, 8, 0))
  await db.delete()
  await db.open()
  resetDeviceId()
  resetPanel()
  closeEventEditor()
  hideBulkToast()
  localStorage.clear()
  vi.stubGlobal('fetch', vi.fn(async (_url: string, init?: RequestInit) => {
    const body = init?.body ? JSON.parse(String(init.body)) as Record<string, unknown> : undefined
    if (init?.method === 'POST') return new Response(JSON.stringify({ event: timed({ id: 'real1', uid: String(body?.clientId), summary: String(body?.summary), rev: 'r5' }), clientId: body?.clientId }), { status: 201, headers: { 'Content-Type': 'application/json' } })
    return new Response('{}', { status: 404 })
  }))
  await db.calendarSubscriptions.put(sub)
  await db.calendarEvents.bulkPut([coffee, weekly, offsite, holiday])
  await setMeta('calendar.events.window', { from: '2026-09-01', to: '2026-11-30', fetchedAt: Date.now() })
})
afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
  resetPanel()
  closeEventEditor()
})

const block = (root: ParentNode, id: string) => root.querySelector<HTMLElement>(`[data-event="${id}"]`)!
const chip = (root: ParentNode, id: string) => root.querySelector<HTMLElement>(`[data-event-chip="${id}"]`)!
async function dblclick(el: Element) { await act(async () => { el.dispatchEvent(new MouseEvent('dblclick', { bubbles: true, cancelable: true })) }) }
async function openDay() {
  const r = await render(<CalendarView mode="day" />)
  await waitFor(() => !!block(r.container, 'ev1') && !!chip(r.container, 'ev3'), 'the events')
  return r
}
const button = (name: string) => [...document.querySelectorAll<HTMLElement>('button')].find((b) => b.textContent?.trim() === name)

describe('opening the editor', () => {
  it('the popover\'s All details opens the editor for an editable event', async () => {
    const r = await openDay()
    await click(block(r.container, 'ev1'))
    await waitFor(() => !!button('All details'), 'the popover')
    await click(button('All details')!)
    expect(currentEventEditor()).toMatchObject({ mode: 'edit', eventId: 'ev1' })
  })

  it('a double-click on a block opens it; a locked event keeps the read-only popover with its reason, Duplicate and Convert', async () => {
    const r = await openDay()
    await dblclick(block(r.container, 'ev1'))
    expect(currentEventEditor()).toMatchObject({ mode: 'edit', eventId: 'ev1' })
    closeEventEditor()
    await dblclick(block(r.container, 'ev2'))
    expect(currentEventEditor()).toBeNull()
    await click(block(r.container, 'ev2'))
    await waitFor(() => !!document.querySelector('[data-event-locked="series_unsupported"]'), 'the reason')
    expect(button('All details')).toBeUndefined()
    await click(document.querySelector<HTMLElement>('[data-event-actions] [aria-haspopup="menu"], [data-event-actions] button[aria-label="More actions"]')!)
    await waitFor(() => !!document.querySelector('[data-event-duplicate]'), 'the overflow')
    expect(document.querySelector('[data-event-convert]')).not.toBeNull()
  })

  it('an all-day chip opens the editor on a double-click and on Enter; a locked one does not', async () => {
    const r = await openDay()
    await dblclick(chip(r.container, 'ev3'))
    expect(currentEventEditor()).toMatchObject({ mode: 'edit', eventId: 'ev3' })
    closeEventEditor()
    await key(chip(r.container, 'ev3'), 'Enter')
    expect(currentEventEditor()).toMatchObject({ mode: 'edit', eventId: 'ev3' })
    closeEventEditor()
    await dblclick(chip(r.container, 'ev4'))
    await key(chip(r.container, 'ev4'), 'Enter')
    expect(currentEventEditor()).toBeNull()
  })

  it('a Month, Plan or agenda row opens the editor on a double-click', async () => {
    const r = await render(<><EventRow ev={coffee} sub={sub} variant="cell" /><EventRow ev={weekly} sub={sub} variant="plan" /><EventChip ev={offsite} sub={sub} /></>)
    await dblclick(r.container.querySelector('[data-month-event="ev1"]')!)
    expect(currentEventEditor()).toMatchObject({ mode: 'edit', eventId: 'ev1' })
    closeEventEditor()
    await dblclick(r.container.querySelector('[data-plan-event="ev2"]')!)
    expect(currentEventEditor()).toBeNull()
  })

  it('an all-day event is editable in the editor only: it does not drag, and the Delete key ignores it', async () => {
    const r = await openDay()
    expect(chip(r.container, 'ev3').getAttribute('data-drag-id')).toBeNull()
    let removed: boolean | null = null
    function Probe() {
      const edits = useEventEdits([offsite, coffee], undefined, null, () => {})
      return <button type="button" data-probe onClick={() => { removed = edits.remove('event:ev3') }}>probe</button>
    }
    await render(<Probe />)
    await click(document.querySelector('[data-probe]')!)
    expect(removed).toBe(false)
  })
})

describe('a pending event', () => {
  const draft = (over: Partial<CalendarEventDraft> = {}): CalendarEventDraft => ({ clientId: 'pend-0001', connectionId: 'c1', calendarUrl: 'https://dav/p/', subscriptionId: 'sub1', summary: 'Dentist', start: { date: DAY, time: '15:00' }, end: { date: DAY, time: '15:30' }, allDay: false, location: '', description: '', free: false, ...over })

  it('is drawn at once as a busy block and swaps to the real event after the answer', async () => {
    const r = await openDay()
    await createEvent(draft(), Date.now())
    await waitFor(() => !!block(r.container, 'pending:pend-0001'), 'the pending block')
    expect(block(r.container, 'pending:pend-0001').getAttribute('aria-busy')).toBe('true')
    await act(async () => { await flushEventEdits(Date.now() + EDIT_SETTLE_MS + 1000) })
    await waitFor(() => !!block(r.container, 'real1'), 'the real block')
    expect(block(r.container, 'pending:pend-0001')).toBeNull()
    expect(block(r.container, 'real1').getAttribute('aria-busy')).toBeNull()
  })

  it('opens the editor on its draft', async () => {
    const r = await openDay()
    await createEvent(draft(), Date.now())
    await waitFor(() => !!block(r.container, 'pending:pend-0001'), 'the pending block')
    await dblclick(block(r.container, 'pending:pend-0001'))
    expect(currentEventEditor()).toMatchObject({ mode: 'edit', eventId: 'pending:pend-0001' })
  })

  it('a create the server refuses removes the block and offers to reopen the editor on what was typed', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({ error: { code: 'event_edits_off', message: 'off' } }), { status: 403, headers: { 'Content-Type': 'application/json' } })))
    await render(<ToastActionHost />)
    await createEvent(draft(), Date.now())
    await act(async () => { await flushEventEdits(Date.now() + EDIT_SETTLE_MS + 1000) })
    await waitFor(() => bulkToast()?.action?.label === 'Edit again', 'the toast')
    expect(bulkToast()!.label).toBe('Editing events is off for this connection')
    expect(await db.calendarEvents.get('pending:pend-0001')).toBeUndefined()
    expect(await db.eventOutbox.count()).toBe(0)
    bulkToast()!.action!.run()
    expect(currentEventEditor()).toMatchObject({ mode: 'create', draft: { summary: 'Dentist', clientId: 'pend-0001' } })
  })

  it('creating with calendars hidden shows them again and says so', async () => {
    await setShowEvents(false)
    expect(await addEvent(draft())).toBe(true)
    expect(await showEvents()).toBe(true)
    expect(bulkToast()?.label).toBe('Event added. Calendars are shown again.')
    expect(await db.eventOutbox.count()).toBe(1)
  })

  it('Undo of the toast removes an unsent create without a request', async () => {
    expect(await addEvent(draft())).toBe(true)
    expect(bulkToast()?.label).toBe('Event added')
    bulkToast()!.action!.run()
    await waitFor(async () => (await db.eventOutbox.count()) === 0, 'the removal')
    expect(await db.calendarEvents.get('pending:pend-0001')).toBeUndefined()
    await flushEventEdits(Date.now() + 60_000)
    expect(fetch).not.toHaveBeenCalled()
  })
})
