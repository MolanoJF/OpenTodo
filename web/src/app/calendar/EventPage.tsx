import { useEffect, useMemo } from 'react'
import { logicalToday } from '../../lib/dates'
import { closePanel } from '../panel'
import { closeEventEditor, useEventEditor, type EventEditorRequest } from './eventEditorStore'
import { EventPanel } from './EventPanel'

/** A normal shell page, including direct links and reloads of an existing event. */
export default function EventPage({ id }: { id?: string }) {
  const current = useEventEditor()
  const request = useMemo<EventEditorRequest>(() => {
    if (current && (id ? current.mode === 'edit' && current.eventId === id : current.mode === 'create')) return current
    return id ? { mode: 'edit', key: 0, eventId: id } : { mode: 'create', key: 0, place: { date: logicalToday() } }
  }, [current, id])
  useEffect(() => closePanel(), [id])
  return <EventPanel key={request.key + (id ?? 'new')} request={request} frame="route" onClose={closeEventEditor} />
}
