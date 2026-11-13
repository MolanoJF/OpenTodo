import type { IconName } from '../../ui'
import type { GuestResponse } from '../../lib/types'

/** An answer's icon; always paired with its word, never colour alone (calendar-event-guests D17). */
export const RESPONSE_ICON: Record<GuestResponse, IconName> = { accepted: 'check', declined: 'x', tentative: 'circle-help', 'needs-action': 'minus' }
