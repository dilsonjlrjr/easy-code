# Padrões e estruturas de pastas

Cada padrão: ideia, quando serve, quando **não** usar, estrutura. Árvores em mais de uma
linguagem; nomes idiomáticos ficam para as skills de `linguagens/`.

## 1. Camadas (layered)

**Ideia:** `controller -> serviço -> repositório -> banco`. Cada camada só fala com a de baixo.

**Serve:** CRUD simples, equipe pequena, sem regra rica.
**Evite quando:** a pasta por tipo (`controllers/`, `services/`, `repositories/`) cresce e cada
mudança toca todas; ou quando o serviço vira só repasse (camada anêmica).
**Variante útil:** camadas **dentro** de cada fatia (próxima seção), não no projeto inteiro.

```
src/
  controllers/  reservas.*  salas.*
  services/     reservas.*  salas.*
  repositories/ reservas.*  salas.*
```

## 2. Por funcionalidade (vertical slice) — recomendado

**Ideia:** cada capacidade do negócio é uma pasta com tudo dela (rota, regra, dados, testes).
O que muda junto fica junto; remover uma funcionalidade é apagar uma pasta.

**Serve:** app pequeno e médio; combina com T em fatias verticais e escopos de arquivos
disjuntos (`backend/src/**/reservas/**`), o que habilita paralelismo seguro.
**Evite quando:** muitas fatias compartilham a mesma regra complexa (extrair módulo comum
com fronteira clara).

Regras:
- Fatia expõe um serviço; outras fatias usam **só** isso.
- Compartilhado (`comum/`) contém infraestrutura: erros, config, conexão, autenticação, não regra de negócio.
- Dentro da fatia, camadas mínimas: handler, serviço, consultas, tipos.

### Java (Spring, exemplo ilustrativo)

```
backend/src/main/java/app/
  Application.java
  reservas/   ReservaController  ReservaService  ReservaRepository  Reserva  NovaReserva
  salas/      SalaController     SalaService     SalaRepository     Sala
  autenticacao/  ...
  comum/      ErroHandler  ProblemDetails  Config
backend/src/main/resources/db/migration/   V001__base.sql ...
backend/src/test/java/app/reservas/        ReservaServiceTest  ReservaApiIT
```

### Go

```
backend/
  cmd/api/main.go
  internal/
    reservas/  handler.go  service.go  store.go  tipos.go  service_test.go
    salas/     handler.go  service.go  store.go
    comum/     erros.go  config.go  db.go  auth.go
  migrations/  001_base.sql
```

### Python (FastAPI ou similar)

```
backend/
  app/
    main.py
    reservas/  rotas.py  servico.py  consultas.py  modelos.py
    salas/     rotas.py  servico.py  consultas.py  modelos.py
    comum/     erros.py  config.py  db.py  auth.py
  migrations/
  tests/reservas/  tests/salas/
```

### Rust (Axum ou similar)

```
backend/
  src/
    main.rs
    reservas/  mod.rs  handler.rs  service.rs  store.rs
    salas/     mod.rs  handler.rs  service.rs  store.rs
    comum/     erro.rs  config.rs  db.rs  auth.rs
  migrations/
```

### Front Vue (por funcionalidade)

```
frontend/src/
  features/
    reservas/  ReservasPage.vue  ReservaForm.vue  useReservas.ts  api.ts  tipos.ts
    salas/     SalasPage.vue  useSalas.ts  api.ts
    login/
  shared/      http.ts  componentes/  utils/
  router/  App.vue  main.ts
```

Sem camada de "domínio" no front pequeno: componentes + composables + um módulo `api` por fatia.

## 3. Hexagonal (portas e adaptadores)

**Ideia:** o núcleo (domínio + casos de uso) define **portas** (interfaces de entrada e
saída); **adaptadores** implementam (HTTP, banco, e-mail). O núcleo não conhece tecnologia.

**Serve:** várias integrações, regra de negócio rica, necessidade real de testar o núcleo
sem I/O em escala, previsão concreta de trocar um adaptador.
**Evite quando:** o "núcleo" é um CRUD, há 1 adaptador de cada tipo e nenhuma troca à vista.
O custo é interface + adaptador + mapeamento por dependência.

```
src/
  dominio/            entidades, regras puras, erros de domínio
  aplicacao/          casos de uso; portas (interfaces) de saída
  adaptadores/
    entrada/http/     handlers
    saida/postgres/   implementa repositórios
    saida/email/      implementa notificador
  main                 compõe tudo (injeção de dependência manual)
```

**Híbrido pragmático (recomendado quando há 1–2 integrações):** manter por funcionalidade e
criar porta **apenas** para a integração externa:

```
reservas/ handler  service  store  tipos
notificacoes/ porta (interface)  adaptador_email  adaptador_falso(teste)
```

## 4. Clean architecture

**Ideia:** anéis concêntricos (entidades, casos de uso, adaptadores de interface,
frameworks); dependência só para dentro. Parente próximo do hexagonal, com casos de uso
explícitos como unidade.

**Serve:** domínio grande, muitos anos de vida, vários times, regras estáveis que devem
sobreviver a trocas de framework.
**Evite quando:** o produto cabe em alguns arquivos por fatia. Em apps pequenos produz
pastas quase vazias e muito código de passagem.

```
src/
  entities/       regras centrais
  usecases/       um por intenção (CriarReserva, CancelarReserva)
  interfaces/     controllers, presenters, gateways (interfaces)
  infrastructure/ web, banco, e-mail (implementações)
```

## 5. Monolito modular

**Ideia:** um deploy, vários **módulos** com fronteira forte (API pública do módulo,
dados do módulo, sem acesso ao interior de outro). Pode usar internamente qualquer padrão
acima. É o caminho recomendado antes de cogitar microsserviços.

**Serve:** sistema médio/grande, várias equipes, domínios distintos (faturamento, reservas,
cadastro).
**Evite quando:** há um único domínio pequeno: seria por funcionalidade com outro nome.

```
modulos/
  reservas/  api (pública)  interno/...  migrations/
  cadastro/  api            interno/...
  faturamento/ ...
comum/
```

Regras: módulo A usa B só pela `api`; esquema/tabelas de cada módulo são "dele"; checar
fronteiras com ferramenta de arquitetura ou teste (por exemplo regras de dependência
automatizadas na linguagem).

## Injeção de dependência: o mínimo

- Preferir **montar à mão** em `main` (construtores recebem dependências).
- Container de DI/framework só se a linguagem/framework o trazem como padrão (Spring) e é o
  jeito idiomático.
- Interface só na fronteira que realmente precisa de substituto (e-mail, relógio, serviço externo).

## Resumo de escolha

| Se... | Então |
|---|---|
| Poucas US, CRUD, regra pontual | Por funcionalidade (padrão da skill) |
| Uma integração externa relevante | Por funcionalidade + porta só para ela |
| Várias integrações + regra rica | Hexagonal (parcial ou completo) com ADR |
| Domínio enorme e longevo, vários times | Clean/hexagonal completo dentro de monolito modular |
| Partes independentes de equipe | Monolito modular |

## Erros comuns

| Erro | Correção |
|---|---|
| Copiar a árvore do livro sem os critérios | Aplicar tabela de decisão |
| Camadas de pasta por tipo no projeto inteiro | Fatias, com camadas dentro |
| Porta para o banco "para trocar um dia" | Assumir PostgreSQL |
| `comum/` com regra de negócio | Mover para a fatia dona |
| Fatia lendo tabela de outra fatia | Chamar o serviço da outra |
| Mapear entidade → DTO → modelo → resposta sem necessidade | Um modelo por fatia |
