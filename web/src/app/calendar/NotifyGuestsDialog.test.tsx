// @vitest-environment happy-dom
// "Tell N guests?" (OpenSpec change calendar-event-guests, task 5.3, design D8, D13, D17): the words, focus on Cancel, Escape
// and Cancel answer no, the confirm answers yes, the question is announced, and nothing is on screen without a request.
import { act } from 'react'
import { describe, expect, it } from 'vitest'
import { notifyPrompt } from '../../lib/eventNotify'
import { click, key, render, waitFor } from '../../ui/test-utils'
import { askNotify, currentNotifyRequest } from './notifyStore'
import { ScopeMenuHost } from './ScopeMenuHost'

const prompt = notifyPrompt({ access: { update: true, delete: true, notifies: true }, guests: { count: 3, organizer: true, canRespond: false } }, 'change')!
const dialog = () => document.querySelector('[data-notify-guests]')
const cancel = () => document.querySelector<HTMLButtonElement>('[data-notify-cancel]')!
const confirm = () => document.querySelector<HTMLButtonElement>('[data-notify-confirm]')!

async function ask() {
  let result: boolean | 'pending' = 'pending'
  await act(async () => { void askNotify(prompt).then((ok) => { result = ok }) })
  await waitFor(() => !!dialog(), 'the dialog')
  return () => result
}

describe('NotifyGuestsDialog', () => {
  it('shows nothing until a request is made', async () => {
    const r = await render(<ScopeMenuHost />)
    expect(dialog()).toBeNull()
    await r.unmount()
  })

  it('reads "Tell 3 guests?" with the one sentence, Save and tell guests and Cancel, and focus on Cancel', async () => {
    const r = await render(<ScopeMenuHost />)
    await ask()
    expect(document.querySelector('[role="dialog"]')!.textContent).toContain('Tell 3 guests?')
    expect(dialog()!.querySelector('p:not(.sr-only)')!.textContent).toBe("Outlook sends them this update. You can't turn that off.")
    expect([...dialog()!.querySelectorAll('button')].map((b) => b.textContent)).toEqual(['Cancel', 'Save and tell guests'])
    await waitFor(() => document.activeElement === cancel(), 'focus on Cancel')
    await r.unmount()
  })

  it('announces the question in a live region', async () => {
    const r = await render(<ScopeMenuHost />)
    await ask()
    expect(dialog()!.querySelector('[role="status"]')!.textContent).toContain('Tell 3 guests?')
    await r.unmount()
  })

  it('the confirm answers yes and closes', async () => {
    const r = await render(<ScopeMenuHost />)
    const result = await ask()
    await click(confirm())
    await waitFor(() => result() === true, 'the answer')
    await waitFor(() => !dialog(), 'the dialog closed')
    expect(currentNotifyRequest()).toBeNull()
    await r.unmount()
  })

  it('Cancel and Escape answer no', async () => {
    const r = await render(<ScopeMenuHost />)
    let result = await ask()
    await click(cancel())
    await waitFor(() => result() === false)
    await waitFor(() => !dialog())
    result = await ask()
    await key(document.activeElement!, 'Escape')
    await waitFor(() => result() === false, 'Escape cancels')
    await waitFor(() => !dialog())
    await r.unmount()
  })

  it('a second question cancels the first', async () => {
    const r = await render(<ScopeMenuHost />)
    const first = await ask()
    await act(async () => { void askNotify(prompt) })
    await waitFor(() => first() === false)
    await click(cancel())
    await r.unmount()
  })
})
