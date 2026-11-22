---
id: REL-QA-01
tipo: REL-QA
titulo: Relatório de QA 01 — Reserva de salas
autor: qa-agent
onda: QA-02
versao: 1
estado: rascunho
vinculos: [US-01, CT-01, CT-02]
historico:
  - {versao: 1, data: "2026-11-22", onda: QA-02, descricao: criação}
execucao: 1
resumo: {ct_executados: 2, passaram: 1, falharam: 1, bloqueados: 0, ca_must_cobertos: 1, ca_must_total: 2}
matriz:
  - {us: US-01, criterio: CA-1, ct: CT-01, nivel: unitario, resultado: passou}
  - {us: US-01, criterio: CA-2, ct: CT-02, nivel: integracao, resultado: falhou, achado: ACH-02}
sem_cobertura: []
comandos: ["cd app && go test -count=1 ./..."]
achados_abertos: [ACH-02]
veredito: reprovado
---
# REL-QA-01
