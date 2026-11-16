// A user-requested online move: create first, revision-checked delete second.
import { useLiveQuery } from 'dexie-react-hooks'
import { api, ApiError } from './api'
import { db, getMeta, setMeta } from './db'
import { isOnline } from './online'
import type { CalendarEvent, CalendarEventDraft } from './types'

export interface EventMoveReceipt {
  source: CalendarEvent
  sourceConnection: string
  draft: CalendarEventDraft
  created?: CalendarEvent
}
const keyOf = (id: string) => `calendar.move.${id}`
export function useEventMove(id?: string): EventMoveReceipt | null | undefined {
  return useLiveQuery(() => id ? getMeta<EventMoveReceipt | null>(keyOf(id), null) : Promise.resolve(null), [id])
}

/** Stop retrying after a confirmed copy, without changing either calendar. */
export async function keepEventCopies(id: string): Promise<void> {
  const receipt = await getMeta<EventMoveReceipt | null>(keyOf(id), null)
  if (receipt?.created) await db.meta.delete(keyOf(id))
}

export async function moveSingleEvent(source: CalendarEvent, sourceConnection: string, draft: CalendarEventDraft): Promise<CalendarEvent> {
  if (!isOnline()) throw new Error('offline')
  let receipt = await db.transaction('rw', db.meta, db.eventOutbox, async () => {
    const existing = await getMeta<EventMoveReceipt | null>(keyOf(source.id), null)
    if (existing) return existing
    if (!source.rev || source.series || source.guests?.count || source.conference || !source.access?.update || !source.access.delete || draft.subscriptionId === source.subscriptionId || draft.repeat || draft.guests?.length || draft.onlineMeeting) throw new Error('unsupported_move')
    if ((await db.eventOutbox.toArray()).some((c) => c.eventId === source.id)) throw new Error('pending_source_write')
    const next: EventMoveReceipt = { source, sourceConnection, draft }
    await setMeta(keyOf(source.id), next)
    return next
  })
  if (!receipt.created) {
    const { connectionId, subscriptionId: _subscriptionId, ...body } = receipt.draft
    let result
    try { result = await api.createCalendarEvent(connectionId, body) }
    catch (error) {
      if (error instanceof ApiError && error.status >= 400 && error.status < 500 && error.status !== 408 && error.status !== 429) await db.meta.delete(keyOf(source.id))
      throw error
    }
    receipt = { ...receipt, created: result.event }
    await setMeta(keyOf(source.id), receipt)
    await db.calendarEvents.put(result.event)
  }
  const created = receipt.created!
  try {
    await api.deleteCalendarEvent(receipt.sourceConnection, receipt.source.id, receipt.source.rev ?? '')
  } catch (error) {
    // A previous delete may have completed before its response was lost.
    if (!(error instanceof ApiError && error.code === 'event_gone')) throw error
  }
  await db.transaction('rw', db.calendarEvents, db.meta, async () => {
    await db.calendarEvents.delete(receipt!.source.id)
    await db.calendarEvents.put(created)
    await db.meta.delete(keyOf(source.id))
  })
  return created
}
