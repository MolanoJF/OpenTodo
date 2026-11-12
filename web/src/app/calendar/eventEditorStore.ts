// Which event the editor is open on (OpenSpec change calendar-event-editor D11): a small store
// the grid, the palette and the keys write to and EventEditorHost reads. Nothing is heavy here:
// the editor itself (EventPanel) loads only when a request is open.
import { resettable } from '../../lib/moduleState'
import { createStore } from '../../lib/store'
import type { CalendarEventDraft } from '../../lib/types'

/** Where a new event goes: a day, and on the grid the start and the end (minutes from midnight) of a click or a sweep. */
export interface EventPlace {
  date: string
  start?: number
  end?: number
}

export type EventEditorRequest =
  | { mode: 'create'; key: number; place?: EventPlace; /** Duplicate, or Edit again after a refused create. */ draft?: CalendarEventDraft }
  | { mode: 'edit'; key: number; eventId: string }

let seq = 0
resettable('eventEditorSeq', () => { seq = 0 })
const store = createStore<EventEditorRequest | null>('eventEditor', null)

export const currentEventEditor = store.get
export const useEventEditor = store.use

/** Opens the editor: a new event at a place (or from a draft), or an existing event by its id. */
export function openEventEditor(request: { mode: 'create'; place?: EventPlace; draft?: CalendarEventDraft } | { mode: 'edit'; eventId: string }): void {
  store.set({ ...request, key: ++seq })
}

export function closeEventEditor(): void {
  store.set(null)
}

/** A request from the editor's Task | Event control to open the add dialog at a place (useAddHere answers it). */
export const addTaskRequest = createStore<EventPlace | null>('addTaskRequest', null)
