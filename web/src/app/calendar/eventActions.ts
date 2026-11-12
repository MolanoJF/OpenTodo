// A connected calendar's event edited from the grid, the keys, the popover or the full editor, with the
// toast and its Undo (OpenSpec changes calendar-event-edits D6, D7 and calendar-event-editor D9, D11).
// Every path ends here, so a drag, an arrow key and the editor's Save make the same command.
import { cachedEvent, cancelEventDelete, currentEventId, DELETE_DELAY_MS, editEvent, queuedDraft } from '../../lib/calendarEventEdits'
import { isPendingId } from '../../lib/calendarFeeds'
import { t } from '../../lib/i18n'
import type { CalendarEvent, CalendarEventChange } from '../../lib/types'
import { reportActionError } from '../actionError'
import { ACTION_TOAST_MS, showBulkToast } from '../bulk/toastStore'

/** What the toast says; a drag that moves says "moved", a rename "renamed", the rest "updated". */
export type EventEditKind = 'moved' | 'renamed' | 'updated'

function label(kind: EventEditKind): string {
  return kind === 'moved' ? t('Event moved') : kind === 'renamed' ? t('Event renamed') : t('Event updated')
}

/**
 * Changes an event, offers Undo (the inverse edit). False when the event cannot be edited.
 * `before.description` is the text the editor loaded: the inverse of a description edit puts it back.
 */
export async function changeEvent(ev: CalendarEvent, change: CalendarEventChange, kind: EventEditKind = 'updated', before?: { description?: string }): Promise<boolean> {
  try {
    const inverse = await editEvent(ev, change, Date.now(), before)
    if (!inverse || 'delete' in inverse) return false
    const undo = () => { void cachedEvent(currentEventId(ev.id)).then((row) => editEvent(row ?? ev, inverse)).catch(reportActionError) }
    showBulkToast(label(kind), null, { ttl: ACTION_TOAST_MS, action: { label: t('Undo'), run: undo } })
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
export async function queueDelete(ev: CalendarEvent): Promise<(() => Promise<void>) | null> {
  const row = (await cachedEvent(currentEventId(ev.id))) ?? ev
  const draft = isPendingId(row.id) ? await queuedDraft(row.id) : null
  if (!(await editEvent(row, { delete: true }))) return null
  // Putting back a create that was never sent makes it again (the code that makes events loads only then).
  return async () => { if (!(await cancelEventDelete(row)) && draft) await (await import('../../lib/eventCreate')).createEvent(draft) }
}

/** Deletes an event after a pause the toast's Undo can use: the event is gone from the grid at once. */
export async function deleteEvent(ev: CalendarEvent): Promise<boolean> {
  try {
    const restore = await queueDelete(ev)
    if (!restore) return false
    showBulkToast(t('Event deleted'), null, { ttl: DELETE_DELAY_MS, action: { label: t('Undo'), run: () => void restore().catch(reportActionError) } })
    return true
  } catch (err) {
    reportActionError(err)
    return false
  }
}
