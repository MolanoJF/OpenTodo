import type { Meta, StoryObj } from '@storybook/react-vite'
import { useState } from 'react'
import { eventFullOf, switchAllDay, type EventFull } from '../../lib/eventFull'
import type { CalendarEvent, CalendarSubscription } from '../../lib/types'
import { EventForm } from './EventForm'
import type { DescriptionState } from './useEventDetail'

// The full event editor's fields (OpenSpec change calendar-event-editor, design D11): creating in a chosen calendar,
// editing an event kept in another zone (with the time in the viewer's zone beneath), an all-day event, and the
// description's three states beside the rich-text read-only one. The panel and the sheet frame the same form.
const meta: Meta = { title: 'Screens/Event editor' }
export default meta

const VIEWER = 'Europe/Lisbon'
const event: CalendarEvent = {
  id: 'e1', subscriptionId: 'work', uid: 'u1', summary: 'Design review', location: 'Room 4', allDay: false, free: false,
  start: { date: '2026-11-13', time: '10:00', tz: 'Europe/Berlin' }, end: { date: '2026-11-13', time: '11:00', tz: 'Europe/Berlin' },
}
const calendars: CalendarSubscription[] = ['Work', 'Home', 'Family'].map((name, i) => ({
  id: name.toLowerCase(), name, color: ['#2563eb', '#16a34a', '#d97706'][i], enabled: true, lastSuccessAt: 1, managed: true, connectionId: 'c1', calendarUrl: `https://cal.example/${name}/`,
  editEvents: true, caps: { create: true, update: true, delete: true },
}))

function Story({ initial, description = { status: 'ready', text: 'Bring the draft and the numbers.', editable: true }, creating = false }: { initial: EventFull; description?: DescriptionState; creating?: boolean }) {
  const [form, setForm] = useState(initial)
  return (
    <div className="w-[22rem] bg-surface p-4">
      <EventForm form={form} viewerZone={VIEWER} floating={false} description={description} invalid={false} onSubmit={() => {}}
        set={(p) => setForm((f) => ({ ...f, ...p }))} setAllDay={(on) => setForm((f) => switchAllDay(f, on))}
        onTask={creating ? () => {} : undefined}
        picker={creating ? { calendars, value: 'work', onChange: () => {} } : undefined} calendarLabel={creating ? undefined : calendars[0]} />
    </div>
  )
}

const created: EventFull = { title: '', allDay: false, startDate: '2026-11-13', startTime: '14:00', endDate: '2026-11-13', endTime: '15:30', zone: VIEWER, location: '', description: '', free: false }

export const Create: StoryObj = { render: () => <Story initial={created} description={{ status: 'ready', text: '', editable: true }} creating /> }
export const EditInAnotherZone: StoryObj = { render: () => <Story initial={eventFullOf(event, VIEWER, 'Bring the draft and the numbers.')} /> }
export const AllDay: StoryObj = { render: () => <Story initial={eventFullOf({ ...event, allDay: true, start: { date: '2026-11-16' }, end: { date: '2026-11-19' } }, VIEWER)} /> }
export const DescriptionLoading: StoryObj = { render: () => <Story initial={eventFullOf(event, VIEWER)} description={{ status: 'loading' }} /> }
export const DescriptionOffline: StoryObj = { render: () => <Story initial={eventFullOf(event, VIEWER)} description={{ status: 'unavailable', reason: 'offline' }} /> }
export const DescriptionRichText: StoryObj = { render: () => <Story initial={eventFullOf(event, VIEWER, 'Formatted agenda')} description={{ status: 'ready', text: 'Formatted agenda', editable: false }} /> }
