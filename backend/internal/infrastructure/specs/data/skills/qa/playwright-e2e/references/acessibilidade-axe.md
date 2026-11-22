# Acessibilidade com `@axe-core/playwright`

`axe-core` (Deque, licença MPL-2.0, open source) analisa a página renderizada contra regras de
acessibilidade; `@axe-core/playwright` o integra ao Playwright. Serve ao RNF de acessibilidade
(WCAG 2.2 AA): **detecta parte dos problemas** (a literatura estima algo em torno de um terço a
metade); o resto exige teclado e leitor de tela manuais.

## 1. Instalação e uso básico

```bash
npm i -D @axe-core/playwright
```

```ts
import { test, expect } from '../fixtures';
import AxeBuilder from '@axe-core/playwright';

test('CT-50 tela de login sem violações WCAG A/AA @a11y', async ({ page }) => {
  await page.goto('/login');
  await expect(page.getByRole('button', { name: 'Entrar' })).toBeVisible(); // página pronta

  const resultado = await new AxeBuilder({ page })
    .withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'wcag22aa'])
    .analyze();

  expect(resultado.violations).toEqual([]);
});
```

- **Espere a página estar pronta antes de analisar** (um elemento que só existe depois do
  carregamento). Analisar durante o carregamento gera falso negativo/positivo.
- As tags de conjunto de regras (`wcag2a`, `wcag2aa`, `wcag21a`, `wcag21aa`, `wcag22aa`,
  `best-practice`) selecionam regras; confirmar as tags disponíveis na versão instalada do
  axe-core (a de WCAG 2.2 AA existe nas versões recentes; `agentes/ambiente.md`).
- Em falha, `toEqual([])` mostra o JSON das violações: id da regra, impacto, nós afetados.
  Para evidência legível, formate:

```ts
function resumo(v: any[]) {
  return v.map(x => `${x.id} (${x.impact}): ${x.help} -> ${x.nodes.map((n: any) => n.target.join(' ')).join(' | ')}`).join('\n');
}
expect(resultado.violations, resumo(resultado.violations)).toEqual([]);
```

## 2. Escopo: quais telas e estados

O RNF define o escopo. Cubra **cada tela em escopo** e os **estados relevantes** (cada um é
uma "tela" para o axe):

| Estado | Como chegar |
|---|---|
| Tela inicial/lista | `goto` |
| Formulário vazio | `goto` |
| Formulário com **erros de validação** | submeter vazio e analisar depois que os erros aparecem |
| Modal/diálogo aberto | abrir e analisar com `.include('[role=dialog]')` ou página inteira |
| Menu/dropdown expandido | expandir e analisar |
| Lista vazia, estado de carregamento, mensagem de erro | provocar o estado |
| Telas por papel (admin) | `storageState` do papel |

```ts
test('CT-51 formulário com erros de validação sem violações @a11y', async ({ page }) => {
  await page.goto('/reservas/nova');
  await page.getByRole('button', { name: 'Reservar' }).click();
  await expect(page.getByRole('alert').first()).toBeVisible();
  const r = await new AxeBuilder({ page }).withTags(['wcag2a','wcag2aa']).analyze();
  expect(r.violations).toEqual([]);
});
```

Teste parametrizado para varrer várias rotas:

```ts
for (const rota of ['/login', '/salas', '/reservas/nova', '/minhas-reservas']) {
  test(`CT-52 ${rota} sem violações A/AA @a11y`, async ({ page }) => {
    await page.goto(rota);
    await page.waitForLoadState('domcontentloaded');
    await expect(page.getByRole('main')).toBeVisible();
    const r = await new AxeBuilder({ page }).withTags(['wcag2a','wcag2aa']).analyze();
    expect(r.violations).toEqual([]);
  });
}
```

(Título dos testes com id único de CT: use `CT-52a`, `CT-52b` ou um CT por rota.)

## 3. Refinar: incluir, excluir, regras

```ts
new AxeBuilder({ page })
  .include('main')                         // restringe a região
  .exclude('#widget-de-terceiros')         // exclui conteúdo que não controlamos (justificar)
  .disableRules(['color-contrast'])        // só com justificativa registrada
  .analyze();
```

Regras:
- **Excluir ou desabilitar exige justificativa escrita** no teste (comentário) e no REL-QA; é
  exceção, não atalho. "Para passar" não é justificativa.
- Violação persistente que o Dev decide aceitar: registre em ACH com `situacao: aceito` e `aceite` (`por`, `quando`, `justificativa`) preenchido pelo aprovador humano,
  não esconda no código do teste.
- Para dívida conhecida, é possível comparar com uma **lista base** de violações tolerantes e
  falhar só em novas; documente a lista e a data de revisão.

## 4. Ler o resultado

Cada violação traz: `id` (regra), `impact` (`minor`, `moderate`, `serious`, `critical`),
`help`, `helpUrl`, `nodes[]` (seletor, trecho HTML, motivo). Além de `violations` há
`incomplete` (o axe **não conseguiu decidir**: ex.: contraste sobre imagem) — **revisão
manual**; liste no REL-QA como pendência manual, não ignore.

Mapa de severidade para ACH (`severidade: critico | alto | medio | baixo`, junto do RNF e de `specs/dominio/achado.md`, seção 3; `categoria: defeito`):

| Impacto axe / situação | Severidade do ACH sugerida |
|---|---|
| Bloqueia a tarefa para usuário de tecnologia assistiva (campo sem rótulo, botão sem nome, armadilha de foco) em fluxo de US Must | Alto |
| `serious`/`critical` em fluxo secundário | Médio |
| `moderate`/`minor`, ou melhoria | Baixo |

(Ajuste ao que o RNF de acessibilidade determinar como obrigatório: RNF Must descumprido = Alto.)

## 5. Teclado (o axe não faz)

Teste e2e do roteiro de teclado do fluxo principal:

```ts
test('CT-53 reservar sala só com teclado @a11y', async ({ page }) => {
  await page.goto('/reservas/nova');
  await page.getByLabel('Data').focus();
  await page.keyboard.type('2026-12-01');
  await page.keyboard.press('Tab');
  await expect(page.getByLabel('Início')).toBeFocused();   // ordem lógica
  // ... preencher e acionar por teclado
  await page.keyboard.press('Tab');                        // até o botão
  await expect(page.getByRole('button', { name: 'Reservar' })).toBeFocused();
  await page.keyboard.press('Enter');
  await expect(page.getByRole('status')).toContainText('Reserva criada');
});
```

Verificações de teclado por e2e: alcance de todos os controles, ordem de foco, `Esc` fecha
diálogo e devolve o foco ao gatilho, foco **visível** (via asserção sobre estilo computado é
frágil; em geral verificar no roteiro manual/screenshot), sem armadilha de foco (Tab sai do
componente).

```ts
await page.getByRole('button', { name: 'Abrir filtros' }).click();
await expect(page.getByRole('dialog')).toBeVisible();
await page.keyboard.press('Escape');
await expect(page.getByRole('dialog')).toBeHidden();
await expect(page.getByRole('button', { name: 'Abrir filtros' })).toBeFocused();
```

## 6. O que o axe **não** cobre (manual no REL-QA)

| Item | Como verificar |
|---|---|
| Qualidade do texto alternativo (existe mas é inútil) | Revisão |
| Ordem de foco lógica e foco visível adequado | Teclado manual |
| Contraste em imagem/gradiente (`incomplete`) | Medição manual |
| Leitor de tela anuncia estados e erros | NVDA/Orca/VoiceOver, roteiro |
| Zoom 200% e reflow 320 px | Manual (também dá para `page.setViewportSize({width:320,...})` e checar rolagem horizontal) |
| Alvos de toque, movimento, linguagem clara | Revisão |
| Conteúdo gerado dinamicamente em estados raros | Provocar o estado e analisar |

No REL-QA, na seção Observações: "Varredura axe reporta ausência das violações que detecta; não
comprova conformidade WCAG completa. Itens manuais pendentes: ...".

## 7. Anti-padrões

- Declarar "WCAG AA atendido" só porque o axe passou.
- Analisar só a tela inicial e não os estados de erro/modais.
- `disableRules` em massa para ficar verde.
- Analisar antes da página carregar.
- Ignorar `incomplete`.
- Não testar com o papel admin (telas diferentes).
