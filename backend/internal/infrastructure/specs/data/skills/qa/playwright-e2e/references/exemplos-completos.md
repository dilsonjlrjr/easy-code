# Exemplos completos de testes

Cinco modelos ligados aos CT. Usam as fixtures de `padroes-fixtures-page-objects.md` e
`dados-isolamento.md` (`test`/`expect` de `../fixtures`, `apiFunc`, `apiAdmin`, `novaReserva`).
Rótulos, textos e rotas são ilustrativos: adapte aos reais da aplicação (README, CT).
Métodos de `ApiClient` usados além dos de `dados-isolamento.md` (`listarMinhas`, `obterReserva`,
`tentarReserva` que devolve a resposta sem lançar erro, `criarSala`) são do mesmo estilo: crie-os
conforme o contrato da API. Os exemplos com `ctxB.request`/`request` sem cabeçalho assumem sessão
por cookie; com token em cabeçalho, use `ApiClient.como(...)` do usuário B.
Cada exemplo segue: setup por API -> ação pela interface -> asserção de UI -> asserção de estado.

## 1. Login (US de autenticação) — sem `storageState`

CT-01 (positivo), CT-02 (senha errada), CT-03 (usuário inexistente, mesma mensagem).

```ts
// tests/us-01-entrar.spec.ts
import { test, expect } from '@playwright/test';
import { env } from '../support/env';

test.use({ storageState: { cookies: [], origins: [] } }); // contexto limpo

test.describe('US-01 Entrar', () => {
  test('CT-01 login válido leva à tela inicial @smoke', async ({ page }) => {
    await page.goto('/login');
    await page.getByLabel('E-mail').fill(env.func.email);
    await page.getByLabel('Senha').fill(env.func.senha);
    await page.getByRole('button', { name: 'Entrar' }).click();

    await expect(page).toHaveURL(/\/salas/);
    await expect(page.getByRole('navigation')).toContainText('Minhas reservas');
  });

  test('CT-02 senha errada mostra mensagem genérica e não entra', async ({ page }) => {
    await page.goto('/login');
    await page.getByLabel('E-mail').fill(env.func.email);
    await page.getByLabel('Senha').fill('senha-errada');
    await page.getByRole('button', { name: 'Entrar' }).click();

    await expect(page.getByRole('alert')).toHaveText('E-mail ou senha inválidos');
    await expect(page).toHaveURL(/\/login/);
  });

  test('CT-03 usuário inexistente recebe a mesma mensagem (sem enumeração)', async ({ page }) => {
    await page.goto('/login');
    await page.getByLabel('E-mail').fill('ninguem@example.test');
    await page.getByLabel('Senha').fill('qualquer-coisa');
    await page.getByRole('button', { name: 'Entrar' }).click();

    await expect(page.getByRole('alert')).toHaveText('E-mail ou senha inválidos'); // idêntica ao CT-02
  });

  test('CT-04 rota protegida sem login redireciona ao login', async ({ page }) => {
    await page.goto('/minhas-reservas');
    await expect(page).toHaveURL(/\/login/);
  });
});
```

## 2. CRUD (criar, listar, editar, excluir) — como funcionário

CT-07 (criar), CT-10 (aparece na lista), CT-21 (cancelar a própria).

```ts
// tests/us-03-reservar-sala.spec.ts
import { test, expect } from '../fixtures';
import { titulo } from '../support/datas';

test.use({ storageState: '.auth/funcionario.json' });

test.describe('US-03 Reservar sala', () => {
  test('CT-07 reserva sala livre e ela aparece em Minhas reservas @smoke', async ({ page, reservas, apiFunc }) => {
    const nome = titulo('Planejamento');
    await reservas.abrir();
    await page.getByLabel('Título').fill(nome);
    await reservas.preencher({ sala: 'Azul', data: '2026-12-01', inicio: '10:00', fim: '11:00' });
    await reservas.reservar.click();

    // UI
    await expect(page.getByRole('status')).toContainText('Reserva criada');
    await page.goto('/minhas-reservas');
    await expect(reservas.linhaDaReserva(nome)).toBeVisible();

    // estado (API): exatamente uma reserva com esse título
    const todas = await apiFunc.listarMinhas();
    expect(todas.filter(r => r.titulo === nome)).toHaveLength(1);
  });
});

// tests/us-05-cancelar-reserva.spec.ts
test.describe('US-05 Cancelar reserva', () => {
  test('CT-21 funcionário cancela a própria reserva', async ({ page, novaReserva, apiFunc }) => {
    const r = await novaReserva();                         // dado preparado via API
    await page.goto('/minhas-reservas');
    const linha = page.getByRole('row', { name: new RegExp(r.titulo) });
    await linha.getByRole('button', { name: 'Cancelar' }).click();
    await page.getByRole('dialog').getByRole('button', { name: 'Confirmar' }).click();

    await expect(linha).toBeHidden();                       // some da lista (ou "cancelada", conforme CA)
    const atual = await apiFunc.obterReserva(r.id);
    expect(atual.estado).toBe('cancelada');                 // estado final
  });
});
```

## 3. Conflito (regra de negócio com erro visível) — CT-08, CT-14

Regra de intervalo vive nos testes de integração; **um** e2e prova a experiência de erro.

```ts
test('CT-14 conflito mostra "horário ocupado" e mantém o formulário preenchido', async ({
  page, reservas, novaReserva, apiFunc,
}) => {
  const existente = await novaReserva({ salaId: 1, inicio: '2026-12-01T10:00:00-03:00', fim: '2026-12-01T11:00:00-03:00' });

  await reservas.abrir();
  await reservas.preencher({ sala: 'Azul', data: '2026-12-01', inicio: '10:30', fim: '11:30' });
  const resposta = page.waitForResponse(r => r.url().endsWith('/reservas') && r.request().method() === 'POST');
  await reservas.reservar.click();

  expect((await resposta).status()).toBe(409);                              // contrato por baixo
  await expect(page.getByRole('alert')).toContainText('Horário indisponível'); // UI
  await expect(reservas.inicio).toHaveValue('10:30');                        // dados mantidos
  await expect(page).toHaveURL(/\/reservas\/nova/);                          // não navegou

  expect(await apiFunc.listarReservas(existente.salaId)).toHaveLength(1);    // nada criado
});
```

Teste de API puro para a **rajada concorrente** (CT-17), sem navegador:

```ts
// tests/api/us-03-concorrencia.spec.ts
import { test, expect } from '../../fixtures';

test('CT-17 10 pedidos simultâneos para o mesmo horário geram 1 reserva', async ({ apiFunc, apiAdmin }) => {
  const sala = await apiAdmin.criarSala({ nome: `Sala ${crypto.randomUUID().slice(0, 6)}`, capacidade: 6 });
  const corpo = { salaId: sala.id, titulo: 'Corrida', inicio: '2026-12-02T10:00:00-03:00', fim: '2026-12-02T11:00:00-03:00' };

  const respostas = await Promise.all(Array.from({ length: 10 }, () => apiFunc.tentarReserva(corpo)));
  const status = respostas.map(r => r.status());

  expect(status.filter(s => s === 201), `status: ${status}`).toHaveLength(1);
  expect(status.filter(s => s === 409), `status: ${status}`).toHaveLength(9);
  expect(await apiFunc.listarReservas(sala.id)).toHaveLength(1);
});
```

(Para provar ausência de corrida, repetir: `--repeat-each=20`; qualquer falha é achado.)

## 4. Permissão negada (autorização) — CT-24 (IDOR), CT-16 (sem login), CT-33 (papel)

```ts
// tests/us-05-permissoes.spec.ts
import { test, expect } from '../fixtures';

test('CT-24 funcionário B não cancela reserva do funcionário A', async ({ browser, novaReserva, apiFunc }) => {
  const r = await novaReserva();                         // criada como A (apiFunc = A)

  // B autenticado em contexto próprio
  const ctxB = await browser.newContext({ storageState: '.auth/funcionario2.json' });
  const pagB = await ctxB.newPage();

  // pela API com sessão de B: o que importa é o servidor recusar
  const resp = await ctxB.request.delete(`${process.env.API_URL}/reservas/${r.id}`);
  expect([403, 404]).toContain(resp.status());           // conforme ADR: registre qual

  // pela interface: o botão não deve existir para B, e a reserva de A não é listada para B
  await pagB.goto('/minhas-reservas');
  await expect(pagB.getByText(r.titulo)).toHaveCount(0);

  // estado final: reserva de A intacta
  expect((await apiFunc.obterReserva(r.id)).estado).toBe('ativa');
  await ctxB.close();
});

test.describe('sem login', () => {
  test.use({ storageState: { cookies: [], origins: [] } });

  test('CT-16 API de reservas exige autenticação', async ({ request }) => {
    const r = await request.get(`${process.env.API_URL}/reservas`);
    expect(r.status()).toBe(401);
    expect(await r.text()).not.toContain('titulo');      // nenhum dado vazou no corpo
  });
});

test.describe('como funcionário', () => {
  test.use({ storageState: '.auth/funcionario.json' });

  test('CT-33 funcionário não acessa o cadastro de salas', async ({ page, request }) => {
    await page.goto('/admin/salas');
    await expect(page.getByRole('heading', { name: 'Cadastro de salas' })).toHaveCount(0);
    await expect(page.getByText(/sem permissão|acesso negado/i)).toBeVisible();

    const r = await request.post(`${process.env.API_URL}/salas`, { data: { nome: 'X', capacidade: 4 } });
    expect(r.status()).toBe(403);                          // controle real é no servidor
  });
});
```

Ponto central: **a recusa é verificada na API**; esconder o botão não prova nada. Um teste de
permissão sem a asserção no servidor e sem estado final intacto é fraco.

## 5. Acessibilidade e teclado — CT-50, CT-53

Ver `acessibilidade-axe.md` (varredura axe por tela/estado e roteiro de teclado).

## 6. Formulário com validação — CT-15 (campo faltando)

```ts
test('CT-15 submeter sem título mostra erro associado ao campo', async ({ page, reservas, apiFunc }) => {
  await reservas.abrir();
  await reservas.preencher({ sala: 'Azul', data: '2026-12-01', inicio: '10:00', fim: '11:00' });
  await reservas.reservar.click();

  const campo = page.getByLabel('Título');
  await expect(campo).toHaveAttribute('aria-invalid', 'true');
  await expect(page.getByText('Informe o título')).toBeVisible();
  await expect(campo).toBeFocused();                       // foco no primeiro erro (boa prática; só se o CA pedir)
  // nada criado
  expect((await apiFunc.listarMinhas()).some(r => r.inicio.startsWith('2026-12-01T10'))).toBe(false);
});
```

## 7. Como o teste mapeia para o CT

| Elemento do CT | No teste |
|---|---|
| `Cobre: US-03 / CA-2` | Arquivo `us-03-...spec.ts`, `test.describe('US-03 ...')` |
| Título do CT | `test('CT-NN <título>')` |
| Pré-condições | Fixtures (`novaReserva`, `storageState`, `apiFunc`) |
| Passos | Ações pela interface/API, nessa ordem |
| Resultado esperado | Asserções de UI **e** de estado final |
| Tipo `segurança` | Asserção no servidor (status), nada alterado |
| Execução > Teste automatizado | `tests/us-03-reservar-sala.spec.ts::CT-14 ...` |

## 8. Revisão rápida de um teste (checklist)

- Nome com id do CT? Um comportamento?
- Dados criados pelo teste (via API), únicos?
- Localizadores por papel/rótulo/texto?
- Sem `waitForTimeout`, sem `isVisible()` em `expect` direto?
- Asserção de UI **e** de estado final (negativos)?
- Roda sozinho (`-g`) e com `--repeat-each=10`?
- Evidência de falha automática (trace/screenshot) ligada?
