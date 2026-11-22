# Revisão manual por categoria — OWASP Top 10:2025

**Conferir ordem, nomes e mapeamentos em owasp.org/Top10/2025 antes da primeira revisão.** A
lista abaixo segue a que o REL-SEG usa. Para cada categoria: o que procurar no código, como
testar (no ambiente local) e exemplo vulnerável x corrigido. "Verificado, sem achado" é
resultado válido e deve constar no REL-SEG.

Método geral: (1) localizar os **pontos de entrada** (rotas, handlers, jobs, uploads);
(2) seguir o dado até os **sorvedouros** (banco, shell, template, URL, arquivo, log);
(3) conferir **controles** no caminho (autenticação, autorização, validação, escape);
(4) provar com requisição ou teste.

Exemplos em pseudocódigo/linguagens comuns; os padrões valem para qualquer stack.

---

## A01 — Controle de acesso quebrado (inclui SSRF)

**O que procurar**
- Endpoint que usa id da URL/corpo sem comparar com o dono (IDOR/BOLA).
- Autorização só no controlador de algumas rotas; rota nova sem proteção; verbos diferentes
  (GET protegido, PUT não).
- Funções administrativas acessíveis por papel comum; papel/dono vindo do corpo (mass assignment).
- Listagens, exportações, anexos e contagens sem escopo.
- **SSRF:** o servidor busca uma URL fornecida pelo usuário (webhook, importar por link,
  pré-visualização, proxy de imagem) sem restringir destino.
- Ações sensíveis só escondidas na interface.

**Como testar:** dois usuários do mesmo papel + um admin; repetir toda operação trocando o
id; sem token; método alternativo. SSRF: apontar a URL para `http://127.0.0.1:<porta>/`,
`http://169.254.169.254/` (endereço de metadados de nuvem) e host interno do Compose
(`http://db:5432`), só no ambiente local. Detalhes: `controle-acesso.md`.

```java
// Vulnerável: confere login, não o dono
@DeleteMapping("/reservas/{id}")
void cancelar(@PathVariable long id) { reservaRepo.deleteById(id); }

// Corrigido: autorização por recurso na camada de serviço
void cancelar(long id, Usuario atual) {
  var r = reservaRepo.findById(id).orElseThrow(NaoEncontrado::new);
  if (!(r.donoId() == atual.id() || atual.ehAdmin())) throw new NaoEncontrado(); // 404 esconde existência
  reservaRepo.delete(r);
}
```

```python
# SSRF vulnerável
def importar(url): return requests.get(url, timeout=5).text

# Corrigido (mínimo): só https, resolver o host, bloquear IP privado/loopback/link-local,
# sem redirecionamento automático, lista de domínios permitidos quando possível
def importar(url):
    p = urlparse(url)
    if p.scheme != "https" or p.hostname not in DOMINIOS_PERMITIDOS: raise Recusado()
    ip = ipaddress.ip_address(socket.gethostbyname(p.hostname))
    if ip.is_private or ip.is_loopback or ip.is_link_local: raise Recusado()
    return requests.get(url, timeout=5, allow_redirects=False).text
```
(Resolver e depois conectar tem janela de troca de DNS; o controle forte é lista de domínios
permitidos e rede sem rota a destinos internos.)

---

## A02 — Configuração insegura

**O que procurar:** modo debug/dev em produção; credenciais padrão; CORS `*` com credenciais;
cabeçalhos de segurança ausentes; stack trace em erro; listagem de diretório; serviços
desnecessários expostos (banco com porta publicada); console administrativo aberto
(Actuator, Swagger, phpMyAdmin); permissões de arquivo amplas; contêiner como root; XML com
entidades externas habilitadas.

**Como testar:** `curl -i` nas rotas e ler cabeçalhos; forçar erro (JSON inválido) e ler o
corpo; `docker compose config` e `docker compose ps` (CI/humano) (portas publicadas); Trivy `misconfig`;
checar `.env`/arquivos de configuração commitados. Detalhes: `cabecalhos-cors.md`,
`conteiner-configuracao.md`.

```yaml
# Vulnerável (Compose): banco exposto ao host e credenciais padrão
db:
  image: postgres:16
  ports: ["5432:5432"]
  environment: { POSTGRES_PASSWORD: postgres }
# Corrigido: sem porta publicada, segredo por variável/arquivo não commitado
db:
  image: postgres:16
  environment: { POSTGRES_PASSWORD_FILE: /run/secrets/db_pass }
  secrets: [db_pass]
```

---

## A03 — Falhas na cadeia de suprimentos de software

**O que procurar:** dependências com vulnerabilidade conhecida; sem lockfile ou versões
flutuantes (`latest`, `^` sem lockfile); pacotes abandonados ou com nome parecido
(typosquatting); scripts de instalação suspeitos; imagem base não fixada e desatualizada;
`curl | sh` em Dockerfile; pipeline que baixa artefato sem verificar integridade; dependência
de repositório não oficial.

**Como testar:** Trivy (`fs` com `vuln`), conferir lockfile commitado, `npm ci`/instalação
reproduzível, revisar `Dockerfile` e pipeline, inspecionar dependências diretas. Triagem:
a função vulnerável é alcançável? Há versão corrigida? (`varreduras-opensource.md`).

```dockerfile
# Vulnerável
FROM node:latest
RUN curl -sSL https://exemplo.invalid/install.sh | sh
# Corrigido: base com versão fixada (e, se possível, digest), instalação do gerenciador de pacotes com lockfile
FROM node:<versão-LTS-fixada>
COPY package.json package-lock.json ./
RUN npm ci --omit=dev
```

---

## A04 — Falhas criptográficas

**O que procurar:** senha em claro ou com hash rápido (MD5/SHA-1/SHA-256 simples); segredo
fixo no código; HTTP sem TLS; dado pessoal sensível sem criptografia em repouso quando o RNF
exige; algoritmos fracos (DES, ECB, RC4); IV/nonce fixo; geração de token com `Math.random`/
`rand()`; validação de certificado desligada; chaves no repositório; JWT com segredo fraco.

**Como testar:** ler código de hash/token; inspecionar tabela de usuários (campo de senha parece
hash adaptativo?); `curl -I http://...` redireciona para HTTPS?; Gitleaks; revisar geração de
tokens (usar fonte criptográfica). Detalhes: `autenticacao-sessao.md`.

```js
// Vulnerável: hash rápido, sem sal; token previsível
const hash = crypto.createHash('sha256').update(senha).digest('hex');
const token = Math.random().toString(36).slice(2);
// Corrigido: hash adaptativo com sal embutido; token de fonte criptográfica
const hash = await argon2.hash(senha);                      // ou bcrypt/scrypt com custo adequado
const token = crypto.randomBytes(32).toString('base64url');
```

---

## A05 — Injeção

**O que procurar:** concatenação/interpolação de entrada em SQL, comandos de SO, consultas
NoSQL, LDAP, expressões, templates, XPath; `eval`/`exec`; renderização de HTML com entrada sem
escape (XSS — injeção no navegador); desserialização de dado não confiável; nomes de coluna/
ordenação vindos do cliente; path traversal em nomes de arquivo.

**Como testar:** entrada `'` e `' OR '1'='1` em filtros e login; `<script>`/`"><img src=x onerror=alert(1)>`
em campos exibidos; `; id` e `$(id)` onde há chamada ao SO; `../../` em nomes de arquivo; Opengrep
com regras de injeção. Provar com a menor requisição.

```java
// Vulnerável
String sql = "SELECT * FROM salas WHERE nome = '" + nome + "' ORDER BY " + ordem;
// Corrigido: parâmetro + lista de colunas permitidas
var ordemOk = Map.of("nome","nome","capacidade","capacidade").getOrDefault(ordem, "nome");
var ps = con.prepareStatement("SELECT * FROM salas WHERE nome = ? ORDER BY " + ordemOk);
ps.setString(1, nome);
```

```js
// XSS vulnerável (renderiza HTML de entrada)
elemento.innerHTML = reserva.titulo;
// Corrigido: texto, não HTML (frameworks escapam por padrão; cuidado com dangerouslySetInnerHTML/v-html/[innerHTML])
elemento.textContent = reserva.titulo;
```

```python
# Comando: vulnerável x corrigido
os.system(f"convert {arquivo} out.png")               # vulnerável
subprocess.run(["convert", arquivo, "out.png"], check=True)  # sem shell; validar `arquivo`
```

---

## A06 — Design inseguro

**O que procurar (nível de desenho, não só de código):** fluxo sem limite de abuso (tentativas
de login, criação de recursos, envio de e-mail); regra de negócio burlável (reservar infinito,
cupom reutilizável, sequência de passos pulável); falta de separação de privilégios; confiança
no cliente; ausência de trilha de auditoria; recuperação de senha fraca; dado coletado além do
necessário; ameaças do `ameacas.md` sem mitigação implementada.

**Como testar:** comparar `ameacas.md` com o código (cada mitigação existe?); testar abuso de
negócio (reservar 1000 vezes, repetir operação, pular etapa por requisição direta); testar
limites de taxa; revisar o fluxo de recuperação de conta.

```text
Vulnerável (desenho): "reserva sem limite por usuário nem antecedência máxima"
  -> um usuário ocupa todas as salas de todos os dias (negação de serviço de negócio)
Corrigido (desenho + código): limite de reservas ativas por usuário (RNF/regra), antecedência
  máxima, limite de taxa no endpoint; teste de abuso em QA.
```

---

## A07 — Falhas de autenticação

**O que procurar:** hash de senha fraco; sem limite de tentativas; mensagens que revelam se o
usuário existe; sessão que não expira nem invalida no logout; ID de sessão em URL; fixação de
sessão; JWT sem verificação/expiração, `alg: none`; cookie sem `HttpOnly`/`Secure`/`SameSite`;
política de senha ausente (sem verificação de senhas vazadas); MFA inexistente em contas
privilegiadas se exigido; recuperação de senha com token previsível ou sem expiração; credenciais
padrão. Detalhes completos: `autenticacao-sessao.md`.

**Como testar:** 20 tentativas de login errado (bloqueia? atrasa?); mesmo texto/tempo para usuário
inexistente; reutilizar token após logout; token expirado; alterar o payload do JWT; inspecionar
`Set-Cookie`; fluxo de recuperação com token antigo/reutilizado.

```js
// Vulnerável: resposta diferente revela usuário; compara senha em texto
if (!usuario) return res.status(404).send('Usuário não existe');
if (usuario.senha !== senha) return res.status(401).send('Senha errada');
// Corrigido: mesma resposta, comparação do hash, esforço constante
const ok = usuario ? await argon2.verify(usuario.hash, senha) : await argon2.verify(HASH_FALSO, senha);
if (!ok) return res.status(401).json({ erro: 'E-mail ou senha inválidos' });
```

---

## A08 — Falhas de integridade de software e dados

**O que procurar:** desserialização insegura (Java `ObjectInputStream`, Python `pickle`, YAML
com `load` não seguro) de dado do cliente; atualização/plugin sem verificação de assinatura;
pipeline de CI que executa código de PR sem isolamento; dependências de CDN sem
**Subresource Integrity** (`integrity=`); cookies/dados de estado assinados sem verificação
(valor do cliente confiável: preço, papel, desconto no campo oculto); artefatos de build sem
integridade; migrações aplicadas sem revisão.

**Como testar:** alterar campo oculto/preço/papel no corpo; reutilizar cookie adulterado; revisar
uso de desserializadores; conferir `<script src>` externo com `integrity`/`crossorigin`; revisar
pipeline.

```python
# Vulnerável: pickle de dado do cliente
dados = pickle.loads(base64.b64decode(request.cookies["estado"]))
# Corrigido: JSON + assinatura (HMAC) ou estado no servidor
dados = json.loads(serializador_assinado.loads(request.cookies["estado"]))
```

```html
<!-- Vulnerável -->  <script src="https://cdn.exemplo.invalid/lib.js"></script>
<!-- Corrigido -->   <script src="https://cdn.exemplo.invalid/lib.js" integrity="sha384-..." crossorigin="anonymous"></script>
```

---

## A09 — Falhas de log e alerta

**O que procurar:** eventos de segurança sem registro (login, falha de login, mudança de papel,
ação de admin, acesso negado, exclusão); logs sem quem/quando/o quê/de onde; dado sensível nos
logs (senha, token, número de documento, corpo inteiro de requisição); logs que o usuário pode
forjar (injeção de quebra de linha); sem correlação (id da requisição); ninguém é alertado de
rajadas de falha; logs apagáveis pela aplicação comprometida.

**Como testar:** fazer login válido, inválido, acesso negado, ação de admin e **ler o log**;
procurar a senha digitada no log (`docker compose logs api | grep`, CI/humano; no sandbox, o log da execução); inserir `\n` em um campo e ver
se forja linha.

```js
// Vulnerável
logger.info('login', { corpo: req.body });             // loga a senha
// Corrigido
logger.info('login_falhou', { usuarioId, ip: req.ip, requestId });  // sem segredo; estruturado
```

---

## A10 — Tratamento incorreto de condições excepcionais

**O que procurar:** `catch` vazio ou que segue como se nada; falha **aberta** (erro na checagem
de permissão resulta em liberar); exceção que vaza stack trace/SQL; recursos não liberados em
erro; falta de tratamento de nulo/estado inesperado que derruba o serviço; timeout ausente em
chamadas externas; erro parcial que deixa estado inconsistente (transação não revertida);
mensagens de erro diferentes que servem de oráculo (existe/não existe); loop de retry infinito.

**Como testar:** provocar erros (JSON inválido, campo nulo, id inexistente, banco indisponível
com `docker compose stop db` (CI/humano), serviço externo lento); verificar: status adequado, mensagem
genérica, nada gravado pela metade, serviço volta quando a dependência volta, **falha fecha**.

```java
// Vulnerável: falha aberta
boolean podeVer;
try { podeVer = servicoPermissao.checar(usuario, recurso); }
catch (Exception e) { podeVer = true; }              // erro libera!
// Corrigido: falha fechada, registrada
try { podeVer = servicoPermissao.checar(usuario, recurso); }
catch (Exception e) { log.warn("falha_checagem", e); podeVer = false; }
```

---

## Mapeamento rápido: onde cada skill/ferramenta ajuda

| Categoria | Opengrep | Trivy | Gitleaks | Revisão manual / teste |
|---|---|---|---|---|
| A01 | parcial (padrões de rota sem checagem) | — | — | **principal** (2 usuários) |
| A02 | parcial | **misconfig** | — | cabeçalhos, erros, Compose |
| A03 | — | **vuln** | — | lockfile, pipeline |
| A04 | sim (hash fraco, aleatoriedade) | — | **segredos** | revisão de cripto |
| A05 | **sim** | — | — | payloads de prova |
| A06 | — | — | — | **principal** (modelo de ameaças x código) |
| A07 | parcial | — | — | **principal** (testes de login/sessão) |
| A08 | parcial (desserialização) | — | — | revisão de pipeline/dados |
| A09 | parcial | — | — | ler logs |
| A10 | parcial | — | — | provocar erros |

## Como registrar no REL-SEG

Um item de `owasp` por categoria (`A01` a `A10`, as dez; esquema `rel-seg.schema.json`): `verificado: true`,
`resultado` em uma frase (o que foi feito e concluído) e `achados` com os ids de ACH (`[]` se nenhum). Exemplo:

```yaml
owasp:
  - {categoria: A01, verificado: true, resultado: "testado com 2 funcionários e admin em 9 endpoints; 1 falha", achados: [ACH-04]}
  - {categoria: A09, verificado: true, resultado: "login, falha, admin e acesso negado registrados; sem segredo no log", achados: []}
```
