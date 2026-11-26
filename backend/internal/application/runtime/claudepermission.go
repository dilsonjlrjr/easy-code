package runtime

import (
	"strings"

	"easycode/internal/domain/permission"
)

// Messages of a refused Claude Code tool call (ADR-24). The model reads them as the tool_result; the proxy
// classifies the refusal by the decision it recorded for the tool_use, not by the text.
const (
	deniedCommandMessage = "easycode: this command is not allowed for this agent"
	deniedToolMessage    = "easycode: this tool is not allowed for this agent"
	deniedForeignMessage = "easycode: an approved artifact of another agent is never changed; ask the user or record a finding"
)

// PermissionDecision is the answer of the platform to the permission prompt of Claude Code (ADR-24): the
// worker decides every tool call that is not pre-approved, with the same policy as the own harness.
type PermissionDecision struct {
	Allow   bool
	Reason  string // permission.Refused*, empty when allowed
	Message string // shown to the model when refused
}

func allowTool() PermissionDecision { return PermissionDecision{Allow: true} }

func refuseTool(reason, message string) PermissionDecision {
	return PermissionDecision{Reason: reason, Message: message}
}

// shellOperators are the characters that make a Bash command more than one argv; a category without
// shell (D-38) refuses them.
const shellOperators = ";&|<>`$(){}\n\\\"'*?~"

// DecidePermission decides one tool call of a Claude Code run against the agent policy (RNF-05, RNF-07):
// writes by the exact write globs, never in .easycode/ nor in an approved artifact of another agent;
// commands by the categories of the agent; any other tool is refused.
func DecidePermission(wave WaveContext, policy permission.Policy, tool string, input map[string]any) PermissionDecision {
	switch tool {
	case "Read", "Glob", "Grep", "LS":
		if policy.Allows(ToolReadFile) || policy.Allows(ToolListFiles) {
			return allowTool()
		}
		return refuseTool(permission.RefusedTool, deniedToolMessage)
	case "Write", "Edit", "MultiEdit", "NotebookEdit":
		if !policy.Allows(ToolWriteFile) {
			return refuseTool(permission.RefusedTool, deniedToolMessage)
		}
		return decideWrite(wave, policy, input)
	case "Bash":
		if !policy.Allows(ToolExecuteCommand) {
			return refuseTool(permission.RefusedTool, deniedToolMessage)
		}
		command, _ := input["command"].(string)
		return decideCommand(policy, command)
	}
	return refuseTool(permission.RefusedTool, deniedToolMessage)
}

func decideWrite(wave WaveContext, policy permission.Policy, input map[string]any) PermissionDecision {
	target := ""
	for _, key := range []string{"file_path", "notebook_path"} {
		if value, ok := input[key].(string); ok && value != "" {
			target = value
			break
		}
	}
	clean, ok := workspacePath(target)
	refused := refuseTool(permission.RefusedPath, "easycode: write to "+clipText(target, 300)+" "+deniedPathMarker)
	if !ok || strings.HasPrefix(clean+"/", agentDir) {
		return refused
	}
	if author, approved := wave.Approved[clean]; approved && author != wave.Agent && !policy.CanTrack(clean) {
		return refuseTool(permission.RefusedForeignArtifact, deniedForeignMessage)
	}
	if !policy.CanWrite(clean) && !policy.CanTrack(clean) {
		return refused
	}
	return allowTool()
}

// workspacePath turns a path of Claude Code (absolute under the workspace, or relative to it) into the clean
// relative path of the repository; a path out of the workspace is refused.
func workspacePath(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "/") {
		if raw != workspaceRoot && !strings.HasPrefix(raw, workspaceRoot+"/") {
			return "", false
		}
		raw = strings.TrimPrefix(strings.TrimPrefix(raw, workspaceRoot), "/")
	}
	for _, part := range strings.Split(raw, "/") {
		if part == ".." {
			return "", false
		}
	}
	return permission.CleanPath(raw)
}

// decideCommand allows a Bash command in a shell category (sh -c, D-38) or, without shell, a single argv
// that one category of the agent accepts.
func decideCommand(policy permission.Policy, command string) PermissionDecision {
	refused := refuseTool(permission.RefusedCommand, deniedCommandMessage+
		"; categories: "+strings.Join(policy.Categories, ", "))
	if strings.TrimSpace(command) == "" {
		return refused
	}
	argv := strings.Fields(command)
	plain := !strings.ContainsAny(command, shellOperators)
	for _, name := range policy.Categories {
		category, ok := policy.Category(name)
		if !ok {
			continue
		}
		if category.Shell && category.Allowed([]string{"sh", "-c", command}) == nil {
			return allowTool()
		}
		if plain && category.Allowed(argv) == nil {
			return allowTool()
		}
	}
	return refused
}
