import type { ReactNode } from 'react'
import { Icon, type IconName } from '../../ui'

export function PreviewFact({ icon, children }: { icon: IconName; children: ReactNode }) {
  return <div className="flex min-w-0 items-start gap-2 py-1 text-meta">
    <Icon name={icon} size="sm" className="mt-0.5 shrink-0 text-muted" />
    <div className="min-w-0 flex-1 break-words">{children}</div>
  </div>
}
