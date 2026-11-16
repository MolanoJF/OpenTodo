// The guests of an event in its popover (OpenSpec change calendar-event-guests D13, D17): one line from the summary
// ("Ana Silva · 3 guests, 2 accepted") that expands into the list read from the server's cache when it is opened.
// Answers are words with an icon, never colour alone; a room is read-only. The list is held in memory while the
// popover is open (useGuestList) and never stored. A lazy chunk of the popover (EventPopoverActions).
import { useId, useState } from 'react'
import { guestRowWords, guestSummaryWords, yourAnswerWords } from '../../lib/eventGuests'
import { t } from '../../lib/i18n'
import type { CalendarEvent, EventGuest } from '../../lib/types'
import { Icon, Skeleton } from '../../ui'
import { RESPONSE_ICON } from './guestIcons'
import { useGuestList } from './useGuestList'

function GuestRow({ g }: { g: EventGuest }) {
  return (
    <li className="flex items-start gap-1.5 text-meta text-default" data-guest={g.email}>
      <Icon name={RESPONSE_ICON[g.response]} size="xs" className="mt-0.5 text-muted" />
      <span className="min-w-0">
        <span className="block truncate">{guestRowWords(g)}{g.you ? `, ${t('you')}` : ''}</span>
        {g.name && <span className="block truncate text-hint text-faint">{g.email}</span>}
      </span>
    </li>
  )
}

export function EventGuests({ ev }: { ev: CalendarEvent }) {
  const g = ev.guests
  const [open, setOpen] = useState(false)
  const state = useGuestList(ev.id, open)
  const listId = useId()
  if (!g) return null
  const list = state.status === 'ready' ? state.list : undefined
  const organizer = list?.organizer?.name || list?.organizer?.email
  const summary = [organizer, guestSummaryWords(g, list)].filter(Boolean).join(' · ')
  return (
    <div className="space-y-1" data-event-guests>
      <button type="button" aria-expanded={open} aria-controls={listId} data-event-guests-toggle onClick={() => setOpen((o) => !o)}
        className="flex w-full items-center gap-1.5 rounded-control text-left text-meta text-muted focus-visible:outline-2 focus-visible:outline-focus">
        <Icon name="users" size="xs" />
        <span className="min-w-0 flex-1 truncate">{summary}</span>
        <Icon name={open ? 'chevron-down' : 'chevron-right'} size="xs" />
      </button>
      {g.you && !g.canRespond && <p className="pl-[18px] text-meta text-muted" data-event-guests-you>{t('Your answer: {answer}', { answer: yourAnswerWords(g.you) })}</p>}
      <div id={listId} hidden={!open} className="pl-[18px]">
        {open && state.status === 'loading' && <Skeleton shape="line" count={2} label={t('Loading the guests')} />}
        {open && state.status === 'unavailable' && <p className="text-meta text-muted" data-event-guests-note>{state.reason === 'offline' ? t('The guest list needs a connection.') : t('The guest list could not be loaded.')}</p>}
        {open && list && (
          <>
            {list.organizer && <p className="pb-1 text-meta text-default" data-event-organizer>{t('Organizer: {name}', { name: organizer ?? '' })}{list.organizer.you ? `, ${t('you')}` : ''}</p>}
            <ul className="space-y-1" aria-label={t('Guests')}>{list.guests.map((x) => <GuestRow key={x.email} g={x} />)}</ul>
            {list.truncated && <p className="pt-1 text-meta text-faint" data-event-guests-cut>{t('Showing {shown} of {count} guests.', { shown: list.guests.length, count: list.count })}</p>}
          </>
        )}
      </div>
    </div>
  )
}
