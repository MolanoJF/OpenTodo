// @vitest-environment happy-dom
// The Join control (OpenSpec change calendar-event-guests, task 5.5, design D10, D17): an anchor named by the service, opened in a
// new tab with no opener and no referrer, with the host as its title and the event's title as its description; no link
// at all for an address that is not `https` (a stale or hostile cached row).
import { describe, expect, it } from 'vitest'
import { render, spokenText } from '../../ui/test-utils'
import { JoinButton } from './JoinButton'

const meet = { url: 'https://meet.google.com/abc-defg-hij', provider: 'meet' as const }

describe('JoinButton', () => {
  it('is a link to the call that opens in a new tab with no opener or referrer', async () => {
    const r = await render(<JoinButton ev={{ conference: meet, summary: 'Design review' }} />)
    const a = r.container.querySelector<HTMLAnchorElement>('a[data-event-join]')!
    expect(a.getAttribute('href')).toBe('https://meet.google.com/abc-defg-hij')
    expect(a.getAttribute('target')).toBe('_blank')
    expect(a.getAttribute('rel')).toBe('noopener noreferrer')
    expect(a.hasAttribute('title')).toBe(false) // the host is in the Tooltip, not a native title
    expect(a.textContent).toContain('Join Google Meet')
    await r.unmount()
  })

  it('says it opens in a new tab as hidden text, and is described by the event title', async () => {
    const r = await render(<JoinButton ev={{ conference: meet, summary: 'Design review' }} />)
    const a = r.container.querySelector<HTMLAnchorElement>('a[data-event-join]')!
    const hidden = a.querySelector('.sr-only')!
    expect(hidden.textContent).toBe('opens in a new tab')
    expect(spokenText(a)).toContain('Join Google Meet')
    expect(r.container.querySelector(`#${CSS.escape(a.getAttribute('aria-describedby')!)}`)!.textContent).toBe('Design review')
    await r.unmount()
  })

  it.each(['javascript:alert(1)', 'http://meet.google.com/abc', 'https://meet.google.com@evil.example/x', 'data:text/html,x'])('draws no link for %s', async (url) => {
    const r = await render(<JoinButton ev={{ conference: { url, provider: 'meet' }, summary: 'Review' }} />)
    expect(r.container.querySelector('a')).toBeNull()
    expect(r.container.textContent).toBe('')
    await r.unmount()
  })

  it('draws nothing for an event without a call', async () => {
    const r = await render(<JoinButton ev={{ summary: 'Review' }} />)
    expect(r.container.innerHTML).toBe('')
    await r.unmount()
  })
})
