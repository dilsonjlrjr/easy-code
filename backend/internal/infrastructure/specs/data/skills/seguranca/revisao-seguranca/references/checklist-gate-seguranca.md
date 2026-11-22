# Checklist do gate de Segurança

Usar em dois momentos: **gate de ameaças** (fim do modo AMEAÇAS) e **gate de auditoria** (fim
do modo AUDITORIA, aprova a etapa Segurança). Severidades da **revisão**:

- **Bloqueia:** o gate não passa até corrigir.
- **Alerta:** corrigir ou justificar por escrito.
- **Nota:** melhoria.

(Não confundir com a severidade dos ACH, que descreve defeitos do produto: Crítico, Alto,
Médio, Baixo.)

## 1. Gate de ameaças

Critério oficial: "toda ameaça Alta ou Crítica com mitigação no desenho ou ACH aberto".

| # | Verificação | Severidade se falha |
|---|---|---|
| A1 | `ameacas.md` existe, com cabeçalho YAML válido no esquema `ameacas.schema.json` e `onda` `SEG-<nn>` | Bloqueia |
| A2 | Escopo, ativos e atores descritos; fora de escopo declarado | Alerta |
| A3 | Diagrama de fluxo de dados com **fronteiras de confiança** identificadas (coerente com o C4) | Bloqueia |
| A4 | Cada fronteira/fluxo analisado com as 6 letras STRIDE (ou "não se aplica" justificado) | Bloqueia |
| A5 | Ameaça é concreta (ator + ação + elemento + consequência), não genérica | Alerta |
| A6 | Probabilidade, impacto e severidade em cada ameaça, pela matriz | Bloqueia |
| A7 | Mitigação nomeia controle, onde mora e como verifica (CT/revisão/varredura) | Bloqueia para Alta/Crítica; Alerta para as demais |
| A8 | Toda Alta/Crítica **sem** mitigação no desenho tem `status: achado` e ACH com `etapa_destino: arquitetura` | Bloqueia |
| A9 | RNF de segurança ausente (senha, sessão, taxa, retenção de log, LGPD) tem ACH com `etapa_destino: requisitos` | Bloqueia se o RNF é necessário para mitigar Alta/Crítica; Alerta nos demais |
| A10 | Ameaças de abuso de negócio consideradas (não só técnicas) | Alerta |
| A11 | Dependências externas (e-mail, SSO, pagamento) tratadas como fronteiras | Alerta |
| A12 | Mapeamento para categorias OWASP:2025 (para guiar a auditoria) | Nota |
| A13 | `status` por ameaça (`mitigada`/`achado`/`aceita`); aceitas com registro do aprovador humano | Bloqueia se aceita sem aprovador |
| A14 | ACH abertos têm evidência (cenário de ataque descrito) e severidade justificada | Bloqueia |

## 2. Gate de auditoria

Critério oficial: "zero achados Críticos e Altos abertos (ou aceitos explicitamente pelo aprovador humano)".

### 2.1 Resultado

| # | Verificação | Severidade se falha |
|---|---|---|
| G1 | Zero ACH Crítico/Alto em `aberto`, `em-retrabalho`, `revalidando` ou `escalado` (exceto `aceito` pelo aprovador humano, com registro) | Bloqueia |
| G2 | `resumo` do REL-SEG bate com os ACH (contagem por severidade: `abertos`, `fechados`, `aceitos`) | Bloqueia |
| G3 | Veredito segue a regra (aprovado só se G1) | Bloqueia |
| G4 | Achados Médios/Baixos abertos listados com plano ou aceite | Alerta |

### 2.2 Varreduras

| # | Verificação | Severidade se falha |
|---|---|---|
| V1 | Opengrep, Trivy e Gitleaks rodaram; tabela do REL-SEG com versão, comando, achados, descartes | Bloqueia |
| V2 | Gitleaks cobriu o **histórico** (`git`) e os arquivos atuais (`dir`) | Bloqueia |
| V3 | Trivy cobriu dependências, configuração e a **imagem final** | Alerta (Bloqueia se há imagem de produção) |
| V4 | Regras do Opengrep de licença aberta, com origem e licença registradas | Bloqueia |
| V5 | Cada resultado triado; falso positivo com justificativa escrita | Bloqueia |
| V6 | Lockfile presente (senão Trivy cego) | Alerta |
| V7 | Saídas guardadas como evidência, sem valores de segredo | Alerta |
| V8 | Comandos do REL-SEG rodam do zero (verificado) | Bloqueia |
| V9 | Limitações das ferramentas declaradas (o que não cobrem) | Nota |

### 2.3 Revisão manual — OWASP Top 10:2025

| # | Verificação | Severidade se falha |
|---|---|---|
| M1 | As 10 categorias (A01..A10) com "Verificado = sim" e resultado de uma frase; ordem/nomes conferidos em owasp.org/Top10/2025 | Bloqueia |
| M2 | A01 com **dois usuários** em todas as operações por id e matriz de endpoints | Bloqueia |
| M3 | A01 inclui SSRF (funções que buscam URL do usuário) ou "não há" justificado | Alerta |
| M4 | A05 provou injeção/XSS nos pontos de entrada principais (ou "sem achado" com método) | Bloqueia |
| M5 | A07: hash, sessão/JWT, cookies, CSRF, limite de tentativas conferidos | Bloqueia |
| M6 | A02: cabeçalhos, CORS, erros, consoles, Compose, config de produção | Bloqueia |
| M7 | A09: eventos de segurança registrados; sem segredo no log | Alerta |
| M8 | A10: falha fechada, erro sem vazamento, estado consistente | Alerta |
| M9 | A06: mitigações de `ameacas.md` conferidas no código (implementada? testada?) | Bloqueia |
| M10 | A03/A08: lockfile, pipeline, integridade (SRI, desserialização) | Alerta |

### 2.4 Qualidade dos achados e do relatório

| # | Verificação | Severidade se falha |
|---|---|---|
| Q1 | Todo achado é um ACH separado com **evidência reproduzível** | Bloqueia |
| Q2 | Evidência mínima convincente (efeito provado, causa localizada) | Bloqueia |
| Q3 | Severidade justificada; ajustes explicados | Alerta |
| Q4 | Destino coerente (Dev/Arquitetura/Requisitos) | Alerta |
| Q5 | Sem segredo real, dado pessoal real ou ataque fora do ambiente local | Bloqueia |
| Q6 | Mitigações do modelo de ameaças: `conferencia` por ameaça (`implementada`, `testada_por`, `resultado`) | Bloqueia |
| Q7 | Ameaças novas encontradas na auditoria listadas | Alerta |
| Q8 | Cabeçalho YAML completo; `onda` `SEG-<nn>` | Bloqueia |
| Q9 | Achados revalidados por quem emitiu; tabela de ciclos preenchida | Bloqueia |
| Q10 | Sem ACH por suspeita | Bloqueia |

## 3. Conduzindo a revisão

1. **Reconte** o resumo a partir dos arquivos ACH (G2).
2. **Reproduza 3 achados** (um de cada tipo: acesso, injeção/config, varredura) com a evidência
   escrita; se a reprodução falha, Q2 bloqueia.
3. **Reexecute uma varredura** com o comando do relatório do zero (V8).
4. **Teste surpresa de A01:** escolha um endpoint por id fora dos já testados e repita com
   dois usuários; achar falha ali revela cobertura incompleta (M2).
5. **Compare** `ameacas.md` x código para 3 ameaças Altas/Críticas (M9).
6. **Pergunte:** "o que as ferramentas não cobrem e quem olhou?" (V9, M1).

## 4. Modelo de resultado

```markdown
## Revisão do gate — SEG (auditoria)
Veredito da revisão: reprovado (2 bloqueios)

Bloqueios
- M2: matriz de A01 não cobre `GET /reservas/{id}/participantes`; falha reproduzida (B lista participantes de A).
- V2: Gitleaks rodado só com `dir`; faltou `git` (histórico).

Alertas
- M3: SSRF não mencionado; função de importar por URL existe (app/import.py:20).

Notas
- A12: mapear AM-xx às categorias facilita a auditoria.
```

## 5. Armadilhas do revisor

- Aprovar porque "as ferramentas passaram": elas não veem lógica de acesso.
- Aceitar "sem achado" sem método descrito.
- Confiar no número do resumo sem recontar.
- Tolerar descarte de falso positivo sem texto.
- Deixar passar ACH sem evidência "porque o risco é óbvio".
- Aprovar com Crítico/Alto `aceito` sem registro do aprovador humano.
- Esquecer que correção que muda decisão aprovada reabre o gate de Arquitetura.
