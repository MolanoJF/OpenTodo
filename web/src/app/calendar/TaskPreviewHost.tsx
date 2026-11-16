import { lazy, Suspense, useEffect } from 'react'
import { taskPreview } from './taskPreviewStore'
const TaskPreview = lazy(() => import('./TaskPreview'))

export function CalendarTaskPreviewHost() {
  const request = taskPreview.use()
  useEffect(() => () => taskPreview.set(null), [])
  return request ? <Suspense fallback={null}><TaskPreview request={request} onClose={() => taskPreview.set(null)} /></Suspense> : null
}
