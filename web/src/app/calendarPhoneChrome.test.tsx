// @vitest-environment happy-dom
// The calendar's chrome on a phone (OpenSpec changes phone-calendar-chrome and
// calendar-header-cleanup): the row under the bar holds the view dropdown, Today and the
// arrows, at every width; Show other calendars on this device is an item of the page's menu.
import 'fake-indexeddb/auto'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { db, setMeta } from '../lib/db'
import { resetDeviceId } from '../lib/ops'
import { PageActions, usePageBar } from '../ui'
import { click, render, waitFor } from '../ui/test-utils'
import { CalendarView } from './CalendarView'
import { MonthView } from './MonthView'
import { resetPanel } from './panel'

const original = window.matchMedia
/** Below 768 px: no `min-width` query matches. */
function phone() {
  window.matchMedia = ((q: string) => ({
    matches: !q.includes('min-width'), media: q, onchange: null,
    addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {}, dispatchEvent: () => false,
  })) as typeof window.matchMedia
}

/** The shell's phone bar, as far as the page's menu goes. */
function Bar() {
  const bar = usePageBar()
  return bar ? <div data-phone-bar><PageActions overflow={bar.overflow} /></div> : null
}

const items = () => [...document.querySelectorAll<HTMLElement>('[role="menu"] [role^="menuitem"]')]
const names = (root: Element) => [...root.querySelectorAll('button')].map((b) => b.getAttribute('aria-label') ?? b.textContent)

beforeEach(async () => {
  vi.useFakeTimers({ toFake: ['Date'] })
  // Wednesday 14 October 2026.
  vi.setSystemTime(new Date(2026, 9, 14, 12, 0))
  await db.delete()
  await db.open()
  resetDeviceId()
  resetPanel()
  localStorage.clear()
  history.replaceState(null, '', '/week')
  vi.stubGlobal('fetch', vi.fn(async () => new Response('{}', { status: 404 })))
})
afterEach(() => {
  window.matchMedia = original
  vi.useRealTimers()
  vi.unstubAllGlobals()
  resetPanel()
})

describe('the calendar on a phone', () => {
  it('has one row under the bar: the view dropdown, Today and the arrows', async () => {
    phone()
    const r = await render(<CalendarView mode="week" date="2026-10-14" />)
    const row = r.container.querySelector('[data-page-header] [data-page-actions]')!
    expect(names(row)).toEqual(['Calendar range Week', 'Today', 'Previous week', 'Next week'])
    expect(row.querySelector('[role="group"][aria-label="Calendar range"]')).toBeNull()

    const view = row.querySelector<HTMLElement>('[data-calendar-range]')!
    expect(view.getAttribute('aria-haspopup')).toBe('menu')
    expect(view.textContent).toBe('Calendar range Week')
    await click(view)
    await waitFor(() => items().length === 6, 'the five views and Number of days')
    expect(items().map((i) => `${i.textContent}:${i.getAttribute('aria-checked')}`))
      .toEqual(['Day:false', 'Week:true', 'Month:false', 'Plan:false', 'Year:false', 'Number of days:null'])
    // Choosing a view keeps the anchor date.
    await click(items()[2])
    expect(location.pathname).toBe('/month/2026-10-14')
    await r.unmount()
  })

  it('moves in time from the same row', async () => {
    phone()
    const r = await render(<MonthView date="2026-11-18" />)
    const row = r.container.querySelector('[data-page-header] [data-page-actions]')!
    expect(names(row)).toEqual(['Calendar range Month', 'Today', 'Previous month', 'Next month'])
    await click(row.querySelector('button[aria-label="Next month"]')!)
    expect(location.pathname).toBe('/month/2026-12-18')
    await click(row.querySelector('button[aria-label="Today"]')!)
    expect(location.pathname).toBe('/month')
    await r.unmount()
  })

  it('keeps Show other calendars on this device in the page’s menu', async () => {
    phone()
    await db.calendarSubscriptions.put({ id: 'sub1', name: 'Work', color: '#2563eb', enabled: true, lastSuccessAt: Date.now() })
    await db.calendarEvents.put({ id: 'ev1', subscriptionId: 'sub1', uid: 'u1', summary: 'Team sync', location: '', start: { date: '2026-10-14', time: '10:00' }, end: { date: '2026-10-14', time: '11:00' }, allDay: false, free: false })
    await setMeta('calendar.events.window', { from: '2026-09-01', to: '2026-11-30', fetchedAt: Date.now() })
    const r = await render(<><Bar /><CalendarView mode="day" date="2026-10-14" /></>)
    await waitFor(() => r.container.querySelector('[data-event="ev1"]') !== null, 'the event')
    expect(r.container.querySelector('[data-page-header] [data-calendars-toggle]')).toBeNull()
    await click(r.container.querySelector('[data-phone-bar] button[aria-label="More actions"]')!)
    await waitFor(() => document.querySelector('[role="menu"] [data-calendars-toggle]') !== null, 'the menu item')
    const item = document.querySelector<HTMLElement>('[role="menu"] [data-calendars-toggle]')!
    expect(item.textContent).toBe('Show other calendars on this device')
    expect(item.getAttribute('aria-checked')).toBe('true')
    await click(item)
    await waitFor(() => r.container.querySelector('[data-event="ev1"]') === null, 'the event hidden')
    await r.unmount()
  })

  it('is the same dropdown from 768 px, not a switch of five', async () => {
    const r = await render(<CalendarView mode="week" date="2026-10-14" />)
    const header = r.container.querySelector('[data-page-header]')!
    expect(header.querySelector('[role="group"][aria-label="Calendar range"]')).toBeNull()
    expect(header.querySelector('[data-calendar-range]')!.textContent).toBe('Calendar range Week')
    await r.unmount()
  })
})
