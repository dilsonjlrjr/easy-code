# RNF de privacidade e LGPD

Categoria (`categoria`): `privacidade_lgpd`. Base legal: **Lei Geral de Proteção de Dados
Pessoais, Lei 13.709/2018 (LGPD)**. Quem aplica e regulamenta: **ANPD**. Esta referência traduz
a lei em requisitos de sistema; **não substitui parecer jurídico**. Em dúvida jurídica, registre
como pergunta aberta ao cliente (que consulta o encarregado ou o jurídico).

Pontos que dependem de regulamentação da ANPD (prazos de comunicação de incidente, por
exemplo) mudam com o tempo: **confira a norma vigente antes de fixar prazo** e registre a
fonte.

## 1. Quando se aplica

Aplica-se a qualquer tratamento de **dado pessoal** (informação relacionada a pessoa natural
identificada ou identificável). Nome, e-mail corporativo, matrícula, endereço IP e identificador
de login são dados pessoais. Tratar "só nome e e-mail" já é tratar dado pessoal.

**Dado pessoal sensível** (origem racial/étnica, convicção religiosa, opinião política,
filiação sindical, dado de saúde, vida sexual, genético, biométrico) tem regime mais rígido
(art. 11). Se aparecer, escale para pergunta bloqueante.

## 2. Perguntas-chave

1. Que dados pessoais o sistema coleta, de quem, e **para quê** (finalidade de cada dado)?
2. Algum dado é desnecessário para as histórias? (Eliminar.)
3. Qual a **base legal** de cada tratamento? (seção 3)
4. Quem vê cada dado? Por que precisa ver?
5. Por quanto tempo guarda? O que acontece depois (apagar, anonimizar)?
6. Há compartilhamento com terceiros ou transferência internacional (nuvem, e-mail, analytics)?
7. Como o titular exerce seus direitos? Quem atende (encarregado)?
8. O que entra nos logs? Há dado pessoal neles?
9. Quem responde por incidente de segurança?

## 3. Bases legais (art. 7º; sensíveis: art. 11)

| Base | Quando faz sentido em aplicação web | Observação para requisitos |
|---|---|---|
| Consentimento | Opcional, revogável (newsletter, preferências) | Exige ato claro do titular, registrável e revogável; evite como base de dados essenciais ao serviço |
| Cumprimento de obrigação legal ou regulatória | Guarda exigida por lei | Cite a lei |
| Execução de contrato ou de procedimentos preliminares | Dados necessários ao serviço contratado pelo titular | Comum em sistemas com cliente |
| Legítimo interesse | Interesses do controlador sem prejudicar direitos do titular (ex.: segurança, antifraude) | Exige avaliação de legítimo interesse documentada |
| Exercício regular de direitos em processo | Retenção para defesa | Raro no escopo inicial |
| Proteção da vida, tutela da saúde | Casos específicos | Raros |
| Administração pública (políticas públicas) | Órgãos públicos | Específico |
| Estudos por órgão de pesquisa | Pesquisa | Específico |
| Proteção do crédito | Análise de crédito | Específico |

**Sistema interno de empresa:** o tratamento de dados dos próprios funcionários para
operar a ferramenta costuma se apoiar em execução de contrato (de trabalho) e/ou legítimo
interesse; **a escolha da base é do controlador (a empresa) com apoio jurídico**: o agente
registra a base proposta como "a confirmar".

## 4. Princípios (art. 6º) e como viram requisito

| Princípio | Requisito de sistema |
|---|---|
| Finalidade | Cada campo coletado tem finalidade registrada; sem uso, sem coleta |
| Adequação | Uso compatível com a finalidade informada |
| Necessidade (minimização) | Coletar o mínimo; nenhum campo "para o futuro" |
| Livre acesso | O titular consulta seus dados |
| Qualidade dos dados | O titular corrige dados incorretos |
| Transparência | Aviso de privacidade claro, acessível na interface |
| Segurança | Medidas técnicas (ver `seguranca-e-autorizacao.md`) |
| Prevenção | Riscos avaliados; logs sem dado pessoal desnecessário |
| Não discriminação | Dados não usados para fins discriminatórios |
| Responsabilização | Registro das operações de tratamento (art. 37) |

## 5. Direitos do titular (art. 18) como requisitos

O sistema ou o processo da empresa deve permitir:

| Direito | Requisito típico |
|---|---|
| Confirmação de tratamento e acesso | O titular vê seus dados (tela "meus dados") ou obtém cópia sob demanda |
| Correção | Dados incorretos podem ser corrigidos (pelo titular ou por solicitação) |
| Anonimização, bloqueio ou eliminação de dados desnecessários ou tratados em desconformidade | Procedimento e capacidade técnica de anonimizar/eliminar |
| Portabilidade | Exportação em formato estruturado (quando aplicável e regulamentado) |
| Eliminação dos dados tratados com consentimento | Capacidade de apagar/anonimizar quando solicitado e não houver obrigação de guarda |
| Informação sobre compartilhamento | Lista de com quem se compartilha |
| Informação sobre a possibilidade de não consentir | Aviso ao coletar com consentimento |
| Revogação do consentimento | Mecanismo simples |

Prazos de resposta: a LGPD (art. 19) fala em resposta imediata em formato simplificado ou em
até 15 dias por declaração completa para confirmação/acesso; os demais prazos dependem de
regulamentação. **Confirmar a norma vigente** e registrar no RNF como "origem: lei + norma
ANPD vigente".

**Atenção ao conflito eliminação x histórico:** apagar uma pessoa pode quebrar
integridade (reservas passadas, auditoria). Defina a estratégia: **anonimizar** (substituir
identificador por um pseudônimo irreversível) mantendo o fato de negócio. É decisão do cliente;
vira pergunta.

## 6. Minimização e finalidade: técnica prática

Monte a tabela de dados pessoais e corte o que não tem uso:

| Dado | Finalidade | US que usa | Base proposta | Quem vê | Retenção |
|---|---|---|---|---|---|
| Nome | Identificar quem reservou | US-03, US-05 | Execução do contrato de trabalho / legítimo interesse (a confirmar) | Todos os funcionários logados | Enquanto a conta existir |
| E-mail corporativo | Login e contato | US-01 | idem | O próprio, Administrador | Enquanto a conta existir |
| Matrícula | (nenhuma) | - | - | - | **Não coletar** |

Dado sem US na coluna 3 é candidato à eliminação (minimização).

## 7. Retenção

| Elemento | Requisito |
|---|---|
| Prazo por tipo de dado | Cada categoria com prazo e justificativa (ex.: reservas passadas 12 meses) |
| Ação ao fim do prazo | Eliminar ou anonimizar automaticamente (rotina agendada) |
| Exceções | Obrigação legal ou defesa em processo, com prazo |
| Logs | Prazo curto e definido (ex.: 90 dias para logs de aplicação) |
| Backups | Dados apagados saem dos backups no ciclo de expiração; declare |

Se o cliente não disse o prazo: **suposição visível**, origem "boa prática", confirmar.

## 8. Logs e privacidade

- **Nunca** registrar senha, token, cookie de sessão, chave de API.
- Não registrar dado pessoal além do necessário (identificador interno em vez de e-mail e nome).
- Dados em log de auditoria (quem fez o quê) são necessários, mas com acesso restrito e
  retenção definida.
- Mascarar quando inevitável (e-mail parcial).
- Erros e traces não devem expor dados do usuário.
- Verificação: teste que injeta valores sentinela (e-mail e senha de teste) e procura nos logs.

## 9. Segurança, incidentes e governança

| Tema | Requisito |
|---|---|
| Medidas de segurança (art. 46) | Ver RNF de segurança; vincular |
| Incidente de segurança (art. 48) | Procedimento definido para detectar, registrar e comunicar à ANPD e ao titular quando houver risco relevante, no prazo da norma vigente |
| Encarregado (art. 41) | Contato do encarregado publicado no aviso de privacidade (cliente define quem é) |
| Registro das operações (art. 37) | Inventário de tratamentos mantido (tabela da seção 6) |
| Relatório de impacto (art. 38) | Quando a ANPD solicitar ou houver alto risco; fora do escopo de aplicação pequena simples, registrar |
| Operadores e terceiros | Contrato e verificação dos que tratam dados em nome do controlador (hospedagem, e-mail) |
| Transferência internacional (arts. 33 e seguintes) | Verificar localização da hospedagem/serviços; perguntar |

## 10. Exemplos completos

Bom (acesso e visibilidade):

```yaml
categoria: privacidade_lgpd
requisito: "dado pessoal de um funcionário é visível apenas ao próprio funcionário, aos demais funcionários nos limites definidos (nome em reserva) e ao Administrador"
metrica: "o e-mail do funcionário não aparece em nenhuma resposta da API para outros funcionários; apenas o nome em reservas; 0 campos de dado pessoal fora da tabela de finalidades"
verificacao:
  tipo: teste_automatizado
  detalhe: "teste automatizado de contratos de resposta por papel + revisão da tabela de dados pessoais"
prioridade: must
origem:
  tipo: lei
  detalhe: "lei (LGPD, arts. 6º e 46) e boa prática (minimização)"
```

Bom (retenção):

```yaml
requisito: "reservas com data de término há mais de 12 meses são anonimizadas"
metrica: "100% das reservas com mais de 12 meses não contêm identificador do funcionário; rotina executa diariamente"
verificacao:
  tipo: teste_automatizado
  detalhe: "teste automatizado com dados de 13 meses; revisão da rotina agendada"
origem:
  tipo: boa_pratica
  detalhe: "boa prática (prazo proposto, a confirmar com o cliente)"
```

Ruim:

```yaml
requisito: "o sistema deve estar em conformidade com a LGPD"
metrica: "conformidade total"
verificacao:
  tipo: teste_automatizado
  detalhe: "auditoria jurídica"
```

Por quê: conformidade é resultado de vários requisitos; sem decomposição não há teste nem
responsável.

## 11. Heurísticas de especialista

- **Decomponha "LGPD" em requisitos testáveis**: finalidade, minimização, acesso, retenção,
  log, direitos do titular, incidente.
- **Não decida a base legal sozinho:** proponha, marque "a confirmar".
- **Dado que ninguém usa não se coleta.**
- **Papel justifica acesso:** "administrador vê" precisa de motivo (suporte, gestão).
- **Pseudonimizar/anonimizar** costuma ser melhor que apagar quando há obrigação de histórico.
- **Logs são a fuga mais comum.**
- **Terceiros contam:** hospedagem, e-mail transacional e analytics recebem dados.
- **Cookies:** se houver cookies não essenciais ou rastreamento, exige-se aviso e, em geral,
  consentimento; cookies estritamente necessários (sessão) não.
- **Crianças e adolescentes** (art. 14) e dado sensível mudam o jogo: pergunta bloqueante.

## 12. Anti-padrões

- "Conforme a LGPD" como RNF único.
- Coletar CPF, data de nascimento, telefone "por precaução".
- Consentimento como base de tudo (inclusive o que é essencial ao serviço).
- Prometer "exclusão total" sem tratar histórico e backup.
- Senha, token ou e-mail completo em log.
- Esquecer o aviso de privacidade e o contato do encarregado.
- Copiar prazo de retenção sem origem.

## 13. Checklist

- [ ] Tabela de dados pessoais com finalidade, US, base proposta, visibilidade, retenção.
- [ ] Cada dado tem finalidade ligada a uma US; sobras eliminadas.
- [ ] Base legal proposta e marcada "a confirmar" quando não confirmada.
- [ ] Direitos do titular (acesso, correção, eliminação/anonimização) com como atender.
- [ ] Retenção por categoria e ação ao fim do prazo.
- [ ] Logs sem senha, token e dado pessoal desnecessário; teste previsto.
- [ ] Procedimento de incidente e contato do encarregado previstos.
- [ ] Terceiros e localização de hospedagem perguntados.
- [ ] Nenhum parecer jurídico simulado: dúvidas viram perguntas ao cliente.
