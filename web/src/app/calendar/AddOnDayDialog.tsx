// Quick add with a day as the date (month-and-timeline-views; lean-chrome D7): the
// Month's and Plan's `q` on the focused day, the calendar's empty days' "Add a task",
// and adding with the pointer (calendar-clarity D2), which may bring a time of the
// Day and Week grid. Created locally, so it works offline.
import { formatTime as toHHMM } from '../../lib/calendar'
import { formatDate, formatTime, logicalToday } from '../../lib/dates'
import type { ProjectRef, QuickAddInput } from '../../lib/nlp'
import { t } from '../../lib/i18n'
import { createTask } from '../../lib/ops'
import { addTasksUndoable } from '../../lib/pasteTasks'
import type { Due } from '../../lib/types'
import { showBulkToast } from '../bulk/toastStore'
import { Dialog, SegmentedControl } from '../../ui'
import { setNewKind } from '../../lib/writableCalendars'
import { QuickAdd } from '../QuickAdd'
import { openEventEditor } from './eventEditorStore'
import { useEventCreation } from './useWritableCalendars'

/**
 * The due of a line typed in the dialog: the dialog's day (and time) unless the line
 * names its own. A bare time ("at 4pm") reads as today's in the parser, so a parsed
 * due on today with a time keeps that time on the dialog's day.
 */
export function dueOnDay(input: Pick<QuickAddInput, 'due'>, date: string, time?: string, today = logicalToday()): Due {
  if (!input.due) return time ? { date, time } : { date }
  if (input.due.date === today && date !== today && input.due.time) return { ...input.due, date }
  return input.due
}

export function AddOnDayDialog({ date, time: start, estimate, projects, onClose }: {
  date: string
  /** Minutes from midnight of the grid's quarter hour, when added there. */
  time?: number
  /** The length of a sweep, in minutes: the new task's estimate. */
  estimate?: number
  projects: readonly ProjectRef[]
  onClose: () => void
}) {
  const time = start === undefined ? undefined : toHHMM(start)
  const when = time ? `${formatDate(date, 'long')} ${formatTime(time)}` : date
  const { list } = useEventCreation()
  // Task | Event, only while a calendar allows new events; Event opens the editor at the same place (calendar-event-editor D10).
  const kind = list.length > 0 && (
    <SegmentedControl<'task' | 'event'> label={t('Add a')} options={[{ id: 'task', label: t('Task') }, { id: 'event', label: t('Event') }]} value="task" data-add-kind
      onChange={(k) => { if (k === 'event') { void setNewKind('event'); onClose(); openEventEditor({ mode: 'create', place: { date, start, end: start !== undefined && estimate ? start + estimate : undefined } }) } }} />
  )
  const withEstimate = <T extends { estimate_minutes?: number | null }>(input: T): T => (estimate && input.estimate_minutes == null ? { ...input, estimate_minutes: estimate } : input)
  return (
    <Dialog title={t('Add a task on {adding}', { adding: when })} onClose={onClose} size="md" actions={kind || undefined}>
      <QuickAdd autoFocus hideHint placeholder={t('Task name')} label={t('Add a task due {adding}', { adding: when })} projects={projects} onAdd={async (input) => {
        await createTask(withEstimate({ ...input, due: dueOnDay(input, date, time) }))
        onClose()
      }} onAddMany={async (inputs) => {
        // A pasted list (paste-to-tasks D3): every line due then unless it names its own date.
        const added = await addTasksUndoable(inputs.map((input) => withEstimate({ ...input, due: dueOnDay(input, date, time) })))
        showBulkToast(added.label, added.undoId)
        onClose()
      }} />
    </Dialog>
  )
}
