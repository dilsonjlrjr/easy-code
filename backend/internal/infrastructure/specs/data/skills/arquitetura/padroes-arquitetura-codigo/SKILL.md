---
name: padroes-arquitetura-codigo
description: Guia de decisão para organizar o código de uma aplicação web (backend e front) - camadas, por funcionalidade (vertical slice), hexagonal (portas e adaptadores), clean architecture e monolito modular - com critérios objetivos de escolha, padrão recomendado para app pequeno, sinais de evolução, anti-padrões e DDD tático só onde há domínio rico. Use na etapa de Arquitetura ao decidir a estrutura de pastas e na etapa de Dev ao implementar conforme essa decisão; sempre com a pergunta "isso é necessário para este tamanho de app?".
---

# Padrões de arquitetura de código: guia de decisão

Skill das etapas de **Arquitetura** e **Dev**. Não é um catálogo de padrões: é um guia para
**escolher o mais simples que serve** e saber quando evoluir. Vale para qualquer aplicação web
de backend + front; os exemplos usam reserva de salas apenas como ilustração. Detalhes
idiomáticos de cada linguagem ficam nas skills de `linguagens/`.

## Posição da skill (opinativa)

Para um app de 5 a 10 histórias, **hexagonal e clean completos são exagero**: multiplicam
interfaces, mapeadores e pastas sem trazer ganho, e cada arquivo extra é custo de geração,
teste e revisão do agente (custo conta: orçamento do projeto e custo por etapa são indicadores da plataforma). O padrão
recomendado é **organização por funcionalidade, com camadas internas mínimas e a regra de negócio isolada do transporte**. Evoluir só quando um sinal objetivo aparecer.

## Princípios

1. **Proporcionalidade.** A estrutura cresce com a complexidade do problema, não com a
   ambição do livro. Estrutura simples que cabe na cabeça de quem lê.
2. **Abstração tem de pagar o aluguel.** Interface, camada ou DTO extra só entra se resolver
   um problema presente (testar, trocar, isolar), não um hipotético.
3. **Dependências apontam para a regra de negócio.** A regra não importa framework web nem
   driver de banco. Isso vale em qualquer padrão e é barato.
4. **Coesão por assunto, não por tipo de arquivo.** Código que muda junto fica junto.
5. **Decisão revisável.** Registrada em ADR (artefato com cabeçalho YAML; ver `arquitetura-web`) com gatilho de evolução.
6. **Teste guia o limite.** Se testar a regra exige subir web e banco, o isolamento falhou;
   se exige 5 mocks, a abstração está errada.

## Os padrões em uma tabela

| Padrão | Ideia | Custo | Quando serve |
|---|---|---|---|
| Camadas (controller, serviço, repositório) | Organiza por papel técnico | Baixo | CRUD simples, equipe pequena |
| **Por funcionalidade (vertical slice)** | Cada fatia (reservas, salas) com tudo dela | Baixo | **App pequeno/médio: recomendado** |
| Hexagonal (portas e adaptadores) | Domínio no centro; entrada/saída por portas | Médio | Várias integrações, regra rica, trocas previstas |
| Clean architecture | Hexagonal com anéis, casos de uso explícitos | Alto | Domínio grande, longa vida, muitos times |
| Monolito modular | Módulos com fronteiras fortes no mesmo deploy | Médio | Sistema médio/grande, pré-requisito para dividir depois |

Definições, estruturas e exemplos em `references/padroes-e-estruturas.md`.

## Regras de decisão (critérios objetivos)

| Critério | Pequeno (padrão simples) | Pede mais estrutura |
|---|---|---|
| Nº de histórias Must | até ~15 | mais de ~30 |
| Regras de domínio | validações e 1–2 invariantes | muitas regras que interagem, estados, políticas |
| Integrações externas | 0–1 | 3 ou mais, ou previsão de troca |
| Vida útil | protótipo, 1–2 anos | muitos anos, equipe rotativa |
| Equipe | 1–3 pessoas/agentes | vários times no mesmo código |
| Requisito de teste | testes de API + unidade na regra | regra testada sem I/O em larga escala |
| Escala de deploy | um deploy | partes com ritmo ou carga muito diferentes |

Regra prática de pontuação: conte quantos critérios da coluna direita se aplicam.
**0–1:** por funcionalidade com camadas mínimas. **2–3:** adicionar portas só onde há
integração/regra rica (hexagonal parcial). **4 ou mais:** considerar hexagonal/clean
completo ou monolito modular, com ADR.

| Situação | Decisão |
|---|---|
| CRUD com poucas regras | Por funcionalidade; serviço fino ou lógica no handler |
| Regra crítica (ex.: conflito de horário) mas só 1 | Função/serviço da fatia; invariante no banco; sem hexagonal |
| Segunda integração externa chegando (e-mail, pagamento) | Porta/interface **só para ela**, adaptador isolado |
| Quer trocar de banco "no futuro" | Não criar abstração por isso; PostgreSQL é decisão assumida |
| Quer "testabilidade" | Teste de integração com banco real em contêiner costuma bastar; interface só onde há I/O lento ou externo |
| Front Vue pequeno | Por funcionalidade (`features/`), componentes e composables; sem camadas de domínio |
| Equipe pede "clean architecture" por padrão | Pedir o problema concreto que resolve; sem problema, não adotar |
| Dois módulos acoplados por detalhes internos | Fronteira explícita (API do módulo), sem sair do monólito |

## Padrão recomendado para app pequeno

```
<fatia>/                 ex.: reservas/, salas/, autenticacao/
  rotas / handlers       HTTP: valida entrada, chama o serviço, mapeia erro para status
  serviço                regra de negócio da fatia (funções puras quando possível)
  repositório / consultas  SQL ou acesso a dados da fatia
  tipos / modelos        estruturas da fatia (entidade, entrada, saída)
compartilhado/           erros (Problem Details), configuração, banco, autenticação
```

- **Fatias** independentes; uma fatia chama outra só pelo serviço dela, nunca pelo repositório.
- **Serviço** não conhece HTTP; **handler** não contém regra.
- **Repositório** sem interface própria até haver 2ª implementação ou necessidade real de dublê.
- **Um modelo por fatia**; DTO separado só quando a forma da API diverge de forma relevante
  do modelo interno (senha, campos calculados).
- **Banco garante invariantes** (constraints), serviço traduz erro.
- Estrutura por linguagem em `references/padroes-e-estruturas.md`.

## Fluxo de trabalho

1. Levantar os critérios da tabela (nº de US, regras, integrações, vida útil, equipe).
2. Contar quantos pedem mais estrutura; aplicar a regra de pontuação.
3. Escolher o padrão **mais simples** que cobre os critérios; registrar ADR com alternativa
   mais simples e mais pesada, e **gatilhos de evolução**.
4. Descrever a árvore de pastas e as regras de dependência em `estrutura.md`.
5. Durante o Dev, respeitar escopo por fatia; ao sentir atrito, checar sinais de evolução
   (`references/evolucao-e-adr.md`) em vez de improvisar abstração.
6. Revisar contra os anti-padrões (`references/anti-padroes.md`).

## Checklist

- [ ] Critérios levantados com números (US, regras, integrações, vida útil, equipe).
- [ ] Padrão escolhido é o mais simples que atende; justificativa em ADR.
- [ ] ADR cita alternativa mais simples e mais complexa, e gatilhos de evolução.
- [ ] Regra de dependência declarada (regra não importa web nem banco).
- [ ] Nenhuma interface com uma única implementação sem motivo registrado.
- [ ] Nenhuma camada que só repassa chamadas.
- [ ] DDD tático somente onde há domínio rico (`references/ddd-tatico.md`).
- [ ] Árvore de pastas em `estrutura.md` casa com as T (escopos por fatia).

## Erros comuns

| Erro | Efeito | Correção |
|---|---|---|
| Hexagonal completo para CRUD | Dobro de arquivos, nenhum ganho | Por funcionalidade |
| Interface para tudo (repositório, serviço, utilitário) | Ruído e navegação difícil | Interface onde há fronteira real |
| Camadas anêmicas (só repasse) | Código sem lugar para a regra | Fundir ou levar a regra ao serviço |
| DTO para cada conversão entre camadas | Explosão de tipos | Um modelo por fatia, DTO só na borda quando diverge |
| Regra de negócio no handler/controller | Difícil testar | Mover para serviço da fatia |
| Pasta por tipo (`controllers/`, `services/`) crescendo sem fim | Mudança toca 6 pastas | Pasta por fatia |
| Abstrair o banco "por segurança" | Esconde recursos úteis do PostgreSQL | Assumir o banco |
| DDD completo sem domínio rico | Cerimônia | Só o necessário (`ddd-tatico.md`) |
| Pular o ADR | Dev reinterpreta a cada tarefa | ADR + `estrutura.md` |
| Migrar de padrão no meio sem T | Refatoração sem rastro | T de refatoração com critério de pronto |

## Índice de references

| Arquivo | Quando ler |
|---|---|
| `references/padroes-e-estruturas.md` | Para entender cada padrão e ver árvores de pastas (Java, Go, Python, Rust, Vue) |
| `references/evolucao-e-adr.md` | Para saber quando evoluir e como registrar a escolha em ADR |
| `references/anti-padroes.md` | Em revisão de código/estrutura ou ao sentir atrito |
| `references/ddd-tatico.md` | Quando houver regras ricas (estados, invariantes que interagem) |

Skills relacionadas: `../arquitetura-web/` (ADR, tarefas), `../../dados/modelagem-postgresql/`
(invariantes no banco), `../../linguagens/` (idiomas de cada linguagem, quando existirem).
