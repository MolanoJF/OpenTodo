import { createStore } from '../../lib/store'
import type { HueName } from '../../ui'

export interface TaskPreviewRequest { id: string; anchor: HTMLElement; hue?: HueName }
export const taskPreview = createStore<TaskPreviewRequest | null>('calendarTaskPreview', null)
export const openCalendarTaskPreview = (id: string, anchor: HTMLElement, hue?: HueName): void => taskPreview.set({ id, anchor, hue })
