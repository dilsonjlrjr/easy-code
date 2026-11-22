# RNF de desempenho e capacidade

Categoria (`categoria`): `desempenho` (capacidade e escalabilidade entram aqui). Desempenho é
sobre **o que o usuário sente** (tempo de resposta) e **o que o sistema aguenta** (volume e
concorrência).

## 1. Perguntas-chave

- Quantos usuários existem? Quantos usam **ao mesmo tempo** no pico? (Concorrentes costumam
  ser 5% a 20% dos usuários totais em ferramentas internas; confirme.)
- Qual é a operação mais frequente? Qual é a mais crítica? (Medir essas, não tudo.)
- Quando é o pico (início do expediente, virada de mês)?
- Quantos registros haverá em 1 ano? E em 5 anos? (A listagem que é rápida com 100 linhas pode
  não ser com 100 mil.)
- Há operações pesadas (relatório, exportação)? Podem ser assíncronas?
- Qual a rede típica do usuário (rede corporativa, celular em 4G)?
- O que acontece se passar do limite: degradar ou recusar?

## 2. Tipos de requisito

| Tipo | O que mede | Exemplo de métrica |
|---|---|---|
| Latência de API | Tempo até a resposta do servidor | p95 <= 300 ms para consultas, p95 <= 500 ms para escritas, com 20 usuários concorrentes |
| Tempo percebido de tela | Até o conteúdo útil aparecer | Conteúdo principal visível em até 2 s (p75) em rede de 4G simulada |
| Throughput | Requisições por tempo | Suporta 50 req/s sustentadas sem erro |
| Concorrência | Usuários simultâneos | 100 usuários concorrentes com p95 dentro da meta |
| Volume de dados | Tamanho do conjunto | Meta de latência mantida com 100.000 reservas |
| Capacidade de crescimento | Margem | Suporta 3x o volume atual sem mudar arquitetura |
| Peso da página | Bytes transferidos | Página inicial <= 500 KB transferidos (compactados) |
| Uso de recurso | CPU/memória | Uso de memória do processo <= 512 MB no pico |
| Eficiência de consulta | Consultas por requisição | Listagem principal executa no máximo N consultas ao banco (sem padrão N+1) |
| Paginação | Resposta limitada | Listagens retornam no máximo 50 itens por página |

**Nunca usar média** como métrica única: uma média de 200 ms pode esconder 10% das chamadas em
3 s. Use **percentil** (p95, p99) e declare a **condição de carga**.

## 3. Como verificar

| Instrumento | Uso | Ferramentas open source |
|---|---|---|
| Teste de carga | Latência e throughput sob carga definida | k6, Locust, Apache JMeter |
| Medição de página | Tempo de carregamento, peso, métricas de experiência (LCP, INP, CLS) | Lighthouse |
| Perfilamento | Achar o gargalo | Profilers da linguagem, `EXPLAIN` do banco |
| Teste automatizado de contagem de consultas | Impedir N+1 | Contador de consultas no teste |
| Dados de volume | Popular o banco de teste com o volume-alvo | Script de seed |

Defina **ambiente de medição**: "ambiente de teste com a mesma configuração do alvo". Medir no
notebook do dev produz números que ninguém aceita.

## 4. Exemplos completos

Bom:

```yaml
categoria: desempenho
requisito: "a consulta de disponibilidade de salas por data responde de forma fluida mesmo com o volume de um ano de uso"
metrica: "p95 <= 500 ms para a listagem de disponibilidade com 15 salas e 5.000 reservas na base, sob 20 usuários concorrentes"
verificacao:
  tipo: teste_automatizado
  detalhe: "teste de carga (k6) em ambiente de teste, com base populada por script"
prioridade: must
origem:
  tipo: boa_pratica
  detalhe: "boa prática (valor proposto, a confirmar com o cliente)"
```

Ruim:

```yaml
requisito: "o sistema deve ser rápido"
metrica: "tempo de resposta baixo"
verificacao:
  tipo: teste_automatizado
  detalhe: "teste de performance"
```

Por quê: sem número, sem estatística, sem condição, sem ferramenta, e "rápido" não é métrica.

## 5. Como escolher números (valores de partida para ferramenta interna)

Use como **proposta de boa prática, a confirmar**, nunca como fato:

| Operação | Ponto de partida |
|---|---|
| Consulta/listagem simples | p95 <= 300–500 ms |
| Escrita simples (criar/cancelar) | p95 <= 500–800 ms |
| Operação pesada (relatório) | Assíncrona, ou até 5 s com indicador de progresso |
| Carga | Usuários concorrentes = 10%–20% dos usuários totais; mínimo 20 |
| Volume | 12 meses de dados projetados, com folga de 2x a 3x |

Lembre os limiares de percepção: até ~100 ms parece instantâneo; até ~1 s o usuário mantém o
fluxo de pensamento; acima de ~10 s ele desiste ou perde o foco, daí a necessidade de feedback
de progresso.

## 6. Heurísticas de especialista

- **Meça o que o usuário faz mais**, não o que é fácil de medir.
- **Volume de dados importa mais que usuários** em sistema interno: um índice faltando mostra o
  problema só com milhares de linhas.
- **Paginação como requisito**: nenhuma listagem sem limite (proteção de memória e latência).
- **Páginas de resposta** devem retornar só o que a tela usa.
- **Condição de carga explícita** ("20 concorrentes") evita discussão depois.
- **Margem:** requisito sem folga falha na primeira alteração.
- **Verificável em aplicação pequena:** k6 ou Locust rodando por 1 a 5 minutos já valida.
- **Desempenho vs. outras qualidades:** cache melhora latência e pode violar frescura de dado
  (reserva recém-feita invisível). Se disponibilidade em tempo real é requisito de negócio,
  registre "dado consistente com no máximo N segundos de atraso" ou "sem cache de
  disponibilidade".
- **Aviso de Arquitetura:** o RNF diz o alvo; a solução (cache, índice, fila) é decisão da
  Arquitetura.

## 7. Anti-padrões

- "Rápido", "responsivo" (confunde com layout), "escalável" sem número.
- Média como métrica.
- Meta copiada de grande empresa (milhões de usuários) para app interno.
- Medir só com 10 registros.
- Ignorar a primeira carga (cold start) ou declarar se é excluída.
- Pedir desempenho de **todas** as operações igual: dilui o foco.
- Requisitos de desempenho sem forma de gerar o volume de teste.

## 8. Capacidade: dimensionar com o cliente

Perguntas curtas que viram números:

1. Funcionários ativos? (ex.: 200)
2. Salas/entidades principais? (ex.: 15)
3. Operações por usuário por dia? (ex.: 2 reservas)
4. Registros por ano = usuários x operações x dias úteis (ex.: 200 x 2 x 250 = 100.000)
5. Pico: que fração faz a operação nos mesmos 15 minutos?

Resultado típico: "suportar 100.000 reservas ao ano, 5 anos de histórico (500.000)". A conta
fica na seção de suposições, visível.

## 9. Checklist

- [ ] Operações críticas identificadas (não "todas").
- [ ] Métrica com percentil, alvo numérico, unidade e condição de carga/volume.
- [ ] Volume projetado definido e declarado como suposição quando não veio do cliente.
- [ ] Paginação/limite de listagem previsto.
- [ ] Forma de verificar executável com ferramenta open source e dados de volume.
- [ ] Ambiente de medição declarado.
- [ ] Valores de boa prática marcados para aprovação.
