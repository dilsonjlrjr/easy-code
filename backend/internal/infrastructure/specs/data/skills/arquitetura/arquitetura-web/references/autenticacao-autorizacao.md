# Autenticação e autorização no desenho

Escopo: o que a **Arquitetura** decide e documenta. Ameaças, revisão de código e verificação
profunda são da skill `seguranca/revisao-seguranca` (e do modelo de ameaças `AMEACAS`).
Aqui você escolhe o mecanismo, define papéis e deixa a checagem de cada endpoint explícita
para o Dev implementar e o QA testar.

## Princípios

1. **Autenticação diz quem é; autorização diz o que pode.** Duas decisões, duas seções na ADR.
2. **Checar na API, sempre.** Front só esconde botão; a API é a barreira.
3. **Negar por padrão.** Endpoint sem regra de acesso explícita exige login (ou é marcado público de propósito).
4. **Mínimo de peças.** Para front único + API própria, não adote OAuth/OIDC com provedor
   externo sem US ou RNF que peça (SSO, login social).
5. **Não inventar criptografia.** Usar bibliotecas e padrões testados da linguagem.

## Sessão com cookie vs token

| Critério | Sessão (cookie `HttpOnly`) | Token (JWT curto + refresh) |
|---|---|---|
| Revogar acesso imediatamente | Fácil (apaga a sessão) | Difícil (espera expirar ou lista de bloqueio) |
| Estado no servidor | Sim (memória ou tabela) | Não por requisição; refresh precisa de estado |
| Exposição ao JavaScript | Cookie `HttpOnly` não é lido por JS | Se guardado em `localStorage`, é lido por XSS |
| CSRF | Precisa tratar (`SameSite`, token CSRF) | Menos exposto se enviado em cabeçalho `Authorization` |
| Vários clientes (mobile, terceiros) | Menos natural | Natural |
| Número de peças | Menos | Mais (assinatura, rotação, refresh) |

**Recomendação para front único e API própria:** sessão com cookie `HttpOnly`, `Secure` (em
HTTPS), `SameSite=Lax` ou `Strict`, expiração e renovação deslizante; sessão guardada em
tabela no PostgreSQL (evita Redis). Escolha token quando houver mais de um tipo de cliente ou
API pública; nesse caso: validade curta (minutos), refresh com rotação, nunca no `localStorage`
se puder evitar. Registre a escolha em ADR com opções reais.

Se front e API estiverem em origens diferentes, descreva CORS: lista fechada de origens
(sem `*` com credenciais), métodos e cabeçalhos mínimos, e `credentials` quando cookie.

## Senhas

- Hash adaptativo com sal individual: argon2id (preferido) ou bcrypt/scrypt, parâmetros
  conforme a recomendação atual do OWASP (**conferir**). Nunca SHA simples, nunca reversível.
- Política mínima: tamanho mínimo (ex.: 8–12) em vez de regras de composição rígidas; nenhuma
  senha em log, em erro ou em resposta.
- Mensagem de login genérica ("credenciais inválidas") para não revelar se o usuário existe.
- Limite de tentativas ou atraso progressivo no login (RNF, se houver).
- Troca/recuperação de senha só se houver US; senão fica fora do escopo.

## Papéis e permissões

Comece com **papéis fixos** (ex.: `USUARIO`, `ADMIN`) numa coluna de `usuarios` (ou tabela
`papeis` se um usuário puder ter vários). Permissões granulares (RBAC completo, ABAC) só com
requisito.

Matriz de acesso — entregue na ADR ou no OpenAPI (`x-papeis`, ou descrição):

| Endpoint | Público | Usuário | Admin | Regra por recurso |
|---|---|---|---|---|
| `POST /login` | sim | | | — |
| `GET /salas` | | sim | sim | — |
| `POST /salas` | | | sim | — |
| `POST /reservas` | | sim | sim | `usuarioId` vem da sessão, nunca do corpo |
| `DELETE /reservas/{id}` | | dono | sim | só o dono da reserva ou admin |

## Checagem por recurso (propriedade)

A falha mais comum e grave: autenticar e papel corretos, mas **sem verificar o dono**
(acesso indevido a objeto de outro usuário, IDOR).

Regras de desenho:
1. O identificador do usuário vem **da sessão/token**, nunca do corpo ou da URL.
2. Toda rota com `{id}` de recurso de dono específico verifica `recurso.usuarioId == usuarioAtual` (ou admin).
3. Consultas de lista filtram pelo dono quando o papel não é admin; o filtro está na query,
   não depois de carregar tudo.
4. Decida e documente o status para recurso alheio: **404** (não revela existência; mais
   seguro) ou **403** (mais explícito). Escolha uma regra única para todo o contrato.
5. Campos protegidos (`papel`, `usuarioId`, `criadoEm`) não aceitos no corpo de criação/edição.

## Onde a checagem fica no código

Resumo para `estrutura.md`:
- Autenticação: um middleware/filtro único valida sessão/token e injeta o "usuário atual".
- Papel: declarativo por rota (anotação, middleware) para ver a matriz no código.
- Propriedade: no serviço/caso de uso, próximo da regra, com teste que prova que usuário B
  não acessa recurso do usuário A.

## O que o QA precisa conseguir testar

Cada linha da matriz vira CT: sem login → 401; papel errado → 403; recurso alheio →
404/403 conforme a regra; campo protegido ignorado ou rejeitado. A Arquitetura entrega a
matriz; sem ela o QA não tem como derivar os casos.

## Usuários de teste

O ambiente de teste tem pelo menos um usuário por papel, criados por seed (ver skill
`ambiente-docker`). Senhas de teste só em `.env.example`/seed de desenvolvimento, jamais
reutilizadas; o seed não roda em produção.

## Dados de sessão e logout

- Logout invalida a sessão no servidor (sessão) ou o refresh (token).
- Expiração absoluta e por inatividade, com valores em RNF ou na ADR.
- Cookie com nome neutro; sem dados sensíveis dentro do cookie além do identificador.

## Fora do escopo desta skill (apontar para Segurança)

Modelo de ameaças, proteção contra força bruta avançada, cabeçalhos de segurança, CSP,
gestão de segredos, auditoria de dependências, revisão do código de autenticação:
`../../seguranca/revisao-seguranca/`. Ameaças altas do `AMEACAS` voltam como mitigação
nas ADR/T desta etapa.

## ADR modelo (sessão)

Entra nos campos `opcoes` (com `pros` e `contras`), `decisao` e `consequencias` da ADR (esquema `adr.schema.json`).

| Opção | Prós | Contras |
|---|---|---|
| A — Sessão em cookie + tabela `sessoes` | Revogação imediata; sem token acessível a JS; poucas peças | Estado no banco; CSRF a tratar; consulta por requisição |
| B — JWT curto + refresh | Sem consulta por requisição | Revogação difícil; mais peças; armazenamento do token |

Decisão típica: A. Consequências: `SameSite` + proteção CSRF em métodos que mudam estado;
limpeza periódica de sessões expiradas; T de autenticação antes de qualquer endpoint protegido.

## Checklist

- [ ] ADR de autenticação com opções reais e hash de senha definido.
- [ ] Papéis e matriz endpoint x papel x regra por recurso.
- [ ] Regra única 404/403 para recurso alheio.
- [ ] `usuarioId` nunca do corpo; campos protegidos listados.
- [ ] CORS e cookie (`HttpOnly`, `Secure`, `SameSite`) descritos se aplicável.
- [ ] Usuários de teste por papel previstos no seed.
- [ ] Ameaças da Segurança mapeadas para ADR/T.

## Erros comuns

| Erro | Correção |
|---|---|
| Papel checado só no front | Checar na API |
| `usuarioId` aceito no corpo | Derivar da sessão |
| JWT longo no `localStorage` | Sessão com cookie `HttpOnly`, ou token curto com refresh |
| SHA-256 para senha | argon2id/bcrypt |
| CORS `*` com credenciais | Lista fechada de origens |
| Mensagens de login distintas para "usuário" e "senha" | Mensagem genérica |
| OAuth/OIDC completo sem requisito | Login próprio simples; ADR com gatilho para SSO |
| Matriz de acesso ausente | QA não deriva CT; criar |
