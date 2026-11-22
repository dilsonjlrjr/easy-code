---
name: ux-acessibilidade
description: Desenha a experiência de uma aplicação web interna ou corporativa, do fluxo de telas ao protótipo HTML acessível, aplicando heurísticas de usabilidade, padrões de formulário e mensagens, estados de tela, WCAG 2.2 AA verificável por critério, design tokens e checklist do gate de Design. Use na etapa de Design ao criar fluxo, wireframes, protótipo, guia de estilo ou mapa tela x história, e ao revisar acessibilidade de telas.
---

# UX e acessibilidade

Skill da etapa de **Design**. Genérica: vale para qualquer aplicação web (principalmente
sistemas internos, com tarefas repetidas e usuários que querem terminar rápido). Exemplos usam
reserva de salas só como ilustração.

## O que o Design entrega neste processo

O Design corre **em paralelo com o DBA**, depois da Arquitetura. Na **Validação técnica**
registra o seu parecer (`participante: designer` em `pareceres`) em `docs/validacao/VALIDACAO-NN.md` (artefato
VALIDACAO, esquema `validacao.schema.json`): telas x contrato da API x modelo de dados (pares
`api_telas` e `api_dados`). Nada de campo na tela sem origem no contrato ou no modelo.

Pelas instruções do agente, em `docs/design/`:

| Arquivo | Conteúdo |
|---|---|
| `fluxo.md` | Mapa de navegação em Mermaid: telas, transições, login e erros |
| `wireframes.md` | Por tela: propósito, elementos, estados (vazio, carregando, erro, sucesso) e ações |
| `prototipo/` | HTML + CSS + JS puro, uma página por tela, dados fictícios, links entre telas, abre sem build. Sem CDN: fontes, bibliotecas e imagens embutidas (CSP da origem de conteúdo, ADR-19); `?estado=<snake_case>` troca o estado |
| `estilo.md` | Design tokens (cor, tipografia, espaçamento) e componentes reaproveitados |
| `TELAS.md` | **Artefato TELAS**: cabeçalho YAML (esquema `telas.schema.json`) com o mapa tela x história, estados, acessibilidade e lacunas do contrato; `fluxo.md`, `prototipo/` e `estilo.md` são anexos dele |

O Dev (que implementa em Vue) segue o protótipo. Por isso nomes de campos, mensagens e estados
do protótipo são **contrato**: o que o Design escrever, o Dev vai usar.

## Princípios

1. **Tarefa antes de tela.** Parta das US com `tem_interface: true` e dos CA visíveis; cada CA
   visível tem um estado desenhado (ex.: a mensagem de conflito de horário).
2. **Menos telas, menos passos.** Cada tela tem uma ação principal óbvia.
3. **O sistema fala com clareza.** Diga o que aconteceu e o que fazer agora.
4. **Todos os estados.** Vazio, carregando, erro, sucesso, sem permissão.
5. **Acessível por construção.** Semântica HTML correta antes de ARIA; teclado e contraste
   desde o primeiro wireframe, não como retrofit.
6. **Consistência.** Mesma ação, mesmo rótulo, mesmo componente, mesmo lugar.
7. **Fiel ao contrato.** Campos e ações batem com `openapi.yaml`; se faltar algo, abrir ACH para
   a Arquitetura (id em `lacunas_contrato` do TELAS) em vez de inventar endpoint.
8. **Interface em pt-BR**, com datas dd/mm/aaaa e 24 h.
9. **Protótipo autossuficiente.** Sem CDN nem recurso remoto: embuta fontes, bibliotecas e imagens. A origem de conteúdo que serve o protótipo tem CSP fechada e bloqueia chamada externa.

## Fluxo de trabalho

1. Ler US com interface, RNF de acessibilidade/usabilidade/compatibilidade e o contrato da API.
2. Inventariar telas necessárias e ações (`references/fluxo-e-arquitetura-de-informacao.md`).
3. Desenhar o fluxo em Mermaid, incluindo login, erro e sem permissão.
4. Escrever wireframes por tela com estados (`references/estados-de-tela.md`) e formulários
   (`references/formularios-validacao-mensagens.md`).
5. Definir tokens e componentes (`references/design-tokens-sistema-visual.md`).
6. Construir o protótipo HTML acessível (`references/prototipo-html-acessivel.md`).
7. Verificar acessibilidade por critério (`references/wcag-22-aa-detalhado.md`).
8. Revisar heurísticas (`references/heuristicas-usabilidade-nielsen.md`).
9. Montar o artefato TELAS (`TELAS.md`, seção abaixo) e passar o checklist (`references/checklist-gate-design.md`).
10. Na Validação técnica, registrar o parecer do Design no `VALIDACAO-NN.md` (telas x contrato x
    modelo de dados); inconsistência vira ACH para a etapa dona do artefato.

## Regras de decisão

| Situação | Decisão |
|---|---|
| Elemento clicável | Navega para outra URL: `<a href>`; executa ação: `<button>` |
| Campo de formulário | `<label>` visível associado; placeholder nunca substitui rótulo |
| Mensagem de erro de campo | Texto junto ao campo, ligada por `aria-describedby`, resumo no topo em formulários longos |
| Mensagem global de sucesso/erro | Região de status (`role="status"` ou `role="alert"`) |
| Ação destrutiva | Confirmação com descrição do efeito e botão que nomeia a ação; ou desfazer |
| Informação por cor | Sempre duplicar com texto ou ícone |
| Componente customizado (calendário, combobox) | Preferir elemento nativo; se customizar, seguir padrão WAI-ARIA Authoring Practices |
| ARIA | Só quando a semântica nativa não basta; ARIA ruim é pior que nenhum |
| Falta endpoint/campo no contrato | ACH `categoria: lacuna_contrato`, `etapa_destino: arquitetura`; o id entra em `lacunas_contrato` do TELAS |
| Mais de uma forma de fazer a tarefa | Mostrar a principal; as demais secundárias |
| Tabela larga em celular | Reorganizar (cartões) ou rolagem contida com rótulo; nunca rolagem da página inteira |

## Piso de acessibilidade (WCAG 2.2 AA)

Contraste de texto >= 4,5:1 (texto grande e componentes >= 3:1); tudo operável por teclado;
foco visível e nunca escondido; rótulos em campos; erros em texto e ligados ao campo;
alvos >= 24x24 px; `lang="pt-BR"`; estrutura de títulos; sem rolagem horizontal a 320 px;
mensagens de status anunciadas. Detalhe e verificação por critério em
`references/wcag-22-aa-detalhado.md`.

## Artefato TELAS (`TELAS.md`)

O mapa tela x história é o artefato `TELAS` (id fixo, um por projeto, arquivo
`docs/design/TELAS.md`): **cabeçalho YAML validado** por `specs/dominio/artefatos/telas.schema.json`
+ corpo Markdown livre que nunca contradiz o YAML (decisão D-01). Molde e exemplo em
`agentes/templates/TELAS.md`. **Vale o esquema.** Etapa dona: `design` (gate.design); mudança depois de
aprovado só por ACH `inconsistencia_design` (afetado: `TELAS` ou `TELAS/TL-n`), que reabre o gate.

Campos próprios (além do cabeçalho comum, `autor: designer-agent`, `onda: DSG-<nn>`):

- `fluxo`, `prototipo`, `estilo`: caminhos (sem espaços) dos anexos `fluxo.md`, da pasta `prototipo/` e de `estilo.md`.
- `telas` (ao menos 1): cada uma com `id` (`TL-n`, local ao artefato), `nome`, `arquivo` (a página do
  protótipo), `us` (ao menos 1), `criterios` (`US-NN/CA-n`, ao menos 1; cada critério pertence a uma US
  listada na mesma tela) e `estados` (lista em `snake_case` dos estados desenhados: `vazio`, `carregando`,
  `erro`, `sucesso`, `sem_permissao`, ou outros como `erro_conflito`).
- `acessibilidade`: `nivel: wcag22_aa`, `verificado` (`true` só depois de conferir teclado, foco,
  contraste e rótulos no protótipo) e `observacoes` opcional (exceções ou pendências).
- `lacunas_contrato`: ids de ACH abertos para a Arquitetura por campo ou ação sem endpoint no contrato;
  `[]` se nenhuma. Cada um tem `etapa_destino: arquitetura` (ver `specs/dominio/achado.md`).

Exemplo mínimo válido:

```yaml
---
id: TELAS
tipo: TELAS
titulo: Mapa de telas — Reserva de salas
autor: designer-agent
onda: DSG-01
versao: 1
estado: rascunho
vinculos: []
historico:
  - {versao: 1, data: "2026-11-19", onda: DSG-01, descricao: criação}
fluxo: docs/design/fluxo.md
prototipo: docs/design/prototipo/
estilo: docs/design/estilo.md
telas:
  - id: TL-1
    nome: Nova reserva
    arquivo: docs/design/prototipo/nova-reserva.html
    us: [US-03]
    criterios: [US-03/CA-1, US-03/CA-2, US-03/CA-4]
    estados: [padrao, carregando, erro_validacao, erro_conflito, sucesso]
  - id: TL-2
    nome: Minhas reservas
    arquivo: docs/design/prototipo/minhas-reservas.html
    us: [US-04]
    criterios: [US-04/CA-1]
    estados: [vazio, carregando, lista, erro, confirmacao_cancelamento]
acessibilidade:
  nivel: wcag22_aa
  verificado: true
  observacoes: Teclado, foco, contraste e rótulos conferidos nas duas telas.
lacunas_contrato: [ACH-03]
---
```

Corpo (tabela de apoio; a fonte é o YAML):

```markdown
| Tela | Arquivo | US | Critérios | Estados |
|---|---|---|---|---|
| TL-1 Nova reserva | `prototipo/nova-reserva.html` | US-03 | US-03/CA-1, CA-2, CA-4 | padrão, carregando, erro de validação, erro de conflito, sucesso |
```

Regra: todo CA com efeito visível aparece em `criterios` de uma tela, com o **estado** que o mostra
(o CA de conflito aponta para `erro_conflito` de Nova reserva).

## Checklist de saída (resumo)

- [ ] Toda US com `tem_interface: true` tem tela em `telas` do TELAS (`TELAS.md`).
- [ ] Todo CA visível tem estado desenhado.
- [ ] Cada tela tem estados vazio, carregando, erro, sucesso (e sem permissão, se houver papéis).
- [ ] Fluxo cobre login, erro e logout; nenhuma tela sem saída.
- [ ] Protótipo abre no navegador e navega por todas as telas.
- [ ] Teclado, foco, contraste e rótulos verificados.
- [ ] Campos e ações iguais ao contrato da API; lacunas viraram ACH e estão em `lacunas_contrato`.
- [ ] TELAS válido no esquema: `acessibilidade.verificado: true` só após a verificação; `estados` em `snake_case`.
- [ ] Tokens definidos em `estilo.css` (variáveis CSS) e documentados em `estilo.md`.
- [ ] Textos em pt-BR, mensagens acionáveis.

Checklist completo, com severidade, em `references/checklist-gate-design.md`.

## Erros comuns

- Desenhar só o caminho feliz.
- Placeholder no lugar do rótulo; rótulo que some ao digitar.
- `<div onclick>` em vez de `<button>`.
- Foco removido com `outline: none` sem substituto.
- Erro só em vermelho; erro genérico ("Algo deu errado").
- Modal sem foco gerenciado ou sem saída por Esc.
- Calendário/seletor customizado que só funciona com mouse.
- Mensagens e rótulos diferentes entre protótipo e contrato da API.
- Tokens de cor sem checagem de contraste.
- Texto em imagem, ícone sem nome acessível.
- Tela sem título de página (`<title>`) distinto.
- Dependência de hover para revelar ação essencial.

## Índice dos arquivos de referência

| Arquivo | Quando ler |
|---|---|
| `references/heuristicas-usabilidade-nielsen.md` | Para avaliar e justificar decisões; revisão heurística de protótipo |
| `references/fluxo-e-arquitetura-de-informacao.md` | Para montar `fluxo.md`, navegação, nomes de telas e hierarquia |
| `references/formularios-validacao-mensagens.md` | Para qualquer formulário, validação, mensagens de erro e sucesso |
| `references/estados-de-tela.md` | Para desenhar vazio, carregando, erro, sucesso, sem permissão, conflito |
| `references/wcag-22-aa-detalhado.md` | Para checar cada critério A/AA relevante e como verificar |
| `references/design-tokens-sistema-visual.md` | Para `estilo.md` e `estilo.css`: cores, tipografia, espaço, foco, componentes |
| `references/prototipo-html-acessivel.md` | Para construir as páginas do protótipo: estrutura, semântica, ARIA, JS mínimo |
| `references/checklist-gate-design.md` | Antes de entregar: checklist do gate com severidade |
