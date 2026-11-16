// Calendar source colors: device preferences never change provider data or ops.
import { HUES, hueFor } from '../ui/tokens/palette'
import { getMeta, setMeta } from './db'
import type { CalendarSubscription } from './types'

export const accountColorKey = (id: string): string => `calendar.accountColor.${id}`

/** Stable across reloads and independent of which calendars are currently shown. */
export function defaultAccountColor(id: string): string {
  const hues = HUES.filter((h) => h.name !== 'slate')
  let hash = 0
  for (const c of id) hash = (Math.imul(hash, 31) + c.charCodeAt(0)) >>> 0
  return hues[hash % hues.length].hex
}

export function accountColor(id: string, stored: unknown): string {
  return typeof stored === 'string' && HUES.some((h) => h.hex === stored) ? stored : defaultAccountColor(id)
}

export async function saveAccountColor(id: string, color: string | null): Promise<void> {
  await setMeta(accountColorKey(id), color === null ? null : HUES.find((h) => h.name === hueFor(color))!.hex)
}

/** A display-only projection. The original subscription and its provider color stay intact. */
export async function coloredSubscriptions(subs: CalendarSubscription[]): Promise<CalendarSubscription[]> {
  const ids = [...new Set(subs.flatMap((s) => s.connectionId ? [s.connectionId] : []))]
  const colors = new Map(await Promise.all(ids.map(async (id) => [id, accountColor(id, await getMeta<unknown>(accountColorKey(id), null))] as const)))
  return subs.map((s) => s.connectionId ? { ...s, color: colors.get(s.connectionId)! } : s)
}
