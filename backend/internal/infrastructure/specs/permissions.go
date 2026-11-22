package specs

import (
	"encoding/json"
	"fmt"

	"easycode/internal/domain/permission"
	"easycode/internal/domain/sandbox"
)

type permissionsDocument struct {
	CommandCategories map[string]sandbox.Category `json:"command_categories"`
	Agents            []struct {
		Agent   string `json:"agent"`
		AskUser bool   `json:"ask_user"`
		Tools   struct {
			ReadFile       []string `json:"read_file"`
			WriteFile      []string `json:"write_file"`
			ExecuteCommand struct {
				Allowed    bool     `json:"allowed"`
				Categories []string `json:"categories"`
			} `json:"execute_command"`
		} `json:"tools"`
		Limits *struct {
			CPU         float64 `json:"cpu"`
			MemoryMiB   int     `json:"memory_mib"`
			DiskMiB     int     `json:"disk_mib"`
			CommandTime string  `json:"command_time"`
		} `json:"limits"`
	} `json:"agents"`
}

func loadPermissions() (permissionsDocument, error) {
	var doc permissionsDocument
	raw, err := files.ReadFile("data/governanca/permissoes.yaml")
	if err != nil {
		return doc, err
	}
	encoded, err := YAMLToJSON(raw)
	if err != nil {
		return doc, fmt.Errorf("permissoes.yaml: %w", err)
	}
	if err := json.Unmarshal(encoded, &doc); err != nil {
		return doc, fmt.Errorf("permissoes.yaml: %w", err)
	}
	return doc, nil
}

// AgentPolicy returns the least-privilege policy of an agent from permissoes.yaml (RNF-07, US-30).
func AgentPolicy(agent string) (permission.Policy, error) {
	doc, err := loadPermissions()
	if err != nil {
		return permission.Policy{}, err
	}
	for _, entry := range doc.Agents {
		if entry.Agent != agent {
			continue
		}
		policy := permission.Policy{
			Agent: agent, ReadPaths: entry.Tools.ReadFile, WritePaths: entry.Tools.WriteFile, AskUser: entry.AskUser,
			Execute: entry.Tools.ExecuteCommand.Allowed, Categories: entry.Tools.ExecuteCommand.Categories, Commands: doc.CommandCategories,
		}
		if entry.Limits != nil {
			policy.Ceiling = sandbox.Limits{CPU: entry.Limits.CPU, MemoryMiB: entry.Limits.MemoryMiB, DiskMiB: entry.Limits.DiskMiB,
				Time: permission.ParseDuration(entry.Limits.CommandTime)}
		}
		return policy, nil
	}
	return permission.Policy{}, fmt.Errorf("agent %s not in permissoes.yaml", agent)
}

// CommandCategories returns command_categories of permissoes.yaml (the runner checks argv again, S-05).
func CommandCategories() (map[string]sandbox.Category, error) {
	doc, err := loadPermissions()
	return doc.CommandCategories, err
}
