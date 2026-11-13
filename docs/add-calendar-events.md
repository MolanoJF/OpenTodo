---
title: Add events to your calendar
summary: Add an event to your Google, Outlook or CalDAV calendar from the Day and Week views, edit all its details in a side panel, duplicate it or turn it into a task.
audience: user
section: Guides
specs: [calendar-event-edits, time-blocking]
order: 64
---
# Add events to your calendar

<picture><source srcset="images/week-dark.webp" media="(prefers-color-scheme: dark)"><img src="images/week-light.webp" alt="The Week calendar with timed tasks as coloured blocks; an event can be added at any empty time."></picture>

Plan the call where you plan your day, and have it in your real calendar. It needs a [connected calendar](calendar-sync.md#edit-events-in-opentodo) with **Edit events in OpenTodo** on, and a calendar of it that allows new events. Without one nothing here appears and adding works as before.

## Task or Event

With such a calendar, adding on the [Day or Week](calendar-views.md) asks **Task** or **Event**. The default is Task, and this device remembers your last choice.

- **Double-click** an empty time.
- **Right-click**, or press and hold on a touch screen, an empty time and choose **Add task here…** or **Add event here…**.
- **Drag** over an empty stretch of the day with a mouse or pen: a block shows the times, and letting go adds the task or event for exactly that stretch. A touch screen scrolls.
- Press <kbd>q</kbd> for the kind you added last on the focused day, or <kbd>Shift</kbd>+<kbd>Q</kbd> for an event, at the next free half hour (09:00 on another day).
- Open the command bar and choose **New event**, for the next free half hour today.

## The editor

The event opens in a side panel (a sheet on a phone) with its title, **All day**, start and end, **Time zone**, **Calendar**, **Location**, **Description** (plain text) and **Show as** Busy or Free. Choose **Add event**, or press <kbd>Ctrl</kbd>+<kbd>Enter</kbd> (<kbd>⌘</kbd>+<kbd>Enter</kbd>). A toast confirms and offers **Undo**.

The new event is written to your calendar by the server, so it shows in your calendar app and on your other devices. It invites nobody. Set a repeat and reminders in the editor: [Change repeating calendar events](repeating-calendar-events.md).

Open an existing event the same way, with a double-click on it, or **All details** in its popover. Change what you like and choose **Save**; a toast offers **Undo**. The description is read from your calendar when the panel opens. If it has formatting the editor can't keep, it is shown but not changed.

## Duplicate, or turn it into a task

The panel's **More actions** menu has two more moves:

- **Duplicate** opens a new event with the same title, times, location and description, in the same calendar. Guests, reminders and the repeat are not copied.
- **Convert to task…** makes a task from the event, in the Inbox: its title, its notes with the location, and its time and length. The switch **Delete the event from your calendar** is on by default, since a timed task and its event would otherwise both be in your calendar. **Undo** within 8 seconds moves the task to the Trash and keeps the event; after that the event is gone from your calendar.

## Offline, and what is not here

Offline, the event shows at once and is added when you are back online, once. If your calendar refuses it, a toast offers **Edit again** with what you typed.

Guests and links to a video call are not part of this; the same [limits as editing](calendar-sync.md#what-stays-read-only) apply. Assistants and scripts can add events too: [`create_calendar_event`](mcp.md#write-tools) and [`ot calendar event add`](cli.md#calendar-events).
