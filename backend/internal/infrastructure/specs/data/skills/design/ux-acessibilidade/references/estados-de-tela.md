# Estados de tela

Toda tela de dados tem mais de uma aparência. O protótipo e o `wireframes.md` precisam
mostrar **cada estado**; o Dev implementa o que o Design mostrou, e o QA testa cada um. Um
estado não desenhado vira estado improvisado (ou quebrado).

## 1. Catálogo de estados

| Estado | Quando ocorre | Obrigatório para |
|---|---|---|
| **Padrão / com dados** | Dados carregados | Toda tela |
| **Vazio (primeiro uso)** | Nenhum registro existe ainda | Toda lista/consulta |
| **Vazio (sem resultado)** | Filtro/busca sem correspondência | Toda tela com filtro |
| **Carregando** | Aguardando dados ou ação | Toda chamada à API |
| **Erro de validação** | Entrada inválida | Todo formulário |
| **Erro de regra (negócio)** | Regra violada (conflito de horário) | Todo CA de falha visível |
| **Erro de sistema/rede** | Falha técnica | Toda tela com API |
| **Sucesso** | Ação concluída | Toda ação que muda dado |
| **Sem permissão** | Papel não autorizado | Telas com papéis |
| **Não encontrado** | Recurso inexistente ou removido | Telas com id na URL |
| **Sessão expirada** | Credencial inválida | Telas protegidas |
| **Parcial** | Parte dos dados falhou | Telas com várias fontes |
| **Desabilitado** | Ação indisponível por estado | Botões/campos condicionados |
| **Confirmação** | Ação destrutiva | Cancelar/excluir |

## 2. Anatomia de cada estado

### Vazio

Explica **por que está vazio** e **o que fazer**. Diferencie "ainda não há nada" de "o filtro
não achou nada".

| Variante | Texto | Ação |
|---|---|---|
| Primeiro uso | "Você ainda não tem reservas futuras." | Botão "Reservar uma sala" |
| Sem resultado | "Nenhuma sala disponível em 10/03, das 10h às 11h." | "Limpar filtros" ou "Ver outros horários" |
| Sem permissão para ver | Estado "sem permissão" (não "vazio") | Orientação |

Ruim: tabela vazia sem mensagem; "Nenhum dado".

### Carregando

| Duração esperada | Padrão |
|---|---|
| < 1 s | Pode não precisar de indicador; evite piscar (atraso de ~300 ms antes de mostrar) |
| 1 a 10 s | Indicador de atividade e/ou esqueleto (skeleton) com o formato do conteúdo |
| > 10 s | Barra de progresso real, estimativa e opção de cancelar |

Acessibilidade: anunciar o estado a leitores de tela (`aria-busy="true"` na região,
e/ou texto "Carregando salas..." em região `role="status"`); o indicador visual sozinho não
basta. Preserve o layout para evitar saltos (CLS). Esqueletos devem ter `aria-hidden` e o texto
de status acessível ao lado.

```html
<section aria-labelledby="titulo-lista" aria-busy="true">
  <h2 id="titulo-lista">Minhas reservas</h2>
  <p role="status">Carregando suas reservas...</p>
  <div class="esqueleto" aria-hidden="true"></div>
</section>
```

### Erro

Três tipos, três tratamentos:

| Tipo | Onde | Texto | Ação |
|---|---|---|---|
| Validação | No campo + resumo | O que corrigir | Corrigir e reenviar |
| Regra de negócio | No topo do formulário/ação, perto do foco | Motivo + alternativa | Escolher outra opção |
| Sistema/rede | Região da tela afetada | Causa provável + "Tentar de novo" | Botão de nova tentativa; código de suporte |

Regras: **preservar dados**, **dar nova tentativa**, **não derrubar a tela inteira** se só uma
região falhou, e **nunca expor detalhes internos**.

### Sucesso

Confirma o resultado com os dados e leva ao próximo passo (ver `formularios-validacao-mensagens.md`).
O efeito também aparece na tela de destino (a nova reserva na lista).

### Sem permissão

Mensagem clara: "Você não tem acesso a esta página." + caminho ("Voltar ao início"). Em ações
(cancelar reserva alheia), a ação nem deve aparecer para quem não pode; se aparecer por erro,
resposta amigável. Não revele existência do recurso quando isso for sensível (alinhar com RNF).

### Sessão expirada

Mensagem + botão "Entrar novamente"; após login, volta ao ponto e **mantém o que foi digitado**
quando possível.

### Desabilitado

Quando possível, mostre o **motivo** perto do controle ("Reserva já iniciada: não pode ser
cancelada"). `aria-disabled="true"` mantém o foco e permite explicar; `disabled` nativo remove do
foco. Escolha conforme a necessidade de explicar.

## 3. Estados do protótipo estático

Como o protótipo é HTML sem build, mostre estados assim:

| Técnica | Como |
|---|---|
| Parâmetro na URL | `nova-reserva.html?estado=conflito` altera o que a página exibe via JS mínimo |
| Seletor de estado de demonstração | Faixa discreta no topo "Estado de demonstração: [padrão, vazio, carregando, erro, conflito]" |
| Páginas separadas | `minhas-reservas-vazio.html` (mais simples e mais fácil de testar) |
| Atributo `data-estado` no corpo | CSS exibe o bloco certo |

Mantenha o seletor de demonstração fora do foco principal e **marcado como demonstração**
para o Dev não implementá-lo. Documente cada estado em `wireframes.md`.

```html
<body data-estado="vazio">
  <p class="demo">Demonstração: <a href="?estado=vazio">vazio</a> ·
     <a href="?estado=carregando">carregando</a> · <a href="?estado=erro">erro</a> ·
     <a href="?estado=padrao">com dados</a></p>
  ...
</body>
```

## 4. Matriz tela x estado (modelo para `wireframes.md`)

| Tela | Padrão | Vazio | Sem resultado | Carregando | Validação | Regra | Sistema | Sucesso | Sem permissão |
|---|---|---|---|---|---|---|---|---|---|
| Login | x | - | - | x | x | x (credenciais) | x | (redireciona) | - |
| Salas disponíveis | x | x | x | x | x (filtros) | - | x | - | - |
| Nova reserva | x | - | - | x | x | x (conflito) | x | x | x |
| Minhas reservas | x | x | - | x | - | x (cancelar fora do prazo) | x | x | - |
| Gerenciar salas | x | x | x | x | x | x (nome duplicado) | x | x | x (funcionário) |

"-" significa "não se aplica" e deve ser decisão consciente. Toda célula "x" tem
descrição e, se for visível, aparece no protótipo.

## 5. Mapeamento de CA para estado

Para cada CA visível: tela e estado. No artefato TELAS isso vira `criterios` (`US-NN/CA-n`) e `estados`
(`snake_case`, ex.: `erro_conflito`) da tela. Exemplo:

| CA | Tela | Estado |
|---|---|---|
| US-03 CA-2 (conflito) | Nova reserva | Erro de regra |
| US-03 CA-4 (fim antes do início) | Nova reserva | Erro de validação |
| US-03 CA-6 (sem login) | qualquer | Sessão expirada / redirecionamento ao login |
| US-04 CA-1 (lista de reservas) | Minhas reservas | Padrão e vazio |

## 6. Acessibilidade dos estados

- **Mudanças de estado são anunciadas** (WCAG 4.1.3): `role="status"` para informativas,
  `role="alert"` para erros que exigem atenção imediata.
- **Foco:** após erro, mover para o resumo/campo; após sucesso, para a mensagem ou título do
  destino; ao abrir diálogo, para dentro dele; ao fechar, de volta ao gatilho.
- **Não só cor, não só ícone:** estado tem texto.
- **Tempo:** mensagens temporárias dão tempo suficiente ou persistem.
- **Movimento:** animações de carregamento respeitam `prefers-reduced-motion`.

## 7. Heurísticas de especialista

- O **estado vazio é a primeira impressão** do sistema: trate-o como tela de verdade.
- **Diferencie vazio, sem resultado e sem permissão**: significam coisas diferentes.
- **Erro de regra não é erro técnico**: dê alternativas (próximos horários livres).
- **Nunca deixe uma tela só com spinner** sem texto e sem saída em caso de falha.
- **Nova tentativa** em todo erro de rede.
- **Estado parcial**: se a lista carrega e o painel lateral falha, não bloqueie a tela.
- **Concorrência** (outro usuário reservou enquanto você escolhia): tratar como erro de regra
  ao confirmar, com os dados atualizados.

## 8. Anti-padrões

- Tela em branco ou tabela só com cabeçalho no vazio.
- Spinner infinito sem tempo limite.
- "Erro" sem ação.
- Mesmo texto para "sem dados" e "sem permissão".
- Estados só descritos no texto e ausentes do protótipo.
- Seletor de demonstração indistinguível da interface real.
- Mudanças dinâmicas sem anúncio para leitor de tela.

## 9. Checklist

- [ ] Matriz tela x estado preenchida; células "não se aplica" justificadas.
- [ ] Vazio de primeiro uso e vazio de filtro distintos, com ação.
- [ ] Carregando com texto acessível e layout estável.
- [ ] Erros de validação, de regra e de sistema distintos; dados preservados.
- [ ] Sucesso com dados e próximo passo; anunciado.
- [ ] Sem permissão e sessão expirada desenhados onde há login/papéis.
- [ ] Cada CA visível aponta tela e estado.
- [ ] Protótipo permite ver cada estado; seletor identificado como demonstração.
