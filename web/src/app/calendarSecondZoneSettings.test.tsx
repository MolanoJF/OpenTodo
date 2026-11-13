// @vitest-environment happy-dom
// The "Second time zone" row of Settings > Language and region > Calendar grid (OpenSpec `calendar-time-zones` D4).
import 'fake-indexeddb/auto'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { DEFAULT_GRID_PREFS, GRID_PREFS_META_KEY, normalizeGridPrefs } from '../lib/calendar'
import { db, getMeta } from '../lib/db'
import { saveGridPrefs } from '../lib/localData'
import { flush, key, render, type } from '../ui/test-utils'
import { CalendarSettingsView } from './CalendarSettings'

const input = (c: HTMLElement) => c.querySelector('[data-second-zone-setting] input[role="combobox"]') as HTMLInputElement
const options = (_c: HTMLElement) => [...document.querySelectorAll('[role="option"]')].map((o) => o.textContent ?? '')

beforeEach(async () => {
  await db.delete()
  await db.open()
})

describe('Second time zone setting', () => {
  it('is off by default and the picker says so', async () => {
    const { container, unmount } = await render(<CalendarSettingsView prefs={DEFAULT_GRID_PREFS} onSave={() => {}} />)
    expect(input(container).placeholder).toBe('Off')
    await unmount()
  })

  it('shows the chosen zone by its city', async () => {
    const { container, unmount } = await render(<CalendarSettingsView prefs={{ ...DEFAULT_GRID_PREFS, secondZone: 'Asia/Kolkata' }} onSave={() => {}} />)
    expect(input(container).placeholder).toBe('Kolkata')
    await unmount()
  })

  it('finds Off and zones with their offset, and leaves the device zone out', async () => {
    const { container, unmount } = await render(<CalendarSettingsView prefs={DEFAULT_GRID_PREFS} onSave={() => {}} />)
    input(container).focus()
    await flush()
    await type(input(container), 'Of')
    expect(options(container)[0]).toMatch(/^Off/)
    await type(input(container), 'UTC')
    expect(options(container).some((o) => /^UTC(?![A-Za-z])/.test(o))).toBe(false)
    await type(input(container), 'Kat')
    expect(options(container)[0]).toMatch(/Kat(h)?mandu.*UTC\+5:45/)
    await unmount()
  })

  it('choosing a zone saves it, choosing Off clears it', async () => {
    const onSave = vi.fn()
    const { container, unmount } = await render(<CalendarSettingsView prefs={{ ...DEFAULT_GRID_PREFS, secondZone: 'Asia/Kolkata' }} onSave={onSave} />)
    await type(input(container), 'Bogota')
    await key(input(container), 'Enter')
    await flush()
    expect(onSave).toHaveBeenLastCalledWith({ secondZone: 'America/Bogota' })
    await type(input(container), 'Off')
    await key(input(container), 'Enter')
    await flush()
    expect(onSave).toHaveBeenLastCalledWith({ secondZone: '' })
    await unmount()
  })

  it('is stored with the other grid preferences on this device, and Off removes it', async () => {
    await saveGridPrefs({ ...DEFAULT_GRID_PREFS, startHour: 8, secondZone: 'America/Bogota' })
    expect(normalizeGridPrefs(await getMeta(GRID_PREFS_META_KEY, null))).toMatchObject({ startHour: 8, secondZone: 'America/Bogota' })
    await saveGridPrefs({ ...DEFAULT_GRID_PREFS, startHour: 8, secondZone: '' })
    expect(normalizeGridPrefs(await getMeta(GRID_PREFS_META_KEY, null)).secondZone).toBeUndefined()
  })
})
