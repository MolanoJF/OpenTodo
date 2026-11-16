import { useDateTimeFormat } from '../DateTimePrefsProvider'
import { useGridPrefs } from '../../lib/liveData'
import { saveGridPrefs } from '../../lib/localData'
import { setUserPref } from '../../lib/userPrefs'
import { act } from '../actionError'
// What Day, Week, Month and Plan share of calendar-clarity: Colour by (D3), the
// calendar's query filter and its chip (D4), the project scope (D5), adding with the
// pointer (D2) and the overflow items that choose them.
import { useCallback, useEffect, useMemo, useState, type ReactNode } from 'react'
import { CALENDAR_COLOR_BY_KEY } from '../../lib/colorBy'
import { t } from '../../lib/i18n'
import type { ProjectRef } from '../../lib/nlp'
import { deadlineMarkers } from '../../lib/calendar'
import type { Project, Task } from '../../lib/types'
import { DESKTOP_HEADER_QUERY, MenuGroup, MenuItem, MenuSeparator, PageHeader, setPageBar, TruncatedText, useMediaQuery } from '../../ui'
import { saveTray } from './planSource'
import { useAddHere, type AddHere } from './addHere'
import { EventEditorHost } from './EventEditorHost'
import { ScopeMenuHost } from './ScopeMenuHost'
import { ProjectionsMenuItem } from './projectionSwitches'
import { ColorByMenuItems, useColorBy, type ColorByState } from './colorByControls'
import { CALENDAR_FILTER_KEY, FilterQueryChip, QueryFilterHost, useQueryFilter } from './queryFilter'
import { useCalendarScope, type CalendarHeaderParts, type CalendarScope } from './scope'

export interface CalendarChrome {
  color: ColorByState
  scope: CalendarScope | null
  /** True when a task is drawn: inside the scope and kept by the filter. */
  keeps: (task: Task) => boolean
  /** True when a task belongs to the scope (capacity counts these, filtered or not). */
  inScope: (task: Task) => boolean
  /** The active filter's chip, or null. */
  chip: ReactNode
  /** Filter… and Colour by, for the view's overflow menu. */
  overflow: ReactNode
  add: AddHere
  /** The filter dialog and the add dialog and menu. */
  element: ReactNode
  /** Plan from a list: in a project's Calendar layout the planning list opens on the project (D5). */
  plan: (open: boolean, toggle: () => void) => void
}

/** The calendar's page header, or the project page's header in a project's Calendar layout (design D5). */
export function CalendarPageHeader({ chrome, ...parts }: { chrome: Pick<CalendarChrome, 'scope'> } & CalendarHeaderParts) {
  const desktop = useMediaQuery(DESKTOP_HEADER_QUERY)
  if (chrome.scope) return <>{chrome.scope.header(parts)}</>
  if (!desktop) return <PhoneCalendarHeader {...parts} />
  return <PageHeader kind="day" title={parts.title} help="calendar" meta={parts.meta ?? undefined} actions={parts.actions} overflow={parts.overflow} />
}

function PhoneCalendarHeader({ title, phoneTitle, meta, actions, overflow }: CalendarHeaderParts) {
  useEffect(() => {
    setPageBar({ title, day: true, overflow, help: 'calendar' })
    return () => setPageBar(null)
  })
  return <header className="shrink-0 pb-2" data-page-header data-page-head="calendar" data-phone-calendar-header>
    <div className="flex min-w-0 items-center gap-2" data-page-header-row>
      <TruncatedText as="h1" text={phoneTitle ?? title} className="min-w-0 flex-1 text-body font-semibold leading-tight text-strong" data-page-title="" />
      <div className="flex shrink-0 items-center gap-1" data-page-actions>{actions}</div>
    </div>
    {meta && <div className="mt-1 min-w-0 text-meta text-muted" data-page-meta>{meta}</div>}
  </header>
}

/**
 * What a view draws (design D1, D4): the shown tasks the scope and the filter keep, how
 * many open ones the filter hides per day (capacity still counts them), and the
 * deadline markers of the kept tasks among `all` over [from, to].
 */
export function useDrawn(chrome: Pick<CalendarChrome, 'keeps' | 'inScope'>, shown: readonly Task[], all: readonly Task[], projects: readonly Pick<Project, 'id' | 'archived'>[], from: string, to: string) {
  const { keeps, inScope } = chrome
  const drawn = useMemo(() => shown.filter(keeps), [shown, keeps])
  const hidden = useMemo(() => {
    const out = new Map<string, number>()
    for (const tk of shown) if (tk.due && !tk.completed_at && inScope(tk) && !keeps(tk)) out.set(tk.due.date, (out.get(tk.due.date) ?? 0) + 1)
    return out
  }, [shown, inScope, keeps])
  const deadlines = useMemo(() => deadlineMarkers(all.filter(keeps), projects, from, to), [all, keeps, projects, from, to])
  return { drawn, hidden, deadlines }
}

/**
 * `all` are every live task (for `blocked` and friends in the filter). `projections`
 * adds "Show repeating occurrences" to the overflow (Day, Week, Month and Year; Plan always draws them, series-occurrences D4).
 * `colorBy: false` leaves Colour by out (the Year draws no task rows, year-view D3).
 */
export function useCalendarChrome(all: readonly Task[], projects: readonly ProjectRef[], { projections = false, colorBy = true }: { projections?: boolean; colorBy?: boolean } = {}): CalendarChrome {
  const color = useColorBy(CALENDAR_COLOR_BY_KEY)
  const filter = useQueryFilter(CALENDAR_FILTER_KEY, all)
  const scope = useCalendarScope()
  const [open, setOpen] = useState(false)
  const add = useAddHere(projects)
  const ids = scope?.projectIds
  const inScope = useCallback((task: Task) => !ids || (!!task.project_id && ids.has(task.project_id)), [ids])
  const test = filter.test
  const keeps = useCallback((task: Task) => inScope(task) && (!test || test(task)), [inScope, test])
  const chip = filter.query ? <FilterQueryChip query={filter.query} onOpen={() => setOpen(true)} onClear={() => filter.setQuery('')} /> : null
  const { resolved } = useDateTimeFormat()
  const grid = useGridPrefs()
  const overflow = useMemo(() => (
    <>
      {projections && <ProjectionsMenuItem surface="calendar" />}
      <MenuSeparator />
      <MenuItem label={t('Filter…')} icon="filter" description={t('Show only the tasks a query matches')} data-calendar-filter onSelect={() => setOpen(true)} />
      <MenuItem label={t('Shade outside grid hours')} icon="moon" checked={!!grid.shadeOffHours} description={t('Dim hours outside your configured calendar day')} data-calendar-shading onSelect={() => act(saveGridPrefs({ ...grid, shadeOffHours: !grid.shadeOffHours }))} />
      <MenuGroup label={t('Week starts on')}>
        <MenuItem label={t('Monday')} icon="calendar-days" checked={resolved.weekStart === 1} description={t('Start the calendar week on Monday')} data-week-start="mon" onSelect={() => act(setUserPref('week_start', 'mon'))} />
        <MenuItem label={t('Sunday')} icon="calendar-days" checked={resolved.weekStart === 0} description={t('Start the calendar week on Sunday')} data-week-start="sun" onSelect={() => act(setUserPref('week_start', 'sun'))} />
      </MenuGroup>
      {colorBy && <ColorByMenuItems mode={color.mode} onChange={color.setMode} />}
    </>
  ), [color.mode, color.setMode, projections, colorBy, grid, resolved.weekStart])
  const element = (
    <>
      <QueryFilterHost open={open} filter={filter} title={t('Filter the calendar')} onClose={() => setOpen(false)} />
      {add.element}
      <EventEditorHost />
      <ScopeMenuHost />
    </>
  )
  const projectId = scope?.projectId
  const plan = useCallback((isOpen: boolean, toggle: () => void) => {
    if (!isOpen && projectId) saveTray(`project:${projectId}`)
    toggle()
  }, [projectId])
  return { color, scope, keeps, inScope, chip, overflow, add, element, plan }
}
