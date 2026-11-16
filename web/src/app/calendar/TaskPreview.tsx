import { useEffect, useMemo } from 'react'
import { formatShort, formatTime, localDueOf } from '../../lib/dates'
import { formatMinutes } from '../../lib/duration'
import { t } from '../../lib/i18n'
import { useLabels, useProjects, useTask } from '../../lib/localData'
import { Button, Icon, IconButton, Popover } from '../../ui'
import { openTaskPage } from '../taskTrail'
import { useViewerZone } from '../useViews'
import { PreviewFact } from './PreviewFact'
import type { TaskPreviewRequest } from './taskPreviewStore'

export default function TaskPreview({ request, onClose }: { request: TaskPreviewRequest; onClose: () => void }) {
  const task = useTask(request.id)
  const projects = useProjects()
  const labels = useLabels()
  const zone = useViewerZone()
  const anchor = useMemo(() => ({ current: request.anchor }), [request.anchor])
  useEffect(() => { if (task === null || task?.deleted_at) onClose() }, [task, onClose])
  if (!task || task.deleted_at) return null
  const project = projects.find((p) => p.id === task.project_id)
  const due = task.due ? localDueOf(task.due, zone) : null
  const names = labels.filter((l) => task.labels?.includes(l.id)).map((l) => l.name)
  return <Popover open anchor={anchor} onClose={onClose} label={task.title || t('Task')} width="sm:w-[21rem]" className="p-0" align="start">
    <div data-task-preview={task.id}>
      <header className="hidden sm:flex items-center justify-between gap-2 border-b border-soft px-4 py-2 text-meta text-muted">
        <span className="flex items-center gap-2"><Icon name="circle-check-big" size="sm" />{t('Task')}</span>
        <div className="flex items-center gap-1"><IconButton tooltip={false} icon="external-link" label={t('Open task')} size="sm" onClick={() => { onClose(); openTaskPage(task.id) }} /><IconButton tooltip={false} icon="x" label={t('Close')} size="sm" onClick={onClose} /></div>
      </header>
      <div className="space-y-2 p-3">
        <h3 className={`hidden sm:block break-words text-body font-semibold text-strong ${task.completed_at ? 'line-through' : ''}`}>{task.title || t('Untitled')}</h3>
        <div className="space-y-1 text-default">
          {due && <PreviewFact icon="clock"><p>{formatShort(due.date)}{due.time ? ` · ${formatTime(due.time)}` : ` · ${t('All day')}`}</p>{!!task.estimate_minutes && <p className="text-meta text-muted">{formatMinutes(task.estimate_minutes)}</p>}</PreviewFact>}
          {task.deadline && <PreviewFact icon="hourglass">{t('Deadline')}: {formatShort(task.deadline)}</PreviewFact>}
          <PreviewFact icon="folder"><span data-hue={request.hue} className="inline-flex items-center gap-2"><span aria-hidden className="size-2.5 rounded-full bg-hue-solid" />{project?.name ?? t('Inbox')}</span></PreviewFact>
          {names.length > 0 && <PreviewFact icon="tag">{names.join(', ')}</PreviewFact>}
          {task.description && <PreviewFact icon="list"><p className="max-h-24 overflow-y-auto whitespace-pre-wrap text-meta">{task.description}</p></PreviewFact>}
        </div>
        <div className="border-t border-soft pt-3"><Button size="sm" icon="external-link" data-task-preview-open onClick={() => { onClose(); openTaskPage(task.id) }}>{t('All details')}</Button></div>
      </div>
    </div>
  </Popover>
}
