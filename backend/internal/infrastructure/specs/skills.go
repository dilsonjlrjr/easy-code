package specs

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"

	appskill "easycode/internal/application/skill"
	"easycode/internal/domain/skill"
)

const skillsRoot = "data/skills"

// nativeAgents maps the folders of the native catalog to the agents that use them (agentes/skills/README.md,
// "Valor de {SKILLS} por agente"); a language skill serves every agent that works in that language.
var nativeAgents = []struct {
	prefix string
	agents []string
}{
	{"requisitos/", []string{"requirements-agent"}},
	{"arquitetura/arquitetura-web", []string{"architect-agent"}},
	{"arquitetura/padroes-arquitetura-codigo", []string{"architect-agent", "developer-agent"}},
	{"arquitetura/ambiente-docker", []string{"architect-agent", "dba-agent", "developer-agent"}},
	{"dados/modelagem-postgresql", []string{"dba-agent", "developer-agent"}},
	{"dados/", []string{"dba-agent"}},
	{"design/", []string{"designer-agent"}},
	{"dev/estrategia-testes", []string{"developer-agent", "qa-agent"}},
	{"dev/", []string{"developer-agent"}},
	{"qa/", []string{"qa-agent"}},
	{"seguranca/", []string{"security-agent"}},
	{"linguagens/vue", []string{"architect-agent", "designer-agent", "developer-agent", "qa-agent"}},
	{"linguagens/", []string{"architect-agent", "dba-agent", "developer-agent", "qa-agent"}},
}

// NativeSkills reads the native catalog (agentes/skills): one package per folder with a SKILL.md, named
// and described by its front matter, with the agents of the catalog table and their stages (permissoes.yaml).
func NativeSkills() ([]skill.Package, error) {
	stages, err := agentStages()
	if err != nil {
		return nil, err
	}
	var packages []skill.Package
	err = fs.WalkDir(files, skillsRoot, func(p string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || path.Base(p) != "SKILL.md" {
			return err
		}
		dir := path.Dir(p)
		pkg, err := readSkill(dir)
		if err != nil {
			return err
		}
		folder := strings.TrimPrefix(dir, skillsRoot+"/")
		for _, rule := range nativeAgents {
			if strings.HasPrefix(folder, rule.prefix) {
				pkg.Agents = rule.agents
				break
			}
		}
		seen := map[string]bool{}
		for _, agent := range pkg.Agents {
			for _, stage := range stages[agent] {
				if !seen[stage] {
					seen[stage] = true
					pkg.Stages = append(pkg.Stages, stage)
				}
			}
		}
		sort.Strings(pkg.Stages)
		packages = append(packages, pkg)
		return nil
	})
	sort.Slice(packages, func(i, j int) bool { return packages[i].Name < packages[j].Name })
	return packages, err
}

func readSkill(dir string) (skill.Package, error) {
	pkg := skill.Package{Files: map[string]string{}}
	err := fs.WalkDir(files, dir, func(p string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		raw, err := files.ReadFile(p)
		if err != nil {
			return err
		}
		pkg.Files[strings.TrimPrefix(p, dir+"/")] = string(raw)
		return nil
	})
	if err != nil {
		return pkg, err
	}
	_, header, ok := strings.Cut(pkg.Files["SKILL.md"], "---\n")
	if ok {
		header, _, ok = strings.Cut(header, "\n---")
	}
	if !ok {
		return pkg, fmt.Errorf("%s/SKILL.md without front matter", dir)
	}
	encoded, err := YAMLToJSON([]byte(header))
	if err != nil {
		return pkg, fmt.Errorf("%s/SKILL.md: %w", dir, err)
	}
	var front struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(encoded, &front); err != nil || front.Name == "" || front.Description == "" {
		return pkg, fmt.Errorf("%s/SKILL.md needs name and description", dir)
	}
	pkg.Name, pkg.Description = front.Name, front.Description
	return pkg, nil
}

// agentStages are the stages and activities of each agent (permissoes.yaml).
func agentStages() (map[string][]string, error) {
	raw, err := files.ReadFile("data/governanca/permissoes.yaml")
	if err != nil {
		return nil, err
	}
	encoded, err := YAMLToJSON(raw)
	if err != nil {
		return nil, err
	}
	var doc struct {
		Agents []struct {
			Agent      string   `json:"agent"`
			Stages     []string `json:"stages"`
			Activities []string `json:"activities"`
		} `json:"agents"`
	}
	if err := json.Unmarshal(encoded, &doc); err != nil {
		return nil, err
	}
	out := map[string][]string{}
	for _, a := range doc.Agents {
		out[a.Agent] = append(append([]string{}, a.Stages...), a.Activities...)
	}
	return out, nil
}

// AgentProfile is what the agent page shows of an agent (ProjectAgent): name from the instruction title,
// stages and the artifact types it delivers (permissoes.yaml).
type AgentProfile struct {
	Agent    string
	Name     string
	Role     string   // first paragraph of the instruction ("Você é…", PL-25)
	Limits   []string // prohibitions of permissoes.yaml (what the agent does not do)
	Stages   []string
	Delivery []string
}

// Profile returns the profile of an executor agent.
func Profile(agent string) (AgentProfile, error) {
	defaults, err := DefaultAgent(agent)
	if err != nil {
		return AgentProfile{}, err
	}
	profile := AgentProfile{Agent: agent}
	title, _, _ := strings.Cut(string(defaults.Instruction), "\n")
	title = strings.TrimSpace(strings.TrimPrefix(title, "#"))
	if name, _, ok := strings.Cut(title, " — "); ok {
		title = name
	}
	profile.Name = title
	if _, body, ok := strings.Cut(string(defaults.Instruction), "\n---\n"); ok {
		for _, paragraph := range strings.Split(body, "\n\n") {
			if paragraph = strings.TrimSpace(paragraph); strings.HasPrefix(paragraph, "Você é") {
				profile.Role = strings.Join(strings.Fields(paragraph), " ")
				break
			}
		}
	}
	raw, err := files.ReadFile("data/governanca/permissoes.yaml")
	if err != nil {
		return profile, err
	}
	encoded, err := YAMLToJSON(raw)
	if err != nil {
		return profile, err
	}
	var doc struct {
		Agents []struct {
			Agent        string   `json:"agent"`
			Stages       []string `json:"stages"`
			Activities   []string `json:"activities"`
			Prohibitions []string `json:"prohibitions"`
			Writes       []struct {
				Type      string `json:"type"`
				Operation string `json:"operation"`
			} `json:"writes"`
		} `json:"agents"`
	}
	if err := json.Unmarshal(encoded, &doc); err != nil {
		return profile, err
	}
	for _, a := range doc.Agents {
		if a.Agent != agent {
			continue
		}
		profile.Stages = append(append([]string{}, a.Stages...), a.Activities...)
		profile.Limits = a.Prohibitions
		seen := map[string]bool{}
		for _, w := range a.Writes {
			if w.Type != "" && w.Operation != "own_review_opinion" && !seen[w.Type] {
				seen[w.Type] = true
				profile.Delivery = append(profile.Delivery, w.Type)
			}
		}
	}
	return profile, nil
}

// ConfigurableAgents are the executor agents of the platform (ExecutorAgent).
var ConfigurableAgents = []string{"requirements-agent", "architect-agent", "dba-agent", "designer-agent", "developer-agent", "qa-agent", "security-agent"}

// AgentProfiles implements appskill.Profiles from the embedded instructions and permissoes.yaml.
type AgentProfiles struct{}

// Agents implements appskill.Profiles.
func (AgentProfiles) Agents() []string { return ConfigurableAgents }

// Profile implements appskill.Profiles.
func (AgentProfiles) Profile(agent string) (appskill.Profile, error) {
	profile, err := Profile(agent)
	if err != nil {
		return appskill.Profile{}, err
	}
	defaults, err := DefaultAgent(agent)
	if err != nil {
		return appskill.Profile{}, err
	}
	return appskill.Profile{Agent: agent, Name: profile.Name, Role: profile.Role, Prohibitions: profile.Limits, Stages: profile.Stages,
		Delivery: profile.Delivery, InstructionVersion: defaults.InstructionVersion, InstructionHash: defaults.InstructionHash,
		Tools: defaults.Tools, Limits: defaults.Limits}, nil
}
