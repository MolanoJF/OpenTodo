// The toasts of calendar event writes that did not go through (OpenSpec change calendar-event-edits D6): a refusal
// says why, a refused create offers Edit again, and a write that failed offers Retry and Discard. Part of the
// calendar's code, not the shell's: loaded with the first event the app draws or edits, it listens once for the
// notices the data layer raises (and for the ones raised before it loaded).
import { discardEventWrite, eventEditNoticeText, retryEventWrite } from '../../lib/calendarEventEdits'
import { onEventEditNotice, type EventEditNotice } from '../../lib/eventEditNotices'
import { resettable } from '../../lib/moduleState'
import { t } from '../../lib/i18n'
import { ACTION_TOAST_MS, hideBulkToast, showActionToast, showBulkToast } from '../bulk/toastStore'
import { navigate } from '../router'
import { openEventEditor } from './eventEditorStore'

function show(notice: EventEditNotice): void {
  const { draft, failure } = notice
  const text = eventEditNoticeText(notice)
  // A write that failed stays on the device: Retry sends it again, Discard puts the event back.
  if (failure) return showBulkToast(text, null, { ttl: 2 * ACTION_TOAST_MS, action: { label: t('Retry'), run: () => void retryEventWrite(failure.key) }, second: { label: t('Discard'), run: () => { void discardEventWrite(failure.key); hideBulkToast() } } })
  if (!draft) return showActionToast(text)
  // A refused create hands back what the person typed: the toast offers to reopen the editor on it (calendar-event-editor D9).
  const again = () => { navigate({ view: 'day', date: draft.start.date }); openEventEditor({ mode: 'create', draft }) }
  showBulkToast(text, null, { ttl: 2 * ACTION_TOAST_MS, action: { label: t('Edit again'), run: again } })
}

let off: (() => void) | null = null
resettable('eventEditToasts', () => { off?.(); off = null })

/** Listens for the notices, once. */
export function hostEventEditToasts(): void {
  off ??= onEventEditNotice(show)
}
hostEventEditToasts()
