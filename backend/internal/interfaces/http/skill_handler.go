package http

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strconv"
	"time"

	"github.com/valyala/fasthttp"

	appproject "easycode/internal/application/project"
	appskill "easycode/internal/application/skill"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/identity"
	domainskill "easycode/internal/domain/skill"
)

// SkillUseCases are the skill library (US-43 CA-1).
type SkillUseCases interface {
	List(ctx context.Context, principal identity.Principal, filter appskill.Filter) ([]appskill.Summary, error)
	Get(ctx context.Context, principal identity.Principal, name string) (appskill.Summary, error)
}

// AgentUseCases are the agents of a project and their configuration (US-43, US-25).
type AgentUseCases interface {
	List(ctx context.Context, principal identity.Principal, projectID string) ([]appskill.Agent, error)
	Get(ctx context.Context, principal identity.Principal, projectID, agent string) (appskill.Agent, error)
	Versions(ctx context.Context, principal identity.Principal, projectID, agent string) ([]appskill.Configuration, error)
	Modify(ctx context.Context, principal identity.Principal, projectID, agent string, version int, change appskill.NewConfiguration) (appskill.Agent, error)
}

type skillVersionJSON struct {
	Version          int       `json:"version"`
	PublishedAt      time.Time `json:"published_at"`
	Status           string    `json:"status"`
	SubmissionOrigin *string   `json:"submission_origin,omitempty"`
	SHA256           string    `json:"sha256"`
}

type skillJSON struct {
	ID              string             `json:"id"`
	Name            string             `json:"name"`
	Description     string             `json:"description"`
	Origin          string             `json:"origin"`
	Stages          []string           `json:"stages"`
	Agents          []string           `json:"agents"`
	CurrentVersion  int                `json:"current_version"`
	EstimatedTokens int                `json:"estimated_tokens"`
	Status          string             `json:"status"`
	Versions        []skillVersionJSON `json:"versions"`
}

func skillOf(s appskill.Summary) skillJSON {
	out := skillJSON{ID: s.Name, Name: s.Name, Description: s.Description, Origin: s.Origin, Stages: s.Stages, Agents: s.Agents,
		CurrentVersion: s.CurrentVersion, EstimatedTokens: s.EstimatedTokens, Status: s.Status, Versions: []skillVersionJSON{}}
	for _, v := range s.Versions {
		out.Versions = append(out.Versions, skillVersionJSON{Version: v.Version, PublishedAt: v.PublishedAt, Status: v.Status, SubmissionOrigin: optional(v.Origin), SHA256: v.SHA256})
	}
	return out
}

func (a *api) listSkills(rc *fasthttp.RequestCtx, principal identity.Principal) error {
	limit, err := queryLimit(rc)
	if err != nil {
		return err
	}
	filter := appskill.Filter{Origin: string(rc.QueryArgs().Peek("origin")), Agent: string(rc.QueryArgs().Peek("agent")),
		Stage: string(rc.QueryArgs().Peek("stage")), Limit: limit}
	if filter.Origin != "" && filter.Origin != domainskill.OriginNative && filter.Origin != domainskill.OriginOrganization {
		return failure.Invalid(failure.FieldError{Field: "origin", Message: "invalid"})
	}
	if cursor := string(rc.QueryArgs().Peek("cursor")); cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(cursor)
		if err != nil {
			return failure.Invalid(failure.FieldError{Field: "cursor", Message: "invalid"})
		}
		filter.After = string(raw)
	}
	if filter.Limit <= 0 {
		filter.Limit = 50
	}
	want := filter.Limit
	filter.Limit++
	items, err := a.deps.Skills.List(RequestContext(rc), principal, filter)
	if err != nil {
		return err
	}
	out := struct {
		Items      []skillJSON `json:"items"`
		NextCursor *string     `json:"next_cursor"`
	}{Items: []skillJSON{}}
	if len(items) > want {
		items = items[:want]
		next := base64.RawURLEncoding.EncodeToString([]byte(items[want-1].Name))
		out.NextCursor = &next
	}
	for _, s := range items {
		out.Items = append(out.Items, skillOf(s))
	}
	a.responder.JSON(rc, fasthttp.StatusOK, out)
	return nil
}

func (a *api) getSkill(rc *fasthttp.RequestCtx, principal identity.Principal) error {
	s, err := a.deps.Skills.Get(RequestContext(rc), principal, PathParam(rc, "skillId"))
	if err != nil {
		return err
	}
	a.responder.JSON(rc, fasthttp.StatusOK, skillOf(s))
	return nil
}

type pinnedSkillJSON struct {
	Skill            string `json:"skill"`
	Version          int    `json:"version"`
	AvailableVersion *int   `json:"available_version"`
}

type agentConfigurationJSON struct {
	Version             int               `json:"version"`
	InstructionsVersion string            `json:"instructions_version"`
	Tools               json.RawMessage   `json:"tools"`
	Skills              []pinnedSkillJSON `json:"skills"`
	OutputCriteria      []string          `json:"output_criteria"`
	CreatedAt           time.Time         `json:"created_at"`
	CreatedBy           *userRefJSON      `json:"created_by,omitempty"`
}

type projectAgentJSON struct {
	Agent         string                 `json:"agent"`
	Name          string                 `json:"name"`
	Stages        []string               `json:"stages"`
	Configuration agentConfigurationJSON `json:"configuration"`
	AllowedByRole json.RawMessage        `json:"allowed_by_role"`
	Role          string                 `json:"role,omitempty"`
	Limits        []string               `json:"limits"`
	Delivery      []string               `json:"delivery"`
	ModelsByStage map[string]pairJSON    `json:"models_by_stage"`
}

func agentConfigurationOf(c appskill.Configuration) agentConfigurationJSON {
	out := agentConfigurationJSON{Version: c.Version, InstructionsVersion: c.InstructionVersion, Tools: c.Tools, Skills: []pinnedSkillJSON{},
		OutputCriteria: []string{}, CreatedAt: c.CreatedAt}
	if out.Tools == nil {
		out.Tools = json.RawMessage(`{}`)
	}
	for _, s := range c.Skills {
		pinned := pinnedSkillJSON{Skill: s.Name, Version: s.Version}
		if s.Available > 0 {
			available := s.Available
			pinned.AvailableVersion = &available
		}
		out.Skills = append(out.Skills, pinned)
	}
	if c.CreatedBy != nil {
		out.CreatedBy = &userRefJSON{ID: c.CreatedBy.Subject, Name: c.CreatedBy.Name}
	}
	return out
}

func projectAgentOf(agent appskill.Agent) projectAgentJSON {
	out := projectAgentJSON{Agent: agent.Agent, Name: agent.Name, Stages: agent.Stages, Configuration: agentConfigurationOf(agent.Configuration),
		AllowedByRole: agent.AllowedByRole, Role: agent.Role, Limits: agent.Limits, Delivery: agent.Delivery, ModelsByStage: map[string]pairJSON{}}
	if out.Limits == nil {
		out.Limits = []string{}
	}
	if out.Delivery == nil {
		out.Delivery = []string{}
	}
	for stage, pair := range agent.ModelsByStage {
		out.ModelsByStage[stage] = pairOf(pair)
	}
	return out
}

func (a *api) listProjectAgents(rc *fasthttp.RequestCtx, principal identity.Principal) error {
	projectID, err := pathUUID(rc, "projectId")
	if err != nil {
		return err
	}
	agents, err := a.deps.Agents.List(RequestContext(rc), principal, projectID)
	if err != nil {
		return err
	}
	out := struct {
		Items []projectAgentJSON `json:"items"`
	}{Items: []projectAgentJSON{}}
	for _, agent := range agents {
		out.Items = append(out.Items, projectAgentOf(agent))
	}
	a.responder.JSON(rc, fasthttp.StatusOK, out)
	return nil
}

func (a *api) getProjectAgent(rc *fasthttp.RequestCtx, principal identity.Principal) error {
	projectID, err := pathUUID(rc, "projectId")
	if err != nil {
		return err
	}
	agent, err := a.deps.Agents.Get(RequestContext(rc), principal, projectID, PathParam(rc, "agent"))
	if err != nil {
		return err
	}
	setETag(rc, strconv.Itoa(agent.Configuration.Version))
	a.responder.JSON(rc, fasthttp.StatusOK, projectAgentOf(agent))
	return nil
}

func (a *api) listAgentConfigurationVersions(rc *fasthttp.RequestCtx, principal identity.Principal) error {
	projectID, err := pathUUID(rc, "projectId")
	if err != nil {
		return err
	}
	versions, err := a.deps.Agents.Versions(RequestContext(rc), principal, projectID, PathParam(rc, "agent"))
	if err != nil {
		return err
	}
	out := struct {
		Items []agentConfigurationJSON `json:"items"`
	}{Items: []agentConfigurationJSON{}}
	for _, v := range versions {
		out.Items = append(out.Items, agentConfigurationOf(v))
	}
	a.responder.JSON(rc, fasthttp.StatusOK, out)
	return nil
}

// modifyAgentConfiguration is PUT /projects/{projectId}/agents/{agent}/configuration (US-43, If-Match).
func (a *api) modifyAgentConfiguration(rc *fasthttp.RequestCtx, principal identity.Principal) error {
	projectID, err := pathUUID(rc, "projectId")
	if err != nil {
		return err
	}
	raw, err := ifMatch(rc)
	if err != nil {
		return err
	}
	version, err := strconv.Atoi(raw)
	if err != nil {
		return failure.New(failure.KindConflict, appproject.CodeFailedPrecondition, nil)
	}
	var body struct {
		AdjustmentInstructions *string         `json:"adjustment_instructions"`
		Tools                  json.RawMessage `json:"tools"`
		Skills                 []struct {
			Skill   string `json:"skill"`
			Version int    `json:"version"`
		} `json:"skills"`
	}
	if err := decodeJSON(rc, &body); err != nil {
		return err
	}
	if len(body.Tools) == 0 || body.Skills == nil {
		return failure.Invalid(failure.FieldError{Field: "tools", Message: "required"})
	}
	if body.AdjustmentInstructions != nil && *body.AdjustmentInstructions != "" {
		// The adjustment of the instructions is US-25 (MARCO-8).
		return failure.Invalid(failure.FieldError{Field: "adjustment_instructions", Message: "unsupported"})
	}
	change := appskill.NewConfiguration{Tools: body.Tools}
	for _, s := range body.Skills {
		change.Skills = append(change.Skills, appskill.Ref{Name: s.Skill, Version: s.Version})
	}
	agent, err := a.deps.Agents.Modify(RequestContext(rc), principal, projectID, PathParam(rc, "agent"), version, change)
	if err != nil {
		return err
	}
	setETag(rc, strconv.Itoa(agent.Configuration.Version))
	a.responder.JSON(rc, fasthttp.StatusOK, agentConfigurationOf(agent.Configuration))
	return nil
}
