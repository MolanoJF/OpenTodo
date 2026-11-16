// @vitest-environment happy-dom
import 'fake-indexeddb/auto'
import { act } from 'react'
import { beforeEach, afterEach, expect, it, vi } from 'vitest'
import { db } from '../../lib/db'
import { resetDeviceId } from '../../lib/ops'
import { DEFAULT_GRID_PREFS, normalizeGridPrefs } from '../../lib/calendar'
import { USER_PREFS_ID } from '../../lib/userPrefs'
import { click, render, type, waitFor } from '../../ui/test-utils'
import { CalendarSettingsView } from '../CalendarSettings'
import { navigate, parseRoute, routePath, setLeaveGuard } from '../router'
import { EventPageTestRoute } from '../eventPage.testkit'
import { closeEventEditor, openEventEditor } from './eventEditorStore'

const event = { id: 'ev-polish', subscriptionId: 'sub', uid: 'u1', summary: 'Review', location: '', start: { date: '2026-11-13', time: '10:00' }, end: { date: '2026-11-13', time: '11:00' }, allDay: false, free: false, access: { update: true, delete: true }, rev: 'r1' }
beforeEach(async () => {
  setLeaveGuard(null); closeEventEditor(); navigate({ view: 'week', date: '2026-11-13' })
  await db.delete(); await db.open(); resetDeviceId()
  await db.calendarSubscriptions.put({ id: 'sub', name: 'Work', color: '#3b82f6', enabled: true, lastSuccessAt: 1, connectionId: 'c1' })
  await db.calendarEvents.put(event)
  vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({ event, description: 'Agenda', descriptionEditable: true }), { status: 200, headers: { 'Content-Type': 'application/json' } })))
})
afterEach(() => { setLeaveGuard(null); closeEventEditor(); vi.unstubAllGlobals() })

it('round-trips event routes including direct links and new events', () => {
  expect(parseRoute('/event/new')).toEqual({ view: 'event' })
  expect(parseRoute(routePath({ view: 'event', id: 'an event' }))).toEqual({ view: 'event', id: 'an event' })
})

it('navigates to an event page without a modal and guards dirty navigation', async () => {
  await render(<EventPageTestRoute />)
  openEventEditor({ mode: 'edit', eventId: event.id })
  await waitFor(() => !!document.querySelector('[data-event-page-route]'))
  expect(location.pathname).toBe('/event/ev-polish')
  expect(document.querySelector('[data-overlay="dialog"]')).toBeNull()
  await waitFor(() => (document.querySelector('[data-event-form-description]') as HTMLTextAreaElement)?.value === 'Agenda')
  await type(document.querySelector('[data-event-form-title]')!, 'Changed review')
  navigate({ view: 'settings', section: 'language' })
  await waitFor(() => document.body.textContent?.includes('Discard changes?') === true)
  expect(location.pathname).toBe('/event/ev-polish')
  await click([...document.querySelectorAll('button')].find((b) => b.textContent === 'Keep editing')!)
  expect(location.pathname).toBe('/event/ev-polish')
  navigate({ view: 'settings', section: 'language' })
  await waitFor(() => document.body.textContent?.includes('Discard changes?') === true)
  await click([...document.querySelectorAll('button')].find((b) => b.textContent === 'Discard')!)
  await waitFor(() => location.pathname === '/settings/language')
})

it('exposes opt-in off-hours shading and writes week-start through existing user prefs', async () => {
  expect(normalizeGridPrefs({}).shadeOffHours).toBeUndefined()
  expect(normalizeGridPrefs({ shadeOffHours: true }).shadeOffHours).toBe(true)
  const save = vi.fn()
  const r = await render(<CalendarSettingsView prefs={DEFAULT_GRID_PREFS} onSave={save} />)
  await click(r.container.querySelector('[role="switch"]')!)
  expect(save).toHaveBeenCalledWith({ shadeOffHours: true })
  await act(async () => { const select = r.container.querySelector<HTMLSelectElement>('#calendar-week-start')!; select.value = 'sun'; select.dispatchEvent(new Event('change', { bubbles: true })) })
  await waitFor(async () => (await db.userPrefs.get(USER_PREFS_ID))?.week_start === 'sun')
})
