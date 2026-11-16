// The editor's Video call (OpenSpec change calendar-event-guests D10, D13): "Add a Teams meeting" where the calendar can
// make one and the event has none, the Join button where it has one. Once a Teams meeting is added the calendar keeps it:
// the switch says so, and there is no switch to remove one.
import { t } from '../../lib/i18n'
import type { CalendarConference } from '../../lib/types'
import { Switch } from '../../ui'
import { JoinButton } from './JoinButton'

export interface VideoCallProps {
  /** The calendar can make an online meeting. */
  available: boolean
  /** The event already has a call. */
  conference?: CalendarConference
  value: boolean
  onChange: (on: boolean) => void
}

export function EventVideoCall({ available, conference, value, onChange }: VideoCallProps) {
  if (!conference && !available) return null
  return (
    <div className="space-y-1.5" data-event-form-video-call>
      <p className="text-body font-medium text-default">{t('Video call')}</p>
      {conference
        ? <JoinButton ev={{ conference, summary: '' }} />
        : <Switch checked={value} onChange={onChange} label={t('Add a Teams meeting')} description={t("A Teams meeting can't be removed from here once it is added.")} />}
    </div>
  )
}
