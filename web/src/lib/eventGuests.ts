// Guests of a connected calendar's events (OpenSpec change calendar-event-guests D11, D12). Words, the reply's
// scopes and the guest list. The list is personal data of people who are not users of this server: it is requested
// when a popover or the editor opens, held in this module's memory and never written to Dexie, localStorage or the
// service worker's cache; the popover drops it when it closes.
import { api } from './api'
import { t } from './i18n'
import { isMicrosoftCalendar } from './eventRepeat'
import { resettable } from './moduleState'
import type { CalendarEvent, CalendarSubscription, EventGuest, EventGuestList, EventScope, GuestResponse } from './types'

/** "3 guests", and with the list at hand, "3 guests, 2 accepted". */
export function guestSummaryWords(g: { count: number }, list?: Pick<EventGuestList, 'guests'>): string {
  const base = t('{n, plural, one {# guest} other {# guests}}', { n: g.count })
  const accepted = list?.guests.filter((x) => x.response === 'accepted').length ?? 0
  return accepted > 0 ? t('{guests}, {n} accepted', { guests: base, n: accepted }) : base
}

/** An answer in words: never colour alone. */
export function responseWords(r: GuestResponse): string {
  switch (r) {
    case 'accepted': return t('Accepted')
    case 'tentative': return t('Maybe')
    case 'declined': return t('Declined')
    default: return t('No answer')
  }
}

/** The user's own answer, as the reply row names it. */
export const yourAnswerWords = (r: GuestResponse | undefined): string => (r === 'accepted' ? t('Yes') : r === 'tentative' ? t('Maybe') : r === 'declined' ? t('No') : t('No answer yet'))

/** A guest row as a screen reader says it: "Ben Costa, optional, accepted". */
export function guestRowWords(g: Pick<EventGuest, 'name' | 'email' | 'role' | 'response'>): string {
  const role = g.role === 'optional' ? t('optional') : g.role === 'room' ? t('room') : ''
  return [g.name || g.email, role, responseWords(g.response).toLowerCase()].filter(Boolean).join(', ')
}

/** A hint for the add field only: one plain `local@domain` address with a dot in the domain. The server decides. */
export function validEmail(raw: string): boolean {
  const s = raw.trim()
  return s.length <= 254 && /^[^\s@,<>;"()[\]\\]+@[^\s@,<>;"()[\]\\]+\.[^\s@,<>;"()[\]\\.]+$/.test(s) && !s.includes('..')
}

/** Which provider a managed calendar is, as far as the device can tell; undefined when it is a generic one. */
export function providerOf(sub: Pick<CalendarSubscription, 'maxReminders' | 'calendarUrl'> | undefined): 'google' | 'microsoft' | undefined {
  if (isMicrosoftCalendar(sub)) return 'microsoft'
  try {
    const host = new URL(sub?.calendarUrl ?? '').hostname
    if (/(^|\.)(google|googleusercontent|googleapis)\.com$/.test(host)) return 'google'
  } catch { /* no address */ }
  return undefined
}

/** The scopes an answer to an occurrence can be for (D11): this event or all of them; Google answers all of them only; none for a single event. */
export function replyScopes(ev: Pick<CalendarEvent, 'series'>, sub: Pick<CalendarSubscription, 'maxReminders' | 'calendarUrl'> | undefined): EventScope[] {
  if (!ev.series) return []
  return providerOf(sub) === 'google' ? ['all'] : ['this', 'all']
}

const lists = new Map<string, EventGuestList>()
const loading = new Map<string, Promise<EventGuestList>>()
resettable('eventGuests', () => { lists.clear(); loading.clear() })

/** The list held for an event, if it was loaded and not dropped. */
export const peekGuestList = (eventId: string): EventGuestList | undefined => lists.get(eventId)

/** Drops the list of an event (the popover closed). */
export function dropGuestList(eventId: string): void {
  lists.delete(eventId)
  loading.delete(eventId)
}

/** The guest list of an event from the server's cache (no request to the calendar), kept in memory only. */
export function loadGuestList(eventId: string): Promise<EventGuestList> {
  const held = lists.get(eventId)
  if (held) return Promise.resolve(held)
  let p = loading.get(eventId)
  if (!p) {
    const mine: Promise<EventGuestList> = api.calendarEventGuests(eventId).then((l) => {
      if (loading.get(eventId) === mine) lists.set(eventId, l) // dropped meanwhile: not kept
      return l
    }).finally(() => { if (loading.get(eventId) === mine) loading.delete(eventId) })
    p = mine
    loading.set(eventId, p)
  }
  return p
}
