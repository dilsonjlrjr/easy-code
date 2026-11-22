# Logging, configuração por ambiente e segredos

## Logging útil

Log serve para **operar**: entender o que aconteceu, com que dado, em que requisição. Não é
depuração permanente nem auditoria.

### Níveis

| Nível | Uso | Exemplo |
|---|---|---|
| ERROR | Falha que exige atenção; operação não concluída por causa inesperada | Banco indisponível, exceção não tratada |
| WARN | Situação anormal tratada, que pode virar problema | Retry esgotado e usou fallback; limite próximo |
| INFO | Marcos de negócio/ciclo de vida, **poucos** | Aplicação iniciou; reserva criada (id) |
| DEBUG | Detalhe para diagnóstico, desligado em produção | Parâmetros de consulta (sem dado sensível) |

Recusa **esperada** do domínio (sala ocupada, validação) **não é ERROR**: é INFO/DEBUG ou nem
log (métrica). Ruído em ERROR esconde falha de verdade.

### Estrutura

- **Estruturado** (JSON ou chave=valor), uma linha por evento, em **stdout** (o Docker
  coleta). Nada de arquivo de log dentro do container.
- Campos úteis: horário (UTC), nível, mensagem **constante**, `request_id` (correlação),
  `usuario_id` (id, não nome/e-mail), recurso (`reserva_id`), duração.
- Mensagem **fixa** + campos variáveis (`"reserva criada"` com `reserva_id=...`), não
  interpolada, para agrupar e buscar.
- **Id de correlação** por requisição: gere na entrada (ou aceite do cabeçalho), propague no
  contexto e devolva no erro 500 para o usuário citar.
- Registrar o erro **uma vez**, onde é tratado (ver `erros-validacao-tipos`), com a causa e
  a pilha.

```go
slog.Info("reserva criada", "reserva_id", r.ID, "sala_id", r.SalaID, "request_id", reqID)
```

```java
log.atInfo().addKeyValue("reservaId", r.id()).log("reserva criada");   // SLF4J 2 (conferir)
```

```python
log.info("reserva criada", extra={"reserva_id": r.id, "request_id": req_id})   # ou structlog (conferir)
```

```rust
tracing::info!(reserva_id = %r.id, sala_id = %r.sala_id, "reserva criada");
```

Bibliotecas por linguagem (open source): `slog` (Go, biblioteca padrão), SLF4J + Logback ou
Log4j2 (Java), `logging` padrão/`structlog` (Python), `tracing` (Rust), `pino` (Node).
Conferir a que `estrutura.md` define.

### O que NUNCA logar

| Dado | Motivo |
|---|---|
| Senha, hash de senha, token, chave de API, cookie de sessão, cabeçalho `Authorization` | Segredo |
| Corpo inteiro de requisição/resposta | Pode conter qualquer dado |
| Dado pessoal (CPF, e-mail completo, telefone, endereço, data de nascimento) | LGPD; usar **id** |
| Cartão, dado financeiro | Regulatório |
| String de conexão com senha | Segredo |
| Stack de exceção devolvida ao cliente | Vazamento (log sim, resposta não) |

Se precisa identificar alguém, use o **id interno**. Se precisa do e-mail para suporte,
mascare (`a***@exemplo.com`). Cuidado com **mensagem de exceção** de biblioteca que embute a
consulta SQL ou a URL com token: sanitize.

### Quanto logar

- Cada requisição: uma linha de acesso (método, rota **template**, status, duração) — muitas
  vezes já vem do framework/proxy.
- Cada decisão importante de negócio: uma linha INFO com ids.
- Dentro de laço: **não** (volume explode). Agregue.
- Erros: com contexto suficiente para reproduzir **sem** precisar do dado sensível.

## Configuração por ambiente

Regra: **o mesmo artefato roda em qualquer ambiente; o que muda é a configuração**, injetada
por variável de ambiente (12-factor).

| Tipo | Exemplo | Onde mora |
|---|---|---|
| Não sensível, varia por ambiente | `PORT`, `LOG_LEVEL`, `DB_HOST`, URL base, CORS permitido | Variável de ambiente (Compose/orquestrador) |
| **Sensível** | `DB_PASSWORD`, chave JWT, chave de API | Variável de ambiente vinda de **segredo** (não do git) |
| Constante de negócio | Duração mínima de reserva, horário comercial | Código/constante ou tabela de configuração do domínio — **não** variável de ambiente, salvo se o negócio exige mudar sem deploy |

### Práticas

- **Carregar e validar tudo na inicialização** em um único módulo de configuração, com tipos
  (inteiro, URL, duração). Faltou obrigatória ou formato ruim: **a aplicação não sobe**,
  com mensagem que diz qual variável.
- **Sem `if (ambiente == "prod")` espalhado** no código. Comportamento muda por configuração
  (`CORS_ORIGINS`, `LOG_LEVEL`), não por teste de nome de ambiente.
- Defaults seguros: sem default para segredo; default de desenvolvimento só em
  `.env.example`/Compose de dev.
- `.env.example` **atualizado** a cada variável nova, com valor fictício e comentário curto.
- `.env` real **no `.gitignore`**; nunca commitado.
- Config de **teste** separada e sem segredo real.

```python
# validação na partida (Pydantic Settings é uma opção, conferir versão)
class Config:
    db_url: str = env_obrigatoria("DATABASE_URL")
    log_level: str = env("LOG_LEVEL", "info")
```

```rust
let porta: u16 = std::env::var("PORT").unwrap_or_else(|_| "8080".into()).parse()
    .expect("PORT deve ser um número");   // na inicialização, falhar alto é aceitável
```

## Segredos

### Regras

1. **Nenhum segredo no repositório**: nem no código, nem em teste, nem em `docker-compose`
   versionado, nem em `README`, nem no histórico do git.
2. **Segredo entra por variável de ambiente** (ou arquivo montado, ex.: Docker secrets) em
   tempo de execução; **nunca na imagem** (nem `ENV` no Dockerfile, nem `ARG` de build para
   segredo real).
3. **Credenciais com o menor privilégio**: usuário do banco da aplicação sem permissão de
   DDL em produção (migration roda com outro papel, se o DBA definir).
4. **Rotacionável**: trocar sem mudar código.
5. Valores de **exemplo** (`.env.example`) são claramente fictícios (`troque-isto`).
6. **Segredo vazou (commitado)**: considere comprometido; **rotacione**; não basta apagar o
   commit.

### Detecção

Varredura automática de segredos no repositório (gitleaks, trufflehog, ambos open source;
conferir a ferramenta da skill de segurança) antes de concluir a T e no CI. Detalhes em
`revisao-seguranca`.

### Front-end

- **Variável pública é pública:** tudo que vai para o bundle (`VITE_*`) é lido por qualquer
  um. Nunca coloque chave secreta nelas (ver `frontend-vue`).

## Erros comuns

| Erro | Efeito | Correção |
|---|---|---|
| `println`/`console.log` de depuração esquecido | Ruído, vazamento | Remover; log estruturado se for útil |
| Logar o corpo da requisição inteiro | Dado pessoal/segredo no log | Logar ids e campos escolhidos |
| Recusa de domínio em ERROR | Alarme falso | INFO/DEBUG ou métrica |
| Mensagem interpolada com dado | Não agrega nem filtra | Mensagem fixa + campos |
| Mesmo erro logado em 4 camadas | Log inútil | Logar só onde trata |
| Config lida espalhada pelo código | Impossível validar | Módulo único na partida |
| Faltou variável e a app sobe com valor errado | Falha em runtime estranha | Validar e abortar na inicialização |
| Senha no `docker-compose.yml` versionado | Vazamento | Variável + `.env` ignorado; segredo no ambiente |
| `ARG`/`ENV` com segredo no Dockerfile | Vai para as camadas da imagem | Passar em runtime |
| `.env.example` desatualizado | Onboarding quebrado | Atualizar junto da variável nova |
