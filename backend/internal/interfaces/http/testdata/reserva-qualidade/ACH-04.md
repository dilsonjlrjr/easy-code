---
id: ACH-04
tipo: ACH
titulo: Dependência com vulnerabilidade conhecida sem correção publicada
autor: security-agent
onda: SEG-03
versao: 1
estado: rascunho
vinculos: [REL-SEG-01]
historico:
  - {versao: 1, data: "2026-11-22", onda: SEG-03, descricao: criação}
severidade: alto
categoria: vulnerabilidade
etapa_destino: implementacao
artefatos_afetados: [app/go.mod]
evidencia: "trivy fs app: CVE-2026-0001 (HIGH) em example.org/calendar v1.2.0, sem versão corrigida."
impacto: Negação de serviço no cálculo de horários.
correcao_sugerida: Trocar a biblioteca quando houver correção ou isolar o cálculo.
situacao: aberto
ciclos: []
---
# ACH-04 — CVE sem correção

## Evidência
Saída do trivy.
## Impacto
## Correção sugerida
## Justificativa do destino
Vulnerabilidade no código: destino padrão da categoria.
