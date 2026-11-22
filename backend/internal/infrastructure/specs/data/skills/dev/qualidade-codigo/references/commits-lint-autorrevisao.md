# Commits, lint como gate e autorrevisão

## Commits

### Padrão do projeto (instruções do Dev)

| Situação | Mensagem | Exemplo |
|---|---|---|
| Uma T concluída | `T-NN: <título>` | `T-05: Endpoint de criar reserva com checagem de conflito` |
| Onda de retrabalho de achado | `fix(ACH-NN): <resumo>` | `fix(ACH-03): reserva colada deixa de conflitar` |

- **Um commit por T** é o mínimo exigido; dentro dele, o diff deve ser coeso. Se durante a T
  houver passos naturais (migration, depois regra, depois endpoint), commits intermediários
  são bem-vindos **desde que** o build fique verde em cada um, e o último carrega `T-NN:` no
  padrão. Em dúvida, siga as instruções do Dev: um commit `T-NN: <título>` por T.
- Primeira linha até ~72 caracteres, no imperativo ou título direto, sem ponto final. Corpo
  (opcional) responde **por que**, não repete o diff; separado por linha em branco.
- O commit de `fix(ACH-NN)` contém **o teste de regressão e a correção juntos**.
- Mensagem em pt-BR (idioma dos artefatos), com o termo do glossário (Achado, Tarefa).
- Atribuição de coautoria conforme a regra do ambiente quando aplicável.

### Propósito único

| Misturar | Separar em |
|---|---|
| Refatoração + mudança de comportamento | Dois commits (refatoração com testes verdes antes) |
| Formatação em massa + lógica | Commit só de formatação, antes |
| Feature + correção de bug alheio | Bug alheio vira ACH, não carona |
| Dependência nova + uso | Pode ir junto, se a T exige; dizer no corpo o porquê |

### Higiene

- `git status` e `git diff --staged` antes de cada commit; nada de arquivo gerado, `.env`,
  `node_modules`, `dist`, binário, dado real.
- `.gitignore` correto desde a primeira T (`git init` na primeira tarefa).
- Nunca `--no-verify` para passar por hook que falha; corrija a causa.
- Nunca reescrever histórico já compartilhado.
- Segredo commitado por engano: rotacionar (ver `logging-config-segredos`); apagar o commit
  não basta.

## Lint e formatação como gate

**Gate** = a T não conclui enquanto não estiver limpa. Automatize para a máquina discutir
estilo, não as pessoas.

| Camada | O que faz | Ferramentas open source (conferir na versão usada) |
|---|---|---|
| Formatador | Layout único, sem debate | Spotless/google-java-format ou similar (Java), `gofmt`/`goimports` (Go), `rustfmt` (Rust), `ruff format`/`black` (Python), Prettier (TS/Vue) |
| Linter | Erros prováveis, estilo, complexidade | Checkstyle/SpotBugs/Error Prone (Java), `go vet` + `staticcheck`/`golangci-lint` (Go), `clippy` (Rust), `ruff` (Python), ESLint (TS/Vue) |
| Tipos | Contratos e nulidade | compilador (Java/Go/Rust), `mypy`/`pyright` strict (Python), `tsc`/`vue-tsc --noEmit` strict (TS/Vue) |
| Segredos | Credenciais no código | gitleaks, trufflehog |
| Dependências | Vulnerabilidades conhecidas | OWASP dependency-check, `govulncheck`, `cargo audit`, `pip-audit`, `npm audit` (ver skill de segurança) |

Regras:

- **Um comando** documentado no `app/README.md` que roda formatação (verificação), lint e
  tipos (`make lint` ou o script do projeto). O QA e o CI usam o mesmo.
- O que o linter acusa, **corrija**. Exceção pontual (`// nolint`, `@SuppressWarnings`,
  `# noqa`) só com **comentário do motivo** e o menor escopo possível.
- **Não desligue regra** global para a T passar. Regra ruim é discussão para a Arquitetura
  (ACH), não atalho.
- Avisos (warnings) de compilador contam: aviso novo = corrigir.
- Formatador roda **antes** do commit para o diff não carregar ruído.
- Complexidade: se o linter acusa função complexa, é pista de extrair, não de aumentar o
  limite.

## Autorrevisão do diff

Antes de declarar a T concluída, **leia o próprio diff completo** como revisor:

1. `git diff` (ou o diff staged) do começo ao fim, arquivo por arquivo.
2. Para cada trecho pergunte: isto está no escopo? Tem teste? O nome é claro? Pode falhar
   de forma silenciosa? Há dado sensível?
3. Procure ativamente:

| Sinal no diff | Pergunta |
|---|---|
| Arquivo fora de `escopo_arquivos` | Necessário? Se sim, desvio em `registro_dev.desvios`; se não, reverter |
| Código sem teste associado | Qual comportamento cobre? |
| `catch` vazio, `unwrap`, `!`, `as any`, `# type: ignore` | Por quê? Há alternativa? |
| `TODO`, `FIXME`, `console.log`, `print`, código comentado | Remover ou virar ACH |
| String/valor fixo de ambiente (URL, porta, senha) | Deveria ser configuração |
| SQL concatenado com entrada | Parametrizar |
| Mudança de contrato (rota, campo, tipo) | O `openapi.yaml` manda: ACH para Arquitetura |
| Mudança em arquivo gerado | Regerar, não editar à mão |
| Dependência nova | Necessária? Licença e manutenção ok? |
| Diff grande sem relação com a T | Separar ou remover |

4. Rodar do zero, em ambiente limpo: build, testes, lint e tipos. "Funciona na minha
   máquina" não conta; o comando do README precisa passar.
5. Preencher o `registro_dev` da T (`commits`, `testes`, `desvios`, incluindo o que ficou de
   dívida) com honestidade e marcar `situacao`; `justificada` com `justificativa` em vez de
   entrega parcial escondida.

### Relatório final do Dev (resumo)

Tarefas concluídas, commits, testes criados, desvios e dúvidas. Dúvida aberta é melhor que
decisão silenciosa.

## Erros comuns

| Erro | Efeito | Correção |
|---|---|---|
| Mensagem "ajustes", "wip", "fix" | Histórico inútil | `T-NN: <título>`; `fix(ACH-NN): <resumo>` |
| Commit com `.env` ou segredo | Vazamento permanente | `.gitignore`, varredura, rotação |
| Pular o lint "porque é só estilo" | Dívida e ruído nos próximos diffs | Gate obrigatório |
| `--no-verify` | Hook ignorado, CI quebra | Corrigir a causa |
| Desabilitar regra do linter globalmente | Gate vazio | Exceção pontual com motivo; ACH se a regra é ruim |
| Autorrevisão só olhando o arquivo aberto | Mudança esquecida em outro arquivo | Ler o diff inteiro |
| Concluir com teste vermelho "flaky" | Falha escondida | Estabilizar antes de concluir |
| `registro_dev` vazio | QA e aprovador sem pista | Preencher `commits`, `testes`, `desvios` |
