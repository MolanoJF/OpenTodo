import { expect, it } from 'vitest'
import { blockGeometry, isWeekend } from './calendarPresentation'

it('leaves a four-pixel gap between adjacent timed cards without changing inputs', () => {
  const a = { id: 'a', start: 420, end: 510, lane: 0, lanes: 1 }
  const b = { ...a, id: 'b', start: 510, end: 570 }
  const first = blockGeometry(a, 0.8, 20), second = blockGeometry(b, 0.8, 20)
  expect(second.top - (first.top + first.height)).toBeCloseTo(4)
  expect(a.end).toBe(510)
})
it('stacks wide overlapping cards and layers later lanes above earlier ones', () => {
  const a = blockGeometry({ id: 'a', start: 600, end: 720, lane: 0, lanes: 3 }, 0.8, 20)
  const b = blockGeometry({ id: 'b', start: 630, end: 750, lane: 1, lanes: 3 }, 0.8, 20)
  expect(a.width).toBe('calc(75% - 6px)')
  expect(b.left).toBe('calc(25% + 3px)')
  expect(b.zIndex).toBeGreaterThan(a.zIndex)
})
it('recognizes weekends independently of the displayed week start', () => {
  expect(isWeekend('2026-11-14')).toBe(true)
  expect(isWeekend('2026-11-15')).toBe(true)
  expect(isWeekend('2026-11-16')).toBe(false)
})
