package http

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"io"
	"strings"
	"time"

	"github.com/valyala/fasthttp"

	"easycode/internal/application/localclient"
	apprepository "easycode/internal/application/repository"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/identity"
)

// RepositoryUseCases are getRepository and the read-only clone (US-42).
type RepositoryUseCases interface {
	Get(ctx context.Context, principal identity.Principal, projectID string) (apprepository.View, error)
	UploadPack(ctx context.Context, principal identity.Principal, projectID string, advertise bool, in io.Reader, out io.Writer) error
}

const maxUploadPackRequest = 8 << 20

// getRepository is getRepository: clone address, main branch and last commit (US-42, US-19).
func (a *api) getRepository(rc *fasthttp.RequestCtx, principal identity.Principal) error {
	projectID, err := pathUUID(rc, "projectId")
	if err != nil {
		return err
	}
	view, err := a.deps.Repository.Get(RequestContext(rc), principal, projectID)
	if err != nil {
		return err
	}
	body := map[string]any{"status": "not_created", "clone_url": view.CloneURL, "main_branch": view.MainBranch, "last_commit": nil, "marks": []any{}}
	if c := view.LastCommit; c != nil {
		body["status"] = "available"
		commit := map[string]any{"sha": c.SHA, "when": c.When.UTC().Format(time.RFC3339)}
		if c.Wave != "" {
			commit["wave"] = c.Wave
		}
		body["last_commit"] = commit
	}
	marks := make([]map[string]any, 0, len(view.Marks))
	for _, m := range view.Marks {
		mark := map[string]any{"name": m.Name, "commit": m.Commit}
		if !m.When.IsZero() {
			mark["when"] = m.When.UTC().Format(time.RFC3339)
		}
		marks = append(marks, mark)
	}
	body["marks"] = marks // US-08 CA-3: the version mark of the approved Release
	a.responder.JSON(rc, fasthttp.StatusOK, body)
	return nil
}

// gitPrincipal authenticates the git client: HTTP Basic with any user and the session token as the
// password (the token of the local client in MARCO-8, cliente-local.md). Without it the client is asked
// for credentials (401 with WWW-Authenticate), so git prompts or uses its credential helper.
func (a *api) gitPrincipal(rc *fasthttp.RequestCtx) (identity.Principal, error) {
	header := string(rc.Request.Header.Peek(fasthttp.HeaderAuthorization))
	encoded, ok := strings.CutPrefix(header, "Basic ")
	if ok {
		if raw, err := base64.StdEncoding.DecodeString(encoded); err == nil {
			if _, password, found := strings.Cut(string(raw), ":"); found {
				principal, err := a.gitAuthenticate(rc, password)
				if err == nil {
					SetUserID(rc, principal.Subject)
					return principal, nil
				}
			}
		}
	}
	rc.Response.Header.Set("WWW-Authenticate", `Basic realm="Easy Code"`)
	return identity.Principal{}, failure.New(failure.KindUnauthenticated, failure.CodeNotAuthenticated, nil)
}

// gitAuthenticate accepts the web session token or the access token of the desktop app (a JWT: its git
// credential helper answers it as the password, cliente-local.md section 5; the clone is a read).
func (a *api) gitAuthenticate(rc *fasthttp.RequestCtx, password string) (identity.Principal, error) {
	if a.auth.local != nil && strings.Count(password, ".") == 2 {
		principal, _, err := a.auth.local.Authenticate(RequestContext(rc), password, localclient.Client{
			UserAgent: string(rc.Request.Header.UserAgent()), IPAddress: clientAddress(rc),
		})
		return principal, err
	}
	principal, _, err := a.auth.sessions.Authenticate(RequestContext(rc), password)
	return principal, err
}

// gitProject reads the project of /git/{repository}: "<project uuid>.git".
func gitProject(rc *fasthttp.RequestCtx) (string, error) {
	name, ok := strings.CutSuffix(strings.ToLower(PathParam(rc, "repository")), ".git")
	if !ok || !uuidPattern.MatchString(name) {
		return "", failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
	}
	return name, nil
}

// gitReferences answers GET /git/{project}.git/info/refs?service=git-upload-pack (smart HTTP, read-only:
// git-receive-pack is never served, so nobody pushes).
func (a *api) gitReferences(rc *fasthttp.RequestCtx) error {
	if string(rc.QueryArgs().Peek("service")) != "git-upload-pack" {
		return failure.New(failure.KindForbidden, failure.CodeForbidden, nil)
	}
	projectID, err := gitProject(rc)
	if err != nil {
		return err
	}
	principal, err := a.gitPrincipal(rc)
	if err != nil {
		return err
	}
	var out bytes.Buffer
	out.WriteString("001e# service=git-upload-pack\n0000")
	if err := a.deps.Repository.UploadPack(RequestContext(rc), principal, projectID, true, strings.NewReader(""), &out); err != nil {
		return err
	}
	rc.Response.Header.Set("Cache-Control", "no-cache")
	rc.SetContentType("application/x-git-upload-pack-advertisement")
	rc.SetStatusCode(fasthttp.StatusOK)
	rc.SetBody(out.Bytes())
	return nil
}

// gitUploadPack answers POST /git/{project}.git/git-upload-pack (the pack of the clone).
func (a *api) gitUploadPack(rc *fasthttp.RequestCtx) error {
	projectID, err := gitProject(rc)
	if err != nil {
		return err
	}
	principal, err := a.gitPrincipal(rc)
	if err != nil {
		return err
	}
	body := rc.PostBody()
	if len(body) > maxUploadPackRequest {
		return failure.New(failure.KindInvalidInput, failure.CodeValidation, nil)
	}
	var in io.Reader = bytes.NewReader(body)
	if strings.EqualFold(string(rc.Request.Header.Peek(fasthttp.HeaderContentEncoding)), "gzip") {
		reader, err := gzip.NewReader(in)
		if err != nil {
			return failure.New(failure.KindInvalidInput, failure.CodeValidation, err)
		}
		in = io.LimitReader(reader, maxUploadPackRequest)
	}
	var out bytes.Buffer
	if err := a.deps.Repository.UploadPack(RequestContext(rc), principal, projectID, false, in, &out); err != nil {
		return err
	}
	rc.Response.Header.Set("Cache-Control", "no-cache")
	rc.SetContentType("application/x-git-upload-pack-result")
	rc.SetStatusCode(fasthttp.StatusOK)
	rc.SetBody(out.Bytes())
	return nil
}
