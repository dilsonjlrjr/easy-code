# Heurísticas de usabilidade (Nielsen) aplicadas a sistemas internos

As **10 heurísticas de usabilidade de Jakob Nielsen** são princípios amplos, não regras
rígidas. Em sistemas internos (tarefas repetidas, usuários que dominam o domínio, pouca
tolerância a cliques extras) algumas pesam mais. Use esta referência para **projetar** e para
**avaliar o protótipo** antes do gate.

## 1. As 10 heurísticas, com tradução prática

### H1. Visibilidade do estado do sistema

O usuário sempre sabe o que está acontecendo.

| Aplicação | Exemplo |
|---|---|
| Feedback de ação | Após reservar: mensagem "Reserva criada: Sala Atlântico, 10/03, 10h–11h" |
| Indicador de progresso | Botão "Reservando..." desabilitado durante a chamada; carregando em listas |
| Localização | Item de menu atual marcado (`aria-current="page"`); título da página |
| Estado de dados | "Última atualização 10:32" quando houver cache |

Falha comum: ação sem retorno; o usuário clica de novo e duplica.

### H2. Correspondência entre o sistema e o mundo real

Linguagem do usuário, não do banco.

- Termos do glossário da `VISAO.md` ("reserva", "sala"), não "registro", "entidade", "booking".
- Ordem natural: data e horário antes de capacidade, se é assim que a pessoa pensa.
- Datas por extenso quando ajuda ("qua, 10 de março") ou dd/mm/aaaa.

### H3. Controle e liberdade do usuário

Saída de emergência e desfazer.

- "Cancelar" e "Voltar" em todo fluxo de múltiplos passos.
- Fechar modal por Esc e botão.
- Ação destrutiva reversível quando possível (desfazer por alguns segundos) ou confirmada.
- Não prender o usuário (sem armadilha de foco).

### H4. Consistência e padrões

- Mesma ação com o mesmo rótulo e lugar em todas as telas ("Cancelar reserva", nunca
  "Excluir" numa e "Remover" noutra).
- Seguir convenções da plataforma: logo leva à página inicial; link sublinhado; botão
  primário destacado, um por tela.
- Reutilizar componentes (ver `design-tokens-sistema-visual.md`).

### H5. Prevenção de erros

Melhor que boa mensagem de erro é não deixar o erro acontecer.

| Técnica | Exemplo |
|---|---|
| Restringir entrada | Campo de hora com passos de 30 min; data mínima = hoje |
| Padrões inteligentes | Data de hoje pré-selecionada; duração padrão de 1 h |
| Desabilitar o impossível (com explicação) | Horário ocupado visível como "Ocupado" (texto, não só cor) |
| Confirmar o destrutivo | Diálogo "Cancelar a reserva de 10h–11h?" |
| Mostrar conflito antes do envio | Disponibilidade visível na seleção |

Atenção: desabilitar botão sem dizer por quê é antipadrão (ver `formularios-validacao-mensagens.md`).

### H6. Reconhecimento em vez de memorização

- Opções visíveis (listas de salas com capacidade e recursos) em vez de exigir digitar nomes.
- Rótulos e instruções junto do campo, não só em tela anterior.
- Ações recentes ou valores padrão a partir do último uso, quando fizer sentido.

### H7. Flexibilidade e eficiência de uso

Atalhos para o experiente sem prejudicar o novato.

- Fluxo principal curto (reservar em até 3 passos).
- Filtros memorizados da última busca; teclas padrão (Enter envia, Esc fecha).
- Atenção: atalhos de tecla única exigem mecanismo de desativação (WCAG 2.1.4).

### H8. Design estético e minimalista

- Cada elemento compete por atenção: remova o que não ajuda a tarefa.
- Hierarquia visual: ação principal destacada; secundárias discretas.
- Informação rara fica atrás de "mais detalhes".
- Minimalismo não é ocultar o necessário: nunca esconder ação essencial em hover.

### H9. Ajudar a reconhecer, diagnosticar e recuperar-se de erros

Mensagem de erro = **o que aconteceu + por quê (se útil) + o que fazer**, em linguagem simples,
sem código técnico, junto do problema.

| Ruim | Bom |
|---|---|
| "Erro 409" | "Esta sala já está reservada das 10h às 11h. Escolha outro horário ou outra sala." |
| "Dados inválidos" | "O horário de término deve ser depois do horário de início." |
| "Algo deu errado" | "Não foi possível salvar. Verifique sua conexão e tente de novo." |
| Mensagem só em vermelho | Ícone + texto + cor; ligada ao campo |

### H10. Ajuda e documentação

Em sistema interno bem desenhado, a ajuda é mínima e contextual: dicas junto ao campo,
texto de ajuda curto, um local consistente para contato/suporte (WCAG 3.2.6, ajuda consistente).

## 2. Heurísticas que pesam mais em sistemas internos

| Peso alto | Por quê |
|---|---|
| H7 (eficiência) | Uso diário; cada clique extra multiplica |
| H5 (prevenção de erro) | Erro em dado de negócio custa tempo de terceiros |
| H1 (estado) | Muita gente em simultâneo; dado muda sob os pés (conflitos) |
| H4 (consistência) | Treinamento é mínimo; padrões reduzem aprendizado |
| H9 (erros) | Suporte interno não escala |

## 3. Como conduzir uma avaliação heurística do protótipo

1. Percorra **cada tarefa principal** (uma por US com interface) do início ao fim.
2. Em cada tela, passe pelas 10 heurísticas e anote violações com **severidade**:

| Severidade | Critério |
|---|---|
| 0 | Não é problema |
| 1 | Cosmético; corrigir se sobrar tempo |
| 2 | Pequeno; baixa prioridade |
| 3 | Grande; alta prioridade (atrapalha a tarefa) |
| 4 | Catastrófico; corrigir antes do gate (impede a tarefa ou causa dano) |

3. Registre numa tabela: tela, heurística, problema, severidade, correção.
4. Corrija severidade 3 e 4 antes de entregar; as demais viram "decisões que merecem atenção
   do aprovador humano" no relatório final.

Modelo de registro:

| Tela | Heurística | Problema | Sev. | Correção |
|---|---|---|---|---|
| Nova reserva | H5 | Permite escolher horário passado; só falha ao enviar | 3 | Data mínima = hoje; erro no campo |
| Minhas reservas | H3 | Cancelar sem confirmar | 4 | Diálogo de confirmação |
| Salas | H1 | Sem indicador de carregamento | 2 | Estado de carregamento |

## 4. Princípios complementares úteis

| Princípio | Aplicação |
|---|---|
| **Lei de Hick** | Menos opções por tela, decisão mais rápida; agrupe e priorize |
| **Lei de Fitts** | Alvos importantes grandes e próximos; ação primária fácil de clicar |
| **Lei de Jakob** | Usuários esperam que o seu site funcione como os outros; use convenções |
| **Regra do 'até 3 cliques'** | Heurística, não lei; o que importa é cada passo ser claro e sem dúvida |
| **Divulgação progressiva** | Mostre o essencial; detalhes sob demanda |

## 5. Anti-padrões frequentes em sistemas internos

- Tabelas gigantes sem filtro nem ordenação.
- Excesso de campos obrigatórios; pedir o que o sistema já sabe (o usuário logado).
- Botão "Salvar" no rodapé de uma página longa, fora de vista.
- Dois botões primários na mesma tela.
- Rótulos técnicos (IDs, códigos) em vez de nomes.
- Modal em cima de modal.
- Mensagens inconsistentes entre telas.
- Cores como único diferenciador de estado.

## 6. Perguntas-chave por tela

1. Qual é a ação principal e ela é a mais visível?
2. O usuário sabe onde está e como voltar?
3. O que ele vê após agir?
4. O que acontece se errar? Consegue desfazer?
5. Há algo que ele precisa lembrar que a tela poderia mostrar?
6. Há algo na tela que não ajuda a tarefa?

## 7. Checklist de revisão heurística

- [ ] H1: toda ação tem retorno visível; carregamento sinalizado.
- [ ] H2: vocabulário do glossário; sem jargão técnico.
- [ ] H3: cancelar/voltar/Esc em fluxos; destrutivos confirmados ou desfazíveis.
- [ ] H4: rótulos, componentes e posições consistentes.
- [ ] H5: restrições e padrões impedem o erro previsível.
- [ ] H6: opções visíveis; instruções junto do campo.
- [ ] H7: tarefa principal em poucos passos; Enter/Esc funcionam.
- [ ] H8: sem elemento supérfluo; hierarquia clara.
- [ ] H9: erros com causa e solução, junto do campo.
- [ ] H10: ajuda contextual e contato em local consistente.
- [ ] Severidade 3 e 4 corrigidas; restante registrado.
