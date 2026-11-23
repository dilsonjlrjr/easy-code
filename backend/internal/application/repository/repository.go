// Package repository holds the api use cases of the project git repository (US-42): the clone
// address with the last commit (getRepository) and the read-only clone over smart HTTP. Without a
// role in the project both answer not found, the same as a missing project (CA-3, RNF-04).
package repository

import (
	"context"
	"errors"
	"io"
	"time"

	appproject "easycode/internal/application/project"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/identity"
)

// ErrEmpty is returned by Git when the project has no commit yet.
var ErrEmpty = errors.New("repository without commit")

// Commit is the last commit of the main branch.
type Commit struct {
	SHA  string
	When time.Time
	Wave string
}

// Mark is a tag of the project repository: the version mark of an approved Release (US-08 CA-3).
type Mark struct {
	Name, Commit string
	When         time.Time
}

// Git reads the project repositories (volume repos, read-only for the api).
type Git interface {
	// Marks lists the tags of the repository, oldest first.
	Marks(ctx context.Context, projectID string) ([]Mark, error)
	// Head returns the last commit of main, or ErrEmpty.
	Head(ctx context.Context, projectID string) (Commit, error)
	// UploadPack answers git-upload-pack (stateless RPC); advertise writes the references.
	UploadPack(ctx context.Context, projectID string, advertise bool, in io.Reader, out io.Writer) error
}

// View is getRepository.
type View struct {
	CloneURL   string
	MainBranch string
	LastCommit *Commit
	Marks      []Mark
}

// Service is the api side of the repository.
type Service struct {
	projects appproject.Repository
	git      Git
	baseURL  string
	branch   string
}

// NewService builds the service; baseURL is the public origin of the clone URLs.
func NewService(projects appproject.Repository, git Git, baseURL, branch string) *Service {
	return &Service{projects: projects, git: git, baseURL: baseURL, branch: branch}
}

// Get is getRepository (US-42, US-19).
func (s *Service) Get(ctx context.Context, principal identity.Principal, projectID string) (View, error) {
	if _, err := appproject.Authorize(ctx, s.projects, principal, projectID); err != nil {
		return View{}, err
	}
	view := View{CloneURL: s.baseURL + "/git/" + projectID + ".git", MainBranch: s.branch}
	head, err := s.git.Head(ctx, projectID)
	switch {
	case errors.Is(err, ErrEmpty):
	case err != nil:
		return View{}, err
	default:
		view.LastCommit = &head
	}
	if view.Marks, err = s.git.Marks(ctx, projectID); err != nil {
		return View{}, err
	}
	return view, nil
}

// UploadPack serves the clone (US-42 CA-1, CA-2) to a member of the project; without role, not found (CA-3).
func (s *Service) UploadPack(ctx context.Context, principal identity.Principal, projectID string, advertise bool, in io.Reader, out io.Writer) error {
	if _, err := appproject.Authorize(ctx, s.projects, principal, projectID); err != nil {
		return err
	}
	err := s.git.UploadPack(ctx, projectID, advertise, in, out)
	if errors.Is(err, ErrEmpty) {
		return failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
	}
	return err
}
