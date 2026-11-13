---
title: Change repeating calendar events
summary: Move, rename or delete one repeating event, this and the following ones, or all of them, set how an event repeats and add reminders, from the Day and Week views.
audience: user
section: Guides
specs: [calendar-event-edits]
order: 65
---
# Change repeating calendar events

<picture><source srcset="images/week-dark.webp" media="(prefers-color-scheme: dark)"><img src="images/week-light.webp" alt="The Week calendar with timed blocks; moving a repeating event asks which events to change."></picture>

A daily routine or a weekly meeting in your [connected calendar](calendar-sync.md#repeating-events-and-reminders) can be changed from the [Day and Week](calendar-views.md) views like any other event, once **Edit events in OpenTodo** is on.

## Which events?

Drag an occurrence, press the arrow keys on it, rename it, or choose **Save** or **Delete** in its popover or the editor. A small menu asks which events the change is for:

- **This event** changes only that one. A deleted occurrence is skipped.
- **This and following events** changes that one and every later one. The earlier ones stay as they were and a new series starts here. There is **no Undo**, and the halves can't be joined again. It is not offered on the first occurrence.
- **All events** changes every occurrence. Ones you changed individually keep their own day.

Use the arrow keys and <kbd>Enter</kbd> in the menu. <kbd>Esc</kbd> cancels and the event goes back. **This event** shows at once; the others show the block saving, then the calendar is read again. Moving all events to another day works for daily and weekly repeats only. A delete can be undone for 8 seconds, whichever you chose.

## Set the repeat

In the editor, **Repeat** offers Every day, Every weekday, Every week, Every month (by day or by weekday), Every year and **Custom…**, with every 1 to 99 days, weeks, months or years, chosen weekdays, and an end: never, on a date or after a number of times. Choose it when you [add an event](add-calendar-events.md) too. Changing the repeat of an existing series needs **This and following** or **All events** and resets the occurrences you changed individually.

A repeat the editor can't represent shows **Custom repeat** and stays as it is; everything else about the event can still be changed.

## Reminders

**Add reminder** offers At the start, minutes, hours or days before, up to five. Your calendar app sends them, not OpenTodo. Outlook takes one.

## What stays read-only

The popover says why. An event is also read-only with guests, when you are not its organiser, or in a read-only calendar. A repeat is **too complex to change here** when rewriting it could lose something: extra dates, an exclusion rule, several rules, a changed occurrence on a day the repeat doesn't have, or a very large series.

On Outlook, changing the times or repeat of **All events** drops occurrences you changed individually, **This and following** is refused for a series with attachments or an online meeting, and an existing series can't be set to stop repeating.
