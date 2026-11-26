# Template — Mapa de telas (TELAS) — v1

- **Esquema:** specs/dominio/artefatos/telas.schema.json
- **Quem preenche:** agente Designer.
- **Quem lê:** Dev (implementa as telas), QA (e2e e acessibilidade), Arquiteto e DBA (parecer na Validação), orquestrador (gate de Design).
- **Gate confere:** toda US com interface aparece em ao menos uma tela; todo critério de aceite visível
  consta em `criterios` e tem estado desenhado; cada `arquivo` existe no protótipo; acessibilidade
  verificada; `lacunas_contrato` aponta ACH para cada ação sem endpoint.
- **Arquivo:** `docs/design/TELAS.md` (um por projeto, versionado).

## Molde

````markdown
---
id: TELAS
tipo: TELAS
titulo: Mapa de telas — <projeto>
autor: designer-agent
onda: DSG-<nn>
versao: 1
estado: rascunho              # rascunho | em_revisao | aprovado | substituido (ciclo do artefato)
vinculos: []
historico:
  - {versao: 1, data: "<AAAA-MM-DD>", onda: DSG-<nn>, descricao: criação}
fluxo: docs/design/fluxo.md
prototipo: docs/design/prototipo/
estilo: docs/design/estilo.md
telas:                        # ao menos 1
  - id: TL-1                  # TL-n, local ao artefato
    nome: <nome da tela>
    arquivo: docs/design/prototipo/<tela>.html
    us: [US-NN]               # ao menos 1
    criterios: [US-NN/CA-n]   # ao menos 1: critérios que a tela mostra
    operacoes: []             # opcional: operationId do contrato usados pela tela
    estados: [vazio, carregando, erro, sucesso, sem_permissao]   # estados desenhados; outros em snake_case
acessibilidade:
  nivel: wcag22_aa
  verificado: false           # true quando teclado, foco, contraste e rótulos foram conferidos
  observacoes: <exceções ou pendências>   # opcional
lacunas_contrato: []          # ids de ACH para a Arquitetura (ação sem endpoint); [] se nenhuma
---

# Mapa de telas — <projeto>

| Tela | Arquivo | US | Critérios | Estados |
|---|---|---|---|---|
| TL-1 <nome> | `prototipo/<tela>.html` | US-NN | US-NN/CA-n | vazio, erro, sucesso |

## Observações
<decisões de design, exceções de acessibilidade, lacunas do contrato; o YAML guarda o resumo>
````

## Regras

- O YAML é a fonte; a tabela do corpo explica e não contradiz.
- Todo critério de `criterios` pertence a uma US listada em `us` da mesma tela.
- Campo ou ação sem endpoint na API: ACH de categoria `lacuna_contrato` para a Arquitetura, id em `lacunas_contrato`. Não invente endpoint.
- O mapa é a etapa dona `design`; mudança depois de aprovado só por ACH `inconsistencia_design`
  (afetado: `TELAS` ou `TELAS/TL-n`), que reabre `gate.design`.
- `fluxo.md`, `wireframes.md`, `prototipo/` e `estilo.md` são anexos deste artefato (sem template próprio).

## Exemplo

````markdown
---
id: TELAS
tipo: TELAS
titulo: Mapa de telas — Reserva de salas
autor: designer-agent
onda: DSG-01
versao: 1
estado: aprovado
vinculos: []
historico:
  - {versao: 1, data: "2026-11-19", onda: DSG-01, descricao: criação}
fluxo: docs/design/fluxo.md
prototipo: docs/design/prototipo/
estilo: docs/design/estilo.md
telas:
  - id: TL-1
    nome: Login
    arquivo: docs/design/prototipo/login.html
    us: [US-01]
    criterios: [US-01/CA-1, US-01/CA-2]
    estados: [carregando, erro, sucesso]
  - id: TL-2
    nome: Reservar sala
    arquivo: docs/design/prototipo/reservar.html
    us: [US-03]
    criterios: [US-03/CA-1, US-03/CA-2]
    estados: [vazio, carregando, erro, sucesso, sem_permissao]
acessibilidade:
  nivel: wcag22_aa
  verificado: true
  observacoes: Navegação por teclado e contraste conferidos nas duas telas.
lacunas_contrato: []
---

# Mapa de telas — Reserva de salas

| Tela | Arquivo | US | Critérios | Estados |
|---|---|---|---|---|
| TL-1 Login | `prototipo/login.html` | US-01 | US-01/CA-1, US-01/CA-2 | carregando, erro, sucesso |
| TL-2 Reservar sala | `prototipo/reservar.html` | US-03 | US-03/CA-1, US-03/CA-2 | vazio, carregando, erro (conflito), sucesso, sem permissão |
````
