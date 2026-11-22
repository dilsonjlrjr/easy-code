---
id: REL-SEG-01
tipo: REL-SEG
titulo: Relatório de segurança 01 — Reserva de salas
autor: security-agent
onda: SEG-03
versao: 1
estado: rascunho
vinculos: [AMEACAS, REL-QA-02]
historico:
  - {versao: 1, data: "2026-11-22", onda: SEG-03, descricao: criação}
execucao: 1
resumo:
  critico: {abertos: 0, fechados: 0, aceitos: 0}
  alto: {abertos: 1, fechados: 1, aceitos: 0}
  medio: {abertos: 0, fechados: 0, aceitos: 0}
  baixo: {abertos: 0, fechados: 0, aceitos: 0}
varreduras:
  - {ferramenta: opengrep, versao: "1.2.0", comando: "opengrep scan --config auto app", achados: [], falsos_positivos: 0}
  - {ferramenta: trivy, versao: "0.60.0", comando: "trivy fs --offline-scan --skip-db-update app", achados: [ACH-04], falsos_positivos: 0}
  - {ferramenta: gitleaks, versao: "8.24.0", comando: "gitleaks dir app", achados: [], falsos_positivos: 0}
owasp:
  - {categoria: A01, verificado: true, resultado: sem problema, achados: []}
  - {categoria: A02, verificado: true, resultado: sem problema, achados: []}
  - {categoria: A03, verificado: true, resultado: dependência vulnerável, achados: [ACH-04]}
  - {categoria: A04, verificado: true, resultado: sem problema, achados: []}
  - {categoria: A05, verificado: true, resultado: sem problema, achados: []}
  - {categoria: A06, verificado: true, resultado: sem problema, achados: []}
  - {categoria: A07, verificado: true, resultado: sem problema, achados: []}
  - {categoria: A08, verificado: true, resultado: sem problema, achados: []}
  - {categoria: A09, verificado: true, resultado: sem problema, achados: []}
  - {categoria: A10, verificado: true, resultado: sem problema, achados: []}
ameacas_conferidas: []
falsos_positivos: []
comandos: ["opengrep scan --config auto app", "trivy fs --offline-scan --skip-db-update app", "gitleaks dir app"]
veredito: reprovado
---
# REL-SEG-01
