# Design
Add /event/new and /event/<id> routes rendered lazily by Shell. Keep the existing editor state and write path, but render a page with PageHeader instead of Dialog. Record the opening address/scroll; close returns there. Direct links work from cached rows. Use the router leave guard for dirty navigation and existing confirmations for recurrence and guests. No modal focus trap or scrim on event pages.

Keep layoutDay's real intervals, lane assignments and overflow/capacity semantics. A shared drawing helper gives cards a small vertical inset and expanded staggered widths; layer later lanes above earlier ones and raise hover/selection. Top-align content and wrap titles where height permits. No time is changed by visual gaps. Weekend fill uses existing theme surfaces. shadeOffHours defaults false and persists as device meta; week_start reuses existing user prefs and ops, converging normally across devices and working offline.

No new chrome rows, task properties, sidebar destinations or shortcuts. One page scroll region, existing overlay primitives for compact previews, and existing icon/token vocabulary. No dependency or meaningful container memory impact; verify existing bundle budget.

Measured production bundle: entry 143.4 KB gzip; entry and static imports 692.5 KB against the existing 696 KB budget. No budget increase.
