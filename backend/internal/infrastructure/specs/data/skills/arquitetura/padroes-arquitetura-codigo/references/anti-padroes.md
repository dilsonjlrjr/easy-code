# Anti-padrões de organização de código

Para revisão de estrutura e de código. Cada item: como reconhecer, por que dói, correção
mínima. Em app pequeno, quase todos têm a mesma origem: **estrutura de sistema grande
aplicada a problema pequeno**.

## 1. Camadas anêmicas (passa-prato)

**Sinal:** o serviço só chama o repositório com os mesmos argumentos; o controller só chama
o serviço; nenhuma camada tem regra.

```
controller.criar(x) -> service.criar(x) -> repo.criar(x)
```

**Dói:** três arquivos para uma linha de lógica; ninguém sabe onde pôr a regra.
**Correção:** fundir camadas sem papel; manter o serviço **apenas** se há regra (validação
de negócio, orquestração, tradução de erro). CRUD puro pode ir do handler à consulta, com
a validação no handler.

## 2. Abstração prematura

**Sinal:** classes base genéricas, `BaseService<T>`, fábricas de fábricas, "framework interno",
parâmetros de configuração que ninguém usa.
**Dói:** o próximo caso real nunca encaixa; complexidade fica para sempre.
**Correção:** regra dos três: abstraia na **terceira** repetição, não na primeira. Duplicar
duas vezes é mais barato do que a abstração errada.

## 3. Interface para tudo

**Sinal:** `IReservaService` com uma única `ReservaServiceImpl`; interface de repositório
sem segunda implementação nem dublê.
**Dói:** dobra os arquivos; navegar exige salto extra; refatoração duplica trabalho.
**Correção:** interface só em fronteira real: I/O externo (e-mail, relógio, pagamento),
ou quando há 2 implementações. Teste de repositório contra PostgreSQL real dispensa interface.

## 4. DTO explosion

**Sinal:** para uma entidade existem `ReservaEntity`, `ReservaModel`, `ReservaDTO`,
`ReservaRequest`, `ReservaResponse`, `ReservaView`, mais mapeadores entre todos.
**Dói:** campo novo exige mexer em 6 arquivos; mapeamento esconde bugs.
**Correção:** um modelo por fatia; entrada (`NovaReserva`) e saída (`Reserva`) quando a forma
difere; DTO extra só para proteger campos sensíveis (senha, papel). Tipos gerados do
OpenAPI valem como DTO de borda, sem camada intermediária.

## 5. Regra de negócio no lugar errado

| Onde está | Problema | Onde deve estar |
|---|---|---|
| No controller/handler | Difícil testar sem HTTP | Serviço da fatia |
| No front só | Qualquer cliente HTTP ignora | API; front só espelha |
| Só na aplicação, e é invariante concorrente | Condição de corrida | Constraint no banco |
| Em trigger de banco escondido | Surpresa e difícil de testar | Constraint declarativa; lógica no serviço |
| Em `comum/` | Fatia sem dono | Fatia dona da regra |

## 6. Pasta por tipo que cresce

**Sinal:** `services/` com 40 arquivos; mudar uma funcionalidade toca 5 pastas.
**Correção:** por funcionalidade (ver `padroes-e-estruturas.md`).

## 7. Camada de domínio sem domínio

**Sinal:** pasta `domain/` com classes que são só dados com getters; "agregados" sem
invariantes; `ValueObject` para tudo (`Nome`, `Idade`) sem regra.
**Correção:** modelo simples de dados; objetos de valor só quando carregam validação ou
comportamento (intervalo de tempo, dinheiro). Ver `ddd-tatico.md`.

## 8. Repositório genérico com `findAll()` e filtros em memória

**Sinal:** carregar tudo e filtrar no código; `repo.findAll().stream().filter(...)`.
**Dói:** desempenho e memória; ignora índices.
**Correção:** consulta específica com `WHERE`, índice e paginação (skill `modelagem-postgresql`).

## 9. Acoplamento entre fatias pelo banco

**Sinal:** `reservas` faz `JOIN` e `UPDATE` em tabelas de `salas`.
**Dói:** mudança em uma quebra a outra; não há fronteira.
**Correção:** `reservas` pergunta a `salas` via serviço; `JOIN` somente de leitura em consultas
de relatório explícitas, documentadas.

## 10. Injeção de dependência por mágica e globais

**Sinal:** singletons globais, estado mutável compartilhado, dependências resolvidas por
reflexão difícil de seguir.
**Correção:** construtores explícitos; em frameworks com DI, injeção por construtor.

## 11. Configuração espalhada

**Sinal:** `os.getenv` espalhado pelo código; valor padrão diferente em cada lugar.
**Correção:** um módulo `config` que lê, valida e expõe; falha na subida se faltar algo obrigatório.

## 12. Tratamento de erro inconsistente

**Sinal:** cada handler monta sua resposta de erro; exceção genérica engolida.
**Correção:** erros de domínio tipados + tradutor central para Problem Details (`arquitetura-web`).

## 13. Testes acoplados à estrutura

**Sinal:** teste mocka cinco camadas e quebra a cada refatoração; teste que só repete a implementação.
**Correção:** testar comportamento: regra com unidade pura; fluxo com teste de API + banco real.

## 14. Microsserviços (ou "serviços") por moda

**Sinal:** vários processos para 8 US; chamadas de rede entre componentes do mesmo domínio.
**Dói:** consistência distribuída, deploy e custo multiplicados.
**Correção:** monolito (modular se preciso). Serviço separado só por escala, equipe ou
isolamento de falha com requisito explícito.

## 15. Padrão como identidade

**Sinal:** "somos hexagonais" como objetivo; revisor reprova código por não seguir o padrão
mesmo quando a simplicidade resolveria.
**Correção:** padrão é meio. Pergunta de revisão: "que problema isto resolve aqui?"

## Perguntas de revisão (rápidas)

1. Se eu apagar esta camada/interface/DTO, algo quebra de verdade?
2. A regra pode ser testada sem subir web e banco? (Se sim, ótimo; se não, por quê?)
3. Uma funcionalidade nova entra em uma pasta ou em cinco?
4. Há dependência apontando de domínio para framework?
5. Quantos arquivos tocar para adicionar um campo? (alvo: 3 a 5 no máximo, incluindo migração e teste)

## Checklist

- [ ] Nenhuma camada só de repasse.
- [ ] Nenhuma interface com implementação única sem fronteira real.
- [ ] Entidade/modelo/DTO: no máximo dois tipos por fatia na maioria dos casos.
- [ ] Regra de negócio no serviço (ou no banco, se invariante concorrente).
- [ ] Sem `findAll` + filtro em memória.
- [ ] Fatias independentes, interação por serviço.
- [ ] Configuração e erros centralizados.

## Erros comuns na revisão

| Erro | Correção |
|---|---|
| Exigir abstração "por boa prática" | Pedir o problema concreto |
| Remover tudo de uma vez | Uma abstração por vez, com testes verdes |
| Confundir DRY com abstração precoce | Duplicação tolerável até a 3ª ocorrência |
