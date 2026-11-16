import Dexie from 'dexie'
import { describe, expect, it } from 'vitest'
import { clearLocalData, useDatabase } from './db'
import type { CalendarEvent, EventCommand } from './types'

const ev: CalendarEvent = {
  id: 'e1', subscriptionId: 's1', uid: 'u1', summary: 'Standup', location: '',
  start: { date: '2026-11-12', time: '09:00' }, end: { date: '2026-11-12', time: '09:30' }, allDay: false, free: false,
}

const command: EventCommand = {
  key: 'c1:e1', connectionId: 'c1', eventId: 'e1', rev: 'r1', base: ev, patch: { summary: 'Planning' }, notBefore: 0, tries: 0,
}

describe('eventOutbox (Dexie v31)', () => {
  it('upgrades a v30 database, keeps its events and adds the table', async () => {
    const name = `v30-${Math.random()}`
    const old = new Dexie(name)
    old.version(30).stores({ calendarEvents: 'id, subscriptionId, start.date' })
    await old.table('calendarEvents').put(ev)
    old.close()

    const db = useDatabase(name)
    await db.open()
    expect(await db.calendarEvents.get('e1')).toEqual(ev)
    expect(await db.eventOutbox.count()).toBe(0)
    await db.eventOutbox.put(command)
    expect((await db.eventOutbox.get('c1:e1'))?.rev).toBe('r1')
    db.close()
  })

  it('is emptied at sign-out', async () => {
    const db = useDatabase(`signout-${Math.random()}`)
    await db.eventOutbox.put(command)
    await db.calendarEvents.put(ev)
    await clearLocalData()
    expect(await db.eventOutbox.count()).toBe(0)
    expect(await db.calendarEvents.count()).toBe(0)
    db.close()
  })
})
