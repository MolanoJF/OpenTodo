// The full event editor's fields (OpenSpec change calendar-event-editor D11): title, All day, start and end,
// time zone, calendar, location, description, Show as. A controlled form: the panel owns the state and the
// Save; the date and time inputs are also the click alternative to the grid's sweep (WCAG 2.5.7).
import { startInViewerZone, type EventFull } from '../../lib/eventFull'
import { t } from '../../lib/i18n'
import type { CalendarSubscription } from '../../lib/types'
import { Combobox, Field, hueFor, Input, SegmentedControl, Skeleton, Switch, Textarea } from '../../ui'
import { useDateTimeFormat } from '../DateTimePrefsProvider'
import { EventRemindersField, type RemindersFieldProps } from './EventRemindersField'
import { EventRepeatField, type RepeatFieldProps } from './EventRepeatField'
import { EventZoneField } from './EventZoneField'
import type { DescriptionState } from './useEventDetail'

export const DESCRIPTION_MAX = 10_000

export interface EventFormProps {
  form: EventFull
  set: (patch: Partial<EventFull>) => void
  setAllDay: (on: boolean) => void
  viewerZone: string
  /** A floating event keeps "Floating" among its zones. */
  floating: boolean
  /** Create mode: the writable calendars to choose from; edit mode: the event's calendar as a label. */
  picker?: { calendars: readonly CalendarSubscription[]; value: string; onChange: (id: string) => void }
  calendarLabel?: CalendarSubscription
  description: DescriptionState
  /** Create mode: the Task | Event control; choosing Task goes back to the add dialog. */
  onTask?: () => void
  invalid: boolean
  /** The repeat and the reminders (calendar-event-series D12); absent where the form has no calendar to ask (a story). */
  repeat?: Omit<RepeatFieldProps, 'startDate'>
  reminders?: Omit<RemindersFieldProps, 'allDay'>
  onSubmit: () => void
  children?: React.ReactNode
}

const dot = (c: Pick<CalendarSubscription, 'color'>) => <span className="h-2 w-2 shrink-0 rounded-full bg-hue-solid" data-hue={hueFor(c.color)} aria-hidden />

function descriptionNote(d: DescriptionState): string {
  if (d.status === 'unavailable') return d.reason === 'offline' ? t('You are offline, so the description is left as it is.') : t('The description could not be loaded, so it is left as it is.')
  if (d.status === 'ready' && !d.editable) return t('This description has formatting, so it is shown as plain text and cannot be changed here.')
  return ''
}

export function EventForm({ form, set, setAllDay, viewerZone, floating, picker, calendarLabel, description, onTask, invalid, repeat, reminders, onSubmit, children }: EventFormProps) {
  const dt = useDateTimeFormat()
  const inYourTime = startInViewerZone(form, viewerZone)
  const note = descriptionNote(description)
  const fixed = description.status !== 'ready' || !description.editable
  const bad = invalid || undefined
  return (
    <form data-event-form className="space-y-4" onSubmit={(e) => { e.preventDefault(); onSubmit() }}>
      {onTask && (
        <SegmentedControl<'task' | 'event'> label={t('Add a')} options={[{ id: 'task', label: t('Task') }, { id: 'event', label: t('Event') }]} value="event" data-add-kind onChange={(k) => { if (k === 'task') onTask() }} />
      )}
      <Field label={t('Title')} hideLabel>
        <Input data-event-form-title data-autofocus value={form.title} maxLength={500} placeholder={t('Event title')} aria-invalid={form.title.trim() === '' && invalid ? true : undefined} onChange={(e) => set({ title: e.target.value })} />
      </Field>
      <Switch checked={form.allDay} onChange={setAllDay} label={t('All day')} />
      <fieldset className="space-y-1.5">
        <legend className="text-body font-medium text-default">{t('When')}</legend>
        <div className="grid grid-cols-[1fr_auto] gap-1.5">
          <Input data-event-form-start type="date" aria-label={t('Start date')} value={form.startDate} aria-invalid={bad} onChange={(e) => set({ startDate: e.target.value })} />
          {!form.allDay && <Input data-event-form-start-time type="time" aria-label={t('Start time')} value={form.startTime} aria-invalid={bad} onChange={(e) => set({ startTime: e.target.value })} />}
          <Input data-event-form-end type="date" aria-label={form.allDay ? t('Last day') : t('End date')} value={form.endDate} aria-invalid={bad} onChange={(e) => set({ endDate: e.target.value })} />
          {!form.allDay && <Input data-event-form-end-time type="time" aria-label={t('End time')} value={form.endTime} aria-invalid={bad} onChange={(e) => set({ endTime: e.target.value })} />}
        </div>
        {invalid && <p role="alert" className="text-meta text-danger" data-event-form-invalid>{t('The event must end after it starts.')}</p>}
        {inYourTime && (
          <p className="text-meta text-muted" data-event-form-your-time>
            {inYourTime.date === form.startDate
              ? t('{time} in your time', { time: dt.formatTime(inYourTime.time) })
              : t('{date}, {time} in your time', { date: dt.formatDate(inYourTime.date, 'compact'), time: dt.formatTime(inYourTime.time) })}
          </p>
        )}
      </fieldset>
      {!form.allDay && <EventZoneField zone={form.zone} viewerZone={viewerZone} floatingAllowed={floating} onChange={(zone) => set({ zone })} />}
      {repeat && <EventRepeatField {...repeat} startDate={form.startDate} />}
      {reminders && <EventRemindersField {...reminders} allDay={form.allDay} />}
      <div className="space-y-1.5">
        <p className="text-body font-medium text-default">{t('Calendar')}</p>
        {picker
          ? <Combobox<CalendarSubscription> label={t('Calendar')} placeholder={picker.calendars.find((c) => c.id === picker.value)?.name ?? t('Choose a calendar')} source={picker.calendars}
              getKey={(c) => c.id} getLabel={(c) => c.name} selected={[picker.value]} renderLeading={dot} emptyText={t('No calendar allows new events.')} onPick={(c) => picker.onChange(c.id)} />
          : calendarLabel && <p className="flex items-center gap-1.5 text-body text-default" data-event-form-calendar>{dot(calendarLabel)}{calendarLabel.name}</p>}
      </div>
      <Field label={t('Location')}>
        <Input data-event-form-location value={form.location} maxLength={500} onChange={(e) => set({ location: e.target.value })} />
      </Field>
      <Field label={t('Description')} description={note || undefined}>
        {description.status === 'loading'
          ? <Skeleton shape="line" count={3} label={t('Loading the description')} />
          : <Textarea data-event-form-description rows={4} maxLength={DESCRIPTION_MAX} value={description.status === 'ready' ? form.description : ''} disabled={description.status === 'unavailable'} readOnly={fixed}
              onChange={(e) => set({ description: e.target.value })} />}
      </Field>
      <div className="space-y-1.5">
        <p className="text-body font-medium text-default">{t('Show as')}</p>
        <SegmentedControl<'busy' | 'free'> label={t('Show as')} options={[{ id: 'busy', label: t('Busy') }, { id: 'free', label: t('Free') }]} value={form.free ? 'free' : 'busy'} onChange={(v) => set({ free: v === 'free' })} />
      </div>
      {children}
    </form>
  )
}
