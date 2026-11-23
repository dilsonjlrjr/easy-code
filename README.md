# Easy Code

**Uma fábrica de software orientada por IA, com governança.** Você descreve uma necessidade de negócio.
Sete agentes especializados levam essa necessidade até uma aplicação documentada, testada, segura e
rastreável. As pessoas do time aprovam cada passo importante.

Ferramentas de código com IA aceleram o trabalho individual, mas falham em times dentro de empresas. Faltam
requisitos validados, decisões de arquitetura registradas, testes ligados aos requisitos, segurança desde o
desenho e controle sobre o que a IA muda. O Easy Code organiza o trabalho da IA como um processo de engenharia:
cada agente cuida de uma disciplina, um orquestrador coordena todos e nada avança sem passar pelos gates de
qualidade.

## Como funciona

1. **Projeto.** Um líder cria o projeto com a necessidade de negócio, o orçamento e o par de modelos de IA. O
   par tem um modelo de raciocínio para decidir e um modelo de execução para produzir. O líder também atribui
   os papéis do time.
2. **Etapas.** O workflow passa pelos agentes de **Requisitos, Arquitetura, Dados (DBA), Design,
   Desenvolvimento, QA e Segurança**. Cada agente produz artefatos no seu formato: especificações, decisões de
   arquitetura, modelo de dados, telas, código, testes e análise de riscos.
3. **Ondas.** Cada execução de uma etapa é uma onda. A tela da onda mostra ao vivo o que o agente faz, quanto
   custa em tokens e em dinheiro e quanto tempo leva.
4. **Gates.** Ao fim de cada etapa, um gate confere as regras de qualidade. Nos pontos críticos, o gate espera
   a aprovação de uma pessoa com o papel certo. Quem aprova vê os artefatos e as checagens antes de decidir.
5. **Achados.** Problemas encontrados por agentes ou pessoas viram achados com severidade e responsável.
   Um achado Crítico ou Alto bloqueia o gate até ser corrigido ou ter o risco aceito por uma pessoa.
6. **Release.** No fim, o orquestrador monta a Release com tudo o que foi produzido. Uma pessoa aprova a
   Release antes da entrega.
7. **Entrega.** O resultado vai para um repositório git do projeto, com o código e a documentação em Markdown
   em `docs/`. Cada requisito fica ligado às decisões, ao código e aos testes que o atendem.

## Recursos

- **Sete agentes especializados**, com instruções e skills versionadas, executados em sandbox isolado.
- **Governança configurável:** papéis por projeto, permissões, gates com aprovação humana e trilha de auditoria
  de cada ação.
- **Rastreabilidade** de ponta a ponta, do requisito ao teste.
- **Custo sob controle:** orçamento por projeto, alertas e custo por onda, etapa, projeto e provedor, a partir
  de uma tabela de preços versionada.
- **Provedores de IA:** Anthropic e OpenAI atrás do mesmo contrato. O par de modelos pode misturar os dois.
- **Chaves de API protegidas:** a chave é cadastrada pela web e só o gateway de IA consegue abri-la.
- **Login corporativo** por OpenID Connect (Keycloak incluído no ambiente de desenvolvimento).
- **Aceite de risco registrado:** quem aceita um risco não aprova a Release do mesmo ciclo.
- **App desktop** somente leitura, para acompanhar ondas e receber notificações do sistema operacional.
- **Interface em português (padrão) e inglês.**
- **Logs estruturados** com remoção automática de segredos e de conteúdo de prompts e artefatos.

## Arquitetura

| Componente | Função | Tecnologia |
|---|---|---|
| Web | Interface do time | Vue 3, TypeScript, Vuetify 3 |
| API | Regras de negócio, governança e eventos ao vivo (SSE) | Go, fasthttp |
| Worker | Orquestrador: executa o workflow, as ondas e os gates | Go |
| Gateway de IA | Único processo que fala com os provedores e abre as chaves | Go |
| Sandbox | Executa o trabalho dos agentes em containers isolados | Go, Docker |
| Banco | Estado da plataforma | PostgreSQL |
| Identidade | Login e papéis da organização | Keycloak (OIDC) |
| App desktop | Acompanhamento e notificações, somente leitura | Go, Wails 3, Vue 3 |

## Requisitos

- Linux x86_64 com **Docker** (Engine 24 ou mais novo, com o plugin `docker compose`) e **make**.
- Nada mais no host. Go, Node, Python e Playwright rodam dentro de containers.
- Portas livres em `127.0.0.1`: 8080 (web), 8081 (API), 8180 (Keycloak) e 5432 (PostgreSQL).

## Início rápido

```sh
git clone git@github.com:dilsonjlrjr/easy-code.git
cd easy-code

# grupo do socket do Docker, usado pelo sandbox dos agentes
export EASYCODE_DOCKER_GID=$(stat -c %g /var/run/docker.sock)

docker compose up --build -d
```

A primeira subida demora alguns minutos, porque compila as imagens. O serviço `migrate` aplica as migrações do
banco e termina. O serviço `gateway-keys` gera o par de chaves da plataforma uma única vez. Depois sobem a API, o
worker, o gateway, o sandbox e a web.

| Serviço | Endereço |
|---|---|
| Web | http://localhost:8080 |
| API | http://localhost:8081/api/v1/health (liveness) e `/api/v1/ready` (readiness) |
| Keycloak | http://localhost:8180/auth |

### Usuários de desenvolvimento

Todos usam a senha `dev-only-password`. Valem apenas para desenvolvimento, porque o realm do Keycloak está
público neste repositório.

| Usuário | Papel |
|---|---|
| `admin.dev` | Administrador da organização |
| `lead.dev`, `approver.dev`, `member.dev` | Sem papel de projeto. Os papéis são atribuídos na plataforma |
| `disabled.dev` | Conta desativada, para teste |

### Primeiro uso

1. Entre como `admin.dev`.
2. Em Configurações, cadastre a chave do provedor de IA e a tabela de preços dos modelos.
3. Crie um projeto: informe a necessidade de negócio, o par de modelos e o orçamento. Atribua os papéis.
4. Inicie o workflow e acompanhe as ondas, os gates e os achados.

### Provedor de IA real

Por padrão, o gateway roda **sem rede**, com um provedor falso e determinístico que atende pelos nomes
`anthropic` e `openai`. Esse provedor aceita qualquer chave, exceto `invalid`. Para usar um provedor real:

```sh
# Anthropic
docker compose -f compose.yaml -f compose.anthropic.yaml up --build -d

# Anthropic e OpenAI
EASYCODE_GATEWAY_PROVIDERS=anthropic,openai \
  docker compose -f compose.yaml -f compose.anthropic.yaml -f compose.openai.yaml up --build -d
```

Esses arquivos liberam acesso à internet para o gateway, sem lista de destinos permitidos. Use apenas em máquina
de desenvolvimento.

### Backup

```sh
docker compose --profile backup up -d backup   # pg_dump e cópia dos repositórios, com retenção de 30 dias
```

Para restaurar, use `infra/backup/restore.sh`.

### Parar e limpar

```sh
docker compose down        # para os serviços e mantém os dados
docker compose down -v     # apaga também banco, repositórios e chaves
```

## App desktop

```sh
make -C desktop build   # gera desktop/bin/easycode-desktop (Linux amd64)
EASYCODE_SERVER=http://localhost:8081 desktop/bin/easycode-desktop
```

O app precisa de GTK 4.10 ou mais novo (Debian 13, Ubuntu 24.04 ou mais novos) e de um cofre de credenciais do
sistema (GNOME Keyring ou KWallet). O login é feito pelo navegador, com um código de dispositivo. Builds para
macOS e Windows estão em [desktop/README.md](desktop/README.md).

## Desenvolvimento

| Pasta | Conteúdo |
|---|---|
| [backend/](backend/) | API, worker, gateway e sandbox em Go. Camadas `internal/{domain,application,infrastructure,interfaces/http}` e migrações SQL em `migrations/` |
| [frontend/](frontend/) | Web em Vue 3, Vite, TypeScript, Pinia, vue-i18n e Vuetify 3. Pastas `src/{app,features,shared}` |
| [desktop/](desktop/) | App desktop em Wails 3 |
| [infra/](infra/) | Keycloak, imagens do sandbox, proxy de dependências e backup |
| [compose.yaml](compose.yaml) | Ambiente de desenvolvimento completo |

O código segue Clean Architecture e está em inglês. A interface é traduzida com i18n e não aceita texto fixo nos
templates.

### Backend

```sh
make -C backend build   # compila os binários e roda go vet
make -C backend test    # testes
make -C backend lint    # golangci-lint
make -C backend image   # imagem distroless, sem root
```

Variáveis principais: `EASYCODE_DATABASE_URL`, `EASYCODE_MIGRATION_DATABASE_URL`, `EASYCODE_HTTP_ADDR`,
`EASYCODE_LOG_LEVEL` e `EASYCODE_OTEL_ENABLED`/`EASYCODE_OTEL_ENDPOINT`. O código não tem endereços fixos.

Os logs saem em JSON na saída padrão, ligados ao trace id. O redator central remove chaves, tokens, cookies,
senhas, `Authorization` e o conteúdo de prompts e artefatos.

### Frontend

```sh
cd frontend
npm ci
npm run dev         # servidor de desenvolvimento
npm run lint        # eslint e prettier
npm run test        # vitest
npm run build       # type-check e bundle
npm run e2e:docker  # Playwright dentro da imagem oficial
```

A URL da API vem de `config.js`, gerado na subida do container a partir de `API_BASE_URL`.

### Testes e CI

Um único ponto de entrada, que só precisa de Docker e make no host. Qualquer executor de CI pode chamar:

```sh
make ci                       # backend, frontend e testes end-to-end (Playwright)
make desktop-ci               # app desktop (fora do ci: a primeira imagem tem cerca de 1,6 GB)
make -C backend load-test     # teste de carga
```

| Alvo | O que roda |
|---|---|
| `backend-ci` | Lint, testes com PostgreSQL descartável e build do backend |
| `frontend-ci` | Lint, type-check, testes (Vitest) e build |
| `frontend-e2e` | Playwright contra o build de produção |
| `specs` | Validação da especificação do domínio. Só roda quando a pasta de especificação existe ao lado deste repositório |

Se os testes do backend não conseguirem conectar ao PostgreSQL, a faixa de rede automática do Docker pode não
ser roteável no host. Nesse caso, fixe uma sub-rede: `EASYCODE_CI_SUBNET=10.250.50.0/24 make ci`.

O backend embute cópias das especificações do domínio (workflow, gates, permissões, esquemas de artefatos e
instruções dos agentes) em `backend/internal/infrastructure/specs/data`. Por isso o build não depende de nada
fora deste repositório. Quem mantém a especificação usa `make -C backend check-specs` e `sync-specs`, com
`SPECS_SOURCE` apontando para a pasta da especificação.

## Antes de produção

O `compose.yaml` serve só para desenvolvimento:

- Todos os segredos dele são públicos.
- Ele conecta ao banco como superusuário. Em produção, cada processo usa um papel próprio no banco.
- Ele não tem proxy TLS nem proxy de saída.
