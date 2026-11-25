package http

import (
	"context"
	"io"
	"time"

	"github.com/valyala/fasthttp"

	appattachment "easycode/internal/application/attachment"
	"easycode/internal/domain/identity"
)

// AttachmentUseCases are listAttachments and attachFile (US-03).
type AttachmentUseCases interface {
	List(ctx context.Context, principal identity.Principal, projectID string) ([]appattachment.Attachment, error)
	Attach(ctx context.Context, principal identity.Principal, projectID string, file appattachment.File) (appattachment.Attachment, error)
	Limit() int
}

type projectAttachmentJSON struct {
	ID          string      `json:"id"`
	FileName    string      `json:"file_name"`
	MediaType   string      `json:"media_type"`
	SizeBytes   int64       `json:"size_bytes"`
	SHA256      string      `json:"sha256"`
	SubmittedAt time.Time   `json:"submitted_at"`
	SubmittedBy userRefJSON `json:"submitted_by"`
}

func projectAttachmentOf(a appattachment.Attachment) projectAttachmentJSON {
	return projectAttachmentJSON{ID: a.ID, FileName: a.FileName, MediaType: a.MediaType, SizeBytes: a.SizeBytes, SHA256: a.SHA256,
		SubmittedAt: a.SubmittedAt, SubmittedBy: userRefJSON{ID: a.SubmittedBy.Subject, Name: a.SubmittedBy.Name}}
}

func (a *api) listAttachments(rc *fasthttp.RequestCtx, principal identity.Principal) error {
	projectID, err := pathUUID(rc, "projectId")
	if err != nil {
		return err
	}
	items, err := a.deps.Attachments.List(RequestContext(rc), principal, projectID)
	if err != nil {
		return err
	}
	out := itemsJSON[projectAttachmentJSON]{Items: []projectAttachmentJSON{}}
	for _, item := range items {
		out.Items = append(out.Items, projectAttachmentOf(item))
	}
	a.responder.JSON(rc, fasthttp.StatusOK, out)
	return nil
}

// attachFile receives one file (multipart field "file", the name from its filename). The service checks
// the role and the project state before the file itself, so a project without a role answers 404 (RNF-04).
func (a *api) attachFile(rc *fasthttp.RequestCtx, principal identity.Principal) error {
	projectID, err := pathUUID(rc, "projectId")
	if err != nil {
		return err
	}
	limit := a.deps.Attachments.Limit()
	file := appattachment.File{Missing: true}
	if len(rc.Request.Body()) > limit+64<<10 {
		file = appattachment.File{Oversize: true}
	} else if form, err := rc.MultipartForm(); err == nil && len(form.File["file"]) == 1 {
		header := form.File["file"][0]
		file = appattachment.File{Name: header.Filename, Oversize: header.Size > int64(limit)}
		if !file.Oversize {
			reader, err := header.Open()
			if err != nil {
				return err
			}
			file.Content, err = io.ReadAll(io.LimitReader(reader, int64(limit)+1))
			_ = reader.Close()
			if err != nil {
				return err
			}
		}
	}
	return a.idempotent(rc, principal, "attachFile", func(ctx context.Context) (created, error) {
		stored, err := a.deps.Attachments.Attach(ctx, principal, projectID, file)
		if err != nil {
			return created{}, err
		}
		return created{status: fasthttp.StatusCreated, body: projectAttachmentOf(stored),
			location: APIPrefix + "/projects/" + projectID + "/attachments/" + stored.ID}, nil
	})
}
