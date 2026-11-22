# Depuração, trace, evidência e flakiness

## 1. Ferramentas de diagnóstico do Playwright

| Ferramenta | Uso | Comando |
|---|---|---|
| **Trace viewer** | Linha do tempo: cada ação, snapshot do DOM antes/depois, rede, console, localizador resolvido | `npx playwright show-trace caminho/trace.zip` |
| **Relatório HTML** | Resultado da suíte, anexos (trace, screenshot, vídeo) | `npx playwright show-report` |
| **UI mode** | Rodar/observar testes interativamente | `npx playwright test --ui` |
| **Debug / Inspector** | Passo a passo com pausa | `npx playwright test --debug` (ou `PWDEBUG=1`) |
| **Headed** | Ver o navegador | `npx playwright test --headed` |
| **Codegen** | Gerar localizadores clicando | `npx playwright codegen <url>` |
| **Repetição** | Medir intermitência | `npx playwright test -g "CT-08" --repeat-each=20` |
| **Isolar** | Um teste/projeto | `-g "texto"`, `--project=chromium`, `--workers=1` |

(Comandos estáveis do CLI do Playwright; flags novas: conferir `npx playwright test --help` na versão instalada.)

## 2. Evidência automática (configuração)

```ts
use: {
  trace: 'retain-on-failure',      // trace.zip só quando falha
  screenshot: 'only-on-failure',
  video: 'retain-on-failure',
}
```

Onde fica: `test-results/<teste>/trace.zip`, `test-failed-1.png`, `video.webm`. O relatório HTML
referencia esses arquivos.

**Para o ACH:** copie `trace.zip` e a screenshot para
`docs/qa/evidencias/ACH-NN/` e cite o caminho; **repita no texto do ACH** o
essencial (mensagem da asserção, esperado x obtido, passos), porque anexos podem se perder.

## 3. Lendo uma falha (roteiro de 5 minutos)

1. **Mensagem da asserção:** esperado x recebido; qual linha do teste.
2. **Trace -> passo que falhou:** o snapshot mostra o que o usuário via? A página estava no
   estado esperado?
3. **Aba Network:** a requisição relevante, status e corpo. A UI mostra erro porque a API
   falhou? (Bug de API) ou a API acertou e a UI errou? (Bug de front)
4. **Console:** erros de JavaScript.
5. **Localizador:** resolveu o elemento certo? Mais de um? (erro de modo estrito).
6. **Reproduza fora do teste** com `curl`/manual para formar a evidência do ACH (a menor
   sequência que reproduz).

Classificação final (bug, CA ambíguo, teste ruim, ambiente): ver
`cenarios-teste/references/execucao-automacao-evidencia.md`, seção 6.

## 4. Flakiness: o que é e como tratar

**Flaky** = o mesmo teste, sem alteração, passa e falha. Política do projeto: **conta como
falhou**; `retries` não é correção.

### 4.1 Medir

```bash
npx playwright test tests/us-03-reservar-sala.spec.ts -g "CT-08" --repeat-each=30 --workers=1
npx playwright test tests/us-03-reservar-sala.spec.ts -g "CT-08" --repeat-each=30 --workers=4
```
- Isolado passa e em paralelo falha -> dado compartilhado, ordem, ou concorrência da aplicação.
- Falha só com muitos workers -> máquina saturada ou recurso compartilhado.
- Falha sempre no mesmo passo -> localizador/espera.

### 4.2 Causas e correções

| Causa | Sinal no trace | Correção |
|---|---|---|
| **Espera por tempo** | `waitForTimeout`; falha quando a máquina é lenta | Asserção sobre estado |
| **Asserção sem espera** | `expect(await x.isVisible())` | `expect(x).toBeVisible()` |
| **Clique antes de pronto** (handler ainda não ligado) | clique "funciona" mas nada acontece | Esperar o sinal de hidratação/pronto da tela (elemento habilitado, texto final) antes de agir |
| **Elemento animado/cobrir por overlay** | erro "intercepts pointer events" | Esperar o overlay sumir; desativar animação no ambiente de teste |
| **Localizador ambíguo** | resolve elemento diferente às vezes | Escopo por linha/diálogo |
| **Dado compartilhado** | falha só em paralelo | Dados únicos por teste |
| **Dependência de ordem** | falha com `-g` isolado | Setup próprio |
| **Relógio/data** | falha em certos horários | Âncora de data, `page.clock` |
| **Corrida real da aplicação** | falha em rajada; resultado varia no estado final | **É bug**: ACH com contagem de execuções |
| **Rede/serviço externo** | timeout em chamada externa | Stub (`page.route`) se fora do escopo do CT; senão registrar |
| **Sessão expirada** | 401 no meio | Regenerar `storageState` |
| **Hardware/CI lento** | falha só no CI | Reduzir workers; revisar timeouts com critério |
| **Dependência de recurso do navegador** (permissão, clipboard) | só em um projeto | Conceder em `context.grantPermissions` |

### 4.3 Estabilizado quando…

N execuções seguidas sem falha **com paralelismo** (N >= 20; 50 para teste de fluxo crítico),
e em ambiente recém-criado. Registre o número no REL-QA ("estabilizado: 30/30"). Antes disso,
o CT permanece `falhou` e conta como não coberto.

### 4.4 Quarentena

Teste que não dá para estabilizar no prazo: **não** marcar `test.skip` silenciosamente.
Mantenha-o falhando/`bloqueado` no relatório, com a causa suspeita e ACH aberto. `test.fixme('CT-xx ... ', ...)`
com comentário apontando o ACH é aceitável **desde que** o REL-QA liste o CT como sem cobertura.

### 4.5 `retries`

- Local: `0`.
- CI: pode ser `1` **apenas para diagnóstico**; o relatório HTML marca como "flaky" o que
  passou na 2ª tentativa; a regra do projeto continua: flaky = falhou.
- Jamais `retries` alto para "esverdear".

## 5. Dicas de depuração específicas

- **Esperar algo que nunca acontece:** o timeout dá a mensagem do localizador esperado; confira
  no snapshot do trace o que existia (talvez o texto muda por idioma — fixe `locale`).
- **Elemento fora da viewport:** o Playwright rola sozinho; se falha, é overlay/sticky header.
- **Teste passa local e falha em CI:** compare viewport, `timezoneId`, `locale`, versão do
  navegador (`npx playwright install` com a mesma versão do lockfile), recursos da máquina, fonte
  instalada (screenshot), variáveis de ambiente.
- **Logs da aplicação:** `docker compose logs --since=2m api` junto da falha (CI/humano; no sandbox, o log
  vem da saída do comando da suíte) — erro 500 no servidor é evidência melhor que o print.
- **Capturar console e erros de página** como fixture automática (ver
  `padroes-fixtures-page-objects.md`), útil para achar exceções silenciosas.
- **Rede:** `page.on('requestfailed', ...)`, `page.on('response', ...)` para depurar chamadas.
- **`await page.pause()`** só durante depuração; nunca commitar. Lint do repositório deve falhar com `page.pause` e `test.only`.

## 6. Evidência mínima convincente para ACH vindo de e2e

```markdown
## Evidência
Teste: app/e2e/tests/us-05-cancelar-reserva.spec.ts::CT-24 funcionário não cancela reserva de outro
Comando: cd app/e2e && npx playwright test -g "CT-24" --project=chromium
Esperado: 403 e reserva R1 ativa. Obtido: 204; GET /reservas/R1 -> 404.
Passos manuais equivalentes: (1) A cria R1; (2) com token de B, DELETE /reservas/R1; (3) GET /reservas/R1.
Anexos: evidencias/ACH-05/trace.zip, evidencias/ACH-05/falha.png
Execuções: falhou 3 de 3 (estável).
```

Checklist da evidência: comando reproduzível, esperado x obtido, estado final, quantas
execuções falharam, anexos com caminho, sem segredos.

## 7. Anti-padrões

- "Passou na segunda tentativa, logo está ok."
- Aumentar o timeout global até parar de falhar.
- Fechar ACH de flakiness sem número de execuções.
- Anexar vídeo de 2 minutos sem dizer o passo que falhou.
- `test.skip` sem registro no relatório.
