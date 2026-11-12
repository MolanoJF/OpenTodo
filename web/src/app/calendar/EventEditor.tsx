// The event popover's fields and Delete (OpenSpec change calendar-event-edits D7): the
// one-click alternative to every drag of an event (WCAG 2.5.7), producing the same command as
// a drop. A locked event says why instead; Save and Delete go through app/calendar/eventActions.
import { useState } from 'react'
import { Button, Field, Input } from '../../ui'
import { eventFormChange, eventFormOf, type EventForm } from '../../lib/eventDrop'
import { t } from '../../lib/i18n'
import type { CalendarEvent } from '../../lib/types'
import { changeEvent, deleteEvent } from './eventActions'

/** Title, date, start and end, Save, and Delete when the calendar allows it. */
export function EventEditor({ ev, zone, onDone }: { ev: CalendarEvent; zone?: string; onDone: () => void }) {
  const [form, setForm] = useState<EventForm>(() => eventFormOf(ev, zone))
  const change = eventFormChange(ev, form, zone)
  const invalid = change === 'invalid' || form.title.trim() === ''
  const set = (patch: Partial<EventForm>) => setForm((f) => ({ ...f, ...patch }))
  const save = () => {
    if (invalid) return
    onDone()
    if (change) void changeEvent(ev, change, Object.keys(change).join() === 'summary' ? 'renamed' : 'updated')
  }
  return (
    <form data-event-edit className="space-y-2" onSubmit={(e) => { e.preventDefault(); save() }}>
      <Field label={t('Title')} hideLabel>
        <Input data-event-edit-title data-autofocus value={form.title} maxLength={500} aria-invalid={form.title.trim() === '' || undefined} onChange={(e) => set({ title: e.target.value })} />
      </Field>
      <div className="grid grid-cols-[1fr_auto_auto] items-center gap-1.5">
        <Input data-event-edit-date type="date" aria-label={t('Date')} value={form.date} onChange={(e) => set({ date: e.target.value })} />
        <Input data-event-edit-start type="time" aria-label={t('Start time')} value={form.start} aria-invalid={change === 'invalid' || undefined} onChange={(e) => set({ start: e.target.value })} />
        <Input data-event-edit-end type="time" aria-label={t('End time')} value={form.end} aria-invalid={change === 'invalid' || undefined} onChange={(e) => set({ end: e.target.value })} />
      </div>
      <div className="flex items-center justify-between gap-2 pt-1">
        {ev.access?.delete ? <Button size="sm" variant="ghost" icon="trash-2" data-event-edit-delete onClick={() => { onDone(); void deleteEvent(ev) }}>{t('Delete')}</Button> : <span />}
        <Button size="sm" variant="primary" type="submit" data-event-edit-save disabled={invalid || !change}>{t('Save')}</Button>
      </div>
    </form>
  )
}
