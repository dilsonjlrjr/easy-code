---
name: playwright-e2e
description: Como escrever, organizar e rodar testes e2e e de API com Playwright (TypeScript) contra a aplicação subida com Docker Compose - configuração, localizadores acessíveis, autenticação por papel com storageState, dados isolados, asserções com espera automática, fixtures e page objects leves, acessibilidade com axe, depuração com trace, flakiness e CI. Use ao automatizar CT de nível e2e (modo EXECUÇÃO) de qualquer aplicação web.
---

# Testes e2e com Playwright

Playwright (open source, Apache-2.0) controla navegadores reais e também faz chamadas HTTP.
Um teste e2e bom é **curto, independente, legível como o CT e rápido de diagnosticar**.
Exemplos usam reserva de salas só como ilustração; as regras valem para qualquer aplicação web.

## Princípios

1. **e2e é caro: poucos e bons.** Só jornadas do usuário que os níveis de baixo não provam
   (login -> fazer -> ver resultado; erro visível). Regra de negócio vai para integração/unitário
   (skill `cenarios-teste`, seção de nível).
2. **O teste espelha o CT.** Nome começa com o id (`CT-08 ...`); passos e resultado esperado
   iguais ao CT; um comportamento por teste.
3. **Localizar como o usuário (ou leitor de tela) vê:** papel, rótulo, texto. Isso também
   testa acessibilidade de graça.
4. **Esperar pelo estado, nunca pelo tempo.** Asserções web-first do Playwright repetem até
   passar ou estourar o timeout. Zero `waitForTimeout`.
5. **Independência total.** Cada teste cria o dado que usa (via API), roda em qualquer ordem
   e em paralelo.
6. **Autenticar uma vez por papel** (`storageState`), não em todo teste.
7. **Evidência automática:** trace, screenshot e vídeo em falha, anexados ao ACH.
8. **Interface para provar a jornada, API para preparar e verificar o estado.**
9. **Não corrija a aplicação.** Falha vira ACH com evidência.

## Regras de decisão

| Situação | Decisão | Referência |
|---|---|---|
| Qual localizador? | role > label > text > placeholder > test id; nunca CSS/XPath | seletores-localizadores |
| Preciso de dado | Criar via API no `beforeEach`/fixture; limpar ou usar ambiente efêmero | dados-isolamento |
| Preciso de usuário logado | `storageState` por papel gerado em setup | autenticacao-storagestate |
| Mesma sequência de passos em muitos testes | Fixture (primeiro) ou page object leve | padroes-fixtures-page-objects |
| Verificar regra de negócio com muitas variações | Não é e2e: teste de API/integração | cenarios-teste |
| Teste falha às vezes | Medir, achar a causa; retry não é correção | depuracao-flakiness |
| Verificar WCAG | axe por tela + roteiro de teclado; manual para o resto | acessibilidade-axe |
| Rodar em CI/Compose (CI/humano; no sandbox, serviços de apoio do perfil, sem Docker) | `docker compose up -d --wait`, depois a suíte | ci-docker-compose |
| Precisa de exemplo de CT-> teste | Ver os 5 exemplos prontos | exemplos-completos |

## Fluxo de trabalho

1. Ler o CT (nível e2e) e o README do app (como subir, portas, usuários de teste).
2. Criar/atualizar o projeto `app/e2e/` (estrutura-config) e subir o ambiente limpo.
3. Verificar manualmente o caminho feliz na interface para conhecer os papéis/rótulos reais.
4. Escrever o teste: setup via API -> ação pela interface -> asserção de interface **e** de
   estado final (API).
5. Rodar isolado (`-g "CT-08"`), depois `--repeat-each=10`, depois a suíte inteira com
   paralelismo.
6. Falhou: abrir o trace (`npx playwright show-trace`), classificar (bug, CA ambíguo, teste
   ruim, ambiente), abrir ACH se for da aplicação (ver `cenarios-teste/references/execucao-automacao-evidencia.md`).
7. Preencher o campo `execucao` do CT (`teste` como `arquivo::nome`, `resultado`, `evidencia`, `achado` se falhou; esquema `ct.schema.json`).

## Checklist de saída

- [ ] Projeto em `app/e2e/` com `package.json`, lockfile, `playwright.config.ts`, `tests/`.
- [ ] `baseURL` por variável de ambiente; nada de URL ou credencial fixa no teste.
- [ ] Um arquivo por história (`us-03-reservar-sala.spec.ts`); nome de teste inicia com `CT-NN`.
- [ ] Zero seletor CSS/XPath e zero `waitForTimeout`; `getByTestId` só com justificativa em comentário.
- [ ] Cada teste independente: roda sozinho e em paralelo; cria os próprios dados.
- [ ] Login por `storageState`; um arquivo de estado por papel; ignorado no git.
- [ ] Asserção de estado final (não só mensagem na tela) nos testes negativos.
- [ ] `trace: 'retain-on-failure'`, `screenshot: 'only-on-failure'`, reporters `list` + `html`.
- [ ] Suíte passa 10 vezes seguidas localmente (`--repeat-each=10`) antes de entregar.
- [ ] Comandos do REL-QA rodam do zero num ambiente limpo.
- [ ] Teste de acessibilidade (axe) nas telas em escopo do RNF, se há RNF de WCAG.

## Erros comuns

| Erro | Efeito | Correção |
|---|---|---|
| `page.waitForTimeout(2000)` | Lento e ainda instável | Asserção web-first no estado esperado |
| Seletor `.btn-primary > span` | Quebra com qualquer redesign | `getByRole('button', {name})` |
| Login pela tela em todo teste | Suíte lenta, frágil | `storageState` |
| Testes dependentes (2 usa o dado do 1) | Falham em paralelo/isolados | Dado por teste via API |
| `expect(await el.isVisible()).toBe(true)` | Sem espera automática | `await expect(el).toBeVisible()` |
| `retries: 3` para "estabilizar" | Esconde bug real | Retries 0 local; no CI só diagnóstico; intermitente = falhou |
| Asserir só o toast de sucesso | Estado errado passa | Conferir estado via API/lista |
| Um teste e2e para cada combinação | 200 testes lentos | Tabela no nível de integração |
| Teste lê e altera dados de produção | Risco real | Só o ambiente local do projeto |
| `test.only` esquecido | Suíte "verde" rodando 1 teste | `forbidOnly: true` no CI |

## Índice dos references

| Arquivo | Ler quando |
|---|---|
| `references/estrutura-config.md` | Criar o projeto, `playwright.config.ts`, scripts, projects, TypeScript |
| `references/seletores-localizadores.md` | Escolher/depurar localizadores, filtros, listas, iframes, componentes customizados |
| `references/autenticacao-storagestate.md` | Login por papel, setup project, múltiplos usuários, expiração |
| `references/dados-isolamento.md` | Seed via API, fixtures de dados, limpeza, paralelismo, ambiente efêmero |
| `references/assercoes-espera.md` | Asserções web-first, polling, espera por rede, o que nunca usar |
| `references/padroes-fixtures-page-objects.md` | Escolher entre fixture, page object leve e helper; exemplos |
| `references/acessibilidade-axe.md` | `@axe-core/playwright`, escopo, regras, teclado, o que o axe não cobre |
| `references/depuracao-flakiness.md` | Trace viewer, modo debug, causa de intermitência, evidência para ACH |
| `references/ci-docker-compose.md` | Rodar em CI e contra Docker Compose; relatório e artefatos |
| `references/exemplos-completos.md` | Modelos prontos: login, CRUD, conflito, permissão negada, API |

Skill complementar: `cenarios-teste` (derivação dos CT, execução, ACH).
