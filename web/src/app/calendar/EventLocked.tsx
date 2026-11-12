// Why an event cannot be changed (OpenSpec change calendar-event-edits D7). Static: the editor beside it loads lazily.
import { eventAccessText } from '../../lib/calendarEventEdits'
import { t } from '../../lib/i18n'
import { navigate } from '../router'

/** Why an event cannot be changed, with a way to the connection's settings when the switch is what is missing. */
export function EventLocked({ reason }: { reason: string | undefined }) {
  const text = eventAccessText(reason)
  if (!text) return null
  return (
    <p className="text-meta text-faint" data-event-locked={reason}>
      {text}
      {reason === 'edits_off' && (
        <> · <a className="text-accent underline" href="/settings/integrations/calendar-sync"
          onClick={(e) => { e.preventDefault(); navigate({ view: 'settings', section: 'integrations', page: 'calendar-sync' }) }}>{t('Calendar sync settings')}</a></>
      )}
    </p>
  )
}
