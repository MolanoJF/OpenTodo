// @vitest-environment happy-dom
import type { ReactNode } from 'react'
import { EventPageTestRoute } from '../eventPage.testkit'
import 'fake-indexeddb/auto'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { db, setMeta } from '../../lib/db'
import { createTask, resetDeviceId } from '../../lib/ops'
import { click, render as renderBase, waitFor } from '../../ui/test-utils'
import { CalendarView } from '../CalendarView'
import { closeEventEditor } from './eventEditorStore'
import { taskPreview } from './taskPreviewStore'
import * as trail from '../taskTrail'

const render = (ui: ReactNode) => renderBase(<>{ui}<EventPageTestRoute /></>)
const date = '2026-11-13'
beforeEach(async () => {
  await db.delete(); await db.open(); resetDeviceId(); closeEventEditor(); taskPreview.set(null)
  vi.stubGlobal('fetch', vi.fn(async () => new Response('{}', { status: 404 })))
  await setMeta('calendar.events.window', { from: '2026-01-01', to: '2026-12-31', fetchedAt: Date.now() })
})
afterEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals(); closeEventEditor(); taskPreview.set(null) })

it('single-click previews task facts without writing and offers the full task page', async () => {
  const id = await createTask({ title: 'Car shop', description: 'Check the brake pads', due: { date, time: '11:15' }, estimate_minutes: 45 })
  const writes = await db.outbox.count()
  const open = vi.spyOn(trail, 'openTaskPage').mockImplementation(() => {})
  const r = await render(<CalendarView mode="day" date={date} />)
  await waitFor(() => !!r.container.querySelector(`[data-cal-id="${id}"]`))
  await click(r.container.querySelector(`[data-cal-id="${id}"]`)!)
  await waitFor(() => !!document.querySelector('[data-task-preview]'))
  expect(document.querySelector('[data-task-preview]')?.textContent).toContain('Check the brake pads')
  expect(document.querySelector('[data-task-preview]')?.textContent).toContain('45m')
  expect(document.querySelector('[data-task-preview] input')).toBeNull()
  expect(document.querySelector('[data-task-preview-open]')?.getAttribute('aria-describedby')).toBeNull()
  expect(document.querySelector('[role="tooltip"]')).toBeNull()
  expect(await db.outbox.count()).toBe(writes)
  await click(document.querySelector('[data-task-preview-open]')!)
  expect(open).toHaveBeenCalledWith(id)
})

it('previews event descriptions and reminders before opening a full event page', async () => {
  const event = { id: 'event1', uid: 'u1', subscriptionId: 'sub1', summary: 'Design review', location: 'Studio', start: { date, time: '10:00' }, end: { date, time: '11:00' }, allDay: false, free: false, rev: 'r1', access: { update: true, delete: true } }
  await db.calendarSubscriptions.put({ id: 'sub1', name: 'Work', connectionId: 'work', color: '#3b82f6', enabled: true, lastSuccessAt: 1 })
  await db.calendarEvents.put(event)
  vi.stubGlobal('fetch', vi.fn(async (url: string) => new Response(JSON.stringify(url.includes('/events/') ? { event, description: 'Bring the mockups', descriptionEditable: true, reminders: [30] } : {}), { status: url.includes('/events/') ? 200 : 404, headers: { 'Content-Type': 'application/json' } })))
  const r = await render(<CalendarView mode="day" date={date} />)
  await waitFor(() => !!r.container.querySelector('[data-event="event1"]'))
  await click(r.container.querySelector('[data-event="event1"]')!)
  await waitFor(() => document.querySelector('[data-event-details]')?.textContent?.includes('Bring the mockups') === true)
  expect(document.querySelector('[data-event-details]')?.textContent).toContain('30 minutes before')
  expect(document.querySelector('[data-event-edit-title]')).toBeNull()
  expect(document.querySelector('[data-event-open-editor]')?.getAttribute('aria-describedby')).toBeNull()
  expect(document.querySelector('[role="tooltip"]')).toBeNull()
  await click(document.querySelector('[data-event-open-editor]')!)
  await waitFor(() => !!document.querySelector('[data-event-page]'))
  expect(document.querySelector('[data-event-page] [data-event-content]')).toBeTruthy()
  expect(document.querySelector('[data-event-page] [data-event-facts]')).toBeTruthy()
})
