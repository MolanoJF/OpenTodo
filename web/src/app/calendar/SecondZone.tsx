// The second hour column of the Day and Week grids (OpenSpec `calendar-time-zones`): its labels, the zone
// codes above both columns and the spacer cells that keep the header rows aligned. Decorative like the
// first column; the zone is stated once in text for assistive technology (design D5).
import { useMemo } from 'react'
import { formatHour, zoneCity } from '../../lib/dates'
import { t } from '../../lib/i18n'
import { hourClocks, secondZoneOf, zoneCode } from '../../lib/zoneGutter'
import { useDateTimeFormat } from '../DateTimePrefsProvider'
import { zoneOffset } from '../task-detail/ZoneControl'
import { GRID_HEIGHT, PX_PER_MIN } from './grid'

/** A fixed, non-shrinking gutter: two of them leave 264 px of a 360 px screen for the day (design D5). */
const GUTTER = 'w-12 shrink-0'

export interface SecondZone {
  zone: string
  /** The device zone, for the code above the first column. */
  viewer: string
  /** The time in `zone` at each of the 24 device-zone hour rows of the first date shown. */
  clocks: string[]
  /** "Bogotá (UTC−5)" at that date, for the sentence read once. */
  name: string
}

/** The second zone to draw for the first date on screen, or null when it is off or the device's own. */
export function useSecondZone(stored: string | undefined, date: string, viewer: string | undefined): SecondZone | null {
  const { resolved } = useDateTimeFormat()
  const zone = viewer ? secondZoneOf(stored, viewer) : null
  return useMemo(() => {
    if (!zone || !viewer) return null
    const offset = zoneOffset(zone, Date.parse(`${date}T12:00:00Z`))
    return { zone, viewer, clocks: hourClocks(date, viewer, zone, resolved), name: offset ? `${zoneCity(zone)} (${offset})` : zoneCity(zone) }
  }, [zone, viewer, date, resolved])
}

/** The hour labels of the first column, and beside it the second when a second zone is on. */
export function HourGutters({ second }: { second: SecondZone | null }) {
  return (
    <>
      <div className={`relative ${GUTTER}`} style={{ height: GRID_HEIGHT }} aria-hidden>
        {Array.from({ length: 24 }, (_, h) => (
          <div key={h} className="absolute right-1 -translate-y-1/2 text-hint tabular-nums text-faint" data-hour-label style={{ top: h * 60 * PX_PER_MIN }}>
            {h === 0 ? '' : formatHour(h)}
          </div>
        ))}
      </div>
      {second && (
        <div className={`relative ${GUTTER}`} style={{ height: GRID_HEIGHT }} aria-hidden data-second-zone-hours>
          {second.clocks.map((label, h) => (
            <div key={h} className="absolute right-1 -translate-y-1/2 text-hint tabular-nums text-faint" data-second-label style={{ top: h * 60 * PX_PER_MIN }}>
              {h === 0 ? '' : label}
            </div>
          ))}
        </div>
      )}
    </>
  )
}

/**
 * The gutter cells of a sticky-header row. `label` is the row with the day names: it says "all day" and,
 * with a second zone, the zone codes (two lines inside the header's existing height) and the one sentence.
 */
export function GutterCells({ label, second }: { label: boolean; second: SecondZone | null }) {
  return (
    <>
      <div className={`${GUTTER} pt-1 text-right text-hint text-faint`} aria-hidden>
        {label && second && <div className="pr-1" data-zone-code>{zoneCode(second.viewer)}</div>}
        {label && <span className="pr-1">{t('all day')}</span>}
      </div>
      {second && (
        <div className={`${GUTTER} pt-1 text-right text-hint text-faint`} data-second-zone-head={label ? '' : undefined}>
          {label && <span className="pr-1" aria-hidden data-zone-code>{zoneCode(second.zone)}</span>}
          {label && <span className="sr-only">{t('Times also shown in {zone}', { zone: second.name })}</span>}
        </div>
      )}
    </>
  )
}
