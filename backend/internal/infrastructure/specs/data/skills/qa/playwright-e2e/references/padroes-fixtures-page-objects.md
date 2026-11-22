# Padrões: fixtures, page objects leves e helpers

Pergunta: **onde coloco o que se repete?** Três ferramentas, com papéis distintos.

## 1. Quando usar cada uma

| Ferramenta | Serve para | Evite quando |
|---|---|---|
| **Fixture** (`test.extend`) | Preparar e desmontar **estado/recurso**: usuário logado, dado criado, cliente de API, contexto de papel | Só encapsular cliques |
| **Page object leve** | Encapsular **localizadores e ações** de uma tela usada por muitos testes | A tela é usada em 1–2 testes; o objeto vira camada de indireção |
| **Helper (função)** | Operação pequena e pura (formatar data, montar payload) | Precisa de ciclo de vida (setup/teardown) → fixture |
| **Nada** (inline) | Teste curto e único | O mesmo bloco aparece em 3+ testes |

Regra prática: **comece inline, extraia na 3ª repetição**. Prefira fixture para estado,
page object leve para tela reutilizada.

## 2. Fixtures

Fixture = valor que o teste declara como parâmetro; o Playwright cria antes e desmonta depois
(inclusive em falha), só se o teste a usa.

```ts
// fixtures/index.ts
import { test as base, expect } from '@playwright/test';
import { ApiClient } from './api';
import { PaginaReservas } from '../pages/reservas';
import { env } from '../support/env';

type F = { apiFunc: ApiClient; reservas: PaginaReservas };

export const test = base.extend<F>({
  apiFunc: async ({}, use) => {
    const c = await ApiClient.como(env.func.email, env.func.senha);
    await use(c);
    await c.dispose();
  },
  reservas: async ({ page }, use) => {
    await use(new PaginaReservas(page));
  },
});
export { expect };
```

Boas práticas:
- Nome da fixture descreve o **que o teste recebe** (`novaReserva`, `apiAdmin`), não como é feito.
- Fixture com teardown garante limpeza mesmo com falha (ver `dados-isolamento.md`).
- `scope: 'worker'` para recursos caros e compartilháveis e **somente leitura** (ex.: cliente
  de API autenticado); `scope: 'test'` (padrão) para tudo que muta.
- `{ auto: true }` roda a fixture em todo teste (ex.: capturar erros de console):

```ts
errosDeConsole: [async ({ page }, use) => {
  const erros: string[] = [];
  page.on('pageerror', e => erros.push(e.message));
  await use();
  expect(erros, 'erros de JavaScript na página').toEqual([]);
}, { auto: true }],
```

## 3. Page object leve

Objetivo: **um lugar** para os localizadores de uma tela; os testes continuam mostrando a
intenção. "Leve" = sem herança, sem asserções escondidas, sem espera dentro, poucos métodos.

```ts
// pages/reservas.ts
import { Page, Locator, expect } from '@playwright/test';

export class PaginaReservas {
  readonly sala: Locator;
  readonly data: Locator;
  readonly inicio: Locator;
  readonly fim: Locator;
  readonly reservar: Locator;
  readonly alerta: Locator;

  constructor(private page: Page) {
    this.sala = page.getByRole('combobox', { name: 'Sala' });
    this.data = page.getByLabel('Data');
    this.inicio = page.getByLabel('Início');
    this.fim = page.getByLabel('Fim');
    this.reservar = page.getByRole('button', { name: 'Reservar' });
    this.alerta = page.getByRole('alert');
  }

  async abrir() { await this.page.goto('/reservas/nova'); }

  async preencher(d: { sala: string; data: string; inicio: string; fim: string }) {
    await this.sala.selectOption({ label: d.sala });
    await this.data.fill(d.data);
    await this.inicio.fill(d.inicio);
    await this.fim.fill(d.fim);
  }

  linhaDaReserva(titulo: string): Locator {
    return this.page.getByRole('row', { name: new RegExp(titulo) });
  }
}
```

O teste:

```ts
test('CT-08 reserva sobreposta é recusada', async ({ reservas, novaReserva, apiFunc }) => {
  const existente = await novaReserva();                       // fixture de dados
  await reservas.abrir();
  await reservas.preencher({ sala: 'Azul', data: '2026-12-01', inicio: '10:30', fim: '11:30' });
  await reservas.reservar.click();
  await expect(reservas.alerta).toContainText('Horário indisponível'); // asserção NO teste
  expect(await apiFunc.listarReservas(existente.salaId)).toHaveLength(1);
});
```

Regras do page object leve:
1. **Asserções ficam no teste**, não no objeto (quem lê o teste vê o que é verificado).
2. Expõe **locators** (`readonly`) e **ações de intenção** (`preencher`, `abrir`), não cada clique.
3. Sem `waitForTimeout` e sem lógica condicional escondida.
4. Um objeto por **tela/componente real**, não por história.
5. Sem herança profunda (`BasePage`): composição, se necessário.
6. Métodos não retornam outros page objects de forma encadeada obrigatória (evita "fluent" excessivo).

## 4. Componentes reutilizáveis (cabeçalho, modal, tabela)

Para partes que aparecem em várias telas, crie um **objeto de componente** recebendo o
locator-raiz:

```ts
export class TabelaDeReservas {
  constructor(private raiz: Locator) {}
  linha(titulo: string) { return this.raiz.getByRole('row', { name: new RegExp(titulo) }); }
  cancelarDe(titulo: string) { return this.linha(titulo).getByRole('button', { name: 'Cancelar' }); }
}
```

## 5. Helpers puros

```ts
// support/datas.ts
export function amanhaAs(hora: number, minuto = 0): string { /* gera ISO com offset -03:00 */ }
export function titulo(prefixo: string) { return `${prefixo} ${crypto.randomUUID().slice(0, 8)}`; }
```

Funções sem estado e sem página. Fácil de testar e de ler.

## 6. Comparação: o mesmo teste nos três estilos

| Estilo | Efeito |
|---|---|
| Tudo inline | Máxima clareza em testes isolados; duplicação cresce |
| Page object pesado (assertions e fluxo todo dentro) | Teste vira `await app.fazerReservaEVerificar()`; esconde o que se testa; falha aponta para dentro do objeto |
| Fixtures + page object leve | Preparação padronizada, intenção visível no teste, localizadores centralizados |

Recomendação para aplicação pequena: **fixtures para estado e API; page object leve só para as 2–3
telas centrais**.

## 7. Organização de arquivos

- `fixtures/index.ts` exporta `test` e `expect`; **todos** os specs importam daqui
  (`import { test, expect } from '../fixtures'`), nunca de `@playwright/test` diretamente.
- `pages/` um arquivo por tela; `support/` utilitários; evitar pastas profundas.
- Combinar fixtures de arquivos distintos: `mergeTests(testA, testB)` (existe no Playwright).

## 8. Anti-padrões

| Anti-padrão | Por quê | Alternativa |
|---|---|---|
| `BasePage` com 40 métodos | Acoplamento, difícil de ler | Composição, objetos pequenos |
| Page object com `expect` dentro | Esconde a verificação do CT | Asserção no teste |
| Page object por história | Tela real ≠ história | Um por tela |
| Fixture que faz tudo ("cenarioCompleto") | Teste ilegível | Fixtures pequenas combináveis |
| Fixture sem teardown que cria dados | Sujeira entre execuções | Teardown/ambiente efêmero |
| `beforeEach` com 15 linhas de setup pela UI | Lento e frágil | Fixture + API |
| Helper que oculta `await` | Promessa solta, teste passa sem rodar | Sempre `await`; ESLint `no-floating-promises` |
