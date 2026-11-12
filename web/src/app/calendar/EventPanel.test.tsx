// @vitest-environment happy-dom
// The full event editor (OpenSpec change calendar-event-editor, task 5.1): Add event queues one create and closes,
// Save sends only the changed fields, the description is left out when it could not be loaded, a changed form
// asks before it closes, Cmd or Ctrl+Enter saves, and the frame is a panel or a Dialog.
import 'fake-indexeddb/auto'
import { act } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushEventEdits, EDIT_SETTLE_MS } from '../../lib/calendarEventEdits'
import { db, setMeta } from '../../lib/db'
import { resetDeviceId } from '../../lib/ops'
import type { CalendarEvent, CalendarEventDraft, CalendarSubscription } from '../../lib/types'
import { click, flush, key, render, type, waitFor } from '../../ui/test-utils'
import { useBulkToast } from '../bulk/toastStore'
import { resetPanel } from '../panel'
import { closeEventEditor, currentEventEditor, type EventEditorRequest } from './eventEditorStore'
import { EventPanel } from './EventPanel'

const DAY = '2026-11-13'
const sub = (id: string, name: string): CalendarSubscription => ({ id, name, color: '#2563eb', enabled: true, lastSuccessAt: Date.now(), managed: true, connectionId: `c-${id}`, calendarUrl: `https://dav/${id}/`, editEvents: true, caps: { create: true, update: true, delete: true } })
const review: CalendarEvent = { id: 'ev1', subscriptionId: 'work', uid: 'u1', summary: 'Review', location: '', start: { date: DAY, time: '10:00', tz: 'Europe/Berlin' }, end: { date: DAY, time: '11:00', tz: 'Europe/Berlin' }, allDay: false, free: false, rev: 'r1', access: { update: true, delete: true } }

let requests: { method: string; url: string; body?: Record<string, unknown> }[]
let detail: { status: number; body: unknown }

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
  detail = { status: 200, body: { event: review, description: 'Agenda', descriptionEditable: true } }
  vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
    const body = init?.body ? JSON.parse(String(init.body)) as Record<string, unknown> : undefined
    requests.push({ method: init?.method ?? 'GET', url, body })
    const json = (status: number, b: unknown) => new Response(JSON.stringify(b), { status, headers: { 'Content-Type': 'application/json' } })
    if (init?.method === 'POST') return json(201, { event: { ...review, id: 'real', uid: String(body?.clientId), summary: body?.summary, rev: 'r9' }, clientId: body?.clientId })
    if (init?.method === 'PATCH') return json(200, { event: { ...review, ...body, id: 'ev1b', rev: 'r2' }, replaces: 'ev1' })
    if (init?.method === 'DELETE') return new Response(null, { status: 204 })
    if ((url as string).includes('/events/')) return json(detail.status, detail.body)
    return json(404, {})
  }))
  await db.calendarSubscriptions.bulkPut([sub('work', 'Work'), sub('home', 'Home')])
  await db.calendarEvents.put(review)
  await setMeta('calendar.events.window', { from: '2026-09-01', to: '2026-12-30', fetchedAt: Date.now() })
})
afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
  resetPanel()
  closeEventEditor()
})

const create = (over: Partial<Extract<EventEditorRequest, { mode: 'create' }>> = {}): EventEditorRequest => ({ mode: 'create', key: 1, place: { date: DAY, start: 14 * 60, end: 15 * 60 }, ...over })
const edit: EventEditorRequest = { mode: 'edit', key: 2, eventId: 'ev1' }
const q = <T extends Element = HTMLInputElement>(sel: string) => document.querySelector<T>(sel)!
const submit = async () => { await click(q('[data-event-form-save]')) }
const send = async () => { await act(async () => { await flushEventEdits(Date.now() + EDIT_SETTLE_MS + 1000) }) }

function Toast() {
  const toast = useBulkToast()
  return <p data-test-toast>{toast?.label ?? ''}</p>
}

describe('create mode', () => {
  it('Add event queues one create with the sweep\'s times in the default calendar, closes, and the toast says Event added', async () => {
    const onClose = vi.fn()
    await render(<><EventPanel request={create()} frame="dialog" onClose={onClose} /><Toast /></>)
    await waitFor(() => !!q('[data-event-form-title]'), 'the form')
    expect(q('[data-event-form-start-time]').value).toBe('14:00')
    expect(q('[data-event-form-end-time]').value).toBe('15:00')
    expect(q('[data-event-form-save]').hasAttribute('disabled')).toBe(true) // no title yet
    await type(q('[data-event-form-title]'), 'Call Ana')
    await submit()
    expect(onClose).toHaveBeenCalledTimes(1)
    await waitFor(async () => (await db.eventOutbox.count()) === 1, 'the queued create')
    const cmd = (await db.eventOutbox.toArray())[0]
    const draft = (cmd.patch as { create: CalendarEventDraft }).create
    expect(draft).toMatchObject({ summary: 'Call Ana', connectionId: 'c-home', calendarUrl: 'https://dav/home/', start: { date: DAY, time: '14:00' }, end: { date: DAY, time: '15:00' }, allDay: false, free: false })
    expect(draft.start.tz).toBeTruthy()
    expect(document.querySelector('[data-test-toast]')!.textContent).toBe('Event added')
    await send()
    expect(requests.filter((r) => r.method === 'POST')).toHaveLength(1)
  })

  it('opens with the remembered calendar and a Duplicate draft in its own fields', async () => {
    await setMeta('calendar.eventCalendar', 'work')
    const draft: CalendarEventDraft = { clientId: 'old-id-1234', connectionId: 'c-work', calendarUrl: 'https://dav/work/', subscriptionId: 'work', summary: 'Copy me', start: { date: DAY, time: '09:00', tz: 'Europe/Berlin' }, end: { date: DAY, time: '09:30', tz: 'Europe/Berlin' }, allDay: false, location: 'Room 4', description: 'Notes', free: true }
    await render(<EventPanel request={create({ draft, place: undefined })} frame="dialog" onClose={() => {}} />)
    await waitFor(() => q('[data-event-form-title]').value === 'Copy me', 'the draft')
    expect(q('[data-event-form-location]').value).toBe('Room 4')
    expect(q<HTMLTextAreaElement>('[data-event-form-description]').value).toBe('Notes')
    expect(document.querySelector('[data-add-kind]')).toBeNull() // Duplicate has no Task | Event control
    await type(q('[data-event-form-title]'), 'Copy of it')
    await submit()
    await waitFor(async () => (await db.eventOutbox.count()) === 1, 'the queued create')
    const made = ((await db.eventOutbox.toArray())[0].patch as { create: CalendarEventDraft }).create
    expect(made.clientId).not.toBe('old-id-1234')
    expect(made).toMatchObject({ subscriptionId: 'work', location: 'Room 4', description: 'Notes', free: true })
  })

  it('has the Task | Event control, with Event chosen', async () => {
    await render(<EventPanel request={create()} frame="dialog" onClose={() => {}} />)
    await waitFor(() => !!q('[data-add-kind]'), 'the control')
    expect(q('[data-add-kind] [aria-checked="true"]').textContent).toBe('Event')
  })
})

describe('edit mode', () => {
  it('reads the description when it opens, and Save sends only the fields that changed', async () => {
    await render(<EventPanel request={edit} frame="dialog" onClose={() => {}} />)
    await waitFor(() => q<HTMLTextAreaElement>('[data-event-form-description]')?.value === 'Agenda', 'the description')
    expect(requests.some((r) => r.method === 'GET' && r.url.endsWith('/c-work/events/ev1'))).toBe(true)
    expect(q('[data-event-form-save]').hasAttribute('disabled')).toBe(true) // nothing changed
    await type(q('[data-event-form-title]'), 'Planning')
    await submit()
    await send()
    const patch = requests.find((r) => r.method === 'PATCH')!
    expect(patch.body).toEqual({ rev: 'r1', summary: 'Planning' })
  })

  it('Save carries a location, a description and Free in one change', async () => {
    await render(<EventPanel request={edit} frame="dialog" onClose={() => {}} />)
    await waitFor(() => q<HTMLTextAreaElement>('[data-event-form-description]')?.value === 'Agenda', 'the description')
    await type(q('[data-event-form-location]'), 'Room 4')
    await type(q('[data-event-form-description]'), 'New agenda')
    await click([...document.querySelectorAll<HTMLElement>('[role="radio"]')].find((b) => b.textContent === 'Free')!)
    await submit()
    await send()
    expect(requests.filter((r) => r.method === 'PATCH')).toHaveLength(1)
    expect(requests.find((r) => r.method === 'PATCH')!.body).toEqual({ rev: 'r1', location: 'Room 4', description: 'New agenda', free: true })
  })

  it('leaves the description out when it failed to load', async () => {
    detail = { status: 500, body: { error: { code: 'calendar_unreachable', message: 'down' } } }
    await render(<EventPanel request={edit} frame="dialog" onClose={() => {}} />)
    await waitFor(() => q<HTMLTextAreaElement>('[data-event-form-description]')?.disabled === true, 'the disabled description')
    await type(q('[data-event-form-title]'), 'Planning')
    await submit()
    await send()
    expect(requests.find((r) => r.method === 'PATCH')!.body).toEqual({ rev: 'r1', summary: 'Planning' })
  })

  it('leaves the description out when it is rich text (descriptionEditable false)', async () => {
    detail = { status: 200, body: { event: review, description: 'Formatted', descriptionEditable: false } }
    await render(<EventPanel request={edit} frame="dialog" onClose={() => {}} />)
    await waitFor(() => q<HTMLTextAreaElement>('[data-event-form-description]')?.readOnly === true, 'the read-only description')
    expect(q<HTMLTextAreaElement>('[data-event-form-description]').value).toBe('Formatted')
    await type(q('[data-event-form-location]'), 'Room 4')
    await submit()
    await send()
    expect(requests.find((r) => r.method === 'PATCH')!.body).toEqual({ rev: 'r1', location: 'Room 4' })
  })

  it('swaps to the newer version the calendar answered with (replaces) before the form binds its rev', async () => {
    detail = { status: 200, body: { event: { ...review, id: 'ev1n', rev: 'r7', summary: 'Review (moved)' }, replaces: 'ev1', description: '', descriptionEditable: true } }
    await render(<EventPanel request={edit} frame="dialog" onClose={() => {}} />)
    await waitFor(() => q('[data-event-form-title]')?.value === 'Review (moved)', 'the current title')
    expect(await db.calendarEvents.get('ev1')).toBeUndefined()
    expect((await db.calendarEvents.get('ev1n'))!.rev).toBe('r7')
    await type(q('[data-event-form-title]'), 'Final')
    await submit()
    await send()
    expect(requests.find((r) => r.method === 'PATCH')!.body).toEqual({ rev: 'r7', summary: 'Final' })
  })

  it('shows the calendar as a label, not a picker', async () => {
    await render(<EventPanel request={edit} frame="dialog" onClose={() => {}} />)
    await waitFor(() => !!q('[data-event-form-calendar]'), 'the label')
    expect(q('[data-event-form-calendar]').textContent).toBe('Work')
  })

  it('closes with a message when the calendar no longer has the event', async () => {
    detail = { status: 404, body: { error: { code: 'event_gone', message: 'gone' } } }
    const onClose = vi.fn()
    await render(<><EventPanel request={edit} frame="dialog" onClose={onClose} /><Toast /></>)
    await waitFor(() => onClose.mock.calls.length > 0, 'the close')
    expect(document.querySelector('[data-test-toast]')!.textContent).toContain('deleted in your calendar')
    expect(await db.calendarEvents.get('ev1')).toBeUndefined()
  })

  it('works offline for everything except the description', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => { throw new TypeError('offline') }))
    await render(<EventPanel request={edit} frame="dialog" onClose={() => {}} />)
    await waitFor(() => q<HTMLTextAreaElement>('[data-event-form-description]')?.disabled === true, 'the disabled description')
    expect(document.body.textContent).toContain('offline')
    await type(q('[data-event-form-location]'), 'Room 4')
    await submit()
    await waitFor(async () => (await db.eventOutbox.count()) === 1, 'the queued edit')
    expect((await db.eventOutbox.toArray())[0].patch).toEqual({ location: 'Room 4' })
  })
})

describe('closing and saving by key', () => {
  it('a changed form asks "Discard changes?" before it closes, and Keep editing stays', async () => {
    const onClose = vi.fn()
    await render(<EventPanel request={edit} frame="dialog" onClose={onClose} />)
    await waitFor(() => q<HTMLTextAreaElement>('[data-event-form-description]')?.value === 'Agenda', 'the form')
    await type(q('[data-event-form-location]'), 'Room 4')
    await key(document, 'Escape')
    await waitFor(() => !!document.querySelector('[data-testid="confirm-dialog"]'), 'the confirm')
    expect(document.body.textContent).toContain('Discard changes?')
    expect(onClose).not.toHaveBeenCalled()
    await click([...document.querySelectorAll('button')].find((b) => b.textContent === 'Keep editing')!)
    expect(onClose).not.toHaveBeenCalled()
    await key(document, 'Escape')
    await waitFor(() => !!document.querySelector('[data-testid="confirm-dialog"]'), 'the confirm again')
    await click([...document.querySelectorAll('button')].find((b) => b.textContent === 'Discard')!)
    await waitFor(() => onClose.mock.calls.length > 0, 'the close')
  })

  it('an untouched form closes at once', async () => {
    const onClose = vi.fn()
    await render(<EventPanel request={edit} frame="dialog" onClose={onClose} />)
    await waitFor(() => q<HTMLTextAreaElement>('[data-event-form-description]')?.value === 'Agenda', 'the form')
    await key(document, 'Escape')
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('Ctrl+Enter saves', async () => {
    const onClose = vi.fn()
    await render(<EventPanel request={edit} frame="dialog" onClose={onClose} />)
    await waitFor(() => q<HTMLTextAreaElement>('[data-event-form-description]')?.value === 'Agenda', 'the form')
    await type(q('[data-event-form-title]'), 'Planning')
    await key(q('[data-event-form-title]'), 'Enter', { ctrlKey: true })
    await send()
    expect(onClose).toHaveBeenCalled()
    expect(requests.find((r) => r.method === 'PATCH')!.body).toEqual({ rev: 'r1', summary: 'Planning' })
  })
})

describe('frames', () => {
  it('is a Dialog below 1024 px and a panel in the side slot from it; the same form in both', async () => {
    const dialog = await render(<EventPanel request={edit} frame="dialog" onClose={() => {}} />)
    await waitFor(() => !!q('[data-event-form]'), 'the form')
    expect(document.querySelector('[data-testid="event-editor-dialog"]')).not.toBeNull()
    expect(document.querySelector('[data-panel-frame]')).toBeNull()
    await dialog.unmount()
    await render(<EventPanel request={edit} frame="panel" onClose={() => {}} />)
    await waitFor(() => !!q('[data-event-form]'), 'the form')
    expect(document.querySelector('[data-panel-frame="event-editor"]')).not.toBeNull()
    expect(document.querySelector('[data-testid="event-editor-dialog"]')).toBeNull()
  })

  it('Duplicate opens the editor in create mode with the same fields and a new client id', async () => {
    await render(<EventPanel request={edit} frame="dialog" onClose={() => {}} />)
    await waitFor(() => q<HTMLTextAreaElement>('[data-event-form-description]')?.value === 'Agenda', 'the form')
    await click(q('[data-event-form-menu]'))
    await click(q('[data-event-form-duplicate]'))
    const next = currentEventEditor()
    expect(next).toMatchObject({ mode: 'create', draft: { summary: 'Review', description: 'Agenda', subscriptionId: 'work', connectionId: 'c-work', start: review.start, end: review.end } })
    expect(requests.filter((r) => r.method === 'POST')).toHaveLength(0) // nothing is created until Save
    await flush(0)
  })
})
