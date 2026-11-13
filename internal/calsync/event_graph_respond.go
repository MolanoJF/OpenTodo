package calsync

import (
	"context"
	"net/http"

	"github.com/OpenTodoApp/opentodo/internal/calfeed"
)

// Answering an invitation through Microsoft Graph (calendar-event-guests D7): the
// accept, tentativelyAccept and decline actions, which take sendResponse (true by
// default: the organiser is told) and answer 202 with no body. A single event is
// answered on its id; for a repeating event this is the occurrence's id (Outlook
// records an exception) and all is the series master's, read from the occurrence at
// write time (M7: believed to answer the series, to be confirmed by the smoke test).
// The action takes no precondition, so the ETag is compared on a read just before.

var graphActions = map[Answer]string{
	ResponseAccepted:  "accept",
	ResponseTentative: "tentativelyAccept",
	ResponseDeclined:  "decline",
}

// Reply answers an invitation while the event still has ref.ETag.
func (e graphEvents) Reply(ctx context.Context, ref EventRef, reply ReplyEdit) (EventState, error) {
	action, ok := graphActions[reply.Response]
	if !e.caps.Respond || !ok {
		return EventState{}, ErrCannotRespond
	}
	if ref.ETag == "" {
		return EventState{}, locked(LockNoETag)
	}
	cur, err := e.fetch(ctx, ref.Href, eventSelect)
	if err != nil {
		return EventState{}, err
	}
	switch {
	case cur.ETag != ref.ETag:
		return EventState{}, ErrEventChanged
	case !cur.supported():
		return EventState{}, locked(LockSeriesUnsupported)
	case cur.IsOrganizer == nil || *cur.IsOrganizer:
		return EventState{}, ErrCannotRespond // the user's own meeting, or whose it is is not known
	}
	target := ref.Href
	switch {
	case cur.inSeries() && reply.Scope == calfeed.ScopeAll:
		target = cur.SeriesMasterID
	case cur.inSeries() && reply.Scope == calfeed.ScopeThis:
	case cur.inSeries() || reply.Scope != "":
		return EventState{}, invalid("scope")
	}
	body := map[string]any{"sendResponse": reply.SendResponse}
	resp, err := e.g.do(ctx, http.MethodPost, eventPath(target)+"/"+action, nil, body)
	if err != nil {
		return EventState{}, err
	}
	if err := refusal(resp.Status); err != nil {
		return EventState{}, err
	}
	now, err := e.fetch(ctx, ref.Href, eventSelect)
	if err != nil {
		return EventState{}, err
	}
	st, err := e.state(ref, now)
	if err != nil {
		return EventState{}, err
	}
	if st.Guests.You != string(reply.Response) {
		return st, ErrResponseNotKept
	}
	return st, nil
}
