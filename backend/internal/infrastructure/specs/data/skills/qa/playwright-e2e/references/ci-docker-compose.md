# Execução em CI e contra Docker Compose

> **Onde isto roda.** `docker-compose.yml` e Dockerfiles do projeto gerado continuam sendo artefatos entregues, para CI e para humanos. Os comandos `docker compose ...` e `docker run ...` deste arquivo são **para CI/humano**. No sandbox da plataforma o agente não tem Docker nem rede: testes de integração/E2E rodam contra os **serviços de apoio declarados no perfil** (ex.: PostgreSQL efêmero) que o runner sobe, e só com os comandos da allowlist da sua categoria (`testes`, `migracoes`, `banco_descartavel`).

Objetivo: o mesmo comando roda do zero na máquina do QA, no CI e na de quem for revisar, com
resultado idêntico. Confirmar versões e nomes de serviço em `agentes/ambiente.md`.

## 1. Fluxo canônico (local ou CI)

```bash
# 1. estado limpo
docker compose down -v --remove-orphans
# 2. subir tudo e aguardar saúde (requer healthcheck nos serviços)
docker compose up -d --build --wait
# 3. seed (comando documentado no README do app)
docker compose exec -T api <comando-de-seed-do-README>
# 4. rodar a suíte
cd app/e2e
npm ci
npx playwright install --with-deps chromium
BASE_URL=http://localhost:3000 API_URL=http://localhost:8080 npx playwright test
# 5. relatório
npx playwright show-report
# 6. derrubar
cd .. && docker compose down -v
```

Notas:
- `down -v` remove volumes: o banco nasce limpo e o seed define o estado conhecido.
- `--wait` espera os serviços com `healthcheck` ficarem saudáveis (Compose v2 recente; se a
  versão não suportar, use um laço de espera consultando a URL de saúde). **Nunca** `sleep 30` cego.
- `exec -T` desativa TTY (necessário em CI/script).
- Portas e nomes de serviço vêm do `docker-compose.yml` do projeto; o README do app é a fonte.

### Laço de espera portátil

```bash
for i in $(seq 1 60); do
  curl -fsS "$API_URL/health" >/dev/null && break
  sleep 2
  [ "$i" = 60 ] && { echo "API não ficou saudável"; docker compose logs api; exit 1; }
done
```

## 2. Healthchecks: peça ao Dev

Compose só espera o que declara `healthcheck`. Exemplo no `docker-compose.yml` do app (Dev
escreve; QA verifica que existe):

```yaml
services:
  db:
    image: postgres:<versão>
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U $${POSTGRES_USER}"]
      interval: 5s
      timeout: 3s
      retries: 10
  api:
    depends_on:
      db: { condition: service_healthy }
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:8080/health"]
      interval: 5s
      retries: 20
```

Healthcheck ausente na API/front e sem alternativa de espera = ACH Médio com `etapa_destino: implementacao`
(ambiente não reproduzível com confiança).

## 3. Testes rodando **dentro** do Compose (opcional)

Imagem oficial do Playwright (publicada pela Microsoft, com navegadores e dependências):
`mcr.microsoft.com/playwright:<tag-da-versão-instalada>` — a tag deve **coincidir com a versão do
`@playwright/test`** do lockfile. Útil para evitar instalar navegadores e padronizar fontes.

```yaml
  e2e:
    image: mcr.microsoft.com/playwright:<tag-da-versão-do-playwright>
    working_dir: /work
    volumes: ["./e2e:/work"]
    environment:
      BASE_URL: http://web:3000      # nome do serviço na rede do Compose, não localhost
      API_URL: http://api:8080
      CI: "true"
    depends_on:
      web: { condition: service_healthy }
      api: { condition: service_healthy }
    command: sh -c "npm ci && npx playwright test"
    profiles: ["e2e"]               # só sobe quando pedido
```

Execução: `docker compose --profile e2e run --rm e2e`. Dentro da rede do Compose o host é o
**nome do serviço**; fora, `localhost` + porta publicada. Mantenha **uma** forma como padrão
documentada no README/REL-QA.

## 4. CI genérico (qualquer provedor)

Passos que qualquer pipeline repete: checkout -> instalar Node -> `docker compose up -d --build
--wait` -> seed -> `npm ci` no `app/e2e` -> `npx playwright install --with-deps chromium` (ou
imagem oficial) -> `npx playwright test` -> **publicar artefatos** (sempre, mesmo em falha) ->
`docker compose down -v`.

Exemplo no formato GitHub Actions (ilustrativo; adapte ao provedor; versões de actions:
conferir em `agentes/ambiente.md`):

```yaml
name: e2e
on: [push, pull_request]
jobs:
  e2e:
    runs-on: ubuntu-latest
    timeout-minutes: 20
    steps:
      - uses: actions/checkout@<versão-estável>
      - uses: actions/setup-node@<versão-estável>
        with: { node-version: lts/* }
      - name: Subir ambiente
        run: docker compose up -d --build --wait
      - name: Seed
        run: docker compose exec -T api <comando-de-seed>
      - name: Instalar dependências do e2e
        working-directory: app/e2e
        run: |
          npm ci
          npx playwright install --with-deps chromium
      - name: Rodar testes
        working-directory: app/e2e
        env: { CI: "true", BASE_URL: "http://localhost:3000", API_URL: "http://localhost:8080" }
        run: npx playwright test
      - name: Logs em caso de falha
        if: failure()
        run: docker compose logs --no-color > compose-logs.txt
      - name: Publicar relatório
        if: always()
        uses: actions/upload-artifact@<versão-estável>
        with:
          name: playwright-report
          path: |
            app/e2e/playwright-report/
            app/e2e/test-results/
            compose-logs.txt
          retention-days: 14
      - name: Derrubar
        if: always()
        run: docker compose down -v
```

## 5. Paralelismo e recursos

- `workers`: comece com 2 no CI; aumente medindo falsa intermitência. A API/banco em contêiner
  na mesma máquina disputam CPU com os navegadores.
- **Fragmentação** (`--shard=1/3`) para dividir a suíte entre jobs, quando crescer; relatórios
  `blob` e merge (`npx playwright merge-reports`); só se houver necessidade real.
- Limite de memória do Docker e `shm_size`: Chromium em contêiner pode precisar de `--ipc=host`
  ou `shm_size: "1gb"` se travar por memória compartilhada (sintoma: crash aleatório do navegador).

## 6. Artefatos e relatório

Sempre publicar (mesmo em falha): `playwright-report/` (HTML), `test-results/` (trace,
screenshot, vídeo), logs do Compose. O REL-QA referencia o comando de reprodução; o CI é
conveniência, a reprodução manual é a verdade.

## 7. Variáveis e segredos

- Credenciais de teste vêm do ambiente local/`.env.example` documentado; no CI, variáveis do
  pipeline para eventuais segredos; **nunca** no repositório.
- `CI=true` ativa `forbidOnly` e `workers` do config.
- Não apontar jamais o teste para ambiente que não seja o local do projeto.

## 8. Verificação "do zero" antes de entregar

Antes de escrever o REL-QA, execute **literalmente** os comandos que vai publicar em uma pasta
limpa (clone novo) ou após `down -v` + remoção de `node_modules`. Registre:

- duração total;
- versão do Node, do Playwright (`npx playwright --version`), do Docker/Compose
  (`docker --version`, `docker compose version`);
- número de testes e resultado.

## 9. Anti-padrões

| Anti-padrão | Efeito | Correção |
|---|---|---|
| `sleep 30` após `up` | Lento e ainda instável | `--wait`/laço de saúde |
| Ambiente reaproveitado entre execuções | Falhas que dependem de sujeira | `down -v` antes |
| Imagem do Playwright com tag diferente do lockfile | Erro de versão de navegador | Tag igual à versão do pacote |
| Testes dentro do Compose usando `localhost` | Não alcança os serviços | Nome do serviço |
| Relatório só em caso de sucesso | Sem evidência quando importa | `if: always()` |
| Comando de CI diferente do documentado no REL-QA | Avaliador não reproduz | Mesmo comando |
