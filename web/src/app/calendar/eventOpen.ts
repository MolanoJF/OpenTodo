// Enter on a selected event opens its popover (OpenSpec change calendar-event-edits D7): the
// grid's keys name the block, the block (which owns the popover) opens it.
const listeners = new Set<(id: string) => void>()

/** Calls `fn` with the layout id (`event:<id>`) of every event asked to open; returns the unsubscribe. */
export function onOpenEventRequest(fn: (id: string) => void): () => void {
  listeners.add(fn)
  return () => { listeners.delete(fn) }
}

export function requestOpenEvent(id: string): void {
  for (const fn of [...listeners]) fn(id)
}
