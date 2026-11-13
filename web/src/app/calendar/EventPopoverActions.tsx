// The event popover's "All details" and its overflow: Duplicate and Convert to task (OpenSpec change
// calendar-event-editor D11). A lazy chunk of the popover: it loads when a popover opens, not with the grid.
import { useState } from 'react'
import { draftFromEvent } from '../../lib/eventCreate'
import { eventFullOf } from '../../lib/eventFull'
import { repeatSummary } from '../../lib/eventRepeat'
import { t } from '../../lib/i18n'
import type { CalendarEvent, CalendarSubscription } from '../../lib/types'
import { Button, Icon, Menu, MenuItem } from '../../ui'
import { useViewerZone } from '../useViews'
import { ConvertToTask } from './ConvertToTask'
import { openEventEditor } from './eventEditorStore'
import { eventDescription } from './useEventDetail'
import { useEventCreation } from './useWritableCalendars'

export function EventPopoverActions({ ev, sub, onClose }: { ev: CalendarEvent; sub?: CalendarSubscription; onClose?: () => void }) {
  const zone = useViewerZone()
  const { list, calendar } = useEventCreation()
  const [converting, setConverting] = useState(false)
  const target = list.find((c) => c.id === ev.subscriptionId) ?? calendar
  const duplicate = async () => {
    if (!target) return
    const draft = draftFromEvent(ev, await eventDescription(ev), target, zone)
    onClose?.()
    if (draft) openEventEditor({ mode: 'create', draft })
  }
  const repeat = ev.series?.custom ? t('Custom repeat') : ev.series?.repeat ? repeatSummary(ev.series.repeat, ev.start.date) : ''
  return (
    <>
      {repeat && <p className="flex items-center gap-1.5 text-meta text-muted" data-event-repeat><Icon name="repeat" size="xs" />{repeat}</p>}
      <div className="flex items-center justify-between gap-2 pt-1" data-event-actions>
        {ev.access?.update
          ? <Button size="sm" variant="secondary" data-event-open-editor onClick={() => { onClose?.(); openEventEditor({ mode: 'edit', eventId: ev.id }) }}>{t('All details')}</Button>
          : <span />}
        <Menu label={t('More actions')} kind="menu" trigger={<Icon name="ellipsis" />} triggerClassName="ui-btn ui-btn--ghost ui-btn--sm ui-btn--icon" align="end">
          {target && <MenuItem label={t('Duplicate')} icon="copy" data-event-duplicate onSelect={() => void duplicate()} />}
          <MenuItem label={t('Convert to task…')} icon="list-checks" data-event-convert onSelect={() => setConverting(true)} />
        </Menu>
        {converting && (
          <ConvertToTask ev={ev} calendar={sub?.name} getFields={async () => eventFullOf(ev, zone, await eventDescription(ev))} onClose={() => { setConverting(false); onClose?.() }} />
        )}
      </div>
    </>
  )
}
