# Concorrência, cobertura, regressão, velocidade e testes intermitentes

## 1. Testes de concorrência

Concorrência em aplicação web aparece principalmente como **duas requisições disputando o
mesmo recurso** (última vaga, mesmo horário, saldo, idempotência de reenvio). A garantia
quase sempre mora no banco (constraint, `SELECT ... FOR UPDATE`, nível de isolamento), então
o teste é de integração, com banco real.

### Receita: corrida de N requisições

1. Preparar o estado (uma sala livre às 9h).
2. Disparar N requisições idênticas **ao mesmo tempo** (barreira/latch para sincronizar a
   largada).
3. Afirmar o **invariante**, não a ordem: "exatamente 1 sucesso, N-1 conflitos, 1 linha no
   banco, nenhum 5xx".

```go
func TestReservaConcorrente_SoUmaVence(t *testing.T) {
    app := novaApp(t)
    const n = 10
    var wg sync.WaitGroup
    start := make(chan struct{})
    codigos := make(chan int, n)
    for i := 0; i < n; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            <-start
            codigos <- app.POST(t, "/reservas", corpoSalaAzul9h, comoUsuario(i)).Status
        }()
    }
    close(start); wg.Wait(); close(codigos)
    var ok, conflito int
    for c := range codigos { switch c { case 201: ok++; case 409: conflito++ } }
    if ok != 1 || conflito != n-1 { t.Fatalf("ok=%d conflito=%d", ok, conflito) }
    // e estado final: 1 linha
}
```

```java
// Java: ExecutorService + CountDownLatch; afirmar contagens depois de todos terminarem
```

```python
# Python: ThreadPoolExecutor ou asyncio.gather com httpx.AsyncClient; barreira com threading.Barrier
```

```rust
// Rust: tokio::spawn de N tarefas + tokio::sync::Barrier; collect dos status
```

```ts
// TS: Promise.all de N fetch (a concorrência real é no servidor)
```

### Cuidados

- **Conexões distintas** por requisição (pool com ao menos N conexões); com uma conexão só a
  corrida não existe.
- Isolamento por **TRUNCATE**, não por transação revertida (a transação do teste esconderia
  a disputa).
- Repetir o teste (por exemplo 20 a 50 execuções em laço, local) para ganhar confiança; no CI,
  uma execução por commit é suficiente se o invariante é garantido pelo banco.
- Se a garantia está **só no código** (sem constraint), o teste de corrida é o que expõe o
  bug; a correção correta costuma mover a garantia para o banco.
- Idempotência: mesmo `Idempotency-Key` enviado duas vezes em paralelo resulta em **um**
  efeito e a mesma resposta.
- Deadlock: duas operações que travam recursos em ordem oposta; teste com as duas ordens e
  afirme que uma conclui e a outra recebe erro tratável (409/503 com retry), nunca trava.

### Concorrência dentro do processo (estruturas compartilhadas)

Só quando há estado mutável em memória (cache, contador): use o detector da linguagem
(`go test -race`, ThreadSanitizer, `loom` em Rust, conferir) além do teste de corrida.
Preferir eliminar o estado compartilhado a testá-lo.

## 2. Cobertura

### O que medir

| Métrica | Serve para | Limite |
|---|---|---|
| Cobertura de linha/instrução | Achar código **nunca exercitado** | Não diz se algo foi afirmado |
| Cobertura de ramo (branch) | Achar decisões sem caso (`if` sem o `else`) | Idem |
| Cobertura **de requisitos** (CA/CT) | Provar que cada critério tem teste | É a que o gate confere |
| Mutação | Medir se os testes **detectam** mudança de comportamento | Lenta; usar em módulos críticos |

### Metas sensatas

- **Não imponha um número global** como gate de produto ("80%" vira teste sem asserção).
- Faixa de referência para **regra de negócio** (módulo de domínio): ramo alto, na casa de
  80–90%. Fiação, DTO, configuração: sem meta.
- Use cobertura como **mapa de lacunas**: abra o relatório, procure linhas vermelhas em
  código de risco, escreva o teste que importa.
- Cobertura **caiu** em uma T: justifique (código removido/gerado) ou cubra.
- Nunca excluir arquivo do relatório só para subir o número; excluir só código gerado e
  `main`/fiação, e registrar.

Ferramentas open source: JaCoCo (Java), `go test -cover`/`-coverprofile` (Go),
`cargo-llvm-cov` ou `cargo-tarpaulin` (Rust), `coverage.py`/`pytest-cov` (Python),
`@vitest/coverage-v8` (Vitest). Conferir comando e flags na versão usada.

### Mutação como sinal opcional

Ferramenta altera o código (troca `<` por `<=`, remove chamada) e roda os testes; mutante
que **sobrevive** indica teste fraco. PIT (Java), `cargo-mutants` (Rust), `mutmut` (Python),
Stryker (TypeScript), Go: ferramentas existem (conferir a escolhida).

Quando usar: em 1–2 módulos de regra crítica (sobreposição de reservas, cálculo), uma vez,
para calibrar a suíte. Não como gate de todo commit (lenta, e mutantes equivalentes geram
ruído). Um mutante sobrevivente em limite (`<` vs `<=`) é exatamente o teste de borda que
faltava.

## 3. Regressão para cada bug (ACH)

Todo achado de `categoria: defeito` (ou `vulnerabilidade`) só fecha com teste:

1. Reproduza no **nível mais baixo** que captura o defeito (preferir unitário ou integração a e2e).
2. Veja o teste **falhar com a evidência do achado** (mesma entrada, mesmo erro).
3. Corrija; veja passar; rode a suíte inteira.
4. Teste e correção no **mesmo commit** `fix(ACH-NN): <resumo>`.
5. Nome pelo comportamento ("reserva_colada_nao_conflita"), com o id do ACH em comentário
   curto se ajudar a rastrear.
6. Se o defeito revela **classe** de erro (ex.: fuso), procure e teste os vizinhos (outras
   regras com data), não só o ponto reportado.

Achado de **requisito ambíguo** ou **desvio de arquitetura** não vira teste do Dev sem a
decisão da etapa destino. Achado de segurança (ex.: IDOR) vira teste de autorização no
nível de API.

## 4. Velocidade

Orçamento de referência para aplicação pequena: **unitários em segundos**, integração em **poucos
minutos**. Se passar disso, ninguém roda antes de commitar.

| Alavanca | Efeito |
|---|---|
| Separar `test` (unitário) de `test:integration` | Feedback rápido no ciclo TDD |
| Um container por suíte; clonar banco por worker | Elimina o custo de subir |
| Paralelizar pacotes/arquivos com banco isolado | Tempo total menor |
| Descer cada verificação de e2e para integração | Menos navegador |
| Evitar `sleep`; esperar condição | Tira segundos fixos |
| Não recriar o schema por teste | Migrations uma vez |
| Cache de dependências no build Docker | CI mais curto |

Medir antes de otimizar: a maioria dos runners lista os testes mais lentos (`--durations` no
pytest, `-v`/relatório no Go e Vitest, relatório Surefire/Gradle). Ataque os 5 piores.

## 5. Testes intermitentes (flaky)

**Definição operacional:** passou e falhou sem mudar o código. Conta como **falha** até estabilizar
(regra do QA). Não "rodar de novo até passar".

### Causas e remédios

| Causa | Sinal | Remédio |
|---|---|---|
| Tempo real (`now`, `sleep`) | Falha em horário/virada de dia | Injetar relógio; esperar condição |
| Ordem/dados compartilhados | Passa isolado, falha na suíte | Dados próprios, isolamento, rodar embaralhado |
| Corrida do teste (assíncrono sem esperar) | Falha raramente | Aguardar o evento/condição (polling com limite e mensagem) |
| Porta/recurso fixo | Falha em paralelo | Porta efêmera, banco por worker |
| Dependência externa real | Falha por rede | Fake/servidor local |
| Ordem de `Map`/`set`/consulta sem `ORDER BY` | Mudança de ordem | Ordenar explicitamente no código e no teste |
| Aleatoriedade sem semente | Falha rara | Semente fixa e registrada |
| Recurso limitado (CPU/memória do CI) | Timeout só no CI | Timeout generoso para condição, não para `sleep` |
| Estado global (cache, singleton) | Falha após outro teste | Reiniciar/injetar |

### Espera correta

```ts
// em vez de sleep: aguardar a condição com limite
await vi.waitFor(() => expect(lista.value).toHaveLength(3))   // conferir API na versão
```

```python
# polling simples com limite e mensagem clara
def esperar(cond, limite=5.0, passo=0.05): ...
```

### Procedimento ao achar um flaky

1. Reproduzir em laço (`-count=100`, `--repeat`, `pytest -p no:randomly` vs aleatório,
   conferir flag) e guardar a semente/ordem.
2. Isolar a causa na tabela acima; corrigir a causa, não o sintoma.
3. **Não** aplicar retry automático de teste como correção. Quarentena só com prazo e dono, e
   o teste sai de quarentena ou é removido.
4. Se for bug real de concorrência na aplicação, abrir ACH: o teste instável apontou um defeito.

## Erros comuns

| Erro | Correção |
|---|---|
| Teste de corrida com uma conexão | Pool com N conexões e barreira de largada |
| Afirmar qual requisição venceu | Afirmar o invariante (1 sucesso, estado final) |
| Meta global de cobertura como gate | Cobertura como mapa; gate é CA/CT cobertos |
| Mutação em todo commit | Pontual, em módulo crítico |
| Retry automático para esconder flaky | Achar e corrigir a causa |
| Bug corrigido sem teste | Voltar e escrever o teste que reproduz |
| Ignorar o teste lento "porque roda só no CI" | Medir e atacar os piores |
