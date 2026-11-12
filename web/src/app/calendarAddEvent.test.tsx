// @vitest-environment happy-dom
// Adding an event from the Day and Week grid (OpenSpec change calendar-event-editor, task 5.3): the Task | Event
// choice only with a writable calendar, the remembered kind for the double-click, `q` and the sweep, the menu's two
// items, Shift+Q, and no change at all without a writable calendar.
import 'fake-indexeddb/auto'
import { act } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { db, setMeta } from '../lib/db'
import { createTask, resetDeviceId } from '../lib/ops'
import type { CalendarSubscription } from '../lib/types'
import { click, key, render, type, waitFor } from '../ui/test-utils'
import { CalendarView } from './CalendarView'
import { PX_PER_MIN } from './calendar/grid'
import { closeEventEditor, currentEventEditor } from './calendar/eventEditorStore'
import { resetPanel } from './panel'

const writable: CalendarSubscription = { id: 'w1', name: 'Work', color: '#2563eb', enabled: true, lastSuccessAt: Date.now(), managed: true, connectionId: 'c1', calendarUrl: 'https://dav/work/', editEvents: true, caps: { create: true, update: true, delete: true } }

beforeEach(async () => {
  vi.useFakeTimers({ toFake: ['Date'] })
  // Wednesday 18 November 2026; the week runs Monday 16 to Sunday 22.
  vi.setSystemTime(new Date(2026, 10, 18, 12, 0))
  await db.delete()
  await db.open()
  resetDeviceId()
  resetPanel()
  closeEventEditor()
  localStorage.clear()
  vi.stubGlobal('fetch', vi.fn(async () => new Response('{}', { status: 404 })))
  await setMeta('calendar.events.window', { from: '2026-10-01', to: '2026-12-30', fetchedAt: Date.now() })
})
afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
  resetPanel()
  closeEventEditor()
})

const column = (root: ParentNode, d: string) => root.querySelector<HTMLElement>(`[data-drop-day="${d}"]`)!
const at = (minutes: number) => ({ bubbles: true, cancelable: true, clientX: 10, clientY: minutes * PX_PER_MIN + 1 })
const menuItem = (name: string) => [...document.querySelectorAll<HTMLElement>('[role="menuitem"]')].find((b) => b.textContent?.includes(name))
const dialog = () => document.querySelector('[role="dialog"]')

async function dblclick(el: Element, init: MouseEventInit) { await act(async () => { el.dispatchEvent(new MouseEvent('dblclick', init)) }) }
async function rightClick(el: Element, init: MouseEventInit) { await act(async () => { el.dispatchEvent(new MouseEvent('contextmenu', init)) }) }
async function openGrid(withCalendar = true) {
  if (withCalendar) await db.calendarSubscriptions.put(writable)
  const r = await render(<CalendarView mode="week" />)
  await waitFor(() => !!column(r.container, '2026-11-17'), 'the grid')
  // The live query behind the Event choice settles: the menu offers Add event here….
  if (withCalendar) await waitFor(async () => { await rightClick(column(r.container, '2026-11-17'), at(9 * 60)); const ok = !!menuItem('Add event here'); await key(document, 'Escape'); return ok }, 'the Event choice')
  return r
}
/** A sweep with a mouse: press at `from` minutes, move past the threshold, then to `to`, release (or Escape instead). */
async function sweep(el: Element, from: number, to: number, opts: { pointerType?: string; escape?: boolean } = {}) {
  const init = { bubbles: true, button: 0, pointerId: 1, pointerType: opts.pointerType ?? 'mouse', clientX: 50 }
  const y = (m: number) => m * PX_PER_MIN + 1
  await act(async () => { el.dispatchEvent(new PointerEvent('pointerdown', { ...init, clientY: y(from) })) })
  await act(async () => { window.dispatchEvent(new PointerEvent('pointermove', { ...init, clientY: y(from) + 8 })) })
  await act(async () => { window.dispatchEvent(new PointerEvent('pointermove', { ...init, clientY: to * PX_PER_MIN })) })
  if (opts.escape) await key(document, 'Escape')
  await act(async () => { window.dispatchEvent(new PointerEvent('pointerup', { ...init, clientY: to * PX_PER_MIN })) })
}

describe('without a writable calendar nothing changes', () => {
  it('the menu holds only the task, frame and slot entries; the dialog has no control; a sweep opens nothing', async () => {
    const r = await openGrid(false)
    await rightClick(column(r.container, '2026-11-17'), at(9 * 60))
    await waitFor(() => !!menuItem('Add task here'), 'the menu')
    expect(menuItem('Add event here')).toBeUndefined()
    await key(document, 'Escape')
    await dblclick(column(r.container, '2026-11-17'), at(14 * 60))
    await waitFor(() => !!dialog(), 'the add dialog')
    expect(document.querySelector('[data-add-kind]')).toBeNull()
    await key(document, 'Escape')
    await sweep(column(r.container, '2026-11-17'), 14 * 60, 15 * 60 + 30)
    expect(dialog()).toBeNull()
    expect(currentEventEditor()).toBeNull()
  })

  it('Shift+Q does nothing', async () => {
    await openGrid(false)
    await key(document, 'Q', { shiftKey: true })
    expect(currentEventEditor()).toBeNull()
  })
})

describe('with a writable calendar', () => {
  it('the add dialog shows Task | Event with Task chosen the first time, and Event opens the editor there and is remembered', async () => {
    const r = await openGrid()
    await dblclick(column(r.container, '2026-11-17'), at(14 * 60 + 30))
    await waitFor(() => !!document.querySelector('[data-add-kind]'), 'the control')
    expect(document.querySelector('[data-add-kind] [aria-checked="true"]')!.textContent).toBe('Task')
    await click([...document.querySelectorAll<HTMLElement>('[data-add-kind] [role="radio"]')].find((b) => b.textContent === 'Event')!)
    expect(currentEventEditor()).toMatchObject({ mode: 'create', place: { date: '2026-11-17', start: 14 * 60 + 30 } })
    expect(dialog()?.getAttribute('aria-label') ?? '').not.toContain('Add a task')
    // Remembered: the next double-click opens the editor.
    closeEventEditor()
    // Once the remembered kind reaches the grid a double-click opens the editor (until then it opens the dialog).
    await waitFor(async () => { await dblclick(column(r.container, '2026-11-18'), at(9 * 60)); if (currentEventEditor()) return true; await key(document, 'Escape'); return false }, 'the editor')
    expect(currentEventEditor()).toMatchObject({ place: { date: '2026-11-18', start: 9 * 60 } })
  })

  it('the menu lists both items, the remembered kind first; Add event here… opens the editor at that slot', async () => {
    const r = await openGrid()
    await rightClick(column(r.container, '2026-11-18'), at(9 * 60))
    await waitFor(() => !!menuItem('Add event here'), 'the menu')
    const items = [...document.querySelectorAll<HTMLElement>('[data-add-here]')].map((b) => b.dataset.addHere)
    expect(items.slice(0, 2)).toEqual(['task', 'event'])
    await click(menuItem('Add event here')!)
    expect(currentEventEditor()).toMatchObject({ mode: 'create', place: { date: '2026-11-18', start: 9 * 60 } })
    // The choice was remembered: the event now comes first.
    closeEventEditor()
    await rightClick(column(r.container, '2026-11-18'), at(10 * 60))
    await waitFor(() => !!menuItem('Add event here'), 'the menu again')
    await waitFor(() => document.querySelector<HTMLElement>('[data-add-here]')?.dataset.addHere === 'event', 'the event first')
  })

  it('`q` opens the remembered kind on the focused day, and Shift+Q the event editor at 09:00 of another day', async () => {
    await openGrid()
    await key(document, 'q')
    await waitFor(() => !!dialog(), 'the add dialog')
    await key(document, 'Escape')
    await setMeta('calendar.newKind', 'event')
    await key(document, 'Q', { shiftKey: true })
    await waitFor(() => currentEventEditor()?.mode === 'create', 'the editor')
    // The week shows the 18th (today) as the focused day: it is 12:00 sharp, a free half hour.
    expect(currentEventEditor()).toMatchObject({ place: { date: '2026-11-18', start: 12 * 60, end: 12 * 60 + 30 } })
  })

  it('a sweep from 14:00 to 15:30 opens the editor with those times when the remembered kind is Event', async () => {
    await setMeta('calendar.newKind', 'event')
    const r = await openGrid()
    await sweep(column(r.container, '2026-11-17'), 14 * 60, 15 * 60 + 30)
    await waitFor(() => currentEventEditor()?.mode === 'create', 'the editor')
    expect(currentEventEditor()).toMatchObject({ place: { date: '2026-11-17', start: 14 * 60, end: 15 * 60 + 30 } })
  })

  it('a sweep with the kind Task creates a task due then with a 90-minute estimate', async () => {
    const r = await openGrid()
    await sweep(column(r.container, '2026-11-17'), 14 * 60, 15 * 60 + 30)
    await waitFor(() => !!document.querySelector('[role="dialog"] textarea'), 'the add dialog')
    const input = document.querySelector<HTMLTextAreaElement>('[role="dialog"] textarea')!
    await type(input, 'Write the report')
    await act(async () => { input.form!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })) })
    await waitFor(async () => (await db.tasks.toArray()).some((t) => t.title === 'Write the report'), 'the task')
    const task = (await db.tasks.toArray()).find((t) => t.title === 'Write the report')!
    expect(task.due).toMatchObject({ date: '2026-11-17', time: '14:00' })
    expect(task.estimate_minutes).toBe(90)
  })

  it('Escape cancels the sweep: nothing opens', async () => {
    await setMeta('calendar.newKind', 'event')
    const r = await openGrid()
    await sweep(column(r.container, '2026-11-17'), 14 * 60, 15 * 60 + 30, { escape: true })
    expect(currentEventEditor()).toBeNull()
    expect(dialog()).toBeNull()
  })

  it('touch does not sweep, and a plain click or a double-click without moving is not a sweep', async () => {
    await setMeta('calendar.newKind', 'event')
    const r = await openGrid()
    await sweep(column(r.container, '2026-11-17'), 14 * 60, 15 * 60 + 30, { pointerType: 'touch' })
    expect(currentEventEditor()).toBeNull()
    const init = { bubbles: true, button: 0, pointerId: 2, pointerType: 'mouse', clientX: 50, clientY: 14 * 60 * PX_PER_MIN + 1 }
    await act(async () => { column(r.container, '2026-11-17').dispatchEvent(new PointerEvent('pointerdown', init)) })
    await act(async () => { window.dispatchEvent(new PointerEvent('pointerup', init)) })
    expect(currentEventEditor()).toBeNull()
  })

  it('a task block is not an empty place: a sweep does not start on it', async () => {
    await createTask({ title: 'Existing', due: { date: '2026-11-17', time: '14:00' } })
    await setMeta('calendar.newKind', 'event')
    const r = await openGrid()
    await waitFor(() => !!r.container.querySelector('[data-cal-id]'), 'the block')
    const block = r.container.querySelector('[data-cal-id]')!
    await sweep(block, 14 * 60, 15 * 60)
    expect(currentEventEditor()).toBeNull()
  })
})
