import { describe, expect, it } from 'vitest'
import { defaultRepeat, endsLabel, isMicrosoftCalendar, presetOf, repeatForPreset, repeatSummary, REPEAT_PRESETS, restRepeat, sameRepeat, withFreq } from './eventRepeat'
import type { CalendarRepeat } from './types'

// 2026-11-12 is a Thursday, the second Thursday of November; 2026-11-29 a Sunday, the fifth Sunday.
const THU = '2026-11-12'
const never = { type: 'never' } as const

describe('repeatForPreset: each preset is the REST object of design D3', () => {
  it.each<[Parameters<typeof repeatForPreset>[0], CalendarRepeat | null]>([
    ['none', null],
    ['daily', { freq: 'daily', interval: 1, days: [], ends: never }],
    ['weekdays', { freq: 'weekly', interval: 1, days: ['mo', 'tu', 'we', 'th', 'fr'], ends: never }],
    ['weekly', { freq: 'weekly', interval: 1, days: [], ends: never }],
    ['monthday', { freq: 'monthly', interval: 1, days: [], by: 'monthday', ends: never }],
    ['weekday', { freq: 'monthly', interval: 1, days: [], by: 'weekday', ends: never }],
    ['yearly', { freq: 'yearly', interval: 1, days: [], ends: never }],
  ])('%s', (preset, want) => {
    expect(repeatForPreset(preset)).toEqual(want)
  })

  it('keeps the ends the person chose when the preset changes', () => {
    expect(repeatForPreset('daily', { type: 'after', count: 10 })).toEqual({ freq: 'daily', interval: 1, days: [], ends: { type: 'after', count: 10 } })
  })
})

describe('presetOf: the preset a rule reads as', () => {
  it.each<[CalendarRepeat | null, string]>([
    [null, 'none'],
    [{ freq: 'daily', interval: 1, ends: never }, 'daily'],
    [{ freq: 'daily', interval: 2, ends: never }, 'custom'],
    [{ freq: 'weekly', interval: 1, days: ['mo', 'tu', 'we', 'th', 'fr'], ends: never }, 'weekdays'],
    [{ freq: 'weekly', interval: 1, days: ['fr', 'th', 'we', 'tu', 'mo'], ends: never }, 'weekdays'],
    [{ freq: 'weekly', interval: 1, days: [], ends: never }, 'weekly'],
    [{ freq: 'weekly', interval: 1, days: ['th'], ends: never }, 'weekly'],
    [{ freq: 'weekly', interval: 1, days: ['mo', 'we'], ends: never }, 'custom'],
    [{ freq: 'weekly', interval: 2, days: [], ends: never }, 'custom'],
    [{ freq: 'monthly', interval: 1, ends: never }, 'monthday'],
    [{ freq: 'monthly', interval: 1, by: 'monthday', ends: never }, 'monthday'],
    [{ freq: 'monthly', interval: 1, by: 'weekday', ends: never }, 'weekday'],
    [{ freq: 'yearly', interval: 1, ends: never }, 'yearly'],
    [{ freq: 'yearly', interval: 3, ends: never }, 'custom'],
    [{ freq: 'daily', interval: 1, ends: { type: 'after', count: 5 } }, 'daily'],
  ])('%j -> %s', (repeat, want) => {
    expect(presetOf(repeat, THU)).toBe(want)
  })

  it('"Every weekday" is recognised on read whatever the start day', () => {
    expect(presetOf({ freq: 'weekly', interval: 1, days: ['mo', 'tu', 'we', 'th', 'fr'], ends: never }, '2026-11-14')).toBe('weekdays')
  })

  it('an explicit weekday other than the start reads as custom', () => {
    expect(presetOf({ freq: 'weekly', interval: 1, days: ['tu'], ends: never }, THU)).toBe('custom')
  })

  it('lists the presets in the order the field shows them, Custom last', () => {
    expect(REPEAT_PRESETS).toEqual(['none', 'daily', 'weekdays', 'weekly', 'monthday', 'weekday', 'yearly', 'custom'])
  })
})

describe('repeatSummary: the words', () => {
  it.each<[CalendarRepeat, string, string]>([
    [{ freq: 'daily', interval: 1, ends: never }, THU, 'Every day'],
    [{ freq: 'daily', interval: 3, ends: never }, THU, 'Every 3 days'],
    [{ freq: 'weekly', interval: 1, days: ['mo', 'tu', 'we', 'th', 'fr'], ends: never }, THU, 'Every weekday'],
    [{ freq: 'weekly', interval: 1, days: [], ends: never }, THU, 'Every week on Thursday'],
    [{ freq: 'weekly', interval: 2, days: ['mo', 'we'], ends: never }, THU, 'Every 2 weeks on Monday, Wednesday'],
    [{ freq: 'monthly', interval: 1, ends: never }, THU, 'Every month on day 12'],
    [{ freq: 'monthly', interval: 1, by: 'weekday', ends: never }, THU, 'Every month on the second Thursday'],
    [{ freq: 'monthly', interval: 1, by: 'weekday', ends: never }, '2026-11-29', 'Every month on the last Sunday'],
    [{ freq: 'monthly', interval: 2, ends: never }, THU, 'Every 2 months'],
    [{ freq: 'yearly', interval: 1, ends: never }, THU, 'Every year on November 12'],
    [{ freq: 'yearly', interval: 2, ends: never }, THU, 'Every 2 years'],
    [{ freq: 'daily', interval: 1, ends: { type: 'after', count: 10 } }, THU, 'Every day, 10 times'],
    [{ freq: 'daily', interval: 1, ends: { type: 'after', count: 1 } }, THU, 'Every day, once'],
    [{ freq: 'daily', interval: 1, ends: { type: 'on', date: '2026-12-31' } }, THU, 'Every day, until December 31'],
  ])('%j', (repeat, date, want) => {
    expect(repeatSummary(repeat, date)).toBe(want)
  })

  it('endsLabel is empty for never', () => {
    expect(endsLabel(never)).toBe('')
  })
})

describe('the custom editor helpers', () => {
  it('defaultRepeat repeats weekly on the start weekday and never ends', () => {
    expect(defaultRepeat(THU)).toEqual({ freq: 'weekly', interval: 1, days: ['th'], ends: never })
  })

  it('withFreq drops what the new unit does not use and gives monthly its day-of-month default', () => {
    const weekly: CalendarRepeat = { freq: 'weekly', interval: 2, days: ['mo', 'we'], ends: { type: 'after', count: 4 } }
    expect(withFreq(weekly, 'monthly', THU)).toEqual({ freq: 'monthly', interval: 2, days: [], by: 'monthday', ends: { type: 'after', count: 4 } })
    expect(withFreq(weekly, 'daily', THU)).toEqual({ freq: 'daily', interval: 2, days: [], ends: { type: 'after', count: 4 } })
    expect(withFreq({ freq: 'daily', interval: 1, ends: never }, 'weekly', THU)).toEqual({ freq: 'weekly', interval: 1, days: ['th'], ends: never })
  })

  it('restRepeat is what the request carries: days always there, by only for monthly, days in week order', () => {
    expect(restRepeat({ freq: 'weekly', interval: 1, days: ['we', 'mo'], ends: never })).toEqual({ freq: 'weekly', interval: 1, days: ['mo', 'we'], ends: never })
    expect(restRepeat({ freq: 'daily', interval: 1, by: 'weekday', ends: never })).toEqual({ freq: 'daily', interval: 1, days: [], ends: never })
    expect(restRepeat({ freq: 'monthly', interval: 1, ends: never })).toEqual({ freq: 'monthly', interval: 1, days: [], by: 'monthday', ends: never })
  })

  it('sameRepeat ignores the order of the days and an absent days list', () => {
    expect(sameRepeat({ freq: 'weekly', interval: 1, days: ['we', 'mo'], ends: never }, { freq: 'weekly', interval: 1, days: ['mo', 'we'], ends: never })).toBe(true)
    expect(sameRepeat({ freq: 'daily', interval: 1, ends: never }, { freq: 'daily', interval: 1, days: [], ends: never })).toBe(true)
    expect(sameRepeat({ freq: 'daily', interval: 1, ends: never }, { freq: 'daily', interval: 2, ends: never })).toBe(false)
    expect(sameRepeat(null, null)).toBe(true)
    expect(sameRepeat(null, { freq: 'daily', interval: 1, ends: never })).toBe(false)
  })
})

describe('isMicrosoftCalendar', () => {
  it('reads the one-reminder limit as a Microsoft calendar', () => {
    expect(isMicrosoftCalendar({ maxReminders: 1 })).toBe(true)
    expect(isMicrosoftCalendar({ maxReminders: 5 })).toBe(false)
    expect(isMicrosoftCalendar(undefined)).toBe(false)
  })
})
