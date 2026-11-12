// Adding with the pointer (OpenSpec change calendar-clarity, design D2; GitHub issue
// 555): a double-click on an empty place of Day, Week or Month opens the add dialog for
// that day (and, on the grid, that quarter hour); a right-click, or a 500 ms long press
// on touch, opens a small menu there: Add task here…, and on the grid New frame here…
// (calendar-frames) and New slot here… (calendar-slots). Below 640 px the menu is a
// bottom sheet, like every menu. The keyboard has `q` for the same dialog.
import { useCallback, useEffect, useState, type ReactNode } from 'react'
import { setNewKind, type NewKind } from '../../lib/writableCalendars'
import { isoWeekday } from '../../lib/frames'
import type { useCalendarDrag } from '../calendarDrag'
import { t } from '../../lib/i18n'
import type { ProjectRef } from '../../lib/nlp'
import { Menu, MenuItem } from '../../ui'
import { reportActionError } from '../actionError'
import { AddOnDayDialog } from './AddOnDayDialog'
import { addTaskRequest, openEventEditor } from './eventEditorStore'
import { useEventCreation } from './useWritableCalendars'
import { requestFrameAt } from './framesRequest'
import { newSlotAt } from './slotActions'
import { HOLD_MS, startHold } from './hold'

/** A place to add at: a day, and on the Day and Week grid the quarter hour (minutes from midnight), and the end of a sweep. */
export interface AddPlace {
  date: string
  start?: number
  end?: number
}

/** How long a finger rests on an empty place before the menu opens (design D2). */
export const ADD_HOLD_MS = HOLD_MS

/** Things on the grid that have their own meaning under the pointer: never an empty place. */
const OCCUPIED = '[data-cal-id],[data-event],[data-event-chip],[data-frame-name],[data-overflow],[data-deadline-marker-for],[data-month-task],[data-month-more],button,a,input,[role="button"]'

/** True when `target` is an empty part of `root` (no block, event, chip, frame title or control under it). */
export function isEmptyPlace(target: EventTarget | null, root: Element): boolean {
  if (!(target instanceof Element) || !root.contains(target)) return false
  const hit = target.closest(OCCUPIED)
  return !hit || !root.contains(hit) || hit === root
}

/**
 * A long press of a finger on an empty place: `onHold(x, y)` after ADD_HOLD_MS when the
 * finger has not moved; any movement (a scroll) or a release ends it. Other pointers never hold.
 */
export const startAddHold = startHold

/** Adding with the pointer (calendar-clarity D2) as the day columns take it: the dialog, the menu, and the sweep (calendar-event-editor D10). */
export interface AddGestures extends Pick<AddHere, 'add' | 'menuAt' | 'kind'> {
  /** Starts a sweep over an empty stretch of a day column; only while a calendar allows new events. */
  sweep?: (e: React.PointerEvent, place: { date: string; start: number }) => void
}

/** The gestures of a view: the add handlers, plus a sweep while a writable calendar exists (no sweep for task-only users). */
export function gesturesOf(add: AddHere, start: ReturnType<typeof useCalendarDrag>['start']): AddGestures {
  return {
    add: add.add, menuAt: add.menuAt, kind: add.kind,
    sweep: add.eventsOn ? (e, place) => start(e, { taskId: 'new:range', entity: 'range', kind: 'create', blockDate: place.date, blockStart: place.start }) : undefined,
  }
}

export interface AddHere {
  /** Shift+Q: an event on a day at its next free half hour; absent while no calendar allows new events. */
  newEvent?: (date: string) => void
  /** Opens what the device added last for a place: the add dialog, or the event editor (a double-click, `q`, a sweep). */
  add: (place: AddPlace) => void
  /** The explicit choices (the menu's two items): each is remembered. */
  addTask: (place: AddPlace) => void
  addEvent: (place: AddPlace) => void
  /** The remembered kind, and whether an event can be added at all (a writable calendar exists). */
  kind: NewKind
  eventsOn: boolean
  /** Opens the menu for a place at a point of the screen. */
  menuAt: (place: AddPlace, x: number, y: number) => void
  /** The dialog and the menu, to render once. */
  element: ReactNode
}

/** The add dialog and the add menu of one calendar view. */
export function useAddHere(projects: readonly ProjectRef[]): AddHere {
  const [adding, setAdding] = useState<AddPlace | null>(null)
  const [menu, setMenu] = useState<(AddPlace & { x: number; y: number }) | null>(null)
  const { list, kind } = useEventCreation()
  const eventsOn = list.length > 0
  const addTask = useCallback((place: AddPlace) => { setMenu(null); setAdding(place); if (eventsOn) void setNewKind('task') }, [eventsOn])
  const addEvent = useCallback((place: AddPlace) => { setMenu(null); void setNewKind('event'); openEventEditor({ mode: 'create', place }) }, [])
  const add = useCallback((place: AddPlace) => {
    setMenu(null)
    if (eventsOn && kind === 'event') openEventEditor({ mode: 'create', place })
    else setAdding(place)
  }, [eventsOn, kind])
  // The editor's Task | Event control switches back to the add dialog (D10).
  const requested = addTaskRequest.use()
  useEffect(() => { if (requested) { addTaskRequest.set(null); setAdding(requested) } }, [requested])
  const menuAt = useCallback((place: AddPlace, x: number, y: number) => setMenu({ ...place, x, y }), [])
  const element = (
    <>
      {menu && (
        // The menu's hidden trigger sits at the pointer, so the panel opens there (as the row menu's does).
        <Menu kind="menu" label={t('Add here')} trigger={<span className="sr-only" />} triggerClassName="sr-only"
          triggerProps={{ style: { position: 'fixed', left: menu.x, top: menu.y } }} align="start" width="sm:w-60" open
          onOpenChange={(open) => { if (!open) setMenu(null) }}>
          <AddHereItems place={menu} onAdd={addTask} onAddEvent={eventsOn ? addEvent : undefined} eventFirst={kind === 'event'} />
        </Menu>
      )}
      {adding && <AddOnDayDialog date={adding.date} time={adding.start} estimate={adding.end !== undefined && adding.start !== undefined ? adding.end - adding.start : undefined} projects={projects} onClose={() => setAdding(null)} />}
    </>
  )
  const newEvent = eventsOn ? (date: string) => void import('./newEventNow').then((m) => m.newEventOn(date)).catch(reportActionError) : undefined
  return { add, addTask, addEvent, kind, eventsOn, newEvent, menuAt, element }
}

/**
 * The menu's items: Add task here…, Add event here… while a calendar allows new events (the remembered
 * kind first), and on the grid the frame and the slot of that quarter hour.
 */
export function AddHereItems({ place, onAdd, onAddEvent, eventFirst = false }: { place: AddPlace; onAdd: (place: AddPlace) => void; onAddEvent?: (place: AddPlace) => void; eventFirst?: boolean }) {
  const timed = place.start !== undefined
  const task = <MenuItem label={t('Add task here…')} icon="plus" data-add-here="task" onSelect={() => onAdd(place)} />
  const event = onAddEvent && <MenuItem label={t('Add event here…')} icon="calendar-clock" data-add-here="event" onSelect={() => onAddEvent(place)} />
  return (
    <>
      {eventFirst ? <>{event}{task}</> : <>{task}{event}</>}
      {timed && <MenuItem label={t('New frame here…')} icon="rows-2" data-add-here="frame" onSelect={() => requestFrameAt(isoWeekday(place.date), place.start!)} />}
      {timed && <MenuItem label={t('New slot here…')} icon="layers" data-add-here="slot" onSelect={() => newSlotAt(place.date, place.start!)} />}
    </>
  )
}
