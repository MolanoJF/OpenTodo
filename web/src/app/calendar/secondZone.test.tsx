// @vitest-environment happy-dom
// A second hour column in Day and Week (OpenSpec `calendar-time-zones`).
import 'fake-indexeddb/auto'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { GRID_PREFS_META_KEY } from '../../lib/calendar'
import { db, setMeta } from '../../lib/db'
import { resetDeviceId } from '../../lib/ops'
import { flush, render, waitFor } from '../../ui/test-utils'
import { CalendarView } from '../CalendarView'

const originalMedia = window.matchMedia
function viewport(width: number) {
  window.matchMedia = ((media: string) => ({
    matches: [...media.matchAll(/(min|max)-width:s*([d.]+)(px|rem)/g)].every((m) => {
      const px = Number(m[2]) * (m[3] === 'rem' ? 16 : 1)
      return m[1] === 'min' ? width >= px : width <= px
    }),
    media, onchange: null, addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {}, dispatchEvent: () => true,
  })) as typeof window.matchMedia
}

beforeEach(async () => {
  viewport(1280)
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(new Date(2026, 9, 14, 12, 0))
  await db.delete()
  await db.open()
  resetDeviceId()
  localStorage.clear()
  vi.stubGlobal('fetch', vi.fn(async () => new Response('{}', { status: 404 })))
})
afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
  window.matchMedia = originalMedia
})

const columns = (c: HTMLElement) => c.querySelectorAll('[data-calendar-grid] > [aria-hidden="true"]')

async function open(mode: 'day' | 'week', secondZone?: string) {
  await setMeta(GRID_PREFS_META_KEY, { startHour: 6, endHour: 22, dayLength: 480, ...(secondZone === undefined ? {} : { secondZone }) })
  const r = await render(<CalendarView mode={mode} />)
  await waitFor(() => r.container.querySelector('[data-calendar-grid]') !== null, 'the grid')
  if (secondZone === 'Asia/Kolkata' || secondZone === 'Asia/Kathmandu' || secondZone === 'America/Bogota') await waitFor(() => r.container.querySelector('[data-second-zone-hours]') !== null, 'the second column')
  else await flush()
  return r
}

describe('second time zone in the grid', () => {
  it('is off by default: one hour column, no codes', async () => {
    const r = await open('day')
    expect(r.container.querySelector('[data-second-zone-hours]')).toBeNull()
    expect(r.container.querySelector('[data-zone-code]')).toBeNull()
    expect(r.container.querySelectorAll('[data-hour-label]')).toHaveLength(24)
    await r.unmount()
  })

  it('draws two columns with clocks, codes and the zone stated once', async () => {
    const r = await open('day', 'Asia/Kolkata')
    const second = r.container.querySelector('[data-second-zone-hours]') as HTMLElement
    expect(second).toBeTruthy()
    expect(second.getAttribute('aria-hidden')).toBe('true')
    const labels = [...second.querySelectorAll('[data-second-label]')].map((e) => e.textContent)
    expect(labels).toHaveLength(24)
    expect(labels[9]).toBe('14:30')
    expect(labels[0]).toBe('')
    expect([...r.container.querySelectorAll('[data-zone-code]')].map((e) => e.textContent)).toEqual(['UTC', 'KOL'])
    const said = [...r.container.querySelectorAll('.sr-only')].filter((e) => /Times also shown in/.test(e.textContent ?? ''))
    expect(said).toHaveLength(1)
    expect(said[0].textContent).toBe('Times also shown in Kolkata (UTC+5:30)')
    expect(said[0].closest('[aria-hidden="true"]')).toBeNull()
    await r.unmount()
  })

  it('draws nothing extra for the device zone or an unknown zone', async () => {
    for (const zone of ['UTC', 'Mars/Olympus', '']) {
      const r = await open('day', zone)
      expect(r.container.querySelector('[data-second-zone-hours]')).toBeNull()
      expect(r.container.querySelector('[data-second-zone-head]')).toBeNull()
      await r.unmount()
    }
  })

  it('fits a phone: both gutters are fixed-width and cannot shrink, and the grid has no fixed width in Day', async () => {
    const r = await open('day', 'Asia/Kathmandu')
    const gutters = [...r.container.querySelectorAll('[data-calendar-grid] > [aria-hidden="true"]')]
    for (const g of gutters) expect(g.className).toMatch(/\bw-12\b.*\bshrink-0\b/)
    expect(gutters).toHaveLength(2)
    expect(r.container.querySelector('[data-second-label]')?.parentElement?.className).toMatch(/\bw-12\b/)
    const root = r.container.querySelector('[data-calendar-grid]')!.parentElement as HTMLElement
    expect(root.style.width).toBe('')
    expect(columns(r.container).length).toBeGreaterThanOrEqual(2)
    await r.unmount()
  })

  it('keeps every header row aligned: the span and lane rows get a spacer cell', async () => {
    const r = await open('day', 'Asia/Kolkata')
    const head = r.container.querySelector('[data-second-zone-head=""]') as HTMLElement
    expect(head.parentElement?.children[0].className).toMatch(/\bw-12\b/)
    expect(head.className).toMatch(/\bw-12\b/)
    await r.unmount()
  })

  it('shows in Week as well', async () => {
    const r = await open('week', 'America/Bogota')
    const labels = [...r.container.querySelectorAll('[data-second-label]')].map((e) => e.textContent)
    expect(labels[9]).toBe('04:00')
    await r.unmount()
  })
})
