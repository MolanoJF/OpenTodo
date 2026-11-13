// "Join Google Meet" (OpenSpec change calendar-event-guests D10, D13, D17): an anchor to an event's call, drawn from the
// cached row with no request. It opens in a new tab with no opener and no referrer, is named by the service with the
// event's title as its description, shows the host in its tooltip, and is drawn only for an `https` address (joinOf checks it
// again here, so a stale or hostile cached row cannot make a `javascript:` link).
import { useId } from 'react'
import { joinOf } from '../../lib/eventConference'
import { t } from '../../lib/i18n'
import type { CalendarEvent } from '../../lib/types'
import { Icon, Tooltip } from '../../ui'

export function JoinButton({ ev, className = '' }: { ev: Pick<CalendarEvent, 'conference' | 'summary'>; className?: string }) {
  const titleId = useId()
  const join = joinOf(ev)
  if (!join) return null
  return (
    <>
      <Tooltip label={join.host}>
      <a href={join.url} target="_blank" rel="noopener noreferrer" aria-describedby={titleId} data-event-join
        className={`ui-btn ui-btn--secondary ui-btn--sm shrink-0 ${className}`}>
        {join.label}
        <Icon name="external-link" size="xs" />
        <span className="sr-only">{t('opens in a new tab')}</span>
      </a>
      </Tooltip>
      <span id={titleId} hidden>{ev.summary || t('Busy')}</span>
    </>
  )
}
