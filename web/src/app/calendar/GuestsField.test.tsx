// @vitest-environment happy-dom
// The editor's Guests field (OpenSpec change calendar-event-guests, task 5.4, design D13, D17): the list with a labelled Remove
// per guest, rooms without one, an address typed in the Add guest Combobox, an invalid address as an inline error, a role
// per added guest, read-only when the calendar does not let the person change guests, and offline with the reason.
import 'fake-indexeddb/auto'
import { act, useState } from 'react'
import { beforeEach, describe, expect, it } from 'vitest'
import { useDatabase } from '../../lib/db'
import type { EventGuestList, GuestInput } from '../../lib/types'
import { click, key, render, type, waitFor } from '../../ui/test-utils'
import { GuestsField, type GuestsFieldProps } from './GuestsField'

const list: EventGuestList = {
  organizer: { name: 'Ana Silva', email: 'ana@example.com', you: true },
  guests: [
    { name: 'Ben Costa', email: 'ben@example.com', role: 'required', response: 'accepted', you: false },
    { name: '', email: 'room-4@example.com', role: 'room', response: 'accepted', you: false },
  ],
  count: 3, truncated: false,
}

let changes: { adds: GuestInput[]; removes: string[] }[] = []
function Harness(p: Partial<GuestsFieldProps>) {
  const [v, setV] = useState({ adds: [] as GuestInput[], removes: [] as string[] })
  return <GuestsField state="ready" list={list} editable online {...p} adds={v.adds} removes={v.removes} onChange={(next) => { changes.push(next); setV(next) }} />
}
const input = (c: HTMLElement) => c.querySelector<HTMLInputElement>('input[role="combobox"]')!
const typeIn = async (c: HTMLElement, text: string) => { await act(async () => { input(c).blur(); input(c).focus() }); await type(input(c), text) }
const remove = (c: HTMLElement, who: string) => c.querySelector<HTMLButtonElement>(`[aria-label="Remove ${who}"]`)
let n = 0
const mount = (p: Partial<GuestsFieldProps> = {}) => render(<Harness {...p} />)
beforeEach(() => { useDatabase(`guests-field-${Date.now()}-${n++}`); changes = [] })

describe('GuestsField', () => {
  it('shows the organiser and the guests in words, with a Remove named for each person but not for a room', async () => {
    const r = await mount()
    expect(r.container.querySelector('[data-event-organizer]')!.textContent).toBe('Organizer: Ana Silva')
    expect([...r.container.querySelectorAll('[data-guest]')].map((x) => x.textContent)).toEqual(['Ben Costa, accepted', 'room-4@example.com, room, accepted'])
    expect(remove(r.container, 'Ben Costa')).not.toBeNull()
    expect(r.container.querySelectorAll('[data-guest-remove]')).toHaveLength(1)
    await r.unmount()
  })

  it('Remove takes a guest off the list and sends the address as removed', async () => {
    const r = await mount()
    await click(remove(r.container, 'Ben Costa')!)
    expect(changes.at(-1)).toEqual({ adds: [], removes: ['ben@example.com'] })
    expect(r.container.querySelector('[data-guest="ben@example.com"]')).toBeNull()
    await r.unmount()
  })

  it('adds a typed address from the create row, and shows it with a role switch and its own Remove', async () => {
    const r = await mount()
    await typeIn(r.container, 'new.guest@example.com')
    await waitFor(() => document.querySelectorAll('[role="option"]').length === 1, 'the create row')
    await key(input(r.container), 'Enter')
    await waitFor(() => changes.length === 1, 'the guest added')
    expect(changes[0]).toEqual({ adds: [{ email: 'new.guest@example.com' }], removes: [] })
    expect(r.container.querySelector('[data-guest-added="new.guest@example.com"]')).not.toBeNull()
    await click(r.container.querySelector<HTMLButtonElement>('[data-guest-added] [role="switch"]')!)
    expect(changes.at(-1)!.adds).toEqual([{ email: 'new.guest@example.com', role: 'optional' }])
    await click(remove(r.container, 'new.guest@example.com')!)
    expect(changes.at(-1)).toEqual({ adds: [], removes: [] })
    await r.unmount()
  })

  it('refuses an invalid address with an inline error and adds nothing', async () => {
    const r = await mount()
    await typeIn(r.container, 'not an address')
    await waitFor(() => document.querySelectorAll('[role="option"]').length === 1)
    await key(input(r.container), 'Enter')
    await waitFor(() => !!r.container.querySelector('[data-event-form-guest-error]'), 'the error')
    expect(r.container.querySelector('[data-event-form-guest-error]')!.textContent).toBe('Enter an email address')
    expect(changes).toEqual([])
    await r.unmount()
  })

  it('does not add the organiser, a guest or an address twice', async () => {
    const r = await mount()
    for (const dup of ['ANA@example.com', 'ben@example.com']) {
      await typeIn(r.container, dup)
      await waitFor(() => document.querySelectorAll('[role="option"]').length >= 1)
      await key(input(r.container), 'Enter')
    }
    expect(changes).toEqual([])
    await r.unmount()
  })

  it('is read-only text when the calendar does not let the person change guests', async () => {
    const r = await mount({ editable: false })
    expect(r.container.querySelectorAll('[data-guest]')).toHaveLength(2)
    expect(r.container.querySelector('[data-guest-remove]')).toBeNull()
    expect(input(r.container)).toBeNull()
    await r.unmount()
  })

  it('offline: no editing, with the reason', async () => {
    const r = await mount({ online: false })
    expect(r.container.querySelector('[data-guest-remove]')).toBeNull()
    expect(input(r.container)).toBeNull()
    expect(r.container.textContent).toContain("Guests are told when you're online")
    await r.unmount()
  })

  it('a new event has no list, only the Add guest field', async () => {
    const r = await mount({ list: null })
    expect(r.container.querySelector('[data-event-organizer]')).toBeNull()
    expect(input(r.container)).not.toBeNull()
    await r.unmount()
  })

  it('says it is loading, and when the guests could not be read', async () => {
    const loading = await mount({ state: 'loading', list: null })
    expect(loading.container.querySelector('[role="status"]')).not.toBeNull()
    expect(input(loading.container)).toBeNull()
    await loading.unmount()
    const off = await mount({ state: 'unavailable', reason: 'offline', list: null })
    expect(off.container.textContent).toContain('You are offline, so the guests are left as they are.')
    await off.unmount()
  })
})
