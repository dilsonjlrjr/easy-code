# Segurança (Python): sessão em cookie com tabela no PostgreSQL

Decisão e conceitos: `arquitetura-web` (autenticação e autorização) e `revisao-seguranca`
(autenticação/sessão, controle de acesso, cabeçalhos/CORS). Aqui: a implementação padrão em
Python com FastAPI, `argon2-cffi`, `hashlib`/`secrets` da stdlib e SQLAlchemy — login próprio
(usuário/senha), sessão em cookie `HttpOnly` cujo estado fica numa tabela do PostgreSQL. O
mecanismo é pequeno e fica visível; não é preciso biblioteca de sessão. Conferir assinaturas
de `argon2-cffi`/`pwdlib` e do FastAPI na versão fixada.

## Modelo

```
POST /api/auth/login  -> confere senha -> cria sessão (token aleatório) -> Set-Cookie: sessao=<token>; HttpOnly; Secure; SameSite=Lax
GET  /api/...         -> cookie -> hash do token -> UPDATE/SELECT na tabela -> UsuarioAtual
POST /api/auth/logout -> DELETE da sessão -> cookie expirado
```

- O cookie guarda um **token opaco** (32 bytes aleatórios); o banco guarda só o **SHA-256**.
  Vazar o banco não vaza sessões utilizáveis.
- Revogação imediata: apagar a linha. Sem estado no cliente além do cookie.
- JWT em `localStorage` **não** é o padrão (ver `arquitetura-web`): não se revoga e o JS lê.

## Tabela (e modelo)

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

```python
class Sessao(Base):
    __tablename__ = "sessoes"
    token_hash: Mapped[bytes] = mapped_column(LargeBinary, primary_key=True)
    usuario_id: Mapped[int] = mapped_column(ForeignKey("usuarios.id", ondelete="CASCADE"))
    criada_em: Mapped[datetime] = mapped_column(DateTime(timezone=True), server_default=func.now())
    expira_em: Mapped[datetime] = mapped_column(DateTime(timezone=True))
    expira_max: Mapped[datetime] = mapped_column(DateTime(timezone=True))
```

Limpeza: `DELETE FROM sessoes WHERE expira_em < now()` por comando agendado (serviço do
Compose/cron; ver `sync-async.md`), não por thread dentro do servidor.

## Hash de senha (argon2-cffi)

```python
# src/reservas/auth/senha.py
from argon2 import PasswordHasher
from argon2.exceptions import InvalidHashError, VerifyMismatchError

_hasher = PasswordHasher()          # Argon2id com parâmetros padrão da biblioteca

# Hash válido de uma senha qualquer, para igualar o tempo quando o usuário não existe.
HASH_FALSO = _hasher.hash("senha-inexistente")


def hash_senha(senha: str) -> str:
    if not 10 <= len(senha) <= 128:
        raise EntradaInvalida("A senha deve ter entre 10 e 128 caracteres.")
    return _hasher.hash(senha)


def senha_confere(hash_: str, senha: str) -> bool:
    try:
        return _hasher.verify(hash_, senha)
    except (VerifyMismatchError, InvalidHashError):
        return False


def precisa_rehash(hash_: str) -> bool:
    return _hasher.check_needs_rehash(hash_)     # parâmetros mudaram: re-hashear no login
```

- `argon2-cffi` (`PasswordHasher`) usa **Argon2id** e guarda sal e parâmetros no hash (formato
  PHC); conferir os parâmetros padrão contra a recomendação atual da OWASP e ajustar com
  `PasswordHasher(time_cost=..., memory_cost=..., parallelism=...)`.
- Alternativas: `pwdlib` (`PasswordHash.recommended()`, usado na documentação do FastAPI) e
  `bcrypt` (limite de 72 bytes). Não use SHA-256/MD5 para senha.
- O hash é **CPU pesada**: em rota `def` já roda em thread; em `async def`, use
  `run_in_threadpool` (`sync-async.md`).
- Valide o **tamanho máximo** da senha antes de hashear (evita DoS por senha gigante).
- Guarde só `senha_hash`; nunca logue senha; o schema de login não é ecoado em erro
  (`input` do Pydantic pode trazer a senha: o handler 400 não devolve `input`, ver
  `api-fastapi-erros.md`).
- `HASH_FALSO` é calculado **uma vez** no import (custo no start, não por request).

## Token de sessão

```python
import hashlib
import secrets

def novo_token() -> tuple[str, bytes]:
    token = secrets.token_urlsafe(32)                 # 256 bits de CSPRNG
    return token, hash_do_token(token)

def hash_do_token(token: str) -> bytes:
    return hashlib.sha256(token.encode()).digest()
```

- **`secrets`**, nunca `random`. 256 bits: não adivinhável.
- O token só existe em claro no cookie; no banco, o hash (a busca é por igualdade do hash,
  sem comparação de strings sujeita a temporização).

## Login

```python
# src/reservas/auth/service.py
def login(session: Session, cfg: SessaoCfg, email: str, senha: str) -> tuple[str, datetime]:
    usuario = session.scalars(select(Usuario).where(Usuario.email == email.strip().lower())).first()

    # mesmo custo de tempo exista ou não o usuário
    hash_ = usuario.senha_hash if usuario else HASH_FALSO
    confere = senha_confere(hash_, senha)
    if usuario is None or not usuario.ativo or not confere:
        raise NaoAutenticado()                         # mesma resposta para qualquer causa

    if precisa_rehash(usuario.senha_hash):
        usuario.senha_hash = hash_senha(senha)

    token, th = novo_token()                           # sessão NOVA a cada login: sem fixação
    agora = datetime.now(UTC)
    expira = agora + cfg.ttl_idle
    session.add(Sessao(token_hash=th, usuario_id=usuario.id, expira_em=expira, expira_max=agora + cfg.ttl_max))
    session.commit()
    return token, expira
```

```python
# src/reservas/auth/router.py
@router.post("/login", status_code=status.HTTP_204_NO_CONTENT)
def login_rota(corpo: LoginRequest, session: SessionDep, settings: SettingsDep, response: Response) -> None:
    token, _ = service.login(session, settings.sessao, corpo.email, corpo.senha)
    response.set_cookie(
        "sessao", token,
        max_age=int(settings.sessao.ttl_idle.total_seconds()),
        path="/",
        httponly=True,                       # JS não lê
        secure=settings.cookie_seguro,       # True em produção (HTTPS); False só em dev local
        samesite="lax",                      # não enviado em POST cross-site
    )

@router.post("/logout", status_code=status.HTTP_204_NO_CONTENT)
def logout(request: Request, session: SessionDep, response: Response) -> None:
    if token := request.cookies.get("sessao"):
        session.execute(delete(Sessao).where(Sessao.token_hash == hash_do_token(token)))
        session.commit()
    response.delete_cookie("sessao", path="/")
```

- `NaoAutenticado` é `ErroDominio` com status 401, mensagem "E-mail ou senha incorretos."
  Mesmo status/mensagem para usuário inexistente, senha errada e conta inativa.
- Em FastAPI, o `Response` injetado na rota aceita `set_cookie` mesmo com `204`.
- Força bruta: contador por e-mail/IP no PostgreSQL com bloqueio temporário (429); se
  ausente, registrar como achado.
- Cookie com `Path=/`, `HttpOnly`, `Secure` (produção), `SameSite=Lax`, sem `domain`. O
  prefixo `__Host-sessao` é endurecimento extra opcional.
- E-mail normalizado (`strip().lower()`) na gravação **e** na busca (índice único sobre
  `lower(email)` no banco, ver `modelagem-postgresql`).

## Resolver a sessão (`usuario_atual`)

```python
def usuario_da_sessao(session: Session, cfg: SessaoCfg, token: str | None) -> UsuarioAtual:
    if not token:
        raise NaoAutenticado()
    linha = session.execute(
        text("""
            UPDATE sessoes s
               SET expira_em = LEAST(now() + make_interval(secs => :ttl), s.expira_max)
              FROM usuarios u
             WHERE s.token_hash = :h
               AND s.expira_em  > now()
               AND s.expira_max > now()
               AND u.id = s.usuario_id AND u.ativo
         RETURNING u.id, u.papel
        """),
        {"h": hash_do_token(token), "ttl": cfg.ttl_idle.total_seconds()},
    ).first()
    session.commit()                                   # confirma a renovação da expiração
    if linha is None:
        raise NaoAutenticado()
    return UsuarioAtual(id=linha.id, papel=Papel(linha.papel))
```

- Uma consulta valida e renova (expiração deslizante limitada pelo teto `expira_max`).
  Escrever a cada request é simples; se pesar, renovar só quando faltar menos da metade do TTL.
- `text()` com parâmetros nomeados: seguro contra injeção; não monte o SQL com f-string.
- Sem linha -> 401. Falha de **infraestrutura** (banco fora) propaga como exceção -> 500,
  nunca 401.
- `Papel` é `StrEnum` (`USUARIO`, `ADMIN`); coluna `papel` com `CHECK` no banco.

## CSRF

Cookie é enviado automaticamente pelo navegador: defesa em camadas.

1. **`SameSite=Lax`** no cookie: bloqueia o envio em POST cross-site.
2. **Verificação de origem** em métodos que mudam estado: rejeitar quando `Origin` (ou
   `Sec-Fetch-Site`) indica outro site.
3. CORS restritivo (só a origem do front; `cabecalhos-cors` em `revisao-seguranca`).

```python
METODOS_SEGUROS = {"GET", "HEAD", "OPTIONS"}

def registrar_csrf(app: FastAPI, origem_permitida: str) -> None:
    @app.middleware("http")
    async def proteger_csrf(request: Request, call_next):  # type: ignore[no-untyped-def]
        if request.method not in METODOS_SEGUROS:
            site = request.headers.get("sec-fetch-site")
            origem = request.headers.get("origin")
            if (site and site not in {"same-origin", "none"}) or (origem and origem != origem_permitida):
                return _problema(403, "csrf", "Sem permissão", "Origem não permitida.")
        return await call_next(request)
```

- Reforço: **token CSRF** (valor aleatório por sessão em `X-CSRF-Token`, comparado com
  `secrets.compare_digest`) se houver clientes sem `Origin`/`Sec-Fetch-Site`; para navegador
  moderno com `SameSite=Lax`, a verificação de origem costuma bastar (ADR).
- `GET` **nunca** muda estado. Requisição sem `Origin` (curl, testes) passa: o cookie
  `SameSite` continua protegendo o navegador.

## Autorização: papel e dono do recurso

Papel por rota não basta: A não pode cancelar a reserva de B (IDOR). Checar **no serviço**,
perto do dado:

```python
def cancelar(session: Session, usuario: UsuarioAtual, reserva_id: int) -> None:
    r = repository.buscar(session, reserva_id)
    if r is None or (r.usuario_id != usuario.id and usuario.papel is not Papel.ADMIN):
        raise NaoEncontrado(f"Reserva {reserva_id} não existe.")      # 404: não revela que existe
    session.delete(r)
    session.commit()
```

- Decida e documente (ADR/openapi) 403 ou 404 para recurso de outro usuário; 404 evita
  enumerar IDs.
- O dono vem **sempre** da sessão (`UsuarioAtual`), nunca do corpo ou da URL: o schema
  `CriarReserva` não tem `usuario_id` (e tem `extra="forbid"`).
- Listagens filtram por dono no SQL (`where(Reserva.usuario_id == usuario.id)`); rotas
  administrativas usam `AdminDep` **e** o serviço também confere.

## Cabeçalhos e outras proteções

- `X-Content-Type-Options: nosniff`, `Referrer-Policy`, CSP e HSTS no proxy reverso ou no
  middleware (`dependencias-paginacao.md`).
- CORS só com origens diferentes; nunca `*` com credenciais.
- Segredos só por variável de ambiente (`pydantic-settings`); comparar segredos com
  `secrets.compare_digest`; `pip-audit` e `ruff` (regra `S`) no CI.
- Sem `eval`/`exec`/`pickle` de dado externo; `yaml.safe_load`; nada de `shell=True` com entrada.

## Erros comuns

| Erro | Correção |
|---|---|
| Token de sessão em claro no banco | Guardar SHA-256 do token |
| `random` para token | `secrets.token_urlsafe(32)` |
| Reaproveitar a sessão anterior no login | Criar sessão **nova** a cada login |
| Hash de senha no loop de uma rota `async` | `run_in_threadpool` (ou rota `def`) |
| Mensagens/tempos diferentes para usuário inexistente e senha errada | Mesma resposta; `HASH_FALSO` para igualar tempo |
| Cookie sem `HttpOnly`/`Secure`/`SameSite` | Os três (`secure` em produção) |
| Sem proteção CSRF por usar cookie | `SameSite=Lax` + verificação de origem/token |
| Só `Depends` de papel na rota | Checar dono do recurso no serviço |
| Banco fora vira 401 | Erro de infraestrutura é 5xx |
| Senha com SHA-256 | `argon2` (Argon2id) ou `bcrypt` |
| `response_model` com modelo `Usuario` | Schema de saída sem `senha_hash` |
