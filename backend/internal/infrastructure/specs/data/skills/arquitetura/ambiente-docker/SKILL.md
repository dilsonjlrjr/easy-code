---
name: ambiente-docker
description: Ambiente reproduzível com Docker Compose para front + API + PostgreSQL - serviços, redes, volumes, healthchecks e depends_on com condição, variáveis e .env.example, perfis (dev/teste), seed e usuários de teste, Dockerfiles multi-stage (compilado, interpretado, front estático) com usuário não root e cache de camadas, ambiente efêmero para testes de integração e e2e, um comando para subir e outro para testar, troubleshooting e checklist da T-01 (esqueleto). Use na etapa de Arquitetura ao planejar a T-01 e na de Dev ao montar ou depurar o ambiente.
---

# Ambiente com Docker Compose

Skill das etapas de **Arquitetura** (planejar a T-01 e o ambiente) e **Dev** (montar e
manter). Serve a qualquer app web com front + API + PostgreSQL; a linguagem do backend não
muda o desenho do Compose, só o Dockerfile da API. Detalhes de cada linguagem ficam nas
skills de `linguagens/`. Exemplos usam o projeto de referência (reserva de salas) só como ilustração.

## Para que serve neste processo

Dev, QA e Segurança precisam rodar **o mesmo sistema do mesmo jeito**, sem "na minha máquina
funciona". O ambiente é parte do produto: `criterios_pronto` de toda T inclui "sobe e
testa com um comando". Um agente que não consegue subir o app não consegue provar nada.

> **Onde isto roda.** `docker-compose.yml` e Dockerfiles são artefatos entregues, para CI e humanos; `make up` e `docker compose ...` são **para CI/humano**. No sandbox da plataforma o agente não tem Docker: roda integração/E2E contra os **serviços de apoio declarados no perfil** (ex.: PostgreSQL efêmero) subidos pelo runner, só com comandos da allowlist da sua categoria. Nesse caso, o agente valida os arquivos Compose/Dockerfile por leitura e os executa apenas no CI.

## Princípios

1. **Um comando para subir, um para testar.** `make up` e `make test` (ou `scripts/`),
   documentados no README. Todo o resto é detalhe.
2. **Reprodutível do zero.** Clonar, copiar `.env.example`, subir. Sem passos manuais ocultos.
3. **Pronto significa saudável.** `healthcheck` nos serviços e `depends_on` com
   `condition: service_healthy`; o front/API só sobem quando o banco responde.
4. **Dev e produção parecidos.** Mesma imagem base e mesmo Dockerfile (estágios), difere
   configuração por variável e perfil.
5. **Segredos fora da imagem e do Git.** `.env` ignorado; `.env.example` com valores de
   desenvolvimento **fictícios**.
6. **Imagens mínimas e sem root.** Multi-stage, usuário não privilegiado, só o necessário na imagem final.
7. **Efêmero por padrão nos testes.** Banco de teste recriado do zero, sem depender de estado anterior.
8. **Simples.** Três serviços (web, api, db) bastam; sem proxy, fila ou cache sem requisito.

## Fluxo de trabalho

1. **Definir serviços e portas** (`references/compose.md`).
2. **Escrever Dockerfiles** por tipo de stack (`references/dockerfiles.md`).
3. **Configurar variáveis**: `.env.example`, `env_file`, valores por serviço.
4. **Healthchecks e ordem de subida**.
5. **Perfis** dev/teste; **seed** e usuários de teste.
6. **Comandos** (`Makefile` ou scripts) e ambiente efêmero para testes
   (`references/testes-efemeros-e-comandos.md`).
7. **Validar a T-01** com o checklist abaixo; consultar `references/troubleshooting.md` ao falhar.

## Estrutura de arquivos

```
app/
  docker-compose.yml        (ou compose.yaml)
  .env.example
  .dockerignore             (um por contexto de build)
  Makefile
  backend/Dockerfile
  frontend/Dockerfile
  e2e/                      (testes; roda no perfil de teste)
  db/seed.sql               (dados de desenvolvimento e teste)
```

## Regras de decisão

| Situação | Decisão |
|---|---|
| Quando o banco "ainda não está pronto" na subida | `healthcheck` + `depends_on: condition: service_healthy`, não `sleep` |
| Dados do PostgreSQL | Volume nomeado em dev; sem volume (ou `tmpfs`) em teste efêmero |
| Segredo (senha, chave) | Variável de ambiente via `.env` (ignorado pelo Git); nunca no Dockerfile |
| Linguagem compilada (Go, Rust, Java) | Multi-stage: build com toolchain, runtime mínimo |
| Linguagem interpretada (Python) | Imagem slim, dependências em camada própria, usuário não root |
| Front Vue | Build em Node, servir estático por servidor web leve (nginx) |
| Hot reload em desenvolvimento | Perfil `dev` com bind mount; produção sem bind mount |
| Teste de integração/e2e | Perfil `test` ou projeto Compose separado, efêmero, derrubado ao fim |
| Seed | Script separado, executado por comando; nunca na migração de produção |
| Porta do host ocupada | Porta configurável por variável (`${API_PORT:-8080}`) |
| Precisa de proxy reverso/HTTPS | Só com RNF; ADR |
| Imagem `latest` | Evitar; fixar o major ("estável atual"), conferir |

## Portas padrão (sugestão, ajustáveis por `.env`)

| Serviço | Porta no host | Dentro da rede |
|---|---|---|
| Front | 5173 (dev) / 8081 (estático) | `web:80` ou `web:8080` |
| API | 8080 | `api:8080` |
| PostgreSQL | 5432 (opcional expor) | `db:5432` |

Serviços falam entre si por **nome do serviço** (`db`, `api`), não `localhost`. Expor o banco
no host é conveniência de desenvolvimento; em teste e produção, não.

## Checklist da T-01 (esqueleto)

- [ ] `docker compose up` (via `make up`) sobe front, API e banco, todos **healthy**.
- [ ] `GET /saude` da API responde 200 verificando o banco.
- [ ] Front abre em `http://localhost:<porta>` e fala com a API.
- [ ] Migração base aplicada automaticamente (ou por `make migrate`).
- [ ] `make test` roda: um teste vazio passando no backend, no front e no e2e.
- [ ] `make lint` (lint e formatação) limpo.
- [ ] `.env.example` completo, sem segredo real; `.env` no `.gitignore`.
- [ ] `.dockerignore` em cada contexto de build.
- [ ] Imagens multi-stage, usuário não root.
- [ ] `make seed` cria usuários de teste por papel (se houver autenticação na T-01 ou logo depois).
- [ ] `make down` limpa; `make reset` recria do zero (volumes removidos).
- [ ] README documenta pré-requisitos (Docker, Compose v2), comandos e portas.
- [ ] Do zero: clone limpo + copiar `.env.example` + `make up` funciona.

## Erros comuns

| Erro | Efeito | Correção |
|---|---|---|
| `depends_on` simples (só ordem de início) | API sobe antes do banco aceitar conexão | `condition: service_healthy` |
| `sleep 10` para esperar o banco | Frágil e lento | Healthcheck |
| Senha no Dockerfile ou no Compose versionado | Vazamento | `.env` + `.env.example` fictício |
| Imagem com toolchain de build em produção | Grande e com superfície de ataque | Multi-stage |
| Processo como root | Risco de escape | `USER` não privilegiado |
| Copiar tudo antes de instalar dependências | Cache de camadas invalidado a cada mudança | Copiar manifestos de dependência primeiro |
| Sem `.dockerignore` | Contexto gigante, `.git`/`node_modules` na imagem | `.dockerignore` |
| `localhost` dentro do contêiner para chegar ao banco | Conexão recusada | Nome do serviço (`db`) |
| `init.sql` esperado após o primeiro start | `/docker-entrypoint-initdb.d` só roda com volume vazio | `make reset` ou migração por ferramenta |
| `latest` sem versão | Mudança silenciosa | Fixar major |
| Testes que dependem de estado do dev | Flaky | Ambiente efêmero |
| Expor o banco em teste/produção | Superfície desnecessária | Só na rede interna |

## Índice de references

| Arquivo | Quando ler |
|---|---|
| `references/compose.md` | Ao escrever `docker-compose.yml`, `.env.example`, healthchecks, perfis, volumes e seed |
| `references/dockerfiles.md` | Ao escrever Dockerfiles (compilado, interpretado, front), `.dockerignore`, usuário não root, cache |
| `references/testes-efemeros-e-comandos.md` | Ao montar testes de integração/e2e efêmeros, Makefile e scripts |
| `references/troubleshooting.md` | Quando algo não sobe, não conecta ou está lento |

Skills relacionadas: `../arquitetura-web/` (T-01 no plano), `../../dados/modelagem-postgresql/`
(migração e seed), `../../seguranca/revisao-seguranca/` (segredos, imagens), `../../linguagens/`
(Dockerfile idiomático por linguagem, quando existirem).
