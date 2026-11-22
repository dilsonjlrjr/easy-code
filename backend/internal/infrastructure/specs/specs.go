// Package specs embeds the domain specifications the binaries run on: the Phase 1 workflow
// (specs/dominio/workflow/workflow-basico.yaml), the artifact JSON schemas, the agent permissions
// and the agent instructions. The files under data/ are copies kept equal to the repository
// sources by "make check-specs" (part of make ci), so builds stay reproducible from project/backend.
package specs

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"strings"

	"easycode/internal/domain/workflow"
)

//go:embed data
var files embed.FS

// Workflow is the embedded workflow definition with its canonical JSON and hash (RNF-15:
// the project pins this copy at start).
type Workflow struct {
	Definition workflow.Definition
	JSON       []byte
	Hash       string
}

// BasicWorkflow loads workflow-basico.yaml (Phase 1, PD-01).
func BasicWorkflow() (Workflow, error) {
	raw, err := files.ReadFile("data/workflow/workflow-basico.yaml")
	if err != nil {
		return Workflow{}, err
	}
	encoded, err := YAMLToJSON(raw)
	if err != nil {
		return Workflow{}, fmt.Errorf("workflow-basico.yaml: %w", err)
	}
	var definition workflow.Definition
	if err := json.Unmarshal(encoded, &definition); err != nil {
		return Workflow{}, fmt.Errorf("workflow-basico.yaml: %w", err)
	}
	if err := definition.Validate(); err != nil {
		return Workflow{}, err
	}
	sum := sha256.Sum256(encoded)
	return Workflow{Definition: definition, JSON: encoded, Hash: hex.EncodeToString(sum[:])}, nil
}

// AgentDefaults is the platform default configuration of an agent (agent_configurations
// version 1): its instruction file and the tools and limits of permissoes.yaml.
type AgentDefaults struct {
	Agent              string
	InstructionPath    string
	InstructionVersion string
	InstructionHash    string
	Instruction        []byte
	Tools              json.RawMessage
	Limits             json.RawMessage
}

type permissions struct {
	Agents []struct {
		Agent       string          `json:"agent"`
		Instruction string          `json:"instruction"`
		Tools       json.RawMessage `json:"tools"`
		Limits      json.RawMessage `json:"limits"`
	} `json:"agents"`
}

// DefaultAgent returns the default configuration of an executor agent.
func DefaultAgent(agent string) (AgentDefaults, error) {
	raw, err := files.ReadFile("data/governanca/permissoes.yaml")
	if err != nil {
		return AgentDefaults{}, err
	}
	encoded, err := YAMLToJSON(raw)
	if err != nil {
		return AgentDefaults{}, fmt.Errorf("permissoes.yaml: %w", err)
	}
	var doc permissions
	if err := json.Unmarshal(encoded, &doc); err != nil {
		return AgentDefaults{}, fmt.Errorf("permissoes.yaml: %w", err)
	}
	for _, entry := range doc.Agents {
		if entry.Agent != agent {
			continue
		}
		instruction, err := files.ReadFile("data/agentes/" + path.Base(entry.Instruction))
		if err != nil {
			return AgentDefaults{}, fmt.Errorf("instruction of %s: %w", agent, err)
		}
		sum := sha256.Sum256(instruction)
		hash := hex.EncodeToString(sum[:])
		limits := entry.Limits
		if len(limits) == 0 || string(limits) == "null" {
			limits = json.RawMessage(`{}`)
		}
		return AgentDefaults{
			Agent: agent, InstructionPath: entry.Instruction, InstructionVersion: entry.Instruction + "@" + hash[:12],
			InstructionHash: hash, Instruction: instruction, Tools: entry.Tools, Limits: limits,
		}, nil
	}
	return AgentDefaults{}, fmt.Errorf("agent %s not in permissoes.yaml", agent)
}

// ReferenceNeed is the business need of the reference project (agentes/referencia/reserva-de-salas.md):
// the text after the "---" line, the input of the end-to-end regression (US-17 CA-4).
func ReferenceNeed() (string, error) {
	raw, err := files.ReadFile("data/referencia/reserva-de-salas.md")
	if err != nil {
		return "", err
	}
	_, need, ok := strings.Cut(string(raw), "\n---\n")
	if !ok {
		return "", fmt.Errorf("reserva-de-salas.md without the --- line")
	}
	return strings.TrimSpace(need), nil
}
