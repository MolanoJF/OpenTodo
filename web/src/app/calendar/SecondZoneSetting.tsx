// "Second time zone" in Settings > Language and region > Calendar grid (OpenSpec `calendar-time-zones` D4):
// the zone Combobox of the event editor with an "Off" choice listed first.
import { useMemo } from 'react'
import { zoneCity } from '../../lib/dates'
import { t } from '../../lib/i18n'
import { Combobox } from '../../ui'
import { useViewerZone } from '../useViews'
import { allZones, zoneOffset } from '../task-detail/ZoneControl'

interface ZoneItem { zone: string; city: string; secondary: string }

const OFF = ''

export function SecondZoneSetting({ zone, onChange }: { zone: string; onChange: (zone: string) => void }) {
  const viewerZone = useViewerZone()
  const items = useMemo<ZoneItem[]>(() => {
    const now = Date.now()
    const list = allZones(viewerZone, zone ? [zone] : []).filter((z) => z !== viewerZone).map((z) => {
      const region = z.includes('/') ? z.slice(0, z.indexOf('/')).replace(/_/g, ' ') : ''
      return { zone: z, city: zoneCity(z), secondary: [region, zoneOffset(z, now)].filter(Boolean).join(' · ') }
    })
    return [{ zone: OFF, city: t('Off'), secondary: '' }, ...list]
  }, [viewerZone, zone])
  return (
    <div className="sm:w-56" data-second-zone-setting>
      <Combobox<ZoneItem>
        label={t('Second time zone')}
        placeholder={zone ? zoneCity(zone) : t('Off')}
        source={items}
        getKey={(i) => i.zone || '__off'}
        getLabel={(i) => i.city}
        getSecondary={(i) => i.secondary}
        selected={[zone || '__off']}
        emptyText={t('No zone matches')}
        onPick={(i) => onChange(i.zone)}
      />
    </div>
  )
}
