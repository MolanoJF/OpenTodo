// The editor's Guests field (OpenSpec change calendar-event-guests D13, D17): the organiser line, the guest list with a
// labelled Remove per guest (a room is read-only), the guests added in this editor with a role switch, and an Add guest
// Combobox that takes a typed email address. Read-only text unless the calendar lets the person change guests. Saving a
// change that tells guests asks "Tell N guests?" first (eventActions); nothing here sends anything.
import { useState } from 'react'
import { guestRowWords, validEmail } from '../../lib/eventGuests'
import { t } from '../../lib/i18n'
import type { EventGuestList, GuestInput } from '../../lib/types'
import { Combobox, Icon, IconButton, Skeleton, Switch } from '../../ui'
import { RESPONSE_ICON } from './guestIcons'

export interface GuestsFieldProps {
  /** `loading` while the calendar is read for an existing event; `unavailable` when it could not be. */
  state: 'loading' | 'ready' | 'unavailable'
  reason?: 'offline' | 'failed'
  /** The calendar's own guests (an existing event); null for a new one. */
  list: EventGuestList | null
  /** The calendar lets the person add and remove guests. */
  editable: boolean
  /** The device is online: guests are told when it is, so editing them waits for a connection. */
  online: boolean
  adds: GuestInput[]
  removes: string[]
  onChange: (next: { adds: GuestInput[]; removes: string[] }) => void
}

const lower = (s: string) => s.trim().toLowerCase()

export function GuestsField({ state, reason, list, editable, online, adds, removes, onChange }: GuestsFieldProps) {
  const [error, setError] = useState('')
  const kept = list?.guests.filter((g) => !removes.includes(g.email.toLowerCase())) ?? []
  const known = new Set([...kept.map((g) => lower(g.email)), ...adds.map((a) => lower(a.email)), lower(list?.organizer?.email ?? '')])
  const canEdit = editable && online
  const add = (raw: string) => {
    if (!validEmail(raw)) return setError(t('Enter an email address'))
    setError('')
    const email = raw.trim()
    if (known.has(lower(email))) return
    onChange({ adds: [...adds, { email }], removes })
  }
  const setRole = (email: string, optional: boolean) => onChange({ adds: adds.map((a) => (a.email === email ? { ...a, role: optional ? 'optional' : 'required' } : a)), removes })
  return (
    <div className="space-y-1.5" data-event-form-guests>
      <p className="text-body font-medium text-default">{t('Guests')}</p>
      {state === 'loading' && <Skeleton shape="line" count={2} label={t('Loading the guests')} />}
      {state === 'unavailable' && <p className="text-meta text-muted">{reason === 'offline' ? t('You are offline, so the guests are left as they are.') : t('The guests could not be loaded, so they are left as they are.')}</p>}
      {state === 'ready' && (
        <>
          {list?.organizer && <p className="text-meta text-muted" data-event-organizer>{t('Organizer: {name}', { name: list.organizer.name || list.organizer.email })}</p>}
          {(kept.length > 0 || adds.length > 0) && (
            <ul className="space-y-1" aria-label={t('Guests')}>
              {kept.map((g) => (
                <li key={g.email} className="flex items-center justify-between gap-2 text-body text-default" data-guest={g.email}>
                  <span className="flex min-w-0 items-center gap-1.5"><Icon name={RESPONSE_ICON[g.response]} size="xs" className="text-muted" /><span className="truncate">{guestRowWords(g)}</span></span>
                  {canEdit && g.role !== 'room' && !g.you && <IconButton icon="x" size="sm" label={t('Remove {name}', { name: g.name || g.email })} data-guest-remove={g.email} onClick={() => onChange({ adds, removes: [...removes, g.email.toLowerCase()] })} />}
                </li>
              ))}
              {adds.map((a) => (
                <li key={a.email} className="flex items-center justify-between gap-2 text-body text-default" data-guest-added={a.email}>
                  <span className="flex min-w-0 items-center gap-1.5"><Icon name="user-check" size="xs" className="text-muted" /><span className="truncate">{a.email}</span></span>
                  <span className="flex shrink-0 items-center gap-1">
                    <Switch checked={a.role === 'optional'} label={t('Optional')} onChange={(on) => setRole(a.email, on)} />
                    <IconButton icon="x" size="sm" label={t('Remove {name}', { name: a.email })} data-guest-remove={a.email} onClick={() => onChange({ adds: adds.filter((x) => x.email !== a.email), removes })} />
                  </span>
                </li>
              ))}
            </ul>
          )}
          {list?.truncated && <p className="text-meta text-faint">{t('Showing {shown} of {count} guests.', { shown: list.guests.length, count: list.count })}</p>}
          {editable && online && (
            <div className="space-y-1" data-event-form-add-guest>
              <Combobox<GuestInput> label={t('Add guest')} placeholder={t('Add guest')} source={[]} getKey={(g) => g.email} getLabel={(g) => g.email} emptyText={t('Type an email address')}
                onPick={() => undefined} onCreate={add} />
              {error && <p role="alert" className="text-meta text-danger" data-event-form-guest-error>{error}</p>}
            </div>
          )}
          {editable && !online && <p className="text-meta text-faint">{t("Guests are told when you're online")}</p>}
        </>
      )}
    </div>
  )
}
