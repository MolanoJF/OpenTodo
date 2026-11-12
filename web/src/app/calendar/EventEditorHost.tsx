// Where the event editor opens (OpenSpec change calendar-event-editor D11): from 1024 px in the shell's side
// slot (replacing what is there; the planning list stays remembered open), below that as the overlay module's
// Dialog. Mounted once by the calendar views; the editor itself (EventPanel) is a lazy chunk, fetched only
// when a request is open.
import { lazy, Suspense, useEffect, useRef } from 'react'
import { resettable } from '../../lib/moduleState'
import { useMediaQuery } from '../../ui'
import { closePanel, openPanel, panelOccupant, SPLIT_QUERY, usePanelOccupantKey } from '../panel'
import { closeEventEditor, useEventEditor } from './eventEditorStore'

const EventPanel = lazy(() => import('./EventPanel').then((m) => ({ default: m.EventPanel })))

export const EVENT_EDITOR_PANEL = 'event-editor'
let hosts = 0
resettable('eventEditorHosts', () => { hosts = 0 })

/** The slot's content: follows the store, so a second request replaces the form in place. */
function EventEditorSlot() {
  const request = useEventEditor()
  return request ? <Suspense fallback={null}><EventPanel key={request.key} request={request} frame="panel" onClose={closeEventEditor} /></Suspense> : null
}

export function EventEditorHost() {
  const request = useEventEditor()
  const wide = useMediaQuery(SPLIT_QUERY)
  const occupant = usePanelOccupantKey()
  const opened = useRef(false)
  useEffect(() => {
    if (!wide) {
      if (occupant === EVENT_EDITOR_PANEL) closePanel() // the window narrowed: the Dialog takes over
      opened.current = false
      return
    }
    if (request && occupant !== EVENT_EDITOR_PANEL) {
      if (!opened.current) { opened.current = true; openPanel(EVENT_EDITOR_PANEL, () => <EventEditorSlot />) } // the slot is ours to take
      else { opened.current = false; closeEventEditor() } // something else took it (a route change): the editor is closed
    } else if (!request && occupant === EVENT_EDITOR_PANEL) {
      opened.current = false
      closePanel()
    }
  }, [request, wide, occupant])
  // Leaving the calendar closes the editor with it; moving between two calendar views (one host unmounts as the
  // next mounts) keeps a request that was opened in between, such as the palette's.
  useEffect(() => {
    hosts++
    return () => {
      hosts--
      const o = panelOccupant()
      if (o?.kind === 'custom' && o.key === EVENT_EDITOR_PANEL) closePanel()
      setTimeout(() => { if (hosts === 0) closeEventEditor() }, 0)
    }
  }, [])
  if (!request || wide) return null
  return <Suspense fallback={null}><EventPanel key={request.key} request={request} frame="dialog" onClose={closeEventEditor} /></Suspense>
}
