# Checklist do gate de Arquitetura

Use antes de entregar a onda e como roteiro de revisão do gate. Cada item é verificável;
marque só o que foi conferido. Item que não se aplica: registre "N/A" com motivo.

## 1. Rastreabilidade

- [ ] Toda US **Must** tem ao menos uma T (matriz US x T completa).
- [ ] Cada T cita US/CA cobertos (`cobre`), ADR seguidas (`segue`) e CT que deve fazer passar (`cts`).
- [ ] Todo RNF aplicável aparece em decisão (ADR), contrato ou critério de pronto.
- [ ] Nenhum componente (serviço, camada, biblioteca relevante) sem US/RNF que o justifique.
- [ ] Nenhuma decisão reservada ao aprovador humano tomada sem aprovação.

## 2. ADR (`escrever-adr.md`)

- [ ] ADR mínimas: framework do backend, autenticação, padrão de acesso a dados e migração,
      tratamento de regra de integridade crítica, padrão de erros, organização do código.
- [ ] Cada ADR com 2 ou mais opções reais, a mais simples incluída.
- [ ] Consequências positivas e negativas em cada uma.
- [ ] Cabeçalho YAML válido no esquema `adr.schema.json` (`motivada_por` com US/RNF existentes); `onda: ARQ-<nn>`; `estado` coerente.
- [ ] Desvio de skill de linguagem explicado.
- [ ] ADR substituída marcada `estado: substituido` com `substituido_por`; a nova com `substitui`.

## 3. Diagramas (`c4-mermaid.md`)

- [ ] Contexto e contêineres presentes no corpo do `ARQ.md` (C4); Mermaid válido.
- [ ] Toda seta com verbo e protocolo.
- [ ] Diagrama consistente com `docker-compose.yml` planejado e com OpenAPI.

## 4. Acesso a dados (o modelo é do DBA; skill `dados/modelagem-postgresql`)

- [ ] ADR define o padrão de acesso a dados (ORM, query builder ou SQL).
- [ ] Ferramenta de migração versionada decidida; sem alteração destrutiva sem plano.
- [ ] Violação de constraint mapeada a status/erro da API.
- [ ] Nenhuma tabela, coluna ou DDL definidos na Arquitetura: o modelo (`modelo-dados.dbml`,
      `esquema.sql`, dicionário, persistência) fica em `docs/dados/`, a cargo do DBA.

## 5. Contrato da API (`api-rest-openapi.md`)

- [ ] `openapi.yaml` válido (ferramenta de validação passou).
- [ ] Recursos no plural, sem verbo; status coerentes.
- [ ] Todo endpoint: `operationId`, tags, sucesso e 4xx relevantes, exemplos.
- [ ] Erros em Problem Details (RFC 9457) com `type` estável listado.
- [ ] Paginação, ordenação, filtros padronizados e com limite máximo.
- [ ] Datas ISO 8601 com fuso; dinheiro sem `float`.
- [ ] Segurança declarada por operação.
- [ ] Todo CA de US com rastro no contrato (campo, status ou erro).

## 6. Autenticação e autorização (`autenticacao-autorizacao.md`)

- [ ] Mecanismo e hash de senha decididos em ADR.
- [ ] Papéis definidos; matriz endpoint x papel x regra por recurso entregue.
- [ ] Regra única de 404/403 para recurso alheio.
- [ ] Identidade do usuário derivada da sessão, não do corpo.
- [ ] Cookie/CORS descritos quando aplicável.
- [ ] Ameaças altas do `AMEACAS` com mitigação (conferido depois da onda de Segurança).

## 7. Organização do código (skill `padroes-arquitetura-codigo`)

- [ ] Padrão escolhido e proporcional ao tamanho (para app pequeno, o padrão simples).
- [ ] `estrutura.md` com árvore de pastas, convenções, comandos de build, teste e lint.
- [ ] Desvio do padrão simples justificado por critério objetivo.

## 8. Ambiente (skill `ambiente-docker`)

- [ ] Compose planejado com healthchecks, volumes, rede e `.env.example`.
- [ ] Um comando para subir e um para testar.
- [ ] Seed com usuários de teste por papel.

## 9. Plano de tarefas (`plano-de-tarefas.md`)

- [ ] T-01 é o esqueleto, com critério de pronto completo.
- [ ] Todas as T com campos obrigatórios; tamanho P/M; G justificado.
- [ ] Critério de pronto verificável em toda T.
- [ ] Sem ciclos; escopos disjuntos nas T paralelizáveis; regra anti-colisão de migrações.
- [ ] Ordem coerente: esqueleto, autenticação, base, regra crítica, listagens, telas, acabamento.

## 10. Documento de arquitetura (artefato ARQ)

- [ ] `ARQ.md` presente, com cabeçalho válido no esquema `arq.schema.json`: `contexto`,
      `componentes` (cada um com `atende` US/RNF), `contratos`, `decisoes` (ids de ADR existentes),
      `diretrizes` e `riscos` (pode ser `[]`).
- [ ] Consistente com C4, ADR e `openapi.yaml`.

## 11. Modo VALIDAÇÃO (validação técnica conjunta; artefato VALIDACAO)

- [ ] `docs/validacao/VALIDACAO-NN.md` com cabeçalho YAML válido no esquema `validacao.schema.json` e `onda: VAL-<nn>`.
- [ ] Conferidos: arquitetura x modelo de dados; contrato da API x modelo e x telas;
      ameaças x mitigações; US Must x T x CT.
- [ ] `pareceres` do arquiteto, do dba e do designer registrados; `veredito: liberado` só sem conferência `divergente` e sem DBA reprovado.
- [ ] Cada conferência `divergente` tem `inconsistencia` e `achado` (ACH para a etapa dona do artefato).
- [ ] Reprovação do DBA em persistência tratada como bloqueio da liberação.

Exemplo mínimo (esquema `validacao.schema.json`; os 5 pares e os 3 pareceres são obrigatórios):

```yaml
---
id: VALIDACAO-01
tipo: VALIDACAO
titulo: Validação técnica 01 — Reserva de salas
autor: architect-agent
onda: VAL-01
versao: 1
estado: rascunho
vinculos: []
historico:
  - {versao: 1, data: "2026-11-19", onda: VAL-01, descricao: criação}
conferencias:
  - {par: arquitetura_dados, resultado: ok}
  - {par: api_dados, resultado: ok}
  - {par: api_telas, resultado: divergente, inconsistencia: Tela TL-2 usa o campo "recurso" que o contrato não expõe, achado: ACH-04}
  - {par: ameacas_arquitetura, resultado: ok}
  - {par: cobertura_us_t_ct, resultado: ok}
pareceres:
  - {participante: arquiteto, parecer: aprovado, ressalvas: []}
  - {participante: dba, parecer: aprovado, ressalvas: []}
  - {participante: designer, parecer: reprovado, ressalvas: [Contrato sem o campo recurso da TL-2]}
veredito: devolvido
---
```

## 12. Formato e qualidade geral

- [ ] Documentos em pt-BR; termos do glossário (Etapa, Agente, Skill, Artefato, Onda, Achado, Gate).
- [ ] Sem preço de token, sem versão de biblioteca fixada de memória.
- [ ] Exemplos de código curtos, só nas ADR.
- [ ] Riscos técnicos listados na resposta final do agente.

## Veredito

| Resultado | Condição |
|---|---|
| Aprovar | Seções 1 a 9 sem item aberto |
| Aprovar com ressalva | Itens abertos só de baixo risco, registrados como suposição |
| Devolver | Qualquer item de rastreabilidade, contrato, modelo ou T-01 aberto |

## Erros comuns na revisão

| Erro | Correção |
|---|---|
| Aprovar porque "parece completo" | Executar a validação (OpenAPI, DDL) de fato |
| Não checar a matriz US x T | Conferir linha a linha das Must |
| Ignorar componente sem justificativa | Perguntar a US/RNF; remover se não houver |
| Aceitar T "G" multi-camada | Exigir quebra |
| Conferir contrato só pelo YAML, sem exemplos | Validar exemplos contra os esquemas |
