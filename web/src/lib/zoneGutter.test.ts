import { describe, expect, it } from 'vitest'
import { resolveDateTime } from './dates'
import { hourClocks, secondZoneOf, zoneCode } from './zoneGutter'

const h24 = resolveDateTime({ date_format: 'iso', time_format: '24h', week_start: 'mon' } as never)
const h12 = resolveDateTime({ date_format: 'iso', time_format: '12h', week_start: 'mon' } as never)

describe('hourClocks (calendar-time-zones D3)', () => {
  it('reads Bogotá beside Berlin', () => {
    expect(hourClocks('2026-11-13', 'Europe/Berlin', 'America/Bogota', h24)[9]).toBe('03:00')
  })
  it('half-hour zone: Kolkata is UTC+5:30', () => {
    const rows = hourClocks('2026-11-13', 'UTC', 'Asia/Kolkata', h24)
    expect(rows[9]).toBe('14:30')
    expect(rows).toHaveLength(24)
    expect(rows[18]).toBe('23:30')
  })
  it('45-minute zone: Kathmandu is UTC+5:45', () => {
    expect(hourClocks('2026-11-13', 'UTC', 'Asia/Kathmandu', h24)[9]).toBe('14:45')
  })
  it('follows the 12-hour format', () => {
    expect(hourClocks('2026-11-13', 'UTC', 'Asia/Kolkata', h12)[9]).toBe('2:30 PM')
    expect(hourClocks('2026-11-13', 'UTC', 'Asia/Kolkata', h12)[0]).toBe('5:30 AM')
  })
  it('DST in the second zone: New York moves forward at 07:00 UTC on 2026-03-08', () => {
    const rows = hourClocks('2026-03-08', 'UTC', 'America/New_York', h24)
    expect(rows[6]).toBe('01:00')
    expect(rows[7]).toBe('03:00')
    expect(rows[8]).toBe('04:00')
  })
  it('DST in the device zone: Berlin has no 02:00 on 2026-03-29', () => {
    const rows = hourClocks('2026-03-29', 'Europe/Berlin', 'UTC', h24)
    expect(rows[1]).toBe('00:00')
    expect(rows[2]).toBe('01:00')
    expect(rows[3]).toBe('01:00')
  })
  it('DST in the device zone: the repeated hour of 2026-10-25 uses the earlier instant', () => {
    const rows = hourClocks('2026-10-25', 'Europe/Berlin', 'UTC', h24)
    expect(rows[2]).toBe('00:00')
    expect(rows[3]).toBe('02:00')
  })
})

describe('zoneCode', () => {
  it('is three capital letters of the city, without accents', () => {
    expect(zoneCode('America/Bogota')).toBe('BOG')
    expect(zoneCode('Europe/Berlin')).toBe('BER')
    expect(zoneCode('Asia/Kathmandu')).toBe('KAT')
    expect(zoneCode('America/Los_Angeles')).toBe('LOS')
    expect(zoneCode('UTC')).toBe('UTC')
    expect(zoneCode('Etc/UTC')).toBe('UTC')
  })
})

describe('secondZoneOf', () => {
  it('is null when off, unknown or the device zone', () => {
    expect(secondZoneOf(undefined, 'Europe/Berlin')).toBeNull()
    expect(secondZoneOf('', 'Europe/Berlin')).toBeNull()
    expect(secondZoneOf('Mars/Olympus', 'Europe/Berlin')).toBeNull()
    expect(secondZoneOf('Europe/Berlin', 'Europe/Berlin')).toBeNull()
    expect(secondZoneOf('America/Bogota', 'Europe/Berlin')).toBe('America/Bogota')
  })
})
