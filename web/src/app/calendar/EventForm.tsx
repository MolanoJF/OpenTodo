// The full event editor's fields (OpenSpec change calendar-event-editor D11): title, All day, start and end,
// time zone, calendar, location, description, Show as. A controlled form: the panel owns the state and the
// Save; the date and time inputs are also the click alternative to the grid's sweep (WCAG 2.5.7).
import { startInViewerZone, type EventFull } from '../../lib/eventFull'
import { t } from '../../lib/i18n'
import type { CalendarSubscription } from '../../lib/types'
import { Combobox, Field, hueFor, Icon, Input, SegmentedControl, Skeleton, Switch, Textarea, DESKTOP_HEADER_QUERY, useMediaQuery } from '../../ui'
import { useDateTimeFormat } from '../DateTimePrefsProvider'
import { EventRemindersField, type RemindersFieldProps } from './EventRemindersField'
import { EventVideoCall, type VideoCallProps } from './EventVideoCall'
import { GuestsField, type GuestsFieldProps } from './GuestsField'
import { EventRepeatField, type RepeatFieldProps } from './EventRepeatField'
import { EventMobileFields } from './EventMobileFields'
import { EventZoneField, zoneText } from './EventZoneField'
import type { DescriptionState } from './useEventDetail'
import { PreviewFact } from './PreviewFact'
import type { IconName } from '../../ui/Icon'

export const DESCRIPTION_MAX = 10_000

export interface EventFormProps {
  page?: boolean
  formId?: string
  readOnly?: boolean
  form: EventFull
  set: (patch: Partial<EventFull>) => void
  setAllDay: (on: boolean) => void
  viewerZone: string
  /** A floating event keeps "Floating" among its zones. */
  floating: boolean
  /** Create mode: the writable calendars to choose from; edit mode: the event's calendar as a label. */
  picker?: { calendars: readonly CalendarSubscription[]; value: string; onChange: (id: string) => void }
  calendarLabel?: CalendarSubscription
  calendarNote?: string
  description: DescriptionState
  /** Create mode: the Task | Event control; choosing Task goes back to the add dialog. */
  onTask?: () => void
  invalid: boolean
  /** The repeat and the reminders (calendar-event-series D12); absent where the form has no calendar to ask (a story). */
  repeat?: Omit<RepeatFieldProps, 'startDate'>
  reminders?: Omit<RemindersFieldProps, 'allDay'>
  /** The guests and the video call (calendar-event-guests D13): present where the calendar has them. */
  guests?: GuestsFieldProps
  videoCall?: VideoCallProps
  onSubmit: () => void
  children?: React.ReactNode
}

const dot = (c: Pick<CalendarSubscription, 'color'>) => <span className="h-2 w-2 shrink-0 rounded-full bg-hue-solid" data-hue={hueFor(c.color)} aria-hidden />

function descriptionNote(d: DescriptionState): string {
  if (d.status === 'unavailable') return d.reason === 'offline' ? t('You are offline, so the description is left as it is.') : t('The description could not be loaded, so it is left as it is.')
  if (d.status === 'ready' && !d.editable) return t('This description has formatting, so it is shown as plain text and cannot be changed here.')
  return ''
}

export function EventForm({ page = false, formId, readOnly = false, form, set, setAllDay, viewerZone, floating, picker, calendarLabel, calendarNote, description, onTask, invalid, repeat, reminders, guests, videoCall, onSubmit, children }: EventFormProps) {
  const dt = useDateTimeFormat()
  const mobile = !useMediaQuery(DESKTOP_HEADER_QUERY) && page
  const inYourTime = startInViewerZone(form, viewerZone)
  const note = descriptionNote(description)
  const fixed = description.status !== 'ready' || !description.editable
  const bad = invalid || undefined
  const row = (icon: IconName, content: React.ReactNode) => page ? <PreviewFact icon={icon}>{content}</PreviewFact> : content
  const notes = <Field label={t('Description')} hideLabel={page} description={note || undefined}>
    {description.status === 'loading' ? <Skeleton shape="line" count={3} label={t('Loading the description')} />
      : <Textarea data-event-form-description rows={page ? 9 : 4} maxLength={DESCRIPTION_MAX} placeholder={t('Add a description')} className={page ? 'h-28 resize-none border-transparent bg-transparent px-0 shadow-none lg:h-56' : ''} value={description.status === 'ready' ? form.description : ''} disabled={description.status === 'unavailable'} readOnly={fixed} onChange={(e) => set({ description: e.target.value })} />}
  </Field>

  const whenField = (<fieldset className="min-w-0 space-y-1.5">
        <legend className={mobile ? 'sr-only' : 'text-body font-medium text-default'}>{t('When')}</legend>
        <div className="grid grid-cols-1 gap-1.5 min-[22.5rem]:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]">
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
      </fieldset>)
  const calendarField = (<div className="space-y-1.5">
        <p className="text-body font-medium text-default">{t('Calendar')}</p>
        {picker
          ? <Combobox<CalendarSubscription> label={t('Calendar')} placeholder={picker.calendars.find((c) => c.id === picker.value)?.name ?? t('Choose a calendar')} source={picker.calendars}
              getKey={(c) => c.id} getLabel={(c) => c.name} selected={[picker.value]} renderLeading={dot} emptyText={t('No calendar allows new events.')} onPick={(c) => picker.onChange(c.id)} />
          : calendarLabel && <p className="flex items-center gap-1.5 text-body text-default" data-event-form-calendar>{dot(calendarLabel)}{calendarLabel.name}</p>}
        {calendarNote && <p className="text-meta text-faint">{calendarNote}</p>}
      </div>)
  const showAsField = (<div className="space-y-1.5">
        <p className="text-body font-medium text-default">{t('Show as')}</p>
        <SegmentedControl<'busy' | 'free'> label={t('Show as')} options={[{ id: 'busy', label: t('Busy') }, { id: 'free', label: t('Free') }]} value={form.free ? 'free' : 'busy'} onChange={(v) => set({ free: v === 'free' })} />
      </div>)
  const titleField = (<div className={page ? 'flex items-start gap-3' : ''}>
          {page && !mobile && <Icon name="calendar-days" className="mt-2 shrink-0 text-muted" />}
          <Field label={t('Title')} hideLabel className="min-w-0 flex-1">
            <Input data-event-form-title data-autofocus className={page ? 'border-transparent bg-transparent px-0 text-h1 font-semibold shadow-none' : ''} value={form.title} maxLength={500} placeholder={t('Event title')} aria-invalid={form.title.trim() === '' && invalid ? true : undefined} onChange={(e) => set({ title: e.target.value })} />
          </Field>
        </div>)
  const locationField = (<Field label={t('Location')}>
        <Input data-event-form-location value={form.location} maxLength={500} onChange={(e) => set({ location: e.target.value })} />
      </Field>)
  return (
    <form id={formId} data-event-form data-event-page={page || undefined} className="space-y-6" onSubmit={(e) => { e.preventDefault(); onSubmit() }}>
      {onTask && (
        <SegmentedControl<'task' | 'event'> label={t('Add a')} options={[{ id: 'task', label: t('Task') }, { id: 'event', label: t('Event') }]} value="event" data-add-kind onChange={(k) => { if (k === 'task') onTask() }} />
      )}
      <fieldset disabled={readOnly} className="min-w-0">
      {mobile ? <EventMobileFields form={form} setAllDay={setAllDay} title={titleField} when={whenField}
        whenSummary={<>{dt.formatDate(form.startDate, 'compact')}{form.endDate !== form.startDate ? ` - ${dt.formatDate(form.endDate, 'compact')}` : ''}{!form.allDay && ` / ${dt.formatTime(form.startTime)} - ${dt.formatTime(form.endTime)}`}</>}
        zone={!form.allDay ? <EventZoneField zone={form.zone} viewerZone={viewerZone} floatingAllowed={floating} onChange={(zone) => set({ zone })} /> : undefined} zoneSummary={zoneText(form.zone)}
        repeat={repeat ? <EventRepeatField {...repeat} startDate={form.startDate} /> : undefined}
        repeatNote={repeat?.custom ? t('Custom repeat') : repeat?.state === 'loading' ? t('Loading the repeat') : repeat?.state === 'unavailable' ? t('The repeat could not be loaded, so it is left as it is.') : undefined}
        reminders={reminders ? <EventRemindersField {...reminders} allDay={form.allDay} /> : undefined}
        remindersNote={reminders?.state === 'loading' ? t('Loading the reminders') : reminders?.state === 'unavailable' ? reminders.reason === 'offline' ? t('You are offline, so the reminders are left as they are.') : t('The reminders could not be loaded, so they are left as they are.') : reminders?.other ? t('{n, plural, one {# other reminder is kept} other {# other reminders are kept}}', { n: reminders.other }) : undefined}
        calendar={calendarField} calendarName={picker?.calendars.find((c) => c.id === picker.value)?.name ?? calendarLabel?.name ?? t('Choose a calendar')}
        guests={guests ? <GuestsField {...guests} /> : undefined} guestSummary={guests ? [...(guests.list?.guests ?? []).filter((g) => !guests.removes.includes(g.email.toLowerCase())), ...guests.adds].map((g) => g.email).join(', ') : undefined}
        videoCall={videoCall && (videoCall.available || videoCall.conference) ? <EventVideoCall {...videoCall} /> : undefined}
        location={locationField} notes={notes} showAs={showAsField} invalid={invalid} /> : <div className={page ? 'grid min-w-0 gap-8 lg:grid-cols-[minmax(0,1fr)_20rem]' : 'space-y-4'}>
      <section className={page ? 'min-w-0 space-y-6 lg:pt-2' : 'space-y-4'} data-event-content>
        {titleField}
        {page && row('list', notes)}
        {page && guests && row('users', <GuestsField {...guests} />)}
        {page && videoCall && row('link', <EventVideoCall {...videoCall} />)}
      </section>
      <aside aria-label={t('Facts')} className={page ? 'min-w-0 space-y-4 lg:border-l lg:border-soft lg:pl-8 lg:pt-2' : 'space-y-4'} data-event-facts>
      {page && <h3 className="text-meta font-semibold text-muted">{t('Schedule')}</h3>}
      {row('calendar-days', <Switch checked={form.allDay} onChange={setAllDay} label={t('All day')} />)}
      {row('clock', whenField)}
      {!form.allDay && row('globe', <EventZoneField zone={form.zone} viewerZone={viewerZone} floatingAllowed={floating} onChange={(zone) => set({ zone })} />)}
      {repeat && row('repeat', <EventRepeatField {...repeat} startDate={form.startDate} />)}
      {reminders && row('bell', <EventRemindersField {...reminders} allDay={form.allDay} />)}
      {page && <h3 className="border-t border-soft pt-4 text-meta font-semibold text-muted">{t('Details')}</h3>}
      {row('calendar-days', calendarField)}
      {!page && guests && <GuestsField {...guests} />}
      {!page && videoCall && <EventVideoCall {...videoCall} />}
      {row('map-pin', locationField)}
      {!page && notes}
      {row('eye', showAsField)}
      </aside>
      </div>}
      </fieldset>
      {children && <div data-event-form-footer className={page ? 'border-t border-soft bg-transparent lg:bg-surface pt-3 pb-[max(0.75rem,var(--safe-bottom))] lg:sticky lg:bottom-0' : ''}>{children}</div>}
    </form>
  )
}
