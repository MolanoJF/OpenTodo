// Convert an event to a task (OpenSpec change calendar-event-editor D11): the task is made with the
// normal operations (local-first, so it works offline), then the event is deleted with the usual 8 second
// pause. One toast and one Undo reverse both: the task goes to the Trash and the delete is cancelled.
import { DELETE_DELAY_MS } from '../../lib/calendarEventEdits'
import { readString, writeString } from '../../lib/deviceStorage'
import { MAX_ESTIMATE_MINUTES } from '../../lib/duration'
import { daysBetweenISO } from '../../lib/dates'
import { parseTime } from '../../lib/calendar'
import { type EventFull } from '../../lib/eventFull'
import { t } from '../../lib/i18n'
import type { NewTaskInput } from '../../lib/ops'
import { addTasksUndoable } from '../../lib/pasteTasks'
import { undoBatch } from '../../lib/undo'
import type { CalendarEvent } from '../../lib/types'
import { reportActionError } from '../actionError'
import { ACTION_TOAST_MS, showBulkToast } from '../bulk/toastStore'
import { queueDelete } from './eventActions'

const DELETE_KEY = 'opentodo.calendar.convertDelete'

/** Whether the switch "Delete the event from <calendar>" was left on on this device (on by default). */
export const rememberedConvertDelete = (): boolean => readString(DELETE_KEY) !== '0'
export const rememberConvertDelete = (on: boolean): void => { writeString(DELETE_KEY, on ? '1' : '0') }

/** The minutes from the start to the end of a timed event's fields (both in the same zone), at least 1. */
function minutesOf(f: EventFull): number | null {
  const a = parseTime(f.startTime)
  const b = parseTime(f.endTime)
  if (a === null || b === null) return null
  return daysBetweenISO(f.startDate, f.endDate) * 1440 + b - a
}

/** The task the fields stand for (design D11): unplaced, so it lands where any task without a project does. */
export function taskInputOf(f: EventFull, fallbackTitle = ''): NewTaskInput {
  const place = f.location.trim() ? t('Location: {place}', { place: f.location.trim() }) : ''
  const description = [f.description.trim(), place].filter(Boolean).join('\n\n')
  const input: NewTaskInput = { title: f.title.trim() || fallbackTitle || t('Event'), ...(description ? { description } : {}) }
  if (f.allDay) {
    // The last day is the due; an event over several days starts the span on its first day.
    return f.endDate > f.startDate ? { ...input, due: { date: f.endDate }, start: f.startDate, span: true } : { ...input, due: { date: f.endDate } }
  }
  const length = minutesOf(f)
  return {
    ...input,
    due: { date: f.startDate, time: f.startTime, ...(f.zone ? { tz: f.zone } : {}) },
    ...(length && length > 0 ? { estimate_minutes: Math.min(MAX_ESTIMATE_MINUTES, length) } : {}),
  }
}

/** Makes the task, deletes the event when asked, and shows the one toast whose Undo reverses both. */
export async function convertEvent(ev: CalendarEvent, fields: EventFull, deleteToo: boolean): Promise<boolean> {
  try {
    const added = await addTasksUndoable([taskInputOf(fields, ev.summary)])
    const restore = deleteToo ? await queueDelete(ev) : null
    const undo = () => { void (async () => {
      if (added.undoId) await undoBatch(added.undoId)
      if (restore) await restore()
    })().catch(reportActionError) }
    showBulkToast(t('Task created'), null, { ttl: restore ? DELETE_DELAY_MS : ACTION_TOAST_MS, action: { label: t('Undo'), run: undo } })
    return true
  } catch (err) {
    reportActionError(err)
    return false
  }
}

