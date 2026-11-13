// The editor's repeat (OpenSpec change calendar-event-series D12): presets in a Combobox, Custom with an interval,
// the unit, weekdays and the monthly choice, and how it ends. A rule the form cannot say is read-only text. In the
// editor's lazy chunk with the form.
import { useState } from 'react'
import { addDaysISO } from '../../lib/dates'
import { defaultRepeat, presetOf, repeatForPreset, repeatSummary, REPEAT_PRESETS, type RepeatPreset, WEEKDAY_CODES, weekdayOf, withFreq } from '../../lib/eventRepeat'
import { t } from '../../lib/i18n'
import type { CalendarRepeat } from '../../lib/types'
import { Combobox, Input, NumberInput, SegmentedControl, Skeleton } from '../../ui'

export interface RepeatFieldProps {
  value: CalendarRepeat | null
  onChange: (repeat: CalendarRepeat | null) => void
  /** The event's first day: the weekday, the day of the month and the year's date the presets mean. */
  startDate: string
  /** A rule the form cannot say: shown as text, never changed. */
  custom: boolean
  /** `loading` and `unavailable` are for a rule the calendar has not given yet (a Microsoft series, before its detail). */
  state: 'ready' | 'loading' | 'unavailable'
  /** An existing Microsoft series cannot stop repeating. */
  canStop: boolean
  /** The new rule resets events changed one by one: say so beside the field. */
  resets: boolean
}

interface PresetItem { id: RepeatPreset; label: string }
type Freq = CalendarRepeat['freq']
type EndsType = CalendarRepeat['ends']['type']

const presetLabel = (id: RepeatPreset, startDate: string): string => {
  if (id === 'none') return t('Does not repeat')
  if (id === 'custom') return t('Custom…')
  return repeatSummary(repeatForPreset(id)!, startDate)
}

function Interval({ value, startDate, onChange }: { value: CalendarRepeat; startDate: string; onChange: (r: CalendarRepeat) => void }) {
  const units: { id: Freq; label: string }[] = [{ id: 'daily', label: t('Day') }, { id: 'weekly', label: t('Week') }, { id: 'monthly', label: t('Month') }, { id: 'yearly', label: t('Year') }]
  const days = value.days ?? []
  return (
    <div className="space-y-2" data-event-form-repeat-custom-fields>
      <div className="flex flex-wrap items-end gap-2">
        <NumberInput label={t('Every')} value={value.interval} min={1} max={99} className="w-28" onChange={(n) => { if (n !== null) onChange({ ...value, interval: n }) }} />
        <SegmentedControl<Freq> label={t('Repeat unit')} options={units} value={value.freq} onChange={(f) => onChange(withFreq(value, f, startDate))} />
      </div>
      {value.freq === 'weekly' && (
        <div role="group" aria-label={t('Repeat on')} className="flex flex-wrap gap-1" data-event-form-repeat-days>
          {WEEKDAY_CODES.map((code) => {
            const on = days.includes(code)
            return (
              <button key={code} type="button" aria-pressed={on} data-repeat-day={code} className={`ui-btn ui-btn--sm ${on ? 'ui-btn--primary' : 'ui-btn--secondary'}`}
                onClick={() => { const next = on ? days.filter((d) => d !== code) : [...days, code]; if (next.length) onChange({ ...value, days: next }) }}>
                {weekdayOf(code).slice(0, 3)}
              </button>
            )
          })}
        </div>
      )}
      {value.freq === 'monthly' && (
        <SegmentedControl<'monthday' | 'weekday'> label={t('Repeat by')} options={[{ id: 'monthday', label: t('Day of the month') }, { id: 'weekday', label: t('Weekday of the month') }]}
          value={value.by ?? 'monthday'} onChange={(by) => onChange({ ...value, by })} wrap />
      )}
    </div>
  )
}

function Ends({ value, startDate, onChange }: { value: CalendarRepeat; startDate: string; onChange: (r: CalendarRepeat) => void }) {
  const ends = value.ends
  const pick = (type: EndsType): void => onChange({ ...value, ends: type === 'never' ? { type } : type === 'on' ? { type, date: addDaysISO(startDate, 30) } : { type, count: 10 } })
  return (
    <div className="space-y-1.5" data-event-form-repeat-ends>
      <div className="flex flex-wrap items-center gap-2">
        <span className="text-meta text-muted">{t('Ends')}</span>
        <SegmentedControl<EndsType> label={t('Ends')} options={[{ id: 'never', label: t('Never') }, { id: 'on', label: t('On a date') }, { id: 'after', label: t('After') }]} value={ends.type} onChange={pick} />
      </div>
      {ends.type === 'on' && <Input type="date" aria-label={t('Repeat until')} data-event-form-repeat-until value={ends.date} min={startDate} onChange={(e) => { if (e.target.value) onChange({ ...value, ends: { type: 'on', date: e.target.value } }) }} />}
      {ends.type === 'after' && <NumberInput label={t('Times')} hideLabel value={ends.count} min={1} max={999} unit={t('times')} className="w-32" onChange={(n) => { if (n !== null) onChange({ ...value, ends: { type: 'after', count: n } }) }} />}
    </div>
  )
}

export function EventRepeatField({ value, onChange, startDate, custom, state, canStop, resets }: RepeatFieldProps) {
  const [forced, setForced] = useState(false) // Custom was chosen: its fields stay open even when the rule reads as a preset
  const preset = presetOf(value, startDate)
  const showCustom = !!value && (forced || preset === 'custom')
  const items: PresetItem[] = REPEAT_PRESETS.filter((id) => id !== 'none' || canStop).map((id) => ({ id, label: presetLabel(id, startDate) }))
  const shown = showCustom ? (preset === 'custom' && value ? repeatSummary({ ...value, ends: { type: 'never' } }, startDate) : t('Custom…')) : presetLabel(preset, startDate)
  const pick = (id: RepeatPreset) => {
    setForced(id === 'custom')
    onChange(id === 'custom' ? (value ?? defaultRepeat(startDate)) : repeatForPreset(id, value?.ends))
  }
  return (
    <div className="space-y-1.5" data-event-form-repeat>
      <p className="text-body font-medium text-default">{t('Repeat')}</p>
      {custom && <p className="text-body text-default" data-event-form-repeat-read-only>{t('Custom repeat')}</p>}
      {!custom && state === 'loading' && <Skeleton shape="line" count={1} label={t('Loading the repeat')} />}
      {!custom && state === 'unavailable' && <p className="text-meta text-muted">{t('The repeat could not be loaded, so it is left as it is.')}</p>}
      {!custom && state === 'ready' && (
        <>
          <Combobox<PresetItem> label={t('Repeat')} placeholder={shown} source={items} getKey={(i) => i.id} getLabel={(i) => i.label} selected={[showCustom ? 'custom' : preset]} onPick={(i) => pick(i.id)} />
          {showCustom && value && <Interval value={value} startDate={startDate} onChange={onChange} />}
          {value && <Ends value={value} startDate={startDate} onChange={onChange} />}
          {resets && <p className="text-meta text-muted" data-event-form-repeat-resets>{t('Changing the repeat resets events you changed individually')}</p>}
        </>
      )}
    </div>
  )
}
