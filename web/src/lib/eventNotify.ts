// The one question before a change tells people (OpenSpec change calendar-event-guests D8): "Tell 3 guests?".
// Nothing that tells a person is sent without this confirmation, and it offers only what the provider honours:
// today the calendar sends the message itself and it cannot be turned off, so there is one button and Cancel.
// A provider that could skip the message would add its second option here and nowhere else.
import { t } from './i18n'
import type { CalendarEvent } from './types'

export type NotifyKind = 'change' | 'delete' | 'invite'

export interface NotifyPrompt {
  title: string
  body: string
  confirm: string
}

/**
 * The dialog's words, or null when the change tells nobody (the event does not notify, or has no guests).
 * `invited` is the number of guests a create invites, or a change adds: a new event has no cached row. People already
 * invited are told too when the event notifies; the first guests added to an event with none are invited, not updated.
 */
export function notifyPrompt(ev: Pick<CalendarEvent, 'access' | 'guests'> | null, kind: NotifyKind, invited = 0): NotifyPrompt | null {
  const told = ev?.access?.notifies ? ev.guests?.count ?? 0 : 0
  const n = kind === 'invite' ? invited : kind === 'change' ? told + invited : told
  if (n <= 0) return null
  const invite = kind === 'invite' || (kind === 'change' && told === 0)
  const title = invite ? t('{n, plural, one {Invite # guest?} other {Invite # guests?}}', { n }) : t('{n, plural, one {Tell # guest?} other {Tell # guests?}}', { n })
  if (kind === 'delete') return { title, body: t("Outlook tells them this event is cancelled. You can't turn that off."), confirm: t('Delete and tell guests') }
  if (invite) return { title, body: t("Outlook sends them an invitation. You can't turn that off."), confirm: t('Invite and save') }
  return { title, body: t("Outlook sends them this update. You can't turn that off."), confirm: t('Save and tell guests') }
}
