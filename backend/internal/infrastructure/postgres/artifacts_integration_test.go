package postgres_test

import (
	"context"
	"strings"
	"testing"

	appartifact "easycode/internal/application/artifact"
	appproject "easycode/internal/application/project"
	domainartifact "easycode/internal/domain/artifact"
	"easycode/internal/domain/failure"
	"easycode/internal/infrastructure/postgres"
	"easycode/internal/infrastructure/specs"
)

const visao = `---
id: VISAO
tipo: VISAO
titulo: Visão da reserva de salas
autor: requirements-agent
onda: {wave}
versao: {version}
estado: rascunho
vinculos: []
historico:
- versao: {version}
  data: 2026-11-22
  onda: {wave}
  descricao: {description}
problema: Reservas por planilha geram conflito.
atores:
- {nome: Funcionário, quem_e: quem reserva, necessidade: reservar sem conflito}
objetivo: Reservar salas pelo navegador.
fora_de_escopo: [catering]
glossario:
- {termo: Sala, definicao: espaço reservável}
---
# Visão

{body}
`

func visaoFile(wave, version, description, body string) []byte {
	return []byte(strings.NewReplacer("{wave}", wave, "{version}", version, "{description}", description, "{body}", body).Replace(visao))
}

type artifactFixture struct {
	startFixture
	store   *appartifact.Store
	queries *appartifact.Queries
}

// newArtifactFixture starts the workflow so REQ-01 exists.
func newArtifactFixture(t *testing.T) artifactFixture {
	t.Helper()
	f := newStartFixture(t)
	ctx := context.Background()
	if _, err := f.config.Modify(ctx, f.dev, f.projectID, 1, appproject.Change{ModelPair: pair("execution-model")}); err != nil {
		t.Fatal(err)
	}
	f.activeKey(t)
	f.price(t, "reasoning-model", "execution-model")
	if _, err := f.start(t, "0193a5c0-44aa-7e01-b2c9-7f3d1e6a9d01"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.dispatcher.Drain(ctx); err != nil {
		t.Fatal(err)
	}
	validator, err := specs.NewArtifactValidator()
	if err != nil {
		t.Fatal(err)
	}
	worker := f.db.PoolAs("easycode_worker")
	store := appartifact.NewStore(postgres.NewTxRunner(worker), specs.HeaderParser{}, validator, postgres.NewArtifacts(worker), postgres.NewOutbox(worker))
	queries := appartifact.NewQueries(f.repo, postgres.NewArtifacts(f.db.PoolAs("easycode_api")))
	return artifactFixture{startFixture: f, store: store, queries: queries}
}

func (f artifactFixture) put(wave string, content []byte) (appartifact.Stored, error) {
	return f.store.StoreVersion(context.Background(), appartifact.Input{
		ProjectID: f.projectID, WaveCode: wave, Agent: "requirements-agent", Path: "docs/requisitos/VISAO.md", Content: content,
	})
}

// US-29 CA-1: a file without a required field is refused with the schema errors and nothing is
// stored; CA-3: a valid file gets a version in the history.
func TestStoreRefusesOutOfSchemaAndVersionsValidArtifacts(t *testing.T) {
	f := newArtifactFixture(t)
	broken := []byte(strings.Replace(string(visaoFile("REQ-01", "1", "criação", "corpo")), "objetivo: Reservar salas pelo navegador.\n", "", 1))
	_, err := f.put("REQ-01", broken)
	classified := failure.From(err)
	errors, _ := classified.Extensions["schema_errors"].([]domainartifact.SchemaError)
	if classified.Code != domainartifact.CodeInvalid || len(errors) == 0 || !strings.Contains(errors[0].Message, "objetivo") {
		t.Fatalf("CA-1: want artifact-invalid listing objetivo, got %v %+v", err, errors)
	}
	if n := count(t, f.db, "SELECT count(*) FROM easycode.artifacts"); n != 0 {
		t.Fatal("an invalid artifact is not stored")
	}
	if _, err := f.put("REQ-01", []byte("no header here")); failure.From(err).Code != domainartifact.CodeInvalid {
		t.Fatalf("a file without front matter is invalid, got %v", err)
	}
	stored, err := f.put("REQ-01", visaoFile("REQ-01", "1", "criação", "primeira"))
	if err != nil || stored.Version != 1 || !stored.Created || stored.Code != "VISAO" {
		t.Fatalf("CA-3: %+v %v", stored, err)
	}
	// Same wave, not committed: the agent corrects in place (CA-1 "na mesma onda").
	if stored, err = f.put("REQ-01", visaoFile("REQ-01", "1", "criação", "corrigida")); err != nil || stored.Version != 1 {
		t.Fatalf("rewrite in the wave: %+v %v", stored, err)
	}
	if _, err := f.put("REQ-01", visaoFile("REQ-01", "2", "criação", "x")); failure.From(err).Code != domainartifact.CodeInvalid {
		t.Fatalf("versao must be the version being written, got %v", err)
	}
	if n := count(t, f.db, "SELECT count(*) FROM easycode.events WHERE type = 'br.easycode.artifact.created.v1' AND subject = 'artifacts/VISAO'"); n != 1 {
		t.Fatal("artifact.created once")
	}
}

// US-26 CA-1, CA-2, CA-3: three versions; the current one with header, body, state, author and
// wave; the history with date, wave and description; version 1 exactly as stored.
func TestArtifactAndVersionsAreReadable(t *testing.T) {
	f := newArtifactFixture(t)
	ctx := context.Background()
	if _, err := f.put("REQ-01", visaoFile("REQ-01", "1", "criação", "versão um")); err != nil {
		t.Fatal(err)
	}
	for i, wave := range []string{"REQ-02", "REQ-03"} {
		f.extraWave(t, wave)
		version := []string{"2", "3"}[i]
		if _, err := f.put(wave, visaoFile(wave, version, "retrabalho "+version, "versão "+version)); err != nil {
			t.Fatal(err)
		}
	}
	current, err := f.queries.Get(ctx, f.dev, f.projectID, "VISAO", 0)
	if err != nil || current.Version != 3 || current.WaveCode != "REQ-03" || current.State != "draft" || current.Author != "requirements-agent" ||
		!strings.Contains(current.Body, "versão 3") || current.Stage != "requirements" || !strings.Contains(strings.ReplaceAll(string(current.Header), " ", ""), `"tipo":"VISAO"`) {
		t.Fatalf("CA-1/CA-3: %+v %v", current, err)
	}
	versions, err := f.queries.Versions(ctx, f.dev, f.projectID, "VISAO")
	if err != nil || len(versions) != 3 || versions[0].Version != 3 || versions[2].WaveCode != "REQ-01" || versions[1].Description != "retrabalho 2" {
		t.Fatalf("CA-1 history: %+v %v", versions, err)
	}
	first, err := f.queries.Get(ctx, f.dev, f.projectID, "VISAO", 1)
	if err != nil || !strings.Contains(first.Body, "versão um") || first.Version != 1 {
		t.Fatalf("CA-2: version 1: %+v %v", first, err)
	}
	page, err := f.queries.List(ctx, f.dev, f.projectID, appartifact.Filter{Limit: 20, Types: []string{"VISAO"}})
	if err != nil || len(page.Items) != 1 || page.Items[0].Version != 3 {
		t.Fatalf("list: %+v %v", page, err)
	}
	if _, err := f.queries.Get(ctx, f.dev, f.projectID, "VISAO", 9); failure.From(err).Kind != failure.KindNotFound {
		t.Fatalf("unknown version is not found, got %v", err)
	}
	other := f.db.CreateProject("other")
	if _, err := f.queries.Versions(ctx, f.dev, other.ProjectID, "VISAO"); failure.From(err).Kind != failure.KindNotFound {
		t.Fatalf("RNF-04: other project is not found, got %v", err)
	}
}

// extraWave queues another requirements wave (as a rework wave of part 2 would).
func (f artifactFixture) extraWave(t *testing.T, code string) {
	t.Helper()
	_, err := f.db.Admin.Exec(context.Background(), `
INSERT INTO easycode.waves (project_id, stage, stage_id, code, agent, agent_configuration_id, max_retries, execution_timeout, call_timeout, currency)
SELECT w.project_id, w.stage, w.stage_id, $1, w.agent, w.agent_configuration_id, 3, '1 hour', '10 minutes', 'BRL'
  FROM easycode.waves w WHERE w.code = 'REQ-01'`, code)
	if err != nil {
		t.Fatal(err)
	}
}
