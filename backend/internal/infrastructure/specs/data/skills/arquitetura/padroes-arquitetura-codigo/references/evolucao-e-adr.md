# Quando evoluir e como registrar em ADR

Começar simples é correto; **ficar simples depois que o problema cresceu** também é erro.
Esta reference dá sinais objetivos de evolução, o caminho incremental e o modelo de ADR.

## Princípio

Evolua **por sintoma, não por antecipação**. Cada passo abaixo é pequeno, reversível e
feito como T própria de refatoração (com critério de pronto: testes antes e depois verdes).

## Sinais de que é hora de evoluir

| Sintoma observável | Diagnóstico | Evolução |
|---|---|---|
| Serviço da fatia > ~300–400 linhas e com várias razões para mudar | Fatia grande demais | Dividir a fatia em duas ou extrair subserviços |
| Mesma regra duplicada em 3 fatias | Regra compartilhada sem dono | Módulo `dominio/` comum com a regra (API pequena) |
| Testar uma regra exige subir banco/HTTP, e isso atrasa a suíte | Regra acoplada à infra | Extrair função pura; teste de unidade sem I/O |
| Terceira integração externa (e-mail, pagamento, ERP) | Integrações múltiplas | Portas e adaptadores para elas (hexagonal parcial) |
| Trocar/mocar um serviço externo em teste é doloroso | Falta de porta | Interface na fronteira, adaptador falso |
| Fatia A mexe em tabelas da fatia B | Fronteira vazando | API do módulo; dados "pertencem" a uma fatia |
| Estados e transições espalhados em `if` | Domínio rico emergindo | DDD tático local (`ddd-tatico.md`) |
| Mudanças simples tocam muitas pastas | Estrutura por tipo | Reorganizar por fatia |
| Dois times pisando no mesmo código | Acoplamento organizacional | Monolito modular com donos |
| Parte do sistema com carga/ritmo muito diferente | Escala desigual | Extrair módulo; só então considerar serviço separado |

Sinal **falso** (não evolui): "queremos nos preparar", "outro projeto fez assim", "é boa
prática". Sem sintoma, sem mudança.

## Caminho incremental

1. **Por funcionalidade com camadas mínimas** (ponto de partida).
2. **Funções puras de domínio** dentro da fatia, para regras testáveis sem I/O.
3. **Porta + adaptador só nas integrações** que justificaram.
4. **Módulo de domínio compartilhado** para regras reutilizadas.
5. **Monolito modular** com APIs de módulo e testes de fronteira.
6. **Hexagonal/clean completo** num módulo cujo domínio é de fato rico.
7. **Serviço separado** só com requisito de escala/equipe/implantação independente, via ADR forte.

Pular etapas é o caminho para o excesso de abstração; ir devagar é barato.

## Quanto custa evoluir depois

Uma estrutura por funcionalidade é fácil de evoluir: as fatias já são módulos em potencial,
e a regra isolada do transporte já é testável. Evoluir é **mover e extrair**, sem reescrever.
Já estrutura complexa demais é cara de **simplificar** (remover interfaces e camadas exige
tocar tudo). Assimetria que favorece começar simples.

## ADR da organização do código

Cabeçalho YAML (esquema `specs/dominio/artefatos/adr.schema.json`; vale o esquema):

```yaml
---
id: ADR-06
tipo: ADR
titulo: Organização do backend por funcionalidade
autor: architect-agent
onda: ARQ-01
versao: 1
estado: rascunho
vinculos: []
historico:
  - {versao: 1, data: "2026-11-19", onda: ARQ-01, descricao: criação}
contexto: 12 US (8 Must), 3 regras de negócio (1 crítica, garantida no banco), 0 integrações externas, equipe de 1 a 3 agentes/pessoas, vida útil esperada de 1 ano; RNF-04 limita custo de geração.
opcoes:
  - nome: A — Camadas globais (controllers/services/repositories)
    pros: [Familiar, Pouco a decidir]
    contras: [Cada mudança toca 3 pastas, Conflitos de escopo entre T]
  - nome: B — Por funcionalidade, camadas mínimas internas
    pros: [Fatias coesas, Escopos disjuntos por T, Pouca cerimônia]
    contras: [Exige disciplina de fronteira]
  - nome: C — Hexagonal completo
    pros: [Núcleo isolado, Trocas fáceis]
    contras: [2x a 3x arquivos, Sem integrações que justifiquem, Estoura o custo]
decisao: Usar a opção B; nenhum critério de "mais estrutura" se aplica além de uma regra crítica, tratada no banco.
consequencias:
  - Cada T tem escopo por fatia (**/reservas/**)
  - O serviço da fatia é a única porta de entrada de outras fatias
  - "Gatilhos de revisão: 3ª integração externa, regra duplicada em 3 fatias, serviço > 400 linhas ou mais de 30 US Must; qualquer um abre ADR nova"
skills: [padroes-arquitetura-codigo]
motivada_por: [US-03, RNF-04]
---
```

Pontos obrigatórios da ADR: números do contexto, opção mais simples **e** mais pesada, e
gatilhos de revisão concretos.

## T de refatoração para evoluir

A T de refatoração segue o esquema `t.schema.json`. O `cobre` exige ao menos uma US e um CA:
cite a US cujo comportamento a refatoração preserva (o esquema não prevê T sem US).

```yaml
---
id: T-09
tipo: T
titulo: Extrair porta de notificações
autor: architect-agent
onda: ARQ-01
versao: 1
estado: rascunho
vinculos: []
historico:
  - {versao: 1, data: "2026-11-19", onda: ARQ-01, descricao: criação}
cobre:
  - {us: US-03, criterios: [CA-1]}
segue: [ADR-06]
depende_de: []
escopo_arquivos:
  - backend/src/**/notificacoes/**
  - backend/src/**/reservas/service.*
tamanho: m
descricao: Refatoração sem US nova, motivada pela ADR de evolução; comportamento idêntico, notificação atrás de uma porta.
criterios_pronto:
  - comportamento idêntico, suíte existente passa sem mudança nos testes
  - adaptador falso usado nos testes de serviço
  - nenhuma fatia importa o adaptador diretamente
situacao: planejada
---
```

## Como medir sem achismo

| Métrica | Como obter | Limiar de alerta (ajustável) |
|---|---|---|
| Tamanho do serviço | contar linhas do arquivo | > 400 |
| Dependências por arquivo | imports internos | > 10 |
| Arquivos tocados por T | diff | > 15 |
| Tempo da suíte unitária | CI | > 1 a 2 min |
| Retrabalho por conflito de escopo | Achados | recorrente |

São indicadores para conversar, não metas; limiares dependem do projeto.

## Checklist

- [ ] Houve sintoma observável antes de evoluir?
- [ ] O passo é o menor que resolve?
- [ ] T de refatoração com critério de pronto e testes verdes antes/depois?
- [ ] ADR nova (ou atualização) com gatilhos revistos?
- [ ] A ADR antiga marcada `estado: substituido` (com `substituido_por`) e a nova com `substitui`?

## Erros comuns

| Erro | Correção |
|---|---|
| Evoluir "para o futuro" | Esperar sintoma |
| Refatoração grande sem T | T com escopo e critério |
| ADR sem gatilhos | Acrescentar limiares concretos |
| Misturar refatoração com funcionalidade nova na mesma T | Separar |
| Simplificar tarde demais (excesso de estrutura estabelecido) | Plano gradual: remover uma abstração por vez |
