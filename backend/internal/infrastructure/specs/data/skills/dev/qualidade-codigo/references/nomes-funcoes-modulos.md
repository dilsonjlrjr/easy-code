# Nomes, funções e módulos

## Nomes

O nome é a documentação mais lida. Regras:

| Regra | Ruim | Bom |
|---|---|---|
| Diga a **intenção**, não o tipo | `data`, `lista`, `obj` | `reservasDoDia`, `salasLivres` |
| Use o **vocabulário do domínio** (glossário/US) | `Booking`, `Room` se o projeto diz Reserva, Sala | `Reserva`, `Sala` |
| Um conceito, uma palavra | `buscar`, `obter`, `pegar`, `carregar` misturados | escolher `buscar` (pode falhar) e `obter` (deve existir) e manter |
| Verbo para ação, substantivo para coisa | `reserva()` que cancela | `cancelarReserva()` |
| Booleano como pergunta | `flag`, `status` | `estaAtiva`, `temConflito` |
| Sem negação dupla | `naoEstaInativo` | `estaAtivo` |
| Tamanho proporcional ao escopo | `indiceDoUsuarioNaListaDeConvidados` em laço de 3 linhas | `i` em laço curto; nome longo para escopo longo |
| Sem prefixo de tipo/encoding | `strNome`, `IReserva` (onde não é convenção) | `nome` |
| Unidade no nome quando for número cru | `timeout = 30` | `timeoutSegundos`, ou tipo `Duration` |

Convenções de caixa seguem a linguagem: `camelCase` (Java, TypeScript), `snake_case` (Rust,
Python), `MixedCaps` (Go), `PascalCase` para tipos. Pacote/módulo curto e sem plural
gratuito.

Nome difícil de achar costuma indicar função fazendo duas coisas ou conceito mal entendido.
Pare e releia o requisito.

### Evitar

- Palavras vazias: `Manager`, `Helper`, `Util`, `Handler`, `Data`, `Info`, `Processor`
  (aceite só quando for convenção do framework e o resto do nome especifica: `ReservaController`).
- `Service` genérico para tudo: prefira `CancelamentoDeReserva` ou funções soltas ao lado do
  domínio.
- Nome enganoso (`validar` que também grava).

## Funções

### Tamanho e foco

- **Uma coisa, em um nível de abstração.** Lê-se de cima para baixo como uma história.
- Referência (não lei): cabe em uma tela, ~10 a 30 linhas. O sinal real é "preciso comentar
  blocos" ou "tem um 'e' no nome".
- **Retorno antecipado** em vez de `if` aninhado:

```python
# antes
def cancelar(reserva, usuario):
    if reserva is not None:
        if reserva.dono == usuario:
            if not reserva.iniciada():
                reserva.estado = CANCELADA
# depois
def cancelar(reserva, usuario):
    if reserva.dono != usuario:
        raise SemPermissao()
    if reserva.iniciada():
        raise ReservaJaIniciada()
    reserva.estado = CANCELADA
```

- Profundidade de aninhamento: 2 níveis é ok; 3+ extraia ou inverta condição.

### Parâmetros

| Situação | Faça |
|---|---|
| 0–3 parâmetros | Normal |
| Muitos parâmetros relacionados | Objeto/struct de parâmetro com nome (`NovaReserva{sala, inicio, fim}`) |
| Parâmetro booleano que troca o comportamento | Duas funções (`cancelar`, `cancelarSemAviso`) ou enum |
| Parâmetros do mesmo tipo em sequência (`String, String, String`) | Tipos distintos ou objeto nomeado |
| Valores opcionais | Opcional explícito/defaults/builder; não `null` posicional |

### Efeitos colaterais

- Separe **consulta** (devolve algo, não altera) de **comando** (altera, devolve pouco).
- Função pura é a mais fácil de testar: puxe a decisão para função pura e deixe o I/O nas
  bordas (núcleo funcional, casca imperativa).
- Não altere o argumento recebido a menos que o nome diga (`ordenarNoLugar`).

```go
// decisão pura (testável sem banco)
func podeCancelar(r Reserva, u Usuario, agora time.Time) error { ... }

// casca com I/O
func (s *Servico) Cancelar(ctx context.Context, id ReservaID, u Usuario) error {
    r, err := s.repo.Obter(ctx, id)
    if err != nil { return err }
    if err := podeCancelar(r, u, s.agora()); err != nil { return err }
    return s.repo.Salvar(ctx, r.Cancelada())
}
```

## Módulos, pacotes e arquivos

### Coesão e acoplamento

- **Alta coesão:** tudo que muda junto fica junto. Agrupar por **funcionalidade/domínio**
  (`reserva/`, `sala/`) em vez de por tipo técnico (`controllers/`, `services/`), salvo se
  `estrutura.md` da Arquitetura define outra coisa — ela manda.
- **Baixo acoplamento:** um módulo expõe uma **API pequena**; o resto é privado. Dependência
  vai na direção domínio ← infraestrutura, não o contrário (ver `padroes-arquitetura-codigo`).
- **Sem ciclo** entre módulos. Ciclo = extrair o conceito comum ou inverter a dependência.
- Visibilidade mínima: privado por padrão (`private`, minúscula em Go, `pub(crate)` em Rust,
  `_prefixo`/`__all__` em Python, `export` seletivo em TypeScript).

### Tamanho de arquivo/classe

- Referência: arquivo com mais de ~300 linhas ou classe com mais de ~7 responsabilidades
  merece olhar. Mas dividir por tamanho sem coesão só espalha o problema.
- Evite arquivo `utils`/`helpers`/`common` que vira depósito: nomeie pelo que faz
  (`intervalos`, `formatacao_data`).

### Dados e comportamento

- Dados de domínio com comportamento junto quando a regra é da entidade
  (`reserva.cancelar(agora)`), em vez de "serviço anêmico" que só mexe em campos. Mas sem
  forçar orientação a objetos onde funções simples bastam (Go, Python e Rust incentivam
  funções + tipos).
- Imutável por padrão (`record`/`final` em Java, `readonly` em TypeScript, structs sem setters
  em Go/Rust, `dataclass(frozen=True)` em Python): menos estado, menos bug.

## Estrutura do diretório de um recurso (ilustração)

```
reserva/
  dominio      Reserva, Intervalo, regras puras (sem I/O)
  servico      casos de uso (orquestra repositório + regras)
  repositorio  SQL/persistência
  http         handlers/controllers, DTOs, mapeamento de erro -> status
  testes
```

Os nomes das pastas seguem a linguagem e o `estrutura.md`; a ideia é separar regra, I/O e
borda sem criar camadas vazias.

## Erros comuns

| Erro | Correção |
|---|---|
| `processarDados()` | Nome do que realmente faz: `calcularMultaPorAtraso()` |
| Função de 200 linhas "porque é sequencial" | Extrair passos com nome; a sequência vira a história |
| `Util` com 40 funções soltas | Mover cada função para o módulo do conceito |
| Parâmetro `modo: string` ("a", "b") | Enum/tipo; ou funções distintas |
| Getter/setter em tudo | Expor comportamento, não campos |
| Pasta por tipo técnico, uma alteração toca 6 pastas | Agrupar por funcionalidade (se a Arquitetura permitir) |
| Dependência circular entre pacotes | Extrair interface/conceito comum |
| Nome no idioma errado/misturado | Seguir a convenção do projeto (pt-BR no domínio só se `estrutura.md` define; código em inglês é comum) |
