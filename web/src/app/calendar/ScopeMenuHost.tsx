// Where the scope menu opens (OpenSpec change calendar-event-series D12): mounted once by the calendar views, it
// follows the scope request store and loads the menu itself only when a request is open, so the first load carries none of it.
import { lazy, Suspense } from 'react'
import { useScopeRequest } from './eventScopeStore'

const ScopeMenu = lazy(() => import('./ScopeMenu'))

export function ScopeMenuHost() {
  const req = useScopeRequest()
  return req ? <Suspense fallback={null}><ScopeMenu key={req.key} req={req} /></Suspense> : null
}
