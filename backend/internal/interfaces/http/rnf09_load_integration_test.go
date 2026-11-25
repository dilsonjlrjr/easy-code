package http_test

import (
	"github.com/jackc/pgx/v5/pgxpool"

	"fmt"
	"math/rand/v2"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	appartifact "easycode/internal/application/artifact"
	appcost "easycode/internal/application/cost"
	appproject "easycode/internal/application/project"
	appsearch "easycode/internal/application/search"
	apptrail "easycode/internal/application/trail"
	appwave "easycode/internal/application/wave"
	appworkflow "easycode/internal/application/workflow"
	"easycode/internal/infrastructure/oidc/oidctest"
	"easycode/internal/infrastructure/postgres"
	"easycode/internal/infrastructure/postgres/postgrestest"
	"easycode/internal/infrastructure/specs"
	httpapi "easycode/internal/interfaces/http"
)

// scalePool is the pool of the reads under load, for its acquire statistics.
var scalePool *pgxpool.Pool

// EnvLoadTest turns on the load test of RNF-09 (make load-test); it seeds the base of S-4 and takes minutes.
const EnvLoadTest = "EASYCODE_LOAD_TEST"

// scaleDeps wires the reads of the screens over a pool of the production size (EASYCODE_DATABASE_MAX_CONNS
// default, 10 connections).
func scaleDeps(t *testing.T) func(*httpapi.Dependencies, *postgrestest.Database) {
	return func(d *httpapi.Dependencies, db *postgrestest.Database) {
		pool := db.PoolOf("easycode_api", 10)
		scalePool = pool
		wf, err := specs.StandardWorkflow(specs.ReleaseAgents)
		if err != nil {
			t.Fatal(err)
		}
		tx := postgres.NewTxRunner(pool)
		trail := postgres.NewAuditTrail(pool)
		projects := postgres.NewProjects(pool, wf.Definition.ID, len(wf.Definition.Stages))
		workflows := postgres.NewWorkflow(pool, wf.Definition)
		waveStore := postgres.NewWaves(pool, wf.Definition)
		waves := appwave.NewService(tx, projects, waveStore, waveStore, trail)
		d.Projects = appproject.NewService(tx, projects, postgres.NewOutbox(pool), trail, wf.Definition.ID, wf.Definition.Version)
		d.Workflow = appworkflow.NewService(tx, projects, workflows, workflows, workflows, trail)
		d.Artifacts = appartifact.NewQueries(projects, postgres.NewArtifacts(pool))
		d.Waves, d.Access = waves, waves
		d.Costs = appcost.NewService(projects, postgres.NewCosts(pool, wf.Definition), wf.Definition.StageIDs())
		d.Audit = apptrail.NewQueries(projects, postgres.NewAuditReader(pool))
		d.Search = appsearch.NewService(postgres.NewSearch(pool)) // US-56
	}
}

// US-27 CA-2, US-38 CA-2, US-50 over the synthetic base: a version driven by a finding shows it, the cost of
// rework is apart, the escalated findings are counted apart and every grouping adds up.
func TestMarco7SeededVisibility(t *testing.T) {
	live := newLiveAPI(t, scaleDeps(t))
	ids := live.db.SeedScale(postgrestest.Scale{Users: 2, Projects: 10, Waves: 180, EventsPerWave: 1, AuditPerProject: 20})
	person := live.as(t, oidctest.User{Subject: "load-02", Name: "Pessoa 2"})
	base := "/api/v1/projects/" + ids[3] // Carga 004: in progress
	cmp := person.json(t, "GET", base+"/artifacts/VISAO/comparison?from=1&to=2", "", nil, 200)
	if fmt.Sprint(cmp["driving_findings"]) != "[ACH-01]" || len(cmp["body"].([]any)) < 300 {
		t.Fatalf("US-27 CA-2: %v", cmp["driving_findings"])
	}
	if versions := person.json(t, "GET", base+"/artifacts/VISAO/versions", "", nil, 200)["items"].([]any); versions[0].(map[string]any)["finding"] != "ACH-01" {
		t.Fatalf("US-27 CA-2: the version driven by the finding: %v", versions)
	}
	for _, groupBy := range []string{"stage", "agent", "model", "provider"} {
		costs := person.json(t, "GET", base+"/costs?group_by="+groupBy, "", nil, 200)
		total := costs["total"].(map[string]any)
		var groups, rework []string
		for _, g := range costs["groups"].([]any) {
			c := g.(map[string]any)["cost"].(map[string]any)
			groups = append(groups, money(t, c["total"]))
			rework = append(rework, money(t, c["rework"]))
		}
		if sum(t, groups...) != money(t, total["total"]) || sum(t, rework...) != money(t, total["rework"]) || money(t, total["rework"]) == "0" ||
			money(t, total["lost"]) == "0" {
			t.Fatalf("US-38 CA-1, CA-2 %s: %v", groupBy, costs)
		}
		if groupBy == "provider" && len(total["per_provider"].([]any)) != 2 {
			t.Fatalf("US-24 CA-3: %v", total["per_provider"])
		}
	}
	rework := person.json(t, "GET", base+"/costs", "", nil, 200)["rework"].(map[string]any)
	if rework["waves"] == float64(0) || fmt.Sprint(rework["findings"]) != "[ACH-01]" {
		t.Fatalf("US-38 CA-2: %v", rework)
	}
	board := person.json(t, "GET", "/api/v1/projects?limit=100", "", nil, 200)
	if len(board["items"].([]any)) != 10 || board["count_by_state"].(map[string]any)["in_progress"] != float64(6) {
		t.Fatalf("US-50 CA-1: %v", board["count_by_state"])
	}
	for _, item := range board["items"].([]any) {
		p := item.(map[string]any)
		if p["id"] == ids[3] {
			if f := p["summary"].(map[string]any)["open_findings"].(map[string]any); f["escalated"] != float64(1) || f["high"] != float64(1) {
				t.Fatalf("US-50: open findings: %v", f)
			}
		}
	}
	trail := person.json(t, "GET", base+"/audit?action=gate.reject", "", nil, 200)["items"].([]any)
	if len(trail) == 0 || trail[0].(map[string]any)["reason"] != "Motivo sintético da devolução" {
		t.Fatalf("US-35: %v", trail)
	}
	if e := person.json(t, "GET", base+"/stages/design/estimate", "", nil, 200); e["wave"] == nil || e["performed"] == nil {
		t.Fatalf("US-40 CA-3: %v", e)
	}
	// Carga 010 was not started: its first estimate uses the history of the stage in the organization (capped).
	if e := person.json(t, "GET", "/api/v1/projects/"+ids[9]+"/stages/requirements/estimate", "", nil, 200); e["no_history"] != false ||
		e["basis"].(map[string]any)["considered_waves"] != float64(20) || e["performed"] != nil {
		t.Fatalf("US-40 CA-1 over the history of the organization: %v", e)
	}
}

type sample struct {
	operation string
	write     bool
	elapsed   time.Duration
	status    int
}

func percentile(values []time.Duration, p float64) time.Duration {
	if len(values) == 0 {
		return 0
	}
	slices.Sort(values)
	index := int(float64(len(values))*p+0.999999) - 1
	return values[max(0, min(index, len(values)-1))]
}

// thinkTime is the mean pause of a person between two requests (each person works in a screen, not in a loop).
const thinkTime = time.Second

// TestRNF09LoadAtScaleS4 measures RNF-09: 50 people at once over 100 projects and 20,000 waves; every read
// of the screens (dashboard, project, costs, estimate, trail, comparison, artifact, waves, workflow, home
// card, header search of US-56) answers within 500 ms at p95 and the write (createProject) within 1 s. The model providers are out
// of the measure (no model call is made). The 50 people work at the same time, each with a think time between
// requests (about 50 requests per second in all), after one unmeasured round that warms the api. Run it with
// make load-test.
func TestRNF09LoadAtScaleS4(t *testing.T) {
	if os.Getenv(EnvLoadTest) == "" {
		t.Skip(EnvLoadTest + " not set (make load-test)")
	}
	live := newLiveAPI(t, scaleDeps(t))
	seeded := time.Now()
	ids := live.db.SeedScale(postgrestest.ScaleS4)
	t.Logf("base of S-4 seeded in %s: %d projects", time.Since(seeded).Round(time.Second), len(ids))
	var startedIDs []string
	for i, id := range ids {
		if (i+1)%10 != 0 { // Carga 010, 020… stay created
			startedIDs = append(startedIDs, id)
		}
	}
	people := make([]liveAPI, postgrestest.ScaleS4.Users)
	for i := range people {
		people[i] = live.as(t, oidctest.User{Subject: fmt.Sprintf("load-%02d", i+1), Name: fmt.Sprintf("Pessoa %d", i+1)})
	}
	rounds := 4
	var keys atomic.Int64
	var mu sync.Mutex
	var samples []sample
	var wg sync.WaitGroup
	begin := time.Now()
	for i, person := range people {
		wg.Add(1)
		go func(n int, person liveAPI) {
			defer wg.Done()
			random := rand.New(rand.NewPCG(uint64(n), 7))
			warm := true
			measure := func(operation string, write bool, method, path, body string, headers map[string]string) {
				at := time.Now()
				rc := person.send(method, path, body, headers)
				mu.Lock()
				if !warm {
					samples = append(samples, sample{operation: operation, write: write, elapsed: time.Since(at), status: rc.Response.StatusCode()})
				}
				mu.Unlock()
				// A person reads the screen before the next action: think time of 0.5 to 1.5 s.
				time.Sleep(thinkTime/2 + time.Duration(random.Int64N(int64(thinkTime))))
			}
			// People do not all press the first key in the same millisecond: each starts within one think time.
			time.Sleep(time.Duration(random.Int64N(int64(thinkTime))))
			// The first round warms the api (connections of the pool, prepared statements and their plans, caches)
			// and is not measured: RNF-09 is about the platform in use, not right after a restart.
			for r := -1; r < rounds; r++ {
				warm = r < 0
				base := "/api/v1/projects/" + startedIDs[random.IntN(len(startedIDs))]
				measure("listProjects", false, "GET", "/api/v1/projects", "", nil)
				measure("listProjects?state", false, "GET", "/api/v1/projects?state=in_progress", "", nil)
				measure("getProject", false, "GET", base, "", nil)
				measure("getWorkflow", false, "GET", base+"/workflow", "", nil)
				measure("listWaves", false, "GET", base+"/waves", "", nil)
				measure("getProjectCosts", false, "GET", base+"/costs", "", nil)
				measure("getProjectCosts?model", false, "GET", base+"/costs?group_by=model", "", nil)
				measure("getStageEstimate", false, "GET", base+"/stages/design/estimate", "", nil)
				measure("listProjectAudit", false, "GET", base+"/audit?order=desc", "", nil)
				measure("compareArtifactVersions", false, "GET", base+"/artifacts/VISAO/comparison?from=1&to=2", "", nil)
				measure("getArtifact", false, "GET", base+"/artifacts/VISAO", "", nil)
				measure("getUserCosts", false, "GET", "/api/v1/me/costs", "", nil)
				measure("searchPlatform", false, "GET", "/api/v1/search?search=carga%200", "", nil)
				measure("searchPlatform?id", false, "GET", "/api/v1/search?search=ACH-01", "", nil)
				body := strings.Replace(newProjectBody, "Reserva de salas", fmt.Sprintf("Escrita %02d-%d", n, r+1), 1)
				key := map[string]string{"Idempotency-Key": fmt.Sprintf("0193a5c0-44aa-7e01-b2c9-%012d", 900000+keys.Add(1))}
				measure("createProject", true, "POST", "/api/v1/projects", body, key)
			}
		}(i, person)
	}
	wg.Wait()
	elapsed := time.Since(begin)
	byOperation := map[string][]time.Duration{}
	var reads, writes []time.Duration
	for _, s := range samples {
		if (s.write && s.status != 201) || (!s.write && s.status != 200) {
			t.Errorf("%s answered %d", s.operation, s.status)
		}
		byOperation[s.operation] = append(byOperation[s.operation], s.elapsed)
		if s.write {
			writes = append(writes, s.elapsed)
		} else {
			reads = append(reads, s.elapsed)
		}
	}
	names := make([]string, 0, len(byOperation))
	for name := range byOperation {
		names = append(names, name)
	}
	slices.Sort(names)
	t.Logf("RNF-09: %d requests by %d people in %s", len(samples), len(people), elapsed.Round(time.Millisecond))
	for _, name := range names {
		values := byOperation[name]
		limit := 500 * time.Millisecond
		if name == "createProject" {
			limit = time.Second
		}
		p95 := percentile(values, 0.95)
		t.Logf("  %-26s n=%-4d p50=%-8s p95=%-8s max=%s", name, len(values), percentile(values, 0.5).Round(time.Millisecond),
			p95.Round(time.Millisecond), percentile(values, 1).Round(time.Millisecond))
		if p95 > limit {
			t.Errorf("RNF-09: %s p95 %s > %s", name, p95.Round(time.Millisecond), limit)
		}
	}
	stat := scalePool.Stat()
	if stat.AcquireCount() > 0 {
		t.Logf("  pool: %d acquires, %d waited for a connection, mean wait %s", stat.AcquireCount(), stat.EmptyAcquireCount(),
			(stat.AcquireDuration() / time.Duration(stat.AcquireCount())).Round(time.Microsecond))
	}
	t.Logf("  reads p95=%s, writes p95=%s", percentile(reads, 0.95).Round(time.Millisecond), percentile(writes, 0.95).Round(time.Millisecond))
}
