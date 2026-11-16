import { addDaysISO, daysBetweenISO, formatShort, localDueOf } from '../../lib/dates'
import { parseTime } from '../../lib/calendar'
import { formatMinutes } from '../../lib/duration'
import { reminderLabel } from '../../lib/eventReminders'
import { t } from '../../lib/i18n'
import type { CalendarEvent, CalendarSubscription } from '../../lib/types'
import { Icon, IconButton, Skeleton, hueFor } from '../../ui'
import { eventTimeText } from '../CalendarEvents'
import { EventLocked } from './EventLocked'
import { EventPopoverActions } from './EventPopoverActions'
import { EventWriteState } from './EventWriteState'
import { PreviewFact } from './PreviewFact'
import { openEventEditor } from './eventEditorStore'
import { useEventDetail } from './useEventDetail'

export default function EventPreview({ ev, sub, zone, onClose }: { ev: CalendarEvent; sub?: CalendarSubscription; zone?: string; onClose?: () => void }) {
  const detail = useEventDetail(ev.id)
  const start = ev.start.time && ev.start.tz ? localDueOf(ev.start, zone) : ev.start
  const end = ev.end.time && ev.end.tz ? localDueOf(ev.end, zone) : ev.end
  const lastDate = ev.allDay ? addDaysISO(end.date, -1) : end.date
  const dates = start.date === lastDate ? formatShort(start.date) : `${formatShort(start.date)} – ${formatShort(lastDate)}`
  const duration = !ev.allDay ? daysBetweenISO(start.date, end.date) * 1440 + (parseTime(end.time) ?? 0) - (parseTime(start.time) ?? 0) : 0
  const description = detail.description
  const edit = () => { onClose?.(); openEventEditor({ mode: 'edit', eventId: ev.id }) }
  return <div data-event-details>
    <header className="hidden sm:flex items-center justify-between gap-2 border-b border-soft px-4 py-2 text-meta text-muted">
      <span className="flex items-center gap-2"><Icon name="calendar-days" size="sm" />{t('Event')}</span>
      <div className="flex items-center gap-1">{ev.access?.update && <IconButton tooltip={false} icon="external-link" label={t('Edit event')} size="sm" onClick={edit} />}<IconButton tooltip={false} icon="x" label={t('Close')} size="sm" onClick={() => onClose?.()} /></div>
    </header>
    <div className="space-y-2 p-3">
      <h3 className="hidden sm:block break-words text-body font-semibold text-strong">{ev.summary || t('Busy')}</h3>
      <div className="space-y-1 text-default">
        <PreviewFact icon="clock"><p>{eventTimeText(ev, zone)}{duration > 0 && <span className="ml-2 text-meta text-muted">{formatMinutes(duration)}</span>}</p><p className="text-meta text-muted">{dates}</p></PreviewFact>
        {!ev.allDay && <PreviewFact icon="globe">{zone || ev.start.tz || t('Floating')}</PreviewFact>}
        {ev.location && <PreviewFact icon="map-pin">{ev.location}</PreviewFact>}
        {description.status === 'loading' && <Skeleton shape="line" count={2} label={t('Loading the description')} />}
        {description.status === 'ready' && description.text && <PreviewFact icon="list"><p className="max-h-24 overflow-y-auto whitespace-pre-wrap text-meta">{description.text}</p></PreviewFact>}
        {ev.access && description.status === 'unavailable' && <p className="text-meta text-faint">{description.reason === 'offline' ? t('You are offline, so the description is left as it is.') : t('The description could not be loaded, so it is left as it is.')}</p>}
        {detail.extras?.reminders?.map((m) => <PreviewFact key={m} icon="bell">{reminderLabel(m, ev.allDay)}</PreviewFact>)}
      </div>
      <div className="border-t border-soft pt-2">
        {sub && <PreviewFact icon="calendar-days"><span className="inline-flex items-center gap-2" data-hue={hueFor(sub.color)}><span aria-hidden className="size-2.5 shrink-0 rounded-full bg-hue-solid" />{sub.name}</span></PreviewFact>}
        <p className="pl-7 text-meta text-muted">{ev.free ? t('Free') : t('Busy')}</p>
      </div>
      {ev.access && <EventWriteState id={ev.id} />}
      {ev.access && !ev.access.update && <EventLocked reason={ev.access.reason} sub={sub} />}
      {!ev.access && <p className="text-meta text-muted">{t('From a subscribed calendar. Edit it in its own app.')}</p>}
      <div className="border-t border-soft pt-2"><EventPopoverActions ev={ev} sub={sub} onClose={onClose} /></div>
    </div>
  </div>
}
