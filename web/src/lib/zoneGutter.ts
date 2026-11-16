// The second hour column of the Day and Week grids (OpenSpec `calendar-time-zones` D3): clocks read
// through Intl at the instant each device-zone row names, never offsets added to hours.
import { formatTime, instantToLocalDue, isKnownZone, zonedInstant, zoneCity, type ResolvedDateTime } from './dates'

/** The second zone to draw: the stored one when it is a known zone other than the device's, else null. */
export function secondZoneOf(stored: string | undefined, viewer: string): string | null {
  return stored && stored !== viewer && isKnownZone(stored) ? stored : null
}

/** `BOG`, `BER`, `UTC`: the first three letters of the city, capitals, without accents. */
export function zoneCode(zone: string): string {
  return zoneCity(zone).normalize('NFD').replace(/[^A-Za-z]/g, '').slice(0, 3).toUpperCase()
}

/** For each hour 0 to 23 of `date` in the `viewer` zone, the time in `zone` at that instant, in the user's format. */
export function hourClocks(date: string, viewer: string, zone: string, p?: ResolvedDateTime): string[] {
  return Array.from({ length: 24 }, (_, h) => formatTime(instantToLocalDue(zonedInstant(date, `${String(h).padStart(2, '0')}:00`, viewer), zone).time, p))
}
