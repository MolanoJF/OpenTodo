// Which events of a repeating series a change can be for (OpenSpec change calendar-event-series D1, D11).
// Pure: the grid, the keys and the editor ask here before they show the scope menu.
import type { CalendarEvent, CalendarEventChange, CalendarRepeat, EventScope } from './types'

/** An event is "an occurrence" when the server says it belongs to a series; only then does a write need a scope. */
export const needsScope = (ev: CalendarEvent): boolean => !!ev.series

/** Why a scope cannot be chosen for this change. */
export type ScopeBlock = 'day-shift'

export interface ScopeChoice {
  scope: EventScope
  /** Shown but disabled, with its reason. */
  blocked?: ScopeBlock
}

/** Whether a series' rule can move to another day: daily and weekly can, monthly and yearly cannot; a rule not known here is left to the server. */
export function canShiftDays(repeat: CalendarRepeat | null | undefined): boolean {
  return !repeat || repeat.freq === 'daily' || repeat.freq === 'weekly'
}

/** Whether a change moves the occurrence's start to another day (in the event's own zone). */
export const dayMoved = (ev: CalendarEvent, change: CalendarEventChange): boolean => !!change.start && change.start.date !== ev.start.date

/**
 * The scopes the menu offers for a change (or for a delete), in order. A new repeat is for this and
 * following events or all events; stopping the repeat is for following only; the first occurrence
 * has no "following" (it is the series); a day move cannot be applied to a monthly or yearly rule.
 */
export function scopesFor(ev: CalendarEvent, change: CalendarEventChange | 'delete'): ScopeChoice[] {
  const first = !!ev.series?.first
  if (change !== 'delete' && change.repeat === null) return [{ scope: 'following' }]
  const repeating = change !== 'delete' && change.repeat !== undefined
  const out: ScopeChoice[] = []
  if (!repeating) out.push({ scope: 'this' })
  if (!first) out.push({ scope: 'following' })
  const shifted = change !== 'delete' && !repeating && dayMoved(ev, change) && !canShiftDays(ev.series?.repeat)
  out.push(shifted ? { scope: 'all', blocked: 'day-shift' } : { scope: 'all' })
  return out
}
