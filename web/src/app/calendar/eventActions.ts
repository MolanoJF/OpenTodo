// A connected calendar's event edited from the grid, the keys or the popover, with the
// toast and its Undo (OpenSpec change calendar-event-edits D6, D7). Every path ends here, so a
// drag, an arrow key and the popover's Save make the same command.
import { cachedEvent, cancelEventDelete, DELETE_DELAY_MS, editEvent } from '../../lib/calendarEventEdits'
import { t } from '../../lib/i18n'
import type { CalendarEvent, CalendarEventChange } from '../../lib/types'
import { reportActionError } from '../actionError'
import { ACTION_TOAST_MS, showBulkToast } from '../bulk/toastStore'

/** What the toast says; a drag that moves says "moved", a rename "renamed", the rest "updated". */
export type EventEditKind = 'moved' | 'renamed' | 'updated'

function label(kind: EventEditKind): string {
  return kind === 'moved' ? t('Event moved') : kind === 'renamed' ? t('Event renamed') : t('Event updated')
}

/** Changes an event, offers Undo (the inverse edit). False when the event cannot be edited. */
export async function changeEvent(ev: CalendarEvent, change: CalendarEventChange, kind: EventEditKind = 'updated'): Promise<boolean> {
  try {
    const inverse = await editEvent(ev, change)
    if (!inverse || 'delete' in inverse) return false
    const undo = () => { void cachedEvent(ev.id).then((row) => editEvent(row ?? ev, inverse)).catch(reportActionError) }
    showBulkToast(label(kind), null, { ttl: ACTION_TOAST_MS, action: { label: t('Undo'), run: undo } })
    return true
  } catch (err) {
    reportActionError(err)
    return false
  }
}

/** Deletes an event after a pause the toast's Undo can use: the event is gone from the grid at once. */
export async function deleteEvent(ev: CalendarEvent): Promise<boolean> {
  try {
    const row = (await cachedEvent(ev.id)) ?? ev
    if (!(await editEvent(row, { delete: true }))) return false
    const undo = () => { void cancelEventDelete(row).catch(reportActionError) }
    showBulkToast(t('Event deleted'), null, { ttl: DELETE_DELAY_MS, action: { label: t('Undo'), run: undo } })
    return true
  } catch (err) {
    reportActionError(err)
    return false
  }
}
