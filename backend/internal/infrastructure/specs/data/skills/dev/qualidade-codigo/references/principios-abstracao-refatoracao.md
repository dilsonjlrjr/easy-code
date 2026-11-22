# Princípios na medida certa, abstração, comentários e refatoração

## SOLID: quando aplicar e quando é exagero

Use como **diagnóstico quando dói**, não como cerimônia prévia.

| Princípio | Ideia curta | Aplique quando | É exagero quando |
|---|---|---|---|
| **S** Responsabilidade única | Um motivo para mudar | Classe/módulo muda por razões diferentes (regra + SQL + e-mail juntos) | Dividir uma função de 15 linhas em 5 classes |
| **O** Aberto/fechado | Estender sem editar | Há **variações reais** e crescentes (3 formas de notificar) | `Strategy` para um `if` que nunca vai ter terceiro caso |
| **L** Substituição de Liskov | Subtipo cumpre o contrato do tipo | Há herança/interface com mais de uma implementação | Hierarquias de herança por reuso de código (prefira composição) |
| **I** Segregação de interfaces | Interface pequena e do cliente | Cliente é forçado a depender do que não usa | Interface de 1 método por classe "porque sim" |
| **D** Inversão de dependência | Regra não depende de detalhe | Há **borda externa** (banco, e-mail, relógio) a trocar/dublar | Interface para cada classe interna com um só implementador |

Heurística: se o ganho concreto é "poder testar sem rede" ou "poder trocar o terceiro", é
inversão que se paga. Se o ganho é "pode ser que um dia", não é.

### Outros princípios úteis

- **KISS:** a solução mais simples que cumpre o critério.
- **YAGNI:** não construa o que ninguém pediu. Funcionalidade "para o futuro" tem custo hoje.
- **DRY, corretamente:** não repita **conhecimento** (uma regra em dois lugares). Repetir
  *código parecido* com razões de mudar diferentes não é violação.
- **Lei de Demeter, leve:** evite cadeia `a.b().c().d()`; peça ao colega próximo.
- **Composição sobre herança:** herança só para "é um" verdadeiro e estável.
- **Tell, don't ask** com moderação: o objeto decide em vez de expor campos para fora decidirem.

## Duplicação versus abstração prematura

Abstração errada custa mais que duplicação: espalha-se, ganha flags e ninguém ousa mexer.

### Regra de três

| Ocorrências | Ação |
|---|---|
| 1 | Escreva direto |
| 2 | **Tolere a duplicação**; anote mentalmente |
| 3 com a **mesma razão de mudar** | Extraia, com o nome do conceito |
| 3 parecidos mas com razões diferentes | **Não extraia** (coincidência) |

### Sinais de abstração ruim

- A função extraída ganha parâmetros booleanos/modos para atender chamadores diferentes.
- O nome é vago (`processar`, `tratarGenerico`).
- Mudar para um chamador exige "um `if` a mais" para os outros.

Quando acontece: **desfaça** (inline) e extraia de novo com a forma certa. Remover abstração
é refatoração legítima.

### Dá para duplicar de propósito?

Sim: entre **módulos/serviços independentes**, entre código de teste e de produção, e entre
camadas (DTO de API e entidade de domínio parecem iguais mas mudam por razões diferentes).

## Comentários: só o porquê

| Comentar | Não comentar |
|---|---|
| **Por que** uma decisão não óbvia (restrição de terceiro, bug de biblioteca, regra de negócio contraintuitiva) | O que o código faz linha a linha |
| Link/ID da ADR, US ou ACH que justifica | Histórico ("alterado por fulano em 2026") (use git) |
| Contrato de API pública (docstring/JavaDoc/godoc) com semântica e erros | Código comentado (apague; o git guarda) |
| Aviso de armadilha (ordem importa, thread-safety) | `TODO` sem dono; se é dívida, vire ACH |

```go
// Fim é exclusivo: 9h-10h e 10h-11h não conflitam (US-03, CA-2).
func (a Intervalo) Sobrepoe(b Intervalo) bool { return a.Inicio.Before(b.Fim) && b.Inicio.Before(a.Fim) }
```

Se precisa de comentário para explicar o **quê**, renomeie ou extraia. Comentário que mente é
pior que ausência: mudou o código, mude ou apague o comentário.

## Refatoração segura com testes

Refatorar = **mudar a estrutura sem mudar o comportamento**. Regras:

1. **Teste verde antes.** Sem teste, escreva o de caracterização primeiro (ver
   `estrategia-testes`, `tdd-e-unitarios`).
2. **Passos pequenos**, rodando os testes a cada um. Quebrou? Volte um passo.
3. **Não misture** refatoração e mudança de comportamento no mesmo commit.
4. Refatore **no escopo da T**. Bagunça fora do escopo: ACH de dívida técnica.
5. Use as ferramentas da IDE/linguagem para renomear e mover (mais seguro que busca e troca);
   o compilador e `vue-tsc`/mypy/clippy ajudam a achar o que quebrou.
6. Termina quando lint e testes estão verdes; o diff deve ser **revisável** em minutos.

### Catálogo curto dos refactorings mais úteis

| Refactoring | Quando | Como |
|---|---|---|
| **Renomear** | Nome não diz a intenção | Renomear com a ferramenta; atualizar docs/testes |
| **Extrair função** | Trecho com nome possível; bloco comentado; função longa | Mover para função com nome da intenção; passar o que usa |
| **Inline** | Função/variável que só repete o nome do que faz | Substituir pelo corpo |
| **Extrair variável explicativa** | Condição/expressão obscura | `var horarioUtil = ...; if (horarioUtil && salaLivre)` |
| **Substituir condicional por retorno antecipado** | Aninhamento profundo | Inverter, retornar/lançar cedo |
| **Introduzir objeto de parâmetro** | Muitos parâmetros que viajam juntos | `NovaReserva{...}` |
| **Substituir primitivo por tipo** | String/int com regra (`Email`, `Duracao`) | Tipo com construtor validante |
| **Mover função** | Função usa mais dados de outro módulo | Mover para onde estão os dados |
| **Extrair módulo/classe** | Responsabilidades misturadas | Mover as funções coesas; manter API antiga delegando, depois remover |
| **Substituir número/string mágica por constante/enum** | Literal repetido ou sem significado | Nome do conceito |
| **Separar consulta de comando** | Função que lê e altera | Duas funções |
| **Decompor condicional / tabela** | `if`/`switch` por tipo crescente | Mapa de comportamento só se houver 3+ casos reais |
| **Remover código morto** | Não chamado, flag antiga | Apagar (o git guarda) |

### Exemplo: extrair + retorno antecipado + tipo

```typescript
// antes
function reservar(s: string, i: string, f: string, u: string) {
  if (s && i && f) {
    if (new Date(f) > new Date(i)) {
      // ... gravar
    }
  }
}
// depois: validação na borda produziu tipos de domínio
function reservar(cmd: NovaReserva, usuario: UsuarioId): Promise<Reserva> {
  /* regra com Intervalo já válido; sem checagens de string aqui */
}
```

### Quando NÃO refatorar

- Perto do fim da T sem testes que protejam a mudança.
- Código que vai ser removido/substituído em breve.
- Só por gosto de estilo contra a convenção do projeto.
- Fora do escopo de arquivos da T (abra ACH).

## Erros comuns

| Erro | Efeito | Correção |
|---|---|---|
| Aplicar padrão de projeto "porque é boa prática" | Código maior sem ganho | Só quando a dor aparece |
| Interface + implementação + factory para uma classe | Cerimônia | Classe/função concreta |
| Herança para reaproveitar código | Acoplamento rígido | Composição |
| Extrair na segunda repetição | Abstração errada | Esperar a terceira com a mesma razão |
| Comentar o "o quê" | Desatualiza | Renomear/extrair |
| Refatoração gigante no mesmo commit da feature | Diff ilegível, bug escondido | Commits separados |
| Refatorar sem rede de testes | Quebra silenciosa | Caracterização primeiro |
| "Limpar" além do escopo | Conflito, risco, estouro de T | ACH de dívida técnica |
