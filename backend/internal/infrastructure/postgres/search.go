package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"easycode/internal/application/search"
	"easycode/internal/domain/identity"
)

// Search implements search.Store (US-56). It starts from the projects of the person
// (project_members_user_idx), so it reads only the artifacts of those projects (artifacts_code_uk and
// artifacts_project_type_idx lead with project_id) and needs no text index (PENDENCIAS PD-24 option a).
type Search struct {
	pool *pgxpool.Pool
}

var _ search.Store = Search{}

// NewSearch builds the adapter.
func NewSearch(pool *pgxpool.Pool) Search { return Search{pool: pool} }

// searchSQL: $1 organization, $2 subject, $3 term, $4 LIKE pattern, $5 limit per group. A project marked
// for deletion (and later anonymized) is out (US-56 CA-6), like in every list. An exact id or name comes
// first.
const searchSQL = `
WITH me AS (` + principalUserIDSQL + `),
mine AS (
  SELECT DISTINCT pr.id, pr.public_id, pr.name, pr.state
    FROM easycode.projects pr JOIN easycode.project_members m ON m.project_id = pr.id
   WHERE m.user_id = (SELECT * FROM me) AND pr.deletion_requested_at IS NULL
),
hits AS (
  SELECT 'project' AS kind, p.public_id::text AS project_id, p.name AS project_name, p.state AS project_state,
         '' AS code, '' AS type, p.name AS title, p.state AS state, '' AS severity, lower(p.name) = lower($3) AS exact
    FROM mine p
   WHERE p.name ILIKE $4
  UNION ALL
  SELECT 'artifact', p.public_id::text, p.name, p.state, a.code, a.type, a.title, a.state, '', upper(a.code) = upper($3)
    FROM mine p JOIN easycode.artifacts a ON a.project_id = p.id
   WHERE a.type <> 'ACH' AND (a.code ILIKE $4 OR a.title ILIKE $4)
  UNION ALL
  SELECT 'finding', p.public_id::text, p.name, p.state, a.code, a.type, a.title, f.status, f.severity, upper(a.code) = upper($3)
    FROM mine p JOIN easycode.artifacts a ON a.project_id = p.id AND a.type = 'ACH'
    JOIN easycode.findings f ON f.artifact_id = a.id
   WHERE a.code ILIKE $4 OR a.title ILIKE $4
),
ranked AS (
  SELECT h.*, row_number() OVER (PARTITION BY kind ORDER BY exact DESC, lower(project_name), project_id, code) AS n,
         count(*) OVER (PARTITION BY kind) AS total,
         dense_rank() OVER (PARTITION BY kind ORDER BY project_id) AS project_rank
    FROM hits h
),
counted AS (
  SELECT r.*, max(project_rank) OVER (PARTITION BY kind) AS projects FROM ranked r
)
SELECT kind, project_id, project_name, project_state, code, type, title, state, severity, total, projects
  FROM counted
 WHERE n <= $5
 ORDER BY kind, n`

// likeEscaper keeps %, _ and \ of the term literal in the pattern.
var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// Find implements search.Store.
func (s Search) Find(ctx context.Context, principal identity.Principal, term string, limit int) (search.Results, error) {
	rows, err := conn(ctx, s.pool).Query(WithOperation(ctx, "search.find"), searchSQL,
		principal.OrganizationID, principal.Subject, term, "%"+likeEscaper.Replace(term)+"%", limit)
	if err != nil {
		return search.Results{}, fmt.Errorf("search: %w", classify(err))
	}
	var results search.Results
	groups := map[string]*search.Group{"project": &results.Projects, "artifact": &results.Artifacts, "finding": &results.Findings}
	var kind string
	var hit search.Hit
	var total, projects int
	_, err = pgx.ForEachRow(rows, []any{&kind, &hit.ProjectID, &hit.ProjectName, &hit.ProjectState, &hit.ID, &hit.Type,
		&hit.Title, &hit.State, &hit.Severity, &total, &projects}, func() error {
		group := groups[kind]
		group.Items = append(group.Items, hit)
		group.Total, group.Projects = total, projects
		return nil
	})
	if err != nil {
		return search.Results{}, fmt.Errorf("scan search: %w", classify(err))
	}
	return results, nil
}
