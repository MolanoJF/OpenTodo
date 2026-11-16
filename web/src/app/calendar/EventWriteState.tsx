// What a queued write of a calendar event is doing, when it is not just on its way (OpenSpec change
// calendar-event-edits D6): waiting for the device to be online, or failed. A failed write is never sent
// again by itself; the person retries it (the same command, the same version) or discards it.
import { Button } from '../../ui'
import { discardEventWrite, retryEventWrite, useEventCommands, writeFailureText } from '../../lib/calendarEventEdits'
import { t } from '../../lib/i18n'

export function EventWriteState({ id }: { id: string }) {
  const cmd = useEventCommands().get(id)
  if (!cmd || (!cmd.failed && !cmd.offline)) return null
  const failed = !!cmd.failed
  return (
    <div role="status" data-event-write={failed ? 'failed' : 'waiting'} className={`space-y-1 rounded-control border p-2 text-meta ${failed ? 'border-danger bg-danger-soft text-on-danger-soft' : 'border-default text-muted'}`}>
      <p className="font-medium">{failed ? t('Not saved') : t('Waiting to send')}</p>
      {cmd.failed && <p>{writeFailureText(cmd.failed)}</p>}
      <div className="flex gap-1.5">
        <Button size="sm" variant="ghost" data-event-write-retry onClick={() => void retryEventWrite(cmd.key)}>{t('Retry')}</Button>
        <Button size="sm" variant="ghost" data-event-write-discard onClick={() => void discardEventWrite(cmd.key)}>{t('Discard')}</Button>
      </div>
    </div>
  )
}
