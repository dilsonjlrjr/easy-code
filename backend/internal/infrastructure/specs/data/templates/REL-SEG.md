# Template — Relatório de Segurança (REL-SEG) — v1

- **Esquema:** specs/dominio/artefatos/rel-seg.schema.json
- **Quem preenche:** agente de Segurança, na auditoria final.
- **Quem lê:** aprovador humano (gate de Segurança), Release.
- **Gate confere:** zero achados Críticos e Altos abertos (ou aceitos explicitamente pelo aprovador humano).
- **Arquivo:** `docs/seguranca/REL-SEG-NN.md` (um por execução).

## Molde

````markdown
---
id: REL-SEG-NN
tipo: REL-SEG
titulo: Relatório de Segurança — execução NN
autor: security-agent
onda: SEG-<nn>
versao: 1
estado: rascunho              # rascunho | em_revisao | aprovado | substituido (ciclo do artefato)
vinculos: []
historico:
  - {versao: 1, data: "<AAAA-MM-DD>", onda: SEG-<nn>, descricao: criação}
execucao: 1
resumo:                       # Aceitos = risco assumido por humano (ACH aceito)
  critico: {abertos: 0, fechados: 0, aceitos: 0}
  alto: {abertos: 0, fechados: 0, aceitos: 0}
  medio: {abertos: 0, fechados: 0, aceitos: 0}
  baixo: {abertos: 0, fechados: 0, aceitos: 0}
varreduras:                   # ao menos 3
  - {ferramenta: opengrep, versao: <v>, comando: <comando>, achados: [], falsos_positivos: 0}   # SAST
  - {ferramenta: trivy, versao: <v>, comando: <comando>, achados: [], falsos_positivos: 0}      # dependências/contêiner
  - {ferramenta: gitleaks, versao: <v>, comando: <comando>, achados: [], falsos_positivos: 0}   # segredos
owasp:                        # OWASP Top 10:2025, exatamente 10 (conferir nomes em owasp.org/Top10/2025)
  - {categoria: A01, verificado: true, resultado: <verificado, sem achado | ...>, achados: []}  # Controle de acesso quebrado (inclui SSRF)
  - {categoria: A02, verificado: true, resultado: <...>, achados: []}   # Configuração insegura
  - {categoria: A03, verificado: true, resultado: <...>, achados: []}   # Falhas na cadeia de suprimentos de software
  - {categoria: A04, verificado: true, resultado: <...>, achados: []}   # Falhas criptográficas
  - {categoria: A05, verificado: true, resultado: <...>, achados: []}   # Injeção
  - {categoria: A06, verificado: true, resultado: <...>, achados: []}   # Design inseguro
  - {categoria: A07, verificado: true, resultado: <...>, achados: []}   # Falhas de autenticação
  - {categoria: A08, verificado: true, resultado: <...>, achados: []}   # Falhas de integridade de software e dados
  - {categoria: A09, verificado: true, resultado: <...>, achados: []}   # Falhas de log e alerta
  - {categoria: A10, verificado: true, resultado: <...>, achados: []}   # Tratamento incorreto de condições excepcionais
ameacas_conferidas: []        # ids AM do AMEACAS conferidos na auditoria
falsos_positivos: []          # {ferramenta, regra, local, justificativa}
comandos:                     # ao menos 1; reproduzem as varreduras
  - <comando exato>
veredito: reprovado           # aprovado | reprovado (aprovado exige critico e alto com abertos: 0)
---

# Relatório de Segurança — execução NN

## Resumo
## Varreduras automáticas
## Revisão manual — OWASP Top 10:2025
## Modelo de ameaças
- Ameaças do modelo conferidas: <lista com status>
- Ameaças novas encontradas na auditoria: <lista; viram ACH e entram em AMEACAS>

## Falsos positivos descartados
## Como reproduzir as varreduras
````

## Regras

- Todo achado vira um ACH separado; o relatório só referencia.
- Falso positivo só é descartado com justificativa escrita (`falsos_positivos`).
- Verificar a categoria OWASP mesmo quando não há achado: "verificado, sem achado" é resultado.
- O esquema trava `veredito: aprovado` com Crítico ou Alto abertos; a conferência dos contadores
  contra os ACH reais é checagem de governança.
- Usar só ferramentas open source (agentes/README.md).
