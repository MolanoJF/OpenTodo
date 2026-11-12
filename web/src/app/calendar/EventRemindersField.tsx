// The editor's reminders (OpenSpec change calendar-event-series D12): the list of "time before" rows with Remove, a
// menu of presets to add one, at most `max` (one for a Microsoft calendar), the reminders the form cannot show are kept
// and counted, and a line that the calendar app sends them, not OpenTodo. In the editor's lazy chunk with the form.
import { addReminder, reminderLabel, reminderPresets, removeReminder } from '../../lib/eventReminders'
import { t } from '../../lib/i18n'
import { Icon, IconButton, Menu, MenuItem, Skeleton } from '../../ui'

export interface RemindersFieldProps {
  value: number[]
  onChange: (minutes: number[]) => void
  allDay: boolean
  /** At most this many (5, and 1 for Microsoft). */
  max: number
  microsoft: boolean
  /** Reminders the form cannot show and keeps. */
  other: number
  /** `unavailable` is offline (or a failed read) for an existing event: the list is left as it is and not sent. */
  state: 'ready' | 'loading' | 'unavailable'
  reason?: 'offline' | 'failed'
}

export function EventRemindersField({ value, onChange, allDay, max, microsoft, other, state, reason }: RemindersFieldProps) {
  const presets = reminderPresets(allDay, microsoft).filter((m) => !value.includes(m))
  const full = value.length >= max
  return (
    <div className="space-y-1.5" data-event-form-reminders>
      <p className="text-body font-medium text-default">{t('Reminders')}</p>
      {state === 'loading' && <Skeleton shape="line" count={1} label={t('Loading the reminders')} />}
      {state === 'unavailable' && <p className="text-meta text-muted">{reason === 'offline' ? t('You are offline, so the reminders are left as they are.') : t('The reminders could not be loaded, so they are left as they are.')}</p>}
      {state === 'ready' && (
        <>
          {value.length > 0 && (
            <ul className="space-y-1">
              {value.map((m) => (
                <li key={m} className="flex items-center justify-between gap-2 text-body text-default" data-event-form-reminder={m}>
                  <span className="flex items-center gap-1.5"><Icon name="bell" size="xs" />{reminderLabel(m, allDay)}</span>
                  <IconButton icon="x" size="sm" label={t('Remove reminder')} onClick={() => onChange(removeReminder(value, m))} />
                </li>
              ))}
            </ul>
          )}
          {!full && presets.length > 0 && (
            <Menu label={t('Add reminder')} kind="menu" trigger={<><Icon name="plus" size="sm" />{t('Add reminder')}</>} triggerClassName="ui-btn ui-btn--ghost ui-btn--sm" align="start" triggerProps={{ 'data-event-form-add-reminder': '' }}>
              {presets.map((m) => <MenuItem key={m} label={reminderLabel(m, allDay)} data-reminder-preset={m} onSelect={() => onChange(addReminder(value, m))} />)}
            </Menu>
          )}
          {full && microsoft && <p className="text-meta text-muted" data-event-form-reminder-limit>{t('This calendar takes one reminder.')}</p>}
          {other > 0 && <p className="text-meta text-muted" data-event-form-reminders-other>{t('{n, plural, one {# other reminder is kept} other {# other reminders are kept}}', { n: other })}</p>}
          <p className="text-meta text-faint">{t('The calendar app sends reminders; OpenTodo does not.')}</p>
        </>
      )}
    </div>
  )
}
