// A connected calendar's event edited from the grid, the keys, the popover or the full editor, with the
// toast and its Undo (OpenSpec changes calendar-event-edits D6, D7 and calendar-event-editor D9, D11).
// Every path ends here, so a drag, an arrow key and the editor's Save make the same command.
import { cachedEvent, cancelEventDelete, currentEventId, DELETE_DELAY_MS, type EditBefore, editEvent, queuedDraft } from '../../lib/calendarEventEdits'
import { isPendingId } from '../../lib/calendarFeeds'
import { needsScope } from '../../lib/eventScope'
import { previewEvent, restoreEvent } from '../../lib/eventPreview'
import { t } from '../../lib/i18n'
import type { CalendarEvent, CalendarEventChange, EventScope } from '../../lib/types'
import { reportActionError } from '../actionError'
import { ACTION_TOAST_MS, showBulkToast } from '../bulk/toastStore'
import { askScope, type ScopeRequest } from './eventScopeStore'

/** What the toast says; a drag that moves says "moved", a rename "renamed", the rest "updated". */
export type EventEditKind = 'moved' | 'renamed' | 'updated'

function label(kind: EventEditKind): string {
  return kind === 'moved' ? t('Event moved') : kind === 'renamed' ? t('Event renamed') : t('Event updated')
}

/**
 * Which events of a series a change of `ev` is for (calendar-event-series D12): undefined for an event that does
 * not repeat (nothing to ask), null when the person cancelled. The change is drawn on the block while the menu is
 * open and the block goes back when it closes. `from` is a selector for the control that asked (Save).
 */
export async function scopeFor(ev: CalendarEvent, change: CalendarEventChange | 'delete', opts: { from?: string; resets?: ScopeRequest['resets'] } = {}): Promise<EventScope | null | undefined> {
  if (!needsScope(ev)) return undefined
  if (change !== 'delete') await previewEvent(ev, change)
  try {
    return await askScope(ev, change, opts)
  } finally {
    await restoreEvent(ev.id)
  }
}

/**
 * Changes an event, offers Undo (the inverse edit). False when the event cannot be edited or the person
 * cancelled the scope menu. `scope` is what an occurrence's change is for; left out, an occurrence asks.
 * `before` is what the editor loaded: the inverse of a description, repeat or reminders edit puts it back.
 * Undo is not offered for "this and following events": it split the series and there is no merge.
 */
export async function changeEvent(ev: CalendarEvent, change: CalendarEventChange, kind: EventEditKind = 'updated', before?: EditBefore, scope?: EventScope): Promise<boolean> {
  try {
    const chosen = scope ?? await scopeFor(ev, change)
    if (chosen === null) return false
    const inverse = await editEvent(ev, change, Date.now(), before, chosen)
    if (!inverse || 'delete' in inverse) return false
    const undo = () => { void cachedEvent(currentEventId(ev.id)).then((row) => editEvent(row ?? ev, inverse, Date.now(), undefined, chosen)).catch(reportActionError) }
    // No Undo for a split, nor for a repeat change whose previous rule is not known (a single event that began to repeat).
    const undoable = chosen !== 'following' && !(change.repeat !== undefined && before?.repeat === undefined)
    showBulkToast(label(kind), null, { ttl: ACTION_TOAST_MS, ...(undoable ? { action: { label: t('Undo'), run: undo } } : {}) })
    return true
  } catch (err) {
    reportActionError(err)
    return false
  }
}

/**
 * Hides an event and queues its delete after the pause (no toast); returns what puts it back, or null when it cannot be deleted.
 * A pending event that was not sent makes no request at all; putting it back makes the create again.
 */
export async function queueDelete(ev: CalendarEvent, scope?: EventScope): Promise<(() => Promise<void>) | null> {
  const row = (await cachedEvent(currentEventId(ev.id))) ?? ev
  const draft = isPendingId(row.id) ? await queuedDraft(row.id) : null
  if (!(await editEvent(row, { delete: true }, Date.now(), undefined, scope))) return null
  // Putting back a create that was never sent makes it again (the code that makes events loads only then).
  return async () => { if (!(await cancelEventDelete(row)) && draft) await (await import('../../lib/eventCreate')).createEvent(draft) }
}

/** Deletes an event after a pause the toast's Undo can use: the event is gone from the grid at once. An occurrence asks which events first. */
export async function deleteEvent(ev: CalendarEvent, scope?: EventScope): Promise<boolean> {
  try {
    const chosen = scope ?? await scopeFor(ev, 'delete')
    if (chosen === null) return false
    const restore = await queueDelete(ev, chosen)
    if (!restore) return false
    showBulkToast(t('Event deleted'), null, { ttl: DELETE_DELAY_MS, action: { label: t('Undo'), run: () => void restore().catch(reportActionError) } })
    return true
  } catch (err) {
    reportActionError(err)
    return false
  }
}
