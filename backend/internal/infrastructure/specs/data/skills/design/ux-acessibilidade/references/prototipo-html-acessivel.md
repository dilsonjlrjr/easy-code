# Protótipo HTML acessível

O protótipo é **HTML + CSS + JS puro, sem build**, abre com duplo clique, tem uma página por
tela, dados fictícios coerentes com o contrato da API e links entre telas. Ele é o modelo que o
Dev reimplementa em Vue: **semântica, nomes de campo, mensagens e estados do protótipo são
contrato**.

## 1. Estrutura de pastas

```
design/prototipo/
  index.html              (ou login.html)  entrada
  salas.html
  nova-reserva.html
  minhas-reservas.html
  admin-salas.html
  sem-permissao.html
  estilo.css              tokens e componentes (nomes iguais ao estilo.md)
  app.js                  JS mínimo: estados de demonstração, diálogo, mensagens
  dados.js                (opcional) dados fictícios no formato do contrato da API
```

Regras: caminhos **relativos** (funciona via `file://`); **sem CDN e sem recurso remoto**: fontes,
bibliotecas e imagens ficam embutidas no próprio protótipo (arquivo local na pasta ou `data:`),
porque a origem de conteúdo que serve o protótipo tem CSP fechada (`default-src 'self'`,
`style-src 'self' 'unsafe-inline'`, `img-src 'self' data:`, `font-src 'self' data:`,
`connect-src 'none'`, `form-action 'none'`; ADR-19) e bloqueia o resto; um único `estilo.css`; JS opcional e pequeno (o protótipo deve fazer sentido sem
ele, exceto estados interativos).

## 2. Esqueleto de página

```html
<!doctype html>
<html lang="pt-BR">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Nova reserva - Reserva de Salas</title>
  <link rel="stylesheet" href="estilo.css">
</head>
<body>
  <a class="pular" href="#conteudo">Pular para o conteúdo</a>

  <header class="topo">
    <a class="marca" href="salas.html">Reserva de Salas</a>
    <nav aria-label="Principal">
      <ul>
        <li><a href="salas.html">Salas</a></li>
        <li><a href="minhas-reservas.html">Minhas reservas</a></li>
        <li><a href="admin-salas.html">Administração</a></li>
      </ul>
    </nav>
    <p class="usuario">Ana Souza <a href="index.html">Sair</a></p>
  </header>

  <main id="conteudo" tabindex="-1">
    <h1>Nova reserva</h1>
    <div id="mensagens" role="status"></div>
    <!-- conteúdo da tela -->
  </main>

  <footer class="rodape">
    <a href="#">Ajuda e contato</a> · <a href="#">Aviso de privacidade</a>
  </footer>
  <script src="app.js"></script>
</body>
</html>
```

Pontos:

| Elemento | Por quê |
|---|---|
| `lang="pt-BR"` | 3.1.1; leitor de tela escolhe a voz certa |
| `<title>` único por tela: "Tela - Sistema" | 2.4.2; título é o primeiro anúncio |
| Link "Pular para o conteúdo" | 2.4.1; visível ao receber foco |
| `<header>`, `<nav aria-label>`, `<main>`, `<footer>` | Regiões navegáveis (1.3.1); `aria-label` distingue múltiplos `<nav>` |
| Um `<h1>` por página, igual ao nome no menu | Orientação |
| `<main tabindex="-1">` | Permite mover o foco programaticamente ao trocar de contexto |
| Região `role="status"` presente **desde o carregamento**, vazia | Leitores anunciam mudanças em regiões já existentes (4.1.3) |
| `aria-current="page"` no item de menu atual | Indica localização |
| Rodapé com ajuda no mesmo lugar | 3.2.6 |

Link para pular: CSS que o esconde fora da tela e o mostra ao focar:

```css
.pular { position: absolute; left: -999px; top: 0; background: var(--cor-superficie);
         padding: var(--espaco-3); z-index: 100; }
.pular:focus { left: var(--espaco-4); top: var(--espaco-4); }
```

## 3. Semântica antes de ARIA

**Primeira regra do ARIA:** se existe elemento HTML nativo com a semântica e o comportamento
que você precisa, use-o em vez de recriar com `div` + ARIA.

| Necessidade | Use | Não use |
|---|---|---|
| Ação | `<button type="button">` | `<div onclick>`, `<a href="#">` para ação |
| Navegação | `<a href>` | `<button>` que navega |
| Lista de itens | `<ul>/<ol>` | `<div>`s soltas |
| Dados tabulares | `<table>` com `<caption>`, `<th scope>` | Grade de `div`s |
| Escolha de opções | `<select>`, `<input type="radio">` | Menu custom sem teclado |
| Data/hora | `<input type="date">`/`time` | Calendário custom (custo alto) |
| Diálogo | `<dialog>` com `showModal()` | `div` com `role="dialog"` feito à mão |
| Revelar/ocultar | `<details><summary>` | `div` com JS |
| Barra de progresso | `<progress>` | `div` de largura variável |

### Quando ARIA é necessário (e como)

| Caso | Atributo |
|---|---|
| Botão só com ícone | `aria-label="Cancelar reserva de 10h"` (ou texto oculto visualmente) |
| Associar dica/erro ao campo | `aria-describedby="id-dica id-erro"` |
| Campo inválido | `aria-invalid="true"` |
| Menu recolhível | `aria-expanded` (true ou false) + `aria-controls` no botão |
| Página atual no menu | `aria-current="page"` |
| Região dinâmica de status | `role="status"` (educado) / `role="alert"` (urgente) |
| Região ocupada | `aria-busy="true"` enquanto carrega |
| Link/botão repetido (várias linhas "Cancelar") | `aria-label` ou texto oculto com o contexto |
| Lista ou `nav` múltiplos | `aria-label` distinto |
| Elemento decorativo | `aria-hidden="true"` |

Regras: ARIA **não** muda comportamento (não torna um `div` focável nem aciona com Enter);
estado ARIA precisa ser **atualizado** pelo JS; ARIA inválida/contraditória é pior que nenhuma;
nunca `role="button"` em `<button>`, nunca `aria-label` que difere do texto visível (2.5.3).

## 4. Exemplos de componentes

### Tabela de dados

```html
<table>
  <caption>Minhas reservas futuras</caption>
  <thead>
    <tr><th scope="col">Sala</th><th scope="col">Data</th><th scope="col">Horário</th><th scope="col">Ações</th></tr>
  </thead>
  <tbody>
    <tr>
      <th scope="row">Sala Atlântico</th><td>10/03/2026</td><td>10h às 11h</td>
      <td><button type="button" class="botao botao--perigo" data-cancelar="r-101"
                  aria-label="Cancelar reserva da Sala Atlântico em 10/03, 10h às 11h">Cancelar</button></td>
    </tr>
  </tbody>
</table>
```

Em tela estreita: transformar linhas em cartões via CSS (`display:block` nas células com
`data-rotulo`) mantendo a semântica, ou conter a rolagem em um `div` com `role="region"`,
`aria-labelledby` e `tabindex="0"`.

### Mensagem global de status

```html
<div id="mensagens" role="status"></div>
<script>
  function mostrarMensagem(tipo, texto) {
    const area = document.getElementById('mensagens');
    area.innerHTML = '';
    const p = document.createElement('p');
    p.className = 'mensagem mensagem--' + tipo;
    p.textContent = texto;            // textContent evita injeção
    area.appendChild(p);
  }
</script>
```

Erros urgentes (conflito): região `role="alert"` ou mover o foco para o resumo.

### Diálogo de confirmação (nativo)

```html
<dialog id="dlg-cancelar" aria-labelledby="dlg-titulo">
  <h2 id="dlg-titulo">Cancelar a reserva?</h2>
  <p>Sala Atlântico, 10/03, 10h às 11h. O horário voltará a ficar livre.</p>
  <div class="acoes">
    <button type="button" id="dlg-voltar" autofocus>Manter reserva</button>
    <button type="button" id="dlg-confirmar" class="botao botao--perigo">Cancelar reserva</button>
  </div>
</dialog>
<script>
  let gatilho;
  document.querySelectorAll('[data-cancelar]').forEach(b => b.addEventListener('click', () => {
    gatilho = b; document.getElementById('dlg-cancelar').showModal();
  }));
  document.getElementById('dlg-voltar').addEventListener('click', () => {
    document.getElementById('dlg-cancelar').close(); gatilho.focus();
  });
</script>
```

`<dialog>` modal já prende o foco, fecha com Esc e deixa o fundo inerte. Garanta que o foco
**volta ao gatilho** ao fechar, e que o botão de confirmar nomeia a ação.

### Seletor de estado de demonstração (para o protótipo)

```html
<nav aria-label="Estados de demonstração" class="demo">
  <p>Demonstração:</p>
  <a href="?estado=padrao">com dados</a>
  <a href="?estado=vazio">vazio</a>
  <a href="?estado=carregando">carregando</a>
  <a href="?estado=erro">erro</a>
</nav>
<script>
  const estado = new URLSearchParams(location.search).get('estado') || 'padrao';
  document.body.dataset.estado = estado;
</script>
```

```css
[data-estado] .so-padrao, [data-estado] .so-vazio, [data-estado] .so-carregando, [data-estado] .so-erro { display: none; }
[data-estado="padrao"] .so-padrao, [data-estado="vazio"] .so-vazio,
[data-estado="carregando"] .so-carregando, [data-estado="erro"] .so-erro { display: block; }
```

Funciona via `file://`. Rotule claramente como demonstração e marque no `wireframes.md` que
**não é parte do produto**.

### Formulário completo (trecho)

Ver `formularios-validacao-mensagens.md` (estrutura do campo, resumo de erros). No protótipo,
simule validação no `submit` com JS mínimo: previna o envio, preencha o resumo, mova o foco
para ele e marque `aria-invalid`.

```js
form.addEventListener('submit', (e) => {
  e.preventDefault();
  const erros = validar(form);                 // retorna [{campo, mensagem}]
  if (erros.length) {
    exibirResumo(erros);                       // monta a lista com links para os campos
    document.getElementById('resumo-erros').focus();
  } else {
    mostrarMensagem('sucesso', 'Reserva criada: Sala Atlântico, 10/03, 10h às 11h.');
  }
});
```

## 5. CSS essencial de acessibilidade

```css
:focus-visible { outline: 3px solid var(--cor-foco); outline-offset: 2px; }
html { scroll-padding-top: 5rem; }              /* foco não escondido por cabeçalho fixo (2.4.11) */
.visualmente-oculto { position: absolute; width: 1px; height: 1px; margin: -1px; padding: 0;
  overflow: hidden; clip: rect(0 0 0 0); white-space: nowrap; border: 0; }
button, a.botao, input, select, textarea { min-height: var(--alvo-min); }  /* alvos (2.5.8) */
@media (prefers-reduced-motion: reduce) { * { animation: none !important; transition: none !important; } }
```

Evite `display:none`/`visibility:hidden` para conteúdo que deve ser lido por leitor de tela;
use `.visualmente-oculto`. Use `hidden` quando o conteúdo realmente não deve existir.

## 6. Dados fictícios coerentes com o contrato

- Use os mesmos **nomes de campo** do `openapi.yaml` (ex.: `salaId`, `inicio`, `fim`), formatos
  e enumerações.
- Dados realistas e variados: nomes acentuados, textos longos, lista com 0, 1 e muitos itens,
  horários nos limites (encostados, fim do dia), conflito.
- Datas coerentes (futuras), formatadas em pt-BR na interface.
- Nada de dado pessoal real.

Se o protótipo precisar de campo ou ação que o contrato não tem, **abra um ACH com
`categoria: lacuna_contrato` (`etapa_destino: arquitetura`)** e liste o id em `lacunas_contrato` do TELAS; não invente endpoint.

## 7. Responsividade

- `<meta name="viewport">` presente; layout fluido com `flex`/`grid` e `rem`.
- Pontos de quebra em `estilo.md`; verificar 360 px, 768 px, 1366 px.
- Sem rolagem horizontal da página a 320 px.
- Tabelas largas conforme seção 4.
- Alvos de toque >= 44 px em mobile, nunca < 24 px.

## 8. Verificação do protótipo

1. Abrir cada página por `file://`; todos os links funcionam; nenhum 404.
2. Rodar axe-core/Lighthouse/pa11y por página e por estado relevante.
3. Percorrer com teclado: ordem, foco visível, diálogo, menu.
4. Ouvir com leitor de tela: título, regiões, rótulos, erros, status.
5. Zoom 200% e viewport de 320 px.
6. Conferir contraste das cores do `estilo.css` (tabela do `estilo.md`).
7. Conferir nomes de campo contra o contrato; textos pt-BR.
8. Conferir `TELAS.md`: cada tela existe como arquivo; cada CA visível aparece em um
   estado.

## 9. Anti-padrões

- `<div>`/`<span>` clicáveis; `<a href="#">` para ação.
- Ordem visual diferente da ordem do DOM.
- `tabindex` positivo.
- ARIA redundante ou contraditória (`role="button"` num `<button>`).
- Modal feito à mão sem foco gerenciado.
- `alert()`/`confirm()` nativos como padrão de UX (não estilizáveis, difíceis de testar).
- Mensagens inseridas com `innerHTML` de entrada do usuário (injeção).
- Dependências externas (CDN, Google Fonts, imagem remota): a CSP da origem de conteúdo bloqueia e o protótipo quebra. Embuta fonte, biblioteca e imagem.
- Seletor de demonstração confundido com interface do produto.
- Estados só descritos, nunca desenhados.
- Campos com nomes diferentes dos do contrato.

## 10. Checklist

- [ ] Estrutura de pasta e arquivos conforme `TELAS.md`; caminhos relativos; abre por duplo clique.
- [ ] Todas as páginas com `lang`, `<title>` único, link de pular, regiões, um `<h1>`.
- [ ] Semântica nativa; ARIA só onde necessário e correta.
- [ ] Todos os estados do `wireframes.md` acessíveis (parâmetro ou páginas).
- [ ] Teclado completo; foco visível; diálogo com retorno de foco.
- [ ] Rótulos, erros ligados, status anunciados.
- [ ] Dados fictícios coerentes com o contrato; textos em pt-BR.
- [ ] Varredura automática limpa e roteiro manual executado.
