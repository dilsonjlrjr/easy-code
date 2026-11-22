# Segurança de contêiner e configuração

Cobre A02 (configuração insegura), A03 (cadeia de suprimentos) e A04 (segredos). Escopo na
aplicação pequena: `Dockerfile`, `docker-compose.yml`, variáveis de ambiente, arquivos de configuração e
a imagem construída. **Ataques/provas apenas no Compose local do projeto.**

## 1. Onde olhar, em ordem

1. `Dockerfile` (cada serviço).
2. `docker-compose.yml` (portas, volumes, variáveis, redes, usuários).
3. Arquivos de configuração da aplicação (`application.yml`, `.env*`, `config/*.json`).
4. Seed e scripts de inicialização.
5. A imagem pronta (`trivy image`) e o que está dentro dela.
6. Pipeline de build/CI (se existir): origem das imagens e ações.

## 2. Dockerfile: o que revisar

| Item | Esperado | Falha típica |
|---|---|---|
| Imagem base | oficial, **versão fixada** (e idealmente por digest), enxuta (`slim`, `distroless`, `alpine` quando compatível) | `FROM x:latest`; imagem de origem desconhecida |
| Usuário | **não root** (`USER app`) | Processo roda como root |
| Estágios | **multi-stage**: build separado do runtime; só o necessário na imagem final | Compilador, `.git`, testes, chaves na imagem final |
| Segredos | **nenhum** em `ENV`, `ARG` ou `COPY`; usar segredo de build (`RUN --mount=type=secret`) se necessário | `ENV DB_PASSWORD=...`; `.env` copiado para a imagem |
| `COPY . .` | com `.dockerignore` (exclui `.git`, `.env`, `node_modules`, chaves, testes) | Copia credenciais e histórico |
| Instalação de pacotes | sem `curl \| sh`; gerenciador com lockfile (`npm ci`, `pip install -r` com hashes/lock) | Instalação não reproduzível |
| Limpeza | cache do gerenciador removido; sem ferramentas de depuração em runtime | Imagem grande, superfície alta |
| `HEALTHCHECK` | presente (ou no Compose) | Sem como saber se está saudável |
| Porta | só a necessária (`EXPOSE`) | Portas extras |
| Sistema de arquivos | permitir somente leitura quando possível | Escrita livre |

```dockerfile
# Vulnerável
FROM node:latest
WORKDIR /app
COPY . .
ENV JWT_SECRET=troque-isto
RUN npm install
CMD ["node", "server.js"]

# Corrigido
FROM node:<versão-LTS-fixada>-slim AS build
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci
COPY src ./src
RUN npm run build

FROM node:<versão-LTS-fixada>-slim AS runtime
WORKDIR /app
ENV NODE_ENV=production
COPY --from=build /app/dist ./dist
COPY --from=build /app/package.json /app/package-lock.json ./
RUN npm ci --omit=dev && npm cache clean --force
USER node
HEALTHCHECK CMD node -e "fetch('http://localhost:8080/health').then(r=>process.exit(r.ok?0:1)).catch(()=>process.exit(1))"
CMD ["node", "dist/server.js"]
```

## 3. Docker Compose: o que revisar

| Item | Esperado | Falha típica |
|---|---|---|
| **Portas publicadas** | só o front/proxy; banco **não** publicado (`ports` ausente; use `expose`) ou ligado a `127.0.0.1` | `5432:5432` aberto a toda a rede |
| Rede | rede interna para banco/API; front e proxy numa rede separada | Tudo em uma rede plana |
| Credenciais | por variável de ambiente/`secrets`/arquivo **não commitado**; `.env.example` sem valor real | `POSTGRES_PASSWORD: postgres` no arquivo |
| Usuário do contêiner | `user:` não root quando a imagem permite | root |
| Privilégios | sem `privileged: true`, sem `cap_add` desnecessário; `cap_drop: [ALL]` quando possível; `security_opt: [no-new-privileges:true]` | `privileged: true` |
| Volumes | sem montar `/var/run/docker.sock`; sem montar a raiz do host; volumes nomeados para dados | docker.sock dentro do contêiner = controle do host |
| Sistema de arquivos | `read_only: true` com `tmpfs` onde precisa escrever | gravável |
| Limites | `mem_limit`/`cpus` (mitiga DoS por consumo) | ilimitado |
| Reinício | política adequada; healthchecks e `depends_on: condition: service_healthy` | falha silenciosa |
| Imagens | versão fixada | `latest` |

```yaml
# Vulnerável
services:
  db:
    image: postgres:latest
    ports: ["5432:5432"]
    environment: { POSTGRES_PASSWORD: postgres }
  api:
    build: .
    privileged: true
    volumes: ["/var/run/docker.sock:/var/run/docker.sock"]
    environment: { JWT_SECRET: troque-isto }

# Corrigido (trechos)
services:
  db:
    image: postgres:<versão-fixada>
    expose: ["5432"]                     # visível só na rede do Compose
    environment:
      POSTGRES_PASSWORD_FILE: /run/secrets/db_pass
    secrets: [db_pass]
    networks: [interna]
  api:
    build: .
    user: "10001:10001"
    read_only: true
    tmpfs: ["/tmp"]
    cap_drop: [ALL]
    security_opt: ["no-new-privileges:true"]
    environment:
      JWT_SECRET_FILE: /run/secrets/jwt_secret
    secrets: [jwt_secret]
    networks: [interna, publica]
    mem_limit: 512m
secrets:
  db_pass:   { file: ./segredos/db_pass.txt }     # arquivo fora do git (.gitignore)
  jwt_secret: { file: ./segredos/jwt_secret.txt }
networks: { interna: { internal: true }, publica: {} }
```
(O gerenciamento exato de segredos no Compose e a leitura de `*_FILE` pela aplicação dependem
da stack: conferir em `agentes/ambiente.md`. `internal: true` impede acesso à Internet — use em redes sem
necessidade de saída.)

**Provar** (comandos Docker são para CI/humano; no sandbox o agente não tem Docker e prova por leitura dos arquivos): `docker compose ps` e `docker compose config` mostram portas e variáveis resolvidas;
de **outra máquina/porta do host**: `nc -zv <host> 5432` (ou `docker run --rm --network host ...`)
para provar exposição **no ambiente local**; `docker compose exec api id` mostra o usuário.

## 4. Configuração da aplicação (produção x desenvolvimento)

| Item | Esperado |
|---|---|
| Modo de execução | produção sem debug, sem recarga automática, sem rotas de depuração |
| Segredos | em variáveis de ambiente/cofre; **nada** no repositório; rotação possível |
| Perfil | arquivo de config de produção separado do de dev; padrão **seguro** quando a variável falta (falhar ao iniciar, não usar valor padrão) |
| CORS/cookies/TLS | conforme `cabecalhos-cors.md` |
| Erros | handler global; sem stack trace ao cliente |
| Logs | sem segredos/dados pessoais; nível adequado |
| Banco | usuário de runtime **sem** DDL, sem superusuário; migração com outro usuário |
| Consoles e documentação | Swagger/Actuator/GraphQL playground desligados ou autenticados em produção |
| Dependências opcionais | recursos não usados desativados |
| Uploads | diretório fora da raiz web; sem execução; tamanho e tipo limitados |

**Padrão seguro ao faltar variável:**

```python
# Vulnerável: cai num segredo fixo se a variável não existe
JWT_SECRET = os.getenv("JWT_SECRET", "dev-secret")
# Corrigido: falha ao iniciar
JWT_SECRET = os.environ["JWT_SECRET"]          # KeyError -> app não sobe sem segredo
```

## 5. Segredos

- Nunca no código, no Dockerfile, no Compose, na imagem ou no histórico do git (Gitleaks:
  `varreduras-opensource.md`).
- `.env` real no `.gitignore`; `.env.example` com placeholders óbvios.
- Segredo vazado: **rotacionar** (remover do git não basta).
- Segredo de build via mecanismo de segredo, não `ARG`/`ENV` (ficam nas camadas da imagem:
  `docker history` mostra).
- Diferenciar segredo de **desenvolvimento descartável** (aceitável no Compose local, documentado,
  nunca reutilizado fora) de segredo de produção (inaceitável no repositório).

## 6. Cadeia de suprimentos da imagem

- Base oficial/verificada; fixar versão (ou digest); atualizar com frequência.
- `trivy image` na imagem final (pacotes do SO + dependências da aplicação).
- Lockfile e instalação reproduzível.
- Sem scripts de instalação remotos sem verificação de integridade.
- SBOM (lista de componentes): o Trivy gera (`trivy image --format cyclonedx` ou `spdx-json`;
  **conferir flags em `agentes/ambiente.md`**), útil como evidência; fora do mínimo exigido.

## 7. Checklist da revisão

- [ ] Imagens com versão fixada, base oficial, sem `latest`.
- [ ] Contêineres sem root; sem `privileged`; sem `docker.sock`.
- [ ] Banco e serviços internos sem porta publicada ao host.
- [ ] Nenhum segredo no código, Dockerfile, Compose, imagem ou histórico.
- [ ] `.dockerignore` e `.gitignore` cobrindo `.env`, chaves, `.git`.
- [ ] Multi-stage; imagem final sem ferramentas de build.
- [ ] Healthchecks; limites de memória/CPU.
- [ ] Config de produção sem debug/consoles; falha ao iniciar sem segredo.
- [ ] Usuário de runtime do banco sem DDL.
- [ ] `trivy config` e `trivy image` sem Crítico/Alto abertos (ou justificados).

## 8. Severidade típica (ACH)

| Achado | Severidade |
|---|---|
| Segredo real de produção no repositório/histórico | Crítico/Alto |
| Banco publicado ao host com senha padrão (Compose local) | Médio (Alto/Crítico se a config vai para produção) |
| `privileged`/`docker.sock` montado | Alto |
| Contêiner como root sem necessidade | Médio |
| Imagem com CVE Alto/Crítico com correção e alcançável | Alto |
| `latest`/sem lockfile | Médio/Baixo |
| Debug/Swagger ligados em config de produção | Médio/Alto |
| Falta `.dockerignore` copiando `.env` para a imagem | Alto (se contém segredo) |
