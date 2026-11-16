# Design
Hide list-only phone chrome on event routes just as on task routes. Bound the shell to its viewport, use one contained vertical content scroller, and contain nested calendar scrolling. Keep task comment composers and event save footers in flow below desktop widths to avoid covering fields and jumping when the keyboard changes the viewport. Preserve desktop pinning and existing controls. Inspect 320, 390 and 430 CSS-pixel widths with touch input, including short viewports, long titles, previews and calendar selectors. Changes use existing tokens and primitives; no dependencies or material bundle growth.

Measured first-load impact: 692.1 KB gzip versus 692.2 KB before this pass; entry remains within 143.4 KB. Shared scroll styles live in CSS, without increasing bundle limits.
