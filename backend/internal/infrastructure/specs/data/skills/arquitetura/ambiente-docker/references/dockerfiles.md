# Dockerfiles: multi-stage, mínimo, sem root

> **Onde isto roda.** `docker-compose.yml` e Dockerfiles do projeto gerado continuam sendo artefatos entregues, para CI e para humanos. Os comandos `docker compose ...` e `docker run ...` deste arquivo são **para CI/humano**. No sandbox da plataforma o agente não tem Docker nem rede: testes de integração/E2E rodam contra os **serviços de apoio declarados no perfil** (ex.: PostgreSQL efêmero) que o runner sobe, e só com os comandos da allowlist da sua categoria (`testes`, `migracoes`, `banco_descartavel`).

Cada serviço que tem código próprio ganha um Dockerfile com **estágios**: um para construir
(toolchain pesada) e um final enxuto. Versões das imagens base: fixar o **major estável
atual** e conferir; os números abaixo são ilustrativos.

## Princípios

1. **Multi-stage:** compilar/instalar em um estágio, copiar só o resultado para o final.
2. **Camadas na ordem da mudança:** o que muda pouco (dependências) antes do que muda muito (código).
3. **Mínimo:** a imagem final tem o que o processo precisa para rodar, nada de compilador, `git`, testes.
4. **Não root:** `USER` não privilegiado; arquivos com dono correto.
5. **Reprodutível:** dependências travadas (lockfile), imagem base fixada por major.
6. **Sem segredo:** nada de senha/chave em `ENV`, `ARG` ou `COPY`.
7. **Um processo, sinais corretos:** `exec` form no `CMD` para o processo receber `SIGTERM` e encerrar limpo.

## Cache de camadas: a regra principal

```dockerfile
# ruim: qualquer mudança no código invalida a instalação de dependências
COPY . .
RUN instalar-dependencias

# bom: dependências em camada própria
COPY manifesto-e-lockfile ./
RUN instalar-dependencias
COPY . .
```

Combine com `.dockerignore` e, quando suportado, cache de montagem do BuildKit
(`RUN --mount=type=cache,...`) para caches de gerenciador de pacotes.

## `.dockerignore` (por contexto de build)

```
.git
.env
.env.*
!.env.example
node_modules
dist
build
target
__pycache__
*.pyc
.venv
coverage
*.log
Dockerfile
docker-compose*.yml
.idea
.vscode
```

Evita contexto enorme, vazamento de `.env` e invalidação de cache.

## Tipo 1: linguagem compilada (Go, Rust, Java)

### Go

```dockerfile
FROM golang:1.23 AS build            # versão: conferir o estável atual
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api

FROM gcr.io/distroless/static-debian12:nonroot AS runtime
COPY --from=build /out/api /api
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/api"]
```

Binário estático em imagem mínima (distroless/`scratch`). Sem shell: healthcheck do Compose
com `wget` não funciona; use um subcomando do binário (`/api saude`) ou imagem `alpine` mínima.

### Rust

```dockerfile
FROM rust:1 AS build                 # conferir versão estável atual
WORKDIR /src
COPY Cargo.toml Cargo.lock ./
RUN mkdir src && echo "fn main(){}" > src/main.rs && cargo build --release && rm -rf src
COPY src ./src
RUN touch src/main.rs && cargo build --release

FROM debian:stable-slim AS runtime
RUN useradd --system --uid 10001 app
COPY --from=build /src/target/release/api /usr/local/bin/api
USER app
EXPOSE 8080
ENTRYPOINT ["api"]
```

O truque do `main.rs` falso pré-compila as dependências para cache. Alternativas: ferramentas
open source de cache de dependências Rust (`cargo-chef`). Binário com `musl` permite imagem
mínima; **conferir** compatibilidade das dependências.

### Java

```dockerfile
FROM eclipse-temurin:21-jdk AS build   # conferir LTS atual
WORKDIR /src
COPY pom.xml ./                        # ou build.gradle + wrapper
RUN ./mvnw -B -q dependency:go-offline || true
COPY src ./src
RUN ./mvnw -B -q package -DskipTests

FROM eclipse-temurin:21-jre AS runtime
RUN useradd --system --uid 10001 app
WORKDIR /app
COPY --from=build /src/target/*.jar app.jar
USER app
EXPOSE 8080
ENTRYPOINT ["java", "-XX:MaxRAMPercentage=75", "-jar", "app.jar"]
```

Ajuste para o wrapper de build do projeto. JRE no runtime (não JDK). `MaxRAMPercentage` faz a
JVM respeitar o limite de memória do contêiner.

## Tipo 2: linguagem interpretada (Python)

```dockerfile
FROM python:3.12-slim AS build        # conferir versão estável atual
WORKDIR /app
ENV PIP_NO_CACHE_DIR=1 PYTHONDONTWRITEBYTECODE=1
RUN python -m venv /opt/venv
ENV PATH="/opt/venv/bin:$PATH"
COPY requirements.txt ./              # com versões travadas (lock)
RUN pip install -r requirements.txt

FROM python:3.12-slim AS runtime
ENV PYTHONUNBUFFERED=1 PATH="/opt/venv/bin:$PATH"
RUN useradd --system --uid 10001 app
WORKDIR /app
COPY --from=build /opt/venv /opt/venv
COPY --chown=app:app . .
USER app
EXPOSE 8080
CMD ["uvicorn", "app.main:app", "--host", "0.0.0.0", "--port", "8080"]
```

Troque pelo gerenciador do projeto (uv, poetry, pip-tools) mantendo a **ordem de camadas**
(manifesto + lock -> instalar -> código). Pacotes com extensão nativa podem exigir
compilador no estágio `build` (não no runtime) ou bibliotecas do sistema.
`--host 0.0.0.0` é obrigatório: `127.0.0.1` dentro do contêiner não recebe tráfego de fora.

## Tipo 3: front estático (Vue)

```dockerfile
FROM node:22-alpine AS deps           # conferir LTS atual
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci

FROM deps AS dev                       # estágio opcional de desenvolvimento
COPY . .
EXPOSE 5173
CMD ["npm", "run", "dev", "--", "--host", "0.0.0.0"]

FROM deps AS build
ARG VITE_API_URL
ENV VITE_API_URL=$VITE_API_URL
COPY . .
RUN npm run build

FROM nginxinc/nginx-unprivileged:stable-alpine AS runtime
COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /app/dist /usr/share/nginx/html
EXPOSE 8080
```

`nginx.conf` mínimo com fallback de SPA:

```nginx
server {
  listen 8080;
  root /usr/share/nginx/html;
  location / { try_files $uri /index.html; }
}
```

- `nginx-unprivileged` já roda sem root e escuta na 8080.
- Variáveis `VITE_*` entram **no build** (viram texto no JavaScript): não colocar segredo.
  Se a URL da API precisa mudar sem rebuild, servir um `config.js` em runtime ou fazer proxy `/api`.
- `npm ci` usa o lockfile exatamente.

## Usuário não root: checklist por tipo

| Imagem | Como |
|---|---|
| distroless `:nonroot` | `USER nonroot:nonroot` |
| debian/ubuntu slim | `useradd --system --uid 10001 app` + `USER app` |
| alpine | `adduser -S -u 10001 app` + `USER app` |
| nginx | usar `nginx-unprivileged` (porta > 1024) |
| postgres (oficial) | a imagem já cuida do usuário |

Diretórios graváveis (uploads, caches) com `chown` para o usuário; preferir `tmpfs`.

## Tamanho e segurança da imagem

- Base mínima compatível: `alpine`, `slim`, distroless. Cuidado com `musl` (alpine) em
  dependências nativas (Python, Node com binários); se houver problema, `slim`.
- Sem ferramentas de build no final; sem `curl`/shell extra se não usados no healthcheck.
- `HEALTHCHECK` no Dockerfile é opcional quando já está no Compose.
- Varredura de vulnerabilidades da imagem com ferramenta open source (por exemplo Trivy) em
  CI: fazer parte da revisão de segurança (`../../seguranca/revisao-seguranca/`).
- Rótulos úteis: `LABEL org.opencontainers.image.source=...`.
- Reconstruir com frequência para receber correções da base.

## Build de teste local

```bash
docker build -t reservas-api:dev --target runtime ./backend
docker run --rm -p 8080:8080 --env-file .env reservas-api:dev
docker run --rm reservas-api:dev id        # confirmar usuário não root (se houver shell)
docker image ls reservas-api               # conferir tamanho
```

## Checklist

- [ ] Multi-stage com runtime mínimo; versão base fixada (major estável, conferida).
- [ ] Manifesto + lockfile copiados e instalados **antes** do código.
- [ ] `.dockerignore` por contexto, excluindo `.git`, `.env`, dependências locais.
- [ ] `USER` não root; processo em `exec` form.
- [ ] Sem segredo em `ENV`/`ARG`/imagem; `VITE_*` sem segredo.
- [ ] Servidor escutando em `0.0.0.0` dentro do contêiner.
- [ ] Healthcheck viável com as ferramentas da imagem final.
- [ ] Imagem construída e testada; tamanho razoável.

## Erros comuns

| Erro | Correção |
|---|---|
| `COPY . .` antes da instalação das dependências | Manifesto primeiro |
| Imagem final com compilador | Multi-stage |
| Rodar como root | `USER` |
| `CMD npm start` em forma shell (sinais perdidos) | `exec` form (`["node", ...]`) |
| Escutar em `127.0.0.1` | `0.0.0.0` |
| Segredo em `ARG` | Variável em runtime via Compose |
| `FROM x:latest` | Major fixado |
| `.dockerignore` ausente | Criar |
| Healthcheck com `curl` em imagem sem `curl` | Ferramenta presente ou subcomando do binário |
| Dependência nativa quebra em alpine | `slim` |
