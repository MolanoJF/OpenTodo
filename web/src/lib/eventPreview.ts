// A change drawn on an occurrence's cached row while the scope menu asks which events it is for
// (OpenSpec change calendar-event-series D12): the block shows where the drag or the keys put it, nothing is
// queued, and Escape puts the row back. Choosing a scope puts it back too, and the real edit patches it again.
import { applyEventPatch } from './calendarFeeds'
import { currentEventId } from './calendarEventEdits'
import { db } from './db'
import { resettable } from './moduleState'
import type { CalendarEvent, CalendarEventChange } from './types'

interface Preview {
  /** The row before the first preview of this block. */
  original: CalendarEvent
  /** The row as last drawn: it is put back only if nothing else has changed it since. */
  shown: CalendarEvent
}

const previews = new Map<string, Preview>()
resettable('eventPreview', () => previews.clear())

/** Draws `change` on the occurrence's row; the first call keeps the row as it was. Returns the row as it was, or null when it is not cached. */
export async function previewEvent(ev: CalendarEvent, change: CalendarEventChange): Promise<CalendarEvent | null> {
  const id = currentEventId(ev.id)
  const kept = previews.get(id)
  const row = kept?.original ?? (await db.calendarEvents.get(id))
  if (!row) return null
  const shown = applyEventPatch(row, change)
  previews.set(id, { original: row, shown })
  await db.calendarEvents.put(shown)
  return row
}

/** The row as it was before the preview of `id`, or undefined when none is drawn. */
export const previewedOriginal = (id: string): CalendarEvent | undefined => previews.get(currentEventId(id))?.original

/** Puts the row back as it was (unless a refresh has replaced it since). */
export async function restoreEvent(id: string): Promise<void> {
  const key = currentEventId(id)
  const p = previews.get(key)
  if (!p) return
  previews.delete(key)
  const now = await db.calendarEvents.get(key)
  if (now && JSON.stringify(now) === JSON.stringify(p.shown)) await db.calendarEvents.put(p.original)
}
