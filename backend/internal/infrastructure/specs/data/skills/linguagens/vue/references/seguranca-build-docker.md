# Segurança no front, build e Docker

A auditoria completa é da skill `revisao-seguranca` (`owasp-top10-2025`, `autenticacao-sessao`,
`cabecalhos-cors`, `conteiner-configuracao`). Aqui: o que o Dev do front deve fazer para a
Segurança não achar problema.

## Princípio

O front roda no navegador do usuário (e do atacante). **Tudo que está no bundle é público**
e toda validação do front é contornável. Segurança de verdade está no backend; o front não
cria brechas e reduz o dano de um erro.

## XSS (Cross-Site Scripting)

O Vue **escapa** interpolações `{{ }}` e bindings `:atributo`. O risco aparece quando você
quebra essa proteção:

| Risco | Evite | Faça |
|---|---|---|
| `v-html` com dado de usuário/API | **Não usar** (regra de lint `vue/no-v-html`) | Interpolação `{{ }}`; se precisa de HTML rico, sanitizar com biblioteca dedicada (DOMPurify, open source; conferir) **e** registrar a exceção com ADR |
| `:href`/`:src` com URL de usuário | `javascript:` e `data:` executam | Validar esquema permitido (`http:`, `https:`, `mailto:`), nunca repassar cru |
| `innerHTML`/`insertAdjacentHTML` via DOM direto | idem `v-html` | Evitar; usar template |
| Template compilado a partir de string do usuário | Execução de código | Nunca |
| Atributos de evento dinâmicos (`:onclick`) | Execução | Nunca vindo de dado |
| Markdown renderizado | HTML embutido | Renderizador com sanitização |
| `eval`, `new Function`, `setTimeout(string)` | Execução | Nunca |

Redirecionamento: `window.location = dadoDoUsuario` é redirecionamento aberto/`javascript:`.
Valide que é caminho interno (começa com `/` e não `//`).

## Tokens, sessão e armazenamento

| Opção | Avaliação |
|---|---|
| **Cookie de sessão `HttpOnly`, `Secure`, `SameSite` definido pelo backend** | Preferida: JavaScript não lê (XSS não rouba o token); atenção a CSRF (SameSite e/ou token anti-CSRF) |
| Token em memória (variável) | Aceitável com ADR; some ao recarregar; exige refresh por cookie |
| `localStorage`/`sessionStorage` | **Evitar:** qualquer XSS lê. Só com ADR justificando e mitigações |
| Token em URL/query | **Nunca** (vai para histórico, logs, `Referer`) |

Regras:

- O front **não** vê o token quando é cookie `HttpOnly`; guarda só o perfil do usuário.
- Logout: pedir ao backend para invalidar e limpar o estado local.
- Não coloque dado sensível (CPF, e-mail, perfil de acesso como "prova") em `localStorage`.
- **Papel/permissão no front é só UX**: o backend nega. Não decida acesso por flag do cliente.
- Não registrar token, senha ou dado pessoal em `console.log` nem em ferramentas de erro.

## CSRF e CORS

- **CSRF:** cookie de sessão + `SameSite=Lax/Strict` reduz; operações que alteram estado
  podem exigir cabeçalho anti-CSRF que o cliente HTTP envia (ver `cliente-http-openapi.md`).
- **CORS** é configurado no **backend**. Preferir mesma origem via proxy reverso (nginx
  encaminha `/api` ao backend): sem CORS, sem `credentials` cross-site. Lista de origens
  explícita (nunca `*` com credenciais).

## CSP e cabeçalhos (via nginx)

Definidos no servidor que entrega o front:

| Cabeçalho | Valor de partida (ajustar) |
|---|---|
| `Content-Security-Policy` | `default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'` |
| `X-Content-Type-Options` | `nosniff` |
| `Referrer-Policy` | `strict-origin-when-cross-origin` (ou mais restrito) |
| `Permissions-Policy` | Desligar o que não usa (câmera, microfone, geolocalização) |
| `Strict-Transport-Security` | Em produção com HTTPS (no proxy/TLS) |

- O build do Vite gera scripts como arquivos (compatíveis com `script-src 'self'`); evite
  `<script>` inline e `eval`. `style-src` pode precisar de ajuste conforme a biblioteca de UI
  (sem biblioteca em aplicação pequena, `'self'` costuma bastar; conferir no navegador).
- Teste a CSP no navegador (console mostra violações) e ajuste; **não** relaxe para
  `unsafe-inline`/`unsafe-eval` sem ADR.
- Mais detalhes e tabela completa em `revisao-seguranca/references/cabecalhos-cors.md`.

## Dependências e bundle

- **Menos dependências, menos superfície.** Cada pacote novo precisa de justificativa
  (tamanho, licença, manutenção).
- Versões travadas por `package-lock.json`; instalação reproduzível no CI com `npm ci`.
- Auditoria: `npm audit` (ou ferramenta open source equivalente definida pela skill de
  segurança); corrigir o que for exploitável no cliente.
- **Nada secreto no bundle**: chave de API de terceiro com poder real fica no backend; o
  front chama o backend. Chave **pública** (ex.: chave de mapas restrita por domínio) pode
  ir no front, restrita.
- Source maps de produção: não publicar (ou restringir acesso), para não expor código/comentários.
- `target="_blank"` sempre com `rel="noopener noreferrer"`.
- Uploads: o front valida tipo/tamanho como conforto; o backend revalida.

## Build

```bash
npm ci
npm run lint && npx vue-tsc --noEmit && npm run test
npm run build          # saída em dist/
```

- `vue-tsc` antes do build (o Vite não checa tipos).
- Build **reproduzível**: `npm ci`, versão do Node fixada (`.nvmrc`/imagem), lockfile no repo.
- `dist/` é o único artefato que vai para a imagem final.

## Variáveis: tempo de build x execução

| | Build (`VITE_*`) | Execução (config em runtime) |
|---|---|---|
| Quando vale | Embutido no bundle ao rodar `vite build` | Lido quando a página carrega |
| Mudar por ambiente | **Exige rebuild** da imagem | **Mesma imagem** em todos os ambientes |
| Segredo | **Nunca** (público) | **Nunca** (continua público) |
| Uso | Valores estáveis (URL relativa `/api`, flags de build) | URL de API absoluta que varia, nome do ambiente |

Recomendação para aplicação pequena: usar **URL relativa `/api`** (proxy do nginx) para a API, o que
dispensa configuração por ambiente. Se precisar de config por ambiente com imagem única:

```html
<!-- index.html: carrega config antes do app -->
<script src="/config.js"></script>
```

```js
// public/config.js (gerado no start do container a partir de variáveis de ambiente)
window.__CONFIG__ = { apiUrl: "/api", ambiente: "dev" };
```

```ts
// src/config.ts
declare global { interface Window { __CONFIG__?: { apiUrl: string } } }
export const config = { apiUrl: window.__CONFIG__?.apiUrl ?? import.meta.env.VITE_API_URL ?? '/api' }
```

O `config.js` é gerado no entrypoint (ex.: `envsubst` sobre um template; conferir) e **não**
pode conter segredo. Com CSP `script-src 'self'`, ele é um arquivo servido do mesmo
domínio, então é compatível.

## Docker: multi-stage e nginx não root

```dockerfile
# --- build ---
FROM node:<lts-estável>-alpine AS build            # tag fixa, conferir
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
RUN npm run build

# --- runtime ---
FROM nginxinc/nginx-unprivileged:<estável>-alpine   # imagem oficial que roda sem root, porta 8080 (conferir)
COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /app/dist /usr/share/nginx/html
EXPOSE 8080
HEALTHCHECK CMD wget -qO- http://127.0.0.1:8080/ >/dev/null || exit 1
```

```nginx
# nginx.conf
server {
  listen 8080;
  root /usr/share/nginx/html;
  index index.html;

  add_header X-Content-Type-Options nosniff always;
  add_header Referrer-Policy strict-origin-when-cross-origin always;
  add_header Content-Security-Policy "default-src 'self'; frame-ancestors 'none'; base-uri 'self'" always;

  location /api/ { proxy_pass http://backend:8080/; }        # nome do serviço no Compose

  location /assets/ { expires 1y; add_header Cache-Control "public, immutable"; }  # arquivos com hash
  location / { try_files $uri /index.html; add_header Cache-Control "no-cache"; } # fallback da SPA
}
```

Regras:

- **Multi-stage:** Node só no estágio de build; a imagem final tem só nginx + `dist/`.
- **Não root:** imagem `nginx-unprivileged` (porta 8080) ou usuário não privilegiado com
  permissões ajustadas; sem `--privileged`.
- **Fallback da SPA** (`try_files ... /index.html`) para rotas do `createWebHistory`.
- **Cache:** assets com hash (`/assets/`) por longo prazo; `index.html` sem cache.
- Cabeçalhos de segurança no nginx (ver acima); atenção: `add_header` em `location` interno
  anula os do nível acima (repita ou use `include`).
- `.dockerignore` com `node_modules`, `dist`, `.env*`, `.git`.
- Nenhum segredo em `ARG`/`ENV`/camadas; a imagem do front não precisa deles.
- No Compose: `healthcheck`, `depends_on` com condição, rede interna com o backend (skill
  `ambiente-docker`). A API não precisa expor porta para o host se o nginx faz proxy.

## Checklist de segurança do front (antes de concluir a T)

- [ ] Sem `v-html`/`innerHTML` com dado externo (lint `vue/no-v-html` limpo).
- [ ] URLs vindas de dado validadas (esquema permitido); `rel="noopener noreferrer"` em `_blank`.
- [ ] Nenhum token/dado sensível em `localStorage`/`sessionStorage`/URL (ou ADR).
- [ ] Nenhum segredo em `VITE_*`, `config.js`, imagem ou repositório.
- [ ] Autorização real no backend; guarda é só UX.
- [ ] CSP e cabeçalhos no nginx; sem `unsafe-inline`/`unsafe-eval`.
- [ ] Imagem final com nginx não root, sem Node, sem `.env`.
- [ ] `npm audit` (ou equivalente) sem vulnerabilidade explorável no cliente.
- [ ] Source maps não expostos em produção.

## Erros comuns

| Erro | Efeito | Correção |
|---|---|---|
| `v-html` para "formatar texto" | XSS | Interpolação; sanitizar com ADR se inevitável |
| JWT em `localStorage` | Roubo por XSS | Cookie `HttpOnly` do backend |
| Chave secreta em `VITE_*` | Vazamento público | Segredo no backend |
| Imagem nginx rodando como root | Superfície maior | `nginx-unprivileged` |
| Faltou fallback da SPA | 404 ao recarregar rota | `try_files $uri /index.html` |
| Rebuild para cada ambiente | Artefato diferente por ambiente | Config em runtime / URL relativa |
| `add_header` duplicado em `location` | Cabeçalhos somem | Repetir no `location` ou `include` |
| CSP relaxada com `unsafe-inline` por preguiça | Sem proteção XSS | Corrigir o inline; ADR se inevitável |
| CORS `*` com credenciais | Navegador recusa/inseguro | Mesma origem via proxy ou origem explícita |
| Ocultar botão como controle de acesso | Rota/API abertas | Backend autoriza |
