package http

import (
	"context"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/valyala/fasthttp"

	appskill "easycode/internal/application/skill"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/identity"
	domainskill "easycode/internal/domain/skill"
)

// SkillImportUseCases are the organization skill imports and their human transitions (US-44).
type SkillImportUseCases interface {
	Upload(ctx context.Context, principal identity.Principal, pkg []byte, key string) (appskill.Import, error)
	ImportStatus(ctx context.Context, principal identity.Principal, id string) (appskill.Import, error)
	Unblock(ctx context.Context, principal identity.Principal, name string, version int, justification string) (appskill.Summary, error)
	Deactivate(ctx context.Context, principal identity.Principal, name string, version int, justification string) (appskill.Summary, error)
}

type skillImportJSON struct {
	ID            string    `json:"id"`
	State         string    `json:"state"`
	Origin        string    `json:"origin"`
	VersionStatus *string   `json:"version_status"`
	ReceivedAt    time.Time `json:"received_at"`
	Skill         *string   `json:"skill"`
	Version       *int      `json:"version"`
	Refusal       *refusal  `json:"refusal,omitempty"`
	BlockReasons  []string  `json:"block_reasons"`
}

type refusal struct {
	Type   string `json:"type"`
	Reason string `json:"reason"`
}

func skillImportOf(i appskill.Import) skillImportJSON {
	out := skillImportJSON{ID: i.ID, State: i.State, Origin: i.Origin, ReceivedAt: i.ReceivedAt, BlockReasons: []string{}}
	if i.Skill != "" {
		out.Skill, out.Version, out.VersionStatus = &i.Skill, &i.Version, &i.VersionStatus
		out.BlockReasons = append(out.BlockReasons, i.BlockReasons...)
	}
	if i.State == appskill.ImportRefused {
		out.Refusal = &refusal{Type: problemTypePrefix + domainskill.CodeInvalidPackage, Reason: i.RefusalReason}
	}
	return out
}

// importSkill receives a zipped skill folder (multipart field "package"): refusals at the submission are 413
// too-large-package (CA-3) and 400 invalid-skill-package (CA-2); otherwise 202 and the worker scans it.
// The import by git URL needs the egress gateway with allowlist (ADR-16), not in this stack: 400.
func (a *api) importSkill(rc *fasthttp.RequestCtx, principal identity.Principal) error {
	if !principal.Administrator {
		return failure.New(failure.KindForbidden, failure.CodeForbidden, nil)
	}
	if strings.HasPrefix(string(rc.Request.Header.ContentType()), "application/json") {
		return failure.Invalid(failure.FieldError{Field: "git_url", Message: "host_not_allowed"})
	}
	if len(rc.Request.Body()) > domainskill.MaxPackageBytes+64<<10 {
		return failure.New(failure.KindInvalidInput, domainskill.CodeTooLargePackage, nil).With("limit_bytes", domainskill.MaxPackageBytes)
	}
	form, err := rc.MultipartForm()
	if err != nil || len(form.File["package"]) != 1 {
		return failure.Invalid(failure.FieldError{Field: "package", Message: "required"})
	}
	header := form.File["package"][0]
	if header.Size > domainskill.MaxPackageBytes {
		return failure.New(failure.KindInvalidInput, domainskill.CodeTooLargePackage, nil).With("limit_bytes", domainskill.MaxPackageBytes)
	}
	file, err := header.Open()
	if err != nil {
		return failure.Invalid(failure.FieldError{Field: "package", Message: "invalid"})
	}
	pkg, err := io.ReadAll(io.LimitReader(file, domainskill.MaxPackageBytes+1))
	_ = file.Close()
	if err != nil {
		return err
	}
	key := string(rc.Request.Header.Peek("Idempotency-Key"))
	return a.idempotent(rc, principal, "importSkill", func(ctx context.Context) (created, error) {
		received, err := a.deps.SkillImports.Upload(ctx, principal, pkg, key)
		if err != nil {
			return created{}, err
		}
		return created{status: fasthttp.StatusAccepted, body: skillImportOf(received), location: APIPrefix + "/skills/imports/" + received.ID}, nil
	})
}

func (a *api) getSkillImport(rc *fasthttp.RequestCtx, principal identity.Principal) error {
	id, err := pathUUID(rc, "importId")
	if err != nil {
		return err
	}
	found, err := a.deps.SkillImports.ImportStatus(RequestContext(rc), principal, id)
	if err != nil {
		return err
	}
	a.responder.JSON(rc, fasthttp.StatusOK, skillImportOf(found))
	return nil
}

func (a *api) skillTransition(rc *fasthttp.RequestCtx, principal identity.Principal, operation string,
	apply func(context.Context, identity.Principal, string, int, string) (appskill.Summary, error)) error {
	version, err := strconv.Atoi(PathParam(rc, "version"))
	if err != nil || version < 1 {
		return failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
	}
	var body struct {
		Justification string `json:"justification"`
	}
	if err := decodeJSON(rc, &body); err != nil {
		return err
	}
	return a.idempotent(rc, principal, operation, func(ctx context.Context) (created, error) {
		summary, err := apply(ctx, principal, PathParam(rc, "skillId"), version, body.Justification)
		if err != nil {
			return created{}, err
		}
		return created{status: fasthttp.StatusOK, body: skillOf(summary)}, nil
	})
}

// unblockSkill releases a version blocked by suspicious content (US-44 CA-4).
func (a *api) unblockSkill(rc *fasthttp.RequestCtx, principal identity.Principal) error {
	return a.skillTransition(rc, principal, "unblockSkill", a.deps.SkillImports.Unblock)
}

// deactivateSkill takes a version of the organization out of the library.
func (a *api) deactivateSkill(rc *fasthttp.RequestCtx, principal identity.Principal) error {
	return a.skillTransition(rc, principal, "deactivateSkill", a.deps.SkillImports.Deactivate)
}
