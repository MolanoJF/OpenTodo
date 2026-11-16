import { t } from '../../lib/i18n'
import { setCalendarShowCompleted } from '../../lib/localData'
import { MenuItem } from '../../ui'
import { act } from '../actionError'

/** Device-local visibility, always in the calendar overflow menu. */
export function CalendarCompletedToggle({ shown, menu = false }: { shown: boolean; menu?: boolean }) {
  if (!menu) return null
  return <MenuItem label={t('Show completed tasks')} icon="circle-check-big" checked={shown} data-calendar-completed-toggle
    onSelect={() => act(setCalendarShowCompleted(!shown))} />
}
