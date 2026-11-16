// Mount the shell's event route beside an isolated calendar in interaction tests.
import { useRoute } from './router'
import EventPage from './calendar/EventPage'
export function EventPageTestRoute() {
  const route = useRoute()
  return route.view === 'event' ? <EventPage id={route.id} /> : null
}
