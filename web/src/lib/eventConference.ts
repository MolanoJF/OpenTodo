// The join link of an event (OpenSpec change calendar-event-guests D10). The server checks the address; the
// device checks it again before it draws an anchor, so a stale or hostile cached row can never make a clickable
// `javascript:` link. Pure and small: the grid, the popover and Today's agenda draw it.
import { t } from './i18n'
import type { CalendarEvent } from './types'

/** What the Join control shows and opens. */
export interface Join {
  url: string
  /** "Join Google Meet": the service, never just "Join". */
  label: string
  /** The address's host, so a person sees where it goes. */
  host: string
}

const MAX_URL = 2048

function labelOf(provider: NonNullable<CalendarEvent['conference']>['provider']): string {
  switch (provider) {
    case 'meet': return t('Join Google Meet')
    case 'teams': return t('Join Teams')
    case 'zoom': return t('Join Zoom')
    case 'webex': return t('Join Webex')
    default: return t('Join call')
  }
}

/** The link of an event's call, or null unless its address is a plain `https` one (no userinfo, a host, a sane length). */
export function joinOf(ev: Pick<CalendarEvent, 'conference'>): Join | null {
  const c = ev.conference
  if (!c || typeof c.url !== 'string' || c.url.length > MAX_URL || !/^https:\/\//i.test(c.url)) return null
  let u: URL
  try { u = new URL(c.url) } catch { return null }
  if (u.protocol !== 'https:' || u.username || u.password || !u.hostname) return null
  return { url: u.href, label: labelOf(c.provider), host: u.host }
}
