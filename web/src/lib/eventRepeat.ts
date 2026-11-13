// The repeat form of the event editor (OpenSpec change calendar-event-series D3, D8, D11): presets, the
// words, and the conversions between the form and the REST `repeat` object. Pure, and no RRULE text on the
// client: the server writes and reads the rule, the client only edits this form.
import { formatDateParts, weekdayName } from './dates'
import { t } from './i18n'
import type { CalendarRepeat, CalendarSubscription } from './types'

type Freq = CalendarRepeat['freq']
type Ends = CalendarRepeat['ends']

/** The presets of the repeat field, in order; Custom opens the interval, weekday and end fields. */
export const REPEAT_PRESETS = ['none', 'daily', 'weekdays', 'weekly', 'monthday', 'weekday', 'yearly', 'custom'] as const
export type RepeatPreset = (typeof REPEAT_PRESETS)[number]

/** `mo` to `su`, in week order (Monday first, as the REST object lists them). */
export const WEEKDAY_CODES = ['mo', 'tu', 'we', 'th', 'fr', 'sa', 'su'] as const
const WORKDAYS = WEEKDAY_CODES.slice(0, 5)
const NEVER: Ends = { type: 'never' }

const inWeek = (days: readonly string[] | undefined): string[] =>
  [...(days ?? [])].sort((a, b) => WEEKDAY_CODES.indexOf(a as 'mo') - WEEKDAY_CODES.indexOf(b as 'mo'))
const dayOf = (iso: string) => new Date(Date.UTC(+iso.slice(0, 4), +iso.slice(5, 7) - 1, +iso.slice(8, 10)))
/** The weekday code of a date. */
export const weekdayCode = (iso: string): string => WEEKDAY_CODES[(dayOf(iso).getUTCDay() + 6) % 7]
/** The name of a weekday code, in the date locale. */
export const weekdayOf = (code: string): string => weekdayName((WEEKDAY_CODES.indexOf(code as 'mo') + 1) % 7)
/** Which weekday of its month a date is: 1 to 4, and 5 for the last. */
const nthOf = (iso: string): number => Math.min(5, Math.floor((+iso.slice(8, 10) - 1) / 7) + 1)

/** Microsoft calendars take one reminder and no "stop repeating" (calendar-event-series D3, open question 2). */
export const isMicrosoftCalendar = (sub: Pick<CalendarSubscription, 'maxReminders'> | undefined): boolean => sub?.maxReminders === 1

/** The rule a preset means; `ends` carries over when the preset changes. Custom is not a rule of its own. */
export function repeatForPreset(preset: RepeatPreset, ends: Ends = NEVER): CalendarRepeat | null {
  switch (preset) {
    case 'none': case 'custom': return null
    case 'daily': return { freq: 'daily', interval: 1, days: [], ends }
    case 'weekdays': return { freq: 'weekly', interval: 1, days: [...WORKDAYS], ends }
    case 'weekly': return { freq: 'weekly', interval: 1, days: [], ends }
    case 'monthday': return { freq: 'monthly', interval: 1, days: [], by: 'monthday', ends }
    case 'weekday': return { freq: 'monthly', interval: 1, days: [], by: 'weekday', ends }
    default: return { freq: 'yearly', interval: 1, days: [], ends }
  }
}

/** The preset a rule reads as; `custom` when no preset says exactly the same (ends do not count). */
export function presetOf(repeat: CalendarRepeat | null, startDate: string): RepeatPreset {
  if (!repeat) return 'none'
  if (repeat.interval !== 1) return 'custom'
  const days = inWeek(repeat.days)
  switch (repeat.freq) {
    case 'daily': return 'daily'
    case 'weekly':
      if (days.length === 0 || (days.length === 1 && days[0] === weekdayCode(startDate))) return 'weekly'
      return days.join() === WORKDAYS.join() ? 'weekdays' : 'custom'
    case 'monthly': return repeat.by === 'weekday' ? 'weekday' : 'monthday'
    default: return 'yearly'
  }
}

/** The start values of Custom when the event does not repeat yet: every week on the start's weekday. */
export const defaultRepeat = (startDate: string): CalendarRepeat => ({ freq: 'weekly', interval: 1, days: [weekdayCode(startDate)], ends: NEVER })

/** Another unit for a custom rule: what the new unit does not use is dropped, and what it needs gets a default. */
export function withFreq(r: CalendarRepeat, freq: Freq, startDate: string): CalendarRepeat {
  const days = freq === 'weekly' ? (r.days?.length ? r.days : [weekdayCode(startDate)]) : []
  return { freq, interval: r.interval, days, ...(freq === 'monthly' ? { by: r.by ?? 'monthday' } : {}), ends: r.ends }
}

/** The rule as a request carries it: `days` always (in week order), `by` for monthly only. */
export function restRepeat(r: CalendarRepeat): CalendarRepeat {
  return { freq: r.freq, interval: r.interval, days: r.freq === 'weekly' ? inWeek(r.days) : [], ...(r.freq === 'monthly' ? { by: r.by ?? 'monthday' } : {}), ends: r.ends }
}

/** Whether two rules say the same (the order of the days and an absent `days` do not matter). */
export const sameRepeat = (a: CalendarRepeat | null, b: CalendarRepeat | null): boolean =>
  a === b || (!!a && !!b && JSON.stringify(restRepeat(a)) === JSON.stringify(restRepeat(b)))

function ordinalRule(n: number, weekday: string): string {
  switch (n) {
    case 1: return t('Every month on the first {weekday}', { weekday })
    case 2: return t('Every month on the second {weekday}', { weekday })
    case 3: return t('Every month on the third {weekday}', { weekday })
    case 4: return t('Every month on the fourth {weekday}', { weekday })
    default: return t('Every month on the last {weekday}', { weekday })
  }
}

/** How a rule ends, in words; '' for never. */
export function endsLabel(ends: Ends): string {
  if (ends.type === 'on') return t('until {date}', { date: formatDateParts(ends.date, { month: 'long', day: 'numeric' }) })
  if (ends.type === 'after') return t('{n, plural, one {once} other {# times}}', { n: ends.count })
  return ''
}

/** A rule in words, for an event that starts on `startDate` ("Every week on Thursday, 10 times"). */
export function repeatSummary(r: CalendarRepeat, startDate: string): string {
  const n = r.interval
  let rule: string
  if (r.freq === 'daily') rule = n === 1 ? t('Every day') : t('Every {n} days', { n })
  else if (r.freq === 'weekly') {
    if (n === 1 && presetOf({ ...r, ends: NEVER }, startDate) === 'weekdays') rule = t('Every weekday')
    else {
      const days = inWeek(r.days)
      const list = (days.length ? days : [weekdayCode(startDate)]).map(weekdayOf).join(', ')
      rule = n === 1 ? t('Every week on {days}', { days: list }) : t('Every {n} weeks on {days}', { n, days: list })
    }
  } else if (r.freq === 'monthly') {
    if (n !== 1) rule = t('Every {n} months', { n })
    else rule = r.by === 'weekday' ? ordinalRule(nthOf(startDate), weekdayOf(weekdayCode(startDate))) : t('Every month on day {day}', { day: +startDate.slice(8, 10) })
  } else rule = n === 1 ? t('Every year on {date}', { date: formatDateParts(startDate, { month: 'long', day: 'numeric' }) }) : t('Every {n} years', { n })
  const ends = endsLabel(r.ends)
  return ends ? `${rule}, ${ends}` : rule
}
