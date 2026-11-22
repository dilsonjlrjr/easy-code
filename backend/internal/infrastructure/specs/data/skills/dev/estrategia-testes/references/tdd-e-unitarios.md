# TDD na prática e testes unitários bons

## O ciclo

```
 vermelho  -->  verde  -->  refatorar  --+
    ^                                    |
    +------------------------------------+
```

1. **Vermelho:** escreva um teste pequeno para o próximo comportamento. Rode. Ele deve falhar
   **pelo motivo certo** (a asserção, não erro de compilação, import ou setup). Falhar por outro
   motivo não prova nada.
2. **Verde:** escreva o código mais simples que passa. Pode ser feio, pode ser constante.
   "Fake it till you make it": com um segundo teste, a generalização se impõe.
3. **Refatorar:** com tudo verde, melhore nomes, remova duplicação, extraia função. Não muda
   comportamento, não adiciona teste novo aqui.

Passos curtos: ciclo de 2 a 10 minutos. Se o vermelho dura meia hora, o passo estava grande.

### Estratégias para sair do vermelho

| Situação | Estratégia |
|---|---|
| Sei exatamente o código | **Implementação óbvia**: escreva direto |
| Não sei como generalizar | **Triangulação**: segundo teste com outro valor força a regra geral |
| Não sei por onde começar | **Fake it**: devolva o valor esperado e evolua |
| Teste grande demais | Reduza: comece pelo caso degenerado (lista vazia, intervalo de 0) |

### Ordem sugerida dos casos

Degenerado (vazio/zero) → caso feliz → limites → recusas/erros → invariantes. Os limites e as
recusas é que pagam o teste.

## Exemplo completo: sobreposição de intervalos (regra pura)

Regra: dois intervalos `[inicio, fim)` na mesma sala se sobrepõem se `a.inicio < b.fim` e
`b.inicio < a.fim`. Fim é exclusivo: terminar às 10h e começar às 10h **não** conflita.

Casos (um por teste): separados; um dentro do outro; sobrepõe pelo início; sobrepõe pelo fim;
idênticos; colados (fim = início) → sem conflito.

```java
// Java (JUnit 5)
@Test
void intervalosColadosNaoSeSobrepoem() {
    var a = Intervalo.de(hora(9), hora(10));
    var b = Intervalo.de(hora(10), hora(11));
    assertFalse(a.sobrepoe(b));
}
```

```go
// Go
func TestIntervalo_Colados_NaoSobrepoem(t *testing.T) {
    a := NovoIntervalo(hora(9), hora(10))
    b := NovoIntervalo(hora(10), hora(11))
    if a.Sobrepoe(b) {
        t.Fatal("intervalos colados não devem se sobrepor")
    }
}
```

```rust
// Rust
#[test]
fn intervalos_colados_nao_se_sobrepoem() {
    let a = Intervalo::novo(hora(9), hora(10)).unwrap();
    let b = Intervalo::novo(hora(10), hora(11)).unwrap();
    assert!(!a.sobrepoe(&b));
}
```

```python
# Python (pytest)
def test_intervalos_colados_nao_se_sobrepoem():
    a = Intervalo(hora(9), hora(10))
    b = Intervalo(hora(10), hora(11))
    assert not a.sobrepoe(b)
```

```ts
// TypeScript (Vitest)
it('intervalos colados não se sobrepõem', () => {
  const a = intervalo(hora(9), hora(10))
  const b = intervalo(hora(10), hora(11))
  expect(sobrepoe(a, b)).toBe(false)
})
```

Mesma regra, em testes **tabulares** quando só mudam os dados (um motivo de falha por linha,
mensagem identificando a linha):

```go
casos := []struct{ nome string; a, b Intervalo; quer bool }{
    {"separados", iv(9, 10), iv(11, 12), false},
    {"colados", iv(9, 10), iv(10, 11), false},
    {"sobrepoe pelo fim", iv(9, 11), iv(10, 12), true},
    {"contido", iv(9, 12), iv(10, 11), true},
}
for _, c := range casos {
    t.Run(c.nome, func(t *testing.T) { /* ... */ })
}
```

Equivalentes: `@ParameterizedTest` + `@CsvSource` (Java), `#[rstest]` (Rust, crate `rstest`, conferir),
`@pytest.mark.parametrize` (Python), `it.each` (Vitest).

## Quando TDD vale e quando não

| Vale muito | Pode pular o "teste primeiro" (mas teste depois, no mesmo commit) |
|---|---|
| Regra de negócio, cálculo, máquina de estados | Layout, CSS, texto de tela |
| Bug (teste que reproduz primeiro) | Spike descartável (jogue fora ou reescreva com teste) |
| Parser, validação, formatação | Configuração, fiação de framework, boilerplate gerado |
| Código com muitos casos de borda | Código que só repassa chamada a biblioteca |
| Refatoração de código sem teste (teste de caracterização primeiro) | Descobrir uma API desconhecida (explore, depois teste) |

Pular TDD **não** significa pular o teste: a T só conclui com o teste dentro do critério de
pronto. E nunca escreva o teste copiando a saída atual sem conferir se ela está certa.

### Teste de caracterização (código sem teste)

Antes de refatorar código herdado: escreva testes que **fixam o comportamento atual**, mesmo
estranho; refatore; depois discuta o que está errado como achado (não conserte no meio).

## Testes unitários bons

### Estrutura Arrange-Act-Assert (Dado-Quando-Então)

```python
def test_cancelar_reserva_ja_iniciada_e_recusado():
    # Arrange
    agora = datetime(2026, 3, 10, 10, 30, tzinfo=UTC)
    reserva = uma_reserva(inicio=agora - timedelta(minutes=30), fim=agora + timedelta(minutes=30))
    servico = ServicoReserva(relogio=RelogioFixo(agora))
    # Act
    with pytest.raises(ReservaJaIniciada):
        servico.cancelar(reserva)
    # Assert (estado também)
    assert reserva.estado == Estado.ATIVA
```

- Uma fase Act; várias asserções só se descrevem **um** comportamento (ex.: os campos de um
  objeto resultante).
- Sem `if`, laço ou lógica no teste. Teste não deve ter bug próprio.
- Arrange grande: extraia **builder** com defaults válidos; o teste mostra só o que importa.

```java
var reserva = umaReserva().naSala("Azul").das(9, 10).doUsuario(ana).build();
```

### Um comportamento por teste

Nome longo é bom. "e" no nome costuma indicar dois testes. Se a falha não diz qual regra
quebrou sem abrir o código, divida.

### Asserção sobre resultado, não sobre implementação

| Frágil | Robusto |
|---|---|
| Verificar que `repo.save` foi chamado 1 vez | Ler de volta (fake) ou conferir o retorno/estado |
| Comparar o texto inteiro da exceção | Comparar o tipo/código de erro (+ campo relevante) |
| Comparar lista na ordem sem a ordem importar | Comparar como conjunto/ordenar |
| Asserção em `toString()` | Asserção nos campos |

### Determinismo: tempo

Nunca chame `now()` na regra. Injete um relógio e use um fixo no teste.

```java
// Java: java.time.Clock
var relogio = Clock.fixed(Instant.parse("2026-03-10T10:30:00Z"), ZoneOffset.UTC);
var servico = new ServicoReserva(relogio, repo);
```

```go
// Go: função ou interface simples
type Relogio interface{ Agora() time.Time }
type relogioFixo struct{ t time.Time }
func (r relogioFixo) Agora() time.Time { return r.t }
```

```rust
// Rust: trait pequena (ou passar o instante como parâmetro)
trait Relogio { fn agora(&self) -> DateTime<Utc>; }
```

```python
# Python: parâmetro/injeção; congelar tempo só se não houver alternativa (biblioteca freezegun, conferir)
servico = ServicoReserva(relogio=lambda: datetime(2026, 3, 10, 10, 30, tzinfo=UTC))
```

```ts
// TypeScript: Vitest tem vi.useFakeTimers() e vi.setSystemTime(...) (conferir na versão usada)
vi.useFakeTimers(); vi.setSystemTime(new Date('2026-03-10T10:30:00Z'))
```

Regra: **passar o instante como parâmetro** é a forma mais simples e vence qualquer
biblioteca de congelar tempo. Testar também as bordas: virada de dia, fuso, horário de verão
(se o fuso tem), 29 de fevereiro.

### Determinismo: aleatoriedade e identificadores

- Injete a fonte (gerador de id, semente do gerador aleatório).
- Teste de propriedade (`jqwik`, `testing/quick`, `proptest`, `hypothesis`, `fast-check`) é
  útil em parser, formatação, ordenação e invariantes ("nunca existem duas reservas
  sobrepostas após qualquer sequência de operações"). Registre a semente da falha para
  reproduzir. É complemento, não substituto, dos casos nomeados.

### Determinismo: ordem, ambiente e rede

- Teste não depende de outro teste nem da ordem (rode embaralhado de vez em quando).
- Sem leitura de variável de ambiente, arquivo fora do repositório ou rede real. Diretório
  temporário do framework para arquivos.
- Nada de estado global mutável compartilhado (singletons, cache estático): reinicie ou injete.

### Testar erros e recusas

Cada regra que recusa merece o teste do **tipo de erro** e do **estado intacto**. Erro de
domínio tipado (`ReservaSobreposta`) é testável; mensagem de texto livre, não.

## Erros comuns

| Erro | Correção |
|---|---|
| Teste passa na primeira execução | Quebre o código de propósito para vê-lo falhar |
| Asserção que sempre é verdadeira (`assertTrue(lista != null)`) | Afirmar conteúdo |
| Setup copiado em 30 testes | Builder/fábrica com defaults válidos |
| Teste acoplado à ordem do `Map`/`set` | Comparar de forma independente da ordem |
| `catch (Exception)` no teste engolindo a falha | Usar a asserção de exceção do framework |
| Um teste enorme "faz tudo" | Dividir por comportamento |
| Dado "esperto" gerado por função igual à do código | Valor esperado literal e calculado à mão |
