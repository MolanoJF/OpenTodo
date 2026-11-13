// The guest list of an open popover (OpenSpec change calendar-event-guests D11, D12): requested from the server's cache
// when the person expands it, held in memory while the popover is open and dropped when it closes. It is never written to
// Dexie, localStorage or the service worker's cache.
import { useEffect, useState } from 'react'
import { ApiError } from '../../lib/api'
import { dropGuestList, loadGuestList, peekGuestList } from '../../lib/eventGuests'
import { isOnline } from '../../lib/online'
import type { EventGuestList } from '../../lib/types'

export type GuestListState =
  | { status: 'idle' }
  | { status: 'loading' }
  | { status: 'ready'; list: EventGuestList }
  | { status: 'unavailable'; reason: 'offline' | 'failed' }

/** The list of `eventId` once `wanted`; the memory it used is dropped when the popover unmounts. */
export function useGuestList(eventId: string, wanted: boolean): GuestListState {
  const [state, setState] = useState<GuestListState>({ status: 'idle' })
  useEffect(() => {
    if (!wanted) return
    const held = peekGuestList(eventId)
    if (held) { setState({ status: 'ready', list: held }); return }
    if (!isOnline()) { setState({ status: 'unavailable', reason: 'offline' }); return }
    let live = true
    setState({ status: 'loading' })
    loadGuestList(eventId).then(
      (list) => { if (live) setState({ status: 'ready', list }) },
      (err: unknown) => { if (live) setState({ status: 'unavailable', reason: err instanceof ApiError ? 'failed' : 'offline' }) },
    )
    return () => { live = false }
  }, [eventId, wanted])
  useEffect(() => () => dropGuestList(eventId), [eventId])
  return state
}
