package gate

import (
	"context"
	"time"

	appproject "easycode/internal/application/project"
	domainfinding "easycode/internal/domain/finding"
	"easycode/internal/domain/identity"
	domainworkflow "easycode/internal/domain/workflow"
)

// Block types of the Release (ReleaseSummary.blocks; US-08 CA-2, CA-6, CA-7).
const (
	BlockFindingWithoutAcceptance = "blocking_finding_no_acceptance"
	BlockAcceptanceExpired        = "acceptance_expired"
	BlockGateNotApproved          = "gate_not_approved"
	BlockActiveWave               = "active_wave"
)

// ReleaseBlock is something that keeps the Release from being approved.
type ReleaseBlock struct {
	Type       string
	Reference  string
	ValidUntil *time.Time
}

// SummaryGate is the gate of a stage in the Release summary: its last round, if any.
type SummaryGate struct {
	Gate, Stage string
	Round       *RoundView
}

// ReleaseSummary is getReleaseSummary (US-08 CA-1, CA-7; US-33 CA-4).
type ReleaseSummary struct {
	Gates        []SummaryGate
	Tests        struct{ Total, Passed, Failed int }
	Report       string // REL-QA of the tests
	OpenFindings []domainfinding.Finding
	Accepted     []domainfinding.Finding
	Cost, Lost   string
	Currency     string
	Blocks       []ReleaseBlock
	StageState   string // state of the release stage
}

// ReleaseQueries are the api reads of the Release (consolidation and blocks).
type ReleaseQueries struct {
	projects  appproject.Repository
	decisions DecisionStore
	progress  *Progress
	now       func() time.Time
}

// NewReleaseQueries builds the queries over the read ports of the gate engine.
func NewReleaseQueries(projects appproject.Repository, store Store, decisions DecisionStore, release ReleaseStore, repo ReleaseRepository,
	definition domainworkflow.Definition) *ReleaseQueries {
	p := &Progress{store: store, definition: definition, release: &releaseParts{store: release, repo: repo}, now: time.Now}
	return &ReleaseQueries{projects: projects, decisions: decisions, progress: p, now: time.Now}
}

func (q *ReleaseQueries) facts(ctx context.Context, projectID string) (*evaluation, ReleaseFacts, error) {
	stage, _ := q.progress.definition.Stage(ReleaseStage)
	e := &evaluation{p: q.progress, projectID: projectID, stage: stage}
	if err := e.load(ctx); err != nil {
		return nil, ReleaseFacts{}, err
	}
	facts, err := e.releaseFacts(ctx)
	return e, facts, err
}

// Blocks lists what keeps the Release from being approved now: critical or high findings open without risk
// acceptance, acceptances past their validity, stages not approved and active waves (US-08 CA-2, CA-6, CA-7).
func (q *ReleaseQueries) Blocks(ctx context.Context, projectID string) ([]ReleaseBlock, error) {
	e, facts, err := q.facts(ctx, projectID)
	if err != nil {
		return nil, err
	}
	return q.blocks(e, facts), nil
}

func (q *ReleaseQueries) blocks(e *evaluation, facts ReleaseFacts) []ReleaseBlock {
	blocks := []ReleaseBlock{}
	now := q.now()
	for _, f := range e.findings {
		switch {
		case f.Blocking():
			blocks = append(blocks, ReleaseBlock{Type: BlockFindingWithoutAcceptance, Reference: f.Code})
		case f.AcceptanceExpired(now):
			blocks = append(blocks, ReleaseBlock{Type: BlockAcceptanceExpired, Reference: f.Code, ValidUntil: f.ValidUntil})
		}
	}
	for _, g := range facts.Gates {
		if g.State != domainworkflow.StageApproved || g.Reevaluation {
			if stage, ok := q.progress.definition.Stage(g.Stage); ok {
				blocks = append(blocks, ReleaseBlock{Type: BlockGateNotApproved, Reference: stage.Gate})
			}
		}
	}
	for _, w := range facts.ActiveWaves {
		blocks = append(blocks, ReleaseBlock{Type: BlockActiveWave, Reference: w})
	}
	return blocks
}

// Summary is getReleaseSummary: the consolidation, even partial while the release stage is blocked (PD-14).
func (q *ReleaseQueries) Summary(ctx context.Context, principal identity.Principal, projectID string) (ReleaseSummary, error) {
	if _, err := appproject.Authorize(ctx, q.projects, principal, projectID); err != nil {
		return ReleaseSummary{}, err
	}
	e, facts, err := q.facts(ctx, projectID)
	if err != nil {
		return ReleaseSummary{}, err
	}
	summary := ReleaseSummary{Accepted: facts.Accepted, OpenFindings: facts.Active, Cost: facts.Cost, Lost: facts.Lost, Currency: facts.Currency,
		Blocks: q.blocks(e, facts), Tests: facts.Tests}
	if facts.RelQA != nil {
		summary.Report = facts.RelQA.Code
	}
	if summary.Accepted == nil {
		summary.Accepted = []domainfinding.Finding{}
	}
	if summary.OpenFindings == nil {
		summary.OpenFindings = []domainfinding.Finding{}
	}
	for _, stage := range q.progress.definition.Stages {
		item := SummaryGate{Gate: stage.Gate, Stage: stage.ID}
		rounds, err := q.decisions.Rounds(ctx, projectID, stage.ID)
		if err != nil {
			return ReleaseSummary{}, err
		}
		if len(rounds) > 0 {
			item.Round = &rounds[0]
		}
		summary.Gates = append(summary.Gates, item)
		if stage.ID == ReleaseStage {
			record, err := q.progress.store.Stage(ctx, projectID, stage.ID)
			if err != nil {
				return ReleaseSummary{}, err
			}
			summary.StageState = record.State
		}
	}
	return summary, nil
}
