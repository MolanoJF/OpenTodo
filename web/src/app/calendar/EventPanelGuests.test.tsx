// @vitest-environment happy-dom
// The full editor with guests (OpenSpec change calendar-event-guests, task 5.4, design D3, D8, D13): the Guests and Video
// call fields where the calendar has them, only changed fields sent, Save and Create ask "Tell N guests?" first and send
// at once with `notifyGuests: true`, Cancel keeps the editor open and sends nothing, offline Save waits with the reason.
import 'fake-indexeddb/auto'
import { act } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { db, setMeta } from '../../lib/db'
import { resetDeviceId } from '../../lib/ops'
import type { CalendarEvent, CalendarSubscription, EventGuestList } from '../../lib/types'
import { click, key, render, type, waitFor } from '../../ui/test-utils'
import { resetPanel } from '../panel'
import { closeEventEditor, type EventEditorRequest } from './eventEditorStore'
import { EventPanel } from './EventPanel'
import { ScopeMenuHost } from './ScopeMenuHost'

const DAY = '2026-11-13'
const outlook: CalendarSubscription = { id: 'work', name: 'Work', color: '#2563eb', enabled: true, lastSuccessAt: Date.now(), managed: true, connectionId: 'c-work', calendarUrl: 'https://graph/work/', editEvents: true, maxReminders: 1, caps: { create: true, update: true, delete: true, onlineMeeting: true }, guests: { respond: true, silent: true, edit: true } }
const google: CalendarSubscription = { id: 'goog', name: 'Me', color: '#16a34a', enabled: true, lastSuccessAt: Date.now(), managed: true, connectionId: 'c-goog', calendarUrl: 'https://apidata.googleusercontent.com/caldav/v2/me/events/', editEvents: true, caps: { create: true, update: true, delete: true }, guests: { respond: false, silent: false, edit: false } }
const meeting: CalendarEvent = {
  id: 'ev1', subscriptionId: 'work', uid: 'u1', summary: 'Review', location: '', allDay: false, free: false, rev: 'r1',
  start: { date: DAY, time: '10:00', tz: 'Europe/Berlin' }, end: { date: DAY, time: '11:00', tz: 'Europe/Berlin' },
  access: { update: true, delete: true, notifies: true }, guests: { count: 3, organizer: true, canRespond: false },
}
const guestList: EventGuestList = {
  organizer: { name: 'Me', email: 'me@example.com', you: true },
  guests: [
    { name: 'Ben Costa', email: 'ben@example.com', role: 'required', response: 'accepted', you: false },
    { name: '', email: 'room-4@example.com', role: 'room', response: 'accepted', you: false },
  ],
  count: 3, truncated: false,
}

let requests: { method: string; url: string; body?: Record<string, unknown> }[]
let detail: Record<string, unknown>
const json = (status: number, b: unknown) => new Response(JSON.stringify(b), { status, headers: { 'Content-Type': 'application/json' } })

beforeEach(async () => {
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(new Date(2026, 10, 12, 8, 0))
  await db.delete()
  await db.open()
  resetDeviceId()
  resetPanel()
  closeEventEditor()
  localStorage.clear()
  requests = []
  detail = { event: meeting, description: 'Agenda', descriptionEditable: true, guests: { ...guestList, you: undefined, canRespond: false, canEdit: true }, onlineMeeting: { available: true, on: false } }
  vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
    const body = init?.body ? JSON.parse(String(init.body)) as Record<string, unknown> : undefined
    requests.push({ method: init?.method ?? 'GET', url, body })
    if (init?.method === 'POST') return json(201, { event: { ...meeting, id: 'real', uid: String(body?.clientId), summary: body?.summary, rev: 'r9' }, clientId: body?.clientId })
    if (init?.method === 'PATCH') return json(200, { event: { ...meeting, ...body, id: 'ev1b', rev: 'r2' }, replaces: 'ev1' })
    if ((url as string).includes('/events/')) return json(200, detail)
    return json(404, {})
  }))
  await db.calendarSubscriptions.bulkPut([outlook, google])
  await db.calendarEvents.put(meeting)
  await setMeta('calendar.events.window', { from: '2026-09-01', to: '2026-12-30', fetchedAt: Date.now() })
})
afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
  Object.defineProperty(navigator, 'onLine', { configurable: true, value: true })
  resetPanel()
  closeEventEditor()
})

const edit: EventEditorRequest = { mode: 'edit', key: 2, eventId: 'ev1' }
const q = <T extends Element = HTMLInputElement>(sel: string) => document.querySelector<T>(sel)!
const submit = async () => { await click(q('[data-event-form-save]')) }
const patches = () => requests.filter((r) => r.method === 'PATCH')
const posts = () => requests.filter((r) => r.method === 'POST')
const dialog = () => document.querySelector('[data-notify-guests]')
const typeGuest = async (text: string) => {
  const input = q('[data-event-form-add-guest] input[role="combobox"]')
  await act(async () => { input.blur(); input.focus() })
  await type(input, text)
  await waitFor(() => document.querySelectorAll('[role="option"]').length === 1, 'the create row')
  await key(input, 'Enter')
}
const open = async (request = edit, onClose = vi.fn()) => {
  await render(<><EventPanel request={request} frame="dialog" onClose={onClose} /><ScopeMenuHost /></>)
  await waitFor(() => !!q('[data-event-form-title]') && !document.querySelector('[data-event-form-guests] [aria-busy]') && !!document.querySelector('[data-event-form-guests] [data-guest]'), 'the guests')
  return onClose
}

describe('editing a meeting the user organises', () => {
  it('lists the guests, with Remove for a person and none for a room, and the Teams switch that says it stays', async () => {
    await open()
    expect([...document.querySelectorAll('[data-guest]')].map((x) => x.textContent)).toEqual(['Ben Costa, accepted', 'room-4@example.com, room, accepted'])
    expect(document.querySelector('[aria-label="Remove Ben Costa"]')).not.toBeNull()
    expect(document.querySelectorAll('[data-guest-remove]')).toHaveLength(1)
    const teams = q('[data-event-form-video-call]')
    expect(teams.textContent).toContain('Add a Teams meeting')
    expect(teams.textContent).toContain("A Teams meeting can't be removed from here once it is added.")
  })

  it('Save after adding a guest asks "Tell 4 guests?", sends nothing before the answer, then one request with notifyGuests', async () => {
    const onClose = await open()
    await typeGuest('new.guest@example.com')
    await waitFor(() => !!q('[data-guest-added]'), 'the added guest')
    await submit()
    await waitFor(() => !!dialog(), 'the dialog')
    expect(q('[role="dialog"][aria-label], [data-notify-guests]').textContent).toContain('Outlook sends them this update')
    expect(document.body.textContent).toContain('Tell 4 guests?')
    expect(patches()).toHaveLength(0)
    expect(onClose).not.toHaveBeenCalled()
    await click(q('[data-notify-confirm]'))
    await waitFor(() => onClose.mock.calls.length > 0, 'the close')
    await waitFor(() => patches().length === 1, 'the request')
    expect(patches()[0].body).toEqual({ rev: 'r1', guests: { add: [{ email: 'new.guest@example.com' }] }, notifyGuests: true })
    expect(await db.eventOutbox.count()).toBe(0)
  })

  it('Cancel keeps the editor open with what was typed and sends nothing', async () => {
    const onClose = await open()
    await type(q('[data-event-form-location]'), 'Room 4')
    await submit()
    await waitFor(() => !!dialog())
    await click(q('[data-notify-cancel]'))
    await waitFor(() => !dialog(), 'the dialog closed')
    expect(onClose).not.toHaveBeenCalled()
    expect(q('[data-event-form-location]').value).toBe('Room 4')
    expect(patches()).toHaveLength(0)
    expect(requests.filter((r) => r.method !== 'GET')).toEqual([])
  })

  it('Remove sends the address as removed and only that', async () => {
    await open()
    await click(q('[aria-label="Remove Ben Costa"]'))
    await submit()
    await waitFor(() => !!dialog())
    await click(q('[data-notify-confirm]'))
    await waitFor(() => patches().length === 1)
    expect(patches()[0].body).toEqual({ rev: 'r1', guests: { remove: ['ben@example.com'] }, notifyGuests: true })
  })

  it('an invalid address blocks adding it and shows the field error', async () => {
    await open()
    await typeGuest('nobody')
    await waitFor(() => !!document.querySelector('[data-event-form-guest-error]'))
    expect(q('[data-event-form-guest-error]').textContent).toBe('Enter an email address')
    expect(document.querySelector('[data-guest-added]')).toBeNull()
    expect(q('[data-event-form-save]').hasAttribute('disabled')).toBe(true) // nothing changed
  })

  it('asking for a Teams meeting is sent with the confirmation, and only that field', async () => {
    await open()
    await click(q('[data-event-form-video-call] [role="switch"]'))
    await submit()
    await waitFor(() => !!dialog())
    await click(q('[data-notify-confirm]'))
    await waitFor(() => patches().length === 1)
    expect(patches()[0].body).toEqual({ rev: 'r1', onlineMeeting: true, notifyGuests: true })
  })

  it('a rename of a meeting that tells guests asks too, whatever the field', async () => {
    await open()
    await type(q('[data-event-form-title]'), 'Planning')
    await submit()
    await waitFor(() => !!dialog())
    await click(q('[data-notify-confirm]'))
    await waitFor(() => patches().length === 1)
    expect(patches()[0].body).toEqual({ rev: 'r1', summary: 'Planning', notifyGuests: true })
  })

  it('offline, Save waits with the reason and sends nothing', async () => {
    await open()
    await type(q('[data-event-form-title]'), 'Planning')
    Object.defineProperty(navigator, 'onLine', { configurable: true, value: false })
    await act(async () => { window.dispatchEvent(new Event('offline')) })
    await waitFor(() => !!document.querySelector('[data-event-form-offline]'), 'the reason')
    expect(q('[data-event-form-offline]').textContent).toBe("Guests are told when you're online")
    expect(q('[data-event-form-save]').hasAttribute('disabled')).toBe(true)
    expect(patches()).toHaveLength(0)
  })

  it('shows a call as the Join button, not a switch, and the guests as read-only text when the calendar does not let guests change', async () => {
    detail = { ...detail, guests: { ...guestList, canRespond: false, canEdit: false }, conference: { url: 'https://teams.microsoft.com/l/meetup-join/x', provider: 'teams' }, onlineMeeting: { available: true, on: true } }
    await render(<><EventPanel request={edit} frame="dialog" onClose={() => {}} /><ScopeMenuHost /></>)
    await waitFor(() => !!document.querySelector('[data-event-join]'), 'the Join button')
    expect(q('[data-event-form-video-call] [role="switch"]' as string) ?? null).toBeNull()
    expect(document.querySelector('[data-guest-remove]')).toBeNull()
    expect(document.querySelector('[data-event-form-add-guest]')).toBeNull()
  })
})

describe('an event without guests', () => {
  it('saves as before: no dialog, a queued command, no Guests field on a calendar that cannot', async () => {
    await db.calendarEvents.put({ ...meeting, subscriptionId: 'goog', access: { update: true, delete: true }, guests: undefined })
    detail = { event: { ...meeting, subscriptionId: 'goog', access: { update: true, delete: true }, guests: undefined }, description: '', descriptionEditable: true }
    await render(<><EventPanel request={edit} frame="dialog" onClose={() => {}} /><ScopeMenuHost /></>)
    await waitFor(() => !!q('[data-event-form-title]'))
    await waitFor(() => !document.querySelector('[data-event-form-description][disabled]'), 'the description')
    expect(document.querySelector('[data-event-form-guests]')).toBeNull()
    expect(document.querySelector('[data-event-form-video-call]')).toBeNull()
    await type(q('[data-event-form-location]'), 'Cafe')
    await submit()
    await waitFor(async () => (await db.eventOutbox.count()) === 1, 'the queued command')
    expect(dialog()).toBeNull()
  })
})

describe('creating an event with guests', () => {
  const create = (over: Partial<Extract<EventEditorRequest, { mode: 'create' }>> = {}): EventEditorRequest => ({ mode: 'create', key: 1, place: { date: DAY, start: 14 * 60, end: 15 * 60 }, ...over })
  beforeEach(async () => { await db.calendarSubscriptions.delete('goog') }) // the one writable calendar is Outlook

  it('offers Guests and the Teams switch on a calendar that can, asks "Invite 2 guests?" and creates at once with notifyGuests', async () => {
    const onClose = vi.fn()
    await render(<><EventPanel request={create()} frame="dialog" onClose={onClose} /><ScopeMenuHost /></>)
    await waitFor(() => !!q('[data-event-form-title]'))
    await type(q('[data-event-form-title]'), 'Kickoff')
    await waitFor(() => !!document.querySelector('[data-event-form-guests]'), 'the Guests field')
    await typeGuest('ben@example.com')
    await waitFor(() => !!q('[data-guest-added]'))
    await typeGuest('cy@example.com')
    await waitFor(() => document.querySelectorAll('[data-guest-added]').length === 2)
    await click(q('[data-event-form-video-call] [role="switch"]'))
    await submit()
    await waitFor(() => !!dialog(), 'the dialog')
    expect(document.body.textContent).toContain('Invite 2 guests?')
    expect(document.body.textContent).toContain('Outlook sends them an invitation')
    expect(q('[data-notify-confirm]').textContent).toBe('Invite and save')
    expect(posts()).toHaveLength(0)
    expect(onClose).not.toHaveBeenCalled()
    await click(q('[data-notify-confirm]'))
    await waitFor(() => posts().length === 1, 'the create')
    expect(posts()[0].body).toMatchObject({ summary: 'Kickoff', guests: [{ email: 'ben@example.com' }, { email: 'cy@example.com' }], onlineMeeting: true, notifyGuests: true })
    expect(await db.eventOutbox.count()).toBe(0)
    expect(onClose).toHaveBeenCalled()
  })

  it('Cancel sends nothing and keeps what was typed', async () => {
    const onClose = vi.fn()
    await render(<><EventPanel request={create()} frame="dialog" onClose={onClose} /><ScopeMenuHost /></>)
    await waitFor(() => !!document.querySelector('[data-event-form-guests]'))
    await type(q('[data-event-form-title]'), 'Kickoff')
    await typeGuest('ben@example.com')
    await waitFor(() => !!q('[data-guest-added]'))
    await submit()
    await waitFor(() => !!dialog())
    await click(q('[data-notify-cancel]'))
    await waitFor(() => !dialog())
    expect(onClose).not.toHaveBeenCalled()
    expect(q('[data-event-form-title]').value).toBe('Kickoff')
    expect(requests.filter((r) => r.method !== 'GET')).toEqual([])
    expect(await db.eventOutbox.count()).toBe(0)
  })

  it('a create without guests asks nothing and queues as before', async () => {
    const onClose = vi.fn()
    await render(<><EventPanel request={create()} frame="dialog" onClose={onClose} /><ScopeMenuHost /></>)
    await waitFor(() => !!q('[data-event-form-title]'))
    await type(q('[data-event-form-title]'), 'Solo')
    await submit()
    await waitFor(async () => (await db.eventOutbox.count()) === 1, 'the queued create')
    expect(dialog()).toBeNull()
    expect(onClose).toHaveBeenCalled()
  })

  it('a calendar that cannot invite shows no Guests field', async () => {
    await db.calendarSubscriptions.delete('work')
    await db.calendarSubscriptions.put(google)
    await render(<><EventPanel request={create({ draft: undefined })} frame="dialog" onClose={() => {}} /><ScopeMenuHost /></>)
    await waitFor(() => !!q('[data-event-form-title]'))
    // The one writable calendar is Google, which tells guests on its own terms: nothing to invite with.
    expect(document.querySelector('[data-event-form-guests]')).toBeNull()
  })
})
