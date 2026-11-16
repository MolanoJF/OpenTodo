import { click, waitFor } from '../ui/test-utils'

export async function calendarControl(root: ParentNode, selector: string): Promise<HTMLButtonElement> {
  if (!document.querySelector(selector)) {
    await click(root.querySelector('[aria-label="More actions"]')!)
    await waitFor(() => !!document.querySelector(selector))
  }
  return document.querySelector<HTMLButtonElement>(selector)!
}

export async function chooseCalendarMode(root: ParentNode, label: string) {
  await click(root.querySelector('[data-calendar-range]')!)
  await waitFor(() => !!document.querySelector('[data-calendar-mode]'))
  await click([...document.querySelectorAll('[data-calendar-mode]')].find((el) => el.textContent === label)!)
}
