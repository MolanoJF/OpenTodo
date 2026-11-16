import { useRef, useState } from 'react'
import { accountColor, accountColorKey, saveAccountColor } from '../../lib/calendarColors'
import { useMeta } from '../../lib/localData'
import { t } from '../../lib/i18n'
import { Button, hueFor, Popover } from '../../ui'
import { act } from '../actionError'
import { ColourSwatches } from '../menus/projectAppearance'

/** Local appearance remains editable when the remote account is offline. */
export function AccountColor({ id, name }: { id: string; name: string }) {
  const stored = useMeta<unknown>(accountColorKey(id), null)
  const color = accountColor(id, stored)
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLButtonElement>(null)
  const choose = (next: string | null) => { act(saveAccountColor(id, next)); setOpen(false) }
  return <>
    <Button ref={ref} size="sm" variant="ghost" aria-label={t('Colour for {name}', { name })}
      aria-haspopup="dialog" aria-expanded={open} onClick={() => setOpen(!open)}>
      <span aria-hidden data-hue={hueFor(color)} className="size-3 shrink-0 rounded-full bg-hue-solid" />{t('Colour')}
    </Button>
    <Popover open={open} onClose={() => setOpen(false)} anchor={ref} label={t('Colour for {name}', { name })} className="p-3" width="w-72">
      <p className="text-body font-medium text-default">{t('Colour for {name}', { name })}</p>
      <p className="mt-1 text-meta text-muted">{t('Applies to this account’s events on this device.')}</p>
      <ColourSwatches color={stored ? color : undefined} onPick={choose} onDefault={() => choose(null)} />
    </Popover>
  </>
}
