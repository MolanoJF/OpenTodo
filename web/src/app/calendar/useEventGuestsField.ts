// The guests and the video call of the full editor (OpenSpec change calendar-event-guests D13): what the Guests and
// Video call fields show and what editing them means for Save. They are shown where the calendar lets the person change
// guests (Microsoft), and read-only for an event that has some. A change that tells guests is sent when confirmed and
// never queued, so while the device is offline Save waits with the reason.
import { isPendingId } from '../../lib/calendarEventPatch'
import type { EventFull } from '../../lib/eventFull'
import { useOnline } from '../../lib/online'
import type { CalendarEvent, CalendarEventDetail, CalendarSubscription } from '../../lib/types'
import type { GuestsFieldProps } from './GuestsField'
import type { VideoCallProps } from './EventVideoCall'
import type { DescriptionState } from './useEventDetail'

export interface GuestsEditing {
  guests?: GuestsFieldProps
  videoCall?: VideoCallProps
  /** Saving sends a request that tells people now: it needs a connection. */
  sendsNow: boolean
  offlineBlocked: boolean
}

export function useEventGuestsField(a: {
  editing: boolean
  ev: CalendarEvent | null
  /** The event's calendar (edit) or the chosen one (create). */
  sub: CalendarSubscription | undefined
  form: EventFull | null
  set: (patch: Partial<EventFull>) => void
  info: Pick<CalendarEventDetail, 'guests' | 'conference' | 'onlineMeeting'> | undefined
  description: DescriptionState
}): GuestsEditing {
  const online = useOnline()
  const { editing, ev, sub, form, set, info, description } = a
  const pending = !!ev && isPendingId(ev.id) // a queued create has no calendar copy to add guests to
  const canEdit = editing ? (info?.guests?.canEdit ?? (!!sub?.guests?.edit && !!ev?.access?.update)) : !!sub?.guests?.edit
  const show = !!form && !pending && (editing ? !!(info?.guests || ev?.guests || canEdit) : canEdit)
  const adding = (form?.guestAdds.length ?? 0) > 0 || (form?.guestRemoves.length ?? 0) > 0
  const sendsNow = editing ? !!ev?.access?.notifies || adding || !!form?.teams : adding
  const guests: GuestsFieldProps | undefined = show && form ? {
    state: editing ? description.status : 'ready', reason: description.status === 'unavailable' ? description.reason : undefined,
    list: editing && info?.guests ? info.guests : null, editable: canEdit, online, adds: form.guestAdds, removes: form.guestRemoves,
    onChange: ({ adds, removes }) => set({ guestAdds: adds, guestRemoves: removes }),
  } : undefined
  const available = editing ? !!(info?.onlineMeeting?.available) : !!sub?.caps?.onlineMeeting
  const conference = info?.conference ?? ev?.conference
  const videoCall: VideoCallProps | undefined = form && !pending && (available || conference) ? { available, conference, value: form.teams, onChange: (teams) => set({ teams }) } : undefined
  return { guests, videoCall, sendsNow, offlineBlocked: sendsNow && !online }
}
