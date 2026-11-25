package http

import (
	"context"

	"github.com/valyala/fasthttp"

	"easycode/internal/application/search"
	"easycode/internal/domain/identity"
)

// SearchUseCases is searchPlatform (US-56).
type SearchUseCases interface {
	Search(ctx context.Context, principal identity.Principal, term string) (search.Results, error)
}

type searchProjectRefJSON struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	State string `json:"state"`
}

type searchArtifactJSON struct {
	ID      string               `json:"id"`
	Type    string               `json:"type"`
	Title   string               `json:"title"`
	State   string               `json:"state"`
	Project searchProjectRefJSON `json:"project"`
}

type searchFindingJSON struct {
	ID       string               `json:"id"`
	Title    string               `json:"title"`
	Status   string               `json:"status"`
	Severity string               `json:"severity"`
	Project  searchProjectRefJSON `json:"project"`
}

type searchGroupJSON[T any] struct {
	Items        []T `json:"items"`
	Total        int `json:"total"`
	ProjectCount int `json:"project_count"`
}

func searchGroup[T any](group search.Group, view func(search.Hit) T) searchGroupJSON[T] {
	out := searchGroupJSON[T]{Items: make([]T, 0, len(group.Items)), Total: group.Total, ProjectCount: group.Projects}
	for _, hit := range group.Items {
		out.Items = append(out.Items, view(hit))
	}
	return out
}

func searchProjectOf(hit search.Hit) searchProjectRefJSON {
	return searchProjectRefJSON{ID: hit.ProjectID, Name: hit.ProjectName, State: hit.ProjectState}
}

// searchPlatform is searchPlatform: the header search, grouped by type (US-56 CA-2). The term is read
// from the query and never logged: the access log keeps only the route template (RN-4).
func (a *api) searchPlatform(rc *fasthttp.RequestCtx, principal identity.Principal) error {
	results, err := a.deps.Search.Search(RequestContext(rc), principal, string(rc.QueryArgs().Peek("search")))
	if err != nil {
		return err
	}
	a.responder.JSON(rc, fasthttp.StatusOK, struct {
		Projects  searchGroupJSON[searchProjectRefJSON] `json:"projects"`
		Artifacts searchGroupJSON[searchArtifactJSON]   `json:"artifacts"`
		Findings  searchGroupJSON[searchFindingJSON]    `json:"findings"`
	}{
		Projects: searchGroup(results.Projects, searchProjectOf),
		Artifacts: searchGroup(results.Artifacts, func(hit search.Hit) searchArtifactJSON {
			return searchArtifactJSON{ID: hit.ID, Type: hit.Type, Title: hit.Title, State: hit.State, Project: searchProjectOf(hit)}
		}),
		Findings: searchGroup(results.Findings, func(hit search.Hit) searchFindingJSON {
			return searchFindingJSON{ID: hit.ID, Title: hit.Title, Status: hit.State, Severity: hit.Severity, Project: searchProjectOf(hit)}
		}),
	})
	return nil
}
