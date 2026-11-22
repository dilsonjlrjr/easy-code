---
name: revisao-seguranca
description: Como modelar ameaças (STRIDE) e auditar a segurança de uma aplicação web - revisão manual por categoria do OWASP Top 10:2025, autenticação e sessão, controle de acesso (IDOR/BOLA), varreduras open source (Opengrep, Trivy, Gitleaks) com triagem de falsos positivos, cabeçalhos e CORS, contêiner e configuração, escrita de ACH de segurança com evidência e checklist do gate. Use nos modos AMEAÇAS (após a Arquitetura) e AUDITORIA (no fim) da etapa Segurança.
---

# Revisão de segurança

A etapa Segurança tem dois momentos: no **desenho** (modo AMEAÇAS: o que pode dar errado e
onde o desenho já se defende) e no **código** (modo AUDITORIA: o que realmente está
explorável). Achado sem evidência não existe. Vale para qualquer aplicação web; exemplos de
reserva de salas são só ilustração.

## Princípios

1. **Evidência antes de achado.** Um ACH de segurança traz a requisição, o trecho de código ou
   a saída da ferramenta que prova. Suspeita sem prova não vira achado.
2. **Desenho primeiro.** Falha de desenho (A06) custa mais que falha de código; o modo
   AMEAÇAS existe para pegá-la antes da construção.
3. **Controle de acesso é o risco nº 1 em aplicações com dados de usuários.** Em cada endpoint:
   **quem** é você (autenticação) e **pode** agir sobre **este** recurso (autorização por
   recurso).
4. **Ferramenta acha candidatos, humano decide.** Varredura gera ruído; triagem com
   justificativa escrita separa real de falso positivo.
5. **Falhe fechado.** Na dúvida (erro, token ausente, regra não casou), negar.
6. **Defesa em profundidade, mas sem teatro.** Cada mitigação nomeia a ameaça que cobre.
7. **Ataque de prova só no ambiente local do projeto** (Docker Compose em `app/`). Nunca contra
   qualquer outro alvo.
8. **Você relata, o Dev corrige.** Quem emitiu o ACH revalida depois.
9. **Só ferramentas open source** (lista em `agentes/README.md`).

## Formato dos artefatos (AMEACAS, REL-SEG, ACH)

Cada artefato é Markdown com **cabeçalho YAML validado por JSON Schema** (decisão D-01) + corpo livre
que nunca contradiz o YAML. **Vale o esquema**; moldes em `agentes/templates/`:

| Artefato | Arquivo | Esquema | Detalhe |
|---|---|---|---|
| AMEACAS | `docs/seguranca/ameacas.md` | `ameacas.schema.json` | `references/modelagem-ameacas-stride.md`, seção 3 |
| REL-SEG | `docs/seguranca/REL-SEG-NN.md` | `rel-seg.schema.json` | exemplo abaixo |
| ACH | `docs/achados/ACH-NN.md` | `ach.schema.json` | `references/escrever-ach-seguranca.md` |

Achado: `severidade` (`critico | alto | medio | baixo`), `categoria` (segurança usa `vulnerabilidade`; falha
de desenho, `desvio_arquitetura`), `etapa_destino` (`requisitos | arquitetura | dados | design |
implementacao`), `evidencia` obrigatória (sem evidência o achado é descartado), ver `specs/dominio/achado.md`.

REL-SEG: `resumo` (por severidade: `abertos`, `fechados`, `aceitos`), `varreduras` (ao menos as 3
ferramentas), `owasp` (as 10 categorias `A01` a `A10`, todas com `verificado: true`), `ameacas_conferidas`,
`falsos_positivos`, `comandos` e `veredito` (`aprovado` só com Crítico e Alto sem abertos). Exemplo
mínimo válido:

```yaml
---
id: REL-SEG-01
tipo: REL-SEG
titulo: Relatório de Segurança — execução 01
autor: security-agent
onda: SEG-01
versao: 1
estado: rascunho
vinculos: []
historico:
  - {versao: 1, data: "2026-11-19", onda: SEG-01, descricao: criação}
execucao: 1
resumo:
  critico: {abertos: 0, fechados: 0, aceitos: 0}
  alto: {abertos: 1, fechados: 0, aceitos: 0}
  medio: {abertos: 0, fechados: 0, aceitos: 0}
  baixo: {abertos: 0, fechados: 0, aceitos: 0}
varreduras:
  - {ferramenta: opengrep, versao: "saída de opengrep --version", comando: "opengrep scan --config ./regras-abertas/ .", achados: [ACH-04], falsos_positivos: 5}
  - {ferramenta: trivy, versao: "saída de trivy --version", comando: "trivy fs --scanners vuln,secret,misconfig .", achados: [], falsos_positivos: 2}
  - {ferramenta: gitleaks, versao: "saída de gitleaks version", comando: "gitleaks git .", achados: [], falsos_positivos: 1}
owasp:
  - {categoria: A01, verificado: true, resultado: "controle de acesso testado com dois usuários; 1 achado", achados: [ACH-04]}
  - {categoria: A02, verificado: true, resultado: "verificado, sem achado", achados: []}
  - {categoria: A03, verificado: true, resultado: "verificado, sem achado", achados: []}
  - {categoria: A04, verificado: true, resultado: "verificado, sem achado", achados: []}
  - {categoria: A05, verificado: true, resultado: "verificado, sem achado", achados: []}
  - {categoria: A06, verificado: true, resultado: "verificado, sem achado", achados: []}
  - {categoria: A07, verificado: true, resultado: "verificado, sem achado", achados: []}
  - {categoria: A08, verificado: true, resultado: "verificado, sem achado", achados: []}
  - {categoria: A09, verificado: true, resultado: "verificado, sem achado", achados: []}
  - {categoria: A10, verificado: true, resultado: "verificado, sem achado", achados: []}
ameacas_conferidas: [AM-01]
falsos_positivos:
  - {ferramenta: opengrep, regra: sql-concatenado-python, local: "app/db/relatorio.py:41", justificativa: "A parte concatenada é a constante TABELA_RELATORIO, nunca alterada por entrada externa."}
comandos:
  - opengrep scan --config ./regras-abertas/ .
  - trivy fs --scanners vuln,secret,misconfig .
  - gitleaks git .
veredito: reprovado
---
```

## Regras de decisão

| Situação | Decisão | Referência |
|---|---|---|
| Início, com C4/contrato/modelo de dados em mãos | Modo AMEAÇAS: STRIDE por fronteira e fluxo | modelagem-ameacas-stride |
| Código pronto | Modo AUDITORIA: varreduras + revisão manual das 10 categorias | varreduras-opensource, owasp-top10-2025 |
| Endpoint que lê/altera dado por id | Testar IDOR/BOLA com 2 usuários | controle-acesso |
| Login, senha, token, cookie | Revisão de autenticação e sessão | autenticacao-sessao |
| Resposta HTTP, CORS, cookies | Cabeçalhos e CORS | cabecalhos-cors |
| Dockerfile, Compose, variáveis, imagem | Contêiner e configuração | conteiner-configuracao |
| Resultado de ferramenta | Triagem: alcançável? entrada controlada? já mitigado? | varreduras-opensource |
| Achado confirmado | ACH com evidência mínima | escrever-ach-seguranca |
| Fechar a etapa | Checklist do gate | checklist-gate-seguranca |
| Ameaça Alta/Crítica sem mitigação no desenho | ACH com `etapa_destino: arquitetura` | modelagem-ameacas-stride |
| Requisito de segurança que faltou no RNF | ACH com `etapa_destino: requisitos` | modelagem-ameacas-stride |

## Fluxo de trabalho

### Modo AMEAÇAS

1. Ler C4 (contexto e contêineres), contrato da API, modelo de dados, ADR e RNF de segurança.
2. Desenhar/ajustar o **diagrama de fluxo de dados** com **fronteiras de confiança**.
3. Para cada fronteira e fluxo, aplicar STRIDE; listar ameaças.
4. Pontuar probabilidade x impacto -> severidade (mesma escala do ACH).
5. Propor mitigação **no desenho** (controle concreto, onde mora, quem implementa) e ligar a
   RNF/ADR/CT.
6. Abrir ACH: `etapa_destino: arquitetura` (Alta/Crítica sem mitigação; ameaça com `status: achado`) e `requisitos` (RNF faltando).
7. Escrever `docs/seguranca/ameacas.md` (artefato AMEACAS) e conferir o critério de saída.

### Modo AUDITORIA

1. Subir o ambiente local limpo; registrar versões das ferramentas.
2. Rodar Opengrep, Trivy, Gitleaks (comandos no REL-SEG).
3. Triar cada resultado (real ou falso positivo, sempre com justificativa).
4. Revisão manual das 10 categorias OWASP:2025 com foco em controle de acesso.
5. Conferir as mitigações de `ameacas.md` no código (implementada? testada?).
6. Provar localmente cada achado e abrir ACH com evidência mínima.
7. Escrever o REL-SEG (esquema `rel-seg.schema.json`) com `varreduras`, `owasp`, `ameacas_conferidas`, `falsos_positivos` e `comandos`; atualizar `conferencia` do AMEACAS.
8. Revisar com o checklist do gate.

## Checklist de saída (resumo; completo em `checklist-gate-seguranca.md`)

- [ ] `ameacas.md` (AMEACAS) válido no esquema: fronteiras, fluxo, ameaças STRIDE, severidade, mitigação, `status`.
- [ ] Toda ameaça Alta/Crítica com mitigação no desenho **ou** ACH aberto.
- [ ] Três varreduras executadas, com versão e comando; resultados triados.
- [ ] 10 categorias OWASP:2025 com "verificado" e resultado (inclusive "sem achado").
- [ ] Controle de acesso testado com dois usuários por recurso/operação.
- [ ] Todo achado = um ACH com evidência reproduzível e destino correto.
- [ ] Falsos positivos descartados com justificativa escrita.
- [ ] Zero Crítico/Alto abertos (ou aceitos pelo aprovador humano no ACH).
- [ ] REL-SEG com cabeçalho YAML válido no esquema e comandos que rodam do zero.

## Erros comuns

| Erro | Efeito | Correção |
|---|---|---|
| Abrir ACH por "parece vulnerável" | Ruído; achado descartado no gate | Provar ou não abrir |
| Colar relatório inteiro da ferramenta | Achado ilegível | Evidência mínima: 1 trecho + 1 requisição |
| Conferir só login, não dono do recurso | IDOR passa | Dois usuários, todas as operações |
| Descartar falso positivo sem texto | Não auditável | Justificativa escrita por regra/arquivo |
| Modelo de ameaças só lista categorias STRIDE | Sem ação | Ameaça concreta, severidade, mitigação, responsável |
| Severidade pelo "medo" | Inconsistência | Probabilidade x impacto com a tabela do ACH |
| Confiar em validação do front | Bypass trivial | Testar a API direto |
| Esquecer histórico do git no Gitleaks | Segredo antigo vaza | `gitleaks git` além de `dir` |
| Rodar ataque fora do ambiente local | Risco legal e operacional | Só Docker Compose do projeto |
| Corrigir o código | Sai do papel; Dev perde a rastreabilidade | Relatar com correção sugerida |

## Índice dos references

| Arquivo | Ler quando |
|---|---|
| `references/modelagem-ameacas-stride.md` | Modo AMEAÇAS: passo a passo, tabela, severidade, exemplo completo de web app |
| `references/owasp-top10-2025.md` | Modo AUDITORIA: o que procurar, como testar e código vulnerável x corrigido por categoria |
| `references/autenticacao-sessao.md` | Hash de senha, JWT x sessão, cookies, CSRF, força bruta, recuperação de senha |
| `references/controle-acesso.md` | IDOR/BOLA, checagem por recurso, mass assignment, testes com dois usuários |
| `references/varreduras-opensource.md` | Rodar e ler Opengrep, Trivy, Gitleaks; triagem de falsos positivos |
| `references/cabecalhos-cors.md` | Cabeçalhos HTTP de segurança, CSP, cookies, CORS |
| `references/conteiner-configuracao.md` | Dockerfile, Compose, segredos, configuração de produção x desenvolvimento |
| `references/escrever-ach-seguranca.md` | Redigir ACH com evidência mínima convincente, severidade, destino |
| `references/checklist-gate-seguranca.md` | Fechar a etapa: checklist com severidade da revisão |

Skills relacionadas: `requisitos-nao-funcionais` (RNF de segurança e LGPD) e `cenarios-teste`
(CT de autorização feitos por QA).

**Atenção a referências externas:** a lista OWASP Top 10:2025 deve ser conferida em
owasp.org/Top10/2025 antes da primeira revisão (ordem, nomes e mapeamentos).
