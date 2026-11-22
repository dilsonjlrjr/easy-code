# Cenários de concorrência, tempo/fuso e dados

Três famílias de bug que passam em teste simples e aparecem em produção. Todas exigem
pré-condição controlada: relógio fixo, dados semeados, requisições realmente simultâneas.

## 1. Concorrência

**Quando há CT de concorrência:** regra de unicidade ("uma reserva por sala/horário"), saldo
ou estoque, limite de vagas, número sequencial, "somente um ativo por vez", idempotência de
operação reenviada.

### 1.1 Condição de corrida em unicidade

1. Pré-condição: sala livre no horário H.
2. Disparar **N requisições simultâneas** (N >= 2; use 10 para aumentar a chance) criando a
   reserva da mesma sala/horário.
3. Esperado: **exatamente 1** sucesso (201) e N-1 conflitos (409); **1** linha no banco.
4. A garantia deve vir do **banco** (restrição de unicidade, exclusão por intervalo, lock ou
   transação serializável), não de um "verifica e depois grava" na aplicação: o CT pode
   passar 99 vezes e falhar na centésima; por isso rode a rajada várias vezes (ex.: 20 execuções)
   e conte.

Exemplo de rajada (TypeScript, Playwright `request` ou `fetch` do Node 18+):

```ts
const respostas = await Promise.all(
  Array.from({ length: 10 }, () =>
    api.post('/reservas', { data: { salaId, inicio, fim } })
  )
);
const status = respostas.map(r => r.status());
expect(status.filter(s => s === 201)).toHaveLength(1);
expect(status.filter(s => s === 409)).toHaveLength(9);
// estado final: uma só reserva
const lista = await (await api.get(`/salas/${salaId}/reservas`)).json();
expect(lista).toHaveLength(1);
```

Python equivalente: `concurrent.futures.ThreadPoolExecutor` + `requests`; Java: `ExecutorService`
com `CountDownLatch` para largada simultânea. Largada simultânea importa: dispare depois de
todas as threads prontas.

### 1.2 Outras corridas

| Cenário | Esperado |
|---|---|
| Duplo clique/reenvio do mesmo formulário | 1 efeito (idempotência ou botão desabilitado + chave de idempotência) |
| Editar o mesmo recurso em duas abas (última grava) | conflito detectado (versão/ETag -> 409/412) ou regra "última vence" explícita no requisito |
| Cancelar enquanto outro edita | transição inválida tratada (409), sem estado híbrido |
| Dois admins criando sala com mesmo nome | unicidade respeitada |
| Contador/sequência | sem número duplicado nem pulo inesperado sob rajada |
| Exclusão de pai com filho sendo criado | integridade referencial; erro claro |

### 1.3 Idempotência

Para operações que o cliente pode repetir (rede ruim, timeout): repetir **a mesma requisição
com a mesma chave** (cabeçalho de idempotência, se o design previu) deve devolver o mesmo
resultado sem novo efeito. Sem chave de idempotência no design, o CT documenta o
comportamento real (duplica?) e abre ACH para Requisitos/Arquitetura se o requisito silencia.

### 1.4 Cuidados

- Teste de concorrência é **probabilístico**: não é intermitente por culpa do teste, é o
  defeito. Falha em qualquer execução = achado (evidência: contagem de status das execuções).
- Aumente a janela de corrida se o ambiente for rápido (mais N, mais execuções), nunca
  adicione `sleep` na aplicação.

## 2. Tempo, fuso e calendário

### 2.1 Controle do relógio

Testes que dependem de "agora" precisam de um **relógio controlado**:

- API/integração: o app deve permitir injetar o relógio (variável de ambiente, endpoint só de
  teste, ou semear dados com datas **relativas calculadas no setup**: `hoje + 1 dia` fixado no
  início do teste e usado em todas as asserções).
- e2e: Playwright tem `page.clock` para controlar o tempo do navegador (instalar/avançar
  relógio); para o servidor, siga a regra acima. Confirmar a API de `clock` na versão
  instalada (`agentes/ambiente.md`).
- Nunca compare com `new Date()` calculado depois da ação (virada de minuto quebra).

### 2.2 Cenários

| Cenário | Esperado |
|---|---|
| Data no passado | recusa (400/409) conforme regra |
| Agora exato / início da próxima hora (limite) | conforme CA; teste o minuto exato e -1 minuto |
| Virada de dia (23:30–00:30) | reserva atravessando meia-noite aceita/recusada conforme regra, sem duplicar nem sumir em nenhum dos dois dias |
| Fim de mês / ano / 29 de fevereiro | datas válidas aceitas; 30/02 recusada |
| Horário de verão / mudança de offset | armazenar em UTC (ou instante com offset); exibir no fuso do usuário. Teste uma data em dia de mudança de offset se o país/organização adotar |
| Fuso do usuário diferente do servidor | mesma reserva aparece no horário local correto para cada um; conflito é calculado no **mesmo instante** |
| Formato de data na API | ISO 8601 com offset (`2026-12-01T10:00:00-03:00`); data sem offset deve ser recusada ou ter regra |
| Dia útil / feriado / horário comercial | borda inicial e final do expediente; feriado se previsto |
| Expiração (token, reserva, convite) | no instante exato do limite e 1 s depois |
| Relógio do cliente errado | a regra de negócio usa relógio do servidor |

**Exemplo — CT de virada de dia**

```yaml
titulo: Reserva que atravessa a meia-noite não conflita com o dia seguinte   # CT-31
cobre: {us: US-03, criterio: CA-4}
nivel: integracao
tipo_caso: limite
precondicoes:
  - sala "Azul" com reserva 00:00–01:00 de D+1 (D = data fixada no setup, fuso -03:00)
passos:
  - Reservar "Azul" das 23:00 de D às 00:00 de D+1
resultado_esperado:
  - 201; a reserva termina exatamente quando a outra começa
  - a listagem de D mostra a nova; a de D+1 mostra só a existente
```

### 2.3 Armadilhas de tempo no teste

- Teste que passa de manhã e falha à noite (data derivada de `now` sem âncora).
- Comparar `LocalDateTime` com instantes de fusos diferentes.
- Assumir que o runner de CI está em `America/Sao_Paulo`: fixe `TZ` no ambiente do teste e teste
  explicitamente também outro fuso.

## 3. Dados

### 3.1 Estratégia de dados do teste

| Necessidade | Como |
|---|---|
| Dado mínimo para o CT | Criar no `setup` do teste, via API (rápido, exercita contrato) ou SQL de seed |
| Dado de referência estável (papéis, salas padrão) | Seed do ambiente, versionado |
| Dado único por teste | Sufixo aleatório/uuid no nome para evitar colisão em paralelo |
| Limpeza | Ambiente limpo por execução (CI/humano: `docker compose down -v`; no sandbox, serviço de apoio efêmero do perfil) ou teardown que apaga o que criou |
| Dado sensível | Nunca dado real; dados sintéticos |

Regra: o CT declara em "Pré-condições" os **valores** de que depende; o código de teste os
materializa no setup. Teste que depende de dado deixado por outro teste é teste quebrado.

### 3.2 Variações de dados a cobrir

- **Texto:** acentos (`ç`, `ã`), unicode fora do BMP (emoji), RTL, espaço no início/fim, quebra
  de linha, tamanho mínimo/máximo, caracteres de controle, `NULL`/"null" como texto.
- **Números:** 0, negativo, decimal com vírgula vs ponto, muito grande, `NaN`, notação científica.
- **Coleções:** vazia, 1, muitas (paginação), duplicadas, ordem diferente.
- **Identificadores:** inexistente, formato inválido, de outro usuário, apagado.
- **Arquivos:** vazio, no limite, acima do limite, extensão x conteúdo divergentes, nome com
  espaço/acento.
- **Persistência:** o que foi gravado volta idêntico (ida e volta com acento/emoji); o dado
  aparece igual na tela e na API.

### 3.3 Integridade e efeitos colaterais

Após operações de falha, verifique que **nada parcial** ficou: transação revertida, nenhum
registro órfão, nenhum evento/e-mail enviado, contador não alterado.

| Operação com falha | Verificação de estado |
|---|---|
| Criar com validação falhando | zero registros novos |
| Cancelar já cancelada | estado inalterado, histórico sem entrada nova |
| Erro de servidor no meio (simulado) | transação inteira revertida |
| Exclusão com dependentes | bloqueada ou em cascata, conforme requisito, sem órfãos |

### 3.4 Paginação, ordenação e filtros

- primeira página, última, uma além da última (vazia, 200, não 500), `limit` 0/1/máximo+1;
- ordenação **estável** com empate (mesmo valor de campo): ordem determinística (desempate por id);
- filtro combinado (pairwise) e filtro sem resultado;
- total/contagem coerente com os itens.

## 4. Quadro-resumo de prioridade

| Família | Impacto se falhar | Detecção sem CT |
|---|---|---|
| Corrida em unicidade/saldo | Dados corrompidos/duplicados | Rara, só em produção sob carga |
| Fuso/virada de dia | Reserva no horário errado | Intermitente, por horário do dia |
| Texto especial | 500, XSS, dado truncado | Só com usuários reais |
| Estado parcial após erro | Inconsistência | Muito difícil |
