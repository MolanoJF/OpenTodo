# Design

## Source identity
Connected calendars use a deterministic chromatic palette hue keyed by connection ID. Every subscription of that connection shares the hue. A per-connection device meta key overrides it with a palette hex; an Automatic control clears the override. Individual keys avoid lost updates between different account controls. Plain ICS feeds keep their configured colors. Derive colors in the shared subscription hook without changing cached provider data. Existing project appearance controls and Colour by Project remain the source for task colors; inbox tasks stay neutral.

## Surfaces and accessibility
Use existing light/dark solid, soft and text hue tokens. Events keep a hatched leading edge and dot, tasks a solid edge; color is never the only distinction. Free events keep dashed outlines. Focus, failed-write indicators and selection retain their existing recipes. All-day chips and month/agenda rows use the same recipe. The settings control reuses the keyboard-accessible project swatches in a popover, with an explicit device-only description.

## Data and compatibility
No ops, migrations, routes, provider writes or synced entity changes. Colors work offline and survive refreshes; same-device reads react to meta writes. Device choices deliberately do not converge between devices, as with Colour by. Project colors continue to converge through existing project ops. Cache refresh never overwrites a local account preference.

## Budget
No new screen chrome, shortcut, task property, sidebar destination or scroll region. One compact settings button. No dependency, container memory change or meaningful binary growth expected. Production build: entry 143.4 KB gzip; entry plus static imports 692.2 KB against a 696 KB budget.
