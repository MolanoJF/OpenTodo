// @vitest-environment happy-dom
// The editor's repeat and reminders fields (OpenSpec change calendar-event-series, tasks 5.2 and 5.3, design D12):
// each preset produces the REST object of D3, "Every weekday" is recognised on read, Custom opens the fields, a
// custom series is read-only text, reminders add and remove, Microsoft takes one, the other reminders are kept.
import { act, useState } from 'react'
import { describe, expect, it } from 'vitest'
import { eventFullOf, type EventFull } from '../../lib/eventFull'
import type { CalendarEvent, CalendarRepeat } from '../../lib/types'
import { click, render, type, waitFor } from '../../ui/test-utils'
import { EventForm } from './EventForm'
import type { RemindersFieldProps } from './EventRemindersField'
import type { RepeatFieldProps } from './EventRepeatField'

const BERLIN = 'Europe/Berlin'
// 2026-11-13 is a Friday, the second Friday of its month.
const ev: CalendarEvent = { id: 'e1', subscriptionId: 's1', uid: 'u', summary: 'Routine', location: '', allDay: false, free: false, start: { date: '2026-11-13', time: '07:00', tz: BERLIN }, end: { date: '2026-11-13', time: '08:30', tz: BERLIN } }
const never = { type: 'never' } as const

interface Over {
  repeat?: Partial<Omit<RepeatFieldProps, 'startDate' | 'value' | 'onChange'>>
  reminders?: Partial<Omit<RemindersFieldProps, 'allDay' | 'value' | 'onChange'>>
  initial?: Partial<EventFull>
}

function Harness({ over = {}, onForm }: { over?: Over; onForm?: (f: EventFull) => void }) {
  const [form, setForm] = useState<EventFull>({ ...eventFullOf(ev, BERLIN), ...over.initial })
  onForm?.(form)
  return (
    <EventForm form={form} viewerZone={BERLIN} floating={false} description={{ status: 'ready', text: '', editable: true }} invalid={false} onSubmit={() => {}}
      set={(p) => setForm((f) => ({ ...f, ...p }))} setAllDay={(on) => setForm((f) => ({ ...f, allDay: on, startTime: on ? '' : '09:00', endTime: on ? '' : '10:00' }))}
      repeat={{ value: form.repeat, onChange: (repeat) => setForm((f) => ({ ...f, repeat })), custom: false, state: 'ready', canStop: true, resets: false, ...over.repeat }}
      reminders={{ value: form.reminders, onChange: (reminders) => setForm((f) => ({ ...f, reminders })), max: 5, microsoft: false, other: 0, state: 'ready', ...over.reminders }} />
  )
}

const q = <T extends Element = HTMLElement>(sel: string) => document.querySelector<T>(sel)!
const repeatInput = () => q<HTMLInputElement>('[data-event-form-repeat] input[role="combobox"]')
const options = () => [...document.querySelectorAll<HTMLElement>('[role="option"]')]

async function pickPreset(label: string) {
  await act(async () => { repeatInput().focus() })
  await click(repeatInput())
  await waitFor(() => options().some((o) => o.textContent?.trim() === label), `the option ${label}`)
  await click(options().find((o) => o.textContent?.trim() === label)!)
}

describe('the repeat field: presets', () => {
  it('lists the presets of D12 in order', async () => {
    const r = await render(<Harness />)
    expect(repeatInput().placeholder).toBe('Does not repeat')
    await act(async () => { repeatInput().focus() })
    await click(repeatInput())
    await waitFor(() => options().length > 0, 'the options')
    expect(options().map((o) => o.textContent?.trim())).toEqual([
      'Does not repeat', 'Every day', 'Every weekday', 'Every week on Friday', 'Every month on day 13', 'Every month on the second Friday', 'Every year on November 13', 'Custom…',
    ])
    await r.unmount()
  })

  it.each<[string, CalendarRepeat]>([
    ['Every day', { freq: 'daily', interval: 1, days: [], ends: never }],
    ['Every weekday', { freq: 'weekly', interval: 1, days: ['mo', 'tu', 'we', 'th', 'fr'], ends: never }],
    ['Every week on Friday', { freq: 'weekly', interval: 1, days: [], ends: never }],
    ['Every month on day 13', { freq: 'monthly', interval: 1, days: [], by: 'monthday', ends: never }],
    ['Every month on the second Friday', { freq: 'monthly', interval: 1, days: [], by: 'weekday', ends: never }],
    ['Every year on November 13', { freq: 'yearly', interval: 1, days: [], ends: never }],
  ])('%s makes the REST object', async (label, want) => {
    const forms: EventFull[] = []
    const r = await render(<Harness onForm={(f) => forms.push(f)} />)
    await pickPreset(label)
    expect(forms.at(-1)!.repeat).toEqual(want)
    expect(repeatInput().placeholder).toBe(label)
    await r.unmount()
  })

  it('Does not repeat sets none, and is offered only where the calendar can stop a series', async () => {
    const forms: EventFull[] = []
    const r = await render(<Harness over={{ initial: { repeat: { freq: 'daily', interval: 1, days: [], ends: never } } }} onForm={(f) => forms.push(f)} />)
    await pickPreset('Does not repeat')
    expect(forms.at(-1)!.repeat).toBeNull()
    await r.unmount()
    const ms = await render(<Harness over={{ repeat: { canStop: false }, initial: { repeat: { freq: 'daily', interval: 1, days: [], ends: never } } }} />)
    await act(async () => { repeatInput().focus() })
    await click(repeatInput())
    await waitFor(() => options().length > 0)
    expect(options().map((o) => o.textContent?.trim())).not.toContain('Does not repeat')
    await ms.unmount()
  })

  it('recognises "Every weekday" and the other presets on read', async () => {
    const r = await render(<Harness over={{ initial: { repeat: { freq: 'weekly', interval: 1, days: ['fr', 'mo', 'we', 'tu', 'th'], ends: never } } }} />)
    expect(repeatInput().placeholder).toBe('Every weekday')
    expect(document.querySelector('[data-event-form-repeat-custom-fields]')).toBeNull()
    await r.unmount()
  })

  it('keeps the way it ends when the preset changes', async () => {
    const forms: EventFull[] = []
    const r = await render(<Harness over={{ initial: { repeat: { freq: 'daily', interval: 1, days: [], ends: { type: 'after', count: 5 } } } }} onForm={(f) => forms.push(f)} />)
    await pickPreset('Every weekday')
    expect(forms.at(-1)!.repeat).toMatchObject({ freq: 'weekly', ends: { type: 'after', count: 5 } })
    await r.unmount()
  })
})

describe('the repeat field: Custom', () => {
  it('opens the fields with every week on the start weekday, and each field edits the rule', async () => {
    const forms: EventFull[] = []
    const r = await render(<Harness onForm={(f) => forms.push(f)} />)
    await pickPreset('Custom…')
    expect(q('[data-event-form-repeat-custom-fields]')).not.toBeNull()
    expect(forms.at(-1)!.repeat).toEqual({ freq: 'weekly', interval: 1, days: ['fr'], ends: never })
    // Interval.
    const interval = q<HTMLInputElement>('[data-event-form-repeat-custom-fields] input')
    await type(interval, '2')
    expect(forms.at(-1)!.repeat).toMatchObject({ interval: 2 })
    // Weekdays: Monday on, Friday stays; the last day cannot be switched off.
    await click(q('[data-repeat-day="mo"]'))
    expect(forms.at(-1)!.repeat!.days).toEqual(['fr', 'mo'])
    expect(q('[data-repeat-day="mo"]').getAttribute('aria-pressed')).toBe('true')
    await click(q('[data-repeat-day="fr"]'))
    await click(q('[data-repeat-day="mo"]'))
    expect(forms.at(-1)!.repeat!.days).toEqual(['mo'])
    // Unit: monthly drops the weekdays and gives the day of the month.
    await click([...document.querySelectorAll<HTMLElement>('[role="radio"]')].find((b) => b.textContent === 'Month')!)
    expect(forms.at(-1)!.repeat).toEqual({ freq: 'monthly', interval: 2, days: [], by: 'monthday', ends: never })
    await click([...document.querySelectorAll<HTMLElement>('[role="radio"]')].find((b) => b.textContent === 'Weekday of the month')!)
    expect(forms.at(-1)!.repeat).toMatchObject({ freq: 'monthly', by: 'weekday' })
    await r.unmount()
  })

  it('ends never, on a date, or after a number of times', async () => {
    const forms: EventFull[] = []
    const r = await render(<Harness over={{ initial: { repeat: { freq: 'daily', interval: 1, days: [], ends: never } } }} onForm={(f) => forms.push(f)} />)
    const radio = (label: string) => [...document.querySelectorAll<HTMLElement>('[data-event-form-repeat-ends] [role="radio"]')].find((b) => b.textContent === label)!
    await click(radio('After'))
    expect(forms.at(-1)!.repeat!.ends).toEqual({ type: 'after', count: 10 })
    await type(q<HTMLInputElement>('[data-event-form-repeat-ends] input'), '4')
    expect(forms.at(-1)!.repeat!.ends).toEqual({ type: 'after', count: 4 })
    await click(radio('On a date'))
    expect(forms.at(-1)!.repeat!.ends).toEqual({ type: 'on', date: '2026-12-13' })
    await type(q<HTMLInputElement>('[data-event-form-repeat-until]'), '2026-12-31')
    expect(forms.at(-1)!.repeat!.ends).toEqual({ type: 'on', date: '2026-12-31' })
    await click(radio('Never'))
    expect(forms.at(-1)!.repeat!.ends).toEqual(never)
    await r.unmount()
  })

  it('a rule no preset says opens the fields on read and names itself', async () => {
    const r = await render(<Harness over={{ initial: { repeat: { freq: 'weekly', interval: 2, days: ['mo', 'we'], ends: never } } }} />)
    expect(q('[data-event-form-repeat-custom-fields]')).not.toBeNull()
    expect(repeatInput().placeholder).toBe('Every 2 weeks on Monday, Wednesday')
    await r.unmount()
  })

  it('a custom series shows "Custom repeat" as read-only text with no way to change it', async () => {
    const r = await render(<Harness over={{ repeat: { custom: true } }} />)
    expect(q('[data-event-form-repeat-read-only]').textContent).toBe('Custom repeat')
    expect(document.querySelector('[data-event-form-repeat] input[role="combobox"]')).toBeNull()
    expect(q<HTMLInputElement>('[data-event-form-title]').disabled).toBe(false) // the other fields stay editable
    await r.unmount()
  })

  it('shows a skeleton while the rule loads, and says so when it could not be read', async () => {
    const loading = await render(<Harness over={{ repeat: { state: 'loading' } }} />)
    expect(q('[data-event-form-repeat] [role="status"]')).not.toBeNull()
    expect(document.querySelector('[data-event-form-repeat] input')).toBeNull()
    await loading.unmount()
    const failed = await render(<Harness over={{ repeat: { state: 'unavailable' } }} />)
    expect(q('[data-event-form-repeat]').textContent).toContain('left as it is')
    await failed.unmount()
  })

  it('warns beside the field that a new repeat resets events changed one by one', async () => {
    const r = await render(<Harness over={{ repeat: { resets: true }, initial: { repeat: { freq: 'daily', interval: 1, days: [], ends: never } } }} />)
    expect(q('[data-event-form-repeat-resets]').textContent).toBe('Changing the repeat resets events you changed individually')
    await r.unmount()
  })
})

describe('the reminders field', () => {
  const addPreset = async (label: string) => {
    await click(q('[data-event-form-add-reminder]'))
    await waitFor(() => [...document.querySelectorAll('[role="menuitem"]')].some((i) => i.textContent?.trim() === label), `the preset ${label}`)
    await click([...document.querySelectorAll('[role="menuitem"]')].find((i) => i.textContent?.trim() === label)!)
  }

  it('adds from the presets, in order of time, and removes with a labelled button', async () => {
    const forms: EventFull[] = []
    const r = await render(<Harness onForm={(f) => forms.push(f)} />)
    await addPreset('1 day before')
    await addPreset('10 minutes before')
    expect(forms.at(-1)!.reminders).toEqual([10, 1440])
    expect([...document.querySelectorAll('[data-event-form-reminder]')].map((li) => li.textContent)).toEqual(['10 minutes before', '1 day before'])
    await click(q('[data-event-form-reminder="10"] button[aria-label="Remove reminder"]'))
    expect(forms.at(-1)!.reminders).toEqual([1440])
    await r.unmount()
  })

  it('does not offer a preset that is already in the list', async () => {
    const r = await render(<Harness over={{ initial: { reminders: [10] } }} />)
    await click(q('[data-event-form-add-reminder]'))
    await waitFor(() => document.querySelector('[role="menuitem"]') !== null)
    expect([...document.querySelectorAll('[role="menuitem"]')].map((i) => i.textContent?.trim())).not.toContain('10 minutes before')
    await r.unmount()
  })

  it('stops at the maximum', async () => {
    const r = await render(<Harness over={{ reminders: { max: 2 }, initial: { reminders: [5, 10] } }} />)
    expect(document.querySelector('[data-event-form-add-reminder]')).toBeNull()
    await r.unmount()
  })

  it('refuses a second reminder for Microsoft and says why', async () => {
    const r = await render(<Harness over={{ reminders: { max: 1, microsoft: true }, initial: { reminders: [10] } }} />)
    expect(document.querySelector('[data-event-form-add-reminder]')).toBeNull()
    expect(q('[data-event-form-reminder-limit]').textContent).toBe('This calendar takes one reminder.')
    await r.unmount()
  })

  it('offers all-day presets as times of the day, and none for Microsoft', async () => {
    const r = await render(<Harness over={{ initial: { allDay: true, startTime: '', endTime: '' } }} />)
    await click(q('[data-event-form-add-reminder]'))
    await waitFor(() => document.querySelector('[role="menuitem"]') !== null)
    const labels = [...document.querySelectorAll('[role="menuitem"]')].map((i) => i.textContent?.trim() ?? '')
    expect(labels).toHaveLength(4)
    expect(labels[0]).toMatch(/^On the day at (9:00|09:00)/)
    expect(labels[1]).toMatch(/^1 day before at (9:00|09:00)/)
    await r.unmount()
    const ms = await render(<Harness over={{ reminders: { max: 1, microsoft: true }, initial: { allDay: true, startTime: '', endTime: '' } }} />)
    expect(document.querySelector('[data-event-form-add-reminder]')).toBeNull()
    await ms.unmount()
  })

  it('says how many other reminders are kept and that the calendar app sends them', async () => {
    const r = await render(<Harness over={{ reminders: { other: 1 } }} />)
    expect(q('[data-event-form-reminders-other]').textContent).toBe('1 other reminder is kept')
    expect(q('[data-event-form-reminders]').textContent).toContain('The calendar app sends reminders; OpenTodo does not.')
    await r.unmount()
    const two = await render(<Harness over={{ reminders: { other: 2 } }} />)
    expect(q('[data-event-form-reminders-other]').textContent).toBe('2 other reminders are kept')
    await two.unmount()
  })

  it('is a skeleton while loading and disabled with the reason offline', async () => {
    const loading = await render(<Harness over={{ reminders: { state: 'loading' } }} />)
    expect(q('[data-event-form-reminders] [role="status"]')).not.toBeNull()
    expect(document.querySelector('[data-event-form-add-reminder]')).toBeNull()
    await loading.unmount()
    const offline = await render(<Harness over={{ reminders: { state: 'unavailable', reason: 'offline' } }} />)
    expect(q('[data-event-form-reminders]').textContent).toContain('offline')
    expect(document.querySelector('[data-event-form-add-reminder]')).toBeNull()
    await offline.unmount()
  })
})
