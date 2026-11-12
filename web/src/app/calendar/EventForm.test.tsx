// @vitest-environment happy-dom
// The full event editor's fields (OpenSpec change calendar-event-editor, task 5.1): the zone and the time in
// the viewer's zone, All day both ways, the calendar as a picker or a label, the description's states.
import { useState } from 'react'
import { describe, expect, it } from 'vitest'
import { eventFullOf, switchAllDay, type EventFull } from '../../lib/eventFull'
import type { CalendarEvent, CalendarSubscription } from '../../lib/types'
import { click, render, type } from '../../ui/test-utils'
import { EventForm } from './EventForm'
import type { DescriptionState } from './useEventDetail'

const BERLIN = 'Europe/Berlin'
const LISBON = 'Europe/Lisbon'
const ev: CalendarEvent = { id: 'e1', subscriptionId: 's1', uid: 'u', summary: 'Review', location: 'Room 1', allDay: false, free: false, start: { date: '2026-11-13', time: '10:00', tz: BERLIN }, end: { date: '2026-11-13', time: '11:00', tz: BERLIN } }
const cal = (id: string): CalendarSubscription => ({ id, name: `Cal ${id}`, color: '', enabled: true, lastSuccessAt: 1, managed: true, connectionId: `c-${id}`, calendarUrl: `https://dav/${id}/`, editEvents: true, caps: { create: true, update: true, delete: true } })

function Harness({ initial = eventFullOf(ev, LISBON, 'Agenda'), viewer = LISBON, description = { status: 'ready', text: 'Agenda', editable: true } as DescriptionState, picker = false, onForm }: {
  initial?: EventFull; viewer?: string; description?: DescriptionState; picker?: boolean; onForm?: (f: EventFull) => void
}) {
  const [form, setForm] = useState(initial)
  onForm?.(form)
  return (
    <EventForm form={form} viewerZone={viewer} floating={false} description={description} invalid={false} onSubmit={() => {}}
      set={(p) => setForm((f) => ({ ...f, ...p }))} setAllDay={(on) => setForm((f) => switchAllDay(f, on))}
      picker={picker ? { calendars: [cal('a'), cal('b')], value: 'a', onChange: () => {} } : undefined} calendarLabel={picker ? undefined : cal('a')} />
  )
}

const q = (root: ParentNode, sel: string) => root.querySelector<HTMLInputElement>(sel)!

describe('EventForm', () => {
  it('shows the fields of D11 in order, with the title first', async () => {
    const r = await render(<Harness />)
    const form = q(r.container, '[data-event-form]')
    const names = ['[data-event-form-title]', '[role="switch"]', '[data-event-form-start]', '[data-event-form-end]', '[data-event-form-zone]', '[data-event-form-calendar]', '[data-event-form-location]', '[data-event-form-description]', '[role="radiogroup"]']
    const found = names.map((n) => form.querySelector(n)!)
    expect(found.every(Boolean)).toBe(true)
    const order = found.map((el) => [...form.querySelectorAll('*')].indexOf(el))
    expect([...order].sort((a, b) => a - b)).toEqual(order)
    expect(q(form, '[data-event-form-title]').value).toBe('Review')
  })

  it('keeps the event\'s own zone and says what it is in the viewer\'s time; the zone shows its city', async () => {
    const r = await render(<Harness />)
    expect(q(r.container, '[data-event-form-start-time]').value).toBe('10:00')
    expect(r.container.querySelector('[data-event-form-your-time]')?.textContent).toMatch(/9:00|09:00/)
    expect(r.container.querySelector<HTMLInputElement>('[data-event-form-zone] input')?.placeholder).toContain('Berlin')
  })

  it('says nothing about the viewer\'s time when the zones are the same', async () => {
    const r = await render(<Harness viewer={BERLIN} initial={eventFullOf(ev, BERLIN)} />)
    expect(r.container.querySelector('[data-event-form-your-time]')).toBeNull()
  })

  it('All day hides the times and the zone, turning it off gives 09:00 to 10:00', async () => {
    const forms: EventFull[] = []
    const r = await render(<Harness onForm={(f) => forms.push(f)} />)
    await click(q(r.container, '[role="switch"]'))
    expect(r.container.querySelector('[data-event-form-start-time]')).toBeNull()
    expect(r.container.querySelector('[data-event-form-zone]')).toBeNull()
    expect(forms.at(-1)).toMatchObject({ allDay: true, startDate: '2026-11-13', endDate: '2026-11-13' })
    await click(q(r.container, '[role="switch"]'))
    expect(q(r.container, '[data-event-form-start-time]').value).toBe('09:00')
    expect(q(r.container, '[data-event-form-end-time]').value).toBe('10:00')
  })

  it('shows the calendar as a label when editing and as a picker when creating', async () => {
    const edit = await render(<Harness />)
    expect(edit.container.querySelector('[data-event-form-calendar]')?.textContent).toBe('Cal a')
    expect(edit.container.querySelector('input[role="combobox"][aria-label="Calendar"]')).toBeNull()
    await edit.unmount()
    const create = await render(<Harness picker />)
    expect(create.container.querySelector('[data-event-form-calendar]')).toBeNull()
    expect(create.container.querySelector('input[role="combobox"]')).not.toBeNull()
  })

  it('types into the title, the location and the description', async () => {
    const forms: EventFull[] = []
    const r = await render(<Harness onForm={(f) => forms.push(f)} />)
    await type(q(r.container, '[data-event-form-title]'), 'Planning')
    await type(q(r.container, '[data-event-form-location]'), 'Room 4')
    await type(q(r.container, '[data-event-form-description]'), 'Bring x-rays')
    expect(forms.at(-1)).toMatchObject({ title: 'Planning', location: 'Room 4', description: 'Bring x-rays' })
  })

  it('shows the description as a skeleton while it loads, disabled with the reason when it failed or offline, read-only for rich text', async () => {
    const loading = await render(<Harness description={{ status: 'loading' }} />)
    expect(loading.container.querySelector('[data-event-form-description]')).toBeNull()
    expect(loading.container.querySelector('[role="status"]')).not.toBeNull()
    await loading.unmount()
    const offline = await render(<Harness description={{ status: 'unavailable', reason: 'offline' }} />)
    expect(q(offline.container, '[data-event-form-description]').disabled).toBe(true)
    expect(offline.container.textContent).toContain('offline')
    await offline.unmount()
    const failed = await render(<Harness description={{ status: 'unavailable', reason: 'failed' }} />)
    expect(q(failed.container, '[data-event-form-description]').disabled).toBe(true)
    await failed.unmount()
    const rich = await render(<Harness description={{ status: 'ready', text: 'Formatted', editable: false }} />)
    const area = q(rich.container, '[data-event-form-description]') as unknown as HTMLTextAreaElement
    expect(area.readOnly).toBe(true)
    expect(rich.container.textContent).toContain('formatting')
  })

  it('Show as is a Busy | Free choice', async () => {
    const forms: EventFull[] = []
    const r = await render(<Harness onForm={(f) => forms.push(f)} />)
    const free = [...r.container.querySelectorAll<HTMLElement>('[role="radio"]')].find((b) => b.textContent === 'Free')!
    await click(free)
    expect(forms.at(-1)!.free).toBe(true)
  })
})
