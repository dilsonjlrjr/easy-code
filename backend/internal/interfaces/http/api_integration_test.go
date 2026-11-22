package http_test

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/valyala/fasthttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace/noop"

	"easycode/internal/application/idempotency"
	apppricing "easycode/internal/application/pricing"
	appproject "easycode/internal/application/project"
	"easycode/internal/application/session"
	"easycode/internal/infrastructure/i18n"
	"easycode/internal/infrastructure/logging"
	"easycode/internal/infrastructure/postgres"
	"easycode/internal/infrastructure/postgres/postgrestest"
	"easycode/internal/infrastructure/websession"
	httpapi "easycode/internal/interfaces/http"
)

// liveAPI is the api handler over a real PostgreSQL with the api role grants.
type liveAPI struct {
	harness
	db     *postgrestest.Database
	cookie string
}

func newLiveAPI(t *testing.T, options ...func(*httpapi.Dependencies, *postgrestest.Database)) liveAPI {
	t.Helper()
	db := postgrestest.New(t)
	if err := postgres.SeedDevIdentity(context.Background(), db.Admin, "dev", "Dev"); err != nil {
		t.Fatal(err)
	}
	pool := db.PoolAs("easycode_api")
	translator, err := i18n.NewTranslator()
	if err != nil {
		t.Fatal(err)
	}
	logs := &strings.Builder{}
	tx := postgres.NewTxRunner(pool)
	trail := postgres.NewAuditTrail(pool)
	deps := httpapi.Dependencies{
		Logger: logging.New(logs, slog.LevelDebug, "test", "dev"), Translator: translator, Health: fakeHealth{},
		Tracer: noop.NewTracerProvider().Tracer("test"), Propagator: propagation.TraceContext{},
		Sessions:     session.NewService(postgres.NewUsers(pool), websession.NewCodec(strings.Repeat("s", 32)), "dev"),
		Idempotency:  idempotency.NewGuard(tx, postgres.NewIdempotencyKeys(pool)),
		PublicOrigin: "https://easycode.test",
		Pricing:      apppricing.NewService(tx, postgres.NewCatalog(pool), trail),
		Projects:     appproject.NewService(tx, postgres.NewProjects(pool, "workflow-basico", 3), postgres.NewOutbox(pool), trail, "workflow-basico", 1),
	}
	for _, option := range options {
		option(&deps, db)
	}
	live := liveAPI{harness: harness{handler: httpapi.NewHandler(deps), logText: logs}, db: db}
	live.cookie = sessionCookie(t, live.harness)
	return live
}

func (l liveAPI) send(method, path, body string, headers map[string]string) *fasthttp.RequestCtx {
	rc := &fasthttp.RequestCtx{}
	rc.Request.Header.SetMethod(method)
	rc.Request.SetRequestURI(path)
	rc.Request.Header.Set("Cookie", l.cookie)
	rc.Request.Header.Set("Origin", "https://easycode.test")
	if body != "" {
		rc.Request.Header.SetContentType("application/json")
		rc.Request.SetBodyString(body)
	}
	for k, v := range headers {
		rc.Request.Header.Set(k, v)
	}
	l.handler(rc)
	return rc
}

const newProjectBody = `{"name":"Reserva de salas","business_need":"Reservar salas sem conflito de horário.",
 "provider_submission_acknowledgment":true,"budget":{"stop_limit":{"value":"200.00","currency":"BRL"}}}`

// US-03 CA-1 over HTTP, ADR-17: repeating the same Idempotency-Key replays the answer and
// creates one project; another body with the key is a conflict; no key is 428.
func TestCreateProjectIsIdempotent(t *testing.T) {
	live := newLiveAPI(t)
	key := map[string]string{"Idempotency-Key": "0193a5c0-44aa-7e01-b2c9-7f3d1e6a9b40"}
	first := live.send(fasthttp.MethodPost, "/api/v1/projects", newProjectBody, key)
	if first.Response.StatusCode() != fasthttp.StatusCreated || !strings.HasPrefix(string(first.Response.Header.Peek("Location")), "/api/v1/projects/") {
		t.Fatalf("create: %d %s", first.Response.StatusCode(), first.Response.Body())
	}
	second := live.send(fasthttp.MethodPost, "/api/v1/projects", newProjectBody, key)
	if second.Response.StatusCode() != fasthttp.StatusCreated || string(second.Response.Body()) != string(first.Response.Body()) {
		t.Fatalf("replay must give the same answer: %d", second.Response.StatusCode())
	}
	if n := count(t, live.db, "SELECT count(*) FROM easycode.projects"); n != 1 {
		t.Fatalf("one project, got %d", n)
	}
	other := live.send(fasthttp.MethodPost, "/api/v1/projects", strings.Replace(newProjectBody, "Reserva", "Outra", 1), key)
	if other.Response.StatusCode() != fasthttp.StatusConflict || decode(t, other)["type"] != "urn:easycode:problem:idempotency-conflict" {
		t.Fatalf("other body: %d %s", other.Response.StatusCode(), other.Response.Body())
	}
	missing := live.send(fasthttp.MethodPost, "/api/v1/projects", newProjectBody, nil)
	if missing.Response.StatusCode() != fasthttp.StatusPreconditionRequired {
		t.Fatalf("no key: %d", missing.Response.StatusCode())
	}
	body := decode(t, first)
	if body["state"] != "created" || body["workflow"] != "workflow-basico" || body["summary"].(map[string]any)["stages_total"] != float64(3) {
		t.Fatalf("project body: %v", body)
	}
	got := live.send(fasthttp.MethodGet, "/api/v1/projects/"+body["id"].(string), "", nil)
	if got.Response.StatusCode() != fasthttp.StatusOK {
		t.Fatalf("get: %d", got.Response.StatusCode())
	}
	list := decode(t, live.send(fasthttp.MethodGet, "/api/v1/projects?state=created", "", nil))
	if len(list["items"].([]any)) != 1 || list["next_cursor"] != nil {
		t.Fatalf("list: %v", list)
	}
}

// US-03 CA-2: the empty need is pointed by field.
func TestCreateProjectPointsTheMissingNeed(t *testing.T) {
	live := newLiveAPI(t)
	rc := live.send(fasthttp.MethodPost, "/api/v1/projects", strings.Replace(newProjectBody, "Reservar salas sem conflito de horário.", "", 1),
		map[string]string{"Idempotency-Key": "0193a5c0-44aa-7e01-b2c9-7f3d1e6a9b41", "Accept-Language": "en"})
	problem := decode(t, rc)
	errors, _ := problem["errors"].([]any)
	if rc.Response.StatusCode() != fasthttp.StatusBadRequest || len(errors) != 1 || errors[0].(map[string]any)["field"] != "business_need" {
		t.Fatalf("CA-2: %d %v", rc.Response.StatusCode(), problem)
	}
}

func count(t *testing.T, db *postgrestest.Database, sql string) int {
	t.Helper()
	var n int
	if err := db.Admin.QueryRow(context.Background(), sql).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
