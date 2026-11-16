import { useEffect, useRef, type ReactNode } from 'react'
import { t } from '../../lib/i18n'
import { repeatSummary } from '../../lib/eventRepeat'
import { reminderLabel } from '../../lib/eventReminders'
import type { EventFull } from '../../lib/eventFull'
import { Icon, Switch, type IconName } from '../../ui'

/** Native disclosures keep optional controls out of the initial phone layout. */
function MobileField({ icon, label, summary, forceOpen, children }: { icon: IconName; label: string; summary?: ReactNode; forceOpen?: boolean; children: ReactNode }) {
  const ref = useRef<HTMLDetailsElement>(null)
  useEffect(() => { if (forceOpen && ref.current) ref.current.open = true }, [forceOpen])
  return <details ref={ref} className="group min-w-0" data-event-mobile-field={label}>
    <summary className="flex min-h-12 cursor-pointer list-none items-center gap-3 py-2 text-body outline-none focus-visible:ring-2 focus-visible:ring-focus [&::-webkit-details-marker]:hidden">
      <Icon name={icon} className="shrink-0 text-muted" />
      <span className="min-w-0 flex-1">
        <span className="block text-default">{label}</span>
        {summary && <span className="block truncate text-meta text-muted">{summary}</span>}
      </span>
      <Icon name="chevron-down" size="sm" className="shrink-0 text-muted group-open:rotate-180" />
    </summary>
    <div className="min-w-0 space-y-3 pb-3 pt-1">{children}</div>
  </details>
}

export function EventMobileFields({ form, setAllDay, title, when, whenSummary, zone, zoneSummary, repeat, repeatNote, reminders, remindersNote, calendar, calendarName, guests, guestSummary, videoCall, location, notes, showAs, invalid }: {
  form: EventFull; setAllDay: (on: boolean) => void; title: ReactNode; when: ReactNode; whenSummary: ReactNode
  zone?: ReactNode; zoneSummary: string; repeat?: ReactNode; reminders?: ReactNode; calendar: ReactNode; calendarName: string
  guests?: ReactNode; guestSummary?: string; repeatNote?: string; remindersNote?: string; videoCall?: ReactNode; location: ReactNode; notes: ReactNode; showAs: ReactNode; invalid: boolean
}) {
  return <div className="min-w-0 divide-y divide-soft pb-safe" data-event-mobile-fields>
    <div className="min-w-0 pb-4 pt-2">{title}</div>
    <MobileField icon="clock" label={t('When')} summary={whenSummary} forceOpen={invalid}>{when}</MobileField>
    <div className="flex min-h-12 items-center justify-between gap-3 py-1 pl-7">
      <span className="text-body text-default">{t('All day')}</span><Switch label={t('All day')} hideLabel checked={form.allDay} onChange={setAllDay} />
    </div>
    {zone && <MobileField icon="globe" label={t('Time zone')} summary={zoneSummary}>{zone}</MobileField>}
    {repeat && <MobileField icon="repeat" label={t('Repeat')} summary={repeatNote ?? (form.repeat ? repeatSummary(form.repeat, form.startDate) : t('Does not repeat'))}>{repeat}</MobileField>}
    {guests && <MobileField icon="users" label={t('Guests')} summary={guestSummary}>{guests}</MobileField>}
    {videoCall && <MobileField icon="link" label={t('Video call')}>{videoCall}</MobileField>}
    <MobileField icon="map-pin" label={t('Location')} summary={form.location}>{location}</MobileField>
    <MobileField icon="list" label={t('Description')} summary={form.description}>{notes}</MobileField>
    <MobileField icon="calendar-days" label={t('Calendar')} summary={calendarName}>{calendar}</MobileField>
    <MobileField icon="eye" label={t('Show as')} summary={form.free ? t('Free') : t('Busy')}>{showAs}</MobileField>
    {reminders && <MobileField icon="bell" label={t('Reminders')} summary={remindersNote ?? (form.reminders.length ? form.reminders.map((m) => reminderLabel(m, form.allDay)).join(', ') : t('Add reminder'))}>{reminders}</MobileField>}
  </div>
}
