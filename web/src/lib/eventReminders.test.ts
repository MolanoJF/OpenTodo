import { describe, expect, it } from 'vitest'
import { addReminder, reminderLabel, reminderPresets, removeReminder } from './eventReminders'

describe('reminderLabel', () => {
  it.each<[number, string]>([
    [0, 'At the start'],
    [5, '5 minutes before'],
    [60, '1 hour before'],
    [120, '2 hours before'],
    [90, '90 minutes before'],
    [1440, '1 day before'],
    [2880, '2 days before'],
    [1500, '25 hours before'],
    [1001, '1,001 minutes before'],
  ])('a timed event: %i -> %s', (minutes, want) => {
    expect(reminderLabel(minutes, false)).toBe(want)
  })

  it('an all-day event reads as a time of day: negative minutes are after midnight of the day', () => {
    expect(reminderLabel(-540, true)).toMatch(/^On the day at (9:00|09:00)/)
    expect(reminderLabel(900, true)).toMatch(/^1 day before at (9:00|09:00)/)
    expect(reminderLabel(2340, true)).toMatch(/^2 days before at (9:00|09:00)/)
  })
})

describe('reminderPresets', () => {
  it('offers the timed presets in order', () => {
    expect(reminderPresets(false, false)).toEqual([0, 5, 10, 15, 30, 60, 1440, 2880])
  })

  it('offers all-day presets at 09:00, and none for Microsoft, which cannot take a negative value', () => {
    expect(reminderPresets(true, false)).toEqual([-540, 900, 2340, 9540])
    expect(reminderPresets(true, true)).toEqual([])
    expect(reminderPresets(false, true)).toEqual([0, 5, 10, 15, 30, 60, 1440, 2880])
  })
})

describe('adding and removing', () => {
  it('adds in order of time and never twice', () => {
    expect(addReminder([1440], 10)).toEqual([10, 1440])
    expect(addReminder([10, 1440], 10)).toEqual([10, 1440])
  })

  it('removes one', () => {
    expect(removeReminder([10, 1440], 10)).toEqual([1440])
    expect(removeReminder([10], 5)).toEqual([10])
  })
})
