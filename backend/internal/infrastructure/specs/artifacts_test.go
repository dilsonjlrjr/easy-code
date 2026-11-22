package specs_test

import (
	"testing"

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
