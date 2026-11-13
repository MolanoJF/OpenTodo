// @vitest-environment happy-dom
// The popover and editor line for a write that is not just on its way (OpenSpec change calendar-event-edits D6).
import 'fake-indexeddb/auto'
import { beforeEach, describe, expect, it } from 'vitest'
import { db, useDatabase } from '../../lib/db'
import type { CalendarEvent, EventCommand } from '../../lib/types'
import { click, render, waitFor } from '../../ui/test-utils'
import { EventWriteState } from './EventWriteState'

const ev: CalendarEvent = { id: 'e1', subscriptionId: 's1', uid: 'u', summary: 'Standup', location: '', allDay: false, free: false, rev: 'r1', start: { date: '2026-11-13', time: '10:00' }, end: { date: '2026-11-13', time: '11:00' }, access: { update: true, delete: true } }
const cmd = (over: Partial<EventCommand>): EventCommand => ({ key: 'c1:e1', connectionId: 'c1', eventId: 'e1', rev: 'r1', base: ev, patch: { summary: 'Planning' }, notBefore: 0, tries: 0, ...over })
const line = (c: HTMLElement) => c.querySelector<HTMLElement>('[data-event-write]')

let n = 0
beforeEach(async () => {
  useDatabase(`write-state-${Date.now()}-${n++}`)
  await db.calendarEvents.put({ ...ev, summary: 'Planning' })
})

describe('EventWriteState', () => {
  it('says a write waits for the device and offers Discard, which puts the event back', async () => {
    await db.eventOutbox.put(cmd({ offline: true }))
    const r = await render(<EventWriteState id="e1" />)
    await waitFor(() => !!line(r.container))
    expect(line(r.container)!.textContent).toContain('Waiting to send')
    await click(r.container.querySelector('[data-event-write-discard]')!)
    await waitFor(async () => (await db.eventOutbox.count()) === 0)
    await waitFor(async () => (await db.calendarEvents.get('e1'))?.summary === 'Standup')
    await r.unmount()
  })

  it('says why a write failed and offers Retry and Discard', async () => {
    await db.eventOutbox.put(cmd({ failed: { code: 'calendar_unreachable', status: 502, message: 'The calendar server could not be used: timeout' } }))
    const r = await render(<EventWriteState id="e1" />)
    await waitFor(() => !!line(r.container))
    expect(line(r.container)!.getAttribute('data-event-write')).toBe('failed')
    expect(line(r.container)!.textContent).toContain('Not saved')
    expect(line(r.container)!.textContent).toContain('The calendar server could not be used: timeout')
    expect(r.container.querySelector('[data-event-write-retry]')).not.toBeNull()
    await r.unmount()
  })
})
