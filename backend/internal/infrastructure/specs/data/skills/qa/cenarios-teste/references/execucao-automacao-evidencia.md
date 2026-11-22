# Modo EXECUÇÃO: automação, ambiente, evidência, intermitência e ACH

> **Onde isto roda.** `docker-compose.yml` e Dockerfiles do projeto gerado continuam sendo artefatos entregues, para CI e para humanos. Os comandos `docker compose ...` e `docker run ...` deste arquivo são **para CI/humano**. No sandbox da plataforma o agente não tem Docker nem rede: testes de integração/E2E rodam contra os **serviços de apoio declarados no perfil** (ex.: PostgreSQL efêmero) que o runner sobe, e só com os comandos da allowlist da sua categoria (`testes`, `migracoes`, `banco_descartavel`).

No modo EXECUÇÃO o código existe. O QA automatiza o que o Dev não cobriu, roda do zero,
registra evidência e transforma falha em achado. Não corrige a aplicação.

## 1. Primeira passada: o que já existe

1. Ler o README do app e subir o ambiente exatamente como ele diz. Se não sobe do zero:
   ACH com `etapa_destino: implementacao`: Crítico se o sistema não sobe de jeito nenhum; Médio se a instrução está incompleta mas há contorno.
2. Inventariar os testes do Dev (unitários, integração). Mapear cada CT: **já coberto**
   (apontar `arquivo::teste` no CT), **a automatizar** ou **manual** (justificar).
3. Um CT coberto por teste do Dev só conta se o teste realmente verifica o **resultado
   esperado do CT** (leia a asserção). Teste do Dev que sobe contexto e não assere nada útil
   não cobre.

## 2. Automação por nível

| Nível do CT | Ferramenta | Onde fica | Observação |
|---|---|---|---|
| Unitário | framework do backend/frontend | junto do código | normalmente do Dev; QA só complementa limites |
| Integração (API + banco) | cliente HTTP na linguagem do backend (ex.: framework de teste da stack) ou suíte de API do Playwright (`request`) | `app/` seguindo a estrutura do projeto | banco real (contêiner), não mock, para provar unicidade e transação |
| e2e (interface) | Playwright | `app/e2e/` | skill `playwright-e2e`; poucos, só jornadas principais |
| Não funcional | k6/Locust, axe, scripts | `app/perf/`, `app/e2e/` | ver `nao-funcionais.md` |

**Regra de escolha:** use o nível **declarado no CT**. Se o CT diz integração e você só
consegue por e2e (ou o inverso), corrija o CT (suba `versao`, registre em `historico`) em vez de
testar em nível diferente sem avisar.

**Nome do teste = id do CT** (rastreável): `CT-08 reserva sobreposta é recusada`, ou nome da
função com o id; o campo `execucao.teste` do CT recebe `arquivo::nome`.

## 3. Ambiente limpo e reprodutível

Qualquer pessoa (ou agente) deve conseguir rodar os comandos do REL-QA do zero. Por isso:

```bash
# Padrão (comandos Docker Compose estáveis; confirmar nomes de serviço em `agentes/ambiente.md`)
docker compose down -v            # remove contêineres e volumes: estado limpo
docker compose up -d --build      # sobe app e banco
docker compose ps                 # confere serviços saudáveis
# aguardar saúde (healthcheck/URL) — nunca "sleep 30" cego
# seed: comando do README do app (ex.: script de seed)
# rodar testes: comando da suíte (ex.: cd app/e2e && npx playwright test)
```

Regras:
- Estado limpo **antes** de cada execução completa do relatório; entre testes, isolamento por
  dados únicos (ver `playwright-e2e/references/dados-isolamento.md`).
- Esperar pelo estado, não pelo tempo: `docker compose up -d --wait` aguarda healthchecks
  (flag existente no Compose v2 recente; conferir versão em `agentes/ambiente.md`) ou um loop que consulta a URL de saúde.
- Variáveis e credenciais de teste em `.env.test` documentado (nunca segredo real).
- Fixe a versão das dependências do projeto de teste (lockfile commitado).
- Registre no REL-QA: comando completo, versão das ferramentas, data, e que o ambiente é local.

## 4. Preencher o campo `execucao` do CT

`execucao` é campo de acompanhamento do CT (esquema `ct.schema.json`): o QA o atualiza sem nova
`versao` e sem reabrir etapa.

```yaml
execucao:
  teste: app/e2e/tests/us-03-reservar-sala.spec.ts::CT-08 reserva sobreposta é recusada
  resultado: falhou
  evidencia: "npx playwright test -g \"CT-08\" -> esperado 409, recebido 201; trace em app/e2e/test-results/us-03-.../trace.zip; reservas na sala: 2 (deveria ser 1)"
  achado: ACH-02      # obrigatório se falhou
  onda: QA-02
```

Resultado em quatro valores: `passou`, `falhou`, `bloqueado` (o ambiente ou dependência impede;
dizer qual) e `nao_executado`. "Parcialmente" não existe: divida o CT.

## 5. Evidência que serve

| Tipo de falha | Evidência mínima |
|---|---|
| API | requisição (método, URL, corpo, papel/usuário) + resposta (status, corpo) + estado final consultado; `curl` reproduzível |
| e2e | nome do teste, mensagem da asserção, screenshot, trace (`trace.zip`), passos |
| Dado/estado | consulta ao banco ou à API depois da ação |
| Não funcional | número medido x limiar, condição, configuração da ferramenta, saída resumida |
| Visual/acessibilidade | print + regra violada (id da regra do axe) + elemento/seletor |

Regras de evidência:
- **Curta e reproduzível** vale mais que longa. O Dev precisa reproduzir em 2 minutos.
- Sem segredos: mascarar tokens e senhas nos logs.
- Evidência vem de **execução real**, nunca de dedução do código ("o código parece errado" não é achado de QA).
- Guarde os artefatos em pasta referenciada no ACH (caminho relativo em `docs/qa/evidencias/`); o texto do ACH repete a essência (a pasta pode sumir).

## 6. Falha: bug, CA ambíguo, teste ruim ou ambiente?

Antes de abrir ACH, **reproduza duas vezes** em ambiente limpo e classifique:

| Diagnóstico | Sinal | Ação |
|---|---|---|
| Bug da aplicação | falha estável; resultado diverge do CA claro | ACH `categoria: defeito`, `etapa_destino: implementacao` |
| CA ambíguo/contraditório | duas leituras razoáveis; Dev implementou uma, CT outra | ACH `categoria: requisito_ambiguo`, `etapa_destino: requisitos` |
| Teste ruim | seletor frágil, dado compartilhado, relógio | corrigir o teste; sem ACH |
| Ambiente | serviço fora, porta ocupada, seed incompleto | corrigir ambiente; se for falta no README do app, ACH Médio |
| Intermitente | passa e falha sem mudança | ver seção 8 |

Não "ajuste" o resultado esperado do CT para fazer passar: se o CA mudou, a mudança vem de
Requisitos (retrabalho) e o CT sobe de versão.

## 7. Abrir ACH a partir de falha (campo a campo)

O ACH é o artefato `docs/achados/ACH-NN.md`: cabeçalho YAML validado por
`specs/dominio/artefatos/ach.schema.json` (molde em `agentes/templates/ACH.md`; regras de negócio em
`specs/dominio/achado.md`). **Vale o esquema.** Cabeçalho comum com `autor: qa-agent`, `onda: QA-<nn>`.
O autor do ACH é quem revalida.

| Campo | Como preencher |
|---|---|
| `titulo` | problema em uma frase, sintoma observável, sem culpa: "Reserva sobreposta é aceita (201) quando deveria ser recusada" |
| `severidade` | `critico`, `alto`, `medio` ou `baixo` (tabela abaixo) |
| `categoria` | `defeito` ou `requisito_ambiguo` (QA raramente usa as outras; ver `achado.md`, seção 2) |
| `etapa_destino` | `implementacao` (defeito) ou `requisitos` (ambíguo); outra etapa só com justificativa no corpo |
| `artefatos_afetados` | ao menos 1: `US-03/CA-2`, os `CT-NN` e, se identificável pela execução, `caminho:linha` (com o número da linha) |
| `evidencia` | obrigatória: passos mínimos + esperado x obtido + comando/saída (de execução real) |
| `impacto` | o que acontece para o usuário/negócio |
| `correcao_sugerida` | direção sem impor implementação |
| `situacao` e `ciclos` | `aberto` e `[]` na abertura; o resto é do orquestrador e do ciclo de retrabalho |

**Severidade (tabela do ACH, aplicada ao QA)**

| Severidade | Quando, no QA |
|---|---|
| Crítico | Perda/corrupção de dados, acesso indevido a dado de outro, sistema não sobe |
| Alto | CA de US **Must** não atendido; falha de autorização explorável |
| Médio | CA de US **Should** não atendido; comportamento errado com contorno; instrução de execução faltando |
| Baixo | Cosmético, texto, melhoria, dívida sem efeito visível |

**Um achado, uma causa.** CT diferentes que falham pela mesma causa viram **um** ACH listando
todos os CT em `artefatos_afetados`. CT que falham por causas diferentes = ACH separados.

**Exemplo completo (cabeçalho YAML válido no esquema)**

```yaml
---
id: ACH-02
tipo: ACH
titulo: Reserva sobreposta é aceita quando deveria retornar conflito
autor: qa-agent
onda: QA-02
versao: 1
estado: rascunho
vinculos: []
historico:
  - {versao: 1, data: "2026-11-19", onda: QA-02, descricao: criação}
severidade: alto
categoria: defeito
etapa_destino: implementacao
artefatos_afetados:
  - US-03/CA-2
  - CT-08
  - CT-10
  - app/e2e/tests/us-03-reservar-sala.spec.ts:42
evidencia: |
  Estado: sala "Azul" com reserva 10:00–11:00 de 2026-12-01 (-03:00).
  POST /reservas {"salaId":1,"inicio":"2026-12-01T10:59:00-03:00","fim":"2026-12-01T12:00:00-03:00"}
  -> 201 (esperado 409). GET /salas/1/reservas lista 2 reservas sobrepostas.
  CT-08 (10:30–11:30) passa; falha só no limite de 1 minuto (CT-10): comparação parece usar <=/> trocados.
  Reproduzir: cd app/e2e && npx playwright test -g "CT-10"; trace em docs/qa/evidencias/ACH-02/trace.zip.
impacto: Duas pessoas ocupam a mesma sala no mesmo horário.
correcao_sugerida: "Tratar intervalos como [início, fim): conflito se iniA < fimB e iniB < fimA."
situacao: aberto
ciclos: []
---
```

Corpo Markdown: detalhe e passos de reprodução; o YAML guarda o resumo.

## 8. Teste intermitente (flaky)

Regra do projeto: **intermitente conta como falhou** até estabilizar. Não use retry para
esconder.

Procedimento:
1. **Medir:** rodar o teste isolado 20 vezes (`--repeat-each=20` no Playwright) e em
   conjunto com a suíte; anotar taxa de falha.
2. **Classificar a causa:**

| Causa | Sinal | Correção |
|---|---|---|
| Espera mal feita | falha ao ser mais rápido/lento | asserção com auto-espera; esperar o estado, não o tempo |
| Dado compartilhado/ordem | passa isolado, falha na suíte | dados próprios por teste |
| Relógio/data | falha em certos horários | relógio controlado, datas fixadas |
| Concorrência real da aplicação | falha em paralelo/rajada | **é bug**: ACH |
| Ambiente fraco | falha sob carga da máquina | reduzir workers; registrar |
| Animação/transição | clique em elemento em movimento | esperar estado final; desabilitar animações no ambiente de teste (config) |

3. **Estabilizou?** 20 execuções seguidas (ou 50 para teste crítico) sem falha, após a
   correção. Registrar no REL-QA, seção Observações.
4. **Não deu para estabilizar:** CT fica `falhou`/`bloqueado` com nota; se a causa aponta para
   a aplicação (corrida), ACH; se indeterminado, listar em "Critérios sem cobertura".
5. Retry de CI (`retries`) só como diagnóstico: o relatório marca passou-com-retry como
   intermitente.

Mais detalhe de causa e ferramenta em `playwright-e2e/references/depuracao-flakiness.md`.

## 9. Revalidação

Onda de revalidação: repetir **o CT que originou o ACH** e os vizinhos da mesma regra.
- Passou: o QA registra o ciclo (`resultado: corrigido`, `onda_revalidacao`) e o ACH vai a `fechado`
  com `resolucao: corrigido` (máquina do achado, `specs/dominio/estados/achado.md`).
- Falhou: `resultado: nao_corrigido` e novo ciclo. Limite de 3 ciclos; estourou -> `escalado` (o
  aprovador humano decide).
- Correção que muda algo aprovado (CA, ADR) reabre o gate daquela etapa (regra do ACH, `achado.md` seção 7).

## 10. REL-QA: fechamento

O REL-QA é o artefato `docs/qa/REL-QA-NN.md`: cabeçalho YAML validado por
`specs/dominio/artefatos/rel-qa.schema.json` (molde em `agentes/templates/REL-QA.md`; vale o esquema),
etapa dona `qa` (gate.qa). Exemplo mínimo válido:

```yaml
---
id: REL-QA-01
tipo: REL-QA
titulo: Relatório de QA — execução 01
autor: qa-agent
onda: QA-02
versao: 1
estado: rascunho
vinculos: []
historico:
  - {versao: 1, data: "2026-11-19", onda: QA-02, descricao: criação}
execucao: 1
resumo:
  ct_executados: 14
  passaram: 13
  falharam: 1
  bloqueados: 0
  ca_must_cobertos: 5
  ca_must_total: 6
matriz:
  - {us: US-03, criterio: CA-1, ct: CT-07, nivel: integracao, resultado: passou}
  - {us: US-03, criterio: CA-2, ct: CT-08, nivel: e2e, resultado: falhou, achado: ACH-02}
sem_cobertura: []
comandos:
  - docker compose -f app/docker-compose.yml run --rm e2e npx playwright test
achados_abertos: [ACH-02]
veredito: reprovado
---
```


1. Matriz completa (ver `cobertura-rastreabilidade.md`), resumo com contagem correta.
2. "Como rodar": comandos do zero (seção 3) testados por você num ambiente limpo.
3. Observações: intermitências, limitações do ambiente (máquina, relógio, sem WebKit etc.).
4. `veredito` segundo a regra do template REL-QA (`aprovado` só com todo CA de US Must coberto e passando e zero ACH Crítico/Alto aberto); revisão final com `revisao-gate-qa.md`.
