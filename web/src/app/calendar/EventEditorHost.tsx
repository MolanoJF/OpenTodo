// Calendar previews stay lightweight; full event pages render through Shell.
import { CalendarTaskPreviewHost } from './TaskPreviewHost'
export const EVENT_EDITOR_PANEL = 'event-editor'
export function EventEditorHost() { return <CalendarTaskPreviewHost /> }
