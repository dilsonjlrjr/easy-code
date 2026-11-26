package specs_test

import (
	"strings"
	"testing"

	domainartifact "easycode/internal/domain/artifact"
	"easycode/internal/infrastructure/specs"
)

const visaoHeader = `id: VISAO
tipo: VISAO
titulo: Visão da reserva de salas
autor: requirements-agent
onda: REQ-01
versao: 1
estado: rascunho
vinculos: []
historico:
- versao: 1
  data: 2026-11-22
  onda: REQ-01
  descricao: criação
problema: Reservas por planilha geram conflito.
atores:
- {nome: Funcionário, quem_e: quem reserva, necessidade: reservar sem conflito}
objetivo: Reservar salas pelo navegador.
fora_de_escopo: [catering]
glossario:
- {termo: Sala, definicao: espaço reservável}
`

func TestValidHeaderPassesItsSchema(t *testing.T) {
	validator, err := specs.NewArtifactValidator()
	if err != nil {
		t.Fatal(err)
	}
	header, err := specs.HeaderParser{}.ParseHeader(visaoHeader)
	if err != nil {
		t.Fatal(err)
	}
	if errors, err := validator.Validate("VISAO", header); err != nil || len(errors) != 0 {
		t.Fatalf("valid header: %+v %v", errors, err)
	}
}

// US-29 CA-1: a header without a required field gets the list of schema errors.
func TestHeaderWithoutRequiredFieldListsTheErrors(t *testing.T) {
	validator, err := specs.NewArtifactValidator()
	if err != nil {
		t.Fatal(err)
	}
	header, err := specs.HeaderParser{}.ParseHeader(visaoHeader)
	if err != nil {
		t.Fatal(err)
	}
	delete(header, "objetivo")
	header["estado"] = "pronto"
	errors, err := validator.Validate("VISAO", header)
	if err != nil || len(errors) < 2 {
		t.Fatalf("want errors for objetivo and estado, got %+v %v", errors, err)
	}
	paths := map[string]bool{}
	for _, e := range errors {
		paths[e.Path] = true
		if e.Message == "" {
			t.Fatal("each error has a message")
		}
	}
	if !paths["/estado"] || !paths[""] {
		t.Fatalf("errors point to the field: %+v", errors)
	}
}

// Every artifact type has its template, the mold the agents receive (US-29).
func TestEveryArtifactTypeHasItsTemplate(t *testing.T) {
	for _, artifactType := range domainartifact.Types {
		text, err := specs.Template(artifactType)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(text, "tipo: "+artifactType) {
			t.Errorf("template of %s without its header mold", artifactType)
		}
	}
}

// One invalid field does not report the valid fields of the header as "false schema" (cascade of
// allOf + unevaluatedProperties); a property the type does not have is reported as not allowed.
func TestSchemaErrorsLeaveOutTheFalseSchemaCascade(t *testing.T) {
	validator, err := specs.NewArtifactValidator()
	if err != nil {
		t.Fatal(err)
	}
	header, err := specs.HeaderParser{}.ParseHeader(visaoHeader)
	if err != nil {
		t.Fatal(err)
	}
	header["atores"] = []any{map[string]any{"nome": "Funcionário", "descricao": "quem reserva"}}
	errors, err := validator.Validate("VISAO", header)
	if err != nil || len(errors) == 0 {
		t.Fatalf("want errors, got %+v %v", errors, err)
	}
	for _, e := range errors {
		if e.Message == "false schema" || e.Path == "/problema" || e.Path == "/glossario" {
			t.Fatalf("cascade error reported: %+v", errors)
		}
	}
	header, _ = specs.HeaderParser{}.ParseHeader(visaoHeader)
	header["extra"] = "x"
	errors, err = validator.Validate("VISAO", header)
	if err != nil || len(errors) != 1 || errors[0].Path != "/extra" || errors[0].Message != "property not allowed" {
		t.Fatalf("extra property: %+v %v", errors, err)
	}
}
