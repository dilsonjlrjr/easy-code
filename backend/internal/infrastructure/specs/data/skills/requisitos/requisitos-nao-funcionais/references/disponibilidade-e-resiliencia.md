# RNF de disponibilidade e resiliência

Categoria (`categoria`): `disponibilidade` (resiliência, backup e recuperação entram aqui).
Regra de ouro: **dimensione pelo impacto no negócio, não por vaidade**. Cada "nove" a mais custa
muito; ferramenta interna de reservas não precisa de 99,99%.

## 1. Perguntas-chave

- Quando o sistema é usado: horário comercial ou 24x7? Fins de semana?
- Qual o impacto de ficar fora do ar por 1 hora? E por 1 dia? (Há contorno manual?)
- Há janelas de manutenção aceitáveis (noite, domingo)?
- Quanto de **dado** o cliente aceita perder? (Última hora? Último dia? Nada?)
- Em quanto tempo precisa voltar após uma falha?
- O sistema depende de algo externo (e-mail, diretório de usuários, pagamento)? O que fazer
  quando essa dependência cai?
- Quem é avisado quando algo cai?

## 2. Vocabulário preciso

| Termo | Significado | Cuidado |
|---|---|---|
| Disponibilidade | Fração do tempo, em uma janela, em que o serviço atende | Declare a janela (mês) e o que conta como "fora" |
| SLO | Meta interna de nível de serviço (ex.: 99,5% das requisições com sucesso em 30 dias) | Baseie em requisições com sucesso ou tempo; escolha uma |
| SLA | Compromisso contratual | Só se há contrato; num projeto interno, use SLO |
| RTO | Tempo máximo tolerado para restaurar o serviço | Depende do plano de recuperação |
| RPO | Quantidade máxima de dados que se aceita perder, em tempo | Define a frequência do backup |
| Degradação graciosa | Perder função não essencial mantendo a essencial | Defina o que é essencial |

Referência para traduzir "nove" em minutos de indisponibilidade por mês (30 dias = 43.200 min):

| Disponibilidade | Fora do ar por mês (aprox.) |
|---|---|
| 99% | 7 h 12 min |
| 99,5% | 3 h 36 min |
| 99,9% | 43 min |
| 99,99% | 4 min |

## 3. Métricas típicas

| Tema | Métrica | Como verificar |
|---|---|---|
| Disponibilidade em horário de uso | >= 99,5% das requisições com sucesso (não 5xx) por mês, no horário de 7h às 20h em dias úteis | Medição por monitoramento sintético ou métrica de requisições |
| Backup | Backup completo diário; retenção de N dias; RPO <= 24 h | Verificação do agendamento e do arquivo gerado |
| Restauração testada | Restauração do backup em ambiente limpo concluída com sucesso, em até X h (RTO), a cada release/trimestre | Teste de restauração com evidência |
| Falha de dependência | Se o serviço de e-mail estiver fora, a reserva é concluída e a notificação é reenviada depois (sem perder a reserva) | Teste automatizado simulando falha |
| Tolerância a erro transitório | Chamadas a dependências externas têm tempo limite e repetição limitada | Revisão + teste com falha simulada |
| Verificação de saúde | Endpoint de health check responde 200 quando operante e falha quando o banco está inacessível | Teste automatizado |
| Recuperação do processo | Aplicação reinicia automaticamente após falha, em até X s | Teste de matar processo |
| Manutenção | Implantação com indisponibilidade <= X min, em janela comunicada | Medição na implantação |
| Consistência na concorrência | Sob concorrência, nenhuma violação de regra de unicidade (ex.: reserva dupla) | Teste automatizado concorrente |

## 4. Exemplos completos

Bom (projeto interno):

```yaml
categoria: disponibilidade
requisito: "o sistema fica disponível no horário comercial e não perde reservas confirmadas"
metrica: "disponibilidade >= 99,5% das requisições com sucesso, em dias úteis das 7h às 20h, medida mensalmente; RPO <= 24 h (backup diário do banco com retenção de 14 dias)"
verificacao:
  tipo: medicao
  detalhe: "medição por monitoramento (verificação periódica do health check) e teste de restauração do backup em ambiente limpo a cada release"
prioridade: should
origem:
  tipo: boa_pratica
  detalhe: "boa prática (valores propostos, a confirmar com o cliente)"
```

Ruim:

```yaml
requisito: "o sistema deve estar sempre no ar (24x7, 99,999%)"
metrica: "zero downtime"
verificacao:
  tipo: teste_automatizado
  detalhe: "monitoramento"
```

Por quê: inalcançável para o contexto, sem janela de medição, sem conceito de recuperação; e o
cliente nunca pediu 24x7.

Registro de "sem requisito" aceitável:

```markdown
Suposição: uso apenas em horário comercial, sem SLA; indisponibilidade de até 4 h é tolerável
(contorno: reservar por e-mail). Impacto se errada: muda arquitetura de implantação. Confirmar: sim.
```

## 5. Resiliência: padrões a exigir pelo efeito

O RNF descreve o **comportamento**; a Arquitetura escolhe o padrão.

| Falha | Comportamento exigido | Padrões que a Arquitetura pode usar |
|---|---|---|
| Dependência externa lenta | Responder com erro claro em tempo limitado, sem travar o sistema | Tempo limite (timeout) |
| Dependência instável | Repetir com espera crescente, no máximo N vezes | Retry com backoff; operações idempotentes |
| Dependência fora | Funções essenciais continuam | Degradação graciosa; fila para notificação |
| Banco indisponível | Health check falha; mensagem amigável; sem perda de dado | Pool com limite; readiness |
| Duplo clique/reenvio | Mesma operação não duplica efeito | Idempotência |
| Concorrência | Regra de unicidade respeitada | Restrição no banco, transação |
| Pico de carga | Recusar com mensagem em vez de cair | Limite de taxa |

## 6. Heurísticas de especialista

- **Comece pelo contorno manual:** se existe contorno aceitável, o requisito é mais frouxo.
- **RPO e RTO nascem de perguntas ao cliente em linguagem de negócio** ("se o sistema voltar
  amanhã de manhã com os dados de ontem, tudo bem?").
- **Backup sem teste de restauração não é backup.** Exija a restauração.
- **Idempotência** (repetir não duplica) é o requisito de resiliência mais barato e mais valioso
  em operações de escrita.
- **Concorrência é falha de disponibilidade de dado:** regra "sem reserva duplicada" é RNF de
  consistência, verificável por teste concorrente.
- **Declare o que conta como indisponível** (5xx? timeout? lentidão acima de X?).
- **Manutenção planejada** não deve entrar no cálculo se comunicada; diga isso.
- **Aplicação pequena:** meça com health check periódico; não exija infraestrutura redundante sem
  necessidade.

## 7. Anti-padrões

- "24x7" por padrão.
- Disponibilidade sem janela e sem definição de falha.
- SLA citado sem contrato.
- Backup sem retenção, RPO ou teste de restauração.
- Esquecer a degradação quando uma dependência cai.
- Exigir "alta disponibilidade" (multi-região) para app de 200 usuários.
- Confundir disponibilidade com desempenho (lento demais também é "fora" se passa do limite;
  então declare).

## 8. Checklist

- [ ] Janela de uso e impacto de indisponibilidade perguntados (ou suposição registrada).
- [ ] Meta de disponibilidade com janela de medição e definição de falha.
- [ ] RPO e RTO declarados (mesmo que modestos) e coerentes com o backup.
- [ ] Teste de restauração previsto.
- [ ] Comportamento por dependência externa definido.
- [ ] Idempotência/concorrência tratadas para escritas críticas.
- [ ] Health check previsto.
- [ ] Valores propostos marcados como "boa prática" para aprovação.
