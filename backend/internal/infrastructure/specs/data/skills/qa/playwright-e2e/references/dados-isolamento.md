# Dados de teste e isolamento

Teste independente = cria o que precisa, não depende de ordem, não interfere em outros. É a
condição para paralelismo e para qualquer pessoa rodar do zero.

## 1. Camadas de dados

| Camada | Conteúdo | Quem cria | Ciclo de vida |
|---|---|---|---|
| **Referência** | papéis, usuários de teste, catálogo base (salas padrão) | seed do ambiente | por execução completa (ambiente limpo) |
| **Do teste** | reserva, projeto, pedido que o CT precisa | o próprio teste, via API | por teste |
| **Descartável** | sufixos únicos para nomes/e-mails | gerado no teste | por teste |

## 2. Seed do ambiente

- O seed vem do **app** (script/migração/comando de seed documentado no README do app). O QA
  só o chama; não escreve SQL no banco do app a não ser que o README prescreva.
- Seed mínimo e determinístico: usuários `funcionario`, `funcionario2`, `admin`; N salas com
  nomes e capacidades fixos.
- Com ambiente efêmero (CI/humano: `docker compose down -v` antes de cada execução completa), o seed
  garante estado conhecido sem limpeza elaborada.

## 3. Criar dados via API (não pela tela)

Preparar dados pela interface é lento e acopla o teste a telas irrelevantes. Use um **cliente
de API** próprio, autenticado.

`fixtures/api.ts`:

```ts
import { APIRequestContext, request } from '@playwright/test';
import { env } from '../support/env';

export interface Reserva { id: number; salaId: number; inicio: string; fim: string; titulo: string }

export class ApiClient {
  constructor(private ctx: APIRequestContext) {}

  static async como(email: string, senha: string): Promise<ApiClient> {
    const login = await request.newContext({ baseURL: env.apiURL });
    const r = await login.post('/auth/login', { data: { email, senha } });
    if (!r.ok()) throw new Error(`Login de API falhou: ${r.status()}`);
    const { token } = await r.json();
    const ctx = await request.newContext({
      baseURL: env.apiURL,
      extraHTTPHeaders: { Authorization: `Bearer ${token}` },
    });
    return new ApiClient(ctx);
  }

  async criarReserva(d: Omit<Reserva, 'id'>): Promise<Reserva> {
    const r = await this.ctx.post('/reservas', { data: d });
    if (!r.ok()) throw new Error(`Preparar reserva falhou: ${r.status()} ${await r.text()}`);
    return r.json();
  }

  async listarReservas(salaId: number): Promise<Reserva[]> {
    return (await this.ctx.get(`/salas/${salaId}/reservas`)).json();
  }

  async apagarReserva(id: number) { await this.ctx.delete(`/reservas/${id}`); }
  async dispose() { await this.ctx.dispose(); }
}
```

Falha ao **preparar** dado deve lançar erro claro (não é falha do CT; é setup quebrado) —
diferencie no relatório.

## 4. Fixture de dados com limpeza automática

A fixture entrega o dado e **remove ao fim**, mesmo se o teste falhar:

```ts
// fixtures/index.ts
import { test as base } from '@playwright/test';
import { ApiClient, Reserva } from './api';
import { env } from '../support/env';

type F = {
  apiFunc: ApiClient;
  apiAdmin: ApiClient;
  novaReserva: (p?: Partial<Omit<Reserva, 'id'>>) => Promise<Reserva>;
};

export const test = base.extend<F>({
  apiFunc:  async ({}, use) => { const c = await ApiClient.como(env.func.email, env.func.senha); await use(c); await c.dispose(); },
  apiAdmin: async ({}, use) => { const c = await ApiClient.como(env.admin.email, env.admin.senha); await use(c); await c.dispose(); },

  novaReserva: async ({ apiFunc }, use) => {
    const criadas: Reserva[] = [];
    await use(async (p = {}) => {
      const r = await apiFunc.criarReserva({
        salaId: 1,
        titulo: `Reunião ${crypto.randomUUID().slice(0, 8)}`,
        inicio: '2026-12-01T10:00:00-03:00',
        fim: '2026-12-01T11:00:00-03:00',
        ...p,
      });
      criadas.push(r);
      return r;
    });
    // teardown: roda mesmo com teste falho
    for (const r of criadas) await apiFunc.apagarReserva(r.id).catch(() => {});
  },
});
export { expect } from '@playwright/test';
```

Uso no teste: `const r = await novaReserva({ inicio, fim });`.

## 5. Isolamento em paralelo

Com `fullyParallel: true`, testes diferentes rodam ao mesmo tempo no **mesmo banco**. Evite
colisão:

| Risco | Técnica |
|---|---|
| Duas reservas disputando a mesma sala/horário | cada teste usa **sala própria** (criada via API) ou **horário único** derivado de um contador/uuid |
| Nomes únicos (título, e-mail) | sufixo uuid curto |
| Contagens globais ("há 3 salas") | assere sobre **o que o teste criou**, não sobre o total global |
| Teste que muda configuração global | `test.describe.configure({ mode: 'serial' })` e projeto separado, ou evite |
| Dado que muda de dia para dia | datas fixadas no setup do teste |

Estratégia mais robusta para regras de conflito: criar uma **sala nova por teste** (admin via
API) — conflitos ficam confinados. Se a aplicação não permite criar sala por API, use datas
distantes e únicas por teste (ex.: `2027-01-<dia derivado do índice do worker>`), documentado.

`testInfo.workerIndex` e `testInfo.parallelIndex` identificam o worker, úteis para particionar
dados.

## 6. Datas e relógio

Nunca `new Date()` em asserção. Fixe uma data-âncora **no setup do teste**:

```ts
const base = new Date(); base.setDate(base.getDate() + 7); base.setHours(10, 0, 0, 0);
```
Prefira data absoluta distante e estável (`2026-12-01`) quando o app não impede passado/futuro
distante; quando a regra é "no futuro", calcule `hoje + 7 dias` **uma vez** no início. Para
tempo no navegador: `page.clock` (conferir API na versão instalada, `agentes/ambiente.md`). Detalhes em
`cenarios-teste/references/concorrencia-tempo-dados.md`.

## 7. Limpeza

Ordem de preferência:
1. **Ambiente efêmero** (`down -v` + seed): zero limpeza, máxima reprodutibilidade.
2. **Fixture com teardown** (seção 4): para execução repetida local sem recriar o ambiente.
3. **Nunca** `DELETE FROM` direto no banco do app, nem truncar tabelas no meio da suíte
   (interfere em testes paralelos).

Teardown deve **tolerar falha** (`.catch(() => {})`), senão mascara a falha real do teste.

## 8. Verificar estado final

O teste e2e **negativo** (conflito, permissão negada) confirma o estado por API, não só pela
mensagem:

```ts
const aposTentativa = await apiFunc.listarReservas(sala.id);
expect(aposTentativa).toHaveLength(1); // nada criado
```

## 9. Dados sensíveis

Sintéticos, nunca cópia de produção. E-mails em domínio reservado (`@example.test`). Documentos
fictícios válidos apenas em formato. Evite dados pessoais reais em evidências anexadas ao ACH.

## 10. Anti-padrões

- Teste 2 usa a reserva criada no teste 1.
- Asserir "a lista tem 5 itens" quando outros testes criam itens em paralelo.
- Preparar dados clicando pela tela.
- Limpeza que apaga tudo (afeta testes paralelos).
- Seed implícito (depende de "alguém ter rodado antes"); o README e o comando de seed devem estar no REL-QA.
