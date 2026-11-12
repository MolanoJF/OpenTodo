// @vitest-environment happy-dom
// Events of a connected calendar edited in Day (OpenSpec change calendar-event-edits, tasks 5.1 to 5.4):
// a drop, a resize, the popover's Save and Delete and the keys all queue the same command with an
// Undo toast; a locked event never starts a drag, takes no key and says why.
import 'fake-indexeddb/auto'
import { act } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushEventEdits } from '../lib/calendarEventEdits'
import { db, setMeta } from '../lib/db'
import { resetDeviceId } from '../lib/ops'
import type { CalendarEvent, CalendarSubscription } from '../lib/types'
import { click, key, render, type, waitFor } from '../ui/test-utils'
import { useBulkToast } from './bulk/toastStore'
import { CalendarView } from './CalendarView'
import { PX_PER_MIN } from './calendar/grid'
import { resetPanel } from './panel'

const DAY = '2026-10-14'
const y = (minutes: number) => minutes * PX_PER_MIN

const sub: CalendarSubscription = { id: 'sub1', name: 'Personal', color: '#2563eb', enabled: true, lastSuccessAt: Date.now(), managed: true, connectionId: 'c1', editEvents: true }
const coffee: CalendarEvent = { id: 'ev1', subscriptionId: 'sub1', uid: 'u1', summary: 'Coffee', location: '', start: { date: DAY, time: '10:00' }, end: { date: DAY, time: '11:00' }, allDay: false, free: false, rev: 'r1', access: { update: true, delete: true } }
const weekly: CalendarEvent = { ...coffee, id: 'ev2', uid: 'u2', summary: 'Standup', start: { date: DAY, time: '13:00' }, end: { date: DAY, time: '13:30' }, access: { update: false, delete: false, reason: 'series_unsupported' } }
const routine: CalendarEvent = { ...coffee, id: 'ev3', uid: 'u3', summary: 'Routine', start: { date: DAY, time: '07:00' }, end: { date: DAY, time: '08:30' }, series: { id: 'ser1', repeat: { freq: 'daily', interval: 1, days: [], ends: { type: 'never' } }, rev: 'r1', first: false } }

let requests: { method: string; url: string; body: unknown }[]

beforeEach(async () => {
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(new Date(2026, 9, 14, 8, 0))
  await db.delete()
  await db.open()
  resetDeviceId()
  resetPanel()
  localStorage.clear()
  requests = []
  vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
    requests.push({ method: init?.method ?? 'GET', url, body: init?.body ? JSON.parse(String(init.body)) : undefined })
    if (init?.method === 'PATCH') {
      const body = JSON.parse(String(init.body)) as Partial<CalendarEvent>
      return new Response(JSON.stringify({ event: { ...coffee, ...body, id: 'ev1b', rev: 'r2' }, replaces: 'ev1' }), { status: 200, headers: { 'Content-Type': 'application/json' } })
    }
    if (init?.method === 'DELETE') return new Response(null, { status: 204 })
    return new Response('{}', { status: 404 })
  }))
  await db.calendarSubscriptions.put(sub)
  await db.calendarEvents.bulkPut([coffee, weekly])
  await setMeta('calendar.events.window', { from: '2026-09-01', to: '2026-11-30', fetchedAt: Date.now() })
})
afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
  resetPanel()
})

/** The block of an event sits where its minutes put it; the day column starts at y 0. */
function layout() {
  const spans: Record<string, [number, number]> = { ev1: [10 * 60, 60], ev2: [13 * 60, 30], ev3: [7 * 60, 90] }
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
const command = () => db.eventOutbox.get('c1:ev1')
const toastText = () => document.querySelector('[data-test-toast]')!.firstChild?.textContent
const at = (time: string) => ({ date: DAY, time })

describe('dragging an event', () => {
  it('moves a block to a new time: one command, the block moves at once, a toast offers Undo', async () => {
    const r = await rendered()
    const block = r.container.querySelector('[data-event="ev1"]')!
    expect(block.hasAttribute('data-editable')).toBe(true)
    // Grabbed in the middle (30 minutes below the top), dropped with the pointer at 14:30: the block starts at 14:00.
    await drag(block, y(10 * 60 + 30), y(14 * 60 + 30))
    await waitFor(async () => (await command()) !== undefined, 'the queued command')
    expect((await command())!.patch).toEqual({ start: at('14:00'), end: at('15:00'), allDay: false })
    expect((await db.calendarEvents.get('ev1'))!.start.time).toBe('14:00')
    await waitFor(() => toastText() === 'Event moved')
    await r.unmount()
  })

  it('writes one request after the settle, and the answer replaces the row with its new id', async () => {
    const r = await rendered()
    await drag(r.container.querySelector('[data-event="ev1"]')!, y(10 * 60 + 30), y(14 * 60 + 30))
    await waitFor(async () => (await command()) !== undefined)
    await act(async () => { await flushEventEdits(Date.now() + 1000) })
    expect(requests.filter((q) => q.method === 'PATCH')).toEqual([{ method: 'PATCH', url: '/api/v1/calendar-sync/connections/c1/events/ev1', body: { rev: 'r1', start: at('14:00'), end: at('15:00'), allDay: false } }])
    await waitFor(async () => (await db.calendarEvents.get('ev1b')) !== undefined)
    expect(await db.calendarEvents.get('ev1')).toBeUndefined()
    await r.unmount()
  })

  it('resizes from the lower edge: the end moves, the start stays', async () => {
    const r = await rendered()
    const block = r.container.querySelector('[data-event="ev1"]')!
    expect(block.querySelector('[data-resize-handle]')).toBeTruthy()
    await drag(block, y(11 * 60) - 1, y(12 * 60))
    await waitFor(async () => (await command())?.patch !== undefined)
    expect((await command())!.patch).toEqual({ start: at('10:00'), end: at('12:00'), allDay: false })
    await r.unmount()
  })

  it('resizes from the upper edge: the start moves, the end stays', async () => {
    const r = await rendered()
    await drag(r.container.querySelector('[data-event="ev1"]')!, y(10 * 60) + 1, y(9 * 60 + 30))
    await waitFor(async () => (await command())?.patch !== undefined)
    expect((await command())!.patch).toEqual({ start: at('09:30'), end: at('11:00'), allDay: false })
    await r.unmount()
  })

  it('Undo restores the event with an inverse change: moving back removes the command, so no request is made', async () => {
    const r = await rendered()
    await drag(r.container.querySelector('[data-event="ev1"]')!, y(10 * 60 + 30), y(14 * 60 + 30))
    await waitFor(async () => (await command()) !== undefined)
    await click(document.querySelector('[data-test-undo]')!)
    await waitFor(async () => (await command()) === undefined, 'the command removed')
    expect((await db.calendarEvents.get('ev1'))!.start.time).toBe('10:00')
    await act(async () => { await flushEventEdits(Date.now() + 1000) })
    expect(requests.filter((q) => q.method === 'PATCH')).toEqual([])
    await r.unmount()
  })

  it('never starts a drag on a locked event, and says why', async () => {
    const r = await rendered()
    const locked = r.container.querySelector('[data-event="ev2"]')!
    expect(locked.hasAttribute('data-editable')).toBe(false)
    expect(locked.querySelector('[data-resize-handle]')).toBeNull()
    await drag(locked, y(13 * 60 + 15), y(16 * 60))
    expect(await db.eventOutbox.count()).toBe(0)
    expect((await db.calendarEvents.get('ev2'))!.start.time).toBe('13:00')
    await click(locked)
    await waitFor(() => document.querySelector('[data-event-details]') !== null)
    expect(document.querySelector('[data-event-locked="series_unsupported"]')!.textContent).toBe('This repeat is too complex to change here')
    expect(document.querySelector('[data-event-edit]')).toBeNull()
    await r.unmount()
  })
})

describe('the popover editor', () => {
  it('Save makes the same command as a drag', async () => {
    const r = await rendered()
    await click(r.container.querySelector('[data-event="ev1"]')!)
    await waitFor(() => document.querySelector('[data-event-edit]') !== null)
    await type(document.querySelector<HTMLInputElement>('[data-event-edit-start]')!, '14:00')
    await type(document.querySelector<HTMLInputElement>('[data-event-edit-end]')!, '15:00')
    await click(document.querySelector('[data-event-edit-save]')!)
    await waitFor(async () => (await command()) !== undefined)
    expect((await command())!.patch).toEqual({ start: at('14:00'), end: at('15:00'), allDay: false })
    await waitFor(() => document.querySelector('[data-event-edit]') === null)
    await r.unmount()
  })

  it('renames, and refuses an end that is not after the start', async () => {
    const r = await rendered()
    await click(r.container.querySelector('[data-event="ev1"]')!)
    await waitFor(() => document.querySelector('[data-event-edit]') !== null)
    await type(document.querySelector<HTMLInputElement>('[data-event-edit-end]')!, '09:00')
    await waitFor(() => document.querySelector<HTMLButtonElement>('[data-event-edit-save]')!.disabled)
    await type(document.querySelector<HTMLInputElement>('[data-event-edit-end]')!, '11:00')
    await type(document.querySelector<HTMLInputElement>('[data-event-edit-title]')!, 'Coffee with Sam')
    await click(document.querySelector('[data-event-edit-save]')!)
    await waitFor(async () => (await command()) !== undefined)
    expect((await command())!.patch).toEqual({ summary: 'Coffee with Sam' })
    await waitFor(() => toastText() === 'Event renamed')
    await r.unmount()
  })

  it('Delete hides the event and queues a delete that waits for Undo', async () => {
    const r = await rendered()
    await click(r.container.querySelector('[data-event="ev1"]')!)
    await waitFor(() => document.querySelector('[data-event-edit-delete]') !== null)
    await click(document.querySelector('[data-event-edit-delete]')!)
    await waitFor(() => r.container.querySelector('[data-event="ev1"]') === null)
    expect((await command())!.patch).toEqual({ delete: true })
    expect((await command())!.notBefore).toBeGreaterThan(Date.now() + 7000)
    await waitFor(() => toastText() === 'Event deleted')
    await click(document.querySelector('[data-test-undo]')!)
    await waitFor(() => r.container.querySelector('[data-event="ev1"]') !== null)
    expect(await db.eventOutbox.count()).toBe(0)
    await r.unmount()
  })

  it('says editing is off, with the way to the connection, when the switch is off', async () => {
    await db.calendarEvents.put({ ...coffee, access: { update: false, delete: false, reason: 'edits_off' } })
    const r = await rendered()
    await click(r.container.querySelector('[data-event="ev1"]')!)
    await waitFor(() => document.querySelector('[data-event-locked="edits_off"]') !== null)
    expect(document.querySelector('[data-event-locked="edits_off"]')!.textContent).toContain('Editing events is off for this connection')
    expect(document.querySelector('[data-event-locked="edits_off"] a')!.textContent).toBe('Calendar sync settings')
    await r.unmount()
  })
})

describe('the keys', () => {
  it('j selects a changeable event, Down twice is one 30-minute change, Delete hides it with Undo', async () => {
    const r = await rendered()
    await key(document.body, 'j')
    await waitFor(() => r.container.querySelector('[data-event="ev1"]')!.className.includes('ring-accent'), 'the selection')
    await key(document.body, 'ArrowDown')
    await key(document.body, 'ArrowDown')
    await waitFor(async () => (await command())?.patch !== undefined && (await db.calendarEvents.get('ev1'))!.start.time === '10:30')
    expect((await command())!.patch).toEqual({ start: at('10:30'), end: at('11:30'), allDay: false })
    await act(async () => { await flushEventEdits(Date.now() + 1000) })
    expect(requests.filter((q) => q.method === 'PATCH')).toHaveLength(1)
    await key(document.body, 'Delete')
    await waitFor(() => r.container.querySelector('[data-event="ev1b"], [data-event="ev1"]') === null)
    await waitFor(() => toastText() === 'Event deleted')
    await r.unmount()
  })

  it('Shift+Down moves the end and Alt+Shift+Up the start', async () => {
    const r = await rendered()
    await key(document.body, 'j')
    await key(document.body, 'ArrowDown', { shiftKey: true })
    await waitFor(async () => (await db.calendarEvents.get('ev1'))!.end.time === '11:15')
    await key(document.body, 'ArrowUp', { shiftKey: true, altKey: true })
    await waitFor(async () => (await db.calendarEvents.get('ev1'))!.start.time === '09:45')
    expect((await command())!.patch).toEqual({ start: at('09:45'), end: at('11:15'), allDay: false })
    await r.unmount()
  })

  it('a locked event is not selected by j and ignores the keys', async () => {
    await db.calendarEvents.delete('ev1')
    const r = await render(<CalendarView mode="day" date={DAY} />)
    await waitFor(() => r.container.querySelector('[data-event="ev2"]') !== null)
    layout()
    const locked = r.container.querySelector<HTMLButtonElement>('[data-event="ev2"]')!
    locked.focus()
    await key(locked, 'j')
    await key(locked, 'ArrowDown')
    await key(locked, 'Delete')
    expect(locked.className).not.toContain('ring-accent')
    expect(await db.eventOutbox.count()).toBe(0)
    expect((await db.calendarEvents.get('ev2'))!.start.time).toBe('13:00')
    expect(locked.tabIndex).toBe(0)
    await r.unmount()
  })
})

// ---- repeating events (OpenSpec change calendar-event-series, tasks 5.1 and 5.4) ----
const scopeItems = () => [...document.querySelectorAll<HTMLButtonElement>('[data-event-scope]')]
const chooseScope = async (scope: string) => { await waitFor(() => scopeItems().length > 0, 'the scope menu'); await act(async () => scopeItems().find((i) => i.getAttribute('data-event-scope') === scope)!.click()) }
const routineCommand = () => db.eventOutbox.get('c1:ev3')

describe('repeating events: the scope menu', () => {
  beforeEach(async () => { await db.calendarEvents.put(routine) })

  it('a drag of an occurrence shows the block moved and the menu; nothing is sent until a scope is picked', async () => {
    const r = await rendered()
    const block = r.container.querySelector('[data-event="ev3"]')!
    expect(block.hasAttribute('data-editable')).toBe(true)
    await drag(block, y(7 * 60 + 45), y(9 * 60 + 45)) // grabbed 45 minutes below the top, dropped at 09:45: the block starts at 09:00
    await waitFor(() => scopeItems().length === 3, 'the menu')
    await waitFor(async () => (await db.calendarEvents.get('ev3'))!.start.time === '09:00', 'the block shows the change')
    expect(await routineCommand()).toBeUndefined()
    expect(requests.filter((q) => q.method === 'PATCH')).toEqual([])
    await waitFor(() => r.container.querySelector('[data-event="ev3"]')!.getAttribute('aria-busy') === 'true', 'the saving state')
    await chooseScope('this')
    await waitFor(async () => (await routineCommand()) !== undefined)
    expect(await routineCommand()).toMatchObject({ scope: 'this', patch: { start: { date: DAY, time: '09:00' }, end: { date: DAY, time: '10:30' }, allDay: false } })
    expect((await db.calendarEvents.get('ev3'))!.start.time).toBe('09:00')
    await waitFor(() => toastText() === 'Event moved')
    expect(document.querySelector('[data-test-undo]')).not.toBeNull()
    await r.unmount()
  })

  it('Escape puts the block back and sends nothing', async () => {
    const r = await rendered()
    await drag(r.container.querySelector('[data-event="ev3"]')!, y(7 * 60 + 45), y(9 * 60 + 45))
    await waitFor(() => scopeItems().length === 3, 'the menu')
    await key(document.activeElement!, 'Escape')
    await waitFor(() => scopeItems().length === 0, 'the menu closed')
    await waitFor(async () => (await db.calendarEvents.get('ev3'))!.start.time === '07:00', 'the block back')
    expect(await db.eventOutbox.count()).toBe(0)
    await act(async () => { await flushEventEdits(Date.now() + 1000) })
    expect(requests.filter((q) => q.method === 'PATCH')).toEqual([])
    await r.unmount()
  })

  it('All events queues an all command with an Undo; This and following events has none', async () => {
    const r = await rendered()
    await drag(r.container.querySelector('[data-event="ev3"]')!, y(7 * 60 + 45), y(9 * 60 + 45))
    await chooseScope('all')
    await waitFor(async () => (await routineCommand())?.scope === 'all')
    await waitFor(() => toastText() === 'Event moved')
    expect(document.querySelector('[data-test-undo]')).not.toBeNull()
    await r.unmount()
  })

  it('This and following events has no Undo: it split the series', async () => {
    const r = await rendered()
    await drag(r.container.querySelector('[data-event="ev3"]')!, y(7 * 60 + 45), y(9 * 60 + 45))
    await chooseScope('following')
    await waitFor(async () => (await routineCommand())?.scope === 'following')
    await waitFor(() => toastText() === 'Event moved')
    expect(document.querySelector('[data-test-undo]')).toBeNull()
    await r.unmount()
  })

  it('a single event never asks', async () => {
    const r = await rendered()
    await drag(r.container.querySelector('[data-event="ev1"]')!, y(10 * 60 + 30), y(14 * 60 + 30))
    await waitFor(async () => (await command()) !== undefined)
    expect(scopeItems()).toHaveLength(0)
    expect((await command())!.scope).toBeUndefined()
    await r.unmount()
  })

  it('hides "This and following events" on the first occurrence', async () => {
    await db.calendarEvents.put({ ...routine, series: { ...routine.series!, first: true } })
    const r = await rendered()
    await drag(r.container.querySelector('[data-event="ev3"]')!, y(7 * 60 + 45), y(9 * 60 + 45))
    await waitFor(() => scopeItems().length > 0)
    expect(scopeItems().map((i) => i.getAttribute('data-event-scope'))).toEqual(['this', 'all'])
    await key(document.activeElement!, 'Escape')
    await r.unmount()
  })

  it('the keyboard path: j, Down twice, the keys settle, the menu opens with focus on the first item, Enter picks', async () => {
    await db.calendarEvents.delete('ev1')
    const r = await render(<><CalendarView mode="day" date={DAY} /><Toast /></>)
    await waitFor(() => r.container.querySelector('[data-event="ev3"]') !== null)
    layout()
    await key(document.body, 'j')
    await waitFor(() => r.container.querySelector('[data-event="ev3"]')!.className.includes('ring-accent'), 'the selection')
    await key(document.body, 'ArrowDown')
    await key(document.body, 'ArrowDown')
    await waitFor(async () => (await db.calendarEvents.get('ev3'))!.start.time === '07:30', 'the block moved by the keys')
    await waitFor(() => scopeItems().length === 3, 'the menu after the keys settle')
    await waitFor(() => document.activeElement === scopeItems()[0], 'focus on the first item')
    await act(async () => (document.activeElement as HTMLElement).click()) // Enter on a button is its click
    await waitFor(async () => (await routineCommand()) !== undefined)
    expect(await routineCommand()).toMatchObject({ scope: 'this', patch: { start: { date: DAY, time: '07:30' }, end: { date: DAY, time: '09:00' }, allDay: false } })
    await r.unmount()
  })

  it('Delete on an occurrence asks, and the chosen scope is queued with the 8 second delay', async () => {
    await db.calendarEvents.delete('ev1')
    const r = await render(<><CalendarView mode="day" date={DAY} /><Toast /></>)
    await waitFor(() => r.container.querySelector('[data-event="ev3"]') !== null)
    layout()
    await key(document.body, 'j')
    await key(document.body, 'Delete')
    await waitFor(() => scopeItems().length === 3, 'the menu')
    expect(document.querySelector('[role="menu"]')!.getAttribute('aria-label')).toBe('Delete which events')
    expect(r.container.querySelector('[data-event="ev3"]')).not.toBeNull() // still drawn until a scope is picked
    await chooseScope('all')
    await waitFor(() => r.container.querySelector('[data-event="ev3"]') === null)
    expect(await routineCommand()).toMatchObject({ scope: 'all', patch: { delete: true } })
    expect((await routineCommand())!.notBefore).toBeGreaterThan(Date.now() + 7000)
    await waitFor(() => toastText() === 'Event deleted')
    await r.unmount()
  })

  it('the popover Save on an occurrence asks and then sends the chosen scope', async () => {
    const r = await rendered()
    await click(r.container.querySelector('[data-event="ev3"]')!)
    await waitFor(() => document.querySelector('[data-event-edit]') !== null)
    await type(document.querySelector<HTMLInputElement>('[data-event-edit-title]')!, 'Routine B')
    await click(document.querySelector('[data-event-edit-save]')!)
    await chooseScope('all')
    await waitFor(async () => (await routineCommand()) !== undefined)
    expect(await routineCommand()).toMatchObject({ scope: 'all', patch: { summary: 'Routine B' } })
    await waitFor(() => toastText() === 'Event renamed')
    await r.unmount()
  })

  it('shows the repeat in the popover', async () => {
    const r = await rendered()
    await click(r.container.querySelector('[data-event="ev3"]')!)
    await waitFor(() => document.querySelector('[data-event-details]') !== null)
    expect(document.querySelector('[data-event-repeat]')!.textContent).toBe('Every day')
    await r.unmount()
  })
})
