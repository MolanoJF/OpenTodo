// The toasts of calendar event writes that did not go through (OpenSpec change calendar-event-edits D6).
import 'fake-indexeddb/auto'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { db, useDatabase } from '../../lib/db'
import { notify } from '../../lib/eventEditNotices'
import { resetModuleState } from '../../lib/moduleState'
import type { CalendarEvent } from '../../lib/types'
import { actionToast, bulkToast } from '../bulk/toastStore'
import { hostEventEditToasts } from './eventEditToasts'

const ev: CalendarEvent = { id: 'e1', subscriptionId: 's1', uid: 'u', summary: 'Standup', location: '', allDay: false, free: false, rev: 'r1', start: { date: '2026-11-13', time: '10:00' }, end: { date: '2026-11-13', time: '11:00' }, access: { update: true, delete: true } }
let n = 0
beforeEach(() => {
  useDatabase(`event-toasts-${Date.now()}-${n++}`)
  resetModuleState()
})

describe('event write toasts', () => {
  it('a failure offers Retry and Discard; Discard puts the event back and closes the toast', async () => {
    hostEventEditToasts()
    await db.calendarEvents.put({ ...ev, summary: 'Planning' })
    await db.eventOutbox.put({ key: 'c1:e1', connectionId: 'c1', eventId: 'e1', rev: 'r1', base: ev, patch: { summary: 'Planning' }, notBefore: 0, tries: 1, failed: { code: 'calendar_unreachable', status: 502, message: 'down' } })
    notify({ code: 'calendar_unreachable', title: 'Standup', message: 'down', failure: { key: 'c1:e1' } })
    const toast = bulkToast()!
    expect(toast.label).toBe('Standup was not saved: down')
    expect([toast.action?.label, toast.second?.label]).toEqual(['Retry', 'Discard'])
    toast.second!.run()
    await vi.waitFor(async () => expect(await db.eventOutbox.count()).toBe(0))
    expect((await db.calendarEvents.get('e1'))!.summary).toBe('Standup')
    expect(bulkToast()).toBeNull()
  })

  it('a plain refusal is an action toast, and a notice nobody heard is shown once the toasts are hosted', () => {
    notify({ code: 'event_gone', title: 'Standup', message: '' }) // before anything listens
    expect(actionToast()).toBeNull()
    hostEventEditToasts()
    expect(actionToast()?.label).toBe('Standup was deleted in your calendar.')
  })
})
