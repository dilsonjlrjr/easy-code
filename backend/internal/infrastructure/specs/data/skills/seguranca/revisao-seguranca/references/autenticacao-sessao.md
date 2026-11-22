# Autenticação e sessão em profundidade

Cobre A07 (autenticação), parte de A04 (cripto) e A02 (cookies). Para cada tema: o que é
correto, como revisar o código, como provar uma falha.

## 1. Armazenamento de senha

**Regra:** guardar apenas **hash adaptativo e com sal**, nunca a senha, nunca hash rápido.

| Algoritmo | Situação |
|---|---|
| **Argon2id** | Recomendação atual (OWASP Password Storage Cheat Sheet) |
| **scrypt**, **bcrypt**, PBKDF2 | Aceitáveis com custo adequado (bcrypt limita senha a 72 bytes) |
| MD5, SHA-1, SHA-256/512 simples, hash sem sal | **Inválidos** para senha |
| Criptografia reversível da senha | Inválida |

Parâmetros de custo (memória/iterações/fator) mudam com o hardware: **conferir os valores
mínimos atuais no OWASP Password Storage Cheat Sheet** e aferir tempo por verificação no
ambiente (ordem de centenas de milissegundos). Registre os parâmetros usados no REL-SEG.

**Revisar:** função de hash usada; sal único por senha (bibliotecas modernas embutem no
formato); custo configurável; verificação em tempo constante (a biblioteca faz); migração de
hash antigo ao logar; senhas máximas aceitas (aceitar >= 64 caracteres, sem truncar em
silêncio).

**Provar falha:** inspecionar o campo no banco local. Hash de 32/40/64 hex = hash rápido
(SHA/MD5); texto legível = senha em claro. Evidência: trecho do código + exemplo mascarado do
formato armazenado.

**Política de senha:** mínimo de comprimento (a orientação atual privilegia comprimento e
checagem contra senhas vazadas em vez de regras de composição), sem expiração periódica
obrigatória sem motivo, permitir colar. Verificação contra lista de senhas comprometidas é
desejável; confirmar a política no RNF.

## 2. Mensagens e enumeração de usuários

| Ponto | Correto | Falha |
|---|---|---|
| Login | Mesmo texto, status e tempo para "usuário não existe" e "senha errada" | Mensagens/tempos diferentes |
| Recuperação de senha | "Se o e-mail existir, enviaremos instruções" | "E-mail não cadastrado" |
| Cadastro | Resposta neutra ou verificação por e-mail | "E-mail já usado" revelando (decisão de produto: documente) |

Tempo: verifique hash falso quando o usuário não existe, para igualar o custo.

## 3. Força bruta e credential stuffing

Controles esperados (valores vêm do RNF; se ausentes, ACH com `etapa_destino: requisitos`):

- limite de tentativas por **conta** (atraso progressivo ou bloqueio temporário) **e** por **IP**/
  cliente (limite de taxa no proxy/API);
- bloqueio que não permita **negar serviço à vítima** (bloqueio temporário curto, não
  permanente por tentativas alheias);
- resposta uniforme (item 2);
- MFA para papéis privilegiados, quando exigido;
- log e alerta de rajadas (A09).

**Provar:** script local com 30 tentativas incorretas em sequência para uma conta de teste;
registrar status de cada uma e do login correto logo depois. Evidência: tabela de status
(401 x30 e 200 no final = sem controle).

```bash
# Prova local (ambiente do Compose): 30 tentativas; mostrar a distribuição de status
for i in $(seq 1 30); do
  curl -s -o /dev/null -w "%{http_code}\n" -X POST "$API_URL/auth/login" \
    -H 'Content-Type: application/json' \
    -d '{"email":"funcionario@example.test","senha":"errada'$i'"}'
done | sort | uniq -c
```

## 4. Sessão no servidor x JWT

| Critério | Sessão no servidor (ID opaco + armazenamento) | JWT (token autocontido) |
|---|---|---|
| Revogação (logout, troca de senha, conta bloqueada) | Imediata (apagar a sessão) | Difícil: precisa de lista de revogação ou expiração curta |
| Escala sem estado compartilhado | Exige armazenamento compartilhado | Natural |
| Tamanho/vazamento de dados | ID curto, nada no cliente | Claims legíveis no cliente (não são secretas, só assinadas) |
| Erro comum | ID previsível, fixação | Não validar assinatura/algoritmo/expiração; guardar dado sensível no payload |
| Recomendação para app web tradicional/SPA com backend próprio | **Preferir** | Aceitável com cuidados abaixo |

Regras para **JWT** (se adotado; decisão do ADR):
- algoritmo **fixo na verificação** (`HS256` com segredo forte ou `RS256/ES256` com chaves);
  nunca aceitar o `alg` do cabeçalho do token; rejeitar `none`;
- validar **assinatura, `exp`, `iss`, `aud`**; expiração curta do token de acesso (minutos) +
  token de renovação com rotação e revogação;
- segredo/chave **fora do repositório** (variável de ambiente/cofre), >= 256 bits aleatórios;
- **não** colocar dado sensível no payload; não usar JWT como "sessão que nunca expira";
- armazenar no navegador em **cookie `HttpOnly`** (mitiga roubo por XSS) em vez de
  `localStorage`; com cookie, tratar CSRF (item 6).

**Provar falha (JWT):** alterar `alg` para `none` e reenviar; alterar `papel`/`sub` no payload
mantendo a assinatura antiga; usar token expirado; usar token de outro ambiente. Todas devem
retornar 401.

```python
# Vulnerável: confia no algoritmo do cabeçalho e não valida expiração
payload = jwt.decode(token, options={"verify_signature": False})
# Corrigido: algoritmo fixo, expiração/emitente/audiência exigidos
payload = jwt.decode(token, CHAVE, algorithms=["HS256"], audience="app", issuer="app-auth",
                     options={"require": ["exp", "iss", "aud"]})
```

## 5. Cookies de sessão

| Atributo | Valor esperado | Efeito |
|---|---|---|
| `HttpOnly` | presente | JavaScript não lê (mitiga roubo por XSS) |
| `Secure` | presente (em HTTPS) | Só trafega por TLS |
| `SameSite` | `Lax` (ou `Strict`) | Reduz CSRF; `None` exige `Secure` e justificativa |
| `Path`/`Domain` | o mais restrito possível | Evita vazar para subdomínios |
| `Max-Age`/`Expires` | conforme RNF; sessão com expiração absoluta e por inatividade | Limita janela de roubo |
| Prefixo `__Host-` | recomendado quando possível | Força `Secure`, `Path=/`, sem `Domain` |

**Ciclo de vida:** novo ID **ao autenticar** (evita fixação), invalidação no logout **no
servidor**, invalidação na troca de senha, ID de alta entropia (>= 128 bits de fonte
criptográfica), nunca na URL.

**Provar:** `curl -i` no login e ler `Set-Cookie`; reutilizar o cookie após logout (deve
falhar); tentar fixar um ID conhecido antes do login e conferir se muda depois.

## 6. CSRF (falsificação de requisição entre sites)

Aplica-se quando o navegador **envia credenciais automaticamente** (cookie). Não se aplica da
mesma forma a APIs com token em cabeçalho `Authorization` enviado por JavaScript.

Controles (combinar):
1. `SameSite=Lax`/`Strict` no cookie de sessão (primeira linha de defesa; não a única).
2. **Token anti-CSRF** sincronizado (por sessão) ou cookie duplo assinado, exigido em toda
   requisição que **altera estado** (POST/PUT/PATCH/DELETE); validado no servidor.
3. Mutações **nunca** em `GET`.
4. Conferir `Origin`/`Referer` em mutações como defesa adicional.
5. CORS restrito (não substitui CSRF).

**Provar:** página HTML local (outro origin) com um `<form method=post action=...>` que dispara
a ação com o cookie da vítima; sem token válido a API deve recusar (403).

```html
<!-- prova local (servir em outra porta) -->
<form action="http://localhost:8080/reservas/42/cancelar" method="POST" id="f"></form>
<script>document.getElementById('f').submit()</script>
```

## 7. Recuperação de senha e troca

- Token **aleatório de fonte criptográfica**, longo, **uso único**, expiração curta (ex.: 15–60
  min; valor no RNF), armazenado como **hash** no servidor.
- Link não vaza em `Referer`/logs; resposta neutra (item 2).
- Troca de senha **invalida sessões** existentes e notifica por e-mail.
- Troca de e-mail/senha exige reautenticação (senha atual).
- Perguntas secretas: evitar.

**Provar:** reutilizar o link; usar link expirado; solicitar dois links (o primeiro continua
válido?); alterar o parâmetro de usuário no link.

## 8. Contas privilegiadas e padrão

- Nenhuma conta/senha padrão no repositório/Compose (Gitleaks + revisão do seed).
- Seed de admin só em ambiente de desenvolvimento, com senha de variável de ambiente.
- Papel/permissão nunca vindo do cliente.
- MFA (TOTP/WebAuthn) para admin quando o RNF pedir.

## 9. Checklist da revisão

- [ ] Hash adaptativo (Argon2id/bcrypt/scrypt) com custo documentado; sem hash rápido.
- [ ] Login: mesma resposta para usuário inexistente e senha errada.
- [ ] Limite de tentativas por conta e por IP; prova com 30 tentativas.
- [ ] Sessão/JWT: expira, invalida no logout, ID/segredo fortes, algoritmo fixo.
- [ ] Cookie `HttpOnly`, `Secure`, `SameSite`; novo ID ao autenticar.
- [ ] CSRF tratado para mutações com cookie.
- [ ] Recuperação de senha: token aleatório, uso único, expira, resposta neutra.
- [ ] Sem credenciais padrão; segredos fora do repositório.
- [ ] Nada sensível no payload do JWT nem nos logs.

## 10. Severidade típica (ACH)

| Achado | Severidade |
|---|---|
| Senha em claro/hash rápido sem sal | Alto (Crítico se a base já é acessível) |
| JWT aceita `alg:none` ou assinatura não verificada | Crítico |
| Sem limite de tentativas de login | Alto (Médio se há MFA e senha forte exigida) |
| Cookie de sessão sem `HttpOnly`/`Secure` | Médio |
| CSRF em ação sensível com cookie `SameSite=None` | Alto |
| Enumeração de usuário no login | Baixo/Médio |
| Token de recuperação reutilizável | Alto |
| Credencial padrão em Compose de desenvolvimento | Médio (Alto/Crítico se o mesmo vai para produção) |
