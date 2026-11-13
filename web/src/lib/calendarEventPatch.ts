// The patches of queued edits of connected calendars' events (OpenSpec change calendar-event-edits D6): the row an
// edit draws, the pending row of a create, the draft with a change merged in and the queued commands drawn over fetched
// rows. Split from calendarFeeds so the first load (which only reads the cache) does not carry them: the calendar's
// own code imports this, and a refresh loads it only while commands wait.
import type { CalendarEvent, CalendarEventChange, CalendarEventDraft, EventCommand } from './types'

/** The event with an edit applied: the fields the change names, nothing else. */
export function applyEventPatch(ev: CalendarEvent, patch: CalendarEventChange): CalendarEvent {
  return {
    ...ev,
    ...(patch.summary !== undefined ? { summary: patch.summary } : {}),
    ...(patch.start && patch.end ? { start: patch.start, end: patch.end, allDay: patch.allDay ?? ev.allDay } : {}),
    ...(patch.location !== undefined ? { location: patch.location } : {}),
    ...(patch.free !== undefined ? { free: patch.free } : {}),
  }
}

/** The id of an event drawn before the calendar has it (calendar-event-editor D3). */
export const PENDING_PREFIX = 'pending:'
export const isPendingId = (id: string): boolean => id.startsWith(PENDING_PREFIX)

/** The row the grid draws for a queued create: no `rev`, editable and deletable until the calendar answers. */
export function pendingRowOf(d: CalendarEventDraft): CalendarEvent {
  return {
    id: PENDING_PREFIX + d.clientId, subscriptionId: d.subscriptionId, uid: d.clientId, summary: d.summary, location: d.location,
    start: d.start, end: d.end, allDay: d.allDay, free: d.free, access: { update: true, delete: true },
  }
}

/** The draft with a change merged in (later fields win); a change of the times always carries both points. */
export function applyDraftPatch(d: CalendarEventDraft, patch: CalendarEventChange): CalendarEventDraft {
  return {
    ...d,
    ...(patch.summary !== undefined ? { summary: patch.summary } : {}),
    ...(patch.start && patch.end ? { start: patch.start, end: patch.end, allDay: patch.allDay ?? d.allDay } : {}),
    ...(patch.location !== undefined ? { location: patch.location } : {}),
    ...(patch.description !== undefined ? { description: patch.description } : {}),
    ...(patch.free !== undefined ? { free: patch.free } : {}),
    ...(patch.repeat ? { repeat: patch.repeat } : {}),
    ...(patch.reminders ? { reminders: patch.reminders } : {}),
  }
}

/** Whether a change of `ev` changes nothing: a description, the repeat and the reminders are never on the row, so they always count. */
export const isNoChange = (ev: CalendarEvent, patch: CalendarEventChange): boolean =>
  patch.description === undefined && patch.repeat === undefined && patch.reminders === undefined && Object.keys(eventPatchBetween(ev, applyEventPatch(ev, patch))).length === 0

const pointKey = (p: CalendarEvent['start']) => `${p.date}|${p.time ?? ''}|${p.tz ?? ''}`

/** The edit that turns `from` into `to` (empty when they are the same): the Undo of a change and the rest of a delete. */
export function eventPatchBetween(from: CalendarEvent, to: CalendarEvent): CalendarEventChange {
  const out: CalendarEventChange = {}
  if (from.summary !== to.summary) out.summary = to.summary
  if (from.location !== to.location) out.location = to.location
  if (from.free !== to.free) out.free = to.free
  if (pointKey(from.start) !== pointKey(to.start) || pointKey(from.end) !== pointKey(to.end) || from.allDay !== to.allDay) {
    out.start = to.start
    out.end = to.end
    out.allDay = to.allDay
  }
  return out
}

/** Draws the queued edits over fetched rows, matched by event id: a queued delete hides the event. */
export function overlayQueued(events: Map<string, CalendarEvent>, commands: readonly EventCommand[]): void {
  for (const cmd of commands) {
    if ('create' in cmd.patch) { // a queued create: the pending row, with what waits in `next`
      const row = pendingRowOf(cmd.patch.create)
      if (!cmd.next) events.set(row.id, row)
      else if (!('delete' in cmd.next)) events.set(row.id, applyEventPatch(row, cmd.next))
      continue
    }
    const ev = events.get(cmd.eventId)
    if (!ev) continue
    if ('delete' in cmd.patch) { if (!cmd.failed) events.delete(cmd.eventId); continue } // a delete that failed stays drawn, marked
    const next = cmd.next
    if (next && 'delete' in next && !cmd.failed) { events.delete(cmd.eventId); continue }
    if (next && 'delete' in next) { events.set(cmd.eventId, applyEventPatch(ev, cmd.patch)); continue }
    events.set(cmd.eventId, applyEventPatch(applyEventPatch(ev, cmd.patch), next ?? {}))
  }
}
