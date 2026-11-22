# Cabeçalhos HTTP de segurança, cookies e CORS

Relacionado a A02 (configuração insegura), A05 (XSS) e A07 (cookies). Verifica-se com
`curl -i` no ambiente local; **a aplicação decide onde configurar** (proxy reverso, servidor
web ou aplicação) — avalie a resposta **final** que o navegador recebe.

## 1. Como inspecionar

```bash
curl -sI "$BASE_URL/"                          # cabeçalhos da página
curl -sI "$API_URL/salas" -H "Authorization: Bearer $TOKEN"
curl -si -X OPTIONS "$API_URL/reservas" \
  -H "Origin: https://origem-estranha.example" \
  -H "Access-Control-Request-Method: POST"     # pré-voo CORS
```

Conferir **página (HTML)**, **API (JSON)** e **respostas de erro** (404/500), pois os
cabeçalhos às vezes faltam justamente nelas.

## 2. Cabeçalhos esperados

| Cabeçalho | Valor de referência | Protege contra | Observação |
|---|---|---|---|
| `Strict-Transport-Security` | `max-age=31536000; includeSubDomains` | Downgrade para HTTP | Só em HTTPS; cuidado com `preload` (decisão consciente); em ambiente local HTTP não se aplica |
| `Content-Security-Policy` | política restritiva (ver seção 3) | XSS, injeção de conteúdo | O mais difícil e o mais valioso |
| `X-Content-Type-Options` | `nosniff` | MIME sniffing | Sempre |
| `X-Frame-Options` ou CSP `frame-ancestors` | `DENY` / `frame-ancestors 'none'` (ou origem própria) | Clickjacking | Preferir `frame-ancestors` no CSP; manter ambos é aceitável |
| `Referrer-Policy` | `strict-origin-when-cross-origin` (ou `no-referrer`) | Vazamento de URL em Referer | Importante se há token em URL (não deveria haver) |
| `Permissions-Policy` | desativar o que não usa (`camera=(), microphone=(), geolocation=()`) | Uso indevido de APIs do navegador | Complementar |
| `Cross-Origin-Opener-Policy` / `Cross-Origin-Resource-Policy` | `same-origin` conforme o caso | Vazamento entre origens | Avaliar impacto em integrações |
| `Cache-Control` em dados privados | `no-store` | Dados em cache compartilhado/histórico | Respostas autenticadas com dado pessoal |
| Remoção de `Server`/`X-Powered-By` detalhados | ausente ou genérico | Fingerprinting | Baixa severidade |

Conferir a lista e os valores atuais em MDN (Security headers) e no OWASP Secure Headers
Project antes de fixar no REL-SEG.

## 3. Content-Security-Policy (CSP)

CSP diz ao navegador **de onde** pode carregar scripts, estilos, imagens, conexões. Objetivo:
mesmo que uma injeção de HTML ocorra, o script injetado não executa.

Ponto de partida restritivo para SPA com API própria:

```
Content-Security-Policy:
  default-src 'self';
  script-src 'self';
  style-src 'self';
  img-src 'self' data:;
  connect-src 'self' https://api.exemplo.invalid;
  frame-ancestors 'none';
  base-uri 'self';
  form-action 'self';
  object-src 'none'
```

Regras de revisão:
- `'unsafe-inline'` e `'unsafe-eval'` em `script-src` anulam grande parte da proteção; se o
  framework exige estilo inline, preferir **nonce/hash** para scripts e tolerar inline só em
  `style-src` com justificativa.
- `*` ou `https:` em `script-src`: fraco.
- Sem CSP: achado **Médio** (defesa em profundidade) — **Alto** só se há XSS confirmado.
- Teste: `Content-Security-Policy-Report-Only` primeiro para medir impacto sem quebrar.
- Verifique no navegador (console) violações com a página carregada.

## 4. Cookies (resumo; detalhe em `autenticacao-sessao.md`)

`Set-Cookie: sid=...; HttpOnly; Secure; SameSite=Lax; Path=/; Max-Age=...`
Falta de `HttpOnly`/`Secure` no cookie de sessão: Médio. Verifique também cookies auxiliares
(preferências) que carregam dado sensível.

## 5. CORS

CORS **relaxa** a política de mesma origem do navegador. Configuração errada = outro site lê
respostas da sua API com as credenciais do usuário.

**Como funciona (mínimo):** o navegador manda `Origin`; o servidor responde
`Access-Control-Allow-Origin` (e, para requisições "não simples", responde ao **pré-voo**
`OPTIONS` com métodos/cabeçalhos permitidos). Credenciais (cookies) só são enviadas se o
servidor responder `Access-Control-Allow-Credentials: true` **e** uma origem **específica**.

| Configuração | Avaliação |
|---|---|
| Lista explícita de origens da própria aplicação | Correta |
| `Access-Control-Allow-Origin: *` em API **pública sem credenciais** | Aceitável |
| `*` com cookies/autenticação | O navegador bloqueia; mas veja os próximos itens |
| **Refletir o cabeçalho `Origin`** recebido e `Allow-Credentials: true` | **Crítico/Alto**: qualquer site lê dados do usuário |
| Aceitar `Origin: null` com credenciais | Alto (iframes sandbox, arquivos locais) |
| Validação de origem por `endsWith`/`contains` (`exemplo.com` casa `malexemplo.com`) | Alto: bypass |
| `Access-Control-Allow-Methods`/`Headers` abertos além do necessário | Baixo/Médio |
| Falta `Vary: Origin` quando a origem permitida varia | Baixo (cache) |

**Provar:** enviar requisição com `Origin: https://origem-estranha.example` e verificar se a
resposta traz esse valor em `Access-Control-Allow-Origin` junto de
`Access-Control-Allow-Credentials: true`.

```bash
curl -si "$API_URL/reservas" -H "Origin: https://origem-estranha.example" \
  -H "Authorization: Bearer $TOKEN" | grep -i '^access-control'
# Falha: access-control-allow-origin: https://origem-estranha.example + allow-credentials: true
```

```js
// Vulnerável (Express): reflete qualquer origem com credenciais
app.use((req, res, next) => {
  res.set('Access-Control-Allow-Origin', req.headers.origin);
  res.set('Access-Control-Allow-Credentials', 'true');
  next();
});
// Corrigido: lista fechada, comparação exata
const PERMITIDAS = new Set(['https://app.exemplo.invalid']);
app.use((req, res, next) => {
  const o = req.headers.origin;
  if (o && PERMITIDAS.has(o)) {
    res.set('Access-Control-Allow-Origin', o);
    res.set('Access-Control-Allow-Credentials', 'true');
    res.set('Vary', 'Origin');
  }
  next();
});
```

CORS **não é** controle de acesso: não impede `curl`/servidor de chamar a API. Autorização
continua no servidor.

## 6. Outros itens de configuração HTTP

- **TLS:** HTTP redireciona para HTTPS; protocolos antigos desativados (TLS 1.0/1.1); certificado
  válido. Em ambiente local da aplicação pequena, registrar que o TLS é terminado no proxy/ambiente de
  produção (ou fora do escopo local) e o risco residual.
- **Métodos HTTP:** `TRACE`, `PUT`/`DELETE` não usados desabilitados no proxy.
- **Erros:** página de erro padrão do servidor sem versão/stack.
- **Arquivos expostos:** `/.git`, `/.env`, `/actuator`, `/swagger-ui`, `/debug`, backups
  (`*.bak`, `*.sql`) -> 404/401.
- **Redirecionamento aberto:** parâmetros `next`/`redirect` validados contra lista de destinos.
- **Cache:** respostas autenticadas com `Cache-Control: no-store`.

## 7. Checklist

- [ ] CSP presente e sem `unsafe-inline/eval` em scripts (ou exceção justificada).
- [ ] `X-Content-Type-Options: nosniff`; `frame-ancestors`/`X-Frame-Options`.
- [ ] `Strict-Transport-Security` (quando HTTPS em produção).
- [ ] `Referrer-Policy` e `Permissions-Policy` definidos.
- [ ] Cookies com `HttpOnly`, `Secure`, `SameSite`.
- [ ] CORS por lista exata; sem reflexão de `Origin`; sem `null` com credenciais.
- [ ] Erros sem stack trace; sem endpoints de debug/admin expostos.
- [ ] Respostas autenticadas sem cache compartilhado.

## 8. Severidade típica (ACH)

| Achado | Severidade |
|---|---|
| CORS reflete origem com credenciais (dados autenticados legíveis por terceiro) | Alto/Crítico |
| Ausência de CSP, sem XSS provado | Médio |
| CSP com `unsafe-inline` em `script-src` e XSS provado | Alto |
| Falta `X-Content-Type-Options` / `Referrer-Policy` | Baixo |
| Falta HSTS em produção | Médio |
| `Server`/`X-Powered-By` com versão | Baixo |
| `/actuator`/`/swagger-ui` expostos sem autenticação em produção | Médio/Alto (conforme o que revelam/permitem) |
