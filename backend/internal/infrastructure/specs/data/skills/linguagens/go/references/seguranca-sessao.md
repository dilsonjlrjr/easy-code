# Segurança (Go): sessão em cookie com tabela no PostgreSQL

Decisão e conceitos: `arquitetura-web` (autenticação e autorização) e `revisao-seguranca`
(autenticação/sessão, controle de acesso, cabeçalhos/CORS). Aqui: a implementação padrão em
Go com biblioteca padrão + `golang.org/x/crypto` — login próprio (usuário/senha), sessão em
cookie `HttpOnly` cujo estado fica numa tabela do PostgreSQL. Não é preciso biblioteca de
sessão: o mecanismo é pequeno e fica visível.

## Modelo

```
POST /api/auth/login  -> confere senha -> cria sessão (token aleatório) -> Set-Cookie: sessao=<token>; HttpOnly; Secure; SameSite=Lax
GET  /api/...         -> cookie -> hash do token -> SELECT na tabela -> usuário no context
POST /api/auth/logout -> DELETE da sessão -> cookie expirado
```

- O cookie guarda um **token opaco** (32 bytes aleatórios); o banco guarda só o **SHA-256** do
  token. Vazar o banco não vaza sessões utilizáveis.
- Revogação imediata: apagar a linha. Sem estado no cliente além do cookie.

## Tabela

```sql
CREATE TABLE sessoes (
  token_hash  bytea       PRIMARY KEY,                  -- sha256(token)
  usuario_id  bigint      NOT NULL REFERENCES usuarios(id) ON DELETE CASCADE,
  criada_em   timestamptz NOT NULL DEFAULT now(),
  expira_em   timestamptz NOT NULL,                     -- expiração por inatividade
  expira_max  timestamptz NOT NULL                      -- teto absoluto
);
CREATE INDEX sessoes_usuario_id_idx ON sessoes (usuario_id);   -- toda FK com índice
CREATE INDEX sessoes_expira_em_idx  ON sessoes (expira_em);    -- limpeza
```

Limpeza: `DELETE FROM sessoes WHERE expira_em < now()` num worker com dono (iniciado na
`main`, parado pelo `ctx`; ver `context-concorrencia.md`).

## Hash de senha

```go
import "golang.org/x/crypto/bcrypt"

func HashSenha(senha string) (string, error) {
	if len(senha) < 10 || len(senha) > 72 { // bcrypt só considera 72 bytes
		return "", fmt.Errorf("%w: senha deve ter entre 10 e 72 bytes", ErrEntradaInvalida)
	}
	h, err := bcrypt.GenerateFromPassword([]byte(senha), bcrypt.DefaultCost)
	return string(h), err
}

func SenhaConfere(hash, senha string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(senha)) == nil
}
```

- `bcrypt` (`golang.org/x/crypto/bcrypt`) é suficiente e simples. `argon2id`
  (`golang.org/x/crypto/argon2`, função `IDKey(senha, sal, tempo, memoria, threads, tamanho)`)
  é o critério de troca para exigência de memória-dura; exige guardar sal e parâmetros no
  hash e escolher parâmetros com cuidado (conferir a recomendação atual da OWASP).
- Hash **lento e com sal** (o bcrypt já inclui o sal); nunca SHA-256/MD5 para senha.
- Guarde só o hash (`senha_hash`); nunca logue senha.

## Token de sessão

```go
func novoToken() (token string, hash []byte, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil { // crypto/rand
		return "", nil, fmt.Errorf("gerar token: %w", err)
	}
	token = base64.RawURLEncoding.EncodeToString(b)
	return token, hashDoToken(token), nil
}

func hashDoToken(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}
```

- **`crypto/rand`**, nunca `math/rand`. 256 bits de entropia: não é adivinhável.
- O token só existe em claro no cookie; no banco, o hash (por isso o `SELECT` é por igualdade
  do hash: sem comparação de strings sujeita a temporização).

## Login

```go
type Servico struct {
	pool    *pgxpool.Pool
	agora   func() time.Time
	ttlIdle time.Duration // ex.: 30 min
	ttlMax  time.Duration // ex.: 12 h
}

// hashFalso é um hash bcrypt válido de uma senha qualquer, para igualar o tempo
// quando o usuário não existe (evita enumeração por tempo de resposta).
var hashFalso, _ = bcrypt.GenerateFromPassword([]byte("senha-inexistente"), bcrypt.DefaultCost)

func (s *Servico) Login(ctx context.Context, email, senha string) (token string, expira time.Time, err error) {
	var id int64
	var hash string
	var ativo bool
	e := s.pool.QueryRow(ctx, "SELECT id, senha_hash, ativo FROM usuarios WHERE email = $1",
		strings.ToLower(email)).Scan(&id, &hash, &ativo)
	if errors.Is(e, pgx.ErrNoRows) {
		hash = string(hashFalso) // mesmo custo de tempo
		ativo = false
	} else if e != nil {
		return "", time.Time{}, fmt.Errorf("buscar usuário: %w", e)
	}
	if !SenhaConfere(hash, senha) || !ativo {
		return "", time.Time{}, ErrCredenciais // mesma resposta para qualquer causa
	}

	token, th, err := novoToken()
	if err != nil {
		return "", time.Time{}, err
	}
	agora := s.agora()
	expira = agora.Add(s.ttlIdle)
	_, err = s.pool.Exec(ctx,
		"INSERT INTO sessoes (token_hash, usuario_id, expira_em, expira_max) VALUES ($1, $2, $3, $4)",
		th, id, expira, agora.Add(s.ttlMax))
	if err != nil {
		return "", time.Time{}, fmt.Errorf("criar sessão: %w", err)
	}
	return token, expira, nil // sessão NOVA a cada login: sem fixação de sessão
}
```

Handler:

```go
func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string `json:"email"`
		Senha string `json:"senha"`
	}
	if err := httpx.Ler(w, r, &req); err != nil {
		httpx.ResponderErro(w, r, err, h.log)
		return
	}
	token, expira, err := h.svc.Login(r.Context(), req.Email, req.Senha)
	if errors.Is(err, ErrCredenciais) {
		httpx.ResponderErro(w, r, &httpx.ErroHTTP{Status: 401, Tipo: "credenciais-invalidas",
			Titulo: "Não autenticado", Detalhe: "E-mail ou senha incorretos."}, h.log)
		return
	}
	if err != nil {
		httpx.ResponderErro(w, r, err, h.log)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "sessao",
		Value:    token,
		Path:     "/",
		Expires:  expira,
		HttpOnly: true,                 // JS não lê
		Secure:   h.cookieSeguro,       // true em produção (HTTPS); false só em dev local
		SameSite: http.SameSiteLaxMode, // não enviado em POST cross-site
	})
	w.WriteHeader(http.StatusNoContent)
}
```

- Mesmo status/mensagem para "usuário inexistente", "senha errada" e "conta inativa".
- Força bruta: contador por e-mail/IP no PostgreSQL com bloqueio temporário (429); se
  ausente, registrar como achado.
- Cookie com `Path=/`, `HttpOnly`, `Secure` (produção), `SameSite=Lax`. Sem `Domain` (host
  exato). Prefixo `__Host-sessao` (exige `Secure`, `Path=/`, sem `Domain`) é endurecimento
  extra opcional.
- Logout: `DELETE FROM sessoes WHERE token_hash = $1` e cookie com `MaxAge: -1`; método `POST`.

## Middleware de autenticação

```go
func (s *Servico) Autenticar(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c, err := r.Cookie("sessao")
			if err != nil {
				naoAutenticado(w, r, log)
				return
			}
			u, err := s.usuarioDaSessao(r.Context(), hashDoToken(c.Value))
			if errors.Is(err, ErrNaoEncontrado) {
				naoAutenticado(w, r, log)
				return
			}
			if err != nil {
				httpx.ResponderErro(w, r, err, log)
				return
			}
			next.ServeHTTP(w, r.WithContext(ComUsuario(r.Context(), u)))
		})
	}
}
```

`usuarioDaSessao` faz, numa consulta, a checagem e a renovação deslizante:

```sql
UPDATE sessoes SET expira_em = LEAST(now() + $2::interval, expira_max)
WHERE token_hash = $1 AND expira_em > now() AND expira_max > now()
RETURNING usuario_id;
```

(depois busca id/papel do usuário, ou faça `JOIN` num `WITH`). Sem linha -> `ErrNaoEncontrado`
-> 401. Falha de **infraestrutura** (banco fora) é 5xx, nunca 401.

- Aplique `Autenticar` só às rotas protegidas (`/api/...` exceto login e health).

## CSRF

Cookie é enviado automaticamente pelo navegador: defesa em camadas.

1. **`SameSite=Lax`** no cookie (acima): bloqueia o envio em POST cross-site.
2. **Verificação de origem** em métodos que mudam estado: rejeitar quando `Origin` (ou
   `Sec-Fetch-Site`) indica outro site.
3. CORS restritivo (só a origem do front; ver `cabecalhos-cors` em `revisao-seguranca`).

```go
// ProtegerCSRF recusa requisições que mudam estado vindas de outra origem.
func ProtegerCSRF(origemPermitida string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
				next.ServeHTTP(w, r)
				return
			}
			if s := r.Header.Get("Sec-Fetch-Site"); s != "" && s != "same-origin" && s != "none" {
				proibido(w, r)
				return
			}
			if o := r.Header.Get("Origin"); o != "" && o != origemPermitida {
				proibido(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
```

- A partir do Go 1.25 existe `http.CrossOriginProtection` na biblioteca padrão com esse
  propósito (`http.NewCrossOriginProtection()`; conferir a API e o mínimo do `go.mod`); se o
  `go.mod` já exige essa versão, use-a em vez do middleware manual.
- Reforço: **token CSRF** (valor aleatório por sessão em `X-CSRF-Token`, comparado com
  `subtle.ConstantTimeCompare`) se houver clientes sem `Origin`/`Sec-Fetch-Site`; para
  navegador moderno com `SameSite=Lax`, a verificação de origem costuma bastar (ADR).
- `GET` **nunca** muda estado.

## Autorização: papel e dono do recurso

Papel por rota não basta: A não pode cancelar a reserva de B (IDOR). Checar **no serviço**,
perto do dado:

```go
func (s *Service) Cancelar(ctx context.Context, id int64, u auth.Usuario) error {
	r, err := s.reservas.Buscar(ctx, id)
	if err != nil {
		return fmt.Errorf("cancelar reserva %d: %w", id, err) // ErrNaoEncontrado propaga
	}
	if r.UsuarioID != u.ID && u.Papel != auth.PapelAdmin {
		return fmt.Errorf("reserva %d: %w", id, ErrNaoEncontrado) // 404: não revela que existe
	}
	return s.reservas.Remover(ctx, id)
}
```

- Decida e documente (ADR/openapi) 403 ou 404 para recurso de outro usuário; 404 evita
  enumerar IDs.
- O dono vem **sempre** da sessão (`auth.Usuario(ctx)`), nunca do corpo ou da URL (mass
  assignment).
- Listagens filtram por dono no SQL; rotas administrativas conferem o papel **e** o serviço
  também confere.

## Cabeçalhos e outras proteções

- `X-Content-Type-Options: nosniff`, `Referrer-Policy`, CSP e HSTS no proxy reverso ou em
  middleware (`cabecalhos-cors` em `revisao-seguranca`).
- CORS só com origens diferentes; nunca `Access-Control-Allow-Origin: *` com credenciais.
- Segredos só por variável de ambiente; comparar segredos com `subtle.ConstantTimeCompare`;
  `govulncheck ./...` no CI.

## Erros comuns

| Erro | Correção |
|---|---|
| Token de sessão em claro no banco | Guardar SHA-256 do token |
| `math/rand` para token | `crypto/rand`, 32 bytes |
| Reaproveitar a sessão anterior no login | Criar sessão **nova** a cada login |
| Mensagens/tempos diferentes para usuário inexistente e senha errada | Mesma resposta; bcrypt falso para igualar tempo |
| Cookie sem `HttpOnly`/`Secure`/`SameSite` | Os três (Secure em produção) |
| Sem proteção CSRF por usar cookie | `SameSite=Lax` + verificação de origem/token |
| Só `ExigirPapel` na rota | Checar dono do recurso no serviço |
| Banco fora vira 401 | Erro de infraestrutura é 5xx |
| Senha com SHA-256 | `bcrypt`/`argon2id` |
| `GET` que muda estado | `POST`/`DELETE` |
