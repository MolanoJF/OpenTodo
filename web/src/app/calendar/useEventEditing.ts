// The state of the full event editor (OpenSpec change calendar-event-editor D11): the fields as the
// person left them over the event as it is (an `edits` object over a `base`, so a description that
// arrives late does not disturb what was typed), the Save for both modes, Duplicate and Delete.
import { useMemo, useState } from 'react'
import { addDaysISO } from '../../lib/dates'
import { formatTime } from '../../lib/calendar'
import { draftFromEvent, draftFromFull, useCalendarSubscription, useEventCreation } from '../../lib/eventCreate'
import { eventFullChange, eventFullOf, pointsOf, switchAllDay, type EventFull } from '../../lib/eventFull'
import type { CalendarEvent, CalendarSubscription } from '../../lib/types'
import { useViewerZone } from '../useViews'
import { addEvent } from './eventAdd'
import { changeEvent, deleteEvent } from './eventActions'
import { type EventEditorRequest, openEventEditor } from './eventEditorStore'
import { useEventDetail, type DescriptionState } from './useEventDetail'

const DEFAULT_START = 9 * 60
const DEFAULT_LENGTH = 30

/** The fields of a new event: a place's start and end (or 09:00 and half an hour), or a draft's own. */
function createBase(request: Extract<EventEditorRequest, { mode: 'create' }>, viewerZone: string): EventFull {
  const d = request.draft
  if (d) return eventFullOf({ id: '', subscriptionId: d.subscriptionId, uid: d.clientId, summary: d.summary, location: d.location, start: d.start, end: d.end, allDay: d.allDay, free: d.free }, viewerZone, d.description)
  const date = request.place?.date ?? ''
  const start = request.place?.start ?? DEFAULT_START
  const end = Math.max(start + 15, request.place?.end ?? start + DEFAULT_LENGTH)
  const over = end >= 1440
  return {
    title: '', allDay: false, startDate: date, startTime: formatTime(start), endDate: over ? addDaysISO(date, 1) : date, endTime: formatTime(over ? end - 1440 : end),
    zone: viewerZone, location: '', description: '', free: false,
  }
}

export function useEventEditing(request: EventEditorRequest, close: () => void) {
  const viewerZone = useViewerZone()
  const { list, calendar: fallback } = useEventCreation()
  const editing = request.mode === 'edit'
  const detail = useEventDetail(editing ? request.eventId : null)
  const ev: CalendarEvent | null = detail.event ?? null
  const eventCalendar = useCalendarSubscription(ev?.subscriptionId)
  const [edits, setEdits] = useState<Partial<EventFull>>({})
  const [chosen, setChosen] = useState<string | null>(request.mode === 'create' ? request.draft?.subscriptionId ?? null : null)
  const description = useMemo<DescriptionState>(() => (editing ? detail.description : { status: 'ready', text: '', editable: true }), [editing, detail.description])
  const loaded = description.status === 'ready' && description.editable ? description.text : null
  const base = useMemo(() => (request.mode === 'create' ? createBase(request, viewerZone) : ev ? eventFullOf(ev, viewerZone, description.status === 'ready' ? description.text : '') : null),
    [request, ev, viewerZone, description])
  const form = base ? { ...base, ...edits } : null
  const dirty = !!base && (Object.keys(edits) as (keyof EventFull)[]).some((k) => edits[k] !== base[k])
  const calendar: CalendarSubscription | undefined = list.find((c) => c.id === chosen) ?? fallback
  const timesInvalid = !!form && pointsOf(form) === 'invalid'
  const invalid = !form || timesInvalid || form.title.trim() === ''

  const set = (patch: Partial<EventFull>) => setEdits((e) => ({ ...e, ...patch }))
  const setAllDay = (on: boolean) => {
    if (!form) return
    const next = switchAllDay(form, on)
    set({ allDay: next.allDay, startTime: next.startTime, endTime: next.endTime, endDate: next.endDate })
  }

  const save = async () => {
    if (!form || invalid) return
    if (request.mode === 'create') {
      const draft = calendar && draftFromFull(form, calendar)
      if (!draft) return
      close()
      await addEvent(draft)
      return
    }
    if (!ev) return
    const change = eventFullChange(ev, form, viewerZone, loaded)
    close()
    if (change && change !== 'invalid') await changeEvent(ev, change, Object.keys(change).join() === 'summary' ? 'renamed' : 'updated', loaded !== null ? { description: loaded } : undefined)
  }

  /** Duplicate: the editor again, in create mode, with these fields in this event's calendar (else the default one). */
  const duplicate = () => {
    if (!ev || !form) return
    const cal = list.find((c) => c.id === ev.subscriptionId) ?? fallback
    const draft = cal && (draftFromFull(form, cal) ?? draftFromEvent(ev, loaded ?? '', cal, viewerZone))
    if (draft) openEventEditor({ mode: 'create', draft })
  }
  const remove = async () => {
    if (!ev) return
    close()
    await deleteEvent(ev)
  }

  return {
    form, base, set, setAllDay, dirty, invalid, timesInvalid, save, duplicate, remove, ev, eventCalendar, description, loaded, gone: editing && detail.event === null,
    calendars: list, calendar, chooseCalendar: setChosen, viewerZone, floating: !!base && base.zone === '' && !!ev,
  }
}
