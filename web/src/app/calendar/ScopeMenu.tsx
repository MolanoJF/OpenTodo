// "Which events?" (OpenSpec change calendar-event-series D12): the menu that asks, after a drag, a key press, a rename
// or a Save on an occurrence of a repeating event, whether the change is for this event, this and following events or
// all events. The overlay module's Menu, controlled, opened where the pointer or the block is; Escape or a click
// outside answers nothing and the block goes back. Loaded when a request is open (ScopeMenuHost).
import { useEffect } from 'react'
import { scopesFor, type ScopeBlock } from '../../lib/eventScope'
import { t } from '../../lib/i18n'
import type { EventScope } from '../../lib/types'
import { Menu, MenuItem } from '../../ui'
import type { ScopeRequest } from './eventScopeStore'

const LABEL: Record<EventScope, () => string> = {
  this: () => t('This event'),
  following: () => t('This and following events'),
  all: () => t('All events'),
}

const blockText = (b: ScopeBlock): string => (b === 'day-shift' ? t('This repeat can\'t move to another day for all events') : '')

/** What an item says beneath its name: why it is not available, or what the change resets. */
function noteOf(scope: EventScope, blocked: ScopeBlock | undefined, resets: ScopeRequest['resets']): string | undefined {
  if (blocked) return blockText(blocked)
  if (scope === 'this' || !resets) return undefined
  return resets === 'repeat' ? t('Changing the repeat resets events you changed individually') : t('Events you changed individually go back to the series\' times')
}

/** The menu for a request of the store (the calendar views host it), or for one a control makes itself (the reply row's). */
export default function ScopeMenu({ req }: { req: ScopeRequest }) {
  const deleting = req.change === 'delete'
  const label = req.only ? t('Answer for which events') : deleting ? t('Delete which events') : t('Change which events')
  const id = req.event.id
  // The block takes focus back when the menu closes, whatever the answer.
  useEffect(() => () => {
    const block = [...document.querySelectorAll<HTMLElement>('[data-event]')].find((e) => e.getAttribute('data-event') === id)
    setTimeout(() => block?.focus(), 0)
  }, [id])
  return (
    <>
      <span role="status" className="sr-only">{req.only ? t('Choose which events to answer') : t('Choose which events to change')}</span>
      <Menu kind="menu" label={label} trigger={<span className="sr-only" />} triggerClassName="sr-only"
        triggerProps={{ style: { position: 'fixed', left: req.at.x, top: req.at.y }, 'data-event-scope-menu': '' }} align="start" width="sm:w-72" open
        onOpenChange={(open) => { if (!open) req.resolve(null) }}>
        {(req.only ? req.only.map((scope): { scope: EventScope; blocked?: undefined } => ({ scope })) : scopesFor(req.event, req.change)).map(({ scope, blocked }) => (
          <MenuItem key={scope} label={LABEL[scope]()} description={noteOf(scope, blocked, req.resets)} disabled={!!blocked} data-event-scope={scope} onSelect={() => req.resolve(scope)} />
        ))}
      </Menu>
    </>
  )
}
