# Priorização e backlog

O backlog (`BACKLOG.md`) diz **em que ordem** construir e **o que foi assumido**. Prioridade
mal feita desperdiça Ondas: o Dev entrega o que não importa primeiro.

## 1. MoSCoW

Campo `Prioridade` da US: `Must | Should | Could | Won't`.

| Nível | Critério de decisão | Pergunta-teste |
|---|---|---|
| **Must** | Sem isto o sistema não cumpre o objetivo da ideia; entrega inútil se faltar; obrigação legal | "Se só isto for entregue, o cliente ainda usa?" Se sim, é Must |
| **Should** | Importante; há contorno manual temporário | "Existe uma gambiarra aceitável por um tempo?" |
| **Could** | Melhora a experiência; corte sem dor | "Alguém reclamaria se faltar na primeira versão?" Se não, Could |
| **Won't (agora)** | Fora do escopo desta entrega, registrado para não ser esquecido nem reinventado | Entra com justificativa e, se possível, condição para voltar |

Regras de aplicação:

1. **Must é o mínimo viável e não o desejável.** Tudo Must = nenhuma priorização.
2. **Teto de Must:** no máximo ~60% do esforço estimado total em Must (regra prática do
   MoSCoW); o restante dá folga para imprevisto e para Should.
3. **Won't não é lixo:** é decisão explícita. Cada Won't traz o motivo (custo, dependência,
   não pedido).
4. **Dependência técnica não vira Must de graça:** se a US-B (Should) é pré-requisito de uma
   Must, a ordem de construção muda, mas a prioridade de negócio permanece; registre
   "Depende de".
5. **Uma US pode ter partes de prioridades diferentes:** divida (ver `escrita-de-historias.md`).
6. **RNF Must** (segurança, LGPD) valem como Must independentemente da US.

## 2. Valor x esforço (para desempatar dentro do mesmo nível)

Dentro de Must/Should, ordene por valor/esforço. Use escala simples; não finja precisão.

| Valor (para o cliente) | Esforço (relativo) |
|---|---|
| 3 = objetivo central | S = pequeno (1 Onda curta) |
| 2 = importante | M = médio |
| 1 = conveniência | L = grande (divida se possível) |

Quadrantes:

| | Esforço baixo | Esforço alto |
|---|---|---|
| **Valor alto** | Fazer primeiro | Fazer; considerar dividir para entregar parte cedo |
| **Valor baixo** | Fazer se sobrar | Descartar ou Won't |

O esforço é estimativa de ordem de grandeza (S/M/L), declarada como suposição. Quem mede de
verdade é a Arquitetura ao criar as T.

## 3. Outras técnicas (quando o MoSCoW não basta)

| Técnica | Quando | Cuidado |
|---|---|---|
| **Kano** (básico, desempenho, encantador) | Decidir o que o cliente nem pediu mas esperaria | Básico ausente destrói satisfação; já cai como Must |
| **Custo do atraso** | Há prazo externo (lei, evento) | Peça ao cliente a data real |
| **Risco primeiro** | Há incerteza técnica ou de regra | Sobe a ordem de construção, não a prioridade de negócio |
| **Fatia mínima de ponta a ponta (walking skeleton)** | Começar validando o fluxo inteiro | A primeira Onda atravessa login, reservar e listar, ainda que simples |

Para aplicação pequena, MoSCoW + valor×esforço bastam.

## 4. Mapa de histórias (story map)

Útil para ver se o conjunto de US conta uma jornada completa e se o corte (release) faz
sentido.

Estrutura: eixo horizontal = **jornada do ator** (atividades em ordem); eixo vertical =
**prioridade** (o essencial no topo).

```
Jornada      | Entrar     | Encontrar sala        | Reservar         | Acompanhar    | Administrar
-------------+------------+-----------------------+------------------+---------------+-----------------
Release 1    | US-01      | US-02 listar por      | US-03 reservar   | US-05 minhas  | US-07 cadastrar
(Must)       | login      | data/horário          | US-04 cancelar   | reservas      | sala
Release 2    |            | US-08 filtrar por     | US-09 alterar    |               | US-10 cancelar
(Should)     |            | recurso               | horário          |               | qualquer reserva
Depois       |            |                       | lembrete e-mail  |               |
(Could/Won't)|            |                       |                  |               |
```

Como usar:

1. Liste as atividades da jornada de cada ator, em ordem.
2. Pendure as US embaixo da atividade correspondente.
3. Trace a linha do release: o que está acima dela forma um fluxo **completo** (sem buraco na
   jornada); se faltar uma atividade, a entrega é incompleta, suba a US mínima.
4. Se uma coluna ficou vazia, falta US ou a atividade não importa.

No corpo do BACKLOG, o mapa pode ser tabela Markdown como acima.

## 5. Formato do BACKLOG

O backlog é o artefato `BACKLOG` (arquivo `docs/requisitos/BACKLOG.md`, um por projeto):
cabeçalho YAML validado por `specs/dominio/artefatos/backlog.schema.json` + corpo livre com a
tabela legível (molde em `agentes/templates/BACKLOG.md`). **Vale o esquema.** Campos próprios:

- `historias`: lista **em ordem de execução** (ao menos 1), cada item `{ordem, us, prioridade:
  must|should|could (`wont` não entra aqui), valor: alto|medio|baixo, esforco: p|m|g, observacao}`, com os
  opcionais `epico` (EP-nn, de `epicos`), `fase` (1 a 4), `marco` (MARCO-n, de `marcos`) e `depende_de`
  (lista de US que vêm antes; respeite-a na `ordem`).
- `epicos` e `marcos` (opcionais): `[{id: EP-nn, nome}]` e `[{id: MARCO-n, nome, criterio}]`.
- `rnf`: `[{rnf, prioridade}]` com os RNF do projeto.
- `suposicoes`: obrigatório, `[]` se não houver; cada uma `{id: S-n, descricao, motivo}`.
- `adiadas`: US Won't (`prioridade: wont` na US) registradas fora do corte, `{us, motivo}` (`prioridade`, se
  presente, só `wont`).
- Toda US do projeto aparece em `historias` **ou** em `adiadas`.

Exemplo mínimo válido (cabeçalho do arquivo):

```yaml
---
id: BACKLOG
tipo: BACKLOG
titulo: Backlog — Reserva de salas
autor: requirements-agent
onda: REQ-01
versao: 1
estado: rascunho
vinculos: []
historico:
  - {versao: 1, data: "2026-11-19", onda: REQ-01, descricao: criação}
historias:
  - {ordem: 1, us: US-01, prioridade: must, valor: alto, esforco: p, observacao: ""}
  - {ordem: 2, us: US-02, prioridade: must, valor: alto, esforco: m, depende_de: [US-01]}
  - {ordem: 3, us: US-03, prioridade: must, valor: alto, esforco: m, depende_de: [US-01, US-02]}
rnf:
  - {rnf: RNF-01, prioridade: must}
  - {rnf: RNF-02, prioridade: must}
suposicoes:
  - {id: S-1, descricao: Reserva dentro de um único dia, motivo: "o cliente não citou reservas de vários dias; muda a regra de conflito se estiver errada"}
adiadas:
  - {us: US-09, motivo: Lembrete por e-mail não foi pedido e exige integração, prioridade: wont}
---
```

No corpo Markdown, a tabela de apoio (a fonte é o YAML; o corpo nunca o contradiz):

```markdown
| Ordem | US | Título | Prioridade | Valor | Esforço | Observação |
|---|---|---|---|---|---|---|
| 1 | US-01 | Entrar no sistema | Must | Alto | P | |
| 2 | US-02 | Ver salas disponíveis | Must | Alto | M | depende de US-01 |
```

Perguntas não bloqueantes abertas ficam em `perguntas_abertas` da US correspondente, não no
backlog. O histórico de versões fica no campo `historico` do cabeçalho (o corpo não tem seção
"Histórico").

## 6. Ordem de construção versus prioridade

- **Prioridade (MoSCoW)** = importância de negócio.
- **Ordem** = sequência que respeita dependências e risco.

Regras para o campo `ordem`:

1. Fundação funcional primeiro (login, cadastro base): sem ela nada se demonstra.
2. Dentro de Must, por dependência, depois por valor/esforço.
3. A primeira Onda deve entregar algo demonstrável de ponta a ponta.
4. Risco alto (concorrência, regra complexa) cedo: descobrir errado tarde é caro.

## 7. Tamanho do backlog em aplicação pequena

Para aplicação pequena, o alvo é de 5 a 10 US. Se a ideia gerar mais:

1. Funda US de baixo valor ou divida por release (release 1 Must, release 2 Should).
2. Mova o excedente para Won't com motivo "fora do escopo inicial".
3. Mostre o corte ao cliente como proposta, não como fato.

## 8. Anti-padrões

- **Tudo Must.**
- **Prioridade por barulho** (quem pediu por último/mais alto).
- **Prioridade técnica disfarçada:** "Must porque é a base" sem dizer que é dependência.
- **Won't sem justificativa**, que parece esquecimento.
- **Esforço com falsa precisão** (horas): o agente não mede; use S/M/L.
- **Backlog sem suposições:** o que foi decidido sozinho precisa estar visível.
- **Ordem ignorando dependência:** US de cancelar antes de reservar.

## 9. Perguntas-chave

1. Se cortássemos metade, o que o cliente salvaria?
2. O que é o menor conjunto que fecha a jornada principal?
3. Que US só existe porque outra existe (dependência)?
4. O que ficou fora de propósito e onde isso está escrito?
5. Há prazo externo que muda a ordem?

## 10. Checklist

- [ ] Toda US aparece no backlog e vice-versa.
- [ ] MoSCoW em todas; Must até ~60% do esforço relativo.
- [ ] Dependências registradas em `depende_de` coerentes (sem ciclo) e respeitadas na `ordem`.
- [ ] Won't em `adiadas`, com `motivo`.
- [ ] `suposicoes` numeradas (S-n) com `motivo`/impacto.
- [ ] Jornada principal completa dentro do corte de Must.
- [ ] Cabeçalho YAML completo e válido no esquema `backlog.schema.json`.
