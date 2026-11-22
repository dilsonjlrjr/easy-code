---
name: historias-usuario
description: Transforma uma ideia de aplicação web em histórias de usuário (US) testáveis, com critérios de aceite Dado/Quando/Então, regras de negócio, glossário e backlog priorizado (MoSCoW). Cobre elicitação e perguntas ao cliente, INVEST, divisão de histórias grandes, casos negativos e limite, e revisão de qualidade do gate. Use quando for levantar requisitos funcionais, escrever ou revisar US, ou montar o backlog da etapa de Requisitos.
---

# Histórias de usuário

Skill da etapa de **Requisitos**. Serve a qualquer aplicação web: as regras abaixo não dependem
do domínio. Os exemplos usam reserva de salas só como ilustração.

## Para que serve uma US neste processo

A US é o contrato entre o cliente e todas as Etapas seguintes. QA deriva os CT dos critérios de
aceite (CA), Arquitetura deriva as T, Design desenha as telas, Dev implementa. **O que ficar
ambíguo na US vira retrabalho caro depois** (um Achado, uma Onda de retrabalho). Por isso a US
não é um lembrete de conversa: é um Artefato completo, lido por agentes que não podem
perguntar nada ao autor.

## Princípios

1. **Ator + objetivo, não funcionalidade.** Toda US começa num ator real com um objetivo real.
   "Cadastrar sala" só existe porque o Administrador precisa oferecer salas para reserva.
2. **Valor observável.** O "Para" explica o benefício ao ator, não ao sistema ("para garantir
   o espaço da minha reunião", não "para persistir o registro").
3. **Testável é obrigatório.** Se um CT não pode ser escrito a partir do CA, o CA está ruim.
4. **Origem rastreável.** Cada US cita um trecho real da ideia ou de uma resposta do cliente.
   Sem origem, vira pergunta aberta ou suposição registrada, nunca história "de enfeite".
5. **Requisito, não solução.** Nada de tecnologia, tabela, endpoint ou layout. Isso é
   Arquitetura e Design.
6. **Pequena o bastante para uma Onda.** Se Dev não entrega em uma onda, divida (ver
   `references/escrita-de-historias.md`).
7. **Pergunte pouco e certo.** Perguntas só para o que muda escopo ou regra; o resto vira
   suposição visível.

## Fluxo de trabalho

1. **Ler a ideia** e separar: explícito, implícito, ausente (`references/elicitacao-e-perguntas.md`).
2. **Listar atores e objetivos**; cada objetivo é candidata a US.
3. **Perguntar** ao cliente, numeradas, de uma vez, com sugestão de resposta.
4. **Registrar suposições** para tudo que decidiu sozinho.
5. **Escrever as US** no template `agentes/templates/US.md` (um arquivo por US, nome = id; cabeçalho YAML validado por `specs/dominio/artefatos/us.schema.json`).
6. **Escrever os CA** cobrindo caminho feliz, falhas esperadas e limites
   (`references/criterios-de-aceite.md`).
7. **Extrair regras de negócio e glossário** (`references/glossario-e-regras-de-negocio.md`).
8. **Priorizar** e montar o BACKLOG (`references/priorizacao-e-BACKLOG.md`) e a VISAO (`references/glossario-e-regras-de-negocio.md`).
9. **Revisar** com o checklist do gate (`references/revisao-qualidade-gate.md`) antes de entregar.

## Regras de decisão

| Situação | Decisão |
|---|---|
| O "Quero" tem "e" ligando dois objetivos | Duas US |
| Dois atores com poderes diferentes fazem a mesma coisa | Uma US por ator, ou uma US com CA por papel se o comportamento for idêntico e só a permissão mudar |
| Falta uma informação que não muda escopo nem regra | Decidir, registrar como suposição no backlog |
| Falta uma informação que muda escopo, regra ou custo | Pergunta ao cliente; bloqueante se impede escrever o CA |
| Algo só aparece numa suposição sua | Não é Origem. Origem = trecho do cliente |
| Pedido é de infraestrutura/técnico (criar tabela, configurar CI) | Não é US: é T (Arquitetura) ou RNF |
| Restrição de qualidade (rápido, seguro, acessível) | RNF, vinculado às US afetadas |
| Cliente pede "tudo" como Must | Aplicar MoSCoW com a regra do teto de Must e devolver os trade-offs |
| Mais de 10 US em aplicação pequena | Propor cortes ou divisão em releases |

## Formato do artefato (US)

A US é um arquivo `docs/requisitos/US-NN-<slug>.md`: **cabeçalho YAML validado pelo esquema**
`specs/dominio/artefatos/us.schema.json` + corpo Markdown livre que nunca contradiz o YAML
(decisão D-01). Molde e exemplo completo em `agentes/templates/US.md`. **Vale o esquema**; campos
próprios da US (além do cabeçalho comum):

- `ator`, `quero`, `para`, `origem`: texto obrigatório; `titulo` começa com verbo no infinitivo.
- `prioridade`: `must | should | could | wont`; `tem_interface`: booleano (diz se o Design precisa de tela).
- `criterios_aceite`: ao menos 1, cada um `{id: CA-n, dado, quando, entao}`.
- `regras_negocio`: `[{id: RN-n, descricao}]`, `[]` se não houver.
- `perguntas_abertas`: `[{pergunta, bloqueante: true|false}]`, `[]` se não houver.
- `rnf`: ids dos RNF que afetam a história (campo próprio); `vinculos` fica só para relação genérica.

Exemplo mínimo válido:

```yaml
---
id: US-04
tipo: US
titulo: Cancelar reserva própria
autor: requirements-agent
onda: REQ-01
versao: 1
estado: rascunho
vinculos: []
historico:
  - {versao: 1, data: "2026-11-19", onda: REQ-01, descricao: criação}
ator: Funcionário
quero: cancelar uma reserva minha
para: liberar a sala para outras pessoas quando a reunião não acontecer
prioridade: must
origem: '"fazem reservas e cancelam as próprias"'
tem_interface: true
criterios_aceite:
  - id: CA-1
    dado: reserva minha futura
    quando: cancelo
    entao: ela some de "minhas reservas" e o horário volta a ficar livre
  - id: CA-2
    dado: reserva de outro funcionário
    quando: tento cancelar
    entao: a ação é negada e a reserva permanece
  - id: CA-3
    dado: reserva minha que já terminou
    quando: tento cancelar
    entao: a ação é recusada informando que já ocorreu
regras_negocio:
  - id: RN-1
    descricao: Só o dono (ou Administrador, na US própria) cancela uma reserva.
perguntas_abertas: []
rnf: []
---
```

Bom porque: um ator, um objetivo, origem literal, feliz + autorização + estado inválido,
sem tecnologia nem tela. Mais exemplos (bons e ruins) em `references/criterios-de-aceite.md`.

Bom porque: um ator, um objetivo, origem literal, feliz + autorização + estado inválido,
sem tecnologia nem tela. Mais exemplos (bons e ruins) em `references/criterios-de-aceite.md`.

## Checklist de saída (resumo)

- [ ] Toda US tem `ator`, `quero`, `para`, `prioridade`, `origem` e `tem_interface` preenchidos.
- [ ] Toda US tem pelo menos 1 CA; as US de risco têm caminho feliz + falha + limite.
- [ ] Nenhum CA usa termo vago (rápido, fácil, adequado, corretamente, intuitivo).
- [ ] Nenhum CA tem dois "Então" de comportamentos distintos.
- [ ] Nenhuma pergunta aberta com `bloqueante: true`.
- [ ] Todo termo de domínio usado está no `glossario` da VISAO (`VISAO.md`).
- [ ] Cada regra de negócio aparece em pelo menos um CA.
- [ ] BACKLOG lista todas as US (em `historias` ou `adiadas`), com MoSCoW e as `suposicoes`.
- [ ] Cabeçalho YAML completo e válido no esquema; `onda: REQ-<nn>`; `rnf` com ids existentes.

O checklist completo, com severidade de cada falha, está em `references/revisao-qualidade-gate.md`.

## Erros comuns

- **História técnica** disfarçada ("Como sistema, quero uma tabela...").
- **CA que descreve tela** ("botão verde no canto"): é Design.
- **Ator genérico "usuário"** quando há papéis com poderes diferentes.
- **CA só de caminho feliz**: o bug nasce nas falhas e nos limites.
- **Regra implícita**: "não pode haver conflito" sem definir o que é conflito (encostado conta?).
- **Origem inventada** para "parecer" rastreável.
- **US gigante** ("gerenciar reservas") que esconde dez comportamentos.
- **Pergunta cosmética** ao cliente (cor, fonte) em vez de pergunta que muda regra.
- **Prioridade tudo Must**: sem priorização não há corte possível.
- **Misturar RNF na US**: "a tela carrega em 1 s" vai para um RNF.

## Índice dos arquivos de referência

Leia sob demanda; cada arquivo é independente.

| Arquivo | Quando ler |
|---|---|
| `references/elicitacao-e-perguntas.md` | Ao começar: ler a ideia, achar lacunas, montar o roteiro de perguntas ao cliente |
| `references/escrita-de-historias.md` | Ao redigir US: INVEST, título, ator, divisão de histórias grandes (padrões de split) |
| `references/criterios-de-aceite.md` | Ao escrever CA: Gherkin, cobertura de regras, negativos e limites, bons e maus exemplos |
| `references/priorizacao-e-BACKLOG.md` | Ao priorizar: MoSCoW, valor × esforço, mapa de histórias, formato do BACKLOG (`BACKLOG.md`) |
| `references/glossario-e-regras-de-negocio.md` | Ao tratar termos e regras: glossário, extração e escrita de regras, tabelas de decisão |
| `references/revisao-qualidade-gate.md` | Antes de entregar: checklist do gate com severidade e roteiro de autorrevisão |
