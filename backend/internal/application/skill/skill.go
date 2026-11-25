// Package skill holds the use cases of skills (US-43): the library of the native catalog and of the
// organization, the skills associated with each agent of a project in a pinned version (with the notice
// of a newer version, accepted by the lead), the agent configuration that carries them, and the worker
// job that publishes the native catalog.
package skill

import (
	"context"
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"time"

	"easycode/internal/application/audit"
	appproject "easycode/internal/application/project"
	"easycode/internal/application/transaction"
	domainaudit "easycode/internal/domain/audit"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/identity"
	domainproject "easycode/internal/domain/project"
	domainskill "easycode/internal/domain/skill"
)

// Failure codes of the agent configuration (openapi modifyAgentConfiguration).
const (
	CodeToolNotAllowed = "tool-not-allowed"
	CodeBlockedSkill   = "blocked-skill"
)

// VersionInfo is a version of a skill.
type VersionInfo struct {
	Version     int
	PublishedAt time.Time
	Status      string
	Origin      string // upload | git_repository; "" for native
	SHA256      string
}

// Summary is a skill of the library (Skill).
type Summary struct {
	Name            string
	Description     string
	Origin          string
	Stages          []string
	Agents          []string
	CurrentVersion  int
	EstimatedTokens int
	Status          string
	Versions        []VersionInfo
	Files           []domainskill.Entry // the package of the current version (domainskill.Entries order)
}

// Filter narrows the library.
type Filter struct {
	Origin, Agent, Stage string
	After                string // name of the last skill of the previous page
	Limit                int
}

// Library reads the skills visible to an organization (native catalog and its own).
type Library interface {
	List(ctx context.Context, organizationID string, filter Filter) ([]Summary, error)
	Get(ctx context.Context, organizationID, name string) (Summary, error)
}

// Service is the api side of the library.
type Service struct{ library Library }

// NewService builds the service.
func NewService(library Library) *Service { return &Service{library: library} }

// List is listSkills (US-43 CA-1): name, description, stages and agents, version and estimated tokens.
func (s *Service) List(ctx context.Context, principal identity.Principal, filter Filter) ([]Summary, error) {
	if filter.Limit <= 0 || filter.Limit > 100 {
		filter.Limit = 50
	}
	return s.library.List(ctx, principal.OrganizationID, filter)
}

// Get is getSkill.
func (s *Service) Get(ctx context.Context, principal identity.Principal, name string) (Summary, error) {
	return s.library.Get(ctx, principal.OrganizationID, name)
}

// Tools is the tool section of an agent configuration (AgentTools, permissoes.yaml).
type Tools struct {
	ReadFile       []string `json:"read_file,omitempty"`
	WriteFile      []string `json:"write_file,omitempty"`
	ExecuteCommand *struct {
		Allowed    bool     `json:"allowed"`
		Categories []string `json:"categories,omitempty"`
	} `json:"execute_command,omitempty"`
	Git      []string `json:"git,omitempty"`
	Browser  bool     `json:"browser,omitempty"`
	Scanners []string `json:"scanners,omitempty"`
}

// Within reports the first tool that exceeds the role allowance (US-25 CA-3, US-30): "" when the
// configuration only narrows it.
func (t Tools) Within(allowed Tools) string {
	subset := func(items, of []string) bool {
		for _, item := range items {
			if !slices.Contains(of, item) {
				return false
			}
		}
		return true
	}
	switch {
	case !subset(t.ReadFile, allowed.ReadFile):
		return "read_file"
	case !subset(t.WriteFile, allowed.WriteFile):
		return "write_file"
	case !subset(t.Git, allowed.Git):
		return "git"
	case t.Browser && !allowed.Browser:
		return "browser"
	case !subset(t.Scanners, allowed.Scanners):
		return "scanners"
	case t.ExecuteCommand != nil && t.ExecuteCommand.Allowed && (allowed.ExecuteCommand == nil || !allowed.ExecuteCommand.Allowed):
		return "execute_command"
	case t.ExecuteCommand != nil && allowed.ExecuteCommand != nil && !subset(t.ExecuteCommand.Categories, allowed.ExecuteCommand.Categories):
		return "execute_command"
	}
	return ""
}

// Configuration is a version of the configuration of an agent in a project (AgentConfiguration).
type Configuration struct {
	Version            int
	InstructionVersion string
	// Adjustment is the text of the project added to the default instructions (US-25 CA-2); "" = none.
	Adjustment string
	// OutputCriteria are the checks of the gates of the stages of the agent (gates.yaml; read only in Phases 1-2).
	OutputCriteria []string
	Tools          json.RawMessage
	Skills         []domainskill.Pinned
	CreatedAt      time.Time
	CreatedBy      *appproject.UserRef
}

// Agent is an agent of the project (ProjectAgent).
type Agent struct {
	Agent         string
	Name          string
	Role          string
	Limits        []string
	ModelsByStage map[string]appproject.Pair
	Stages        []string
	Delivery      []string
	AllowedByRole json.RawMessage
	Configuration Configuration
}

// Profile is the platform default of an agent: name, stages, deliveries and the default configuration.
type Profile struct {
	Agent              string
	Name               string
	Role               string
	Prohibitions       []string // what the agent does not do (Agent.Limits)
	Stages             []string
	Delivery           []string
	InstructionVersion string
	InstructionHash    string
	Tools              json.RawMessage
	Limits             json.RawMessage
	OutputCriteria     []string
}

// Profiles gives the platform default of each configurable agent.
type Profiles interface {
	Agents() []string
	Profile(agent string) (Profile, error)
}

// Ref is a skill in a version asked for an agent.
type Ref struct {
	Name    string
	Version int
}

// NewConfiguration is the body of modifyAgentConfiguration.
type NewConfiguration struct {
	Tools  json.RawMessage
	Skills []Ref
	// Adjustment replaces the adjustment of the instructions: nil keeps the previous one, "" removes it.
	Adjustment *string
}

// MaxAdjustment is the size limit of the adjustment of the instructions (openapi AgentConfiguration).
const MaxAdjustment = 20000

// Configurations stores the agent configurations and the project skills.
type Configurations interface {
	// Latest returns the last configuration of the agent, or nil when the project still uses the default.
	Latest(ctx context.Context, projectID, agent string) (*Configuration, error)
	Versions(ctx context.Context, projectID, agent string) ([]Configuration, error)
	// Pinned lists the skills associated with the agent, with the newer unblocked version if any.
	Pinned(ctx context.Context, projectID, agent string) ([]domainskill.Pinned, error)
	// VersionStatus returns the state of a version of a skill visible to the organization ("" if unknown).
	VersionStatus(ctx context.Context, organizationID, name string, version int) (string, error)
	// Save writes the next version (the default first when there is none) and sets the skills of the agent.
	Save(ctx context.Context, principal identity.Principal, projectID string, profile Profile, tools json.RawMessage, skills []Ref, adjustment *string) (Configuration, error)
}

// Pairs resolves the model pair of a stage of the project (project pair or the stage adjustment, PD-21).
type Pairs interface {
	Pair(ctx context.Context, projectID, stage string) (appproject.Pair, bool, error)
}

// Agents is the api side of the agents of a project.
type Agents struct {
	tx             transaction.Runner
	projects       appproject.Repository
	profiles       Profiles
	configurations Configurations
	pairs          Pairs
	trail          audit.Trail
}

// NewAgents builds the use cases.
func NewAgents(tx transaction.Runner, projects appproject.Repository, profiles Profiles, configurations Configurations, pairs Pairs, trail audit.Trail) *Agents {
	return &Agents{tx: tx, projects: projects, profiles: profiles, configurations: configurations, pairs: pairs, trail: trail}
}

func (a *Agents) agent(ctx context.Context, projectID, name string) (Agent, error) {
	profile, err := a.profiles.Profile(name)
	if err != nil {
		return Agent{}, failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
	}
	pinned, err := a.configurations.Pinned(ctx, projectID, name)
	if err != nil {
		return Agent{}, err
	}
	out := Agent{Agent: name, Name: profile.Name, Role: profile.Role, Limits: profile.Prohibitions, Stages: profile.Stages, Delivery: profile.Delivery,
		AllowedByRole: profile.Tools, ModelsByStage: map[string]appproject.Pair{}}
	for _, stage := range profile.Stages {
		pair, ok, err := a.pairs.Pair(ctx, projectID, stage)
		if err != nil {
			return Agent{}, err
		}
		if ok {
			out.ModelsByStage[stage] = pair
		}
	}
	latest, err := a.configurations.Latest(ctx, projectID, name)
	if err != nil {
		return Agent{}, err
	}
	if latest == nil {
		latest = &Configuration{Version: 1, InstructionVersion: profile.InstructionVersion, Tools: profile.Tools}
	}
	out.Configuration = *latest
	out.Configuration.Skills = pinned
	out.Configuration.OutputCriteria = profile.OutputCriteria
	return out, nil
}

// List is listProjectAgents.
func (a *Agents) List(ctx context.Context, principal identity.Principal, projectID string) ([]Agent, error) {
	if _, err := appproject.Authorize(ctx, a.projects, principal, projectID); err != nil {
		return nil, err
	}
	var out []Agent
	for _, name := range a.profiles.Agents() {
		agent, err := a.agent(ctx, projectID, name)
		if err != nil {
			return nil, err
		}
		out = append(out, agent)
	}
	return out, nil
}

// Get is getProjectAgent (US-43 CA-3: the pinned versions and the notice of a newer one).
func (a *Agents) Get(ctx context.Context, principal identity.Principal, projectID, name string) (Agent, error) {
	if _, err := appproject.Authorize(ctx, a.projects, principal, projectID); err != nil {
		return Agent{}, err
	}
	return a.agent(ctx, projectID, name)
}

// Versions is listAgentConfigurationVersions (descending).
func (a *Agents) Versions(ctx context.Context, principal identity.Principal, projectID, name string) ([]Configuration, error) {
	if _, err := appproject.Authorize(ctx, a.projects, principal, projectID); err != nil {
		return nil, err
	}
	profile, err := a.profiles.Profile(name)
	if err != nil {
		return nil, failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
	}
	versions, err := a.configurations.Versions(ctx, projectID, name)
	if err != nil {
		return nil, err
	}
	if len(versions) == 0 {
		versions = []Configuration{{Version: 1, InstructionVersion: profile.InstructionVersion, Tools: profile.Tools}}
	}
	for i := range versions {
		versions[i].OutputCriteria = profile.OutputCriteria
	}
	return versions, nil
}

// Modify is modifyAgentConfiguration (US-43, US-25 CA-2, CA-3): the lead saves a new version with the
// tools (never beyond the role) and the skills in pinned versions; accepting a newer version of a skill
// is saving it with that version (US-43 CA-3). The waves created after it use it; earlier ones keep theirs.
func (a *Agents) Modify(ctx context.Context, principal identity.Principal, projectID, name string, version int, change NewConfiguration) (Agent, error) {
	access, err := appproject.Authorize(ctx, a.projects, principal, projectID, identity.RoleLead)
	if err != nil {
		return Agent{}, err
	}
	if access.State == domainproject.StateCompleted || access.State == domainproject.StateCanceled {
		return Agent{}, failure.New(failure.KindConflict, failure.CodeStateConflict, nil).With("current_state", access.State).With("available_actions", []string{})
	}
	profile, err := a.profiles.Profile(name)
	if err != nil {
		return Agent{}, failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
	}
	if change.Adjustment != nil {
		trimmed := strings.TrimSpace(*change.Adjustment)
		if len([]rune(trimmed)) > MaxAdjustment {
			return Agent{}, failure.Invalid(failure.FieldError{Field: "adjustment_instructions", Message: "max_length"})
		}
		change.Adjustment = &trimmed
	}
	var requested, allowed Tools
	if err := json.Unmarshal(change.Tools, &requested); err != nil {
		return Agent{}, failure.Invalid(failure.FieldError{Field: "tools", Message: "invalid"})
	}
	_ = json.Unmarshal(profile.Tools, &allowed)
	if tool := requested.Within(allowed); tool != "" {
		return Agent{}, failure.New(failure.KindConflict, CodeToolNotAllowed, nil).With("tool", tool)
	}
	seen := map[string]bool{}
	for _, ref := range change.Skills {
		if seen[ref.Name] || ref.Version < 1 {
			return Agent{}, failure.Invalid(failure.FieldError{Field: "skills", Message: "invalid"})
		}
		seen[ref.Name] = true
		status, err := a.configurations.VersionStatus(ctx, principal.OrganizationID, ref.Name, ref.Version)
		if err != nil {
			return Agent{}, err
		}
		switch status {
		case "":
			return Agent{}, failure.Invalid(failure.FieldError{Field: "skills", Message: "unknown"})
		case domainskill.StatusUnblocked:
		default:
			return Agent{}, failure.New(failure.KindConflict, CodeBlockedSkill, nil).With("skill", ref.Name)
		}
	}
	err = a.tx.InTx(ctx, func(ctx context.Context) error {
		latest, err := a.configurations.Latest(ctx, projectID, name)
		if err != nil {
			return err
		}
		current := 1
		if latest != nil {
			current = latest.Version
		}
		if current != version {
			return failure.New(failure.KindConflict, appproject.CodeFailedPrecondition, nil)
		}
		saved, err := a.configurations.Save(ctx, principal, projectID, profile, change.Tools, change.Skills, change.Adjustment)
		if err != nil {
			return err
		}
		references := make([]string, 0, len(change.Skills)+1)
		for _, ref := range change.Skills {
			references = append(references, ref.Name+"@"+strconv.Itoa(ref.Version))
		}
		if change.Adjustment != nil {
			references = append(references, "adjustment_instructions") // the text itself stays in the configuration
		}
		_, err = a.trail.Append(ctx, domainaudit.Record{
			ProjectID: projectID, Actor: domainaudit.Actor{Type: domainaudit.ActorHuman, ID: principal.Subject}, Origin: domainaudit.OriginWeb,
			Action: "agent_configuration.modify", Target: "agents/" + name, Decision: "version " + strconv.Itoa(saved.Version), References: references,
		})
		return err
	})
	if err != nil {
		return Agent{}, err
	}
	return a.agent(ctx, projectID, name)
}

// Publisher writes the native catalog into the library (worker).
type Publisher interface {
	// PublishNative inserts the skill or a new version when the content changed; it reports whether a
	// version was published.
	PublishNative(ctx context.Context, pkg domainskill.Package) (bool, error)
}

// PublishNative syncs the native catalog: a changed skill gets a new version, which projects only use
// after the lead accepts it (US-43 CA-3). It returns how many versions were published.
func PublishNative(ctx context.Context, tx transaction.Runner, publisher Publisher, packages []domainskill.Package) (int, error) {
	published := 0
	for _, pkg := range packages {
		err := tx.InTx(ctx, func(ctx context.Context) error {
			created, err := publisher.PublishNative(ctx, pkg)
			if created {
				published++
			}
			return err
		})
		if err != nil {
			return published, err
		}
	}
	return published, nil
}
