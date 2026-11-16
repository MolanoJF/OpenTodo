---
title: Guests, replies and join links
summary: See who is invited to an event in your Google, Outlook or CalDAV calendar, answer an invitation, change a meeting you organise on Outlook, and join a video call in one click.
audience: user
section: Guides
specs: [calendar-event-edits]
order: 66
---
# Guests, replies and join links

<picture><source srcset="images/week-dark.webp" media="(prefers-color-scheme: dark)"><img src="images/week-light.webp" alt="The Week calendar with timed blocks; an event's popover lists its guests and offers Yes, Maybe and No."></picture>

Events of a [connected calendar](calendar-sync.md#edit-events-in-opentodo) show who is invited and where the call is. Open an event to see them.

## Who is invited

The popover says who organises the event and how many guests it has, for example "Ana Silva · 3 guests, 2 accepted". Select the line to list every guest with their name or address, whether they are required or optional, and their answer in words: accepted, maybe, declined or no answer yet.

## Answer an invitation

When you are a guest, the popover has a row with **Yes**, **Maybe** and **No**, and it works even when the event is otherwise read-only. For a repeating event, choose **This event** or **All events** when asked.

- **Outlook and Microsoft 365:** your answer goes to the organiser. **Tell the organizer** can be switched off.
- **Google:** replies are not available yet. They are built, but stay off until they have been checked against a real Google calendar. Answer in Google Calendar for now.
- **Other CalDAV calendars:** read-only. Answer in your calendar app.


## Change a meeting you organise (Outlook)

On Outlook and Microsoft 365 you can move, rename or delete a meeting you organise, add or remove guests, and add a guest when you create an event. Before anything is sent a dialog asks **Tell 3 guests?** and says that Outlook sends the update itself. There is no choice to send nothing, because Outlook can't do that: choose **Save and tell guests**, or **Cancel** and nothing changes. A new event with guests asks **Invite 2 guests?**.

For a repeating meeting you can change **This event** or **All events**; **This and following events** would invite everyone again, so it isn't offered.

Anything that tells people is sent when you confirm, never queued. Offline, these controls are disabled and say why.

## Why Google events stay read-only

An event with guests on Google or another CalDAV calendar can't be changed here, and its popover says so. Google does not document whether a change sent that way tells the guests, and a wrong guess would email real people. You can still read it and convert it to a task. An Outlook meeting someone else organises is read-only too, apart from your answer.

## Join a call

**Join** appears in the popover, in the editor and on timed events in [Today](calendar-events-in-today.md), until the event ends. It opens the call in a new tab; OpenTodo never fetches it. Recognised links are the call Google or Outlook attached to the event, and secure (`https`) Zoom, Google Meet, Microsoft Teams and Webex addresses found in the event's link, location or description. Other addresses, and anything that is not `https`, are left as text.

On Outlook, **Add a Teams meeting** in the editor makes one with the event. It can't be removed later. A personal Microsoft account may refuse it.

## Privacy

Guest names and addresses belong to people who don't use your server. They are cached on your server with the event, so they are in your [backups](backups.md), and they are never written to a log. The [assistant](assistant-privacy.md) never receives them. OpenTodo sends no mail to guests: Outlook does, and only after you confirm.
