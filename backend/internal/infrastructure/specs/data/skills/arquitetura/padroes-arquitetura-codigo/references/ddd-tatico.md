# DDD tático: só onde há domínio rico

Domain-Driven Design tem uma parte estratégica (linguagem ubíqua, contextos delimitados) e
uma tática (entidades, objetos de valor, agregados, serviços de domínio, eventos). Esta
reference trata da **tática**. Regra geral: **usar o mínimo, e só na fatia que tem
complexidade de regra**. CRUD com validações não é domínio rico.

## O que é domínio rico (critérios)

Aplique DDD tático numa fatia se **três ou mais** forem verdadeiros:

- [ ] Há **estados e transições** com regras (ex.: pedido: criado, pago, enviado, cancelado).
- [ ] Há **invariantes que envolvem vários objetos** e precisam valer juntos.
- [ ] Regras mudam com frequência e **especialistas de negócio** as descrevem em termos próprios.
- [ ] A mesma regra é usada por vários casos de uso.
- [ ] Cálculos ou políticas (preço, prazo, elegibilidade) com variações.
- [ ] Linguagem do negócio tem termos que o código deve espelhar.

Reserva de salas simples (criar, listar, cancelar, sem sobreposição): **não é**; basta
função de serviço + constraint. Um sistema de reservas com política de cancelamento por
antecedência, recorrência, prioridade por cargo e cotas: **seria**.

## Linguagem ubíqua (o que sempre vale, mesmo sem DDD tático)

Use no código os termos do glossário da Requisitos (`Sala`, `Reserva`, `Cancelar`), sem
sinônimos. É grátis e é a parte de DDD que mais evita erro de entendimento.

## Blocos táticos: quando usar cada um

| Bloco | O que é | Use quando | Evite quando |
|---|---|---|---|
| Entidade | Objeto com identidade e ciclo de vida | Tem estado que muda e regras de transição | É só linha de tabela sem comportamento |
| Objeto de valor | Imutável, comparado por valor | Carrega validação/comportamento (intervalo de tempo, dinheiro, e-mail validado) | É `string` sem regra |
| Agregado | Grupo coeso com uma raiz que protege invariantes | Invariantes entre objetos filhos | Objetos independentes |
| Serviço de domínio | Regra que não cabe em uma entidade | Política envolvendo vários agregados | Repassa chamadas |
| Evento de domínio | Fato ocorrido (`ReservaCancelada`) | Outros efeitos desacoplados são necessários | Só para "ficar elegante" |
| Repositório (do agregado) | Persistência por raiz | Agregado complexo | CRUD simples: consultas diretas bastam |
| Fábrica | Criação com regra | Construção complexa | Construtor resolve |

## Exemplo mínimo: objeto de valor com regra

```text
Intervalo(inicio, fim)
  - invariante: fim > inicio
  - sobrepoe(outro) -> bool
  - duracao() -> minutos
```

Vale a pena porque a regra "fim > início" e o cálculo de sobreposição aparecem em vários
lugares. Em Go/Rust/Java, um tipo pequeno; em Python, uma `dataclass(frozen=True)`.

## Exemplo: entidade com transição

```text
Reserva
  estado: ATIVA | CANCELADA
  cancelar(agora):
      se estado = CANCELADA -> erro "já cancelada"
      se inicio - agora < 2h -> erro "fora do prazo de cancelamento"
      estado = CANCELADA
```

A regra de transição vive **na entidade** (ou função pura sobre ela), não no controller. Teste
unitário sem banco cobre cada ramo.

## Agregado: regra de bolso

- Agregado **pequeno**: uma raiz e poucos filhos. Se tiver listas grandes, repensar.
- Referenciar outro agregado **por id**, não por objeto.
- Uma transação altera **um** agregado; consistência entre agregados é eventual ou
  via transação explícita e consciente.
- Invariante concorrente continua sendo constraint no banco (ex.: sobreposição). O agregado
  organiza a regra; **o banco a garante sob concorrência**.

## DDD estratégico em uma linha

Se o sistema crescer a ponto de ter áreas com vocabulários diferentes (o que "cliente" quer
dizer em Faturamento e em Cadastro), separe **contextos delimitados** como módulos
(`padroes-e-estruturas.md`, monolito modular). Para app pequeno: um contexto só.

## Estrutura mínima com DDD tático numa fatia

```
pedidos/
  dominio/        pedido (entidade), item, dinheiro (VO), politica_desconto (serviço de domínio)
  servico         orquestra: carrega, chama regra, persiste
  handler         HTTP
  store           persistência
```

Somente a fatia rica ganha `dominio/`; as demais continuam simples. Mistura é aceitável e
desejável.

## Anti-DDD (cerimônia sem retorno)

| Sintoma | Correção |
|---|---|
| Agregado para toda tabela | Só onde há invariante entre objetos |
| Evento de domínio com barramento para uma ação síncrona | Chamada direta |
| Objetos de valor para todo campo | Só os com regra |
| Repositório com interface e mapeador de domínio para CRUD | Consulta direta |
| Entidade rica no domínio **e** modelo anêmico duplicado na persistência | Um modelo, ou mapeamento justificado |
| Camadas "application", "domain", "infrastructure" em app de 8 US | Por funcionalidade |

## Como registrar

ADR curta: "Aplicar DDD tático na fatia X", citando quais critérios de domínio rico se
cumprem (números), e "não aplicar nas demais". Gatilho: se outra fatia passar a cumprir 3
critérios, abrir ADR.

## Checklist

- [ ] Critérios de domínio rico avaliados e anotados (3 ou mais).
- [ ] Linguagem ubíqua aplicada: termos do glossário no código.
- [ ] Regra de transição em entidade/função pura, com teste sem I/O.
- [ ] Objetos de valor só com regra real.
- [ ] Agregados pequenos, referências por id.
- [ ] Invariantes concorrentes também no banco.
- [ ] Demais fatias seguem o padrão simples.

## Erros comuns

| Erro | Correção |
|---|---|
| "Vamos usar DDD" no projeto todo | Fatia a fatia, com critérios |
| Entidade como saco de getters | Mover regra para ela ou usar modelo de dados simples |
| Agregado gigante | Quebrar; referenciar por id |
| Confiar só no agregado para concorrência | Constraint/lock no banco |
| Eventos para tudo | Eventos só quando há consumidor desacoplado real |
| Nomes técnicos no domínio (`Manager`, `Handler`) | Termos do negócio |
