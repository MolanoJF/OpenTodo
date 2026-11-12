// The grid's edits of connected calendars' events (OpenSpec change calendar-event-edits D7):
// a drop, an arrow key and Delete end in the same commands as the popover (eventActions).
// Event blocks are keyed `event:<id>` like their layout entries.
import { useCallback, useEffect, useMemo, useRef } from 'react'
import { cachedEvent, currentEventId, useEventCommands } from '../../lib/calendarEventEdits'
import { localDueOf } from '../../lib/dates'
import { eventDropChange, eventEditable, type EventNudge, eventNudgeChange, type EventTarget } from '../../lib/eventDrop'
import type { CalendarEvent } from '../../lib/types'
import { reportActionError } from '../actionError'
import type { DropTarget } from '../calendarDrag'
import { changeEvent, deleteEvent } from './eventActions'

/** The calendar keys that act on a selected event. */
export type EventKeys = 'nudge' | 'shift-day' | 'estimate' | 'estimate-start'

/** The nudge an arrow key makes: `dir` is -1 (up, left) or 1 (down, right). */
export function nudgeOf(key: EventKeys, dir: -1 | 1): EventNudge {
  switch (key) {
    case 'nudge': return { minutes: dir * 15 }
    case 'shift-day': return { days: dir }
    case 'estimate': return { end: dir * 15 }
    default: return { start: dir * 15 }
  }
}

export interface EventEdits {
  /** The shown events by layout id (`event:<id>`). */
  byKey: Map<string, CalendarEvent>
  /** Layout ids of events with a queued change. */
  saving: ReadonlySet<string>
  drop: (key: string, target: DropTarget) => void
  /** An arrow key on the event: calls `done` with the day it lands on. Quick presses run in order on the freshest row. */
  nudge: (key: string, n: EventNudge, done?: (date: string) => void) => void
  remove: (key: string) => boolean
}

export function useEventEdits(events: readonly CalendarEvent[], zone: string | undefined, selected: string | null, select: (id: string) => void): EventEdits {
  const byKey = useMemo(() => new Map(events.map((e) => [`event:${e.id}`, e])), [events])
  const commands = useEventCommands()
  const saving = useMemo(() => new Set([...commands.keys()].map((id) => `event:${id}`)), [commands])
  const latest = useRef(byKey)
  latest.current = byKey
  const queue = useRef<Promise<void>>(Promise.resolve())

  // An edit that moved the selected event gave it a new id: the selection follows.
  useEffect(() => {
    if (!selected?.startsWith('event:') || byKey.has(selected)) return
    const next = `event:${currentEventId(selected.slice(6))}`
    if (next !== selected && byKey.has(next)) select(next)
  }, [selected, byKey, select])

  const drop = useCallback((key: string, target: DropTarget) => {
    const ev = latest.current.get(key)
    if (!ev || target.type === 'into-slot' || !eventEditable(ev, zone)) return
    const change = eventDropChange(ev, target as EventTarget, zone)
    if (change) void changeEvent(ev, change, target.type === 'slot' || target.type === 'lane' ? 'moved' : 'updated')
  }, [zone])

  const nudge = useCallback((key: string, n: EventNudge, done?: (date: string) => void) => {
    queue.current = queue.current.then(async () => {
      const shown = latest.current.get(`event:${currentEventId(key.slice(6))}`)
      const ev = shown && (await cachedEvent(shown.id))
      const change = ev && eventEditable(ev, zone) ? eventNudgeChange(ev, n, zone) : null
      if (ev && change?.start && (await changeEvent(ev, change, 'moved'))) done?.(change.start.tz && change.start.time ? localDueOf({ date: change.start.date, time: change.start.time, tz: change.start.tz }, zone).date : change.start.date)
    }).catch(reportActionError)
  }, [zone])

  const remove = useCallback((key: string): boolean => {
    const ev = latest.current.get(key)
    if (!ev?.access?.delete || ev.allDay) return false
    void deleteEvent(ev)
    return true
  }, [])

  return { byKey, saving, drop, nudge, remove }
}
