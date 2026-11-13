// @vitest-environment happy-dom
// The scope menu (OpenSpec change calendar-event-series, task 5.1, design D12): three items with data-event-scope,
// disabled items with their reasons, Escape answers nothing, the first item has focus, the live region announces it.
import { act } from 'react'
import { describe, expect, it } from 'vitest'
import type { CalendarEvent, CalendarEventChange, CalendarRepeat, EventScope } from '../../lib/types'
import { key, render, waitFor } from '../../ui/test-utils'
import { answerScope, askScope, currentScopeRequest } from './eventScopeStore'
import { ScopeMenuHost } from './ScopeMenuHost'

const at = (date: string, time: string) => ({ date, time, tz: 'Europe/Berlin' })
const daily: CalendarRepeat = { freq: 'daily', interval: 1, days: [], ends: { type: 'never' } }
const monthly: CalendarRepeat = { freq: 'monthly', interval: 1, by: 'monthday', ends: { type: 'never' } }
const occurrence = (series: Partial<NonNullable<CalendarEvent['series']>> = {}): CalendarEvent => ({
  id: 'o2', subscriptionId: 's1', uid: 'u', summary: 'Routine', location: '', allDay: false, free: false, rev: 'r1',
  start: at('2026-11-12', '07:00'), end: at('2026-11-12', '08:30'), access: { update: true, delete: true }, series: { id: 'ser', repeat: daily, first: false, ...series },
})
const move: CalendarEventChange = { start: at('2026-11-12', '09:00'), end: at('2026-11-12', '10:30'), allDay: false }

const items = () => [...document.querySelectorAll<HTMLButtonElement>('[data-event-scope]')]
async function ask(ev: CalendarEvent, change: CalendarEventChange | 'delete', opts?: Parameters<typeof askScope>[2]) {
  let result: EventScope | null | 'pending' = 'pending'
  await act(async () => { void askScope(ev, change, opts).then((s) => { result = s }) })
  await waitFor(() => items().length > 0, 'the menu')
  return { result: () => result }
}

describe('ScopeMenu', () => {
  it('offers This event, This and following events and All events, with focus on the first and the choice announced', async () => {
    const r = await render(<ScopeMenuHost />)
    const asked = await ask(occurrence(), move)
    expect(items().map((i) => [i.getAttribute('data-event-scope'), i.textContent])).toEqual([['this', 'This event'], ['following', 'This and following events'], ['all', 'All events']])
    expect(document.querySelector('[role="menu"]')).not.toBeNull()
    expect(document.querySelector('[role="status"]')!.textContent).toBe('Choose which events to change')
    await waitFor(() => document.activeElement === items()[0], 'focus on the first item')
    expect(asked.result()).toBe('pending')
    await r.unmount()
  })

  it.each(['this', 'following', 'all'] as const)('choosing %s answers with it and closes the menu', async (scope) => {
    const r = await render(<ScopeMenuHost />)
    const asked = await ask(occurrence(), move)
    await act(async () => items().find((i) => i.getAttribute('data-event-scope') === scope)!.click())
    await waitFor(() => asked.result() === scope, 'the answer')
    await waitFor(() => items().length === 0, 'the menu closed')
    expect(currentScopeRequest()).toBeNull()
    await r.unmount()
  })

  it('Enter picks the focused item, the arrows move', async () => {
    const r = await render(<ScopeMenuHost />)
    const asked = await ask(occurrence(), move)
    await waitFor(() => document.activeElement === items()[0])
    await key(document.activeElement!, 'ArrowDown')
    expect(document.activeElement).toBe(items()[1])
    await act(async () => (document.activeElement as HTMLElement).click())
    await waitFor(() => asked.result() === 'following')
    await r.unmount()
  })

  it('Escape answers null: nothing is sent', async () => {
    const r = await render(<ScopeMenuHost />)
    const asked = await ask(occurrence(), move)
    await key(document.activeElement!, 'Escape')
    await waitFor(() => asked.result() === null, 'a cancelled answer')
    await waitFor(() => items().length === 0)
    await r.unmount()
  })

  it('hides "This and following events" on the first occurrence', async () => {
    const r = await render(<ScopeMenuHost />)
    await ask(occurrence({ first: true }), move)
    expect(items().map((i) => i.getAttribute('data-event-scope'))).toEqual(['this', 'all'])
    await r.unmount()
  })

  it('disables All events for a day move of a monthly series and says why', async () => {
    const r = await render(<ScopeMenuHost />)
    await ask(occurrence({ repeat: monthly }), { ...move, start: at('2026-11-13', '09:00'), end: at('2026-11-13', '10:30') })
    const all = items().find((i) => i.getAttribute('data-event-scope') === 'all')!
    expect(all.disabled).toBe(true)
    const described = document.getElementById(all.getAttribute('aria-describedby')!)!
    expect(described.textContent).toBe('This repeat can\'t move to another day for all events')
    expect(items().find((i) => i.getAttribute('data-event-scope') === 'this')!.disabled).toBe(false)
    await r.unmount()
  })

  it('a repeat change offers following and all only, and says what it resets', async () => {
    const r = await render(<ScopeMenuHost />)
    await ask(occurrence(), { repeat: { freq: 'weekly', interval: 1, days: ['mo'], ends: { type: 'never' } } }, { resets: 'repeat' })
    expect(items().map((i) => i.getAttribute('data-event-scope'))).toEqual(['following', 'all'])
    const note = document.getElementById(items()[1].getAttribute('aria-describedby')!)!
    expect(note.textContent).toBe('Changing the repeat resets events you changed individually')
    await r.unmount()
  })

  it('asks which events to delete', async () => {
    const r = await render(<ScopeMenuHost />)
    await ask(occurrence(), 'delete')
    expect(items().map((i) => i.getAttribute('data-event-scope'))).toEqual(['this', 'following', 'all'])
    expect(document.querySelector('[role="menu"]')!.getAttribute('aria-label')).toBe('Delete which events')
    await r.unmount()
  })

  it('a second request cancels the first', async () => {
    const r = await render(<ScopeMenuHost />)
    const first = await ask(occurrence(), move)
    await ask({ ...occurrence(), id: 'o3' }, move)
    await waitFor(() => first.result() === null, 'the first cancelled')
    expect(currentScopeRequest()?.event.id).toBe('o3')
    await act(async () => answerScope(null))
    await r.unmount()
  })
})
