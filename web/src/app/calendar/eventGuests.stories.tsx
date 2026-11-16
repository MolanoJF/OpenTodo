import type { Meta, StoryObj } from '@storybook/react-vite'
import { useEffect, useState, type ReactNode } from 'react'
import { notifyPrompt } from '../../lib/eventNotify'
import type { CalendarEvent, CalendarSubscription, EventGuestList, GuestInput } from '../../lib/types'
import { EventGuests } from './EventGuests'
import { EventRespond } from './EventRespond'
import { EventVideoCall } from './EventVideoCall'
import { GuestsField } from './GuestsField'
import { JoinButton } from './JoinButton'
import NotifyGuestsDialog from './NotifyGuestsDialog'

// Guests, replies and join links of connected calendars' events (OpenSpec change calendar-event-guests, design D13, D17): the
// "Tell N guests?" dialog for a change, a delete and an invitation, the reply row where the organiser can be skipped and
// where not, the Guests field of the editor in its states, the guest list of a popover and the Join button for each
// service. Answers are words with an icon, never colour alone.
const meta: Meta = { title: 'Screens/Event guests' }
export default meta

const list: EventGuestList = {
  organizer: { name: 'Ana Silva', email: 'ana@example.com', you: false },
  guests: [
    { name: 'Ben Costa', email: 'ben@example.com', role: 'optional', response: 'accepted', you: false },
    { name: 'Cy Doe', email: 'cy@example.com', role: 'required', response: 'tentative', you: false },
    { name: 'Dee Park', email: 'dee@example.com', role: 'required', response: 'declined', you: false },
    { name: '', email: 'room-4@example.com', role: 'room', response: 'accepted', you: false },
    { name: 'Me', email: 'me@example.com', role: 'required', response: 'needs-action', you: true },
  ],
  count: 6, truncated: false,
}
const invite: CalendarEvent = {
  id: 'ev1', subscriptionId: 'work', uid: 'u1', summary: 'Design review', location: '', allDay: false, free: false, rev: 'r1',
  start: { date: '2026-11-13', time: '10:00', tz: 'Europe/Berlin' }, end: { date: '2026-11-13', time: '11:00', tz: 'Europe/Berlin' },
  access: { update: false, delete: false, reason: 'not_organizer' }, guests: { count: 5, you: 'needs-action', organizer: false, canRespond: true },
}
const outlook: CalendarSubscription = { id: 'work', name: 'Work', color: '#2563eb', enabled: true, lastSuccessAt: 1, managed: true, connectionId: 'c1', maxReminders: 1, guests: { respond: true, silent: true, edit: true }, caps: { create: true, update: true, delete: true, onlineMeeting: true } }
const google: CalendarSubscription = { ...outlook, id: 'me', maxReminders: undefined, calendarUrl: 'https://apidata.googleusercontent.com/caldav/v2/me@gmail.com/events/', guests: { respond: true, silent: false, edit: false } }

const Card = ({ children }: { children: ReactNode }) => <div className="w-72 space-y-3 bg-surface p-3 text-body">{children}</div>
const notifies = { access: { update: true, delete: true, notifies: true }, guests: { count: 3, organizer: true, canRespond: false } }

const dialog = (prompt: NonNullable<ReturnType<typeof notifyPrompt>>) => <div className="h-96 bg-surface"><NotifyGuestsDialog req={{ key: 1, prompt, resolve: () => {} }} /></div>
/** A change of a meeting the user organises: the one sentence, Save and tell guests, and Cancel (focus on Cancel). */
export const NotifyChange: StoryObj = { render: () => dialog(notifyPrompt(notifies, 'change')!) }
export const NotifyDelete: StoryObj = { render: () => dialog(notifyPrompt(notifies, 'delete')!) }
export const NotifyInvite: StoryObj = { render: () => dialog(notifyPrompt(null, 'invite', 2)!) }

/** Yes, Maybe and No; the organiser can be skipped on Outlook ("Tell the organizer"), not on Google. */
export const ReplyOutlook: StoryObj = { render: () => <Card><EventRespond ev={invite} sub={outlook} /></Card> }
export const ReplyGoogle: StoryObj = { render: () => <Card><EventRespond ev={{ ...invite, guests: { ...invite.guests!, you: 'tentative' } }} sub={google} /></Card> }

function Field({ editable, initial = { adds: [], removes: [] }, ...p }: { editable: boolean; online?: boolean; state?: 'loading' | 'ready' | 'unavailable'; reason?: 'offline' | 'failed'; withList?: boolean; initial?: { adds: GuestInput[]; removes: string[] } }) {
  const [v, setV] = useState(initial)
  return (
    <div className="w-[22rem] bg-surface p-4">
      <GuestsField state={p.state ?? 'ready'} reason={p.reason} list={p.withList === false ? null : list} editable={editable} online={p.online ?? true} adds={v.adds} removes={v.removes} onChange={setV} />
    </div>
  )
}
/** The editor's field: Remove per person, none for a room, a guest added with its role switch, the Add guest field. */
export const GuestsFieldEditable: StoryObj = { render: () => <Field editable initial={{ adds: [{ email: 'new.guest@example.com' }], removes: ['cy@example.com'] }} /> }
export const GuestsFieldReadOnly: StoryObj = { render: () => <Field editable={false} /> }
export const GuestsFieldNewEvent: StoryObj = { render: () => <Field editable withList={false} initial={{ adds: [{ email: 'ben@example.com', role: 'optional' }], removes: [] }} /> }
export const GuestsFieldOffline: StoryObj = { render: () => <Field editable online={false} /> }
export const GuestsFieldLoading: StoryObj = { render: () => <Field editable state="loading" withList={false} /> }

function Seeded({ children }: { children: ReactNode }) {
  const [ready, setReady] = useState(false)
  useEffect(() => {
    const real = window.fetch
    window.fetch = async (input, init) => (String(input).includes('/guests') ? new Response(JSON.stringify(list), { status: 200, headers: { 'Content-Type': 'application/json' } }) : real(input, init))
    setReady(true)
    return () => { window.fetch = real }
  }, [])
  return ready ? <>{children}</> : null
}
/** The popover's guests line, and its list once opened (answers as words and icons, a room as a room, the user named). */
export const GuestListCollapsed: StoryObj = { render: () => <Card><EventGuests ev={invite} /></Card> }
export const GuestListExpanded: StoryObj = {
  render: () => <Seeded><Card><EventGuests ev={invite} /></Card></Seeded>,
  play: async ({ canvasElement }) => { canvasElement.querySelector<HTMLElement>('[data-event-guests-toggle]')?.click() },
}

/** Join for each service, named by it with the host in its title; the editor's video call field beside it. */
export const JoinButtons: StoryObj = {
  render: () => (
    <Card>
      {(['meet', 'teams', 'zoom', 'webex', 'other'] as const).map((provider) => (
        <div key={provider}><JoinButton ev={{ summary: 'Design review', conference: { provider, url: `https://${provider === 'meet' ? 'meet.google.com' : provider === 'teams' ? 'teams.microsoft.com' : provider === 'zoom' ? 'acme.zoom.us' : provider === 'webex' ? 'acme.webex.com' : 'calls.example.org'}/j/123` } }} /></div>
      ))}
      <EventVideoCall available value={false} onChange={() => {}} />
    </Card>
  ),
}
