// What tells people is sent now and never queued (OpenSpec change calendar-event-guests D8, D11). Loaded only when a
// change that tells guests, an answer or a create with guests is made (eventActions, EventRespond import it lazily), so
// the first load carries none of it. Split from calendarEventEdits, which keeps the queue.
import { api, ApiError } from './api'
import { createBodyOf, currentEventId, DELETE_DELAY_MS, eventKey, notify, reasonOf, swapped, writeFailureText, type EventEditNotice } from './calendarEventEdits'
import { pendingRowOf, refetchEvents, refreshEvents } from './calendarFeeds'
import { db } from './db'
import { providerOf } from './eventGuests'
import { previewEvent, restoreEvent } from './eventPreview'
import { t } from './i18n'
import { resettable } from './moduleState'
import { isOnline } from './online'
import type { CalendarEvent, CalendarEventChange, CalendarEventDraft, CalendarEventReply, CalendarEventUpdate, EventScope, GuestAnswer } from './types'

// ---- what tells people is sent now and never queued (OpenSpec change calendar-event-guests D8, D11) ----
// A change of an event whose `access.notifies` is true, an answer to an invitation and a create with guests call the API
// directly, with the person's confirmation already given. Nothing of them is written to `eventOutbox`, so nothing can be
// sent later from a queue: offline they are refused with the reason, and a failed request is restored, not retried.

export type SendRequest =
  // `confirmed`: the person answered the "Tell N guests?" dialog; only then does the request carry `notifyGuests: true`.
  | { kind: 'change'; change: CalendarEventChange; scope?: EventScope; confirmed?: boolean }
  | { kind: 'reply'; response: GuestAnswer; scope?: EventScope; sendResponse?: boolean }
  | { kind: 'create'; draft: CalendarEventDraft; confirmed?: boolean }

/** The row of an answer swapped in, the series' version applied to its other rows, and the range read again when asked. */
async function adoptAnswer(res: CalendarEventUpdate | CalendarEventReply): Promise<void> {
  await db.transaction('rw', db.calendarEvents, async () => {
    await db.calendarEvents.delete(res.replaces)
    const series = res.series
    if (series?.rev) await db.calendarEvents.filter((e) => e.series?.id === series.id).modify((e) => { e.rev = series.rev; if (e.series) e.series.rev = series.rev })
    if (res.event) {
      swapped.set(res.replaces, res.event.id)
      await db.calendarEvents.put(res.event)
    }
  })
  if (res.refetch) void refetchEvents()
}

/** Tells why a request that tells people did not go through, from the error it got. */
function refusal(err: unknown, title: string, provider: EventEditNotice['provider'], draft?: CalendarEventDraft): EventEditNotice {
  if (!(err instanceof ApiError)) return { code: 'offline_guests', title, message: '', ...(draft ? { draft } : {}) }
  if (err.status === 502 && /response_not_kept/.test(err.serverMessage)) return { code: 'response_not_kept', title, message: err.message, provider }
  const reason = reasonOf(err)
  return { code: err.code, title, message: writeFailureText({ code: err.code, status: err.status, message: err.message, retryAfter: err.retryAfter }), ...(reason ? { reason } : {}), ...(provider ? { provider } : {}), ...(draft ? { draft } : {}) }
}

/** Puts `before` back unless a refresh has replaced the drawn row since. */
async function undraw(before: CalendarEvent, shown: CalendarEvent): Promise<void> {
  const now = await db.calendarEvents.get(before.id)
  if (now && JSON.stringify(now) === JSON.stringify(shown)) await db.calendarEvents.put(before)
}

/**
 * Sends a change, an answer or a create that tells people, now. The block is drawn at once and restored when the
 * calendar refuses. False when nothing was sent (offline, refused, a queued edit of the event is still waiting).
 */
export async function sendNow(ev: CalendarEvent | null, req: SendRequest): Promise<boolean> {
  const draft = req.kind === 'create' ? req.draft : undefined
  const row = ev && !draft ? (await db.calendarEvents.get(currentEventId(ev.id))) ?? ev : null
  const title = draft?.summary ?? row?.summary ?? ''
  if (!isOnline()) { notify({ code: 'offline_guests', title, message: '', ...(draft ? { draft } : {}) }); return false }
  const sub = await db.calendarSubscriptions.get(draft?.subscriptionId ?? row?.subscriptionId ?? '')
  const connectionId = draft?.connectionId ?? sub?.connectionId
  if (!connectionId || (!draft && (!row || !row.rev))) return false
  const provider = providerOf(sub)
  if (row && (await db.eventOutbox.get(eventKey(connectionId, row.id)))) { notify({ code: 'edit_blocked', title, message: t('Saving this change first') }); return false }
  // Drawn at once: a change as the scope menu draws it, an answer on the row, a create as a pending block.
  let shown: CalendarEvent | null = null
  if (draft) await db.calendarEvents.put(pendingRowOf(draft))
  else if (req.kind === 'change') await previewEvent(row!, req.change)
  else if (req.kind === 'reply') {
    shown = { ...row!, guests: { count: 0, organizer: false, canRespond: true, ...row!.guests, you: req.response } }
    await db.calendarEvents.put(shown)
  }
  try {
    if (req.kind === 'create') {
      const { event } = await api.createCalendarEvent(connectionId, { ...createBodyOf(req.draft), ...(req.confirmed ? { notifyGuests: true } : {}) })
      await db.transaction('rw', db.calendarEvents, async () => {
        await db.calendarEvents.delete(pendingRowOf(req.draft).id)
        await db.calendarEvents.put(event)
      })
    } else if (req.kind === 'change') {
      const res = await api.updateCalendarEvent(connectionId, row!.id, { rev: row!.rev!, ...req.change, ...(req.scope ? { scope: req.scope } : {}), ...(req.confirmed ? { notifyGuests: true } : {}) })
      await restoreEvent(row!.id) // the drawn change is the answer's now
      await adoptAnswer(res)
    } else {
      await adoptAnswer(await api.respondToCalendarEvent(connectionId, row!.id, { rev: row!.rev!, response: req.response, ...(req.scope ? { scope: req.scope } : {}), ...(req.sendResponse === false ? { sendResponse: false } : {}) }))
    }
    return true
  } catch (err) {
    if (draft) await db.calendarEvents.delete(pendingRowOf(draft).id)
    else if (req.kind === 'change') await restoreEvent(row!.id)
    else if (shown) await undraw(row!, shown)
    const n = refusal(err, title, provider, draft)
    if (n.code === 'response_not_kept') await refetchEvents() // show the calendar's real answer
    else if (!(err instanceof ApiError) || err.status < 500) void refreshEvents()
    notify(n)
    return false
  }
}

/** Notified deletes in their pause: key -> the timer that sends them. */
const waitingDeletes = new Map<string, ReturnType<typeof setTimeout>>()
resettable('calendarEventDeletes', () => { for (const timer of waitingDeletes.values()) clearTimeout(timer); waitingDeletes.clear() })

/**
 * A delete that tells guests, after the confirmation: the event leaves the grid at once and the request is sent when
 * the pause is over (`DELETE_DELAY_MS`), so Undo can still cancel it. It lives in memory, not in the outbox: a reload
 * in the pause sends nothing and the event is back. Offline at send time it is restored with the reason. Returns the
 * Undo (false once the request is on its way), or null when the event cannot be deleted from here.
 */
export async function startNotifiedDelete(ev: CalendarEvent, scope?: EventScope, delay = DELETE_DELAY_MS): Promise<(() => Promise<boolean>) | null> {
  const row = (await db.calendarEvents.get(currentEventId(ev.id))) ?? ev
  const sub = await db.calendarSubscriptions.get(row.subscriptionId)
  const connectionId = sub?.connectionId
  if (!connectionId || !row.rev) return null
  const key = eventKey(connectionId, row.id)
  if (waitingDeletes.has(key) || (await db.eventOutbox.get(key))) return null
  await db.calendarEvents.delete(row.id)
  const send = async () => {
    waitingDeletes.delete(key)
    if (!isOnline()) { await db.calendarEvents.put(row); notify({ code: 'offline_guests', title: row.summary, message: '' }); return }
    try {
      await api.deleteCalendarEvent(connectionId, row.id, row.rev!, scope, true)
      if (row.series) void refetchEvents()
    } catch (err) {
      await db.calendarEvents.put(row)
      notify(refusal(err, row.summary, providerOf(sub)))
      if (!(err instanceof ApiError) || err.status < 500) void refreshEvents()
    }
  }
  waitingDeletes.set(key, setTimeout(() => void send(), delay))
  return async () => {
    const timer = waitingDeletes.get(key)
    if (timer === undefined) return false
    clearTimeout(timer)
    waitingDeletes.delete(key)
    await db.calendarEvents.put(row)
    return true
  }
}
