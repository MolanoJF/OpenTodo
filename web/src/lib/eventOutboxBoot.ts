// Resumes the calendar event outbox without loading it (OpenSpec change calendar-event-edits D6): the editing code
// belongs to the calendar's own chunks, so a session only looks whether a command waits (a reload, a device that was
// offline) and loads the outbox then. The shell imports this module once, in the background, and importing it looks;
// the calendar starts the outbox itself when it makes the first command, and the outbox listens for the device being
// back online from then on.
import { db } from './db'

export async function resumeEventWrites(): Promise<void> {
  try {
    if ((await db.eventOutbox.count()) > 0) (await import('./calendarEventEdits')).startEventOutbox()
  } catch { /* the database closed under us (sign-out): nothing waits */ }
}

// The shell loads this module once per session, only for this: look now.
void resumeEventWrites()
