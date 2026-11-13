// What a calendar event write that did not go through tells the person (OpenSpec change calendar-event-edits D6):
// the refusals, the failures and the offers of Retry and Discard. A tiny module on its own, so the shell can listen
// for them without loading the editing code: the code that raises one is already loaded, and the words come from it.
import { resettable } from './moduleState'
import type { CalendarEventDraft } from './types'

/** What a refused or failed write tells the person (the app words it with `eventEditNoticeText`). */
export interface EventEditNotice {
  code: string
  title: string
  message: string
  /** Why an event is not editable (422 `event_not_editable`): the reason code the server's message ends with. */
  reason?: string
  /** Which provider the event's calendar is, for the words of a refusal (calendar-event-guests D11). */
  provider?: 'google' | 'microsoft'
  /** A refused create: what the person typed, so the toast can reopen the editor on it (calendar-event-editor D9). */
  draft?: CalendarEventDraft
  /** A write that failed and stays on the device: the toast offers Retry and Discard on the command with this key. */
  failure?: { key: string }
}

const listeners = new Set<(n: EventEditNotice) => void>()
/** Notices nobody was listening to yet (a write that resumed at start, before the calendar's code loaded): the first listener gets them. */
let unheard: EventEditNotice[] = []
resettable('eventEditNotices', () => { listeners.clear(); unheard = [] })

export const notify = (n: EventEditNotice) => {
  if (listeners.size === 0) unheard = [...unheard, n].slice(-5)
  for (const fn of [...listeners]) fn(n)
}

/** Calls `fn` with every later notice, and with the ones nobody heard; returns the unsubscribe. */
export function onEventEditNotice(fn: (n: EventEditNotice) => void): () => void {
  listeners.add(fn)
  const heard = unheard
  unheard = []
  for (const n of heard) fn(n)
  return () => { listeners.delete(fn) }
}
