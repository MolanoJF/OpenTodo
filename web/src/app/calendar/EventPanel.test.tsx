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
import type { CalendarEvent, CalendarEventDraft, CalendarRepeat, CalendarSubscription } from '../../lib/types'
import { click, flush, key, render, type, waitFor } from '../../ui/test-utils'
import { useBulkToast } from '../bulk/toastStore'
import { resetPanel } from '../panel'
import { closeEventEditor, currentEventEditor, type EventEditorRequest } from './eventEditorStore'
import { EventPanel } from './EventPanel'
import { ScopeMenuHost } from './ScopeMenuHost'

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

// ---- repeating events (OpenSpec change calendar-event-series, task 5.4) ----
const daily: CalendarRepeat = { freq: 'daily', interval: 1, days: [], ends: { type: 'never' } }
const routine: CalendarEvent = { ...review, id: 'ev9', uid: 'u9', summary: 'Routine', series: { id: 'ser1', repeat: daily, first: false, rev: 'r1' } }
const routineEdit: EventEditorRequest = { mode: 'edit', key: 3, eventId: 'ev9' }
const seriesDetail = (over: Record<string, unknown> = {}) => ({ event: routine, description: '', descriptionEditable: true, series: { rrule: 'FREQ=DAILY', repeat: daily, custom: false, exceptions: 2 }, reminders: [10], remindersMax: 5, remindersOther: 1, ...over })
const items = () => [...document.querySelectorAll<HTMLButtonElement>('[data-event-scope]')]
const choose = async (scope: string) => { await waitFor(() => items().length > 0, 'the scope menu'); await act(async () => items().find((i) => i.getAttribute('data-event-scope') === scope)!.click()) }
const patches = () => requests.filter((r) => r.method === 'PATCH')
const reminderRows = () => [...document.querySelectorAll('[data-event-form-reminder]')].map((li) => li.textContent)

describe('repeating events', () => {
  beforeEach(async () => {
    await db.calendarEvents.put(routine)
    detail = { status: 200, body: seriesDetail() }
  })

  it('Save on an occurrence asks which events and sends the chosen scope; nothing is sent before an answer', async () => {
    const onClose = vi.fn()
    await render(<><EventPanel request={routineEdit} frame="dialog" onClose={onClose} /><ScopeMenuHost /></>)
    await waitFor(() => q('[data-event-form-location]') !== null && document.querySelector('[data-event-form-reminder]') !== null, 'the detail')
    await type(q('[data-event-form-location]'), 'Gym')
    await submit()
    await waitFor(() => items().length === 3, 'the menu')
    expect(patches()).toHaveLength(0)
    expect(onClose).not.toHaveBeenCalled()
    await choose('all')
    await waitFor(() => onClose.mock.calls.length > 0, 'the close after the answer')
    await send()
    expect(patches()).toHaveLength(1)
    expect(patches()[0].body).toEqual({ rev: 'r1', scope: 'all', location: 'Gym' })
  })

  it('cancelling the menu leaves the editor open with what was typed and sends nothing', async () => {
    const onClose = vi.fn()
    await render(<><EventPanel request={routineEdit} frame="dialog" onClose={onClose} /><ScopeMenuHost /></>)
    await waitFor(() => document.querySelector('[data-event-form-reminder]') !== null, 'the detail')
    await type(q('[data-event-form-location]'), 'Gym')
    await submit()
    await waitFor(() => items().length === 3, 'the menu')
    await key(document.activeElement!, 'Escape')
    await waitFor(() => items().length === 0, 'the menu closed')
    expect(onClose).not.toHaveBeenCalled()
    expect(q('[data-event-form-location]').value).toBe('Gym')
    await send()
    expect(patches()).toHaveLength(0)
  })

  it('a time change of an occurrence offers all three scopes; the first occurrence has no "this and following"', async () => {
    detail = { status: 200, body: seriesDetail({ event: { ...routine, series: { ...routine.series!, first: true } } }) }
    await render(<><EventPanel request={routineEdit} frame="dialog" onClose={() => {}} /><ScopeMenuHost /></>)
    await waitFor(() => document.querySelector('[data-event-form-reminder]') !== null, 'the detail')
    await type(q('[data-event-form-start-time]'), '08:00')
    await type(q('[data-event-form-end-time]'), '09:30')
    await submit()
    await waitFor(() => items().length > 0)
    expect(items().map((i) => i.getAttribute('data-event-scope'))).toEqual(['this', 'all'])
  })

  it('shows the series rule from the detail, and a new weekday repeat offers only following and all', async () => {
    await render(<><EventPanel request={routineEdit} frame="dialog" onClose={() => {}} /><ScopeMenuHost /></>)
    await waitFor(() => document.querySelector<HTMLInputElement>('[data-event-form-repeat] input[role="combobox"]')?.placeholder === 'Every day', 'the loaded rule')
    const input = q('[data-event-form-repeat] input[role="combobox"]')
    await act(async () => { input.focus() })
    await click(input)
    await waitFor(() => document.querySelector('[role="option"]') !== null)
    await click([...document.querySelectorAll<HTMLElement>('[role="option"]')].find((o) => o.textContent?.trim() === 'Every weekday')!)
    expect(q('[data-event-form-repeat-resets]').textContent).toContain('resets events you changed individually')
    await submit()
    await waitFor(() => items().length > 0, 'the menu')
    expect(items().map((i) => i.getAttribute('data-event-scope'))).toEqual(['following', 'all'])
    expect(document.getElementById(items()[1].getAttribute('aria-describedby')!)!.textContent).toBe('Changing the repeat resets events you changed individually')
    await choose('all')
    await send()
    expect(patches()[0].body).toEqual({ rev: 'r1', scope: 'all', repeat: { freq: 'weekly', interval: 1, days: ['mo', 'tu', 'we', 'th', 'fr'], ends: { type: 'never' } } })
  })

  it('a custom rule reads "Custom repeat", the other fields save, and the rule is not sent', async () => {
    detail = { status: 200, body: seriesDetail({ series: { rrule: 'FREQ=MONTHLY;BYSETPOS=-1;BYDAY=MO,TU,WE,TH,FR', repeat: null, custom: true, exceptions: 0 } }) }
    await db.calendarEvents.put({ ...routine, series: { id: 'ser1', repeat: null, custom: true, first: false, rev: 'r1' } })
    await render(<><EventPanel request={routineEdit} frame="dialog" onClose={() => {}} /><ScopeMenuHost /></>)
    await waitFor(() => document.querySelector('[data-event-form-repeat-read-only]') !== null, 'the read-only rule')
    expect(q('[data-event-form-repeat-read-only]').textContent).toBe('Custom repeat')
    await type(q('[data-event-form-title]'), 'Month end')
    await submit()
    await choose('all')
    await send()
    expect(patches()[0].body).toEqual({ rev: 'r1', scope: 'all', summary: 'Month end' })
  })

  it('reminders come from the detail and are sent only when changed', async () => {
    await render(<><EventPanel request={routineEdit} frame="dialog" onClose={() => {}} /><ScopeMenuHost /></>)
    await waitFor(() => reminderRows().length === 1, 'the reminders')
    expect(reminderRows()).toEqual(['10 minutes before'])
    expect(q('[data-event-form-reminders-other]').textContent).toBe('1 other reminder is kept')
    await click(q('[data-event-form-add-reminder]'))
    await waitFor(() => document.querySelector('[role="menuitem"]') !== null)
    await click([...document.querySelectorAll('[role="menuitem"]')].find((i) => i.textContent?.trim() === '1 day before')!)
    await submit()
    await choose('this')
    await send()
    expect(patches()[0].body).toEqual({ rev: 'r1', scope: 'this', reminders: [10, 1440] })
  })

  it('a Microsoft calendar takes one reminder and its series cannot stop repeating', async () => {
    await db.calendarSubscriptions.put({ ...sub('work', 'Work'), maxReminders: 1 })
    detail = { status: 200, body: seriesDetail({ remindersMax: 1, remindersOther: 0 }) }
    await render(<><EventPanel request={routineEdit} frame="dialog" onClose={() => {}} /><ScopeMenuHost /></>)
    await waitFor(() => reminderRows().length === 1, 'the reminders')
    expect(document.querySelector('[data-event-form-add-reminder]')).toBeNull()
    const input = q('[data-event-form-repeat] input[role="combobox"]')
    await act(async () => { input.focus() })
    await click(input)
    await waitFor(() => document.querySelector('[role="option"]') !== null)
    expect([...document.querySelectorAll('[role="option"]')].map((o) => o.textContent?.trim())).not.toContain('Does not repeat')
  })

  it('offline, the reminders are disabled with the reason and left out; the rule from the events route still shows', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => { throw new TypeError('offline') }))
    await render(<><EventPanel request={routineEdit} frame="dialog" onClose={() => {}} /><ScopeMenuHost /></>)
    await waitFor(() => q<HTMLTextAreaElement>('[data-event-form-description]')?.disabled === true, 'offline')
    expect(q('[data-event-form-reminders]').textContent).toContain('offline')
    expect(q<HTMLInputElement>('[data-event-form-repeat] input[role="combobox"]').placeholder).toBe('Every day')
    await type(q('[data-event-form-location]'), 'Gym')
    await submit()
    await choose('this')
    await waitFor(async () => (await db.eventOutbox.count()) === 1)
    expect((await db.eventOutbox.toArray())[0]).toMatchObject({ scope: 'this', patch: { location: 'Gym' } })
  })

  it('Delete from the editor asks which events and queues the chosen scope', async () => {
    const onClose = vi.fn()
    await render(<><EventPanel request={routineEdit} frame="dialog" onClose={onClose} /><ScopeMenuHost /></>)
    await waitFor(() => document.querySelector('[data-event-form-menu]') !== null)
    await click(q('[data-event-form-menu]'))
    await waitFor(() => document.querySelector('[data-event-form-delete]') !== null)
    await click(q('[data-event-form-delete]'))
    await waitFor(() => items().length === 3, 'the menu')
    expect(document.querySelector('[role="menu"][aria-label="Delete which events"]')).not.toBeNull()
    await choose('following')
    await waitFor(async () => (await db.eventOutbox.count()) === 1, 'the queued delete')
    expect((await db.eventOutbox.toArray())[0]).toMatchObject({ scope: 'following', patch: { delete: true } })
  })

  it('a single event never asks for a scope', async () => {
    detail = { status: 200, body: { event: review, description: 'Agenda', descriptionEditable: true, reminders: [], remindersMax: 5, remindersOther: 0 } }
    await render(<><EventPanel request={edit} frame="dialog" onClose={() => {}} /><ScopeMenuHost /></>)
    await waitFor(() => q<HTMLTextAreaElement>('[data-event-form-description]')?.value === 'Agenda', 'the description')
    await type(q('[data-event-form-title]'), 'Planning')
    await submit()
    await send()
    expect(items()).toHaveLength(0)
    expect(patches()[0].body).toEqual({ rev: 'r1', summary: 'Planning' })
  })

  it('makes a single event repeat from its own start, with no scope', async () => {
    detail = { status: 200, body: { event: review, description: 'Agenda', descriptionEditable: true, reminders: [], remindersMax: 5, remindersOther: 0 } }
    await render(<><EventPanel request={edit} frame="dialog" onClose={() => {}} /><ScopeMenuHost /></>)
    await waitFor(() => q<HTMLTextAreaElement>('[data-event-form-description]')?.value === 'Agenda', 'the description')
    const input = q('[data-event-form-repeat] input[role="combobox"]')
    await act(async () => { input.focus() })
    await click(input)
    await waitFor(() => document.querySelector('[role="option"]') !== null)
    await click([...document.querySelectorAll<HTMLElement>('[role="option"]')].find((o) => o.textContent?.trim() === 'Every week on Friday')!)
    await submit()
    await send()
    expect(items()).toHaveLength(0)
    expect(patches()[0].body).toEqual({ rev: 'r1', repeat: { freq: 'weekly', interval: 1, days: [], ends: { type: 'never' } } })
  })

  it('creates a new weekly event with its repeat and reminders and no scope', async () => {
    await render(<><EventPanel request={create()} frame="dialog" onClose={() => {}} /><ScopeMenuHost /></>)
    await waitFor(() => !!q('[data-event-form-title]'), 'the form')
    await type(q('[data-event-form-title]'), 'Stand-up')
    const input = q('[data-event-form-repeat] input[role="combobox"]')
    await act(async () => { input.focus() })
    await click(input)
    await waitFor(() => document.querySelector('[role="option"]') !== null)
    await click([...document.querySelectorAll<HTMLElement>('[role="option"]')].find((o) => o.textContent?.trim() === 'Every weekday')!)
    await click(q('[data-event-form-add-reminder]'))
    await waitFor(() => document.querySelector('[role="menuitem"]') !== null)
    await click([...document.querySelectorAll('[role="menuitem"]')].find((i) => i.textContent?.trim() === '10 minutes before')!)
    await submit()
    await send()
    const post = requests.find((r) => r.method === 'POST')!
    expect(post.body).toMatchObject({ summary: 'Stand-up', repeat: { freq: 'weekly', interval: 1, days: ['mo', 'tu', 'we', 'th', 'fr'], ends: { type: 'never' } }, reminders: [10] })
    expect(post.body).not.toHaveProperty('scope')
  })

  it('Duplicate of an occurrence is a single event: no repeat in the new draft', async () => {
    await render(<><EventPanel request={routineEdit} frame="dialog" onClose={() => {}} /><ScopeMenuHost /></>)
    await waitFor(() => document.querySelector('[data-event-form-menu]') !== null)
    await click(q('[data-event-form-menu]'))
    await waitFor(() => document.querySelector('[data-event-form-duplicate]') !== null)
    await click(q('[data-event-form-duplicate]'))
    const next = currentEventEditor()
    expect(next).toMatchObject({ mode: 'create' })
    expect((next as { draft: CalendarEventDraft }).draft.repeat).toBeUndefined()
  })
})
