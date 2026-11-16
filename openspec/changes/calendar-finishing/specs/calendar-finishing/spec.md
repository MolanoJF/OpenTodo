## ADDED Requirements
### Requirement: Quiet consistent event UI
Weekend tint SHALL be subtle in light/dark themes. Preview actions SHALL retain accessible names without opening tooltips. Event page fields SHALL use the same icon vocabulary and aligned rows as previews.
#### Scenario: Preview actions
- **WHEN** a preview opens or an action receives focus
- **THEN** no edit/open-action tooltip covers the preview

### Requirement: Move a single event between writable calendars
An eligible single event SHALL offer connected writable calendars on its page. Saving a different destination SHALL create the destination first and only then delete the source with a revision precondition. Moves SHALL require connectivity and loaded supported fields. A persistent device receipt SHALL make retries reuse the create client ID and created event. Failures SHALL retain the source and explain whether a destination copy exists. Pending receipts SHALL freeze editable fields and offer a retry. Successful moves SHALL update the local cache. Ordinary offline edits and their existing convergence SHALL be unchanged. Recurring/guest/conference events and unsupported fields SHALL stay on their original calendars with an explanation.
#### Scenario: Destination response lost
- **WHEN** the destination creation response is lost
- **THEN** the source is not deleted and a retry uses the same client ID
#### Scenario: Source deletion failure
- **WHEN** creation succeeds but source deletion fails
- **THEN** both copies remain and retry finishes deletion without creating another destination event
#### Scenario: Keep copies after conflict
- **WHEN** the destination copy is confirmed and the person chooses Keep both events
- **THEN** the retry receipt is cleared without writing either remote calendar
