// The event an editor opens on, and its description (OpenSpec change calendar-event-editor D2, D4, D11).
// The row is read once: what the person edits does not move under them.
import { useEffect, useState } from 'react'
import { type EventDetail, loadEventDetail } from '../../lib/eventDetail'

export { eventDescription, type DescriptionState, type EventDetail } from '../../lib/eventDetail'

/** Opens `eventId` for editing: its row, and its description from the calendar (or from the draft of a pending event). */
export function useEventDetail(eventId: string | null): EventDetail {
  const [state, setState] = useState<EventDetail>({ event: undefined, description: { status: 'loading' } })
  useEffect(() => {
    if (!eventId) return
    let live = true
    void loadEventDetail(eventId, (d) => { if (live) setState(d) })
    return () => { live = false }
  }, [eventId])
  return state
}
