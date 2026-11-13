import { describe, expect, it } from 'vitest'
import { joinOf } from './eventConference'
import type { CalendarEvent } from './types'

const ev = (conference: CalendarEvent['conference'] | undefined): CalendarEvent => ({
  id: 'e', subscriptionId: 's', uid: 'u', summary: 'Sync', location: '', start: { date: '2026-11-13' }, end: { date: '2026-11-13' }, allDay: false, free: false, ...(conference ? { conference } : {}),
})

describe('joinOf', () => {
  it('names the service and shows the host', () => {
    expect(joinOf(ev({ url: 'https://meet.google.com/abc-defg-hij', provider: 'meet' }))).toEqual({ url: 'https://meet.google.com/abc-defg-hij', label: 'Join Google Meet', host: 'meet.google.com' })
    expect(joinOf(ev({ url: 'https://teams.microsoft.com/l/meetup-join/x', provider: 'teams' }))?.label).toBe('Join Teams')
    expect(joinOf(ev({ url: 'https://acme.zoom.us/j/123?pwd=a', provider: 'zoom' }))).toMatchObject({ label: 'Join Zoom', host: 'acme.zoom.us' })
    expect(joinOf(ev({ url: 'https://acme.webex.com/m/1', provider: 'webex' }))?.label).toBe('Join Webex')
    expect(joinOf(ev({ url: 'https://calls.example.org/r/1', provider: 'other' }))).toMatchObject({ label: 'Join call', host: 'calls.example.org' })
  })
  it('has no link without a conference', () => expect(joinOf(ev(undefined))).toBeNull())
  it.each([
    'javascript:alert(1)', 'http://meet.google.com/abc', 'data:text/html,x', 'intent://x#Intent;end', 'zoommtg://zoom.us/join?confno=1', 'msteams://x', 'tel:+1555', 'HTTP://meet.google.com/a',
    'https://zoom.us@evil.example/j/1', 'https://user:pw@zoom.us/j/1', 'not a url', '', 'https://', ' javascript:alert(1)', '//meet.google.com/a',
  ])('gives no link for %j (a stale or hostile cached row)', (url) => {
    expect(joinOf(ev({ url, provider: 'meet' }))).toBeNull()
  })
})
