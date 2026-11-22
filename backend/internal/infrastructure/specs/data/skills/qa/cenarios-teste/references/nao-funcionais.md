# Cenários não funcionais derivados de RNF

Todo RNF verificável por teste ganha CT próprio com `nivel: nao_funcional` e `cobre: {rnf: RNF-NN}`.
O CT herda do RNF a **métrica e o limiar** — se o RNF não tem número, ele não é testável: abra
ACH com `etapa_destino: requisitos` pedindo a métrica.

## 1. Da métrica ao CT

| Passo | Pergunta |
|---|---|
| 1 | Qual a métrica do RNF? (p95 < 500 ms; 0 violações axe A/AA; recuperação < 5 min) |
| 2 | Em que condição? (carga, volume de dados, ambiente, perfil de usuário) |
| 3 | Como medir de forma repetível? (ferramenta, comando, ambiente) |
| 4 | Qual o limiar de aprovação e quantas repetições? |
| 5 | O ambiente de teste é representativo? (Se não, registre a limitação) |

Resultado esperado do CT = **o número + a condição**, nunca "rápido o bastante".

## 2. Desempenho

Ferramentas open source típicas: **k6** (scripts JS, licença AGPL-3.0, uso como ferramenta de
linha de comando é livre), **Locust** (Python), **JMeter** (Java), `autocannon` (Node), `hey`/`wrk`.
Escolha a que a stack do projeto já tem; comando exato: conferir em `agentes/ambiente.md`.

### 2.1 Tipos de teste

| Tipo | Objetivo | Quando em aplicação pequena |
|---|---|---|
| **Fumaça de desempenho** | 1 usuário, endpoints principais: latência base | sempre |
| **Carga** | carga esperada (RNF) por tempo suficiente: p95/p99, taxa de erro | se há RNF de latência/vazão |
| **Estresse** | acima do esperado: onde quebra e como falha | opcional |
| **Resistência (soak)** | carga moderada por horas: vazamento de memória/conexões | raro em aplicação pequena |
| **Pico (spike)** | salto brusco: recupera? | se RNF cita picos |

### 2.2 Exemplo de CT

```yaml
titulo: Listar salas responde em até 300 ms (p95) com 50 usuários simultâneos   # CT-40
cobre: {rnf: RNF-02}
nivel: nao_funcional
tipo_caso: limite
precondicoes:
  - ambiente limpo com seed de 200 salas e 10 mil reservas; sem outra carga
passos:
  - Gerar 50 usuários virtuais autenticados listando salas por 2 minutos (após 20 s de aquecimento)
resultado_esperado:
  - p95 <= 300 ms
  - taxa de erro (status >= 500 ou timeout) < 0,1%
  - sem crescimento contínuo de memória do contêiner da API
```

### 2.3 Exemplo de script k6 (padrão estável do k6; conferir flags em `agentes/ambiente.md`)

```js
import http from 'k6/http';
import { check } from 'k6';

export const options = {
  vus: 50,
  duration: '2m',
  thresholds: {
    http_req_duration: ['p(95)<300'],   // vira falha do teste se estourar
    http_req_failed: ['rate<0.001'],
  },
};

export function setup() {
  const r = http.post(`${__ENV.BASE_URL}/auth/login`,
    JSON.stringify({ email: __ENV.USER, senha: __ENV.PASS }),
    { headers: { 'Content-Type': 'application/json' } });
  return { token: r.json('token') };
}

export default function (dados) {
  const r = http.get(`${__ENV.BASE_URL}/salas`,
    { headers: { Authorization: `Bearer ${dados.token}` } });
  check(r, { 'status 200': (x) => x.status === 200 });
}
```

`thresholds` torna o resultado binário (passou/falhou), que é o que o CT precisa.

### 2.4 Regras de especialista

- Meça **percentis** (p95/p99), nunca só média.
- Volume de dados importa: teste com o volume do RNF, não com banco vazio (N+1 e falta de
  índice só aparecem com dados).
- O ambiente Docker Compose local não é produção: reporte a máquina e aceite o resultado como
  **indicação**; falha grosseira (10x o limiar) é achado real; limiar apertado exige ambiente
  comparável (registrar em Observações do REL-QA).
- Aqueça o sistema (JIT, cache, pool) e descarte o aquecimento.
- Autentique **uma vez** no `setup`; não conte o login na latência de leitura.
- Procure N+1: contagem de queries por requisição (log do ORM/banco) como asserção auxiliar
  em integração.
- Acompanhe recursos (CPU, memória, conexões) do contêiner: `docker stats` (comando padrão do
  Docker).

## 3. Acessibilidade

Fonte: RNF de acessibilidade (WCAG 2.2 AA). Automático + manual.

| CT | Nível | Como |
|---|---|---|
| 0 violações A/AA em cada tela em escopo | não funcional (e2e) | `@axe-core/playwright` por tela e por estado relevante (modal aberto, erro de validação); ver skill `playwright-e2e`, `acessibilidade-axe.md` |
| Fluxos principais só por teclado | e2e | Tab/Shift+Tab/Enter/Esc: campo alcançável, ordem lógica, foco visível, nenhuma armadilha de foco |
| Rótulo programático em todo campo | e2e | `getByLabel` encontra cada campo (se falha, o rótulo não está associado) |
| Erro de validação identificado em texto e associado ao campo | e2e | mensagem visível e `aria-describedby`/`aria-invalid` |
| Idioma da página | integração/e2e | `html[lang="pt-BR"]` |
| Contraste, zoom 200%/reflow 320 px, leitor de tela | manual | roteiro registrado; CT com `Resultado` manual e evidência (print, nota); marque no CT que é manual |

Limite honesto: ferramenta automática cobre só parte dos critérios; o CT automático **não**
prova conformidade total, prova ausência das violações que ela detecta. Diga isso no REL-QA.

## 4. Resiliência e disponibilidade

| RNF típico | CT |
|---|---|
| Recuperação após queda do banco | derrubar o contêiner do banco, verificar resposta de erro controlada (503, não 500 com stack) e volta ao normal ao reiniciar sem reiniciar a API |
| Timeout de dependência | simular dependência lenta (proxy/stub) e verificar timeout e mensagem, sem travar threads |
| Health check | `GET /health` reflete dependências (banco fora -> não saudável) |
| Reinício a quente | requisições durante restart: falha curta e retomada; nenhuma reserva duplicada |
| Backup/restauração | restaurar dump em ambiente limpo e conferir contagens (se exigido) |

Comandos de Docker Compose (`docker compose stop db`, `docker compose start db`) são padrão **para CI/humano** (o agente no sandbox não tem Docker; resiliência com queda de dependência só roda onde houver Compose);
confirmar nomes de serviço no `docker-compose.yml` do projeto.

## 5. Outros RNF testáveis

| Categoria | CT típico |
|---|---|
| Segurança | rotas sem token = 401; cookies `HttpOnly`/`Secure`; cabeçalhos presentes (ver revisão de segurança) |
| Privacidade/LGPD | exportação e exclusão de dados do titular funcionam; log sem dado pessoal em claro |
| Usabilidade | tempo/passos para concluir tarefa (medição por script e2e: nº de cliques/telas) |
| Compatibilidade | mesma suíte e2e em Chromium, Firefox e WebKit (projects do Playwright); viewport móvel |
| Observabilidade | requisição com erro gera log com id de correlação; métricas expostas |
| Manutenibilidade | em geral fora do QA (cobertura mínima, lint) — conferir no gate de Dev |

## 6. Anti-padrões

- "Desempenho OK" sem número ou condição.
- Teste de carga contra banco vazio.
- Declarar acessibilidade "atendida" só com axe.
- Teste de resiliência que mata o ambiente compartilhado de outros testes.
- Limiar inventado pelo QA: o número vem do RNF; ausente, ACH para Requisitos.
