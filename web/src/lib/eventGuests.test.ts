import { afterEach, describe, expect, it, vi } from 'vitest'
import type { CalendarEvent, CalendarSubscription, EventGuestList } from './types'

const calendarEventGuests = vi.fn()
vi.mock('./api', async (importOriginal) => ({ ...(await importOriginal<typeof import('./api')>()), api: { calendarEventGuests: (...a: unknown[]) => calendarEventGuests(...a) } }))

import { dropGuestList, guestRowWords, guestSummaryWords, loadGuestList, peekGuestList, providerOf, replyScopes, responseWords, validEmail } from './eventGuests'

const list = (over: Partial<EventGuestList> = {}): EventGuestList => ({
  organizer: { name: 'Ana Silva', email: 'ana@example.com', you: false },
  guests: [
    { name: 'Ben Costa', email: 'ben@example.com', role: 'required', response: 'accepted', you: false },
    { name: 'Cy Doe', email: 'cy@example.com', role: 'optional', response: 'accepted', you: false },
    { name: '', email: 'room-4@example.com', role: 'room', response: 'accepted', you: false },
  ],
  count: 4, truncated: false, ...over,
})
const sub = (over: Partial<CalendarSubscription> = {}): CalendarSubscription => ({ id: 's', name: 'Work', color: '', enabled: true, lastSuccessAt: 1, managed: true, ...over })
const ev = (over: Partial<CalendarEvent> = {}): CalendarEvent => ({ id: 'e1', subscriptionId: 's', uid: 'u', summary: 'x', location: '', start: { date: '2026-11-13' }, end: { date: '2026-11-13' }, allDay: false, free: false, ...over })

afterEach(() => { calendarEventGuests.mockReset() })

describe('words', () => {
  it('counts guests and, with the list, those who accepted', () => {
    expect(guestSummaryWords({ count: 1 })).toBe('1 guest')
    expect(guestSummaryWords({ count: 3 })).toBe('3 guests')
    expect(guestSummaryWords({ count: 3 }, list())).toBe('3 guests, 3 accepted')
    expect(guestSummaryWords({ count: 3 }, list({ guests: [] }))).toBe('3 guests')
  })
  it('says every answer in words', () => {
    expect((['accepted', 'tentative', 'declined', 'needs-action'] as const).map((r) => responseWords(r))).toEqual(['Accepted', 'Maybe', 'Declined', 'No answer'])
  })
  it('reads a row as name, role, answer; a room says room', () => {
    const l = list()
    expect(guestRowWords(l.guests[0])).toBe('Ben Costa, accepted')
    expect(guestRowWords(l.guests[1])).toBe('Cy Doe, optional, accepted')
    expect(guestRowWords(l.guests[2])).toBe('room-4@example.com, room, accepted')
  })
})

describe('validEmail (a hint, the server decides)', () => {
  it.each(['ben@example.com', ' ben@example.com ', 'a.b+c@sub.example.org'])('accepts %s', (s) => expect(validEmail(s)).toBe(true))
  it.each(['', 'ben', 'ben@', '@example.com', 'ben@example', 'a b@example.com', 'Ben <ben@example.com>', 'a@b.co,c@d.co', 'a@b.co\nc@d.co', `${'a'.repeat(250)}@example.com`])('refuses %j', (s) => expect(validEmail(s)).toBe(false))
})

describe('replyScopes', () => {
  it('has none for a single event', () => expect(replyScopes(ev(), sub({ maxReminders: 1 }))).toEqual([]))
  it('offers this and all for an occurrence', () => expect(replyScopes(ev({ series: { id: 's1' } }), sub({ maxReminders: 1 }))).toEqual(['this', 'all']))
  it('hides this on Google', () => {
    const google = sub({ calendarUrl: 'https://apidata.googleusercontent.com/caldav/v2/me@gmail.com/events/' })
    expect(replyScopes(ev({ series: { id: 's1' } }), google)).toEqual(['all'])
  })
})

describe('providerOf', () => {
  it('knows Microsoft by its reminder limit and Google by its address', () => {
    expect(providerOf(sub({ maxReminders: 1 }))).toBe('microsoft')
    expect(providerOf(sub({ calendarUrl: 'https://apidata.googleusercontent.com/caldav/v2/x/events/' }))).toBe('google')
    expect(providerOf(sub({ calendarUrl: 'https://www.google.com/calendar/dav/x/events/' }))).toBe('google')
    expect(providerOf(sub({ calendarUrl: 'https://dav.example.com/google.com/' }))).toBeUndefined()
    expect(providerOf(undefined)).toBeUndefined()
  })
})

describe('the guest list in memory', () => {
  it('is requested once, kept in memory and dropped on request', async () => {
    calendarEventGuests.mockResolvedValue(list())
    expect(peekGuestList('e1')).toBeUndefined()
    expect(await loadGuestList('e1')).toEqual(list())
    expect(await loadGuestList('e1')).toEqual(list())
    expect(calendarEventGuests).toHaveBeenCalledTimes(1)
    expect(peekGuestList('e1')).toEqual(list())
    dropGuestList('e1')
    expect(peekGuestList('e1')).toBeUndefined()
    await loadGuestList('e1')
    expect(calendarEventGuests).toHaveBeenCalledTimes(2)
  })
  it('shares one request between two callers and keeps nothing after a failure', async () => {
    let fail = true
    calendarEventGuests.mockImplementation(async () => { if (fail) throw new Error('offline'); return list() })
    await expect(Promise.all([loadGuestList('e2'), loadGuestList('e2')])).rejects.toThrow('offline')
    expect(calendarEventGuests).toHaveBeenCalledTimes(1)
    fail = false
    await expect(loadGuestList('e2')).resolves.toEqual(list())
  })
})
