// "Tell N guests?" (OpenSpec change calendar-event-guests D8, D13): a small store the grid, the keys and the editors write
// to when a change, a delete or a create would tell people, and the dialog reads. Nothing heavy is here: the dialog
// itself (NotifyGuestsDialog) loads only when a request is open (the host in ScopeMenuHost), like the scope menu.
import { resettable } from '../../lib/moduleState'
import type { NotifyPrompt } from '../../lib/eventNotify'
import { createStore } from '../../lib/store'

export interface NotifyRequest {
  /** One per request, so a second dialog opens fresh. */
  key: number
  prompt: NotifyPrompt
  /** The person's answer: true to tell the guests, false to cancel (Cancel, Escape, a click outside). */
  resolve: (ok: boolean) => void
}

let seq = 0
resettable('notifyGuestsSeq', () => { seq = 0 })
const store = createStore<NotifyRequest | null>('notifyGuests', null)

export const currentNotifyRequest = store.get
export const useNotifyRequest = store.use

/** Asks the question; resolves true when the person chose to tell the guests. A request made while another is open cancels that one first. */
export function askNotify(prompt: NotifyPrompt): Promise<boolean> {
  store.get()?.resolve(false)
  return new Promise((resolve) => {
    const request: NotifyRequest = { key: ++seq, prompt, resolve: (ok) => { if (store.get() === request) store.set(null); resolve(ok) } }
    store.set(request)
  })
}

/** Answers the open request. */
export function answerNotify(ok: boolean): void {
  store.get()?.resolve(ok)
}
