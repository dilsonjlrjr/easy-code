// Package permission is the least-privilege policy of an agent (RNF-07, US-30, permissoes.yaml): the
// explicit list of tools, read and write paths and command categories, without wildcard tools. The
// harness checks every tool call against it in code (never trusting the model, RNF-05) and the sandbox
// runner checks the command again.
package permission

import (
	"path"
	"strings"
	"time"

	"easycode/internal/domain/sandbox"
)

// Tools the platform knows (names of permissoes.yaml and of the wave.tool_called event).
const (
	ToolReadFile       = "read_file"
	ToolListFiles      = "list_files"
	ToolWriteFile      = "write_file"
	ToolExecuteCommand = "execute_command"
	ToolAskUser        = "ask_user"
	ToolFinish         = "finish"
)

// Refusal reasons (tool_executions.refusal_reason, wave.tool_called).
const (
	RefusedTool            = "tool_not_allowed"
	RefusedPath            = "path_not_allowed"
	RefusedCommand         = "command_not_allowed"
	RefusedNetwork         = "network_not_allowed"
	RefusedGit             = "git_not_allowed"
	RefusedForeignArtifact = "foreign_artifact_write"
)

// Policy is the policy of one agent.
type Policy struct {
	Agent      string
	ReadPaths  []string // globs relative to the repository root; ** = any subfolder
	WritePaths []string
	AskUser    bool
	Execute    bool
	Categories []string                    // command categories the agent may use
	Commands   map[string]sandbox.Category // command_categories of permissoes.yaml
	Ceiling    sandbox.Limits              // limits of the agent profile (nil limits = zero)
	// TrackingPaths are artifacts of other agents (or approved ones of its own) whose tracking fields the
	// agent updates (permissoes.yaml writes, operation tracking_fields: the T of the Dev, CT.execucao).
	TrackingPaths []string
}

// CanTrack reports whether the agent may update the tracking fields of the artifact at path.
func (p Policy) CanTrack(clean string) bool { return matchAny(p.TrackingPaths, clean) }

// Tools returns the tools the agent may call, in a stable order. finish is always offered.
func (p Policy) Tools() []string {
	var tools []string
	if len(p.ReadPaths) > 0 {
		tools = append(tools, ToolReadFile, ToolListFiles)
	}
	if len(p.WritePaths) > 0 {
		tools = append(tools, ToolWriteFile)
	}
	if p.Execute && len(p.Categories) > 0 {
		tools = append(tools, ToolExecuteCommand)
	}
	if p.AskUser {
		tools = append(tools, ToolAskUser)
	}
	return append(tools, ToolFinish)
}

// Allows reports whether the tool is in the agent list.
func (p Policy) Allows(tool string) bool {
	for _, allowed := range p.Tools() {
		if allowed == tool {
			return true
		}
	}
	return false
}

// CanRead reports whether the clean relative path matches a read glob (writes are readable too).
func (p Policy) CanRead(clean string) bool {
	return matchAny(p.ReadPaths, clean) || matchAny(p.WritePaths, clean)
}

// CanWrite reports whether the clean relative path matches a write glob.
func (p Policy) CanWrite(clean string) bool { return matchAny(p.WritePaths, clean) }

// Category returns the command category when the agent may use it.
func (p Policy) Category(name string) (sandbox.Category, bool) {
	for _, allowed := range p.Categories {
		if allowed == name {
			category, ok := p.Commands[name]
			return category, ok
		}
	}
	return sandbox.Category{}, false
}

// CleanPath normalizes a path the model gave: relative, no "..", no backslash, no empty segment.
func CleanPath(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.Contains(raw, "\\") || strings.ContainsRune(raw, 0) {
		return "", false
	}
	clean := path.Clean(strings.TrimPrefix(raw, "/"))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", false
	}
	return clean, true
}

func matchAny(globs []string, clean string) bool {
	for _, glob := range globs {
		if Match(glob, clean) {
			return true
		}
	}
	return false
}

// Match matches a permissoes.yaml glob: "**" spans any number of segments (also none), "*" any run of
// characters inside one segment; everything else is literal.
func Match(glob, name string) bool {
	return matchSegments(strings.Split(glob, "/"), strings.Split(name, "/"))
}

func matchSegments(glob, name []string) bool {
	for len(glob) > 0 {
		if glob[0] == "**" {
			rest := glob[1:]
			for i := 0; i <= len(name); i++ {
				if matchSegments(rest, name[i:]) {
					return true
				}
			}
			return false
		}
		if len(name) == 0 {
			return false
		}
		if ok, _ := path.Match(glob[0], name[0]); !ok {
			return false
		}
		glob, name = glob[1:], name[1:]
	}
	return len(name) == 0
}

// ParseDuration reads the ISO 8601 durations of permissoes.yaml (PT2M, PT15M, PT1H30M).
func ParseDuration(iso string) time.Duration {
	rest, ok := strings.CutPrefix(strings.ToUpper(iso), "PT")
	if !ok {
		return 0
	}
	var total time.Duration
	number := 0
	for _, r := range rest {
		switch {
		case r >= '0' && r <= '9':
			number = number*10 + int(r-'0')
		case r == 'H':
			total, number = total+time.Duration(number)*time.Hour, 0
		case r == 'M':
			total, number = total+time.Duration(number)*time.Minute, 0
		case r == 'S':
			total, number = total+time.Duration(number)*time.Second, 0
		default:
			return 0
		}
	}
	return total
}
