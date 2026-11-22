# Docker Compose: front + API + PostgreSQL

> **Onde isto roda.** `docker-compose.yml` e Dockerfiles do projeto gerado continuam sendo artefatos entregues, para CI e para humanos. Os comandos `docker compose ...` e `docker run ...` deste arquivo são **para CI/humano**. No sandbox da plataforma o agente não tem Docker nem rede: testes de integração/E2E rodam contra os **serviços de apoio declarados no perfil** (ex.: PostgreSQL efêmero) que o runner sobe, e só com os comandos da allowlist da sua categoria (`testes`, `migracoes`, `banco_descartavel`).

Usa o Compose V2 (`docker compose`, plugin do Docker). O formato é a Compose Specification;
o campo `version:` é obsoleto e deve ser omitido. Arquivo: `docker-compose.yml` (ou
`compose.yaml`, nome preferido pela documentação atual; ambos funcionam). Recursos abaixo:
conferir a versão do Compose instalada quando houver "(conferir)".

## Arquivo completo comentado

```yaml
name: reserva-salas           # nome do projeto Compose (prefixo de redes/volumes)

services:
  db:
    image: postgres:16-alpine          # fixe o major estável atual (conferir); evite latest
    restart: unless-stopped
    environment:
      POSTGRES_DB: ${DB_NAME}
      POSTGRES_USER: ${DB_USER}
      POSTGRES_PASSWORD: ${DB_PASSWORD}
    volumes:
      - pgdata:/var/lib/postgresql/data
    ports:
      - "${DB_PORT:-5432}:5432"        # só em dev; remova em teste/produção
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U $${POSTGRES_USER} -d $${POSTGRES_DB}"]
      interval: 5s
      timeout: 3s
      retries: 10
      start_period: 10s
    networks: [interna]

  api:
    build:
      context: ./backend
      target: runtime                  # estágio final do Dockerfile
    restart: unless-stopped
    env_file: [.env]
    environment:
      DATABASE_URL: postgresql://${DB_USER}:${DB_PASSWORD}@db:5432/${DB_NAME}
      APP_ENV: ${APP_ENV:-dev}
    depends_on:
      db:
        condition: service_healthy     # espera o healthcheck do banco
    ports:
      - "${API_PORT:-8080}:8080"
    healthcheck:
      test: ["CMD-SHELL", "wget -qO- http://localhost:8080/saude || exit 1"]
      interval: 10s
      timeout: 3s
      retries: 5
      start_period: 20s
    networks: [interna]

  web:
    build:
      context: ./frontend
      target: runtime
      args:
        VITE_API_URL: ${PUBLIC_API_URL:-http://localhost:8080}
    restart: unless-stopped
    depends_on:
      api:
        condition: service_healthy
    ports:
      - "${WEB_PORT:-8081}:8080"
    networks: [interna]

volumes:
  pgdata:

networks:
  interna:
```

Notas:
- `$${VAR}` (dólar duplo) evita que o Compose interpole no host; o shell do contêiner expande.
- `wget` existe em imagens alpine (BusyBox); em imagens slim/distroless talvez não exista
  `curl`/`wget`: use binário próprio no `healthcheck` ou instale o mínimo (ver
  `troubleshooting.md`). Imagens distroless muitas vezes pedem um subcomando do próprio binário.
- Variáveis `${...}` vêm do `.env` na pasta do Compose (carregado automaticamente para
  interpolação). `env_file` injeta variáveis no contêiner.
- O `DATABASE_URL` com senha com caracteres especiais exige codificação (percent-encoding).
  Prefira senhas de desenvolvimento simples (alfanuméricas).

## `.env.example`

```dotenv
# Copie para .env e ajuste. NUNCA commitar o .env. Valores abaixo são só de desenvolvimento.
APP_ENV=dev
DB_NAME=reservas
DB_USER=reservas
DB_PASSWORD=dev_senha_trocar
DB_PORT=5432
API_PORT=8080
WEB_PORT=8081
PUBLIC_API_URL=http://localhost:8080

# Usuários de teste (usados apenas pelo seed de desenvolvimento/teste)
SEED_ADMIN_EMAIL=admin@exemplo.local
SEED_ADMIN_PASSWORD=admin_dev_123
SEED_USER_EMAIL=usuario@exemplo.local
SEED_USER_PASSWORD=usuario_dev_123
```

Regras: toda variável usada existe aqui, com comentário; nenhum segredo real; o `.env` vai
no `.gitignore`; a API **falha na subida** se faltar variável obrigatória (config validada).

## Healthchecks e ordem de subida

| Serviço | Teste de saúde | Observação |
|---|---|---|
| db | `pg_isready -U user -d db` | Pronto para aceitar conexões |
| api | `GET /saude` (200 e verifica o banco) | `start_period` cobre compilação/migração na subida |
| web | opcional (`wget` na raiz) | Serve estático |

`depends_on` com `condition: service_healthy` só garante a ordem **na subida**; a aplicação
deve **reconectar** ao banco se ele reiniciar (pool com retry). Opções de `condition`:
`service_started`, `service_healthy`, `service_completed_successfully` (útil para um serviço
de migração que roda e termina).

### Serviço de migração (opcional)

```yaml
  migrar:
    build: { context: ./backend, target: runtime }
    command: ["migrar"]                 # comando da imagem/ferramenta que aplica as migrações
    env_file: [.env]
    depends_on: { db: { condition: service_healthy } }
    networks: [interna]
    profiles: ["tools"]
  api:
    depends_on:
      migrar: { condition: service_completed_successfully }
```

Use quando a migração não deve rodar na subida da API. (Se usar `profiles` no serviço do
qual a API depende, ative o perfil sempre; para uso simples, rode migração dentro da API.)

## Perfis (profiles)

Perfis ativam serviços opcionais: `docker compose --profile test up`.

```yaml
  e2e:
    build: { context: ./e2e }
    profiles: ["test"]
    environment:
      BASE_URL: http://web:8080
      API_URL: http://api:8080
    depends_on:
      web: { condition: service_started }
      api: { condition: service_healthy }
    networks: [interna]

  mailhog:                    # exemplo de ferramenta só de dev, se houver e-mail
    image: mailhog/mailhog
    profiles: ["dev"]
```

Serviços sem `profiles` sobem sempre; com `profiles` só quando o perfil é ativado. Regra:
núcleo (db, api, web) sem perfil; ferramentas e testes com perfil.

## Dev com hot reload (opcional)

Arquivo de sobreposição `docker-compose.dev.yml` (ou `compose.override.yaml`, lido
automaticamente):

```yaml
services:
  api:
    build: { target: dev }          # estágio de dev no Dockerfile
    volumes: ["./backend:/app"]     # bind mount do código
    command: ["<comando de recarga da linguagem>"]
  web:
    build: { target: dev }
    volumes: ["./frontend:/app", "/app/node_modules"]
    ports: ["5173:5173"]
```

`docker compose -f docker-compose.yml -f docker-compose.dev.yml up`. Em app pequeno, uma
só configuração (sem hot reload dentro do contêiner) é aceitável: o dev roda front e API no
host e usa o Compose só para o banco (`make db`). Decidir em `estrutura.md`.

## Redes

- Uma rede `interna` padrão basta. Serviços se resolvem por nome.
- Só `web` (e `api`, se o front chama direto do navegador) expõem portas ao host.
- Segurança: o banco não precisa de porta publicada em teste/produção.
- Navegador acessa a API por **URL pública** (`localhost:8080`), não por `api:8080`;
  `PUBLIC_API_URL` é o que o front usa. Chamadas servidor-a-servidor usam o nome interno.
  Alternativa: o servidor do front faz proxy `/api` -> `api:8080`, evitando CORS.

## Volumes

| Volume | Uso | Observação |
|---|---|---|
| `pgdata` (nomeado) | Dados do PostgreSQL em dev | Persistem entre `down`; apagados por `down -v` |
| bind mount (`./backend:/app`) | Só dev | Cuidado com permissões e desempenho (macOS) |
| `tmpfs` | Dados efêmeros em teste (`tmpfs: [/var/lib/postgresql/data]`) | Rápido; perde tudo ao parar |

A pasta `/docker-entrypoint-initdb.d` da imagem do PostgreSQL executa `*.sql`/`*.sh` **apenas
quando o diretório de dados está vazio** (primeira criação). Não é mecanismo de migração.

## Seed e usuários de teste

- Banco inicial: migrações (ferramenta do projeto) criam o esquema.
- Dados de teste: comando `make seed` executa `db/seed.sql` (ou um comando da API) **depois**
  da migração, e é **idempotente**.
- Usuários de teste: um por papel; e-mail/senha vêm do `.env.example` (`SEED_*`); o hash da
  senha é gerado pela própria aplicação (comando de seed da API), não colado no SQL.
- O seed se recusa a rodar com `APP_ENV=production`.
- Para o QA: o seed é determinístico; mesmo conjunto sempre.

```bash
# exemplo: seed por SQL
docker compose exec -T db psql -U "$DB_USER" -d "$DB_NAME" < db/seed.sql
```

## Recursos e segurança básicos

- `restart: unless-stopped` em dev/produção; ausente em testes efêmeros.
- Limites (`mem_limit`, `cpus`) só com necessidade.
- Imagens fixadas por major (e por digest em produção: conferir política).
- Sem `privileged`, sem montar `/var/run/docker.sock`.
- `read_only: true` + `tmpfs` para `/tmp` na API, se a aplicação permitir (opcional).

## Validar o arquivo

```bash
docker compose config            # resolve variáveis e valida a sintaxe
docker compose up --build -d
docker compose ps                # STATUS deve mostrar (healthy)
docker compose logs -f api
```

## Checklist

- [ ] Serviços `db`, `api`, `web` com healthcheck e `depends_on` com condição.
- [ ] `.env.example` completo, comentado, sem segredo real; `.env` ignorado.
- [ ] Volume nomeado para o banco; testes sem estado persistente.
- [ ] Portas parametrizadas; banco exposto só em dev.
- [ ] Perfis para ferramentas e testes; núcleo sem perfil.
- [ ] Serviços se comunicam por nome; URL pública para o navegador.
- [ ] Seed idempotente, bloqueado em produção, com usuários de teste por papel.
- [ ] `docker compose config` válido.

## Erros comuns

| Erro | Correção |
|---|---|
| `version:` no topo | Remover (obsoleto) |
| `$VAR` do contêiner interpolado no host | `$$VAR` |
| Navegador chamando `http://api:8080` | URL pública (`localhost:PORTA`) ou proxy |
| API sobe e cai por o banco ainda não estar pronto | `service_healthy` + retry de conexão |
| Esperar init SQL após o primeiro start | Migração por ferramenta; `make reset` |
| Porta 5432 ocupada no host | `DB_PORT` configurável |
| `.env` versionado | `.gitignore`; só `.env.example` |
| `latest` nas imagens | Fixar major |
