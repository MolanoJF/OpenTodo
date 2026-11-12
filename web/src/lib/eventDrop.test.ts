import { describe, expect, it } from 'vitest'
import { eventDropChange, eventFormChange, eventFormOf, eventNudgeChange } from './eventDrop'
import { eventFullChange, eventFullOf, pointsOf, startInViewerZone, switchAllDay } from './eventFull'
import type { CalendarEvent } from './types'

const BERLIN = 'Europe/Berlin'
const LISBON = 'Europe/Lisbon'
const ev = (over: Partial<CalendarEvent> = {}): CalendarEvent => ({
  id: 'e1', subscriptionId: 's1', uid: 'u1', summary: 'Standup', location: '', allDay: false, free: false, rev: 'r1',
  start: { date: '2026-11-12', time: '09:00', tz: BERLIN }, end: { date: '2026-11-12', time: '09:30', tz: BERLIN }, ...over,
})
const berlin = (date: string, time: string) => ({ date, time, tz: BERLIN })

describe('eventDropChange', () => {
  it('moves a block and keeps its length, in the event zone (a viewer in Lisbon drops 14:00)', () => {
    // 09:00 Berlin is 08:00 Lisbon; dropped at 14:00 Lisbon it is 15:00 Berlin.
    expect(eventDropChange(ev(), { type: 'slot', date: '2026-11-12', start: 14 * 60 }, LISBON)).toEqual({
      start: berlin('2026-11-12', '15:00'), end: berlin('2026-11-12', '15:30'), allDay: false,
    })
  })

  it('moves to another day column', () => {
    expect(eventDropChange(ev(), { type: 'slot', date: '2026-11-14', start: 9 * 60 }, BERLIN)).toMatchObject({ start: berlin('2026-11-14', '09:00'), end: berlin('2026-11-14', '09:30') })
  })

  it('resizes from the lower edge (start unchanged) and the upper edge (end unchanged)', () => {
    expect(eventDropChange(ev(), { type: 'resize', estimate: 60 }, BERLIN)).toEqual({ start: berlin('2026-11-12', '09:00'), end: berlin('2026-11-12', '10:00'), allDay: false })
    expect(eventDropChange(ev(), { type: 'resize-start', date: '2026-11-12', start: 8 * 60 + 30, estimate: 60 }, BERLIN)).toEqual({ start: berlin('2026-11-12', '08:30'), end: berlin('2026-11-12', '09:30'), allDay: false })
  })

  it('becomes all-day when dropped on the lane; a drop that changes nothing is null', () => {
    expect(eventDropChange(ev(), { type: 'lane', date: '2026-11-13' })).toEqual({ start: { date: '2026-11-13' }, end: { date: '2026-11-14' }, allDay: true })
    expect(eventDropChange(ev(), { type: 'slot', date: '2026-11-12', start: 9 * 60 }, BERLIN)).toBeNull()
  })

  it('keeps a floating event floating, and an all-day event is not a block', () => {
    const floating = ev({ start: { date: '2026-11-12', time: '09:00' }, end: { date: '2026-11-12', time: '09:30' } })
    expect(eventDropChange(floating, { type: 'slot', date: '2026-11-12', start: 600 }, 'Asia/Tokyo')).toEqual({ start: { date: '2026-11-12', time: '10:00' }, end: { date: '2026-11-12', time: '10:30' }, allDay: false })
    expect(eventDropChange(ev({ allDay: true, start: { date: '2026-11-12' }, end: { date: '2026-11-13' } }), { type: 'slot', date: '2026-11-12', start: 600 })).toBeNull()
  })

  it('carries an end past midnight to the next day', () => {
    expect(eventDropChange(ev(), { type: 'resize', estimate: 16 * 60 }, BERLIN)).toMatchObject({ end: berlin('2026-11-13', '01:00') })
  })
})

describe('eventNudgeChange', () => {
  it('moves by 15 minutes and by a day', () => {
    expect(eventNudgeChange(ev(), { minutes: 15 }, BERLIN)).toMatchObject({ start: berlin('2026-11-12', '09:15'), end: berlin('2026-11-12', '09:45') })
    expect(eventNudgeChange(ev(), { days: -1 }, BERLIN)).toMatchObject({ start: berlin('2026-11-11', '09:00'), end: berlin('2026-11-11', '09:30') })
  })
  it('moves one edge and never crosses the other', () => {
    expect(eventNudgeChange(ev(), { end: 15 }, BERLIN)).toMatchObject({ start: berlin('2026-11-12', '09:00'), end: berlin('2026-11-12', '09:45') })
    expect(eventNudgeChange(ev(), { start: -15 }, BERLIN)).toMatchObject({ start: berlin('2026-11-12', '08:45'), end: berlin('2026-11-12', '09:30') })
    expect(eventNudgeChange(ev({ end: berlin('2026-11-12', '09:15') }), { end: -15 }, BERLIN)).toBeNull()
    expect(eventNudgeChange(ev({ end: berlin('2026-11-12', '09:15') }), { start: 15 }, BERLIN)).toBeNull()
  })
})

describe('the popover form', () => {
  it('reads the event in the viewer zone and writes it back in the event zone', () => {
    expect(eventFormOf(ev(), LISBON)).toEqual({ title: 'Standup', date: '2026-11-12', start: '08:00', end: '08:30' })
    expect(eventFormChange(ev(), { title: 'Standup', date: '2026-11-12', start: '15:00', end: '15:30' }, LISBON)).toEqual({ start: berlin('2026-11-12', '16:00'), end: berlin('2026-11-12', '16:30'), allDay: false })
  })
  it('is null when nothing changed and names only the title when only it changed', () => {
    const form = eventFormOf(ev(), BERLIN)
    expect(eventFormChange(ev(), form, BERLIN)).toBeNull()
    expect(eventFormChange(ev(), { ...form, title: ' Planning ' }, BERLIN)).toEqual({ summary: 'Planning' })
  })
  it('refuses an end that is not after the start or an unreadable time', () => {
    const form = eventFormOf(ev(), BERLIN)
    expect(eventFormChange(ev(), { ...form, end: '09:00' }, BERLIN)).toBe('invalid')
    expect(eventFormChange(ev(), { ...form, start: 'abc' }, BERLIN)).toBe('invalid')
  })
  it('moves an all-day event by date and can give it times', () => {
    const allDay = ev({ allDay: true, start: { date: '2026-11-12' }, end: { date: '2026-11-14' } })
    expect(eventFormOf(allDay)).toEqual({ title: 'Standup', date: '2026-11-12', start: '', end: '' })
    expect(eventFormChange(allDay, { title: 'Standup', date: '2026-11-20', start: '', end: '' })).toEqual({ start: { date: '2026-11-20' }, end: { date: '2026-11-22' }, allDay: true })
    expect(eventFormChange(allDay, { title: 'Standup', date: '2026-11-12', start: '10:00', end: '11:00' }, BERLIN)).toMatchObject({ allDay: false, start: { date: '2026-11-12', time: '10:00' }, end: { date: '2026-11-12', time: '11:00' } })
  })
})

describe('the full editor fields (calendar-event-editor D11)', () => {
  const plain = ev({ location: 'Room 1' })
  const full = (e: CalendarEvent, zone = BERLIN, description = 'Agenda') => eventFullOf(e, zone, description)

  it('shows an event in its own zone, with the time in the viewer zone beneath', () => {
    const f = full(plain, LISBON)
    expect(f).toMatchObject({ zone: BERLIN, startDate: '2026-11-12', startTime: '09:00', endTime: '09:30', allDay: false, location: 'Room 1', description: 'Agenda' })
    expect(startInViewerZone(f, LISBON)).toEqual({ date: '2026-11-12', time: '08:00' })
    expect(startInViewerZone(full(plain, BERLIN), BERLIN)).toBeNull()
  })

  it('edits a UTC event in the viewer zone without moving it, and sends nothing for untouched times', () => {
    const utc = ev({ start: { date: '2026-11-12', time: '08:00', tz: 'UTC' }, end: { date: '2026-11-12', time: '08:30', tz: 'UTC' } })
    const f = full(utc, BERLIN)
    expect(f).toMatchObject({ zone: BERLIN, startTime: '09:00', endTime: '09:30' })
    expect(eventFullChange(utc, f, BERLIN, 'Agenda')).toBeNull()
    expect(eventFullChange(utc, { ...f, title: 'Planning' }, BERLIN, 'Agenda')).toEqual({ summary: 'Planning' })
    expect(eventFullChange(utc, { ...f, startTime: '10:00', endTime: '10:30' }, BERLIN, 'Agenda')).toEqual({ start: berlin('2026-11-12', '10:00'), end: berlin('2026-11-12', '10:30'), allDay: false })
  })

  it('keeps a floating event floating until a zone is picked', () => {
    const floating = ev({ start: { date: '2026-11-12', time: '09:00' }, end: { date: '2026-11-12', time: '09:30' } })
    const f = full(floating, BERLIN, '')
    expect(f.zone).toBe('')
    expect(startInViewerZone(f, BERLIN)).toBeNull()
    expect(eventFullChange(floating, { ...f, endTime: '10:00' }, BERLIN, '')).toEqual({ start: { date: '2026-11-12', time: '09:00' }, end: { date: '2026-11-12', time: '10:00' }, allDay: false })
    expect(eventFullChange(floating, { ...f, zone: LISBON }, BERLIN, '')).toEqual({ start: { date: '2026-11-12', time: '09:00', tz: LISBON }, end: { date: '2026-11-12', time: '09:30', tz: LISBON }, allDay: false })
  })

  it('a zone change keeps the wall clock and moves the instant', () => {
    const f = { ...full(plain, LISBON), zone: LISBON }
    expect(eventFullChange(plain, f, LISBON, 'Agenda')).toEqual({ start: { date: '2026-11-12', time: '09:00', tz: LISBON }, end: { date: '2026-11-12', time: '09:30', tz: LISBON }, allDay: false })
    expect(startInViewerZone({ ...f, zone: BERLIN }, LISBON)?.time).toBe('08:00')
  })

  it('all-day on and off, with the end date inclusive', () => {
    const on = switchAllDay(full(plain), true)
    expect(on).toMatchObject({ allDay: true, startTime: '', endTime: '', startDate: '2026-11-12', endDate: '2026-11-12' })
    expect(eventFullChange(plain, on, BERLIN, 'Agenda')).toEqual({ start: { date: '2026-11-12' }, end: { date: '2026-11-13' }, allDay: true })
    const multi = ev({ allDay: true, start: { date: '2026-11-16' }, end: { date: '2026-11-19' } })
    expect(full(multi)).toMatchObject({ allDay: true, startDate: '2026-11-16', endDate: '2026-11-18' })
    expect(eventFullChange(multi, full(multi), BERLIN, 'Agenda')).toBeNull()
    expect(eventFullChange(multi, { ...full(multi), endDate: '2026-11-20' }, BERLIN, 'Agenda')).toEqual({ start: { date: '2026-11-16' }, end: { date: '2026-11-21' }, allDay: true })
    const off = switchAllDay(full(multi), false)
    expect(off).toMatchObject({ allDay: false, startTime: '09:00', endTime: '10:00', endDate: '2026-11-16' })
    expect(eventFullChange(multi, off, BERLIN, 'Agenda')).toEqual({ start: berlin('2026-11-16', '09:00'), end: berlin('2026-11-16', '10:00'), allDay: false })
  })

  it('sends only what changed, and a description only when it could be loaded', () => {
    expect(eventFullChange(plain, full(plain), BERLIN, 'Agenda')).toBeNull()
    expect(eventFullChange(plain, { ...full(plain), location: '', free: true, description: 'New' }, BERLIN, 'Agenda')).toEqual({ location: '', free: true, description: 'New' })
    expect(eventFullChange(plain, { ...full(plain, BERLIN, ''), description: 'typed', title: 'T' }, BERLIN, null)).toEqual({ summary: 'T' })
  })

  it('refuses times that make no event', () => {
    expect(eventFullChange(plain, { ...full(plain), endTime: '09:00' }, BERLIN, 'Agenda')).toBe('invalid')
    expect(pointsOf({ ...full(plain), startTime: '' })).toBe('invalid')
    expect(pointsOf({ ...switchAllDay(full(plain), true), endDate: '2026-11-10' })).toBe('invalid')
  })
})
