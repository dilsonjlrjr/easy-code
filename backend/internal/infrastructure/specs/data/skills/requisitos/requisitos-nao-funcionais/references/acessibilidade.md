# RNF de acessibilidade

Categoria (`categoria`): `acessibilidade`. Para aplicação web, o referencial é a **WCAG 2.2**
(Web Content Accessibility Guidelines, W3C), nível **AA**. No Brasil, a Lei Brasileira de
Inclusão (Lei 13.146/2015) estabelece a acessibilidade na web para sites de empresas com sede
ou representação comercial no país (art. 63); trate acessibilidade como obrigação, não como
opcional. O detalhamento técnico por critério está na skill `ux-acessibilidade`
(`references/wcag-22-aa-detalhado.md`); aqui está o que o **RNF** precisa dizer.

## 1. Perguntas-chave

- O público inclui pessoas com deficiência? (Assuma que sim.)
- Há obrigação contratual, legal ou de política interna de conformidade? Qual nível?
- Quais telas estão no escopo? (Todas as que o usuário final usa.)
- Quais tecnologias assistivas devem funcionar (leitor de tela, ampliação, navegação por teclado,
  comando de voz)?
- Há conteúdo com mídia (vídeo, áudio, imagem informativa)?
- Há documentos para download (PDF) que também precisam ser acessíveis?

## 2. O que o RNF define

| Elemento | Decisão |
|---|---|
| Padrão | WCAG 2.2 |
| Nível | A + AA (nível AA inclui todos os critérios A) |
| Escopo | Lista de telas/fluxos (ou "todas as telas voltadas ao usuário") |
| Exceções | Conteúdo explicitamente fora (ex.: painel interno de operação, se houver justificativa) |
| Verificação | Automática + manual (teclado, leitor de tela) |
| Critério de aprovação | 0 violações de nível A/AA nos fluxos principais, ou lista de exceções aprovadas |

## 3. Verificação: o que automatiza e o que não

Ferramentas automáticas detectam só uma parte (a literatura costuma estimar algo em torno de um
terço a metade dos problemas). Por isso o RNF deve exigir **automática + manual**.

| Tipo | Instrumento (open source) | Cobre |
|---|---|---|
| Varredura automática | axe-core (via integração em teste e2e), Lighthouse, pa11y | Contraste, rótulos ausentes, atributos ARIA inválidos, idioma, títulos, alt ausente |
| Teste de teclado | Roteiro manual ou teste e2e (Tab/Shift+Tab/Enter/Esc) | Alcance, ordem, armadilha de foco, foco visível |
| Leitor de tela | NVDA (Windows), Orca (Linux); VoiceOver (macOS, nativo, não open source) | Nome/papel/valor, anúncio de erros, de estados |
| Zoom e reflow | Zoom a 200% e largura 320 px | Perda de conteúdo, rolagem em duas dimensões |
| Revisão de conteúdo | Checklist manual | Texto alternativo útil, linguagem clara, instruções |
| Contraste | Medição com ferramenta de contraste | Texto, ícones, bordas de campo, estados de foco |

## 4. Métricas típicas

| Tema | Métrica | Verificação |
|---|---|---|
| Conformidade geral | 0 violações automáticas de nível A/AA (axe-core) nas telas em escopo | Teste e2e com varredura em cada tela |
| Teclado | 100% das funções executáveis só por teclado, com ordem de foco lógica e foco visível | Roteiro manual/e2e |
| Contraste | Texto >= 4,5:1; texto grande (>= 24 px, ou >= 18,66 px em negrito) e componentes de interface >= 3:1 | Medição |
| Rótulos | 100% dos campos de formulário com rótulo programaticamente associado | Varredura + revisão |
| Erros | Todo erro de validação é identificado em texto e associado ao campo | Teste manual + e2e |
| Reflow | Sem rolagem horizontal em 320 px de largura (zoom 400%) | Teste manual |
| Alvo de toque | Alvos >= 24x24 px CSS (WCAG 2.2 AA, 2.5.8), preferência 44x44 em mobile | Medição |
| Idioma | `lang="pt-BR"` em todas as páginas | Varredura |
| Movimento | Nenhuma animação automática > 5 s sem mecanismo de pausa; respeita `prefers-reduced-motion` | Revisão |
| Leitor de tela | Fluxos principais concluídos com NVDA sem ajuda visual | Teste manual com roteiro |

## 5. Exemplos completos

Bom:

```yaml
categoria: acessibilidade
requisito: "as telas voltadas ao usuário atendem à WCAG 2.2 nível AA e podem ser operadas só com teclado"
metrica: "0 violações de nível A/AA reportadas por varredura automática (axe-core) em todas as telas do protótipo e da aplicação; os fluxos \"entrar\", \"reservar\" e \"cancelar\" concluídos só com teclado; contraste de texto >= 4,5:1"
verificacao:
  tipo: varredura
  detalhe: "varredura automática integrada ao teste e2e + roteiro manual de teclado e de leitor de tela (NVDA) nos três fluxos"
prioridade: must
origem:
  tipo: lei
  detalhe: "lei (Lei 13.146/2015, art. 63) e boa prática (WCAG 2.2 AA)"
```

Ruim:

```yaml
requisito: "o sistema deve ser acessível"
metrica: "acessibilidade boa"
verificacao:
  tipo: teste_automatizado
  detalhe: "teste de acessibilidade"
```

Por quê: não diz padrão, nível, escopo nem instrumento.

## 6. Quando separar em vários RNF

| RNF | Motivo para separar |
|---|---|
| Conformidade WCAG 2.2 AA | Visão geral, auditável |
| Operação por teclado | Teste manual específico |
| Contraste e identidade visual | Afeta tokens do Design |
| Leitor de tela nos fluxos principais | Teste manual com roteiro |

Um único RNF cobrindo tudo é aceitável em aplicação pequena, desde que a métrica liste os componentes.

## 7. Relação com Design e Dev

- O Design traduz o RNF em tokens de contraste, foco visível, estados e semântica
  (`ux-acessibilidade`).
- O Dev mantém a semântica ao implementar (HTML semântico, rótulos).
- QA automatiza a varredura e executa o roteiro manual.
- Se o RNF for "AAA", declare critérios específicos: AAA completa é raramente atingível para um
  site inteiro (a própria WCAG recomenda não exigi-la como política geral).

## 8. Heurísticas de especialista

- **"Assuma que sim"**: não pergunte se há usuários com deficiência; dimensione o escopo.
- **Escopo por fluxo** (entrar, reservar, cancelar) é mais verificável que "o site todo".
- **Automático não basta**: exija o roteiro manual.
- **Contraste e foco** são decididos cedo (tokens) e caros de corrigir depois.
- **Cor não é o único meio** de informação.
- **Erro acessível** é requisito de formulário: mensagem textual, ligada ao campo, anunciada.
- **WCAG 2.2** acrescenta critérios sobre foco não obscurecido, alvos de 24x24, arrastar com
  alternativa, ajuda consistente, autenticação acessível e entrada redundante. Os critérios
  novos de nível AA relevantes estão no detalhamento da skill de Design.

## 9. Anti-padrões

- "Acessível" sem nível, versão ou escopo.
- Confiar só em varredura automática.
- Deixar acessibilidade para "depois" (a correção custa mais).
- Exigir AAA geral.
- Aceitar overlay/plugin de acessibilidade como solução (não substitui o código correto).
- Testar só no protótipo e nunca na aplicação final: o RNF deve valer nos dois.

## 10. Checklist

- [ ] Padrão e versão (WCAG 2.2), nível (AA) e escopo definidos.
- [ ] Verificação combina varredura automática e roteiro manual (teclado + leitor de tela).
- [ ] Métricas objetivas (violações = 0, contraste, alvos, reflow).
- [ ] Origem cita lei e/ou boa prática.
- [ ] Vinculado às US com interface.
- [ ] Coerente com a skill `ux-acessibilidade` e com as instruções do Design.
