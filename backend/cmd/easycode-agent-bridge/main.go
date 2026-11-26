// Command easycode-agent-bridge (agent-bridge) runs inside an agent session container of easycode-sandbox
// (ADR-23, US-58). The container has no network: "exec" listens on a loopback TCP address, where Claude
// Code sends its model calls, forwards every connection to the Unix socket of the wave model proxy of
// the worker and runs Claude Code. "guard" is the PreToolUse hook of Claude Code: it refuses writes
// outside the paths the agent may write. "mcp" is the MCP stdio server of the platform: its permission tool
// is the --permission-prompt-tool of Claude Code and asks the worker, through the same socket, whether a
// tool call is allowed (ADR-24). "version" prints the bridge version. Static binary, no
// dependency on the image of the container.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

const (
	workspace   = "/workspace"
	denialsFile = workspace + "/.easycode/denials.jsonl"
	// guardRefused is the exit code that makes Claude Code block the tool call and show stderr to the model.
	guardRefused = 2
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: agent-bridge exec|guard|mcp|version")
		return 2
	}
	switch args[0] {
	case "version":
		fmt.Fprintln(stdout, version)
		return 0
	case "guard":
		return guard(stdin, stderr, os.Getenv("EASYCODE_WRITABLE"), workspace, denialsFile)
	case "exec":
		return execute(args[1:], stdin, stdout, stderr)
	case "mcp":
		return serveMCP(stdin, stdout, newPermissionClient(os.Getenv("ANTHROPIC_BASE_URL"), os.Getenv("ANTHROPIC_AUTH_TOKEN")))
	}
	fmt.Fprintf(stderr, "agent-bridge: unknown command %q\n", args[0])
	return 2
}

// execute is "exec --listen A --socket S -- cmd...": the listener is open before the command starts.
func execute(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("exec", flag.ContinueOnError)
	flags.SetOutput(stderr)
	listen := flags.String("listen", "127.0.0.1:8099", "loopback TCP address Claude Code calls")
	socket := flags.String("socket", "", "Unix socket of the wave model proxy")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	command := flags.Args()
	if *socket == "" || len(command) == 0 {
		fmt.Fprintln(stderr, "usage: agent-bridge exec --listen ADDR --socket PATH -- command [args...]")
		return 2
	}
	listener, err := net.Listen("tcp", *listen)
	if err != nil {
		fmt.Fprintf(stderr, "agent-bridge: listen: %v\n", err)
		return 1
	}
	defer listener.Close()
	go forward(listener, *socket)
	return runCommand(command, stdin, stdout, stderr)
}

// forward copies every accepted connection to a new connection of the Unix socket, both ways.
func forward(listener net.Listener, socket string) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		go func(conn net.Conn) {
			defer conn.Close()
			upstream, err := (&net.Dialer{}).DialContext(context.Background(), "unix", socket)
			if err != nil {
				return
			}
			defer upstream.Close()
			done := make(chan struct{})
			go func() {
				_, _ = io.Copy(upstream, conn)
				if unix, ok := upstream.(*net.UnixConn); ok {
					_ = unix.CloseWrite()
				}
				close(done)
			}()
			_, _ = io.Copy(conn, upstream)
			if tcp, ok := conn.(*net.TCPConn); ok {
				_ = tcp.CloseWrite()
			}
			<-done
		}(conn)
	}
}

// runCommand runs the child with the standard streams of the bridge, passes on SIGTERM and SIGINT and
// returns its exit code (128 + signal when killed).
func runCommand(command []string, stdin io.Reader, stdout, stderr io.Writer) int {
	child := exec.Command(command[0], command[1:]...) //nolint:gosec // G204: the fixed command line built by the runner
	child.Stdin, child.Stdout, child.Stderr = stdin, stdout, stderr
	if err := child.Start(); err != nil {
		fmt.Fprintf(stderr, "agent-bridge: start: %v\n", err)
		return 127
	}
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(signals)
	go func() {
		for s := range signals {
			_ = child.Process.Signal(s)
		}
	}()
	err := child.Wait()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if status, ok := exitErr.Sys().(syscall.WaitStatus); ok && status.Signaled() {
			return 128 + int(status.Signal())
		}
		return exitErr.ExitCode()
	}
	if err != nil {
		return 1
	}
	return 0
}

// hookInput is the part of the PreToolUse hook input the guard reads.
type hookInput struct {
	ToolName  string         `json:"tool_name"`
	ToolInput map[string]any `json:"tool_input"`
}

// denial is one line of .easycode/denials.jsonl, read by the worker as a refused tool call.
type denial struct {
	Tool   string `json:"tool"`
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// guard allows a tool call without a path or with a path inside one of the writable prefixes (separated
// by ':'); otherwise it records the denial and exits with guardRefused. A path with "..", or one whose
// existing part resolves through a symbolic link out of the prefix, is refused.
func guard(stdin io.Reader, stderr io.Writer, writable, root, denials string) int {
	var input hookInput
	if err := json.NewDecoder(io.LimitReader(stdin, 1<<20)).Decode(&input); err != nil {
		fmt.Fprintln(stderr, "easycode: the tool call could not be checked")
		return guardRefused
	}
	target := ""
	for _, key := range []string{"file_path", "notebook_path"} {
		if value, ok := input.ToolInput[key].(string); ok && value != "" {
			target = value
			break
		}
	}
	if target == "" {
		return 0
	}
	clean := target
	if !filepath.IsAbs(clean) {
		clean = filepath.Join(root, clean)
	}
	clean = filepath.Clean(clean)
	if allowed(target, clean, writable) {
		return 0
	}
	if f, err := os.OpenFile(denials, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600); err == nil { //nolint:gosec // G304: fixed path in the workspace
		_ = json.NewEncoder(f).Encode(denial{Tool: input.ToolName, Path: clean, Reason: "path_not_allowed"})
		_ = f.Close()
	}
	fmt.Fprintf(stderr, "easycode: write to %s is not allowed for this agent\n", clean)
	return guardRefused
}

func allowed(raw, clean, writable string) bool {
	for _, part := range strings.Split(filepath.ToSlash(raw), "/") {
		if part == ".." {
			return false
		}
	}
	resolved := resolve(clean)
	for _, prefix := range strings.Split(writable, ":") {
		if prefix == "" || !filepath.IsAbs(prefix) {
			continue
		}
		prefix = filepath.Clean(prefix)
		if within(clean, prefix) && within(resolved, resolve(prefix)) {
			return true
		}
	}
	return false
}

// within reports whether p is the prefix itself or below it.
func within(p, prefix string) bool {
	return p != "" && (p == prefix || prefix == "/" || strings.HasPrefix(p, prefix+string(filepath.Separator)))
}

// resolve follows the symbolic links of the longest existing part of p and appends the rest ("" when
// that part is a dangling link).
func resolve(p string) string {
	rest := ""
	current := p
	for {
		if resolved, err := filepath.EvalSymlinks(current); err == nil {
			return filepath.Join(resolved, rest)
		}
		if _, err := os.Lstat(current); err == nil { //nolint:gosec // G703: only inspected, the hook never opens it
			return "" // a dangling link: where the write would land is unknown
		}
		parent := filepath.Dir(current)
		if parent == current {
			return p
		}
		rest = filepath.Join(filepath.Base(current), rest)
		current = parent
	}
}
