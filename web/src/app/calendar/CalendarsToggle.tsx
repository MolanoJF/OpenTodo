import { setShowEvents } from '../../lib/calendarFeeds'
import { t } from '../../lib/i18n'
import { MenuItem } from '../../ui'
import { act } from '../actionError'

/** Shared external-calendar visibility in More actions, absent without calendars. */
export function CalendarsToggle({ shown, count }: { shown: boolean; count: number }) {
  if (count === 0) return null
  return <MenuItem label={t('Show other calendars on this device')} icon="calendar-range" checked={shown}
    data-calendars-toggle onSelect={() => act(setShowEvents(!shown))} />
}
