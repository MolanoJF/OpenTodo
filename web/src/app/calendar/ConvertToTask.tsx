// The one question of Convert to task (OpenSpec change calendar-event-editor D11): create the task, and
// delete the event from its calendar too. The switch is on by default when the event may be deleted,
// off and disabled otherwise, and remembered on this device.
import { useState } from 'react'
import type { EventFull } from '../../lib/eventFull'
import { t } from '../../lib/i18n'
import type { CalendarEvent } from '../../lib/types'
import { Button, Dialog, Switch } from '../../ui'
import { convertEvent, rememberConvertDelete, rememberedConvertDelete } from './eventConvert'

export function ConvertToTask({ ev, calendar, getFields, onClose }: {
  ev: CalendarEvent
  /** The calendar's name, for the switch. */
  calendar?: string
  /** The event's values as the editor shows them; the popover reads the description from the calendar here. */
  getFields: () => Promise<EventFull> | EventFull
  onClose: () => void
}) {
  const canDelete = !!ev.access?.delete
  const [del, setDel] = useState(() => canDelete && rememberedConvertDelete())
  const [busy, setBusy] = useState(false)
  const create = async () => {
    setBusy(true)
    const ok = await convertEvent(ev, await getFields(), canDelete && del)
    setBusy(false)
    if (ok) onClose()
  }
  return (
    <Dialog title={t('Convert to task')} onClose={onClose} size="sm" testId="convert-to-task">
      <div className="space-y-4">
        <p className="text-body text-default">{t('A task is made from the title, time and details of this event.')}</p>
        <Switch checked={del} disabled={!canDelete} label={calendar ? t('Delete the event from {calendar}', { calendar }) : t('Delete the event from its calendar')}
          description={canDelete ? undefined : t('This event cannot be deleted from here.')} onChange={(on) => { setDel(on); rememberConvertDelete(on) }} />
        <div className="flex flex-wrap justify-end gap-2">
          <Button variant="secondary" onClick={onClose}>{t('Cancel')}</Button>
          <Button variant="primary" busy={busy} data-autofocus data-convert-create onClick={() => void create()}>{t('Create task')}</Button>
        </div>
      </div>
    </Dialog>
  )
}
