# Checklist de revisão do gate de Design

Use antes de entregar a Etapa de Design. O Gate (aprovação humana: o aprovador abre o protótipo no
navegador) confere o critério de saída das instruções do agente e a qualidade geral. Esta lista antecipa o que
vai ser reprovado. Severidade:

- **Bloqueia:** reprovação certa no Gate.
- **Alto:** gera Achado nas Etapas seguintes (Dev, QA) ou problema de acessibilidade real.
- **Médio:** reduz a qualidade; corrija se houver tempo.

## 1. Critério de saída das instruções do agente (todos bloqueiam)

- [ ] Toda US com `tem_interface: true` tem tela em `telas` do TELAS (`TELAS.md`).
- [ ] O protótipo abre no navegador e navega entre **todas** as telas.

## 2. Entregáveis e estrutura

| # | Item | Sev. |
|---|---|---|
| 2.1 | Existem `fluxo.md`, `wireframes.md`, `prototipo/`, `estilo.md`, `TELAS.md` em `docs/design/`; `fluxo`, `prototipo` e `estilo` do TELAS apontam para os três primeiros e para `estilo.md` | Bloqueia |
| 2.1a | `TELAS.md` (TELAS) com cabeçalho YAML válido em `telas.schema.json`: `telas` com `us`, `criterios` e `estados` (`snake_case`); `acessibilidade.verificado` coerente com a verificação feita | Bloqueia |
| 2.2 | `prototipo/` abre por duplo clique, sem build, caminhos relativos, **sem CDN nem recurso remoto** (fontes, bibliotecas e imagens embutidas; sem `http(s)://` em `src`, `href` de recurso, `@import`, `url()`) | Bloqueia |
| 2.2a | Cada página aceita `?estado=<snake_case>` e mostra todos os estados do `estados` do TELAS para ela | Alto |
| 2.3 | Uma página por tela; nomes de arquivo coerentes com o `TELAS.md` | Alto |
| 2.4 | Interface em pt-BR (rótulos, mensagens, datas dd/mm/aaaa, 24 h) | Alto |
| 2.5 | Nenhuma tecnologia de produção decidida (o front final é do Dev em Vue) além do protótipo | Médio |

## 3. Cobertura de requisitos

| # | Item | Sev. |
|---|---|---|
| 3.1 | Todo CA com efeito visível tem estado desenhado (ex.: mensagem de conflito) e consta em `criterios` do TELAS | Bloqueia |
| 3.2 | Cada tela de `telas` do TELAS existe como arquivo do protótipo (`arquivo`) e vice-versa | Alto |
| 3.3 | Fluxos de erro e exceção estão no fluxo (conflito, validação, rede, sem permissão, sessão expirada) | Alto |
| 3.4 | RNF de usabilidade/acessibilidade/compatibilidade atendidos e verificados | Alto |
| 3.5 | Papéis: telas e ações de Administrador separadas e protegidas | Alto |

## 4. Fluxo e arquitetura de informação

| # | Item | Sev. |
|---|---|---|
| 4.1 | `fluxo.md` em Mermaid, renderiza sem erro de sintaxe | Alto |
| 4.2 | Inclui login, logout, erro de regra, sem permissão e erro de sistema | Alto |
| 4.3 | Toda tela tem saída (nenhum beco); setas só para telas existentes | Alto |
| 4.4 | Tarefa principal em poucos passos (meta do RNF; ex.: <= 3 a partir da tela inicial) | Médio |
| 4.5 | Navegação consistente; nome igual no menu, título e `<title>` | Médio |
| 4.6 | Comportamento a 360 px e desktop definido | Alto |

## 5. Wireframes e estados

| # | Item | Sev. |
|---|---|---|
| 5.1 | Cada tela documenta propósito, elementos, estados e ações | Bloqueia |
| 5.2 | Estados presentes: vazio, carregando, erro, sucesso (e sem permissão quando há papéis) | Bloqueia |
| 5.3 | Vazio de primeiro uso e vazio de filtro distintos, com ação | Médio |
| 5.4 | Erro de validação, de regra e de sistema distintos; dados preservados | Alto |
| 5.5 | Estados acessíveis no protótipo (parâmetro `?estado=`, ou páginas separadas) | Alto |

## 6. Formulários e mensagens

| # | Item | Sev. |
|---|---|---|
| 6.1 | Todo campo com `<label>` visível associado; placeholder não substitui rótulo | Bloqueia |
| 6.2 | Erro em texto, junto do campo, ligado por `aria-describedby`; resumo em formulários com mais de um campo | Alto |
| 6.3 | Mensagens acionáveis (o que aconteceu + o que fazer); sem códigos técnicos | Alto |
| 6.4 | Sucesso confirma o que foi feito e leva ao próximo passo | Médio |
| 6.5 | Ação destrutiva com confirmação que nomeia a ação, ou desfazer | Alto |
| 6.6 | Login: `autocomplete`, colar senha permitido, mensagem genérica | Alto |
| 6.7 | `type`, `inputmode`, `autocomplete` apropriados | Médio |

## 7. Acessibilidade (WCAG 2.2 AA)

| # | Item | Sev. |
|---|---|---|
| 7.1 | `lang="pt-BR"`, `<title>` único e descritivo por tela | Bloqueia |
| 7.2 | Todo o protótipo operável só por teclado; ordem de foco lógica; sem armadilha | Bloqueia |
| 7.3 | Foco sempre visível; nenhum `outline: none` sem substituto; foco não escondido por cabeçalho fixo | Bloqueia |
| 7.4 | Contraste: texto >= 4,5:1; texto grande, ícones e bordas de componente >= 3:1; tabela de contraste no `estilo.md` | Bloqueia |
| 7.5 | Cor nunca é o único sinal (ocupado, erro, obrigatório) | Alto |
| 7.6 | HTML semântico: `<button>`/`<a>`, títulos em ordem, regiões, listas/tabelas corretas | Alto |
| 7.7 | Alvos >= 24x24 px (preferência 44 px em toque) | Alto |
| 7.8 | Sem rolagem horizontal a 320 px; funciona a 200% de zoom | Alto |
| 7.9 | Mensagens de status anunciadas (`role="status"`/`alert`) | Alto |
| 7.10 | Link para pular ao conteúdo; menus e diálogos operáveis por teclado, Esc fecha | Alto |
| 7.11 | Varredura automática (axe-core/Lighthouse/pa11y) sem violações A/AA | Alto |
| 7.12 | Roteiro manual (teclado + leitor de tela) executado nos fluxos principais, com evidência | Alto |
| 7.13 | `prefers-reduced-motion` respeitado; nada pisca | Médio |
| 7.14 | Ícones sem texto têm nome acessível; imagens decorativas com `alt=""` | Alto |

## 8. Estilo e tokens

| # | Item | Sev. |
|---|---|---|
| 8.1 | Tokens (cor, tipografia, espaçamento, foco, forma) em `estilo.css` como variáveis CSS | Bloqueia |
| 8.2 | `estilo.md` documenta os mesmos nomes e valores; sem divergência com o CSS | Alto |
| 8.3 | Tokens semânticos (papéis), não literais, usados nos componentes | Médio |
| 8.4 | Componentes reaproveitados listados com variantes, estados e HTML | Médio |
| 8.5 | Unidades relativas (`rem`) para texto; sem alturas fixas em texto | Médio |

## 9. Coerência com o contrato da API

| # | Item | Sev. |
|---|---|---|
| 9.1 | Campos, nomes, formatos e enumerações iguais ao `openapi.yaml` | Bloqueia |
| 9.2 | Toda ação do protótipo corresponde a operação do contrato | Alto |
| 9.3 | Lacuna do contrato registrada como ACH `categoria: lacuna_contrato` (`etapa_destino: arquitetura`) e listada em `lacunas_contrato` do TELAS (nada de endpoint inventado) | Bloqueia |
| 9.4 | Dados fictícios variados e coerentes (listas vazias, uma, muitas; limites; conflito) | Médio |
| 9.5 | Nenhum dado pessoal real; dado pessoal exibido só ao papel autorizado | Alto |
| 9.6 | Parecer do Design (`participante: designer`) registrado no `VALIDACAO-NN.md` na Validação técnica (telas x contrato x modelo de dados em `docs/dados/`), com ACH para toda conferência `divergente` | Alto |

## 10. Heurísticas (avaliação do protótipo)

Passar por `heuristicas-usabilidade-nielsen.md`. Severidades 3 e 4 corrigidas; o restante
listado como "decisões que merecem atenção do aprovador humano".

- [ ] H1 feedback e estado; H3 saída/desfazer; H4 consistência; H5 prevenção de erro;
  H9 erros acionáveis.

## 11. Autoverificação em 15 minutos

1. **Abrir `index.html`** (ou login) e navegar por todos os links; contar páginas vs.
   `TELAS.md`.
2. **Teclado:** percorrer cada tela só com Tab/Enter/Esc.
3. **Varredura automática** em cada página; corrigir.
4. **Zoom 200% e 320 px** na página mais densa.
5. **Matriz CA x tela x estado:** nenhum CA visível sem estado.
6. **Matriz tela x estado:** nenhuma célula "x" sem desenho.
7. **Contrato:** buscar nomes de campo do protótipo no `openapi.yaml`.
8. **Texto:** passar pelas mensagens procurando termos técnicos e inglês.
9. **Tokens:** ver se `estilo.md` e `estilo.css` coincidem.
10. **Decisões de UX** relevantes anotadas para o aprovador humano.

## 12. Erros que mais reprovam

1. Estado de erro/conflito não desenhado.
2. Protótipo com link quebrado ou tela faltando.
3. Campo sem rótulo ou só com placeholder.
4. Foco invisível ou perdido.
5. Contraste não medido.
6. Campo do protótipo diferente do contrato.
7. Mensagens técnicas ou em inglês.
8. US com interface ausente do `TELAS.md`.
9. Ação destrutiva sem confirmação.
10. Seletor de demonstração sem identificação.

## 13. Resposta final ao entregar

Conforme as instruções do agente: arquivos criados, como abrir o protótipo, e decisões de UX que merecem
atenção do aprovador humano. Inclua também: ACH abertos para a Arquitetura (lacunas de contrato), itens de
severidade alta não resolvidos (e por quê) e o resultado da verificação de acessibilidade
(automática e manual). Nunca declare "acessível" sem registrar o que foi verificado.

## 14. Retrabalho por Achado

Em Onda de retrabalho: corrija **só** as telas e documentos afetados; suba a versão do que
mudar (se houver cabeçalho); registre a mudança; refaça apenas os itens do checklist tocados,
mais 1 (critério de saída) e 3.1 (CA visíveis).
