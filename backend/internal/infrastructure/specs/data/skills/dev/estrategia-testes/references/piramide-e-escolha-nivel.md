# Pirâmide, troféu e escolha do nível

## Os dois modelos, sem dogma

| Modelo | Forma | Quando faz sentido |
|---|---|---|
| Pirâmide | Muito unitário, algum integração, pouco e2e | Domínio com regra rica (cálculo, estados, políticas) |
| Troféu | Pouco unitário, **integração é a maior camada**, pouco e2e | CRUD com pouca regra, BFF, front com componentes |
| Diamante/"ampulheta" | Muito e2e e muito unitário, nada no meio | Evitar: a falha do e2e não localiza e o unitário não prova a integração |

O que importa não é o desenho, é a pergunta: **qual o teste mais barato que me faz confiar
neste comportamento?** Teste barato é rápido, determinístico e localiza a falha.

### Custo de cada nível (ordem de grandeza)

| Nível | Tempo típico | Localiza falha | Frágil a refatoração | Prova integração |
|---|---|---|---|---|
| Unitário | milissegundos | linha/função | baixo se testa comportamento | não |
| Integração (API + banco real) | dezenas a centenas de ms | camada | baixo | sim (banco, HTTP) |
| Contrato | como integração | endpoint | baixo | contrato |
| e2e (navegador) | segundos | "em algum lugar" | alto | tudo |

## Como escolher o nível de um comportamento

Perguntas em ordem; pare na primeira resposta "sim".

1. A regra é função pura de entradas (sem banco, sem relógio, sem rede)? **Unitário.**
2. A correção depende de SQL, constraint, índice, transação ou mapeamento objeto-tabela?
   **Integração com banco real.**
3. O que vale é o contrato HTTP (status, cabeçalho, corpo, autenticação, erro)? **Integração
   da API** (e contrato contra o OpenAPI).
4. O comportamento só existe com navegador, rota, foco, JavaScript real? Componente: **teste de
   componente**. Jornada multi-tela: **e2e (QA)**.
5. Depende de duas requisições simultâneas? **Integração concorrente.**

### Exemplo: criar reserva (US "reservar sala sem conflito")

| Comportamento | Nível | Por quê |
|---|---|---|
| Fim deve ser depois do início; duração mínima de 15 min | Unitário | Regra pura sobre dois instantes |
| Reserva só em horário comercial do fuso da sala | Unitário (relógio/fuso injetados) | Regra pura |
| Sobreposição na mesma sala é recusada | Integração (constraint real) | A garantia está no banco |
| `POST /reservas` sobreposto retorna 409 e não grava | Integração da API | Contrato + estado final |
| Duas pessoas reservam o mesmo horário ao mesmo tempo: só uma vence | Integração concorrente | Corrida real |
| Usuário A não cancela reserva de B (403/404) | Integração da API | Autorização na borda |
| Usuário vê mensagem de conflito e o foco volta ao campo | Teste de componente + 1 e2e do QA | UI |

## Quando o e2e é a escolha certa

- Jornada crítica de ponta a ponta (login, reservar, ver na agenda), 3 a 8 em aplicação pequena.
- Algo que só o navegador real prova: roteamento com guarda, cookies, foco, CORS, build.
- **Não** para validar regra de campo, variações de entrada ou mensagens de erro: desça de nível.

Quem escreve e2e é o QA (skill `playwright-e2e`); o Dev não duplica. O Dev garante que cada
comportamento do critério de pronto da T esteja no nível baixo.

## Organização de testes

### Onde ficam

| Linguagem | Unitário | Integração |
|---|---|---|
| Java (Maven/Gradle) | `src/test/java` espelhando o pacote; sufixo `Test` | mesma árvore, sufixo `IT` ou pacote `integration`, separado por perfil/tarefa |
| Go | `*_test.go` ao lado do arquivo; pacote `x_test` para testar só a API pública | pasta `internal/.../integration` ou build tag `integration` |
| Rust | `#[cfg(test)] mod tests` no mesmo arquivo; `tests/` para integração | `tests/*.rs` (cada arquivo é um crate) |
| Python (pytest) | `tests/unit/` espelhando o pacote | `tests/integration/` com marker `integration` |
| TypeScript (Vitest) | `*.spec.ts` ao lado do arquivo | `tests/integration/` |

Separar unitário de integração por **comando**: `test` rápido (segundos) roda a todo commit;
`test:integration` sobe containers e roda antes de concluir a T e no CI.

### Nomes

O nome responde "o que, em que condição, com que resultado":

```text
reserva_sobreposta_na_mesma_sala_e_recusada
rejeita_reserva_com_fim_antes_do_inicio
cancelar_reserva_de_outro_usuario_retorna_403
```

```java
@Test void rejeitaReservaComFimAntesDoInicio() { ... }
```

```go
func TestCriarReserva_FimAntesDoInicio_RetornaErroDeValidacao(t *testing.T) { ... }
```

```rust
#[test]
fn rejeita_reserva_com_fim_antes_do_inicio() { /* ... */ }
```

```python
def test_rejeita_reserva_com_fim_antes_do_inicio(): ...
```

```ts
it('rejeita reserva com fim antes do início', () => { /* ... */ })
```

Evitar: `test1`, `testCriar`, `deveFuncionar`, nome do método sob teste repetido sem condição.

### Estrutura dentro do arquivo

- Um arquivo de teste por unidade/comportamento coeso, não por método.
- Agrupar por cenário (`describe`, subtestes `t.Run`, classes `@Nested`, módulos), não por
  tipo de asserção.
- Construtores de dado de teste (builders/fábricas) em um módulo de apoio, com valores
  default válidos e override do que o teste importa (ver `tdd-e-unitarios`).

## Rastreabilidade com os CT

- O CT do QA é a promessa; o teste do Dev é o primeiro a provar. Quando o CT já existe, cite o
  id no comentário do teste ou no nome do grupo (`// CT-07`), para o QA ligar sem adivinhar.
- Teste do Dev **não substitui** CT: o QA ainda confere no ambiente limpo e cobre o que o Dev
  não cobriu (e2e, não funcionais).
- O que o CT diz (nível) manda. Se o CT pede integração e o Dev só fez unitário, falta teste.

## Anti-padrões de forma

| Anti-padrão | Sintoma | Alternativa |
|---|---|---|
| Cone de sorvete | Muito e2e, pouco unitário; suíte lenta e instável | Descer cada verificação ao nível mais baixo |
| Pirâmide inversa de dublês | Unitários cheios de mock, zero integração | Integração com banco real nos pontos de risco |
| Teste de camada | Um teste para controller, outro para service, outro para repository, todos repetindo o mesmo caso | Teste o comportamento no nível onde ele é decidido |
| Testes "de cobertura" | Chamam o método sem afirmar | Apagar ou afirmar algo real |
| Suíte espelho da implementação | Mover método quebra 30 testes | Testar pela API pública do módulo |
