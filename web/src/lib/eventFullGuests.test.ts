// The editor's guests and video call as form fields (OpenSpec change calendar-event-guests, task 4.4, design D3, D11):
// only what changed is sent, a draft carries the guests it invites, and Duplicate copies none.
import { describe, expect, it } from 'vitest'
import { draftFromEvent, draftFromFull } from './eventCreate'
import { eventFullChange, eventFullOf } from './eventFull'
import type { CalendarEvent } from './types'

const BERLIN = 'Europe/Berlin'
const ev = (over: Partial<CalendarEvent> = {}): CalendarEvent => ({
  id: 'e1', subscriptionId: 's1', uid: 'u1', summary: 'Sync', location: '', allDay: false, free: false, rev: 'r1',
  start: { date: '2026-11-12', time: '07:00', tz: BERLIN }, end: { date: '2026-11-12', time: '08:30', tz: BERLIN },
  guests: { count: 3, organizer: true, canRespond: false }, conference: { url: 'https://teams.microsoft.com/l/x', provider: 'teams' }, ...over,
})
const full = (e = ev()) => eventFullOf(e, BERLIN, '')
const change = (e: CalendarEvent, form: ReturnType<typeof full>) => eventFullChange(e, form, BERLIN, '')
const cal = { id: 's1', connectionId: 'c1', calendarUrl: 'https://dav/x/' }

describe('guest fields', () => {
  it('start empty: the existing guests are read from the list, not the form', () => {
    expect(full()).toMatchObject({ guestAdds: [], guestRemoves: [], teams: false })
  })

  it('sends nothing when nothing changed', () => {
    expect(change(ev(), full())).toBeNull()
  })

  it('sends the guests added with their roles and the addresses removed, and nothing else', () => {
    const form = { ...full(), guestAdds: [{ email: 'ben@example.com', role: 'optional' as const }, { email: 'cy@example.com' }], guestRemoves: ['old@example.com'] }
    expect(change(ev(), form)).toEqual({ guests: { add: [{ email: 'ben@example.com', role: 'optional' }, { email: 'cy@example.com' }], remove: ['old@example.com'] } })
  })

  it('leaves out an empty add or remove', () => {
    expect(change(ev(), { ...full(), guestAdds: [{ email: 'ben@example.com' }] })).toEqual({ guests: { add: [{ email: 'ben@example.com' }] } })
    expect(change(ev(), { ...full(), guestRemoves: ['old@example.com'] })).toEqual({ guests: { remove: ['old@example.com'] } })
  })

  it('asks for a Teams meeting only when the switch is on, next to the other changes', () => {
    expect(change(ev(), { ...full(), teams: true, title: 'Renamed' })).toEqual({ summary: 'Renamed', onlineMeeting: true })
  })
})

describe('drafts', () => {
  it('a draft from the form carries the guests it invites and the Teams switch', () => {
    const form = { ...full(), guestAdds: [{ email: 'ben@example.com', role: 'optional' as const }], teams: true }
    expect(draftFromFull(form, cal)).toMatchObject({ guests: [{ email: 'ben@example.com', role: 'optional' }], onlineMeeting: true })
  })

  it('a draft without guests has neither member', () => {
    const d = draftFromFull(full(), cal)!
    expect('guests' in d).toBe(false)
    expect('onlineMeeting' in d).toBe(false)
  })

  it('Duplicate copies no guests and no call', () => {
    const d = draftFromEvent(ev(), '', cal, BERLIN)!
    expect('guests' in d).toBe(false)
    expect('onlineMeeting' in d).toBe(false)
  })
})
