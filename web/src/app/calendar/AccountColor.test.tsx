// @vitest-environment happy-dom
import 'fake-indexeddb/auto'
import { beforeEach, expect, it } from 'vitest'
import { db } from '../../lib/db'
import { defaultAccountColor } from '../../lib/calendarColors'
import { useCalendarSubscriptions } from '../../lib/liveData'
import { click, render, waitFor } from '../../ui/test-utils'
import { AccountColor } from './AccountColor'

beforeEach(async () => { await db.delete(); await db.open() })
function Preview() {
  const subs = useCalendarSubscriptions()
  return <><AccountColor id="work" name="Work" /><output>{subs.map((s) => s.color).join(',')}</output></>
}

it('chooses and resets an account hue offline and updates the shared subscription read', async () => {
  await db.calendarSubscriptions.put({ id: 'work-cal', connectionId: 'work', name: 'Work', color: '#06b6d4', enabled: true, lastSuccessAt: null })
  const r = await render(<Preview />)
  await waitFor(() => r.container.querySelector('output')?.textContent === defaultAccountColor('work'))
  const open = () => r.container.querySelector<HTMLButtonElement>('[aria-label="Colour for Work"]')!
  await click(open())
  await click(document.querySelector<HTMLButtonElement>('[aria-label="Colour yellow"]')!)
  await waitFor(() => r.container.querySelector('output')?.textContent === '#eab308')
  expect((await db.calendarSubscriptions.get('work-cal'))?.color).toBe('#06b6d4')
  await click(open())
  await click(document.querySelector<HTMLButtonElement>('[aria-label="Automatic"]')!)
  await waitFor(() => r.container.querySelector('output')?.textContent === defaultAccountColor('work'))
  await r.unmount()
})
