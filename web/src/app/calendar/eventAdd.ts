// Adding an event from the editor, and Undo of it (OpenSpec change calendar-event-editor D9, D11). Loaded with the
// editor: the first load holds only the grid's own edits (eventActions).
import { cachedEvent, cancelEventDelete, currentEventId, DELETE_DELAY_MS, editEvent } from '../../lib/calendarEventEdits'
import { isPendingId, setShowEvents, showEvents } from '../../lib/calendarFeeds'
import { createEvent } from '../../lib/eventCreate'
import { t } from '../../lib/i18n'
import type { CalendarEventDraft } from '../../lib/types'
import { reportActionError } from '../actionError'
import { ACTION_TOAST_MS, showBulkToast } from '../bulk/toastStore'

/** Undo of a created event: an unsent create is removed, a created event is deleted with the usual 8 seconds to change one's mind. */
export async function undoCreate(clientId: string): Promise<void> {
  const row = await cachedEvent(currentEventId(`pending:${clientId}`))
  if (!row || !(await editEvent(row, { delete: true }))) return
  if (isPendingId(row.id)) return // not sent: nothing reached the calendar
  const undo = () => { void cancelEventDelete(row).catch(reportActionError) }
  showBulkToast(t('Event removed'), null, { ttl: DELETE_DELAY_MS, action: { label: t('Undo'), run: undo } })
}

/** Adds a new event: shown at once, sent when it can be. Turns calendars on first when this device hides them, and says so. */
export async function addEvent(draft: CalendarEventDraft): Promise<boolean> {
  try {
    const hidden = !(await showEvents())
    if (hidden) await setShowEvents(true)
    await createEvent(draft)
    const undo = () => { void undoCreate(draft.clientId).catch(reportActionError) }
    showBulkToast(hidden ? t('Event added. Calendars are shown again.') : t('Event added'), null, { ttl: ACTION_TOAST_MS, action: { label: t('Undo'), run: undo } })
    return true
  } catch (err) {
    reportActionError(err)
    return false
  }
}
