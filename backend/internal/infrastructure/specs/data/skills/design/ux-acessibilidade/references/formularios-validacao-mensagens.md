# Formulários, validação e mensagens de erro e sucesso

Formulários são onde sistemas internos mais falham em usabilidade e acessibilidade. Esta
referência traz padrões prontos, com HTML, para o protótipo e para o Dev (Vue) reproduzirem.

## 1. Estrutura do campo

Cada campo tem: **rótulo**, **controle**, **dica opcional**, **mensagem de erro** (quando
houver), ligados semanticamente.

```html
<div class="campo">
  <label for="nome">Nome da sala <span class="obrigatorio">(obrigatório)</span></label>
  <p id="nome-dica" class="dica">Use um nome que as pessoas reconheçam, como "Sala Atlântico".</p>
  <input id="nome" name="nome" type="text" autocomplete="off" required
         aria-describedby="nome-dica nome-erro" aria-invalid="false">
  <p id="nome-erro" class="erro" hidden>
    <span class="icone-erro" aria-hidden="true"></span> Informe o nome da sala.
  </p>
</div>
```

Regras:

| Regra | Motivo |
|---|---|
| `<label for>` visível e persistente | Placeholder some ao digitar e tem contraste baixo (WCAG 1.3.1, 3.3.2) |
| Rótulo acima do campo | Leitura mais rápida e funciona com zoom e celular |
| Indicar obrigatório em **texto** | Asterisco sozinho é ambíguo; explique no topo ou escreva "(obrigatório)" |
| Dica entre rótulo e campo, ligada por `aria-describedby` | Leitor de tela anuncia junto |
| `type` e `inputmode` corretos (`email`, `tel`, `numeric`) | Teclado certo no celular, validação nativa |
| `autocomplete` com o token correto quando o campo é dado do usuário | WCAG 1.3.5 (identificar finalidade); ajuda gerenciadores de senha |
| Agrupar campos relacionados com `<fieldset>` e `<legend>` | Rádios, datas compostas, endereços |
| Botão de envio com rótulo de ação ("Reservar sala") | Evita "Enviar"/"OK" |

### Quando usar cada controle

| Dado | Controle | Observação |
|---|---|---|
| Escolha entre 2 a 5 opções | Botões de rádio | Mostra todas as opções |
| Escolha de várias | Caixas de seleção (`checkbox`) | Em `fieldset` |
| Escolha entre 6 a ~15 | `<select>` nativo | Prefira nativo a customizado |
| Lista longa com busca | Combobox (seguir padrão WAI-ARIA) | Só se justificar; é caro de fazer acessível |
| Data | `<input type="date">` ou campos de dia/mês/ano | Teste em celular; defina mínimo/máximo |
| Hora | `<input type="time" step="1800">` ou `<select>` com passos | Passo coerente com a regra (30 min) |
| Sim/não único | Caixa de seleção ou interruptor com `role="switch"` | Rótulo diz o efeito |
| Texto longo | `<textarea>` com contador se há limite | Anuncie o limite |

## 2. Validação

### Quando validar

| Momento | Quando usar |
|---|---|
| **No envio** | Padrão; sempre validar também no servidor |
| **Ao sair do campo** (blur) | Formato simples (e-mail), depois que o usuário tocou o campo |
| **Enquanto digita** | Só para ajudar (contador, força de senha), não para reprovar cedo demais |

Evite mostrar erro antes de o usuário terminar de digitar. **Validação do cliente é conforto;
a verdade é a do servidor** (que devolve erros por campo).

### O que fazer no envio com erro

1. **Não apagar** o que o usuário digitou.
2. Exibir **resumo de erros** no topo do formulário (se mais de 1 campo ou formulário longo),
   com link para cada campo.
3. Exibir a **mensagem junto do campo**.
4. Mover o **foco** para o resumo (ou para o primeiro campo inválido).
5. Marcar o campo com `aria-invalid="true"` e associar a mensagem por `aria-describedby`.
6. Usar texto + ícone + cor (nunca só cor).

```html
<div id="resumo-erros" role="alert" tabindex="-1" class="resumo-erros" hidden>
  <h2>Corrija 2 campos para continuar</h2>
  <ul>
    <li><a href="#termino">O horário de término deve ser depois do início</a></li>
    <li><a href="#data">Escolha uma data a partir de hoje</a></li>
  </ul>
</div>
```

Atenção ao `role="alert"`: anuncia imediatamente. Use no resumo ou na mensagem global; para
mensagens menos urgentes, `role="status"`.

### Critérios WCAG relacionados

3.3.1 (identificar o erro em texto), 3.3.2 (rótulos ou instruções), 3.3.3 (sugerir correção),
3.3.4 (prevenção de erro para dados legais/financeiros/ações irreversíveis), 3.3.7 (não pedir
de novo o que já foi informado), 3.3.8 (autenticação acessível). Detalhes em
`wcag-22-aa-detalhado.md`.

## 3. Mensagens de erro: fórmula e catálogo

**Fórmula:** `O que aconteceu` + `por quê (se útil)` + `o que fazer`. Linguagem do usuário.
Sem códigos técnicos, sem culpar, sem humor.

| Situação | Ruim | Bom |
|---|---|---|
| Campo vazio | "Campo inválido" | "Informe o nome da sala." |
| Formato | "Erro de formato" | "Digite um e-mail no formato nome@empresa.com.br." |
| Fim antes do início | "Horário inválido" | "O horário de término deve ser depois do início (10h)." |
| Data no passado | "Data inválida" | "Escolha uma data a partir de hoje." |
| Conflito de regra | "Erro 409" | "Esta sala já está reservada das 10h às 11h. Escolha outro horário ou outra sala." |
| Sem permissão | "403 Forbidden" | "Você não tem permissão para cancelar esta reserva. Fale com o administrador." |
| Sessão expirada | "Unauthorized" | "Sua sessão expirou. Entre novamente para continuar; o que você digitou foi mantido." |
| Rede | "Network Error" | "Não foi possível conectar. Verifique sua conexão e tente de novo." |
| Servidor | "Internal Server Error" | "Algo falhou do nosso lado. Tente de novo em instantes; se persistir, fale com o suporte (código: abc123)." |
| Limite | "Valor inválido" | "A reserva pode ter no máximo 8 horas." |

Dicas:

- Nomeie o campo na mensagem de resumo ("Horário de término: ...").
- Para conflito, inclua o dado que ajuda a resolver (o horário ocupado, próximas opções).
- Para erro de servidor, mostre um **código de correlação** que o suporte consiga rastrear
  (casa com o RNF de observabilidade).
- Mensagens de login **genéricas** ("E-mail ou senha incorretos") por segurança.

## 4. Mensagens de sucesso

| Regra | Exemplo |
|---|---|
| Confirmar **o que** foi feito, com os dados | "Reserva criada: Sala Atlântico, qua 10/03, 10h–11h." |
| Mostrar o próximo passo natural | Link "Ver minhas reservas" |
| Anunciar para leitor de tela | Região `role="status"` (educada, sem tirar o foco) |
| Não depender de desaparecer sozinha | Se some, dar tempo suficiente ou mantê-la; WCAG 2.2.1 |
| Não só toast efêmero | Sucesso importante também fica refletido na tela (item na lista) |

```html
<div role="status" class="mensagem mensagem--sucesso">
  Reserva criada: Sala Atlântico, quarta 10/03, 10h às 11h.
  <a href="minhas-reservas.html">Ver minhas reservas</a>
</div>
```

## 5. Estado do botão de envio

- Não desabilite o botão para "forçar" preenchimento sem explicar: o usuário não entende por
  que não pode clicar. Prefira deixar habilitado e mostrar os erros ao enviar.
- Durante o envio: mudar o texto ("Reservando...") e prevenir envio duplo; manter foco
  coerente.
- Após sucesso: mover o foco para a mensagem ou para o destino (título da próxima tela).

## 6. Confirmação de ações destrutivas

Padrão de diálogo:

```html
<dialog id="dlg-cancelar" aria-labelledby="dlg-titulo" aria-describedby="dlg-desc">
  <h2 id="dlg-titulo">Cancelar a reserva?</h2>
  <p id="dlg-desc">Sala Atlântico, quarta 10/03, 10h às 11h. O horário voltará a ficar livre.</p>
  <button type="button" id="dlg-confirmar">Cancelar reserva</button>
  <button type="button" id="dlg-voltar">Manter reserva</button>
</dialog>
```

Regras: título e botão **nomeiam a ação** (não "Sim"/"Não"); botão que preserva aparece e tem
foco inicial quando a ação é irreversível; Esc fecha; foco volta ao elemento de origem.
Alternativa melhor quando viável: **desfazer** por alguns segundos.

## 7. Formulários longos e multi-passos

- Divida em seções com `<fieldset>`/`<legend>` e títulos.
- Indique progresso ("Passo 2 de 3") em texto.
- Permita voltar sem perder dados.
- Não peça novamente o que já foi informado (WCAG 3.3.7).
- Resumo antes de confirmar quando a ação é irreversível ou legal.

## 8. Autenticação (login) acessível

- Campos `autocomplete="username"` e `autocomplete="current-password"`.
- Permitir colar senha e usar gerenciador de senhas (WCAG 3.3.8).
- Não exigir teste cognitivo (decifrar imagem, lembrar enigma) sem alternativa.
- Botão "mostrar senha" com rótulo e estado (`aria-pressed`).
- Mensagem genérica de falha; link "Esqueci minha senha" visível.

## 9. Exemplo completo: formulário Nova reserva

Campos: Sala (select), Data (date), Início (time), Término (time). Estados:

| Estado | O que mostra |
|---|---|
| Inicial | Data = hoje; campos vazios sem erro |
| Erro de validação | Resumo no topo; mensagem junto do campo; dados preservados |
| Erro de conflito | Resumo: "Esta sala já está reservada das 10h às 11h. Escolha outro horário ou outra sala."; foco no resumo |
| Enviando | Botão "Reservando..." (desabilitado só durante o envio) |
| Sucesso | Mensagem de status com os dados; link para "Minhas reservas" |
| Sessão expirada | Mensagem + redirecionar ao login preservando dados |

## 10. Anti-padrões

- Placeholder como rótulo.
- Erro só em vermelho; erro no topo da página sem ligação ao campo.
- Limpar o formulário ao falhar.
- Validar a cada tecla com mensagens agressivas.
- Mensagem técnica ("Constraint violation").
- Botão desabilitado sem explicação.
- "Tem certeza?" com "OK/Cancelar" ambíguo.
- Datas digitadas em texto livre sem formato indicado.
- Toast de erro que some antes de ser lido.
- Máscara que impede colar.

## 11. Checklist

- [ ] Todo campo com `<label>` visível e associado.
- [ ] Obrigatório indicado em texto; dicas ligadas por `aria-describedby`.
- [ ] `type`, `inputmode` e `autocomplete` apropriados.
- [ ] Validação no envio (e servidor); dados preservados no erro.
- [ ] Resumo de erros + mensagem junto do campo + foco movido.
- [ ] Mensagens com fórmula (o quê + como resolver), sem códigos técnicos.
- [ ] Sucesso confirma o que foi feito, anunciado por região de status.
- [ ] Ação destrutiva com diálogo que nomeia a ação, ou desfazer.
- [ ] Login com autocomplete, colar permitido, mensagem genérica.
