// Package sandbox is the local API of easycode-sandbox (interno.md section 2.2): HTTP/1.1 + JSON over a
// Unix socket shared only with the worker, and the worker client of it. The fixed profile is never a
// parameter: the request carries only project, wave, agent, logical image, category, argv, directory,
// allowed environment names and limits up to the ceiling.
package sandbox

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	appsandbox "easycode/internal/application/sandbox"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/sandbox"
)

const maxWorkspaceArchive = 512 << 20

// Server exposes the runner.
type Server struct {
	runner *appsandbox.Runner
	logger *slog.Logger
}

// NewServer builds the handler.
func NewServer(runner *appsandbox.Runner, logger *slog.Logger) *Server {
	return &Server{runner: runner, logger: logger}
}

// CollectedFile is one file of the collection (content in base64).
type CollectedFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// Collection is the answer of collect_output.
type Collection struct {
	Files   []CollectedFile      `json:"files"`
	Refused []appsandbox.Refused `json:"refused"`
}

// Handler returns the routes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/health", func(w http.ResponseWriter, r *http.Request) {
		images, err := s.runner.Health(r.Context())
		if err != nil {
			s.problem(w, err)
			return
		}
		s.json(w, map[string]any{"status": "ok", "images": images})
	})
	mux.HandleFunc("GET /v1/images", func(w http.ResponseWriter, r *http.Request) {
		images, err := s.runner.Health(r.Context())
		if err != nil {
			s.problem(w, err)
			return
		}
		s.json(w, images)
	})
	mux.HandleFunc("PUT /v1/workspaces/{projectId}", func(w http.ResponseWriter, r *http.Request) {
		archive, err := io.ReadAll(io.LimitReader(r.Body, maxWorkspaceArchive+1))
		if err != nil || len(archive) > maxWorkspaceArchive {
			s.problem(w, failure.New(failure.KindInvalidInput, sandbox.ProblemInvalidRequest, err))
			return
		}
		if err := s.runner.PrepareWorkspace(r.PathValue("projectId"), archive); err != nil {
			s.problem(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("DELETE /v1/workspaces/{projectId}", func(w http.ResponseWriter, r *http.Request) {
		s.runner.DestroyWorkspace(r.PathValue("projectId"))
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /v1/workspaces/{projectId}/collection", func(w http.ResponseWriter, r *http.Request) {
		files, refused, err := s.runner.Collect(r.PathValue("projectId"))
		if err != nil {
			s.problem(w, err)
			return
		}
		out := Collection{Files: []CollectedFile{}, Refused: refused}
		for p, content := range files {
			out.Files = append(out.Files, CollectedFile{Path: p, Content: base64.StdEncoding.EncodeToString(content)})
		}
		s.json(w, out)
	})
	mux.HandleFunc("POST /v1/executions", func(w http.ResponseWriter, r *http.Request) { s.execute(w, r, false) })
	mux.HandleFunc("POST /v1/dependencies", func(w http.ResponseWriter, r *http.Request) { s.execute(w, r, true) })
	mux.HandleFunc("GET /v1/executions/{executionId}", func(w http.ResponseWriter, r *http.Request) {
		result, err := s.runner.Result(r.PathValue("executionId"))
		if err != nil {
			s.problem(w, err)
			return
		}
		s.json(w, result)
	})
	mux.HandleFunc("DELETE /v1/executions/{executionId}", func(w http.ResponseWriter, r *http.Request) {
		if err := s.runner.Abort(r.PathValue("executionId")); err != nil {
			s.problem(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /v1/executions/{executionId}/output", func(w http.ResponseWriter, r *http.Request) {
		output, err := s.runner.Output(r.PathValue("executionId"))
		if err != nil {
			s.problem(w, err)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		_, _ = w.Write(output) //nolint:gosec // G705: plain text to the worker over the Unix socket, never rendered by a browser

	})
	return mux
}

func (s *Server) execute(w http.ResponseWriter, r *http.Request, dependencies bool) {
	var request sandbox.ExecutionRequest
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	decoder.DisallowUnknownFields() // no field for a docker flag can sneak in (ADR-06)
	if err := decoder.Decode(&request); err != nil {
		s.problem(w, failure.New(failure.KindInvalidInput, sandbox.ProblemInvalidRequest, err))
		return
	}
	run := s.runner.Execute
	if dependencies {
		run = s.runner.InstallDependencies
	}
	result, err := run(r.Context(), request)
	if err != nil {
		s.problem(w, err)
		return
	}
	s.logger.InfoContext(r.Context(), "sandbox execution finished", slog.String("execution", result.ExecutionID), slog.String("agent", request.Agent),
		slog.String("category", request.Category), slog.String("end_reason", result.EndReason), slog.Int64("duration_ms", result.Duration.Milliseconds()))
	s.json(w, result)
}

func (s *Server) json(w http.ResponseWriter, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(body)
}

func (s *Server) problem(w http.ResponseWriter, err error) {
	var classified *failure.Error
	if !errors.As(err, &classified) {
		classified = failure.New(failure.KindInternal, sandbox.ProblemUnavailable, err)
	}
	status := map[failure.Kind]int{failure.KindInvalidInput: 400, failure.KindForbidden: 403, failure.KindNotFound: 404,
		failure.KindConflict: 409, failure.KindUnavailable: 503}[classified.Kind]
	if status == 0 {
		status = http.StatusInternalServerError
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"type": "urn:easycode:problem:" + classified.Code, "status": status})
}
