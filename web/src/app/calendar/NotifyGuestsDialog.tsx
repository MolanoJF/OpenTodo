// "Tell 3 guests?" (OpenSpec change calendar-event-guests D8, D13, D17): one sentence and two buttons, in the overlay
// module's Dialog. Focus starts on Cancel, Escape and a tap outside cancel, and the question is announced in a live
// region. There is never a menu of options the server would refuse: the calendar sends the message itself.
// Loaded when a request is open (the host in ScopeMenuHost).
import { t } from '../../lib/i18n'
import { Button, Dialog } from '../../ui'
import { answerNotify, type NotifyRequest } from './notifyStore'

export default function NotifyGuestsDialog({ req }: { req: NotifyRequest }) {
  const { title, body, confirm } = req.prompt
  return (
    <Dialog title={title} onClose={() => answerNotify(false)} size="sm" testId="notify-guests">
      <div className="space-y-4" data-notify-guests>
        <p role="status" className="sr-only">{`${title} ${body}`}</p>
        <p className="text-body text-default">{body}</p>
        <div className="flex flex-wrap justify-end gap-2">
          <Button variant="secondary" data-autofocus data-notify-cancel onClick={() => answerNotify(false)}>{t('Cancel')}</Button>
          <Button variant="primary" data-notify-confirm onClick={() => answerNotify(true)}>{confirm}</Button>
        </div>
      </div>
    </Dialog>
  )
}
