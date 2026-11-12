// The full event editor (OpenSpec change calendar-event-editor D11): one form, two frames. From 1024 px it is a
// panel in the shell's side slot, below that the overlay module's Dialog (a bottom sheet on a phone). Loaded only
// when it opens. Save sends only the fields that changed; Cmd or Ctrl+Enter saves; closing a changed form asks first.
import { useEffect, useRef, useState } from 'react'
import { setNewKind } from '../../lib/writableCalendars'
import { parseTime } from '../../lib/calendar'
import { t } from '../../lib/i18n'
import { Button, ConfirmDialog, Dialog, Icon, Menu, MenuItem, MenuSeparator, Skeleton } from '../../ui'
import { showBulkToast } from '../bulk/toastStore'
import { PanelFrame } from '../PanelFrame'
import { ConvertToTask } from './ConvertToTask'
import { EventForm } from './EventForm'
import { addTaskRequest, type EventEditorRequest } from './eventEditorStore'
import { useEventEditing } from './useEventEditing'

export type EditorFrame = 'panel' | 'dialog'

export function EventPanel({ request, frame, onClose }: { request: EventEditorRequest; frame: EditorFrame; onClose: () => void }) {
  const s = useEventEditing(request, onClose)
  const [asking, setAsking] = useState(false)
  const [converting, setConverting] = useState(false)
  const root = useRef<HTMLDivElement>(null)
  const creating = request.mode === 'create'
  const title = creating ? t('New event') : t('Edit event')
  const attemptClose = () => (s.dirty ? setAsking(true) : onClose())
  // The title takes focus when the panel opens (the Dialog does it through data-autofocus).
  useEffect(() => { if (frame === 'panel') root.current?.querySelector<HTMLElement>('[data-event-form-title]')?.focus() }, [frame, request.key])
  useEffect(() => {
    if (!s.gone) return
    showBulkToast(t('This event was deleted in your calendar.'), null)
    onClose()
  }, [s.gone, onClose])

  const canDelete = !creating && !!s.ev?.access?.delete
  const footer = (
    <div className="flex items-center justify-between gap-2 pt-1">
      {creating || !s.ev ? <span /> : (
        <Menu label={t('More actions')} kind="menu" trigger={<Icon name="ellipsis" />} triggerClassName="ui-btn ui-btn--ghost ui-btn--sm ui-btn--icon" align="start" triggerProps={{ 'data-event-form-menu': '' }}>
          {s.calendars.length > 0 && <MenuItem label={t('Duplicate')} icon="copy" data-event-form-duplicate onSelect={s.duplicate} />}
          <MenuItem label={t('Convert to task…')} icon="list-checks" data-event-form-convert onSelect={() => setConverting(true)} />
          {canDelete && <MenuSeparator />}
          {canDelete && <MenuItem label={t('Delete')} icon="trash-2" danger data-event-form-delete onSelect={() => void s.remove()} />}
        </Menu>
      )}
      <Button variant="primary" type="submit" data-event-form-save disabled={s.invalid || (creating && !s.calendar) || (!creating && !s.dirty)}>{creating ? t('Add event') : t('Save')}</Button>
    </div>
  )
  // Task | Event: Task closes the editor and opens the add dialog at the same place (D10).
  const toTask = () => {
    if (!s.form) return
    const start = parseTime(s.form.startTime)
    const end = parseTime(s.form.endTime)
    const place = { date: s.form.startDate, ...(start !== null ? { start } : {}), ...(start !== null && end !== null && s.form.endDate === s.form.startDate && end > start ? { end } : {}) }
    void setNewKind('task')
    onClose()
    addTaskRequest.set(place)
  }
  const body = s.form ? (
    <EventForm onTask={creating && !request.draft ? toTask : undefined} form={s.form} set={s.set} setAllDay={s.setAllDay} viewerZone={s.viewerZone} floating={s.floating} description={s.description} invalid={s.timesInvalid}
      picker={creating ? { calendars: s.calendars, value: s.calendar?.id ?? '', onChange: s.chooseCalendar } : undefined} calendarLabel={s.eventCalendar}
      onSubmit={() => void s.save()}>
      {footer}
    </EventForm>
  ) : <Skeleton shape="line" count={4} label={t('Loading the event')} />
  const inner = (
    <div ref={root} data-event-editor-frame={frame} onKeyDown={(e) => { if ((e.metaKey || e.ctrlKey) && e.key === 'Enter') { e.preventDefault(); void s.save() } }}>
      {body}
      {asking && <ConfirmDialog title={t('Discard changes?')} consequence={t('What you changed in this event is not saved.')} confirmLabel={t('Discard')} cancelLabel={t('Keep editing')} onConfirm={onClose} onCancel={() => setAsking(false)} />}
      {converting && s.ev && s.form && (
        <ConvertToTask ev={s.ev} calendar={s.eventCalendar?.name} getFields={() => s.form!} onClose={() => setConverting(false)} />
      )}
    </div>
  )
  return frame === 'panel'
    ? <PanelFrame title={title} testId="event-editor" onClose={attemptClose}>{inner}</PanelFrame>
    : <Dialog title={title} onClose={attemptClose} size="md" placement="sheet" testId="event-editor-dialog">{inner}</Dialog>
}

