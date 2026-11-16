// The full event editor (OpenSpec change calendar-event-editor D11): one form, two frames. From 1024 px it is a
// panel in the shell's side slot, below that the overlay module's Dialog (a bottom sheet on a phone). Loaded only
// when it opens. Save sends only the fields that changed; Cmd or Ctrl+Enter saves; closing a changed form asks first.
import { useEffect, useId, useRef, useState } from 'react'
import { setNewKind } from '../../lib/writableCalendars'
import { parseTime } from '../../lib/calendar'
import { t } from '../../lib/i18n'
import { Button, ConfirmDialog, Dialog, Icon, IconButton, PAGE_FRAME, PageHeader, DESKTOP_HEADER_QUERY, useMediaQuery, Menu, MenuItem, MenuSeparator, Notice, Skeleton, overlayDepth } from '../../ui'
import { useShortcutScope } from '../shortcuts'
import { setLeaveGuard } from '../router'
import { showBulkToast } from '../bulk/toastStore'
import { PanelFrame } from '../PanelFrame'
import { ConvertToTask } from './ConvertToTask'
import { EventForm } from './EventForm'
import { EventWriteState } from './EventWriteState'
import { addTaskRequest, type EventEditorRequest } from './eventEditorStore'
import { useEventEditing } from './useEventEditing'

export type EditorFrame = 'panel' | 'dialog' | 'page' | 'route'

export function EventPanel({ request, frame, onClose }: { request: EventEditorRequest; frame: EditorFrame; onClose: () => void }) {
  const desktop = useMediaQuery(DESKTOP_HEADER_QUERY)
  const formId = useId()
  const phoneHeader = frame === 'route' && !desktop
  const s = useEventEditing(request, () => { if (frame === 'route') setLeaveGuard(null); onClose() })
  const [asking, setAsking] = useState(false)
  const [leaving, setLeaving] = useState<(() => void) | null>(null)
  const [converting, setConverting] = useState(false)
  const root = useRef<HTMLDivElement>(null)
  const creating = request.mode === 'create'
  const readOnly = !creating && !!s.ev && !s.ev.access?.update
  const title = creating ? t('New event') : t('Edit event')
  const attemptClose = () => (s.dirty ? setAsking(true) : onClose())
  // The title takes focus when the panel opens (the Dialog does it through data-autofocus).
  useEffect(() => { if (frame === 'panel') root.current?.querySelector<HTMLElement>('[data-event-form-title]')?.focus() }, [frame, request.key])
  useEffect(() => {
    if (!s.gone) return
    showBulkToast(t('This event was deleted in your calendar.'), null)
    onClose()
  }, [s.gone, onClose])

  useEffect(() => {
    if (frame !== 'route' || !s.dirty) return
    setLeaveGuard((go) => { setLeaving(() => go); setAsking(true); return true })
    return () => setLeaveGuard(null)
  }, [frame, s.dirty])
  // Esc leaves the event page through the shortcut dispatcher (keyboard-shortcuts), unless an overlay above it is open.
  useShortcutScope('view', {}, { view: 'event', enabled: frame === 'route', any: (e) => {
    if (e.key !== 'Escape' || overlayDepth() > 0) return false
    e.preventDefault()
    attemptClose()
    return true
  } })
  const canDelete = !creating && !!s.ev?.access?.delete
  const saveAction = (!readOnly && <Button variant="primary" type="submit" form={formId} size={phoneHeader ? 'sm' : 'md'} busy={s.moving} data-event-form-save disabled={!s.form || s.moving || (s.moveReceipt ? s.moveBlocked : s.invalid || s.offlineBlocked || (creating && !s.calendar) || (!creating && !s.dirty) || s.calendarChanged && s.moveBlocked)}>{s.moveReceipt ? t('Finish move') : s.calendarChanged ? t('Move event') : creating ? t('Add event') : t('Save')}</Button>)
  const eventMenu = (creating ? (!request.draft && phoneHeader ? <Menu label={t('More actions')} kind="menu" trigger={<Icon name="ellipsis" />} triggerClassName="ui-btn ui-btn--ghost ui-btn--sm ui-btn--icon" triggerProps={{ 'data-event-form-menu': '' }}>
      <MenuItem label={t('Task')} icon="list-checks" onSelect={() => toTask()} />
    </Menu> : null) : !s.ev || s.moveReceipt || s.moving ? null : (
        <Menu label={t('More actions')} kind="menu" trigger={<Icon name="ellipsis" />} triggerClassName="ui-btn ui-btn--ghost ui-btn--sm ui-btn--icon" align="start" triggerProps={{ 'data-event-form-menu': '' }}>
          {s.calendars.length > 0 && <MenuItem label={t('Duplicate')} icon="copy" data-event-form-duplicate onSelect={s.duplicate} />}
          <MenuItem label={t('Convert to task…')} icon="list-checks" data-event-form-convert onSelect={() => setConverting(true)} />
          {canDelete && <MenuSeparator />}
          {canDelete && <MenuItem label={t('Delete')} icon="trash-2" danger data-event-form-delete onSelect={() => void s.remove()} />}
        </Menu>
      ))
  const offlineNote = s.offlineBlocked ? <p className="text-meta text-faint" data-event-form-offline>{t("Guests are told when you're online")}</p> : null
  const footer = phoneHeader ? offlineNote : <div className="flex flex-wrap items-center justify-between gap-2 pt-1">{eventMenu ?? <span />}{offlineNote}{saveAction}</div>
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
    <EventForm formId={formId} page={frame === 'page' || frame === 'route'} readOnly={readOnly || !!s.moveReceipt || s.moving} onTask={creating && !request.draft && !phoneHeader ? toTask : undefined} form={s.form} set={s.set} setAllDay={s.setAllDay} viewerZone={s.viewerZone} floating={s.floating} description={s.description} invalid={s.timesInvalid}
      repeat={s.repeatField} reminders={s.remindersField} guests={s.guestsField} videoCall={s.videoCall}
      picker={creating ? { calendars: s.calendars, value: s.calendar?.id ?? '', onChange: s.chooseCalendar } : s.canMove && !s.moveReceipt ? { calendars: s.moveTargets, value: s.calendar?.id ?? s.eventCalendar?.id ?? '', onChange: s.chooseCalendar } : undefined} calendarLabel={s.moveReceipt ? s.moveTargets.find((c) => c.id === s.moveReceipt?.draft.subscriptionId) : s.eventCalendar}
      calendarNote={s.moveNote ?? (!creating && !s.canMove && !s.moveReceipt ? t('Calendar switching is available for single events without guests or video calls, after plain descriptions and supported reminders load.') : undefined)}
      onSubmit={() => void s.save()}>
      {footer}
    </EventForm>
  ) : <Skeleton shape="line" count={4} label={t('Loading the event')} />
  const inner = (
    <div ref={root} data-event-editor-frame={frame} onKeyDown={(e) => { if ((e.metaKey || e.ctrlKey) && e.key === 'Enter') { e.preventDefault(); void s.save() } }}>
      {s.ev && <EventWriteState id={s.ev.id} />}
      {s.moveError && <Notice tone="danger">{s.moveError}</Notice>}
      {s.moveReceipt && <Notice>{s.moveReceipt.created
        ? <div className="space-y-2"><p>{t('A copy exists in the new calendar. Finish the move to remove the original, or keep both events.')}</p><Button size="sm" disabled={s.moving} onClick={() => void s.keepBoth()}>{t('Keep both events')}</Button></div>
        : t('A calendar move is unfinished. Finish the move to continue safely.')}</Notice>}
      {body}
      {asking && <ConfirmDialog title={t('Discard changes?')} consequence={t('What you changed in this event is not saved.')} confirmLabel={t('Discard')} cancelLabel={t('Keep editing')} onConfirm={() => { setLeaveGuard(null); if (leaving) leaving(); else onClose() }} onCancel={() => { setLeaving(null); setAsking(false) }} />}
      {converting && s.ev && s.form && (
        <ConvertToTask ev={s.ev} calendar={s.eventCalendar?.name} getFields={() => s.form!} onClose={() => setConverting(false)} />
      )}
    </div>
  )
  if (frame === 'route') return <div className={`${PAGE_FRAME} px-4 md:px-8`} data-event-page-route>
    <PageHeader title={desktop ? <span className="flex min-w-0 items-center gap-2"><button type="button" className="text-muted hover:text-default" onClick={attemptClose}>{t('Calendar')}</button><Icon name="chevron-right" size="xs" /><span className="truncate">{s.form?.title || title}</span></span> : title} help={desktop ? 'calendar' : undefined} back={desktop ? undefined : { label: t('Calendar'), onClick: attemptClose }} barActions actions={desktop ? <IconButton icon="x" label={t('Close')} size="sm" onClick={attemptClose} /> : <>{eventMenu}{saveAction}</>} />
    {inner}
  </div>
  return frame === 'panel'
    ? <PanelFrame title={title} testId="event-editor" onClose={attemptClose}>{inner}</PanelFrame>
    : <Dialog title={<span className="flex items-center gap-2"><Icon name="calendar-range" size="sm" /><span>{t('Calendar')}</span><Icon name="chevron-right" size="xs" /><span className="truncate">{title}</span></span>} label={frame === 'page' ? title : undefined} onClose={attemptClose} size={frame === 'page' ? 'page' : 'md'} placement={frame === 'page' ? 'center' : 'sheet'} className={frame === 'page' ? 'md:px-8 md:py-6' : ''} testId="event-editor-dialog">{inner}</Dialog>
}

