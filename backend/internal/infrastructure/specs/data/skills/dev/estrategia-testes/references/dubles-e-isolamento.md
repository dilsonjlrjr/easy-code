# Dublês de teste e quando NÃO mockar

## Vocabulário (use estes termos)

| Dublê | O que faz | Exemplo |
|---|---|---|
| **Dummy** | Preenche parâmetro que não é usado | Logger nulo passado só para compilar |
| **Stub** | Devolve respostas fixas | `ClienteCep` que sempre devolve "Fortaleza" |
| **Fake** | Implementação funcional simplificada | Repositório em memória; relógio fixo; fila em memória |
| **Spy** | Registra o que recebeu para conferir depois | Enviador de e-mail que guarda as mensagens |
| **Mock** | Pré-programado com expectativas; falha se a "conversa" diferir | `verify(envio).enviar(...)` |

Preferência, da mais robusta para a mais frágil: **fake > stub > spy > mock**. Quanto mais o
teste descreve *como* o código conversa com a dependência, mais ele quebra ao refatorar.

## Regra de decisão

| Dependência | Faça | Por quê |
|---|---|---|
| **PostgreSQL** (repositório, SQL, transação) | **Usar o real** (Testcontainers ou o Compose do projeto) | Constraint, índice, tipo, isolamento e SQL só existem lá |
| Relógio, gerador de id, aleatoriedade | **Injetar** (fake/fixo) | Determinismo |
| E-mail, SMS, pagamento, serviço de terceiros (HTTP) | **Fake ou spy na borda** (porta/interface sua) | Lento, caro, instável, efeito externo |
| Serviço HTTP de terceiros, quando o formato importa | Servidor falso local (WireMock, `httptest`, `wiremock-rs`/`httpmock` conferir, `responses`/`respx`, MSW) | Prova o contrato do cliente |
| Outro módulo seu, na mesma aplicação | **Usar o real** | Mockar o que é seu esconde bug de integração |
| Sistema de arquivos | Diretório temporário real | Barato e fiel |
| Fila/mensageria | Real em container se a semântica importa; fake se só importa "foi enviado" | Depende do risco |
| Biblioteca/framework | **Não** mockar | Você testaria o mock |
| Tipos de valor (entidade, VO) | **Nunca** | São o que você quer testar |

Regra de ouro: **só dubla fronteira que você não controla** (rede, tempo, terceiros) e que
seria lenta, cara ou não determinística. Dublê no meio da sua própria lógica é sinal de
design acoplado (ver `padroes-arquitetura-codigo`).

## Por que não mockar o banco

Um teste com `mock(RepositorioReserva)` que devolve "nenhuma sobreposição" prova que o
**serviço** chama o repositório, nunca que a **sobreposição é detectada**. O que protege a
regra é a constraint `EXCLUDE USING gist` (ou a query) no PostgreSQL. Mock diz "passou" e a
produção grava duas reservas no mesmo horário.

| Aspecto do banco | Mock pega? | Banco real pega? |
|---|---|---|
| Sintaxe/tipo errado no SQL | não | sim |
| Constraint única/exclusão/FK | não | sim |
| Mapeamento coluna-campo, nulos, enum | não | sim |
| Transação, nível de isolamento, deadlock | não | sim |
| Migration aplicada vs. modelo | não | sim |
| Performance de índice (plano) | não | parcial |

Custo do banco real hoje é baixo: container sobe em segundos, e reaproveitá-lo na suíte
dilui o custo. Detalhe em `integracao-banco-api-contrato.md`.

### Fake de repositório em memória: quando serve

Serve para testes **unitários da orquestração** (serviço), quando o foco é a regra do serviço
e não o armazenamento. Cuidados:

- O fake deve ter o **mesmo contrato** do real. Se o real devolve vazio para não achado e o
  fake lança exceção, os testes mentem.
- Rode **os mesmos testes de contrato** contra o fake e o real (uma suíte de comportamento
  do repositório, executada com os dois). Se isso parece caro demais, use só o real.
- Não replique constraints do banco no fake; a garantia testada no integração continua lá.

Se o fake cresce além de uns 30 linhas, pare: provavelmente o teste deveria ser de integração.

## Exemplos

### Stub: resposta fixa

```java
ClienteFeriados feriados = data -> false;   // stub: interface funcional
var servico = new ServicoReserva(relogio, repo, feriados);
```

```go
type feriadosStub struct{ eh bool }
func (f feriadosStub) EhFeriado(time.Time) bool { return f.eh }
```

### Spy: conferir efeito de saída sem acoplar à conversa

```python
class EmailSpy:
    def __init__(self): self.enviados = []
    def enviar(self, para, assunto, corpo): self.enviados.append((para, assunto))

def test_cancelamento_avisa_o_dono_da_reserva():
    email = EmailSpy()
    servico = ServicoReserva(email=email, ...)
    servico.cancelar(reserva_da_ana)
    assert email.enviados == [("ana@exemplo.com", "Reserva cancelada")]
```

Afirma o **resultado observável** ("o e-mail saiu para Ana"), não "o método foi chamado com
tal argumento 1 vez".

### Fake de relógio e de id

```rust
struct RelogioFixo(DateTime<Utc>);
impl Relogio for RelogioFixo { fn agora(&self) -> DateTime<Utc> { self.0 } }

struct IdsSequenciais(std::cell::Cell<u64>);
```

### Mock: quando ainda faz sentido

- **Interação é o requisito**: "ao confirmar a reserva, notificar exatamente uma vez". Mesmo
  assim, um spy que conta é mais legível.
- **Dependência sem estado observável** (auditoria "fire-and-forget").
- Use mock com poucas expectativas, sem `verifyNoMoreInteractions` por reflexo.

Bibliotecas: Mockito (Java), `testify/mock` ou interfaces manuais (Go; preferir fakes
manuais), `mockall` (Rust, conferir), `unittest.mock`/`pytest-mock` (Python), `vi.fn()`
(Vitest). Conferir a API na versão usada.

## Injeção para tornar testável

- Dependência vem pelo **construtor** (ou parâmetro), não por singleton/global/`new` interno.
- A "porta" (interface) fica no lado de quem usa, pequena (`Relogio`, `EnviadorEmail`).
- Em Go: aceite interfaces, devolva structs; interface com 1–3 métodos.
- Em Rust: trait + genérico ou `dyn`; evite trait só para mock se não há 2ª implementação real
  (use função/closure).
- Não crie interface para toda classe "para poder mockar". Sem 2º uso real nem borda externa,
  é cerimônia.

## Testar a borda externa de verdade (sem mock cego)

Cliente HTTP de terceiro: suba servidor falso local que devolve o JSON **real** copiado da
documentação/contrato do terceiro, inclusive respostas de erro e timeout. Assim o teste cobre
serialização, cabeçalhos e tratamento de falha do **seu** cliente.

Casos mínimos: sucesso, 4xx com corpo de erro, 5xx, timeout/conexão recusada, resposta
malformada. Cada um prova uma decisão de tratamento de erro (ver `qualidade-codigo`).

## Erros comuns

| Erro | Efeito | Correção |
|---|---|---|
| Mockar repositório e confiar na constraint "porque existe" | Regra nunca testada | Integração com PostgreSQL real |
| `verify` em tudo | Refatorar quebra suíte inteira | Afirmar saída/estado |
| Mock devolvendo mock devolvendo mock | Desenho acoplado | Passar valores; ocultar a cadeia atrás de uma porta |
| Fake divergente do real | Teste verde, produção quebra | Suíte de contrato comum aos dois, ou usar o real |
| Mockar a classe sob teste (spy parcial) | Testa o mock | Extrair a parte substituída |
| Dublê para código seu e puro | Acopla sem proteger | Usar o real |
| Dados do stub inventados | Cliente real devolve outra forma | Copiar resposta real do terceiro |
