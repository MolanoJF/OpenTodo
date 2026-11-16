import 'fake-indexeddb/auto'
import { beforeEach, expect, it } from 'vitest'
import { db } from './db'
import { accountColor, coloredSubscriptions, defaultAccountColor, saveAccountColor } from './calendarColors'
import type { CalendarSubscription } from './types'

beforeEach(async () => { await db.delete(); await db.open() })
const sub = (id: string, connectionId?: string): CalendarSubscription => ({ id, connectionId, name: id, color: '#06b6d4', enabled: true, lastSuccessAt: null })

it('keeps defaults stable and gives simple distinct identities different hues', () => {
  expect(defaultAccountColor('personal')).toBe(defaultAccountColor('personal'))
  expect(defaultAccountColor('account-a')).not.toBe(defaultAccountColor('account-b'))
  expect(accountColor('personal', 'invalid')).toBe(defaultAccountColor('personal'))
})

it('shares account colors, preserves choices after refresh and never rewrites provider data', async () => {
  const sources = [sub('work', 'account-a'), sub('holidays', 'account-a'), sub('personal', 'account-b'), sub('ics')]
  await saveAccountColor('account-a', '#eab308')
  await saveAccountColor('account-b', '#8b5cf6')
  expect((await coloredSubscriptions(sources)).map((s) => s.color)).toEqual(['#eab308', '#eab308', '#8b5cf6', '#06b6d4'])
  expect(sources.every((s) => s.color === '#06b6d4')).toBe(true)
  expect((await coloredSubscriptions(sources.map((s) => ({ ...s, color: '#ef4444' }))))[0].color).toBe('#eab308')
  await saveAccountColor('account-a', null)
  expect((await coloredSubscriptions(sources))[0].color).toBe(defaultAccountColor('account-a'))
  expect((await coloredSubscriptions(sources))[2].color).toBe('#8b5cf6')
})
