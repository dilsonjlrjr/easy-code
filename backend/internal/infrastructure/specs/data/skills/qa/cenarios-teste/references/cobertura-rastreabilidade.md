# Cobertura de critérios e rastreabilidade

O gate de QA tem uma pergunta central: "todo critério de aceite de US Must tem CT passando?".
Esta referência mostra como responder com evidência.

## 1. Cadeia de rastreabilidade

```
US-NN ── CA-N ──< CT-NN ── teste automatizado (arquivo::nome) ── resultado ── ACH-NN
RNF-NN ─────────< CT-NN (nível "não funcional")
```

- Vínculo por **id**, nunca por título (convenção dos templates).
- CT: campo `cobre: {us: US-NN, criterio: CA-N}` (ou `{rnf: RNF-NN}`) obrigatório, no esquema `ct.schema.json`.
- REL-QA (`rel-qa.schema.json`): `matriz` repete `us | criterio | ct | nivel | resultado | achado` por linha.
- Um CA pode ter vários CT; um CT cobre **um** comportamento. CT que cobre dois CA é sinal de
  CT grande demais, salvo quando o CA é literalmente o mesmo comportamento.

## 2. Construindo a matriz de cobertura

Passo a passo (modo CENÁRIOS):

1. Extraia de cada US a lista de CA (ids `CA-1..n`) e a prioridade (Must/Should/Could).
2. Crie uma linha por CA. Preencha as colunas de CT à medida que escreve.
3. Marque o **tipo mínimo exigido** por CA:
   - todo CA: positivo;
   - CA com regra de negócio (decisão, limite, cálculo, estado): + negativo + limite;
   - CA sobre acesso: + negativo de autorização;
   - CA com unicidade/saldo/vaga: + concorrência.
4. Ao terminar, procure **células vazias** e **CA sem positivo**.

**Exemplo de matriz (cenários)**

| US | Pri | CA | Regra? | Positivo | Negativo | Limite | Segurança | Concorrência |
|---|---|---|---|---|---|---|---|---|
| US-03 | Must | CA-1 reservar livre | sim | CT-07 | CT-15 (campo faltando) | CT-09 | CT-16 (sem login) | - |
| US-03 | Must | CA-2 conflito recusado | sim | CT-08 | CT-11 | CT-10 | - | CT-17 |
| US-03 | Must | CA-3 só horário comercial | sim | CT-18 | CT-19 | CT-20 (08:00 e 18:00) | - | - |
| US-05 | Must | CA-1 cancelar a própria | sim | CT-21 | CT-22 (já cancelada) | CT-23 (prazo) | CT-24 (de outro) | - |
| US-09 | Should | CA-1 listar minhas | não | CT-30 | - | CT-31 (lista vazia) | CT-32 | - |

Célula "-" precisa de justificativa ("não aplicável: sem regra de unicidade"), nunca de
silêncio.

## 3. Cobertura que importa (e a que engana)

| Medida | O que diz | Limite |
|---|---|---|
| **CA com CT** | Todo critério tem teste | Não diz se o teste é bom |
| **CA com CT passando** | O critério vale hoje | Depende de CT bem escrito |
| **Regras de negócio cobertas** (cada regra da tabela de decisão) | Cobertura de lógica | Exige a tabela feita |
| **Cobertura de código** (linhas/ramos) | Código executado por algum teste | Não prova asserção; 100% com testes sem `expect` é inútil |
| **Cobertura de mutação** (ferramentas por linguagem, ex.: Stryker, PIT, mutmut — conferir em `agentes/ambiente.md`) | Teste detecta bug injetado | Lenta; use em módulo crítico |

Regra de especialista: cobertura de código serve para achar **código não exercitado** (pergunte
"qual CA exigiria esse ramo?"), não para declarar qualidade. Reporte-a no REL-QA se medida, sem
usá-la como critério de aprovação. O gate é por CA.

## 4. Encontrando buracos

| Sinal | Pergunta |
|---|---|
| CA sem negativo | O que acontece quando a regra é violada? |
| Estado sem transição inválida testada | Posso fazer isso em estado errado? |
| Endpoint na API/contrato sem CT | Para qual CA existe esse endpoint? (CA faltando ou endpoint sobrando) |
| Papel da matriz de acesso sem CT | Esse papel foi testado em algum recurso de outro? |
| RNF sem CT | É verificável? Se sim, falta CT; se não, ACH para Requisitos |
| CT sem `cobre` | Remover ou achar o CA que justifica |
| CT que só roda via e2e mas é regra pura | Descer de nível |

**Rastreie também na direção inversa:** liste endpoints/telas do contrato (OpenAPI da
Arquitetura) e marque a qual CT cada um pertence. Endpoint sem CT é bug de cobertura ou
funcionalidade sem requisito (ambos viram ACH).

## 5. Critérios sem cobertura no REL-QA

A seção "Critérios sem cobertura (obrigatório, mesmo se vazio)" lista:

| Caso | Registro |
|---|---|
| CA sem CT | US/CA + motivo + ACH se for falta nossa |
| CT bloqueado (ambiente, dependência) | CT + motivo + o que desbloqueia |
| CT intermitente não estabilizado | CT + frequência observada (conta como falhou) |
| CA não testável | ACH com `etapa_destino: requisitos` aberto |

Se a seção está vazia, escreva "Nenhum" explicitamente. Vazio sem texto é interpretado como
esquecimento.

## 6. Números do resumo (como contar)

- **CT executados:** os que rodaram até o fim (passou ou falhou); bloqueado não conta.
- **CA de US Must cobertos por CT passando: x de y:** y = total de CA das US Must; x = CA em
  que **todos** os CT obrigatórios (positivo/negativo/limite) passaram. CA com um CT falhando
  não conta.
- **Achados abertos:** por severidade, só `aberto`, `em_retrabalho`, `revalidando` e
  `escalado` (campo `achados_abertos` do REL-QA).
- **Veredito aprovado** somente se x = y **e** zero Crítico/Alto abertos (esquema `rel-qa.schema.json`; `veredito: aprovado | reprovado`).

## 7. Exemplo de linha ruim e boa na matriz

Ruim:

| US-03 | CA-2 | CT-08 | integração | ok | — |

("ok" não é resultado do vocabulário `passou | falhou | bloqueado | nao_executado`.)

Bom:

| US-03 | CA-2 | CT-08 | integração | falhou | ACH-02 |

## 8. Manutenção em retrabalho

Quando um ACH volta ao QA para revalidação:

1. Reexecute o CT que gerou o achado **e os CT vizinhos** da mesma regra (regressão local).
2. Atualize a linha da matriz e o campo `execucao` do CT; o ciclo do ACH (`ciclos`) é registrado conforme `specs/dominio/achado.md`.
3. Se o CA mudou (retrabalho em Requisitos), suba `versao` do CT e registre em `historico`;
   não apague o CT antigo, marque a mudança.
