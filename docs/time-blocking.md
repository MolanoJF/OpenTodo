---
title: Time blocking
summary: Estimate how long tasks take, then place them on the Day or Week calendar to see whether your plan fits the day.
audience: user
section: Guides
specs: [time-blocking, month-view]
screens: [settings/calendar#adjust-the-grid]
order: 60
card: week
---
# Time blocking

<picture><source srcset="images/week-dark.webp" media="(prefers-color-scheme: dark)"><img src="images/week-light.webp" alt="The Week calendar with timed tasks as blocks, date-only tasks above the hour grid and centered day headings."></picture>

Time blocking puts your list on the calendar: give tasks an estimate, drop them on the Day or Week calendar, and see at a glance whether your day is realistic.

## Estimate how long a task takes

Select a task and press <kbd>e</kbd>, then type `20`, `45m`, `1h` or `1h30` and press <kbd>Enter</kbd>. In the task panel, **Estimate** also has buttons for 15 minutes up to 2 hours. In quick add, write `+30m` or `+1h30`.

## Open the calendar

Choose **Calendar** in the sidebar, then **Day**, **Week**, **Month** or **Plan**, or press <kbd>g</kbd> <kbd>d</kbd>, <kbd>g</kbd> <kbd>w</kbd>, <kbd>g</kbd> <kbd>m</kbd> or <kbd>g</kbd> <kbd>Shift+C</kbd>. Timed tasks are blocks as tall as their estimate; dated tasks without a time wait in the **Unscheduled** lane. See [Day, Week and Month](calendar-views.md#open-the-calendar).

## See the month

**Month** shows six weeks at a glance, each day with its open tasks and planned total. Drag a task onto another day to move it. See [See the month](calendar-views.md#see-the-month).

## Plan a project on a timeline

For work that spans weeks, switch the project to **Timeline**. Each task is a bar from its start to its deadline, so overlaps and gaps show; drag bars to reschedule. See [Sections and boards](sections-and-boards.md#see-a-project-on-a-timeline).

## Plan the next two weeks

**Plan** counts meetings and repeating tasks too. See [Plan the next two weeks](calendar-views.md#plan-the-next-two-weeks).

## Place a task on the calendar

Drag a task from the Unscheduled lane onto a time slot, or select it in any list and press <kbd>s</kbd> to place it with the keyboard. Drag a block's top or bottom edge to change its start or end. See [Place tasks on the calendar](place-tasks-on-the-calendar.md#place-a-task-on-the-calendar).

## See whether the day fits

Month and Plan summarize estimates against your configured day length. Day and Week keep their headings clear of capacity totals. See [See whether the day fits](place-tasks-on-the-calendar.md#see-whether-the-day-fits).

## See your other calendars

With [calendar feeds](calendar-feeds.md) on, events appear in Day and Week beside your tasks, read-only unless [editing is on](calendar-sync.md#edit-events-in-opentodo), and count in the day's total only if you choose (below). See [Other calendars in Day and Week](other-calendars.md).

## Adjust the grid

On a phone the Week is a list: each day with its unscheduled tasks and its timed tasks in order, so nothing is squeezed into seven columns. Tap a task to select it, tap again (or press Enter) to open it.

In **Settings → Language and region → Calendar grid** you can set when the grid starts and ends (6:00 to 22:00 by default) and how long your working day is (8 hours by default). **Count meetings in capacity** adds busy meetings to each day's total. **Second time zone** adds a column of hours in another zone, with a short code such as BOG above each column. These settings belong to each device, so your phone and laptop can differ.

## Tips

- Estimate generously at first. After a week you'll know how long "quick" things really take.
- Leave gaps. When the morning slips anyway, [re-plan the rest of the day](replan-the-day.md).
- Keep weekday mornings for deep work with [frames](frames.md).
- Batch small tasks into one [slot](slots.md).
- **Quick wins**, under the **Filter** action in a list's header, shows tasks of 15 minutes or less, good for filling a short gap.
- Dates and times you set on the calendar are ordinary due dates, so they show up in Today and Upcoming as well, and in calendar apps connected over [CalDAV](caldav.md), estimate included.

## Calendar appearance

Timed tasks and events leave a small gap between consecutive cards. Overlapping items use wider staggered cards; hover or select one to bring it forward. These visual changes do not change event times or capacity. Weekends use the same background as weekdays. Only hour lines are drawn; dragging still snaps to 15-minute increments.

Open the calendar menu to choose **Monday** or **Sunday** under **Week starts on**. This uses your existing date preference and syncs across devices. The same setting is available under **Settings > Language and region > Calendar grid**, including the System and Saturday choices.

**Shade outside grid hours** is off by default. Enable it from the calendar menu or Calendar grid settings to dim hours outside your configured Grid start and Grid end. It is a device preference; every hour stays reachable. The color choices in the calendar menu explain whether task colors come from projects, labels or priorities.

## Calendar toolbar

Choose the current view name to open a dropdown with **Day**, **Week**, **Month**, **Plan** and **Year**. **Number of days** offers 2-6 consecutive days starting at the current date. This range stays on the device, and the arrows advance by that many days. Choose **Week** to restore the full week and its configured first day. **Today** is separate from the previous/next arrows.

Week shows the month and year with the ISO week number. Day names are centered; empty all-day lanes have no Unscheduled placeholder. Date-only tasks, events and deadlines still appear above the hour grid. Drag onto a day's heading to make a timed task date-only.

Open **More actions** for **Show completed tasks** and **Show other calendars on this device**. Both preferences stay on this device.

### Phone calendar space

The phone title sits beside the view dropdown, Today and navigation arrows. On the narrowest screens Today uses a calendar icon. The time grid reaches the bottom navigation, and the add button floats over it. Scroll to the end to bring the last item above the button; no permanent strip is reserved below the calendar.
