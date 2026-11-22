---
name: requisitos-nao-funcionais
description: Escreve requisitos não funcionais (RNF) de aplicação web com requisito, métrica e forma de verificação, cobrindo segurança e autorização, desempenho e capacidade, disponibilidade e resiliência, acessibilidade, privacidade e LGPD, usabilidade, compatibilidade, manutenibilidade e observabilidade. Traz perguntas por categoria, métricas típicas e um catálogo de RNF reutilizáveis. Use na etapa de Requisitos para escrever, calibrar ou revisar RNF, e quando uma US precisar de qualidade mensurável.
---

# Requisitos não funcionais (RNF)

Skill da etapa de **Requisitos**. Genérica: serve a qualquer aplicação web. Complementa a skill
`historias-usuario` (o que o sistema faz) com o **quão bem e sob quais restrições** ele faz.

## Princípio central

Todo RNF tem **requisito + métrica + como verificar**. Sem métrica é desejo; sem forma de
verificação é opinião. O Gate recusa RNF sem os dois, e as Etapas seguintes dependem deles:
Arquitetura escolhe estrutura pelo RNF, Dev constrói para ele, QA escreve CT e Segurança audita
contra ele.

## Formato do artefato (RNF)

Cada RNF é um arquivo `docs/requisitos/RNF-NN-<slug>.md`: cabeçalho YAML validado pelo esquema
`specs/dominio/artefatos/rnf.schema.json` + corpo livre (decisão D-01). Molde e exemplo em
`agentes/templates/RNF.md`. **Vale o esquema**; exemplo mínimo válido:

```yaml
---
id: RNF-04
tipo: RNF
titulo: Sessão com expiração
autor: requirements-agent
onda: REQ-01
versao: 1
estado: rascunho
vinculos: []                 # só relação genérica; as US vão em `us_afetadas`
historico:
  - {versao: 1, data: "2026-11-19", onda: REQ-01, descricao: criação}
categoria: seguranca
requisito: a sessão expira por inatividade e o logout encerra a sessão no servidor
metrica: expira após 30 min de inatividade; credencial usada após o logout retorna 401
verificacao:
  tipo: teste_automatizado   # teste_automatizado | varredura | revisao_manual | medicao
  detalhe: relógio controlado e reuso de credencial após o logout
prioridade: must             # must | should | could (RNF não tem wont)
origem:
  tipo: boa_pratica          # cliente | lei | norma | boa_pratica
  detalhe: OWASP ASVS; valor de 30 min a confirmar com o cliente
us_afetadas: [US-01, US-03, US-05]
---
```

O RNF é o artefato da etapa `requisitos` (gate.requisitos). Os blocos `yaml` das referências
mostram só os campos próprios do RNF (sem cabeçalho comum).

## Categoria

O campo `categoria` aceita só estes valores (snake_case):

`desempenho | disponibilidade | seguranca | privacidade_lgpd | acessibilidade | usabilidade | manutenibilidade | compatibilidade | auditabilidade | observabilidade | portabilidade | custo`

Assuntos que não têm categoria própria entram assim:

| Assunto | `categoria` |
|---|---|
| Autenticação, autorização, sessão, cabeçalhos, dependências | `seguranca` |
| Trilha de auditoria, rastro de quem fez o quê | `auditabilidade` |
| Capacidade, escalabilidade, volume | `desempenho` |
| Resiliência, backup, recuperação, degradação | `disponibilidade` |
| Idioma, i18n, fuso horário, formatos | `usabilidade` |
| Navegadores, dispositivos, resoluções | `compatibilidade` |
| Logs, métricas, alertas, rastreamento | `observabilidade` |
| Execução em outro ambiente ou servidor, independência de fornecedor | `portabilidade` |
| Orçamento, custo de infraestrutura ou de IA | `custo` |
| Operabilidade, testabilidade, build | `manutenibilidade` |

## Fluxo de trabalho

1. **Descobrir contexto de uso:** quantos usuários, em que horário, com que dispositivos, com
   quais dados (`historias-usuario/references/elicitacao-e-perguntas.md`, bloco "Escala").
2. **Percorrer as categorias** com as perguntas de cada referência. Para cada uma, decidir:
   aplica ou "sem requisito" (registrar a suposição e o porquê).
3. **Escolher métrica** proporcional ao contexto; evitar números que ninguém vai verificar nem
   precisa.
4. **Definir como verificar** (teste automatizado, varredura, revisão manual, medição) e quem
   executa (QA, Segurança).
5. **Vincular às US** afetadas (`us_afetadas`) e preencher a origem (`origem.tipo` e `detalhe`).
6. **Marcar origem** (cliente, lei, boa prática) e deixar visível o valor de boa prática para o
   cliente aprovar.
7. **Revisar** com o checklist final.

## Regras de decisão

| Situação | Decisão |
|---|---|
| Cliente deu o número | Origem "cliente"; usar o valor dele |
| Número vem de lei ou norma | Origem "lei" ou "norma", citar o nome |
| Nem cliente nem lei disseram | Propor valor, origem "boa prática", marcado para aprovação |
| Contexto interno e pequeno | Requisitos de capacidade e disponibilidade modestos; não superdimensione |
| Dado pessoal presente | Privacidade/LGPD é obrigatória, mesmo que o cliente não peça |
| Sistema com login | Segurança de autenticação e autorização é obrigatória |
| Há tela | Acessibilidade é obrigatória (assuma que o público inclui pessoas com deficiência) |
| Métrica não medível em aplicação pequena | Reescrever em condição objetiva (ex.: "revisão manual lista X") ou descartar |
| Requisito atravessa várias US | Um RNF só, com `us_afetadas` listando todas, em vez de repetir em cada CA |
| Requisito vale para uma US só | Pode ser CA da US; RNF é para o que é transversal |

## Piso mínimo para aplicação web com login e dados pessoais

Cobrir ao menos: **segurança** (autenticação, autorização, senha), **desempenho**, **acessibilidade**,
**privacidade/LGPD**. Quando houver tela, adicionar **usabilidade/compatibilidade**; para operar,
**manutenibilidade**. Disponibilidade pode ser "sem requisito além do básico" para projeto interno,
mas a suposição fica registrada. O catálogo (`references/catalogo-rnf-web.md`) tem uma lista pronta.

## Como escrever uma métrica boa

| Elemento | Regra | Exemplo |
|---|---|---|
| Grandeza | O que se mede | tempo de resposta |
| Estatística | Percentil ou máximo, não média | p95 |
| Alvo | Número com unidade | 500 ms |
| Condição | Carga, volume, ambiente | com 1.000 reservas e 20 usuários concorrentes |
| Verificação | Instrumento | teste de carga com k6 em ambiente de teste |

Modelo: "<operação> <estatística> <alvo> sob <condição>".

Para requisitos binários (segurança), a métrica é uma **condição objetiva** e a verificação
percorre o universo: "100% das rotas, exceto login e health check, retornam 401 sem token".

## Checklist de saída

- [ ] Toda categoria do piso foi tratada (RNF ou "sem requisito" justificado).
- [ ] Todo RNF tem `categoria` válida, `requisito`, `metrica`, `verificacao` (tipo e detalhe), `prioridade` e `origem` (tipo e detalhe).
- [ ] Nenhuma métrica com adjetivo (rápido, seguro, bom, leve).
- [ ] Verificação é executável por um agente (QA/Segurança) em aplicação pequena.
- [ ] Valores de "boa prática" estão visíveis para aprovação do cliente.
- [ ] `us_afetadas` com ids de US reais; RNF transversais ligados às US certas.
- [ ] Dado pessoal: finalidade, base legal, retenção e acesso definidos (ou perguntas abertas).
- [ ] Cabeçalho YAML completo e válido no esquema; `onda: REQ-<nn>`.

## Erros comuns

- "O sistema deve ser seguro": sem métrica, sem verificação.
- Copiar números de sistemas gigantes (99,99%, 10 mil rps) para um app interno.
- Métrica por média (esconde a cauda lenta).
- Verificação impossível em aplicação pequena ("auditoria externa anual").
- Confundir RNF com solução: "usar JWT" é decisão de Arquitetura; o requisito é "sessão expira
  após N min de inatividade".
- Esquecer a **negação**: segurança e autorização se testam também pelo que *não* deve acontecer.
- Misturar RNF e CA: se vale para uma US só, talvez seja CA.
- Ignorar LGPD por ser "só nome e e-mail": nome e e-mail já são dado pessoal.
- Acessibilidade como "boa prática" vaga; precisa de nível (WCAG 2.2 AA) e escopo (telas).

## Índice dos arquivos de referência

| Arquivo | Quando ler |
|---|---|
| `references/seguranca-e-autorizacao.md` | RNF de autenticação, sessão, autorização, senha, cabeçalhos, dependências, auditoria |
| `references/desempenho-e-capacidade.md` | Tempo de resposta, volume, concorrência, tamanho de página, testes de carga |
| `references/disponibilidade-e-resiliencia.md` | Janela de uso, RTO/RPO, backup, degradação, falhas de dependências |
| `references/acessibilidade.md` | RNF de acessibilidade: nível, escopo, verificação |
| `references/privacidade-lgpd.md` | Dados pessoais, bases legais, direitos do titular, minimização, retenção, logs |
| `references/usabilidade-i18n-compatibilidade.md` | Idioma, formatos, fuso, dispositivos, navegadores, aprendizagem |
| `references/manutenibilidade-operabilidade-observabilidade.md` | Build, testes, logs, métricas, health check, configuração, operação |
| `references/catalogo-rnf-web.md` | RNF prontos para copiar e adaptar, nos campos do esquema `rnf.schema.json` |
