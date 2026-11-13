// Answer an invitation (OpenSpec change calendar-event-guests D13, D17): Yes, Maybe, No as a labelled group with the
// current answer chosen, and "Tell the organizer" only where the calendar can skip that message. It is the one write a
// read-only event allows. The answer is sent at once and never queued: offline the group is disabled with the reason.
// For an occurrence of a repeating event it asks This event or All events with the scope menu.
import { lazy, Suspense, useRef, useState } from 'react'
import { sendNow } from '../../lib/eventSendNow'
import { replyScopes, yourAnswerWords } from '../../lib/eventGuests'
import { t } from '../../lib/i18n'
import { useOnline } from '../../lib/online'
import type { CalendarEvent, CalendarSubscription, EventScope, GuestAnswer } from '../../lib/types'
import { SegmentedControl, Switch } from '../../ui'
import type { ScopeRequest } from './eventScopeStore'

const ScopeMenu = lazy(() => import('./ScopeMenu'))

type Choice = GuestAnswer | 'none'
const options = (): { id: GuestAnswer; label: string }[] => [{ id: 'accepted', label: t('Yes') }, { id: 'tentative', label: t('Maybe') }, { id: 'declined', label: t('No') }]

export function EventRespond({ ev, sub }: { ev: CalendarEvent; sub?: CalendarSubscription }) {
  const g = ev.guests
  const online = useOnline()
  const [tell, setTell] = useState(true)
  const [busy, setBusy] = useState(false)
  const [ask, setAsk] = useState<ScopeRequest | null>(null)
  const group = useRef<HTMLDivElement>(null)
  if (!g?.canRespond) return null
  const silent = !!sub?.guests?.silent
  const scopes = replyScopes(ev, sub)
  const send = async (response: GuestAnswer, scope?: EventScope) => {
    setBusy(true)
    try {
      await sendNow(ev, { kind: 'reply', response, ...(scope ? { scope } : {}), ...(silent && !tell ? { sendResponse: false } : {}) })
    } finally {
      setBusy(false)
    }
  }
  const choose = (response: GuestAnswer) => {
    if (scopes.length < 2) return void send(response, scopes[0])
    const r = group.current?.getBoundingClientRect()
    setAsk({
      key: Date.now(), event: ev, change: {}, only: scopes, at: { x: r?.left ?? 0, y: r?.bottom ?? 0 },
      resolve: (scope) => { setAsk(null); if (scope) void send(response, scope) },
    })
  }
  const current: Choice = g.you && g.you !== 'needs-action' ? g.you : 'none'
  return (
    <div className="space-y-1.5" data-event-respond>
      <p className="text-meta text-muted" data-event-respond-you>{t('Your answer: {answer}', { answer: yourAnswerWords(g.you) })}</p>
      <div ref={group} aria-busy={busy || undefined}>
        <SegmentedControl<Choice> label={t('Your answer')} options={options()} value={current} disabled={!online || busy} data-event-answer onChange={(v) => { if (v !== 'none') choose(v) }} />
      </div>
      {silent && <Switch checked={tell} onChange={setTell} disabled={!online || busy} label={t('Tell the organizer')} />}
      {!online && <p className="text-meta text-faint" data-event-respond-offline>{t("Guests are told when you're online")}</p>}
      {ask && <Suspense fallback={null}><ScopeMenu key={ask.key} req={ask} /></Suspense>}
    </div>
  )
}
