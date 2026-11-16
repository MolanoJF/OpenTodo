# Design

Use the overlay system's focus trap, Escape handling and focus restoration for a viewport-sized event page. Reuse EventForm and useEventEditing with a page layout: prominent title and description in the main column, grouped icon-led schedule/facts in a 20rem right column; stack on phones. Retain dirty-close confirmation, explicit Save, recurrence scope prompts and guest notification prompts. No new backend writes.

Event previews become read-first rather than presenting an inline form. Load full description and reminder data only when the preview opens, show cached facts offline and an explicit unavailable state for remote detail. Never render provider HTML as HTML. All details opens the full page; editing still obeys capabilities. Calendar task previews show cached task data, inherit its color and provide an explicit full-page action. Pointer drags must remain drags; double-click still opens details. Preview popovers use existing overlay primitives and bounded scrolling on small screens.

Data model: no changes. Task writes keep their op path; remote event writes keep their command path and current convergence behavior. Reading or opening a preview never writes data. No new shortcuts or chrome rows, task properties or sidebar destinations. Full page has one main scroll region and responsive columns, within the interface budget. No dependencies, expected negligible runtime memory impact; measure the lazy bundle impact after build.

Measured production bundle: entry and static imports 692.3 KB gzip against the existing 696 KB budget; no dependency added.
