// @vitest-environment happy-dom
import 'fake-indexeddb/auto'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { db } from '../lib/db'
import { calendarDayCount } from '../lib/calendarMode'
import { createTask, resetDeviceId } from '../lib/ops'
import { click, key, render, waitFor } from '../ui/test-utils'
import { CalendarView } from './CalendarView'
import { calendarControl, chooseCalendarMode } from './calendarMenu.testkit'
import { navigate, useRoute } from './router'
import { resetPanel } from './panel'

function Calendar() {
  const route = useRoute()
  return route.view === 'day' || route.view === 'week' ? <CalendarView mode={route.view} date={route.date} /> : null
}
beforeEach(async () => {
  vi.useFakeTimers({ toFake: ['Date'] }); vi.setSystemTime(new Date(2026, 10, 13, 12))
  await db.delete(); await db.open(); resetDeviceId(); resetPanel(); localStorage.clear()
  navigate({ view: 'week', date: '2026-11-13' })
  vi.stubGlobal('fetch', vi.fn(async () => new Response('{}', { status: 404 })))
})
afterEach(() => { vi.useRealTimers(); vi.unstubAllGlobals(); resetPanel() })

it('chooses a consecutive range by keyboard, pages by that range and restores normal Week', async () => {
  const r = await render(<Calendar />)
  await click(r.container.querySelector('[data-calendar-range]')!)
  const options = document.querySelector('[data-calendar-day-options]')!
  await key(options, 'ArrowRight')
  await waitFor(() => !!document.querySelector('[data-calendar-days="3"]'))
  await click(document.querySelector('[data-calendar-days="3"]')!)
  const dates = () => [...r.container.querySelectorAll<HTMLElement>('[data-drop-day]')].map((el) => el.dataset.dropDay)
  await waitFor(() => dates().length === 3)
  expect(dates()).toEqual(['2026-11-13', '2026-11-14', '2026-11-15'])
  expect(r.container.querySelector('[data-calendar-range]')?.textContent).toBe('3 days')
  expect(calendarDayCount.get()).toBe(3)
  expect(localStorage.getItem('ot.calendar.days')).toBe('3')
  await click(r.container.querySelector('[aria-label="Next 3 days"]')!)
  await waitFor(() => dates()[0] === '2026-11-16')
  expect(dates()).toEqual(['2026-11-16', '2026-11-17', '2026-11-18'])
  await click(r.container.querySelector('[aria-label="Previous 3 days"]')!)
  await waitFor(() => dates()[0] === '2026-11-13')
  await chooseCalendarMode(r.container, 'Week')
  await waitFor(() => dates().length === 7)
  expect(dates()[0]).toBe('2026-11-08')
  expect(r.container.querySelector('[data-page-title]')?.textContent).toBe('November 2026')
  expect(r.container.querySelector('[data-calendar-week]')?.textContent).toBe('Week 46')
  expect(r.container.querySelector('[data-calendar-today]')?.parentElement?.getAttribute('aria-label')).not.toBe('Range')
  await r.unmount()
})

it('keeps empty lanes quiet, hourly rules unshaded and real all-day tasks visible', async () => {
  const id = await createTask({ title: 'Car Shop', due: { date: '2026-11-12' } })
  const r = await render(<Calendar />)
  await waitFor(() => !!r.container.querySelector(`[data-lane-task="${id}"]`))
  const empty = r.container.querySelector<HTMLElement>('[data-day-header="2026-11-13"]')!
  expect(empty.textContent).not.toContain('Unscheduled')
  expect(empty.querySelector('[data-lane-empty]')?.textContent).toBe('')
  expect(empty.dataset.dropLane).toBe('2026-11-13')
  expect(r.container.querySelector('[data-capacity]')).toBeNull()
  const weekend = r.container.querySelector<HTMLElement>('[data-drop-day="2026-11-14"]')!
  expect(weekend.style.backgroundColor).toBe('')
  expect(weekend.style.backgroundSize).toBe('100% 48px')
  expect(weekend.style.backgroundImage.match(/linear-gradient/g)).toHaveLength(1)
  expect(r.container.querySelector('[data-calendar-completed-toggle]')).toBeNull()
  expect((await calendarControl(r.container, '[data-calendar-completed-toggle]')).getAttribute('role')).toBe('menuitemcheckbox')
  await r.unmount()
})
