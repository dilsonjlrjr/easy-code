---
name: qualidade-codigo
description: Como escrever código simples e fácil de mudar em qualquer linguagem - nomes, funções e módulos pequenos e coesos, tratamento de erros (falhar cedo, erros de domínio, nunca engolir exceção, nulidade explícita), SOLID e princípios na medida certa, duplicação versus abstração prematura, comentários só do porquê, refatoração segura com testes, validação na borda e tipos ricos, logging útil sem dado sensível, configuração por ambiente e segredos fora do código, commits pequenos (T-NN e fix(ACH-NN)), lint e formatação como gate e autorrevisão antes de concluir uma T. Use no Dev ao implementar, refatorar e fazer a autorrevisão.
---

# Qualidade de código

Código bom é o que **o próximo leitor entende e muda sem medo**. Não é o mais elegante nem o
mais genérico. A regra é simplicidade: faça a coisa mais simples que cumpre o critério de
pronto da T, com testes, e pare. Vale para qualquer linguagem; os exemplos mostram a ideia em
algumas (a idiomática de cada uma fica nas skills de linguagem).

## Princípios

1. **Simplicidade primeiro.** Entre duas soluções corretas, a com menos conceitos. Abstração
   se paga quando há **três** usos reais, não "talvez no futuro".
2. **Legibilidade é o custo dominante.** Código é lido muito mais do que escrito. Otimize o
   leitor.
3. **Nome diz a intenção.** Se precisa de comentário para explicar o que a linha faz, renomeie
   ou extraia.
4. **Falhe cedo, falhe alto.** Valide na borda; erro inesperado sobe, nunca some em silêncio.
5. **Estado inválido não deve ser representável.** Tipos ricos no lugar de strings e
   booleanos soltos.
6. **Mudança pequena, testada, reversível.** Refatore só com testes verdes; um commit por
   propósito.
7. **Consistência vence preferência.** Siga `estrutura.md`, as ADRs e o estilo do projeto,
   ainda que você faria diferente.
8. **Escopo é limite.** Só os arquivos da T. Melhoria fora do escopo vira ACH (`categoria: divida_tecnica`),
   não commit escondido.

## Regras de decisão

| Situação | Faça | Evite | Referência |
|---|---|---|---|
| Função passou de ~30 linhas ou faz "e" | Extrair por intenção | Dividir por tamanho sem sentido | nomes-funcoes-modulos |
| Parâmetros boolean que mudam o comportamento | Duas funções ou tipo enumerado | `f(x, true, false)` | nomes-funcoes-modulos |
| Trecho repetido 2 vezes | Deixar | Abstrair já | principios-abstracao-refatoracao |
| Trecho repetido 3 vezes com a mesma **razão de mudar** | Extrair | Copiar a quarta | principios-abstracao-refatoracao |
| Falha esperada do domínio (sala ocupada) | Erro de domínio tipado, mapeado na borda | Código HTTP na regra | erros-validacao-tipos |
| Falha inesperada (banco caiu) | Deixar subir, logar uma vez na borda | `catch` que ignora | erros-validacao-tipos |
| Valor pode faltar | Tipo opcional explícito | `null` espalhado | erros-validacao-tipos |
| Entrada externa (HTTP, JSON, env) | Validar e converter na borda para tipo de domínio | Validar em todo lugar | erros-validacao-tipos |
| Precisa de variação por ambiente | Variável de ambiente + `.env.example` | Constante, `if (prod)` | logging-config-segredos |
| Quero registrar algo | Log estruturado com contexto, sem dado pessoal | `println`, senha no log | logging-config-segredos |
| Interface com um implementador só e sem borda externa | Classe/função concreta | Interface "por padrão" | principios-abstracao-refatoracao |
| Código sem teste que preciso alterar | Teste de caracterização, depois refatorar | Mudar às cegas | principios-abstracao-refatoracao |

## Fluxo de trabalho (Dev, dentro de uma T)

1. Ler T, ADRs e `estrutura.md`; ver onde o código novo mora (pastas de `escopo_arquivos` da T).
2. Para cada comportamento: teste vermelho (skill `estrategia-testes`), código mínimo, verde.
3. **Refatorar a cada verde**: nomear, extrair, remover duplicação local, apagar código morto.
4. Antes de concluir: rodar formatador, lint, checagem de tipos, testes e build.
5. Fazer a **autorrevisão** (checklist abaixo) lendo o próprio diff como se fosse de outra
   pessoa.
6. Commit `T-NN: <título>`; atualizar os campos de acompanhamento da T (esquema `t.schema.json`):
   `situacao` (`em_andamento`, `concluida` ou `justificada`, esta com `justificativa`) e `registro_dev`
   (`commits`, `testes`, `desvios`). Só esses campos mudam, sem nova `versao` nem reabrir a etapa.
7. Algo fora do escopo que incomoda: anotar como dívida em `registro_dev.desvios` (ou ACH
   `categoria: divida_tecnica`), sem mexer.

Exemplo do que o Dev acrescenta à T ao concluir (resto do cabeçalho inalterado):

```yaml
situacao: concluida
registro_dev:
  commits: [a1b2c3d]
  testes: [backend/src/reserva/ReservaServiceTest, backend/src/reserva/ReservaApiIT]
  desvios: nenhum        # ou o que mudou e por quê
```

## Estilo na medida certa: o que NÃO fazer

- Camada, interface, factory, padrão ou configuração "para o futuro". Não precisou hoje, não
  entra. Ver `padroes-arquitetura-codigo` para o desenho; aqui, a regra é não exagerar.
- Comentário que repete o código, código comentado, `TODO` sem dono ou ACH.
- Generalizar com parâmetros e flags antes do segundo uso real.
- Biblioteca nova para resolver 5 linhas.
- Tratamento de erro para o que não pode acontecer; mas **nunca** engolir o que pode.
- Otimização sem medição.

## Checklist de autorrevisão antes de concluir uma T

Ler o diff completo (`git diff`) e conferir:

**Escopo e critério**
- [ ] Só arquivos de `escopo_arquivos` mudaram (ou o desvio está em `registro_dev.desvios`).
- [ ] Cada item de `criterios_pronto` está provado (teste ou comando).
- [ ] Nenhuma mudança de contrato/modelo (se houver, ACH com `etapa_destino: arquitetura` ou `dados`; contrato em `specs/dominio/achado.md`).

**Clareza**
- [ ] Nomes dizem intenção; sem abreviação obscura; vocabulário do glossário do projeto.
- [ ] Funções fazem uma coisa; sem aninhamento profundo (use retorno antecipado).
- [ ] Sem código morto, comentário redundante, `print`/log de depuração, `TODO` solto.

**Robustez**
- [ ] Entrada externa validada na borda; tipos de domínio dali em diante.
- [ ] Nenhum erro engolido; erros de domínio tipados; falha inesperada sobe.
- [ ] Nulidade explícita; sem `null`/`None` surpreendente.
- [ ] Recursos fechados (conexão, arquivo, cliente); timeouts em chamadas externas.

**Segurança e operação**
- [ ] Sem segredo, senha, token ou dado pessoal no código ou no log.
- [ ] Config por variável de ambiente e `.env.example` atualizado.
- [ ] SQL parametrizado; saída escapada pela camada de apresentação.

**Qualidade mecânica**
- [ ] Formatador, lint e checagem de tipos limpos; build e testes verdes.
- [ ] Sem aviso (warning) novo ignorado.
- [ ] Commit com mensagem `T-NN: <título>` (ou `fix(ACH-NN): <resumo>`), diff coeso.

## Erros comuns

| Erro | Efeito | Correção |
|---|---|---|
| Abstração antes do terceiro uso | Complexidade sem retorno; difícil de trocar | Duplicar até o padrão aparecer |
| `catch` vazio ou só `log` e segue | Falha silenciosa, dado corrompido | Tratar de verdade ou propagar |
| Lógica de negócio no controller/handler | Impossível testar sem HTTP | Mover para o serviço/domínio |
| Strings mágicas e booleanos soltos | Estado inválido representável | Tipos enumerados e valores de domínio |
| Comentário explicando o "o quê" | Mente quando o código muda | Renomear/extrair; comentar o "porquê" |
| Commit gigante "ajustes" | Impossível revisar/reverter | Um propósito por commit |
| Refatorar sem teste | Quebra silenciosa | Teste primeiro (caracterização) |
| Segredo "temporário" no código | Vai para o git para sempre | Variável de ambiente desde o início |
| Misturar refatoração e mudança de comportamento | Diff ilegível, bug escondido | Commits separados |
| Silenciar lint/aviso | Dívida invisível | Corrigir; exceção pontual com motivo |

## Índice dos references

| Arquivo | Ler quando |
|---|---|
| `references/nomes-funcoes-modulos.md` | Para nomear, dividir funções, organizar módulos coesos e evitar acoplamento |
| `references/erros-validacao-tipos.md` | Para desenhar tratamento de erros, nulidade, validação na borda e tipos ricos |
| `references/principios-abstracao-refatoracao.md` | Para decidir sobre SOLID, duplicação x abstração, comentários e refatorar com segurança |
| `references/logging-config-segredos.md` | Para logs úteis, configuração por ambiente e segredos |
| `references/commits-lint-autorreVISAO.md` | Para commits e mensagens, lint/formatação como gate e a revisão do diff |

Skills vizinhas: `estrategia-testes` (teste primeiro, refatoração segura),
`padroes-arquitetura-codigo` (desenho e camadas), `modelagem-postgresql`, `ambiente-docker`,
`ux-acessibilidade` e `frontend-vue` (front), skills de linguagem em `agentes/skills/linguagens/`
(idiomas e ferramentas específicos) e a skill de segurança (`revisao-seguranca`) para o que
a Segurança vai conferir.
