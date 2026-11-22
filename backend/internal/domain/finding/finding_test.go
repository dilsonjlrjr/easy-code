package finding_test

import (
	"testing"
	"time"

	"easycode/internal/domain/finding"
)

// achado.md section 7: critical and high findings in an active state block; accepted, closed and discarded do not.
func TestBlockingFindings(t *testing.T) {
	for _, c := range []struct {
		severity, status string
		want             bool
	}{
		{"high", finding.StatusOpen, true}, {"critical", finding.StatusAwaitingScope, true}, {"high", finding.StatusAccepted, false},
		{"medium", finding.StatusOpen, false}, {"high", finding.StatusClosed, false}, {"critical", finding.StatusDiscarded, false},
	} {
		if finding.Blocking(c.severity, c.status) != c.want {
			t.Errorf("%s %s: want %v", c.severity, c.status, c.want)
		}
	}
}

// D-36: the header values map to the code names of the database and the API.
func TestHeaderValuesMapToCodeNames(t *testing.T) {
	if finding.Severity("alto") != "high" || finding.Category("desvio_arquitetura") != "architecture_deviation" ||
		finding.Stage("implementacao") != "implementation" || finding.Stage("qa-plano") != "qa-plan" || finding.Status("aguardando_escopo") != finding.StatusAwaitingScope ||
		finding.HeaderSeverity("critical") != "critico" || finding.Severity("high") != "high" || finding.Severity("x") != "" {
		t.Fatal("mapping")
	}
	if finding.ArtifactOf("US-03/CA-2") != "US-03" || finding.ArtifactOf("app/reserva.go:12") != "" || finding.ArtifactOf("ARQ") != "ARQ" {
		t.Fatal("ArtifactOf")
	}
}

// US-32 CA-3: a template placeholder or a text too short is no evidence.
func TestWithoutEvidence(t *testing.T) {
	for text, want := range map[string]bool{"<evidência>": true, "  ok ": true, "go test: --- FAIL: TestX": false} {
		if finding.WithoutEvidence(text) != want {
			t.Errorf("%q: want %v", text, want)
		}
	}
	if !finding.JustifiedDestination("## Justificativa do destino\nDefeito de código.\n") || finding.JustifiedDestination("## Justificativa do destino\n<por que>\n") {
		t.Fatal("JustifiedDestination")
	}
}

// US-33 RN-1, PL-68: an acceptance with validity in the past is expired; without validity it never expires.
func TestAcceptanceExpiry(t *testing.T) {
	yesterday := time.Now().AddDate(0, 0, -1)
	f := finding.Finding{Status: finding.StatusAccepted, ValidUntil: &yesterday}
	if !f.AcceptanceExpired(time.Now()) {
		t.Fatal("expired")
	}
	f.ValidUntil = nil
	if f.AcceptanceExpired(time.Now()) {
		t.Fatal("no validity never expires")
	}
	f.Escalations = []finding.Escalation{{Decision: finding.DecisionNewRework, AuthorizedBy: "dev"}, {Decision: ""}}
	if f.ExtraRounds() != 1 {
		t.Fatal("extra rounds")
	}
}
