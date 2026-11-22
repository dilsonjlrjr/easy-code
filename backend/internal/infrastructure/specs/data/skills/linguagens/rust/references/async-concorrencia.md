# Async, concorrência e shutdown (Rust)

Aqui: como não quebrar o runtime `tokio`, onde rodar trabalho bloqueante, tarefas com dono e
encerramento limpo. Regra geral: **não use concorrência explícita quando uma chamada
`.await` sequencial resolve**; cada request já roda concorrente.

## Modelo mental

- `async fn` vira um `Future` que só avança quando aguardado (`.await`) por uma task do
  `tokio`. O runtime multi-thread tem poucas threads de trabalho (≈ núcleos de CPU).
- Dentro de um `.await` a thread é liberada para outras tasks. **Código síncrono demorado
  entre dois `.await` segura a thread** e atrasa todas as tasks dela.
- Regra de ouro: uma task não deve ficar mais que dezenas de microssegundos a poucos
  milissegundos sem chegar a um `.await`.

## O que bloqueia (e o que fazer)

| Operação | Problema | Faça |
|---|---|---|
| Hash de senha (`argon2`, `bcrypt`) | CPU por dezenas/centenas de ms | `tokio::task::spawn_blocking` |
| Compressão, parse/serialização gigante, criptografia pesada | CPU | `spawn_blocking` (ou `rayon` fora do runtime) |
| `std::fs`, `std::net` | E/S síncrona | `tokio::fs`, `tokio::net`, ou `spawn_blocking` |
| `std::thread::sleep` | Para a thread | `tokio::time::sleep` |
| `std::sync::Mutex` segurado em `.await` | `Future` não-`Send`, deadlock | Soltar o lock antes do `.await` |
| Driver de banco síncrono | Bloqueia | Driver async (`sqlx`) |
| Laço infinito sem `.await` | Task nunca cede | `tokio::task::yield_now().await` ou rever o desenho |

```rust
// hash de senha fora do runtime
pub async fn hash_senha(senha: String) -> Result<String, AuthErro> {
    tokio::task::spawn_blocking(move || hash_argon2(&senha))
        .await
        .map_err(|e| AuthErro::Interno(e.into()))?   // JoinError: a task entrou em pânico/foi cancelada
}
```

- O closure de `spawn_blocking` é `'static`: **mova** (`move`) os dados para dentro, não
  empreste do handler.
- `spawn_blocking` usa um pool separado (limite alto por padrão). Muitos hashes simultâneos
  esgotam CPU: proteger o login com limite de taxa e/ou `tokio::sync::Semaphore` para limitar
  hashes em paralelo ao número de núcleos.

## Estado compartilhado

- O padrão do axum é `State<Estado>` com `Estado: Clone`: o `PgPool` já é `Arc` por dentro,
  clonar é barato. Guardar `Config` em `Arc<Config>`.
- **Sem estado mutável em memória** para dado de negócio: o banco é a fonte (sessão, reserva,
  cache de verdade). Isso mantém várias instâncias corretas.
- Quando precisar de estado mutável pequeno (ex.: contador de métrica): `std::sync::atomic`
  ou `Mutex` **sem `.await` dentro da seção crítica**.

```rust
#[derive(Clone)]
pub struct Estado {
    pub pool: sqlx::PgPool,
    pub config: std::sync::Arc<Config>,
    pub relogio: std::sync::Arc<dyn Relogio>,
}
```

| Primitiva | Use quando |
|---|---|
| `Arc<T>` | Compartilhar leitura entre tasks |
| `std::sync::Mutex` | Seção curta, **sem** `.await` dentro |
| `tokio::sync::Mutex` | Precisa segurar o lock **através** de `.await` (raro; sinal de desenho a rever) |
| `RwLock` | Muitas leituras, poucas escritas, seção curta |
| `AtomicU64` etc. | Contadores e flags |
| `mpsc`/`oneshot`/`watch` | Passar mensagens/sinais entre tasks em vez de compartilhar memória |

## Tarefas em segundo plano com dono

`tokio::spawn` devolve `JoinHandle`; **descartar o handle não encerra a task** e esconde o
`panic`. Toda task de longa duração tem quem a espere e como parar.

```rust
// limpeza periódica de sessões expiradas
pub async fn limpar_sessoes(pool: PgPool, parar: CancellationToken) {
    let mut ticker = tokio::time::interval(Duration::from_secs(300));
    ticker.set_missed_tick_behavior(tokio::time::MissedTickBehavior::Skip);
    loop {
        tokio::select! {
            _ = parar.cancelled() => break,
            _ = ticker.tick() => {
                match sqlx::query!("DELETE FROM sessoes WHERE expira_em < now()").execute(&pool).await {
                    Ok(r) => tracing::debug!(removidas = r.rows_affected(), "sessões expiradas removidas"),
                    Err(e) => tracing::warn!(erro = %e, "falha ao limpar sessões"),
                }
            }
        }
    }
}
```

- `CancellationToken` vem de `tokio-util` (crate separada). Alternativa sem dependência:
  `tokio::sync::watch<bool>` ou `broadcast`.
- `JoinSet` agrupa tasks: `set.spawn(...)`, `while let Some(r) = set.join_next().await`. Ao
  descartar o `JoinSet`, as tasks são abortadas.
- Trabalho disparado por request que **deve sobreviver** ao request (ex.: enviar e-mail):
  fila em tabela do PostgreSQL com worker próprio, não `spawn` solto (se o processo cair, o
  trabalho some).
- `tokio::select!`: ramos precisam ser **cancel-safe**; ao ganhar um ramo, os outros futures
  são descartados no meio do caminho. Ler a documentação da operação antes de colocá-la em
  `select!`.

## Cancelamento e `Drop`

- Quando o cliente desconecta, o axum **descarta** o `Future` do handler no próximo `.await`.
  Código entre `.await`s pode não terminar: operação composta tem que estar em **transação**
  (o `Drop` da `Transaction` do `sqlx` faz rollback), não em sequência de escritas soltas.
- Não colocar efeito colateral irreversível (e-mail, cobrança) entre dois `.await` dentro de
  uma transação aberta; faça depois do `commit`.

## Timeouts

```rust
use tokio::time::{timeout, Duration};

let r = timeout(Duration::from_secs(3), cliente_externo.consultar()).await
    .map_err(|_| ApiError::Interno("timeout no serviço externo".into()))??;
```

- Toda chamada externa (HTTP, banco) tem timeout. No banco: `acquire_timeout` do pool e
  `statement_timeout` do PostgreSQL (ver `persistencia-sqlx.md`).
- `TimeoutLayer` do tower-http limita a requisição inteira; não substitui o timeout das
  chamadas internas.

## Graceful shutdown

```rust
// src/plataforma/mod.rs
pub async fn servir(app: Router, config: &Config) -> anyhow::Result<()> {
    let listener = tokio::net::TcpListener::bind(config.endereco).await
        .with_context(|| format!("bind em {}", config.endereco))?;
    tracing::info!(endereco = %config.endereco, "servidor no ar");

    let parar = CancellationToken::new();
    let limpeza = tokio::spawn(limpar_sessoes(pool.clone(), parar.clone()));

    axum::serve(listener, app)
        .with_graceful_shutdown(sinal_de_parada())
        .await
        .context("servidor")?;

    parar.cancel();
    limpeza.await.context("task de limpeza")?;
    tracing::info!("encerrado");
    Ok(())
}

async fn sinal_de_parada() {
    let ctrl_c = async { tokio::signal::ctrl_c().await.expect("instalar handler de Ctrl+C") };
    #[cfg(unix)]
    let term = async {
        tokio::signal::unix::signal(tokio::signal::unix::SignalKind::terminate())
            .expect("instalar handler de SIGTERM")
            .recv().await;
    };
    #[cfg(not(unix))]
    let term = std::future::pending::<()>();
    tokio::select! { _ = ctrl_c => {}, _ = term => {} }
}
```

- Docker/Kubernetes enviam `SIGTERM`; sem tratá-lo, o processo morre com requests em voo.
  `with_graceful_shutdown` para de aceitar conexões novas e espera as em andamento.
- Os `expect` do instalador de sinais são aceitáveis (falha só na subida); comentar a
  invariante. Preferir propagar com `?` se a função retornar `Result`.
- Limite o tempo de espera total (ex.: `tokio::time::timeout` em volta do `serve`) para não
  travar o deploy se houver conexão presa; alinhar com `stop_grace_period` do Compose.
- Ordem: parar de aceitar -> drenar requests -> parar workers (`cancel`) -> fechar o pool
  (`pool.close().await`) -> sair.

## Testar concorrência

- Regra de concorrência crítica (duas reservas simultâneas no mesmo horário) é testada
  contra o **banco real**: dispare N tasks (`JoinSet`) e afirme que exatamente uma vence e as
  demais recebem 409 (ver `testes-rust.md`).
- `tokio::test` usa runtime single-thread por padrão; para concorrência real:
  `#[tokio::test(flavor = "multi_thread", worker_threads = 4)]`.
- `loom` e `miri` são ferramentas para código com `unsafe`/atômicos próprios; não se aplicam
  a uma API que só usa `Arc`/`Mutex` e o banco (conferir se algum dia houver `unsafe`).
