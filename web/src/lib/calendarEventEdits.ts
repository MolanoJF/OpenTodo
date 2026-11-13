// Edits of connected calendars' events (OpenSpec change calendar-event-edits, design D1, D6).
// The calendar is the source of truth: an edit is not an op. The device patches its cached
// row at once and queues one command per event in a local outbox (Dexie `eventOutbox`); the
// server writes to the remote calendar and answers with the event as the calendar now holds
// it, or refuses, and then the device restores the row, refetches and says why.
import { useLiveQuery } from 'dexie-react-hooks'
import { api, ApiError } from './api'
import { applyDraftPatch, applyEventPatch, eventPatchBetween, isNoChange, isPendingId, onEventsRefreshed, pendingRowOf, refetchEvents, refreshEvents } from './calendarFeeds'
import { db } from './db'
import { t } from './i18n'
import { resettable } from './moduleState'
import { platform } from './platform'
import type { CalendarEvent, CalendarEventChange, CalendarEventCreateBody, CalendarEventDraft, CalendarEventUpdate, CalendarRepeat, EventCommand, EventScope } from './types'

/** A delete waits this long so the toast's Undo can cancel it. */
export const DELETE_DELAY_MS = 8000
/** An edit waits this long for the next one (two quick key presses are one change). */
export const EDIT_SETTLE_MS = 300
const BACKOFF_MS = [2_000, 10_000, 30_000, 120_000, 300_000]
/** Answers that end an edit: the server looked at it and said no, so it is rolled back. */
const REFUSED = new Set([400, 403, 404, 409, 422])
const RETRY_CODES = new Set(['reauth_required', 'mfa_enrolment_required'])

type Patch = EventCommand['patch']
export const isDelete = (p: Patch): p is { delete: true } => 'delete' in p
export const isCreate = (p: Patch): p is { create: CalendarEventDraft } => 'create' in p
/** The change a command carries as an edit of an existing event (not a delete or a create). */
/** The request body of a create: the draft without the device's own ids. */
export const createBodyOf = (d: CalendarEventDraft): CalendarEventCreateBody => ({
  clientId: d.clientId, calendarUrl: d.calendarUrl, summary: d.summary, start: d.start, end: d.end, allDay: d.allDay, location: d.location, description: d.description, free: d.free,
  ...(d.repeat ? { repeat: d.repeat } : {}), ...(d.reminders?.length ? { reminders: d.reminders } : {}),
})
const changeOf = (p: Patch | undefined) => p as CalendarEventChange | undefined

/** What a refused edit tells the person (the app words it with `eventEditNoticeText`). */
export interface EventEditNotice {
  code: string
  title: string
  message: string
  /** Why an event is not editable (422 `event_not_editable`): the reason code the server's message ends with. */
  reason?: string
  /** A refused create: what the person typed, so the toast can reopen the editor on it (calendar-event-editor D9). */
  draft?: CalendarEventDraft
}

const listeners = new Set<(n: EventEditNotice) => void>()
const notify = (n: EventEditNotice) => { for (const fn of [...listeners]) fn(n) }
/** Calls `fn` with every later refusal; returns the unsubscribe. */
export function onEventEditNotice(fn: (n: EventEditNotice) => void): () => void {
  listeners.add(fn)
  return () => { listeners.delete(fn) }
}

/** The id an event has now: an edit that moved it gave it a new one (the calendar's answer), and the old id still names it. */
export function currentEventId(id: string): string {
  for (let i = 0; i < 20 && swapped.has(id); i++) id = swapped.get(id)!
  return id
}

/** The cached row of an event, as the grid shows it now (with a queued edit applied). */
export const cachedEvent = (id: string): Promise<CalendarEvent | undefined> => db.calendarEvents.get(id)

export const eventKey = (connectionId: string, eventId: string) => `${connectionId}:${eventId}`

/** Old id -> the id the calendar gave after a move, so an Undo of a sent edit finds its event. */
const swapped = new Map<string, string>()
let started = false
let flushing: Promise<void> | null = null
let again = false
let wantNow = 0
let retryTimer: ReturnType<typeof setTimeout> | undefined
resettable('calendarEventEdits', () => { swapped.clear(); started = false; flushing = null; again = false; wantNow = 0; clearTimeout(retryTimer); retryTimer = undefined; listeners.clear() })

const mergeChange = (a: CalendarEventChange | undefined, b: CalendarEventChange): CalendarEventChange => ({ ...a, ...b })

/** What a change replaced that the row does not hold: the text the editor loaded, the repeat and the reminders it showed. */
export interface EditBefore {
  description?: string
  repeat?: CalendarRepeat | null
  reminders?: number[]
}

/** The change that undoes `patch` on `row`: the fields it names, as `row` has them (the rest: see `before`). */
function inverseOf(row: CalendarEvent, patch: CalendarEventChange, before?: EditBefore): CalendarEventChange {
  return {
    ...(patch.summary !== undefined ? { summary: row.summary } : {}),
    ...(patch.start && patch.end ? { start: row.start, end: row.end, allDay: row.allDay } : {}),
    ...(patch.location !== undefined ? { location: row.location } : {}),
    ...(patch.free !== undefined ? { free: row.free } : {}),
    ...(patch.description !== undefined && before?.description !== undefined ? { description: before.description } : {}),
    ...(patch.repeat !== undefined && before?.repeat !== undefined ? { repeat: before.repeat } : {}),
    ...(patch.reminders !== undefined && before?.reminders !== undefined ? { reminders: before.reminders } : {}),
  }
}

/** A flush when the earliest waiting command is due: a settle, a delete's delay or a retry's backoff. */
export async function armTimer(): Promise<void> {
  clearTimeout(retryTimer)
  const next = await db.eventOutbox.orderBy('notBefore').filter((c) => !c.sending).first()
  if (next) retryTimer = setTimeout(() => void flushEventEdits(), Math.max(0, next.notBefore - Date.now()) + 20)
}

/**
 * Applies an edit to the cached event and queues it. Later edits of the same event merge
 * into one command, a delete replaces it, and an edit that brings the event back to what it
 * was removes it. Returns what undoes it (`{ delete: true }` for a delete: see `cancelEventDelete`),
 * or null when the event cannot be edited (no connection or version known).
 *
 * `scope` is the part of a repeating series the change is for (calendar-event-series D11): an edit with
 * the command's scope merges into it; one with another scope waits in `next` with its own scope and is
 * sent after the answer; a third scope is refused with a toast. Only the edited row is patched here.
 */
export async function editEvent(ev: CalendarEvent, patch: CalendarEventChange | { delete: true }, now = Date.now(), before?: EditBefore, scope?: EventScope): Promise<CalendarEventChange | { delete: true } | null> {
  const id = swapped.get(ev.id) ?? ev.id
  let blocked: CalendarEvent | null = null
  const result = await db.transaction('rw', [db.calendarEvents, db.calendarSubscriptions, db.eventOutbox], async () => {
    const row = (await db.calendarEvents.get(id)) ?? ev
    const connectionId = (await db.calendarSubscriptions.get(row.subscriptionId))?.connectionId
    if (!connectionId || (!row.rev && !isPendingId(row.id))) return null
    const key = eventKey(connectionId, row.id)
    const cmd = await db.eventOutbox.get(key)
    if (isPendingId(row.id) && !cmd) return null // a pending row whose create is gone
    if (cmd && (isDelete(cmd.patch) || (cmd.next && isDelete(cmd.next)))) return null // already going away
    const inverse = isDelete(patch) ? patch : inverseOf(row, patch, before)
    if (cmd && isCreate(cmd.patch) && !cmd.sending) { // not sent yet: the draft takes it, a delete sends nothing
      if (isDelete(patch)) {
        await db.eventOutbox.delete(key)
        await db.calendarEvents.delete(row.id)
        return inverse
      }
      const draft = applyDraftPatch(cmd.patch.create, patch)
      const pending = pendingRowOf(draft)
      await db.calendarEvents.put(pending)
      await db.eventOutbox.put({ ...cmd, base: pending, patch: { create: draft }, notBefore: now + EDIT_SETTLE_MS })
      return inverse
    }
    // Waits for the command's answer: it is in flight, or it is for another scope than this edit.
    const waits = !!cmd && (cmd.sending || cmd.scope !== scope)
    if (cmd && waits && cmd.next && cmd.nextScope !== scope) { blocked = row; return null } // a third scope
    if (isDelete(patch) && cmd && waits) {
      await db.eventOutbox.put({ ...cmd, next: patch, nextScope: scope }) // sent after the answer, against the new id
      await db.calendarEvents.delete(row.id)
      return inverse
    }
    if (isDelete(patch)) {
      const base = cmd?.base ?? row
      await db.eventOutbox.put({ key, connectionId, eventId: row.id, rev: cmd?.rev ?? row.rev ?? '', base, patch, notBefore: now + DELETE_DELAY_MS, tries: 0, ...(scope ? { scope } : {}) })
      await db.calendarEvents.delete(row.id)
      return inverse
    }
    await db.calendarEvents.put(applyEventPatch(row, patch))
    if (cmd && waits) {
      await db.eventOutbox.put({ ...cmd, next: mergeChange(changeOf(cmd.next), patch), nextScope: scope })
    } else if (cmd) {
      const merged = mergeChange(changeOf(cmd.patch), patch)
      if (isNoChange(cmd.base, merged)) await db.eventOutbox.delete(key)
      else await db.eventOutbox.put({ ...cmd, patch: merged, notBefore: now + EDIT_SETTLE_MS })
    } else {
      await db.eventOutbox.put({ key, connectionId, eventId: row.id, rev: row.rev ?? '', base: row, patch, notBefore: now + EDIT_SETTLE_MS, tries: 0, ...(scope ? { scope } : {}) })
    }
    return inverse
  })
  if (blocked) notify({ code: 'edit_blocked', title: (blocked as CalendarEvent).summary, message: t('Saving this change first') })
  if (result) void armTimer().catch(() => undefined)
  return result
}

/**
 * Undo of a delete that has not been sent: the event comes back as `ev` was drawn (with an
 * edit that was queued before the delete still queued). False once it is on its way.
 */
export async function cancelEventDelete(ev: CalendarEvent): Promise<boolean> {
  return db.transaction('rw', [db.calendarEvents, db.eventOutbox], async () => {
    const cmd = (await db.eventOutbox.toArray()).find((c) => c.eventId === ev.id)
    if (cmd?.next && isDelete(cmd.next)) { // asked while an edit was in flight or waiting: not sent yet
      await db.calendarEvents.put(ev)
      await db.eventOutbox.put({ ...cmd, next: undefined, nextScope: undefined })
      return true
    }
    if (!cmd || !isDelete(cmd.patch) || cmd.sending) return false
    await db.calendarEvents.put(ev)
    const rest = eventPatchBetween(cmd.base, ev)
    if (Object.keys(rest).length === 0) await db.eventOutbox.delete(cmd.key)
    else await db.eventOutbox.put({ ...cmd, patch: rest, notBefore: 0 })
    return true
  })
}

const backoff = (tries: number) => BACKOFF_MS[Math.min(tries, BACKOFF_MS.length - 1)]

/** After a refusal: the row as it was, the command gone, the cache read again, the reason told. */
async function rollBack(cmd: EventCommand, err: ApiError): Promise<void> {
  const draft = isCreate(cmd.patch) ? (cmd.next && !isDelete(cmd.next) ? applyDraftPatch(cmd.patch.create, cmd.next) : cmd.patch.create) : undefined
  await db.transaction('rw', [db.calendarEvents, db.eventOutbox], async () => {
    await db.eventOutbox.delete(cmd.key)
    if (draft) await db.calendarEvents.delete(cmd.eventId) // a create that was refused leaves nothing to restore
    else await db.calendarEvents.put(cmd.base)
  })
  const cancelled = !!cmd.next && isDelete(cmd.next) // the person already discarded it: nothing to offer back
  const cut = err.serverMessage.lastIndexOf(': ')
  const reason = err.code === 'event_not_editable' && cut >= 0 ? err.serverMessage.slice(cut + 2) : undefined
  notify({ code: err.code, title: draft?.summary ?? cmd.base.summary, message: err.message, ...(reason ? { reason } : {}), ...(draft && !cancelled ? { draft } : {}) })
  void refreshEvents()
}

/** The calendar accepted a change: swap the old row for the answer and move on to a queued `next`. */
async function settle(sent: EventCommand, res: CalendarEventUpdate | null): Promise<void> {
  let lost: EventCommand | null = null
  await db.transaction('rw', [db.calendarEvents, db.eventOutbox], async () => {
    const cmd = (await db.eventOutbox.get(sent.key)) ?? sent // an edit made while it was in flight sits in `next`
    await db.eventOutbox.delete(cmd.key)
    if (!res) return
    await db.calendarEvents.delete(res.replaces)
    // Every occurrence of an object shares its version: the others take the new one, so a second edit is not stale (D1).
    const rev = res.series?.rev
    if (rev) await db.calendarEvents.filter((e) => e.series?.id === res.series!.id).modify((e) => { e.rev = rev; if (e.series) e.series.rev = rev })
    if (!res.event) { // it left the cached window: nothing to draw, and an edit that waited has no event to go to
      if (cmd.next && !isDelete(cmd.next)) lost = cmd
      return
    }
    swapped.set(res.replaces, res.event.id)
    if (cmd.eventId !== res.replaces) swapped.set(cmd.eventId, res.event.id)
    const next = cmd.next
    const shown = next && !isDelete(next) ? applyEventPatch(res.event, next) : res.event
    if (!next || !isDelete(next)) await db.calendarEvents.put(shown)
    if (next && (isDelete(next) || !isNoChange(res.event, next))) {
      const key = eventKey(cmd.connectionId, res.event.id)
      await db.eventOutbox.put({ key, connectionId: cmd.connectionId, eventId: res.event.id, rev: res.event.rev ?? cmd.rev, base: res.event, patch: next, notBefore: 0, tries: 0, ...(cmd.nextScope ? { scope: cmd.nextScope } : {}) })
      again = true // the edit that waited is due now
    }
  })
  if (lost) notify({ code: 'event_changed', title: (lost as EventCommand).base.summary, message: '' })
  if (res?.refetch) void refetchEvents() // other occurrences changed: the table is replaced from one request
}

async function sendOne(cmd: EventCommand): Promise<void> {
  const now = wantNow
  await db.eventOutbox.update(cmd.key, { sending: true })
  try {
    if (isDelete(cmd.patch)) {
      await (cmd.scope ? api.deleteCalendarEvent(cmd.connectionId, cmd.eventId, cmd.rev, cmd.scope) : api.deleteCalendarEvent(cmd.connectionId, cmd.eventId, cmd.rev))
      await settle(cmd, null)
    } else if (isCreate(cmd.patch)) {
      await settle(cmd, { event: (await api.createCalendarEvent(cmd.connectionId, createBodyOf(cmd.patch.create))).event, replaces: cmd.eventId }) // 200 (a replay) and 201 both end it
    } else {
      await settle(cmd, await api.updateCalendarEvent(cmd.connectionId, cmd.eventId, { rev: cmd.rev, ...cmd.patch, ...(cmd.scope ? { scope: cmd.scope } : {}) }))
    }
  } catch (err) {
    if (err instanceof ApiError && REFUSED.has(err.status) && !RETRY_CODES.has(err.code)) {
      await rollBack((await db.eventOutbox.get(cmd.key)) ?? cmd, err)
      return
    }
    // Offline, a server error or a refused sign-in: kept, tried again later.
    const kept = await db.eventOutbox.get(cmd.key)
    if (kept) await db.eventOutbox.put({ ...kept, sending: false, tries: kept.tries + 1, notBefore: now + backoff(kept.tries) })
  }
}

/**
 * Sends the queued commands that are due, one at a time in order. Runs after every edit, when
 * the device comes online, at start and after the calendar views refresh; one run at a time.
 */
export function flushEventEdits(now = Date.now()): Promise<void> {
  wantNow = Math.max(wantNow, now)
  if (flushing) { again = true; return flushing }
  flushing = (async () => {
    try {
      if (!started) {
        started = true
        await db.eventOutbox.toCollection().modify({ sending: false }) // a send a reload cut short
      }
      do {
        again = false
        for (const due of await db.eventOutbox.where('notBefore').belowOrEqual(wantNow).toArray()) {
          const cmd = await db.eventOutbox.get(due.key)
          if (cmd && !cmd.sending && cmd.notBefore <= wantNow) await sendOne(cmd)
        }
      } while (again)
    } catch {
      // The database closed under us (sign-out, a test): what waits stays queued.
    } finally {
      flushing = null
      void armTimer().catch(() => undefined)
    }
  })()
  return flushing
}

/** Starts the outbox for this session: sends what waits, and again whenever the device is online or the views refresh. */
export function startEventOutbox(): () => void {
  const offs = [platform.network.onOnline(() => void flushEventEdits()), onEventsRefreshed(() => void flushEventEdits())]
  void flushEventEdits()
  return () => offs.forEach((off) => off())
}

/** The queued commands by event id, for the blocks that show a saving state. */
export function useEventCommands(): Map<string, EventCommand> {
  const rows = useLiveQuery(() => db.eventOutbox.toArray(), [])
  return new Map((rows ?? []).map((c) => [c.eventId, c]))
}

/** Why an event cannot be changed, in a sentence; empty when the reason is unknown (a newer server). */
export function eventAccessText(reason: string | undefined): string {
  switch (reason) {
    case 'series_unsupported': return t('This repeat is too complex to change here')
    case 'following_unsupported': return t('Splitting this series would lose its attachments or online meeting')
    case 'has_guests': return t('This event has guests')
    case 'not_organizer': return t('Only the organizer can change this event')
    case 'calendar_read_only': return t('This calendar is read-only')
    case 'edits_off': return t('Editing events is off for this connection')
    case 'no_etag': return t('This calendar did not give a version for this event')
    case 'refresh_pending': return t('Waiting for the calendar to refresh')
    default: return ''
  }
}

/** The toast for a refused edit. */
export function eventEditNoticeText(n: EventEditNotice): string {
  const title = n.title || t('Busy')
  switch (n.code) {
    case 'event_changed': return t('{title} changed in your calendar; your edit was not applied.', { title })
    case 'event_gone': return t('{title} was deleted in your calendar.', { title })
    case 'scope_required': return t('Choose which events to change.')
    case 'event_not_editable': return eventAccessText(n.reason) || n.message
    case 'event_edits_off': return eventAccessText('edits_off')
    case 'calendar_read_only': return eventAccessText('calendar_read_only')
    default: return n.message
  }
}

/** The editor read a newer version of an event than the cache holds (`replaces`): swap it in, as a settled write does. */
export async function adoptEvent(res: { event: CalendarEvent; replaces?: string }): Promise<void> {
  await db.transaction('rw', db.calendarEvents, async () => {
    if (res.replaces && res.replaces !== res.event.id) {
      await db.calendarEvents.delete(res.replaces)
      swapped.set(res.replaces, res.event.id)
    }
    await db.calendarEvents.put(res.event)
  })
}

/** The draft of a queued create, as the grid shows it now (an edit made while it was in flight applied), or null. */
export async function queuedDraft(pendingId: string): Promise<CalendarEventDraft | null> {
  const cmd = (await db.eventOutbox.toArray()).find((c) => c.eventId === pendingId)
  if (!cmd || !isCreate(cmd.patch)) return null
  return cmd.next && !isDelete(cmd.next) ? applyDraftPatch(cmd.patch.create, cmd.next) : cmd.patch.create
}
