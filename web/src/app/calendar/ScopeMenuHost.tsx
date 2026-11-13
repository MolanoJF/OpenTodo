// Where the scope menu and the "Tell N guests?" dialog open (OpenSpec changes calendar-event-series D12 and
// calendar-event-guests D13): mounted once by the calendar views, it follows the request stores and loads each
// overlay itself only when a request is open, so the first load carries none of them.
import { lazy, Suspense } from 'react'
import { useScopeRequest } from './eventScopeStore'
import { useNotifyRequest } from './notifyStore'

const ScopeMenu = lazy(() => import('./ScopeMenu'))
const NotifyGuestsDialog = lazy(() => import('./NotifyGuestsDialog'))

export function ScopeMenuHost() {
  const req = useScopeRequest()
  const notify = useNotifyRequest()
  return (
    <>
      {req && <Suspense fallback={null}><ScopeMenu key={req.key} req={req} /></Suspense>}
      {notify && <Suspense fallback={null}><NotifyGuestsDialog key={notify.key} req={notify} /></Suspense>}
    </>
  )
}
