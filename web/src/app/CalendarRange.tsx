// Shared view dropdown and navigation for every calendar mode.
import { useEffect, useRef, useState } from 'react'
import { Button, Icon, Menu, MenuItem, MenuSeparator, useMediaQuery } from '../ui'
import { calendarDayCount } from '../lib/calendarMode'
import { t, msg, translatedList } from '../lib/i18n'
import { useCalendarGo } from './calendar/scope'

export type CalendarRangeMode = 'day' | 'week' | 'month' | 'plan' | 'year'

const MODES: { mode: CalendarRangeMode; label: string }[] = translatedList([
  { mode: 'day', label: msg('Day') },
  { mode: 'week', label: msg('Week') },
  { mode: 'month', label: msg('Month') },
  { mode: 'plan', label: msg('Plan') },
  { mode: 'year', label: msg('Year') },
], 'label')

export function CalendarRangeSwitch({ mode, date }: { mode: CalendarRangeMode; date?: string }) {
  const go = useCalendarGo()
  const count = calendarDayCount.use()
  const custom = mode === 'week' && count !== 7
  const label = custom ? t('{n} days', { n: count }) : MODES.find((m) => m.mode === mode)!.label
  const [days, setDays] = useState(false)
  const [open, setOpen] = useState(false)
  const items = useRef<HTMLDivElement>(null)
  useEffect(() => { if (open) items.current?.querySelector<HTMLButtonElement>('[role^="menuitem"]')?.focus() }, [days, open])
  return (
    <Menu kind="menu" label={t('Calendar range')} namedByContent title={t('Calendar range')} align="start" anchored width="w-60" open={open}
      onOpenChange={(next) => { setOpen(next); if (!next) setDays(false) }}
      trigger={<><span className="sr-only">{t('Calendar range')} </span><span>{label}</span><Icon name="chevron-down" size="sm" /></>}
      triggerClassName="ui-btn ui-btn--secondary ui-btn--sm gap-2 max-md:gap-1 max-md:px-1.5" triggerProps={{ 'data-calendar-range': mode }}>
      <div ref={items} onKeyDown={(e) => { if (days && e.key === 'ArrowLeft') { e.preventDefault(); setDays(false) } }}>
        {days ? <>
          <button type="button" role="menuitem" data-menu-label={t('Back')} className="ui-btn ui-btn--ghost w-full justify-start gap-2" onClick={() => setDays(false)}>
            <Icon name="chevron-left" />{t('Back')}
          </button>
          <MenuSeparator />
          {[2, 3, 4, 5, 6].map((n) => <MenuItem key={n} label={t('{n} days', { n })} checked={custom && count === n} data-calendar-days={n}
            onSelect={() => { calendarDayCount.set(n); go('week', date) }} />)}
        </> : <>
          {MODES.map((m) => <MenuItem key={m.mode} label={m.label} checked={!custom && mode === m.mode} data-calendar-mode={m.mode}
            onSelect={() => { if (m.mode === 'week') calendarDayCount.set(7); go(m.mode, date) }} />)}
          <MenuSeparator />
          <button type="button" role="menuitem" data-menu-label={t('Number of days')} data-calendar-day-options
            className="ui-btn ui-btn--ghost w-full justify-start gap-2 px-2.5 py-2" onClick={() => setDays(true)}
            onKeyDown={(e) => { if (e.key === 'ArrowRight') { e.preventDefault(); e.stopPropagation(); setDays(true) } }}>
            <span className="w-4 shrink-0">{custom && <Icon name="check" />}</span>
            <span className="flex-1 text-left">{t('Number of days')}</span><Icon name="chevron-right" />
          </button>
        </>}
      </div>
    </Menu>
  )
}

export function CalendarNavigation({ onToday, onPrevious, onNext, previousLabel, nextLabel }: {
  onToday: () => void; onPrevious: () => void; onNext: () => void; previousLabel: string; nextLabel: string
}) {
  const showTodayLabel = useMediaQuery('(min-width: 22.5rem)')
  return <>
    <Button variant="secondary" size="sm" aria-label={t('Today')} icon={showTodayLabel ? undefined : 'calendar-days'} data-calendar-today onClick={onToday}>{showTodayLabel ? t('Today') : undefined}</Button>
    <span className="ml-2 flex items-center gap-1 max-md:ml-0 max-md:gap-0" role="group" aria-label={t('Range')}>
      <Button variant="ghost" size="sm" icon="chevron-left" aria-label={previousLabel} onClick={onPrevious} />
      <Button variant="ghost" size="sm" icon="chevron-right" aria-label={nextLabel} onClick={onNext} />
    </span>
  </>
}
