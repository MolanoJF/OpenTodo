// The shell only looks for waiting calendar writes; the outbox loads when one waits (OpenSpec change calendar-event-edits D6).
import 'fake-indexeddb/auto'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const startEventOutbox = vi.fn()
vi.mock('./calendarEventEdits', () => ({ startEventOutbox: () => startEventOutbox() }))

import { db, useDatabase } from './db'
import { resumeEventWrites } from './eventOutboxBoot'

let n = 0
beforeEach(() => {
  useDatabase(`outbox-boot-${Date.now()}-${n++}`)
  startEventOutbox.mockReset()
})

describe('resumeEventWrites', () => {
  it('loads nothing when no write waits', async () => {
    await resumeEventWrites()
    expect(startEventOutbox).not.toHaveBeenCalled()
  })

  it('starts the outbox when a write waits (a failed one too: the person decides)', async () => {
    const base = { id: 'e1', subscriptionId: 's1', uid: 'u', summary: 'Standup', location: '', allDay: false, free: false, start: { date: '2026-11-13' }, end: { date: '2026-11-13' } }
    await db.eventOutbox.put({ key: 'c1:e1', connectionId: 'c1', eventId: 'e1', rev: 'r1', base, patch: { summary: 'x' }, notBefore: 0, tries: 0, offline: true })
    await resumeEventWrites()
    expect(startEventOutbox).toHaveBeenCalledTimes(1)
  })
})
