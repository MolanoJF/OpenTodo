// The editor's time zone (OpenSpec change calendar-event-editor D11): a Combobox over the platform's
// zones, the viewer's first. Changing the zone keeps the wall-clock numbers and moves the instant;
// the line beneath the times says what that is in the viewer's zone. A floating event offers "Floating".
import { useMemo } from 'react'
import { zoneCity } from '../../lib/dates'
import { t } from '../../lib/i18n'
import { Combobox } from '../../ui'
import { allZones, zoneOffset } from '../task-detail/ZoneControl'

interface ZoneItem { zone: string; city: string; secondary: string }

const FLOATING = ''

/** "Berlin · UTC+1", or "Floating". */
export function zoneText(zone: string): string {
  if (!zone) return t('Floating')
  const offset = zoneOffset(zone)
  return offset ? `${zoneCity(zone)} · ${offset}` : zoneCity(zone)
}

export function EventZoneField({ zone, viewerZone, floatingAllowed, onChange }: { zone: string; viewerZone: string; floatingAllowed: boolean; onChange: (zone: string) => void }) {
  const items = useMemo<ZoneItem[]>(() => {
    const now = Date.now()
    const list = allZones(viewerZone, zone ? [zone] : []).map((z) => {
      const region = z.includes('/') ? z.slice(0, z.indexOf('/')).replace(/_/g, ' ') : ''
      return { zone: z, city: zoneCity(z), secondary: [z === viewerZone ? t('Your zone') : region, zoneOffset(z, now)].filter(Boolean).join(' · ') }
    })
    return floatingAllowed ? [{ zone: FLOATING, city: t('Floating'), secondary: t('The same clock time wherever you are') }, ...list] : list
  }, [viewerZone, zone, floatingAllowed])
  return (
    <fieldset className="space-y-1.5" data-event-form-zone>
      <legend className="text-body font-medium text-default">{t('Time zone')}</legend>
      <Combobox<ZoneItem>
        label={t('Time zone')}
        placeholder={zoneText(zone)}
        source={items}
        getKey={(i) => i.zone || '__floating'}
        getLabel={(i) => i.city}
        getSecondary={(i) => i.secondary}
        pinned={[viewerZone]}
        selected={[zone || '__floating']}
        rankContext="due.zone"
        emptyText={t('No zone matches')}
        onPick={(i) => onChange(i.zone)}
      />
    </fieldset>
  )
}
