# Modelagem de ameaças com STRIDE

Saída do modo AMEAÇAS: `docs/seguranca/ameacas.md`. Objetivo: encontrar, **antes do código**, o que
pode dar errado e garantir que o desenho (ADR, API, modelo de dados, RNF) já tem resposta.

STRIDE é uma lista de verificação por tipo de ameaça, aplicada a **elementos de um diagrama
de fluxo de dados** (processo, repositório, fluxo, entidade externa) cruzando **fronteiras de
confiança**.

## 1. Passo a passo

### Passo 1 — Delimitar escopo e ativos

- O que estamos protegendo? (ativos): dados de usuários, credenciais, integridade das reservas,
  disponibilidade do serviço, reputação.
- Quem são os atores? Legítimos (funcionário, admin) e hostis (anônimo, usuário mal-intencionado,
  atacante externo, insider, dependência comprometida).
- O que **não** está no escopo? (infra do provedor, o navegador do usuário), registrado.

### Passo 2 — Diagrama de fluxo de dados (DFD) com fronteiras

Do C4 (contexto + contêineres) derive os elementos:

| Tipo | Exemplo |
|---|---|
| Entidade externa | Usuário (navegador), serviço de e-mail |
| Processo | SPA, API, worker |
| Repositório | Banco de dados, armazenamento de arquivos, cache |
| Fluxo | HTTPS navegador -> API; API -> banco |
| **Fronteira de confiança** | Internet | rede da aplicação | rede do banco |

Fronteira de confiança = onde muda quem controla os dados ou o nível de privilégio. **A maior
parte das ameaças está nos fluxos que atravessam fronteiras.**

```
[Navegador] --(F1 HTTPS)--> |fronteira A| [Proxy/API] --(F2 SQL)--> |fronteira B| [Banco]
                                   |
                                   +--(F3 SMTP)--> [Serviço de e-mail externo]
```

### Passo 3 — Aplicar STRIDE a cada elemento e fluxo

| Letra | Ameaça | Propriedade violada | Pergunta-guia | Aplica-se a |
|---|---|---|---|---|
| **S** | Spoofing (falsificação de identidade) | Autenticidade | Alguém consegue se passar por outro usuário/serviço? | Entidades, processos, fluxos |
| **T** | Tampering (adulteração) | Integridade | Dá para alterar dado em trânsito ou no repositório sem permissão? | Fluxos, repositórios, processos |
| **R** | Repudiation (repúdio) | Não repúdio | Uma ação importante fica sem registro confiável de quem fez? | Processos, entidades |
| **I** | Information disclosure (exposição) | Confidencialidade | Dado de um usuário vaza para outro? Erros revelam detalhe? Log com segredo? | Fluxos, repositórios, processos |
| **D** | Denial of service (negação de serviço) | Disponibilidade | Uma requisição barata consome recurso caro? Falta limite? | Processos, fluxos, repositórios |
| **E** | Elevation of privilege (elevação) | Autorização | Usuário comum consegue agir como admin ou sobre dado alheio? | Processos |

Por elemento, **perguntas concretas** para web app:

| Elemento | S | T | R | I | D | E |
|---|---|---|---|---|---|---|
| Navegador -> API (F1) | credencial roubada, sessão fixa | corpo/ids adulterados, CSRF | ação sem log | resposta com dado de terceiros, erros detalhados | rajada, payload grande | mass assignment, rota admin |
| API (processo) | token forjado/sem verificação de assinatura | lógica de negócio burlada | falta auditoria | stack trace, logs com PII | consultas caras, sem paginação | checagem de dono ausente |
| API -> Banco (F2) | credencial do banco compartilhada/fraca | injeção SQL | alteração direta sem trilha | backup/dump sem criptografia | consulta sem índice, esgotar conexões | conta do banco com privilégio demais |
| Banco | acesso com credencial padrão | alteração fora da API | sem registro de quem alterou | dados em claro, backup público | disco cheio | usuário do app dono do schema |
| Serviço externo (F3) | e-mail forjado, domínio sem verificação | conteúdo alterado | envio sem registro | dado pessoal enviado a terceiro | dependência fora do ar trava a API | SSRF ao chamar URL fornecida |
| Armazenamento de arquivos | nome previsível | substituir arquivo de outro | sem registro | URL pública previsível | upload sem limite | executar arquivo enviado |

### Passo 4 — Registrar cada ameaça (tabela)

Uma ameaça = **atacante + ação + elemento + consequência**. Evite "falha de segurança" genérica.

| Id | Elemento / fluxo | STRIDE | Ameaça | Prob. | Impacto | Severidade | Mitigação proposta (no desenho) | Onde mora | Status |
|---|---|---|---|---|---|---|---|---|---|
| AM-01 | F1 / API | E | Funcionário B cancela reserva de A trocando o id na URL | Alta | Alto | Alto | Checar dono ou admin em todo acesso por id; 403/404 | API (camada de serviço); RNF-05; CT-24 | mitigada no desenho |

Colunas obrigatórias: id, elemento, STRIDE, ameaça concreta, probabilidade, impacto,
severidade, mitigação, **status** (`mitigada` | `achado` | `aceita`, ver seção 3).

### Passo 5 — Severidade

Mesma escala do ACH (Crítico, Alto, Médio, Baixo). Calcule por **probabilidade x impacto**:

| | Impacto Baixo | Impacto Médio | Impacto Alto |
|---|---|---|---|
| **Prob. Alta** | Médio | Alto | Crítico |
| **Prob. Média** | Baixo | Médio | Alto |
| **Prob. Baixa** | Baixo | Baixo | Médio |

- **Probabilidade:** Alta = qualquer usuário autenticado ou anônimo faz com ferramenta comum;
  Média = exige conhecimento/condição específica; Baixa = exige acesso privilegiado ou combinação
  improvável.
- **Impacto:** Alto = vazamento/perda de dados de vários usuários, acesso administrativo, parada;
  Médio = dado de um usuário, indisponibilidade parcial; Baixo = incômodo sem dado.
- Ajustes: dado pessoal sensível, dinheiro ou irreversibilidade **sobem** o impacto; controle
  compensatório forte **desce** a probabilidade (justificar).
- Alinhe com a tabela do ACH: Crítico = perda/vazamento de dados, acesso indevido; Alto = falha
  explorável com esforço.

### Passo 6 — Mitigação no desenho

Mitigação aceitável **nomeia o controle, onde mora e como será verificada**:

| Fraca | Forte |
|---|---|
| "Validar entradas" | "Esquema de validação no limite da API (tipo, tamanho, enum); 400 com mensagem; CT-15" |
| "Usar HTTPS" | "TLS no proxy; HSTS; cookie `Secure`; teste de cabeçalho" |
| "Controle de acesso" | "Checagem `dono == usuário || papel == admin` na camada de serviço, não no controlador; CT-24" |

Padrão de resposta por ameaça: **evitar** (remover a função), **mitigar** (controle),
**transferir** (serviço de terceiro) ou **aceitar** (decisão do aprovador humano, registrada).

### Passo 7 — Fechar o ciclo

- Alta/Crítica **sem** mitigação no desenho -> ACH com `etapa_destino: arquitetura` (id em `achado` da ameaça).
- Requisito de segurança que faltou no RNF -> ACH com `etapa_destino: requisitos`.
- Cada mitigação gera expectativa verificável: CT (QA), item de revisão (AUDITORIA) ou varredura.
- Critério de saída: toda ameaça Alta/Crítica com mitigação no desenho ou ACH aberto.

## 2. Exemplo completo — Reserva de salas (web app: SPA + API + banco)

**Escopo:** funcionários reservam salas; admin cadastra salas. Dados: e-mail, nome, reservas.
**Ativos:** credenciais, dado pessoal básico, integridade das reservas, disponibilidade.

**DFD**

```
Funcionário/Admin (navegador)
   │ F1 HTTPS (login, API REST + JWT em cookie)
═══╪═══════ fronteira A: Internet ↔ rede da aplicação ═══════
   ▼
 [Proxy reverso] ──F2──▶ [API] ──F3 SQL──▶ [PostgreSQL]
                           │                 ═ fronteira B: rede do banco ═
                           └──F4 SMTP──▶ [Serviço de e-mail] (externo)
```

**Ameaças (excerto)**

| Id | Elem. | STRIDE | Ameaça | P | I | Sev. | Mitigação (desenho) | Onde / verificação | Status |
|---|---|---|---|---|---|---|---|---|---|
| AM-01 | F1/API | E | Funcionário B lê/cancela reserva de A alterando o id | Alta | Médio | Alto (só altera reserva; se vazasse dado pessoal, impacto Alto e severidade Crítico) | Checagem de dono/admin por recurso no serviço; 404 para escopo alheio | API/serviço; CT-24; AUDITORIA A01 | mitigada no desenho |
| AM-02 | F1 | S | Força bruta no login para adivinhar senha | Alta | Alto | Crítico | Limite de tentativas por conta e IP + atraso progressivo; hash adaptativo; mensagem única | API/auth; RNF-07; CT-02/03 | **não mitigada** -> ACH-01 (Arquitetura: sem limite no desenho) |
| AM-03 | API | S | JWT com algoritmo `none`/segredo fraco aceito | Média | Alto | Alto | Algoritmo fixo na verificação; segredo >= 256 bits via variável de ambiente; expiração curta | API/auth; revisão A07 | mitigada no desenho |
| AM-04 | F1 | T | CSRF: página externa dispara cancelamento com cookie da vítima | Média | Médio | Médio | Cookie `SameSite=Lax`/`Strict` + token anti-CSRF em mutações | API/sessão; revisão A07/A02 | mitigada no desenho |
| AM-05 | API | T | Mass assignment: `{"donoId":99}` ao criar reserva | Média | Alto | Alto | DTO de entrada sem campos de propriedade; dono sempre do token | API/DTO; revisão A01/A06 | mitigada no desenho |
| AM-06 | F3 | T | Injeção SQL em filtro de busca de salas | Média | Alto | Alto | Consultas parametrizadas / ORM; lista de campos de ordenação permitidos | API/repositório; Opengrep; revisão A05 | mitigada no desenho |
| AM-07 | API | R | Admin altera sala sem trilha de quem/quando | Média | Médio | Médio | Log de auditoria (usuário, ação, id, instante) em ações de admin | API; RNF-09; revisão A09 | mitigada no desenho |
| AM-08 | API | I | Erro 500 devolve stack trace e SQL | Alta | Baixo | Médio | Handler global de erros com mensagem genérica + id de correlação | API; CT; revisão A02/A10 | mitigada no desenho |
| AM-09 | API/banco | I | Logs gravam senha/token | Média | Alto | Alto | Mascaramento; nunca logar corpo de login; revisão de logs | API; revisão A09 | mitigada no desenho |
| AM-10 | F1/API | D | Listagem sem paginação/limite derruba a API | Média | Médio | Médio | Paginação obrigatória com `limit` máximo; limite de taxa no proxy | API/proxy; teste de carga | mitigada no desenho |
| AM-11 | F4 | I | E-mail de confirmação com dado além do necessário | Baixa | Médio | Baixo | Minimização: só título e horário | API; revisão A01/A06 | aceita |
| AM-12 | Banco | E | App usa usuário do banco dono do schema | Média | Alto | Alto | Usuário de runtime sem DDL; migração com outro usuário | Infra/Compose; revisão A02 | **não mitigada** -> ACH-02 (Arquitetura) |
| AM-13 | Proxy/API | S | Credenciais padrão do banco/admin no Compose | Alta | Alto | Crítico | Segredos por variável de ambiente; sem padrão commitado; Gitleaks | Compose/README; varredura | **não mitigada** -> ACH-03 (Desenvolvimento) |

Se a dúvida for entre dois níveis de severidade, justifique em uma linha no documento.

**ACH abertos a partir do modelo**

- ACH-01 (Arquitetura): sem limite de tentativas de login no desenho. Ameaça AM-02.
- ACH-02 (DBA): usuário de banco com privilégio de DDL em runtime. AM-12.
- ACH-03 (Desenvolvimento): credenciais padrão no Compose. AM-13.
- ACH-04 (Requisitos): RNF de política de senha e bloqueio ausente. AM-02.

## 3. Estrutura do artefato AMEACAS (`ameacas.md`)

`AMEACAS` é artefato único por projeto (id fixo), com **cabeçalho YAML validado** por
`specs/dominio/artefatos/ameacas.schema.json` (molde em `agentes/templates/AMEACAS.md`; **vale o
esquema**) e corpo Markdown livre que nunca contradiz o YAML. A tabela acima é a forma legível, no
corpo; a fonte é `ameacas[]` no YAML. Campos próprios:

- `ativos`, `atores` (ao menos 1 cada) e `fluxo` (diagrama Mermaid ou caminho do arquivo).
- `ameacas[]`: `id` (`AM-n`), `fronteira`, `stride` (lista de `s|t|r|i|d|e`), `descricao` (ameaça
  concreta), `probabilidade` (`baixa|media|alta`), `impacto` (`baixo|medio|alto`), `severidade`
  (`critico|alto|medio|baixo`), `mitigacao`, `onde` (ids de ADR/RNF/T/CT) e `status`.
- `status`: `mitigada` (exige `mitigacao` e `onde` com ao menos 1 item), `achado` (exige `achado: ACH-NN`)
  ou `aceita` (decisão do aprovador humano). Correspondência com a tabela acima: "mitigada no desenho"
  = `mitigada`; "não mitigada" = `achado`; "aceita (aprovador)" = `aceita`.
- `conferencia` (campo de acompanhamento, preenchido por Segurança na auditoria): `{ameaca, implementada,
  testada_por, resultado}`; atualizável sem nova versão nem reabrir etapa.

Exemplo mínimo válido:

```yaml
---
id: AMEACAS
tipo: AMEACAS
titulo: Modelo de ameaças — Reserva de salas
autor: security-agent
onda: SEG-01
versao: 1
estado: rascunho
vinculos: []
historico:
  - {versao: 1, data: "2026-11-19", onda: SEG-01, descricao: criação}
ativos:
  - Dados pessoais dos funcionários (nome, e-mail)
  - Integridade das reservas
atores:
  - Funcionário autenticado
  - Atacante externo sem conta
fluxo: |
  flowchart LR
    navegador -->|F1 HTTPS| api
    api -->|F2 SQL| banco
ameacas:
  - id: AM-01
    fronteira: F1 / API
    stride: [e]
    descricao: Funcionário B cancela reserva de A trocando o id na URL
    probabilidade: alta
    impacto: alto
    severidade: alto
    mitigacao: Checar dono ou admin em todo acesso por id; responder 403/404
    onde: [RNF-05, T-05, CT-24]
    status: mitigada
  - id: AM-02
    fronteira: F1
    stride: [s]
    descricao: Força bruta no login para adivinhar senha
    probabilidade: alta
    impacto: alto
    severidade: critico
    onde: []
    status: achado
    achado: ACH-01
  - id: AM-11
    fronteira: F4
    stride: [i]
    descricao: E-mail de confirmação com dado além do necessário
    probabilidade: baixa
    impacto: medio
    severidade: baixo
    onde: []
    status: aceita
    achado: ACH-02      # ACH com o aceite humano
---
```

Corpo sugerido: 1. Escopo, ativos e atores; 2. Diagrama de fluxo de dados e fronteiras; 3. Ameaças
(tabela); 4. Ameaças sem mitigação (os ACH abertos); 5. Requisitos de segurança derivados (para
Requisitos/QA); 6. Premissas e fora de escopo. O REL-SEG lista as ameaças conferidas em
`ameacas_conferidas`. Etapa produtora: atividade `seguranca-ameacas` (após `arquitetura` aprovada);
aprovado no `gate.validacao`.

## 4. Heurísticas de especialista

- **Comece pelas fronteiras, não pela lista STRIDE:** onde dado entra, ameaça entra.
- **Pergunte "quem controla este dado?"** Todo dado vindo do cliente (corpo, query, cabeçalho,
  cookie, nome de arquivo, id) é hostil até validar.
- **Procure o "dono implícito":** onde o desenho assume que o cliente só manda o que a tela mostra.
- **Funções administrativas** e **fluxos de recuperação** (esqueci a senha) concentram risco.
- **Dependências externas** (e-mail, pagamento, SSO) são fronteiras: o que acontece se mentirem,
  falharem ou demorarem?
- **Ameaças de abuso de negócio:** reservar todas as salas e nunca usar, enumerar usuários,
  criar milhares de contas. STRIDE sozinho não as vê; pergunte "como alguém lucra ou prejudica
  usando a função normalmente demais?".
- **Mapeie para OWASP:** cada ameaça conhece a categoria que a auditoria usará (A01..A10).
- **Revise o modelo** quando o desenho mudar (ADR novo, endpoint novo).

## 5. Anti-padrões

| Anti-padrão | Correção |
|---|---|
| Tabela com 6 linhas, uma por letra | Ameaças concretas por fronteira e fluxo |
| Mitigação "seguir boas práticas" | Controle nomeado, local e verificação |
| Severidade sem critério | Matriz probabilidade x impacto |
| Ignorar ameaças "improváveis" sem registrar | Registrar e marcar "aceita (aprovador)" |
| Modelo escrito uma vez e esquecido | Reabrir a cada mudança de desenho; conferir na AUDITORIA |
| Ameaça sem ligação com CT/revisão/varredura | Toda mitigação com verificação |
