# Catálogo de RNF reutilizáveis para aplicação web

Ponto de partida para o agente de Requisitos. **Não copie às cegas:** cada item tem valores
entre colchetes `[ ]` que dependem do contexto; ajuste, descarte o que não se aplica e
registre a origem real. Valores propostos aqui são **boa prática, a confirmar com o cliente**:
mantenha `origem.tipo: boa_pratica` e deixe o valor visível para o Gate.

O código do catálogo (SEG-01, DES-01...) é só para consulta; o artefato final usa `RNF-NN` do
template `agentes/templates/RNF.md` e validado por `specs/dominio/artefatos/rnf.schema.json`
(vale o esquema).

## 1. Como usar

1. Percorra as categorias; para cada item, decida **adotar, adaptar ou descartar**.
2. Substitua os `[valores]` por números do cliente, da lei ou da proposta justificada.
3. Preencha `us_afetadas` com as US afetadas.
4. Crie um arquivo `RNF-NN-<slug>.md` por item adotado (um RNF = uma métrica verificável).
5. Registre os descartes em "suposições" quando o descarte for decisão sua.

Perfil sugerido por tipo de sistema:

| Perfil | Adotar |
|---|---|
| App interno com login, poucos usuários | SEG-01..06, SEG-08, DES-01, DES-02, ACE-01..02, PRI-01..04, USA-01..02, MAN-01..02 |
| App com dados de clientes externos | Tudo acima + SEG-07, SEG-09, DIS-01..03, PRI-05..06, OBS-01..02 |
| App público/anônimo | Tudo + limitação de taxa, proteção de abuso, revisão reforçada de entrada |

## 2. Segurança (categoria: segurança)

| Código | Requisito | Métrica | Como verificar | Prior. |
|---|---|---|---|---|
| SEG-01 | Toda rota da API, exceto [login e health check], exige autenticação | 100% das rotas protegidas retornam 401 sem credencial válida | Teste automatizado que percorre a lista de rotas sem token | Must |
| SEG-02 | Autorização aplicada no servidor conforme a matriz papel x operação | 100% das células negativas da matriz retornam 403 (ou 404) | Teste automatizado por papel | Must |
| SEG-03 | Um usuário não acessa nem altera recurso de outro, salvo papel autorizado | 0 acessos a recurso alheio nos testes com dois usuários | Teste automatizado | Must |
| SEG-04 | Senhas armazenadas só como hash adaptativo com salt (argon2id, bcrypt ou scrypt) | 0 senhas em texto claro no banco e nos logs | Revisão de código + consulta ao banco de teste | Must |
| SEG-05 | Sessão expira por inatividade e o logout invalida a sessão no servidor | Expira em [30] min de inatividade; token usado após logout retorna 401 | Teste automatizado | Must |
| SEG-06 | Tráfego somente por HTTPS e cabeçalhos de segurança presentes | TLS >= 1.2; HTTP redireciona; CSP, nosniff, frame-ancestors e Referrer-Policy presentes | Varredura (ex.: OWASP ZAP baseline) | Must |
| SEG-07 | Entradas validadas e saída escapada contra injeção e XSS | 0 achados de injeção/XSS de severidade alta em varredura; 0 consultas por concatenação de texto | Revisão + análise estática (ex.: Opengrep) + varredura dinâmica | Must |
| SEG-08 | Nenhum segredo no repositório; configuração por ambiente | 0 segredos detectados | Varredura de segredos (ex.: gitleaks) | Must |
| SEG-09 | Dependências sem vulnerabilidade conhecida alta ou crítica no release | 0 vulnerabilidades alta/crítica | Varredura (ex.: Trivy, OSV-Scanner) | Should |
| SEG-10 | Proteção contra força bruta no login | Após [5] tentativas inválidas, bloqueio ou atraso por [15] min | Teste automatizado | Should |
| SEG-11 | Erros não expõem detalhes internos | 0 respostas com stack trace, SQL ou caminhos | Teste que provoca erro 500 | Must |
| SEG-12 | Operações administrativas e de alteração sensível geram registro de auditoria | 100% registram autor, ação, alvo e instante | Teste + consulta ao log | Should |

## 3. Desempenho e capacidade (categoria: desempenho)

| Código | Requisito | Métrica | Como verificar | Prior. |
|---|---|---|---|---|
| DES-01 | A operação principal de consulta responde rápido com o volume esperado | p95 <= [500] ms com [N] registros e [20] usuários concorrentes | Teste de carga (k6/Locust) em ambiente de teste com base populada | Must |
| DES-02 | Listagens são paginadas | Máximo [50] itens por resposta; sem consulta sem limite | Teste automatizado | Must |
| DES-03 | Operações de escrita respondem em tempo adequado | p95 <= [800] ms sob [20] concorrentes | Teste de carga | Should |
| DES-04 | Sem padrão N+1 nas consultas principais | No máximo [N] consultas por requisição da listagem principal | Teste que conta consultas | Should |
| DES-05 | Peso da página inicial controlado | <= [500] KB transferidos, conteúdo útil em até [2] s em 4G simulado | Lighthouse | Could |
| DES-06 | Capacidade de crescimento | Mantém DES-01 com [3x] o volume atual | Teste de carga com volume ampliado | Could |

## 4. Disponibilidade e resiliência (categoria: disponibilidade)

| Código | Requisito | Métrica | Como verificar | Prior. |
|---|---|---|---|---|
| DIS-01 | Disponível no horário de uso | >= [99,5]% de requisições sem erro 5xx por mês, em [dias úteis 7h–20h] | Monitoramento do health check/requisições | Should |
| DIS-02 | Backup e retenção | Backup completo [diário]; retenção [14] dias; RPO <= [24] h | Verificação de agendamento e do arquivo | Should |
| DIS-03 | Restauração testada | Restauração em ambiente limpo concluída em <= [4] h (RTO), a cada [release] | Teste de restauração com evidência | Should |
| DIS-04 | Health check | Endpoint responde 200 quando operante e falha se o banco está inacessível | Teste automatizado | Must |
| DIS-05 | Operações de escrita idempotentes | Repetir a mesma requisição (mesma chave) não duplica efeito | Teste automatizado | Should |
| DIS-06 | Consistência sob concorrência | 0 violações de unicidade em [N] tentativas concorrentes | Teste concorrente automatizado | Must |
| DIS-07 | Falha de dependência externa não impede função essencial | Com [e-mail] fora, a operação principal conclui e o envio é repetido depois | Teste com falha simulada | Should |

## 5. Acessibilidade (categoria: acessibilidade)

| Código | Requisito | Métrica | Como verificar | Prior. |
|---|---|---|---|---|
| ACE-01 | Telas atendem à WCAG 2.2 nível AA | 0 violações A/AA em varredura automática nas telas em escopo | axe-core integrado ao e2e | Must |
| ACE-02 | Operação apenas por teclado | 100% das funções por teclado; ordem de foco lógica; foco visível | Roteiro manual/e2e | Must |
| ACE-03 | Leitor de tela nos fluxos principais | Fluxos [entrar, criar, cancelar] concluídos com NVDA | Roteiro manual | Should |
| ACE-04 | Contraste | Texto >= 4,5:1; texto grande e componentes >= 3:1 | Medição com ferramenta de contraste | Must |
| ACE-05 | Reflow e zoom | Sem rolagem horizontal em 320 px; funciona a 200% de zoom | Teste manual | Should |

## 6. Privacidade/LGPD (categoria: privacidade/LGPD)

| Código | Requisito | Métrica | Como verificar | Prior. |
|---|---|---|---|---|
| PRI-01 | Minimização: só coleta dado com finalidade ligada a uma US | 0 campos pessoais sem finalidade na tabela de dados | Revisão da tabela de dados pessoais | Must |
| PRI-02 | Visibilidade de dado pessoal por papel | Dado fora do papel não aparece em nenhuma resposta | Teste de contrato por papel | Must |
| PRI-03 | Logs sem senha, token nem dado pessoal desnecessário | 0 ocorrências de valores sentinela nos logs | Teste com sentinelas | Must |
| PRI-04 | Retenção definida e aplicada | [Reservas > 12 meses] anonimizadas/eliminadas por rotina | Teste com dados antigos | Should |
| PRI-05 | Direitos do titular atendíveis (acesso, correção, eliminação/anonimização) | Procedimento descrito e capacidade técnica testada para cada direito | Teste + revisão do procedimento | Should |
| PRI-06 | Aviso de privacidade e contato do encarregado acessíveis | Link visível em toda tela autenticada | Revisão | Should |
| PRI-07 | Procedimento de incidente de segurança | Procedimento escrito com responsáveis e prazo da norma vigente | Revisão | Should |

## 7. Usabilidade e compatibilidade (categorias: usabilidade, compatibilidade)

| Código | Requisito | Métrica | Como verificar | Prior. | Categoria |
|---|---|---|---|---|---|
| USA-01 | Interface em pt-BR, datas dd/mm/aaaa, horas 24 h | 100% dos textos visíveis em pt-BR, 0 formatos mm/dd | Revisão + teste de formatação | Must | usabilidade |
| USA-02 | Tarefa principal curta | <= [3] passos a partir da tela inicial | Revisão do fluxo no protótipo | Should | usabilidade |
| USA-03 | Mensagens de erro acionáveis | 100% dizem o que aconteceu e como resolver | Revisão da lista de mensagens | Must | usabilidade |
| USA-04 | Ação destrutiva com confirmação | 100% das ações destrutivas pedem confirmação | Revisão + e2e | Must | usabilidade |
| COM-01 | Navegadores atuais | Duas últimas versões estáveis de [Chrome, Firefox, Edge, Safari] | e2e multi-navegador | Must | compatibilidade |
| COM-02 | Viewports | Sem rolagem horizontal de [360] a [1920] px | e2e em dois viewports | Must | compatibilidade |

## 8. Manutenibilidade e observabilidade (categoria: manutenibilidade)

| Código | Requisito | Métrica | Como verificar | Prior. |
|---|---|---|---|---|
| MAN-01 | Ambiente sobe com um comando | Clone limpo -> aplicação e banco no ar com 1 comando | Execução em máquina limpa/CI | Must |
| MAN-02 | Testes com um comando | Suíte roda com 1 comando em <= [10] min; falha retorna código != 0 | Execução em CI | Must |
| MAN-03 | Rastreabilidade CA -> teste | 100% dos CA com ao menos um teste identificado pelo id | Matriz CA x CT | Must |
| MAN-04 | Análise estática e formatação limpas | 0 erros do analisador; formatador sem diff | CI | Should |
| MAN-05 | Migrações versionadas e reaplicáveis | Aplicação em banco vazio e em banco com dados sem erro | Teste de migração | Must |
| MAN-06 | Contrato da API versionado | OpenAPI presente e validado a cada mudança | Validador em CI | Should |
| OBS-01 | Logs estruturados com correlação | 100% das linhas de uma requisição com o mesmo `request_id` | Teste automatizado | Should |
| OBS-02 | Métricas básicas por rota | Taxa, erros e latência expostas | Chamada ao endpoint de métricas | Could |

## 9. Exemplo: de catálogo a artefato

Item SEG-05 adaptado a aplicação pequena (valores do cliente ausentes, então boa prática visível):

```markdown
---
id: RNF-04
tipo: RNF
titulo: Sessão com expiração
autor: requirements-agent
onda: REQ-01
versao: 1
estado: rascunho
vinculos: []
historico:
  - {versao: 1, data: "2026-11-19", onda: REQ-01, descricao: criação}
categoria: seguranca
requisito: a sessão expira por inatividade e o logout encerra a sessão no servidor
metrica: sessão expira após 30 min de inatividade (valor proposto, a confirmar); credencial usada após o logout retorna 401
verificacao:
  tipo: teste_automatizado
  detalhe: teste automatizado com relógio controlado e reuso de credencial após logout
prioridade: must
origem:
  tipo: boa_pratica
  detalhe: OWASP ASVS; valor de 30 min a confirmar com o cliente
us_afetadas: [US-01, US-03, US-05]
---

# RNF-04 — Sessão com expiração

## Contexto
Corpo livre; nunca contradiz o YAML.
```

## 10. Anti-padrões no uso do catálogo

- Adotar todos sem avaliar o contexto (inflar o backlog de RNF).
- Deixar `[valores]` entre colchetes no artefato final.
- Declarar origem "cliente" para valor que veio do catálogo.
- Esquecer `us_afetadas`: RNF sem US ligada não é rastreável.
- Usar o catálogo no lugar de perguntar (o cliente tem restrições próprias: navegadores
  padronizados, política de senha, prazos de retenção).

## 11. Checklist

- [ ] Cada item do piso (segurança, desempenho, acessibilidade, privacidade) adotado ou
  descartado com justificativa.
- [ ] Nenhum `[valor]` pendente.
- [ ] Origem honesta (cliente, lei ou boa prática).
- [ ] `categoria` entre as oito do esquema.
- [ ] Métrica e verificação executáveis em aplicação pequena.
