package http

import (
	"context"
	"time"

	"github.com/valyala/fasthttp"

	appartifact "easycode/internal/application/artifact"
	"easycode/internal/domain/identity"
)

// ArtifactEditUseCases are the human edits of artifacts validated by the stage agent (US-53).
type ArtifactEditUseCases interface {
	Request(ctx context.Context, principal identity.Principal, projectID string, edit appartifact.EditRequest, key, bodyHash string) (string, string, error)
	Get(ctx context.Context, principal identity.Principal, projectID, id string) (appartifact.EditView, error)
}

type artifactEditAcceptedJSON struct {
	commandJSON
	Edit string `json:"edit"`
}

// editArtifact asks a human edit (US-53): 202 with the edit_artifact command; the edit only counts after the
// edit_review wave of the stage agent. getArtifactEdit (Content-Location) tells the outcome.
func (a *api) editArtifact(rc *fasthttp.RequestCtx, principal identity.Principal) error {
	projectID, err := pathUUID(rc, "projectId")
	if err != nil {
		return err
	}
	var body struct {
		Operation   string `json:"operation"`
		Type        string `json:"type"`
		Artifact    string `json:"artifact"`
		BaseVersion int    `json:"base_version"`
		Content     string `json:"content"`
		Reason      string `json:"reason"`
		Finding     string `json:"finding"`
	}
	if err := decodeJSON(rc, &body); err != nil {
		return err
	}
	req, err := idempotencyRequest(rc, principal, "editArtifact")
	if err != nil {
		return err
	}
	edit := appartifact.EditRequest{Operation: body.Operation, Type: body.Type, Artifact: body.Artifact, BaseVersion: body.BaseVersion,
		Content: body.Content, Reason: body.Reason}
	return a.idempotent(rc, principal, "editArtifact", func(ctx context.Context) (created, error) {
		editID, commandID, err := a.deps.ArtifactEdits.Request(ctx, principal, projectID, edit, req.Key, req.BodyHash)
		if err != nil {
			return created{}, err
		}
		target := commandTargetJSON{Type: "artifact", ID: body.Artifact}
		if target.ID == "" {
			target.ID = body.Type
		}
		accepted := artifactEditAcceptedJSON{commandJSON: commandJSON{
			ID: commandID, Type: "edit_artifact", Target: target, State: "pending", Origin: "web", RequestedAt: time.Now().UTC(),
			RequestedBy: userRefJSON{ID: principal.Subject, Name: principal.Name},
		}, Edit: editID}
		return created{status: fasthttp.StatusAccepted, body: accepted, location: commandLocation(projectID, commandID)}, nil
	})
}

type artifactEditJSON struct {
	ID        string  `json:"id"`
	Command   string  `json:"command"`
	Operation string  `json:"operation"`
	Type      string  `json:"type"`
	Artifact  *string `json:"artifact"`
	State     string  `json:"state"`
	Wave      *string `json:"review_wave"`
	Version   *int    `json:"version"`
	Refusal   *string `json:"refusal_reason"`
}

// getArtifactEdit tells the outcome of a human edit: pending, reviewing, applied (with the version) or refused
// (CA-2: the agent did not validate it and the previous version still holds).
func (a *api) getArtifactEdit(rc *fasthttp.RequestCtx, principal identity.Principal) error {
	projectID, err := pathUUID(rc, "projectId")
	if err != nil {
		return err
	}
	id, err := pathUUID(rc, "editId")
	if err != nil {
		return err
	}
	v, err := a.deps.ArtifactEdits.Get(RequestContext(rc), principal, projectID, id)
	if err != nil {
		return err
	}
	a.responder.JSON(rc, fasthttp.StatusOK, artifactEditJSON{
		ID: v.ID, Command: v.Command, Operation: v.Operation, Type: v.Type, Artifact: optional(v.Artifact), State: v.State,
		Wave: optional(v.Wave), Version: optional(v.Version), Refusal: optional(v.Refusal),
	})
	return nil
}
