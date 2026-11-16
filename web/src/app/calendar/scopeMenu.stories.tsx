import type { Meta, StoryObj } from '@storybook/react-vite'
import type { CalendarEvent, CalendarRepeat } from '../../lib/types'
import type { ScopeRequest } from './eventScopeStore'
import ScopeMenu from './ScopeMenu'

// "Which events?" (OpenSpec change calendar-event-series, design D12): the menu a drag, a key press, a rename or a Save on an
// occurrence of a repeating event opens, controlled and open here at a fixed place. The first occurrence has no "this and
// following", a day move of a monthly series disables "all events" with its reason, a repeat change offers two and
// says what it resets, and a delete asks the same.
const meta: Meta = { title: 'Screens/Scope menu', parameters: { layout: 'fullscreen' } }
export default meta

const at = (date: string, time: string) => ({ date, time, tz: 'Europe/Berlin' })
const daily: CalendarRepeat = { freq: 'daily', interval: 1, days: [], ends: { type: 'never' } }
const monthly: CalendarRepeat = { freq: 'monthly', interval: 1, by: 'monthday', ends: { type: 'never' } }
const occurrence = (series: Partial<NonNullable<CalendarEvent['series']>> = {}): CalendarEvent => ({
  id: 'o2', subscriptionId: 's1', uid: 'u', summary: 'Morning routine', location: '', allDay: false, free: false, rev: 'r1',
  start: at('2026-11-12', '07:00'), end: at('2026-11-12', '08:30'), access: { update: true, delete: true }, series: { id: 'ser', repeat: daily, first: false, ...series },
})
const moved = { start: at('2026-11-12', '09:00'), end: at('2026-11-12', '10:30'), allDay: false }
const request = (event: CalendarEvent, change: ScopeRequest['change'], resets?: ScopeRequest['resets']): ScopeRequest => ({ key: 1, event, change, at: { x: 40, y: 24 }, resets, resolve: () => {} })

const Frame = ({ req }: { req: ScopeRequest }) => <div className="h-72 bg-surface"><ScopeMenu req={req} /></div>

export const ThreeScopes: StoryObj = { render: () => <Frame req={request(occurrence(), moved)} /> }
export const FirstOccurrence: StoryObj = { render: () => <Frame req={request(occurrence({ first: true }), moved)} /> }
export const DayMoveOfAMonthlySeries: StoryObj = { render: () => <Frame req={request(occurrence({ repeat: monthly }), { ...moved, start: at('2026-11-13', '09:00'), end: at('2026-11-13', '10:30') })} /> }
export const RepeatChange: StoryObj = { render: () => <Frame req={request(occurrence(), { repeat: { freq: 'weekly', interval: 1, days: ['mo'], ends: { type: 'never' } } }, 'repeat')} /> }
export const Delete: StoryObj = { render: () => <Frame req={request(occurrence(), 'delete')} /> }
