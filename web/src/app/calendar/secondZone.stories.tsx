import type { Meta, StoryObj } from '@storybook/react-vite'
import { GRID_PREFS_META_KEY } from '../../lib/calendar'
import { setMeta } from '../../lib/db'
import { CalendarView } from '../CalendarView'
import { seedCalendarWeek, withSeed } from '../stories/fixtures'

// Core screen: a second time zone (OpenSpec change calendar-time-zones). The seeded week at the frozen
// Wednesday noon with Bogota set as the second zone on this device: a second hour column beside the first,
// zone codes above both, and (in the Week) the same on the wide grid. The phone's Week is the agenda, which
// has no hour column.
async function seedSecondZone(): Promise<void> {
  await seedCalendarWeek()
  await setMeta(GRID_PREFS_META_KEY, { startHour: 6, endHour: 22, dayLength: 480, planDays: 14, countEvents: false, secondZone: 'America/Bogota' })
}

const meta: Meta = { title: 'Screens/Second time zone', ...withSeed(seedSecondZone) }
export default meta

const Column = ({ children }: { children: React.ReactNode }) => <div className="-m-4 h-[48rem] overflow-y-auto bg-canvas">{children}</div>

/** Day: the device's hours beside the same rows in Bogota, a code above each column. */
export const Day: StoryObj = {
  render: () => <Column><CalendarView mode="day" /></Column>,
}

/** Week: the two columns before Monday's. */
export const Week: StoryObj = {
  render: () => <Column><CalendarView mode="week" /></Column>,
}
