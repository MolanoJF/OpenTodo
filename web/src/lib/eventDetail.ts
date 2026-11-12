// Reading an event for the editor (OpenSpec change calendar-event-editor D2, D4): the description is not in the
// cache, so it is read from the calendar when an editor opens; a pending event has its draft's.
import { ApiError, api } from './api'
import { adoptEvent, currentEventId, queuedDraft } from './calendarEventEdits'
import { isPendingId } from './calendarFeeds'
import { db } from './db'
import type { EventExtras } from './eventFull'
import type { CalendarEvent, CalendarEventDetail } from './types'

/** The description's states, plus why it is not available. */
export type DescriptionState =
  | { status: 'loading' }
  | { status: 'ready'; text: string; editable: boolean }
  | { status: 'unavailable'; reason: 'offline' | 'failed' }

export interface EventDetail {
  /** undefined while the row is read, null when the event is gone. */
  event: CalendarEvent | null | undefined
  description: DescriptionState
  /** The repeat and the reminders as the calendar gave them with the description (calendar-event-series D3); absent until then. */
  extras?: EventExtras
}

/** What the detail answer adds to the row. */
export function extrasOf(res: Pick<CalendarEventDetail, 'series' | 'reminders' | 'remindersMax' | 'remindersOther'>): EventExtras {
  return {
    repeat: res.series ? res.series.repeat : null, custom: res.series?.custom, exceptions: res.series?.exceptions,
    ...(res.reminders ? { reminders: res.reminders } : {}), remindersMax: res.remindersMax, remindersOther: res.remindersOther,
  }
}

/**
 * Opens `eventId`: calls `put` with the row at once (description loading) and again with the description.
 * A newer version of the event than the cache's replaces the cached row before the form binds.
 */
export async function loadEventDetail(eventId: string, put: (d: EventDetail) => void): Promise<void> {
  const row = await db.calendarEvents.get(currentEventId(eventId))
  if (!row) return put({ event: null, description: { status: 'unavailable', reason: 'failed' } })
  if (isPendingId(row.id)) {
    const draft = await queuedDraft(row.id)
    return put({ event: row, description: draft ? { status: 'ready', text: draft.description, editable: true } : { status: 'unavailable', reason: 'failed' }, ...(draft ? { extras: { repeat: draft.repeat ?? null, reminders: draft.reminders ?? [] } } : {}) })
  }
  put({ event: row, description: { status: 'loading' } })
  const connectionId = (await db.calendarSubscriptions.get(row.subscriptionId))?.connectionId
  if (!connectionId || !row.access) return put({ event: row, description: { status: 'unavailable', reason: 'failed' } })
  try {
    const res = await api.calendarEventDetail(connectionId, row.id)
    if (res.replaces) await adoptEvent(res)
    put({ event: res.event, description: { status: 'ready', text: res.description, editable: res.descriptionEditable }, extras: extrasOf(res) })
  } catch (err) {
    if (err instanceof ApiError && err.code === 'event_gone') {
      await db.calendarEvents.delete(row.id)
      return put({ event: null, description: { status: 'unavailable', reason: 'failed' } })
    }
    put({ event: row, description: { status: 'unavailable', reason: err instanceof ApiError ? 'failed' : 'offline' } })
  }
}

/** The description of an event, read from the calendar (also for a locked one); '' when it cannot be read. */
export async function eventDescription(ev: CalendarEvent): Promise<string> {
  if (isPendingId(ev.id)) return (await queuedDraft(ev.id))?.description ?? ''
  try {
    const connectionId = (await db.calendarSubscriptions.get(ev.subscriptionId))?.connectionId
    return connectionId && ev.access ? (await api.calendarEventDetail(connectionId, ev.id)).description : ''
  } catch {
    return ''
  }
}
