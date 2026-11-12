// The calendars an event can be created in, and the device's choices about them (OpenSpec change
// calendar-event-editor D1): small and read on first load (the add menu, the palette), so the code that makes
// events (lib/eventCreate.ts) stays out of it. Nothing here is synced.
import { useLiveQuery } from 'dexie-react-hooks'
import { db, getMeta, setMeta } from './db'
import type { CalendarSubscription } from './types'

const KIND_KEY = 'calendar.newKind'
const CALENDAR_KEY = 'calendar.eventCalendar'
export type NewKind = 'task' | 'event'

/** A writable calendar (D1): a managed calendar whose connection allows edits and whose account allows creating. */
export const isWritableCalendar = (s: CalendarSubscription): boolean => !!s.connectionId && !!s.calendarUrl && !!s.editEvents && !!s.caps?.create && s.enabled !== false

/** The writable calendars, by name (the cache does not keep the order the server serves them in). */
export function writableCalendars(subs: readonly CalendarSubscription[]): CalendarSubscription[] {
  return subs.filter(isWritableCalendar).sort((a, b) => a.name.localeCompare(b.name) || a.id.localeCompare(b.id))
}

/** The calendar new events go to: the remembered one while it is still writable, else the first. */
export function defaultEventCalendar(list: readonly CalendarSubscription[], remembered: string | null | undefined): CalendarSubscription | undefined {
  return list.find((s) => s.id === remembered) ?? list[0]
}

/** The last Task or Event choice; an `event` is ignored while there is no writable calendar. */
export function newKind(list: readonly CalendarSubscription[], remembered: string | null | undefined): NewKind {
  return remembered === 'event' && list.length > 0 ? 'event' : 'task'
}

export const getNewKind = (): Promise<string | null> => getMeta<string | null>(KIND_KEY, null)
export const setNewKind = (kind: NewKind): Promise<void> => setMeta(KIND_KEY, kind)
export const getEventCalendar = (): Promise<string | null> => getMeta<string | null>(CALENDAR_KEY, null)
export const setEventCalendar = (id: string): Promise<void> => setMeta(CALENDAR_KEY, id)

const NONE: CalendarSubscription[] = []

/** The writable calendars, live (empty until the cache is read): the Event choice appears only when this is not empty. */
export function useWritableCalendars(): CalendarSubscription[] {
  return useLiveQuery(async () => writableCalendars(await db.calendarSubscriptions.toArray()), [], NONE) ?? NONE
}

/** The writable calendars with the default one and the remembered Task or Event choice. */
export function useEventCreation(): { list: CalendarSubscription[]; calendar: CalendarSubscription | undefined; kind: NewKind } {
  const list = useWritableCalendars()
  const remembered = useLiveQuery(async () => ({ kind: await getNewKind(), calendar: await getEventCalendar() }), [], { kind: null, calendar: null })
  return { list, calendar: defaultEventCalendar(list, remembered.calendar), kind: newKind(list, remembered.kind) }
}

/** One cached calendar (an event's), live. */
export function useCalendarSubscription(id: string | undefined): CalendarSubscription | undefined {
  return useLiveQuery(async () => (id ? db.calendarSubscriptions.get(id) : undefined), [id])
}
