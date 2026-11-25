// Package docker is the minimal Docker Engine API client of easycode-sandbox (ADR-06), the only
// process with the Docker socket. It speaks HTTP over the Unix socket of the daemon and exposes only
// what the fixed sandbox profile needs: image digest, create, start, exec, inspect and remove.
package docker

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/textproto"
	"net/url"
	"strings"

	appsandbox "easycode/internal/application/sandbox"
)

// apiVersion 1.45 (Docker Engine 26) is the first with VolumeOptions.Subpath, used by the agent session
// mounts (ADR-23).
const apiVersion = "/v1.45"

// Client talks to the daemon over its Unix socket.
type Client struct {
	socket string
	http   *http.Client
}

var _ appsandbox.Engine = (*Client)(nil)

// New builds the client of the daemon socket (usually /var/run/docker.sock).
func New(socket string) *Client {
	dial := func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}
	return &Client{socket: socket, http: &http.Client{Transport: &http.Transport{DialContext: dial, DisableCompression: true}}}
}

func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://docker"+apiVersion+path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("docker %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return appsandbox.ErrNotFound
	}
	if resp.StatusCode >= 300 && resp.StatusCode != http.StatusNotModified {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("docker %s %s: %d %s", method, path, resp.StatusCode, strings.TrimSpace(string(detail)))
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

// Ping implements appsandbox.Engine.
func (c *Client) Ping(ctx context.Context) error {
	return c.do(ctx, http.MethodGet, "/_ping", nil, nil)
}

// ImageDigest implements appsandbox.Engine: the content-addressed id of the local image.
func (c *Client) ImageDigest(ctx context.Context, ref string) (string, error) {
	var image struct {
		ID string `json:"Id"`
	}
	if err := c.do(ctx, http.MethodGet, "/images/"+url.PathEscape(ref)+"/json", nil, &image); err != nil {
		return "", err
	}
	return image.ID, nil
}

// Create implements appsandbox.Engine with the hardened profile of ADR-05: no network (or only the
// network of the spec for the dependency action), read-only root, tmpfs work dirs, no capability,
// no new privileges, default seccomp, non-root user and cgroup limits.
func (c *Client) Create(ctx context.Context, spec appsandbox.ContainerSpec) (string, error) {
	network := spec.Network
	if network == "" {
		network = "none"
	}
	hostConfig := map[string]any{
		"NetworkMode": network, "ReadonlyRootfs": true, "CapDrop": []string{"ALL"}, "SecurityOpt": []string{"no-new-privileges"},
		"PidsLimit": spec.Processes, "Memory": int64(spec.MemoryMiB) << 20, "MemorySwap": int64(spec.MemoryMiB) << 20,
		"NanoCpus": int64(spec.CPU * 1e9), "Tmpfs": spec.Tmpfs, "Privileged": false, "AutoRemove": false,
		"Ulimits": []map[string]any{{"Name": "nofile", "Soft": 1024, "Hard": 1024}, {"Name": "core", "Soft": 0, "Hard": 0}},
	}
	if spec.Runtime != "" {
		hostConfig["Runtime"] = spec.Runtime
	}
	if len(spec.Mounts) > 0 {
		hostConfig["Mounts"] = mounts(spec.Mounts)
	}
	body := map[string]any{
		"Image": spec.Image, "Cmd": spec.Cmd, "User": spec.User, "Env": spec.Env, "WorkingDir": spec.WorkingDir, "Labels": spec.Labels,
		"NetworkDisabled": network == "none", "HostConfig": hostConfig, "AttachStdout": false, "AttachStderr": false, "Tty": false,
	}
	var created struct {
		ID string `json:"Id"`
	}
	if err := c.do(ctx, http.MethodPost, "/containers/create?name="+url.QueryEscape(spec.Name), body, &created); err != nil {
		return "", err
	}
	return created.ID, nil
}

// mounts turns the mounts of the spec into the HostConfig.Mounts of the Engine API: a bind of a host
// path or a subdirectory of a named volume (never copied into, never created by the daemon).
func mounts(specs []appsandbox.Mount) []map[string]any {
	out := make([]map[string]any, 0, len(specs))
	for _, m := range specs {
		mount := map[string]any{"Type": m.Type, "Source": m.Source, "Target": m.Target, "ReadOnly": m.ReadOnly}
		switch m.Type {
		case appsandbox.MountBind:
			mount["BindOptions"] = map[string]any{"CreateMountpoint": false}
		case appsandbox.MountVolume:
			options := map[string]any{"NoCopy": true}
			if m.Subpath != "" {
				options["Subpath"] = m.Subpath
			}
			mount["VolumeOptions"] = options
		}
		out = append(out, mount)
	}
	return out
}

// Start implements appsandbox.Engine.
func (c *Client) Start(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodPost, "/containers/"+id+"/start", nil, nil)
}

// Remove implements appsandbox.Engine (docker rm -f, with anonymous volumes).
func (c *Client) Remove(ctx context.Context, id string) error {
	err := c.do(ctx, http.MethodDelete, "/containers/"+id+"?force=true&v=true", nil, nil)
	if errors.Is(err, appsandbox.ErrNotFound) {
		return nil
	}
	return err
}

// Inspect implements appsandbox.Engine.
func (c *Client) Inspect(ctx context.Context, id string) (appsandbox.ContainerState, error) {
	var inspected struct {
		State struct {
			Running   bool `json:"Running"`
			OOMKilled bool `json:"OOMKilled"`
		} `json:"State"`
		HostConfig struct {
			NetworkMode    string `json:"NetworkMode"`
			ReadonlyRootfs bool   `json:"ReadonlyRootfs"`
			Memory         int64  `json:"Memory"`
			NanoCpus       int64  `json:"NanoCpus"`
			PidsLimit      int64  `json:"PidsLimit"`
		} `json:"HostConfig"`
	}
	if err := c.do(ctx, http.MethodGet, "/containers/"+id+"/json", nil, &inspected); err != nil {
		return appsandbox.ContainerState{}, err
	}
	return appsandbox.ContainerState{Running: inspected.State.Running, OOMKilled: inspected.State.OOMKilled,
		Network: inspected.HostConfig.NetworkMode, ReadOnlyRoot: inspected.HostConfig.ReadonlyRootfs, MemoryBytes: inspected.HostConfig.Memory,
		NanoCPUs: inspected.HostConfig.NanoCpus, Processes: inspected.HostConfig.PidsLimit}, nil
}

// Exec implements appsandbox.Engine: runs a process in the container, writes stdin (when given) and
// copies stdout and stderr, interleaved, to out. It returns the exit code.
func (c *Client) Exec(ctx context.Context, id string, spec appsandbox.ExecSpec, stdin io.Reader, out io.Writer) (int, error) {
	var created struct {
		ID string `json:"Id"`
	}
	err := c.do(ctx, http.MethodPost, "/containers/"+id+"/exec", map[string]any{
		"AttachStdin": stdin != nil, "AttachStdout": true, "AttachStderr": true, "Tty": false,
		"Cmd": spec.Cmd, "Env": spec.Env, "WorkingDir": spec.WorkingDir, "User": spec.User,
	}, &created)
	if err != nil {
		return -1, err
	}
	if err := c.attach(ctx, created.ID, stdin, out); err != nil {
		return -1, err
	}
	var inspected struct {
		ExitCode int  `json:"ExitCode"`
		Running  bool `json:"Running"`
	}
	if err := c.do(context.WithoutCancel(ctx), http.MethodGet, "/exec/"+created.ID+"/json", nil, &inspected); err != nil {
		return -1, err
	}
	return inspected.ExitCode, nil
}

// attach starts the exec on a hijacked connection (the Engine API upgrades it to a raw stream).
func (c *Client) attach(ctx context.Context, execID string, stdin io.Reader, out io.Writer) error {
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", c.socket)
	if err != nil {
		return err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	body := `{"Detach":false,"Tty":false}`
	request := "POST " + apiVersion + "/exec/" + execID + "/start HTTP/1.1\r\nHost: docker\r\nContent-Type: application/json\r\n" +
		"Connection: Upgrade\r\nUpgrade: tcp\r\nContent-Length: " + fmt.Sprint(len(body)) + "\r\n\r\n" + body
	if _, err := io.WriteString(conn, request); err != nil {
		return err
	}
	reader := bufio.NewReader(conn)
	status, err := textproto.NewReader(reader).ReadLine()
	if err != nil {
		return err
	}
	if !strings.Contains(status, " 101 ") && !strings.Contains(status, " 200 ") {
		return fmt.Errorf("docker exec start: %s", status)
	}
	if _, err := textproto.NewReader(reader).ReadMIMEHeader(); err != nil {
		return err
	}
	written := make(chan error, 1)
	go func() {
		var err error
		if stdin != nil {
			_, err = io.Copy(conn, stdin)
		}
		if unix, ok := conn.(*net.UnixConn); ok {
			_ = unix.CloseWrite()
		}
		written <- err
	}()
	if err := demultiplex(reader, out); err != nil && ctx.Err() == nil {
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return <-written
}

// demultiplex copies the multiplexed stream (8-byte frame header: stream, 0, 0, 0, size) to out.
func demultiplex(reader io.Reader, out io.Writer) error {
	header := make([]byte, 8)
	for {
		if _, err := io.ReadFull(reader, header); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return nil
			}
			return err
		}
		size := int64(binary.BigEndian.Uint32(header[4:]))
		if _, err := io.CopyN(out, reader, size); err != nil {
			return err
		}
	}
}
