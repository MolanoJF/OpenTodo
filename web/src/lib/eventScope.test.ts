import { describe, expect, it } from 'vitest'
import { canShiftDays, dayMoved, needsScope, scopesFor } from './eventScope'
import type { CalendarEvent, CalendarEventSeries, CalendarRepeat } from './types'

const at = (date: string, time: string) => ({ date, time, tz: 'Europe/Berlin' })
const daily: CalendarRepeat = { freq: 'daily', interval: 1, days: [], ends: { type: 'never' } }
const weekly: CalendarRepeat = { freq: 'weekly', interval: 1, days: ['mo'], ends: { type: 'never' } }
const monthly: CalendarRepeat = { freq: 'monthly', interval: 1, by: 'monthday', ends: { type: 'never' } }
const yearly: CalendarRepeat = { freq: 'yearly', interval: 1, ends: { type: 'never' } }
const ev = (series?: Partial<CalendarEventSeries>): CalendarEvent => ({
  id: 'e1', subscriptionId: 's1', uid: 'u1', summary: 'Routine', location: '', start: at('2026-11-12', '07:00'), end: at('2026-11-12', '08:30'), allDay: false, free: false, rev: 'r1',
  access: { update: true, delete: true }, ...(series ? { series: { id: 'x', repeat: daily, ...series } } : {}),
})
const move = (time: string, date = '2026-11-12') => ({ start: at(date, time), end: at(date, '09:00'), allDay: false })

describe('needsScope', () => {
  it('is true for an occurrence of a series and false for a single event', () => {
    expect(needsScope(ev({}))).toBe(true)
    expect(needsScope(ev())).toBe(false)
  })
})

describe('canShiftDays', () => {
  it.each([[daily, true], [weekly, true], [monthly, false], [yearly, false], [null, true], [undefined, true]])('%j -> %s', (repeat, ok) => {
    expect(canShiftDays(repeat)).toBe(ok)
  })
})

describe('dayMoved', () => {
  it('compares the new start date with the occurrence start date', () => {
    expect(dayMoved(ev({}), move('09:00'))).toBe(false)
    expect(dayMoved(ev({}), move('09:00', '2026-11-13'))).toBe(true)
    expect(dayMoved(ev({}), { summary: 'x' })).toBe(false)
  })
})

describe('scopesFor', () => {
  const names = (list: ReturnType<typeof scopesFor>) => list.map((c) => c.scope)

  it('offers this, following and all for a time change', () => {
    expect(scopesFor(ev({ first: false }), move('09:00'))).toEqual([{ scope: 'this' }, { scope: 'following' }, { scope: 'all' }])
  })

  it('does not offer following on the first occurrence', () => {
    expect(names(scopesFor(ev({ first: true }), move('09:00')))).toEqual(['this', 'all'])
    expect(names(scopesFor(ev({ first: true }), 'delete'))).toEqual(['this', 'all'])
  })

  it('a repeat change allows following and all only', () => {
    expect(names(scopesFor(ev({ first: false }), { repeat: weekly }))).toEqual(['following', 'all'])
    expect(names(scopesFor(ev({ first: true }), { repeat: weekly }))).toEqual(['all'])
  })

  it('stop repeating is for following only, also on the first occurrence', () => {
    expect(names(scopesFor(ev({ first: true }), { repeat: null }))).toEqual(['following'])
    expect(names(scopesFor(ev({ first: false }), { repeat: null }))).toEqual(['following'])
  })

  it('a day move disables all for monthly and yearly series, not for daily and weekly ones', () => {
    for (const [repeat, blocked] of [[daily, false], [weekly, false], [monthly, true], [yearly, true], [null, false]] as const) {
      const all = scopesFor(ev({ repeat, first: false }), move('09:00', '2026-11-13')).find((c) => c.scope === 'all')!
      expect(all.blocked, JSON.stringify(repeat)).toBe(blocked ? 'day-shift' : undefined)
    }
  })

  it('a day move still offers this and following for a monthly series', () => {
    const list = scopesFor(ev({ repeat: monthly, first: false }), move('09:00', '2026-11-13'))
    expect(list.filter((c) => !c.blocked).map((c) => c.scope)).toEqual(['this', 'following'])
  })

  it('a custom rule is unknown: all is allowed and the server decides', () => {
    const all = scopesFor(ev({ repeat: null, custom: true, first: false }), move('09:00', '2026-11-13')).find((c) => c.scope === 'all')!
    expect(all.blocked).toBeUndefined()
  })

  it('a new repeat replaces the rule, so a day move with it is not blocked', () => {
    const list = scopesFor(ev({ repeat: monthly, first: false }), { start: at('2026-11-13', '07:00'), end: at('2026-11-13', '08:30'), repeat: weekly })
    expect(list.map((c) => c.blocked)).toEqual([undefined, undefined])
  })
})
