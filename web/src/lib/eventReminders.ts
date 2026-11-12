// The reminders of an event (OpenSpec change calendar-event-series D3, D12): whole minutes before the start, a
// negative value being after midnight of an all-day event. The calendar provider sends them, not OpenTodo.
import { formatTime } from './dates'
import { t } from './i18n'

const TIMED = [0, 5, 10, 15, 30, 60, 1440, 2880]
/** All-day presets at 09:00: on the day, and one, two and seven days before. */
const ALL_DAY = [-540, 900, 2340, 9540]

/** The presets the add menu offers: Microsoft cannot take a negative value, so it has none for an all-day event. */
export function reminderPresets(allDay: boolean, microsoft: boolean): number[] {
  if (!allDay) return [...TIMED]
  return microsoft ? [] : [...ALL_DAY]
}

const hhmm = (minutes: number) => `${String(Math.floor(minutes / 60)).padStart(2, '0')}:${String(minutes % 60).padStart(2, '0')}`

/** A reminder in words; for an all-day event as a time of the day ("1 day before at 9:00"). */
export function reminderLabel(minutes: number, allDay: boolean): string {
  if (allDay) {
    const days = Math.max(0, Math.ceil(minutes / 1440))
    const time = formatTime(hhmm(days * 1440 - minutes))
    return days === 0 ? t('On the day at {time}', { time }) : t('{n, plural, one {# day before at {time}} other {# days before at {time}}}', { n: days, time })
  }
  if (minutes === 0) return t('At the start')
  if (minutes % 1440 === 0) return t('{n, plural, one {# day before} other {# days before}}', { n: minutes / 1440 })
  if (minutes % 60 === 0) return t('{n, plural, one {# hour before} other {# hours before}}', { n: minutes / 60 })
  return t('{n, plural, one {# minute before} other {# minutes before}}', { n: minutes })
}

/** The list with `minutes` added, in order of time, once. */
export const addReminder = (list: readonly number[], minutes: number): number[] => (list.includes(minutes) ? [...list] : [...list, minutes].sort((a, b) => a - b))

/** The list without `minutes`. */
export const removeReminder = (list: readonly number[], minutes: number): number[] => list.filter((m) => m !== minutes)
