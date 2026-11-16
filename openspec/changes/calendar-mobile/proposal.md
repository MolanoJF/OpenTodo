# Why
Calendar additions must fit the phone shell. Event pages currently retain list navigation and the floating add button, which overlap the editor footer. Detail pages should scroll predictably without horizontal movement or mobile pinned composers covering content.

# What
Treat event routes as detail pages in the phone shell. Constrain shell scroll bounds, keep detail-page footers in normal flow on phones, and verify small-screen calendar controls, previews and detail fields with full-shell stories and focused regression tests. No dependency, API or data change.
