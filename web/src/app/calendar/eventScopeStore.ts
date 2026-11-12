// Which events of a repeating series a change is for (OpenSpec change calendar-event-series D12): a small store
// the grid, the keys and the editor write to when a change of an occurrence needs a scope, and the scope menu
// reads. Nothing heavy is here: the menu itself (ScopeMenu) loads only when a request is open.
import { resettable } from '../../lib/moduleState'
import { createStore } from '../../lib/store'
import type { CalendarEvent, CalendarEventChange, EventScope } from '../../lib/types'

/** Where the menu opens: the bottom-left of an element, in viewport pixels. */
export interface ScopeAnchor {
  x: number
  y: number
}

export interface ScopeRequest {
  /** One per request, so a second menu opens fresh. */
  key: number
  event: CalendarEvent
  /** The change the choice is for, or `'delete'`. */
  change: CalendarEventChange | 'delete'
  at: ScopeAnchor
  /** Events changed one by one go back to the series: with a new repeat, or (Microsoft) with new times; the items say so. */
  resets?: 'repeat' | 'times'
  /** The person's answer; null when they cancelled (Escape, a click outside). */
  resolve: (scope: EventScope | null) => void
}

let seq = 0
resettable('eventScopeSeq', () => { seq = 0 })
const store = createStore<ScopeRequest | null>('eventScope', null)

export const currentScopeRequest = store.get
export const useScopeRequest = store.use

/** The menu's place: the element `selector` names, else the event's block, else the middle of the window. */
function anchorFor(ev: CalendarEvent, selector?: string | null): ScopeAnchor {
  const el = (selector && document.querySelector(selector)) || [...document.querySelectorAll('[data-event]')].find((e) => e.getAttribute('data-event') === ev.id)
  const r = el?.getBoundingClientRect()
  return r ? { x: r.left + Math.min(24, r.width / 2), y: r.bottom } : { x: window.innerWidth / 2, y: window.innerHeight / 3 }
}

/**
 * Asks which events a change is for. Resolves with the choice, or null when the person cancelled; a request
 * made while another is open cancels that one first. `from` is a selector for the control that asked (Save).
 */
export function askScope(event: CalendarEvent, change: CalendarEventChange | 'delete', opts: { from?: string | null; resets?: ScopeRequest['resets'] } = {}): Promise<EventScope | null> {
  store.get()?.resolve(null)
  return new Promise((resolve) => {
    const request: ScopeRequest = {
      key: ++seq, event, change, at: anchorFor(event, opts.from), resets: opts.resets,
      resolve: (scope) => { if (store.get() === request) store.set(null); resolve(scope) },
    }
    store.set(request)
  })
}

/** Answers the open request (a chosen scope, or null to cancel it). */
export function answerScope(scope: EventScope | null): void {
  store.get()?.resolve(scope)
}
