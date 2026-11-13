import { describe, expect, it } from 'vitest'
import { notifyPrompt } from './eventNotify'
import type { CalendarEvent } from './types'

const ev = (over: Partial<CalendarEvent> = {}): CalendarEvent => ({
  id: 'e', subscriptionId: 's', uid: 'u', summary: 'Sync', location: '', start: { date: '2026-11-13' }, end: { date: '2026-11-13' }, allDay: false, free: false,
  access: { update: true, delete: true, notifies: true }, guests: { count: 3, organizer: true, canRespond: false }, ...over,
})

describe('notifyPrompt', () => {
  it('has no prompt when nobody is told', () => {
    expect(notifyPrompt(ev({ access: { update: true, delete: true } }), 'change')).toBeNull()
    expect(notifyPrompt(ev({ access: { update: true, delete: true, notifies: false } }), 'delete')).toBeNull()
    expect(notifyPrompt(ev({ access: undefined, guests: undefined }), 'change')).toBeNull()
    expect(notifyPrompt(null, 'invite', 0)).toBeNull()
  })
  it('says the words of a change', () => {
    expect(notifyPrompt(ev(), 'change')).toEqual({
      title: 'Tell 3 guests?', body: 'Outlook sends them this update. You can\'t turn that off.', confirm: 'Save and tell guests',
    })
  })
  it('says the words of a delete', () => {
    expect(notifyPrompt(ev({ guests: { count: 1, organizer: true, canRespond: false } }), 'delete')).toEqual({
      title: 'Tell 1 guest?', body: 'Outlook tells them this event is cancelled. You can\'t turn that off.', confirm: 'Delete and tell guests',
    })
  })
  it('says the words of a create with guests, from the number invited', () => {
    expect(notifyPrompt(null, 'invite', 2)).toEqual({
      title: 'Invite 2 guests?', body: 'Outlook sends them an invitation. You can\'t turn that off.', confirm: 'Invite and save',
    })
  })
})

describe('notifyPrompt with guests added in the same change', () => {
  it('counts the people who are told twice over: those already invited and the new ones', () => {
    expect(notifyPrompt(ev(), 'change', 2)?.title).toBe('Tell 5 guests?')
  })
  it('asks for an invitation when the first guests are added to an event that has none', () => {
    const none = ev({ access: { update: true, delete: true, notifies: false }, guests: undefined })
    expect(notifyPrompt(none, 'change', 1)).toEqual({ title: 'Invite 1 guest?', body: 'Outlook sends them an invitation. You can\'t turn that off.', confirm: 'Invite and save' })
  })
})
