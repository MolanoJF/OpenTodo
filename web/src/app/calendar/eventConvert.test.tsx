// @vitest-environment happy-dom
// Convert an event to a task (OpenSpec change calendar-event-editor, task 5.2): the task's fields, the delete
// switch and its default, one toast whose Undo restores the task and cancels the delete.
import 'fake-indexeddb/auto'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { DELETE_DELAY_MS, flushEventEdits } from '../../lib/calendarEventEdits'
import { db } from '../../lib/db'
import { eventFullOf } from '../../lib/eventFull'
import { resetDeviceId } from '../../lib/ops'
import type { CalendarEvent, CalendarSubscription } from '../../lib/types'
import { click, render, waitFor } from '../../ui/test-utils'
import { hideBulkToast, bulkToast } from '../bulk/toastStore'
import { ConvertToTask } from './ConvertToTask'
import { convertEvent, taskInputOf } from './eventConvert'

const BERLIN = 'Europe/Berlin'
const sub: CalendarSubscription = { id: 's1', name: 'Work', color: '', enabled: true, lastSuccessAt: 1, managed: true, connectionId: 'c1', editEvents: true }
const review: CalendarEvent = { id: 'ev1', subscriptionId: 's1', uid: 'u1', summary: 'Review draft', location: 'Room 4', start: { date: '2026-11-13', time: '14:00', tz: BERLIN }, end: { date: '2026-11-13', time: '15:30', tz: BERLIN }, allDay: false, free: false, rev: 'r1', access: { update: true, delete: true } }
const locked: CalendarEvent = { ...review, id: 'ev2', uid: 'u2', access: { update: false, delete: false, reason: 'recurring' } }

let deletes: string[]
beforeEach(async () => {
  await db.delete()
  await db.open()
  resetDeviceId()
  localStorage.clear()
  deletes = []
  vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
    if (init?.method === 'DELETE') { deletes.push(url); return new Response(null, { status: 204 }) }
    return new Response('{}', { status: 404 })
  }))
  await db.calendarSubscriptions.put(sub)
  await db.calendarEvents.bulkPut([review, locked])
})
afterEach(() => { vi.unstubAllGlobals(); hideBulkToast() })

const full = (ev: CalendarEvent, description = 'Check the numbers') => eventFullOf(ev, BERLIN, description)
const tasks = async () => (await db.tasks.toArray()).filter((t) => !t.deleted_at)

describe('taskInputOf (D11)', () => {
  it('a timed event: title, description then the place, due with time and zone, the length as the estimate', () => {
    expect(taskInputOf(full(review))).toEqual({
      title: 'Review draft', description: 'Check the numbers\n\nLocation: Room 4',
      due: { date: '2026-11-13', time: '14:00', tz: BERLIN }, estimate_minutes: 90,
    })
  })

  it('without a description the place alone is the notes; without either there are none', () => {
    expect(taskInputOf(full(review, '')).description).toBe('Location: Room 4')
    expect(taskInputOf(full({ ...review, location: '' }, '')).description).toBeUndefined()
  })

  it('caps the estimate to the field\'s limit', () => {
    const long = { ...review, end: { date: '2026-11-30', time: '15:30', tz: BERLIN } }
    expect(taskInputOf(full(long)).estimate_minutes).toBe(10080)
  })

  it('an all-day event is due on its last day, and a multi-day one starts a span on its first', () => {
    const one = { ...review, allDay: true, start: { date: '2026-11-16' }, end: { date: '2026-11-17' } }
    expect(taskInputOf(full(one))).toMatchObject({ due: { date: '2026-11-16' } })
    expect(taskInputOf(full(one)).span).toBeUndefined()
    const many = { ...review, allDay: true, start: { date: '2026-11-16' }, end: { date: '2026-11-19' } }
    expect(taskInputOf(full(many))).toMatchObject({ due: { date: '2026-11-18' }, start: '2026-11-16', span: true })
    expect(taskInputOf(full(many)).estimate_minutes).toBeUndefined()
  })
})

describe('convertEvent', () => {
  it('makes the task and deletes the event after the pause, with one toast', async () => {
    expect(await convertEvent(review, full(review), true)).toBe(true)
    const [task] = await tasks()
    expect(task).toMatchObject({ title: 'Review draft', description: 'Check the numbers\n\nLocation: Room 4', estimate_minutes: 90, project_id: null })
    expect(task.due).toEqual({ date: '2026-11-13', time: '14:00', tz: BERLIN })
    expect(await db.calendarEvents.get('ev1')).toBeUndefined() // gone from the grid at once
    expect(bulkToast()?.label).toBe('Task created')
    await flushEventEdits(Date.now() + DELETE_DELAY_MS + 100)
    expect(deletes).toHaveLength(1)
    expect(deletes[0]).toContain('/c1/events/ev1')
  })

  it('one Undo within the pause moves the task to the Trash and keeps the event; no delete is sent', async () => {
    await convertEvent(review, full(review), true)
    bulkToast()!.action!.run()
    await waitFor(async () => (await tasks()).length === 0 && !!(await db.calendarEvents.get('ev1')), 'the undo')
    expect(await db.eventOutbox.count()).toBe(0)
    await flushEventEdits(Date.now() + DELETE_DELAY_MS + 100)
    expect(deletes).toHaveLength(0)
  })

  it('with the switch off the event is untouched', async () => {
    await convertEvent(review, full(review), false)
    expect(await tasks()).toHaveLength(1)
    expect(await db.calendarEvents.get('ev1')).toEqual(review)
    expect(await db.eventOutbox.count()).toBe(0)
  })

  it('works offline: the task is made now and the delete waits in the outbox', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => { throw new TypeError('offline') }))
    await convertEvent(review, full(review), true)
    expect(await tasks()).toHaveLength(1)
    await flushEventEdits(Date.now() + DELETE_DELAY_MS + 100)
    expect(await db.eventOutbox.count()).toBe(1)
  })
})

describe('ConvertToTask', () => {
  it('the switch is on by default for an event that can be deleted, and Create task makes the task and closes', async () => {
    const onClose = vi.fn()
    await render(<ConvertToTask ev={review} calendar="Work" getFields={() => full(review)} onClose={onClose} />)
    const sw = document.querySelector<HTMLElement>('[role="switch"]')!
    expect(sw.getAttribute('aria-checked')).toBe('true')
    expect(document.body.textContent).toContain('Delete the event from Work')
    await click(document.querySelector('[data-convert-create]')!)
    await waitFor(() => onClose.mock.calls.length > 0, 'the close')
    expect(await tasks()).toHaveLength(1)
    expect(await db.calendarEvents.get('ev1')).toBeUndefined()
  })

  it('the switch is remembered per device', async () => {
    const first = await render(<ConvertToTask ev={review} getFields={() => full(review)} onClose={() => {}} />)
    await click(document.querySelector('[role="switch"]')!)
    await first.unmount()
    await render(<ConvertToTask ev={review} getFields={() => full(review)} onClose={() => {}} />)
    expect(document.querySelector('[role="switch"]')!.getAttribute('aria-checked')).toBe('false')
  })

  it('a locked event converts without the switch: off, disabled, the event kept', async () => {
    const onClose = vi.fn()
    await render(<ConvertToTask ev={locked} getFields={() => full(locked)} onClose={onClose} />)
    const sw = document.querySelector<HTMLButtonElement>('[role="switch"]')!
    expect(sw.getAttribute('aria-checked')).toBe('false')
    expect(sw.disabled).toBe(true)
    await click(document.querySelector('[data-convert-create]')!)
    await waitFor(() => onClose.mock.calls.length > 0, 'the close')
    expect(await tasks()).toHaveLength(1)
    expect(await db.calendarEvents.get('ev2')).toBeDefined()
  })
})
