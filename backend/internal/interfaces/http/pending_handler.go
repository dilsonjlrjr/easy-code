package http

import (
	"context"
	"strconv"
	"time"

	"github.com/valyala/fasthttp"

	"easycode/internal/application/pending"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/identity"
)

// PendingUseCases is listPendingItems (US-48, US-10 CA-2).
type PendingUseCases interface {
	List(ctx context.Context, principal identity.Principal, filter pending.Filter) ([]pending.Item, int, error)
}

type pendingSummaryJSON struct {
	FailedChecks *int  `json:"failed_checks,omitempty"`
	Blocks       *bool `json:"blocks,omitempty"`
	Questions    *int  `json:"questions,omitempty"`
	YouDecide    bool  `json:"you_decide"`
}

type pendingItemJSON struct {
	Type    string             `json:"type"`
	Project projectRefJSON     `json:"project"`
	Stage   string             `json:"stage,omitempty"`
	Agent   string             `json:"agent,omitempty"`
	Target  commandTargetJSON  `json:"target"`
	Opened  time.Time          `json:"opened_at"`
	Title   string             `json:"title"`
	Summary pendingSummaryJSON `json:"summary"`
}

type projectRefJSON struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// listPendingItems is listPendingItems: oldest first, with the total for the menu counter (PL-15).
func (a *api) listPendingItems(rc *fasthttp.RequestCtx, principal identity.Principal) error {
	limit, err := queryLimit(rc)
	if err != nil {
		return err
	}
	offset := 0
	if cursor := string(rc.QueryArgs().Peek("cursor")); cursor != "" {
		if offset, err = strconv.Atoi(cursor); err != nil || offset < 0 {
			return failure.Invalid(failure.FieldError{Field: "cursor", Message: "invalid"})
		}
	}
	items, total, err := a.deps.Pending.List(RequestContext(rc), principal, pending.Filter{Types: queryList(rc, "type"), Limit: limit, Offset: offset})
	if err != nil {
		return err
	}
	out := struct {
		Items      []pendingItemJSON `json:"items"`
		NextCursor *string           `json:"next_cursor"`
		Total      int               `json:"total"`
	}{Items: []pendingItemJSON{}, Total: total}
	for _, item := range items {
		view := pendingItemJSON{
			Type: item.Type, Project: projectRefJSON{ID: item.ProjectID, Name: item.ProjectName}, Stage: item.Stage, Agent: item.Agent,
			Target: commandTargetJSON{Type: item.TargetType, ID: item.TargetID}, Opened: item.OpenedAt,
			Summary: pendingSummaryJSON{YouDecide: item.YouDecide},
		}
		switch item.Type {
		case pending.TypeGate:
			view.Title = a.responder.Message(rc, "pending.gate.title", map[string]string{"gate": item.Gate, "round": strconv.Itoa(item.Round)})
			view.Summary.FailedChecks, view.Summary.Blocks = &item.FailedChecks, &item.Blocks
		case pending.TypeQuestion:
			view.Title = a.responder.Message(rc, "pending.question.title", map[string]string{"wave": item.Wave, "agent": item.Agent})
			view.Summary.Questions = &item.Questions
		}
		out.Items = append(out.Items, view)
	}
	if offset+len(items) < total {
		next := strconv.Itoa(offset + len(items))
		out.NextCursor = &next
	}
	a.responder.JSON(rc, fasthttp.StatusOK, out)
	return nil
}
