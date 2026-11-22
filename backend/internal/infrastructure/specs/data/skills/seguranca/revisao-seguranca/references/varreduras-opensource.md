# Varreduras open source: Opengrep, Trivy, Gitleaks

Três ferramentas, três perguntas diferentes. Nenhuma substitui a revisão manual; elas geram
**candidatos** que você tria. Todas rodam localmente, sobre `app/`, sem enviar código a terceiros.

| Ferramenta | Pergunta que responde | Cobre | Não cobre |
|---|---|---|---|
| **Opengrep** (SAST) | O **código que escrevemos** tem padrão inseguro? | Injeção, cripto fraca, desserialização, uso perigoso de API, padrões de config no código | Lógica de negócio (IDOR sem padrão sintático), arquitetura, runtime |
| **Trivy** | As **dependências, imagem e configuração** têm problema conhecido? | CVE em bibliotecas e pacotes do SO, misconfig de Dockerfile/Compose/IaC, segredos em arquivos | Código próprio, lógica |
| **Gitleaks** | Há **segredo** (chave, token, senha) no código **ou no histórico**? | Padrões de credenciais em arquivos e commits | Segredo sem padrão reconhecível, vulnerabilidade |

Os comandos exatos abaixo são os de uso corrente; **flags e nomes mudam entre versões:
confirmar com `<ferramenta> --help` e `--version`** (versões efetivas em `agentes/ambiente.md`) e registrar versão
no REL-SEG (coluna "Versão").

## 1. Opengrep (SAST)

**O que é:** fork aberto do Semgrep, mantido pela comunidade; lê código por **padrões
sintáticos** definidos em regras YAML. Linguagens: as mais comuns de web (JS/TS, Python, Java,
Go, C#, PHP, Ruby etc.).

**Regras: licença importa.** Use apenas regras com **licença aberta**. Não use os pacotes do
registro comercial do Semgrep, cujas regras têm licença que restringe o uso. Opções:
1. repositório de regras mantido pelo projeto Opengrep (`opengrep-rules`; **conferir a
   licença e o conteúdo** antes de adotar);
2. regras próprias (YAML) escritas para os riscos do projeto;
3. outros conjuntos de regras com licença aberta verificada.
Registre no REL-SEG de onde vêm as regras e a licença conferida.

**Como rodar**

```bash
# a partir de app/ ; --config aceita pasta ou arquivo de regras local
opengrep scan --config ./regras-abertas/ .
# saída máquina-legível (para triagem e anexo): conferir flags em `agentes/ambiente.md`
opengrep scan --config ./regras-abertas/ --json -o opengrep.json .
opengrep scan --config ./regras-abertas/ --sarif -o opengrep.sarif .
```

Boas práticas: excluir `node_modules`, `vendor`, `dist`, testes e código gerado com
`--exclude`/arquivo de ignore; fixar a **versão** da ferramenta e das **regras** (para quem
for reproduzir); rodar a partir da raiz de `app/`.

**Como ler um resultado:** `check_id` (regra), arquivo:linha, mensagem, severidade da regra,
trecho. Pergunte: (1) o dado vem de **fonte controlada pelo usuário**? (2) chega ao
**sorvedouro** sem sanitização? (3) há controle no caminho que a regra não vê?

**Exemplo de regra própria** (padrão estável da sintaxe; testar com `opengrep scan --test`
conforme a versão): SQL por concatenação em Python.

```yaml
rules:
  - id: sql-concatenado-python
    languages: [python]
    severity: ERROR
    message: Consulta SQL montada com concatenação/f-string; use parâmetros.
    patterns:
      - pattern-either:
          - pattern: $CUR.execute(f"...")
          - pattern: $CUR.execute("..." + $X)
          - pattern: $CUR.execute("..." % $X)
    metadata: { owasp: "A05" }
```

**Falso positivo típico:** regra de injeção SQL em consulta cuja parte concatenada é uma
**constante** (nome de tabela fixo); regra de "segredo hardcoded" em string de teste; padrão
em código morto/teste.

## 2. Trivy (dependências, imagem, configuração, segredos)

**Modos usados em aplicação pequena**

```bash
# Repositório/dependências + segredos + configuração (Dockerfile, Compose, IaC)
trivy fs --scanners vuln,secret,misconfig .

# Só altas e críticas, para o resumo; exit code diferente de 0 se achar (para CI)
trivy fs --scanners vuln,secret,misconfig --severity HIGH,CRITICAL --exit-code 1 .

# Imagem construída (pacotes do SO + dependências da aplicação na imagem)
trivy image <imagem:tag>
trivy image --severity HIGH,CRITICAL --ignore-unfixed <imagem:tag>

# Somente arquivos de configuração (Dockerfile, Compose, Kubernetes, Terraform)
trivy config .

# Saída para anexo
trivy fs --scanners vuln,secret,misconfig --format json -o trivy.json .
```

- `--ignore-unfixed` oculta vulnerabilidades **sem correção disponível** (útil para priorizar o
  que dá para atualizar; registre que foi usado).
- Na primeira execução baixa a **base de vulnerabilidades** (precisa de rede); sem rede,
  usar a base já baixada (flags `--skip-db-update`/`--offline-scan`; **conferir em `agentes/ambiente.md`**).
  Registre a data da base no REL-SEG, pois o resultado muda com o tempo.
- Lockfile (`package-lock.json`, `poetry.lock`, `pom.xml` resolvido, `go.sum`) deve estar
  presente: sem ele, o Trivy não sabe as versões exatas.

**Como ler:** por pacote: `VulnerabilityID` (CVE/GHSA), `PkgName`, `InstalledVersion`,
`FixedVersion`, `Severity`, título/links. Para misconfig: `ID` (ex.: AVD-DS-xxxx), mensagem,
arquivo:linha, resolução sugerida.

**Triagem de vulnerabilidade de dependência**

| Pergunta | Se sim | Se não |
|---|---|---|
| Há versão corrigida (`FixedVersion`)? | Recomendar atualizar (mesmo se pouco explorável) | Registrar; avaliar mitigação/ risco aceito (aprovador) |
| A função/recurso vulnerável é **usada** pelo app? | Prioridade alta | Pode ser falso positivo prático; justificar por escrito |
| A dependência é **só de desenvolvimento/teste** e não vai na imagem final? | Severidade menor; registrar | Seguir normal |
| A condição de exploração existe aqui (ex.: precisa de entrada do usuário no parser)? | Alto/Crítico se o impacto é alto | Reduzir/justificar |
| Está na **imagem final** de produção? | Contar | Se só em estágio de build, descartar com justificativa |

Severidade do ACH **não é a do CVSS**: combine CVSS com alcançabilidade e impacto no contexto.

**Falso positivo típico:** CVE de pacote do SO em estágio de build descartado na imagem final;
CVE em biblioteca cuja função vulnerável não é chamada; arquivo de exemplo/documentação com
"segredo" de mentira; misconfig em Dockerfile de desenvolvimento documentado como tal.

**Ignorar com rastro:** `.trivyignore` com o id e **comentário com a justificativa e a data**
(ou arquivo de política); nunca ignorar sem texto.

## 3. Gitleaks (segredos)

```bash
# Histórico do git (todos os commits) — pega o segredo que já foi apagado do arquivo
gitleaks git .
# Arquivos atuais (sem histórico)
gitleaks dir .
# Relatório e redação do valor no relatório
gitleaks git . --report-format json --report-path gitleaks.json --redact
```

Notas:
- Os subcomandos `git` e `dir` são os de versões recentes (8.19+); versões antigas usam
  `gitleaks detect`/`protect`. **Conferir com `gitleaks --help`** na versão instalada.
- Rode **ambos**: `git` encontra segredos antigos; `dir` os que ainda não foram commitados.
- `--redact` evita copiar o segredo para o relatório/ACH (importante!).
- Se `app/` não é um repositório git, `gitleaks git` não se aplica; use `dir` e registre.

**Como ler:** `RuleID`, arquivo:linha, commit, autor, data, entropia, trecho (redigido).

**Triagem:**
| Pergunta | Ação |
|---|---|
| É credencial real **em uso** (aceita pelo serviço)? | Achado; **rotacionar** o segredo (remover do git não basta: está no histórico) |
| É valor de exemplo/teste/placeholder (`changeme`, `example`)? | Falso positivo com justificativa; ideal usar placeholder óbvio |
| É segredo de **ambiente local descartável** (senha do Postgres do Compose)? | Médio: padrão fraco no repositório; exigir variável de ambiente e sem reuso em produção |
| É chave pública/identificador não secreto? | Falso positivo |

**Ignorar com rastro:** `.gitleaksignore` (por impressão digital do achado) ou allowlist em
`.gitleaks.toml`, sempre com comentário do motivo.

**Segredo real encontrado:** severidade em geral **Alto/Crítico** (acesso indevido possível);
recomendar **rotação imediata + remoção + prevenção** (variável de ambiente/cofre, `.gitignore`,
hook de pré-commit com Gitleaks). A evidência mostra **apenas o ponto de vista** (arquivo, linha,
tipo, commit), **nunca o valor**.

## 4. Tabela de varreduras do REL-SEG: como preencher

No YAML do REL-SEG (`rel-seg.schema.json`), cada linha vira um item de `varreduras` (ao menos 3:
`ferramenta: opengrep | trivy | gitleaks | outra`, `versao`, `comando`, `achados` com ids de ACH e
`falsos_positivos` como número); cada descarte vira item de `falsos_positivos` (`ferramenta`, `regra`,
`local`, `justificativa`). Exemplo completo na skill (`SKILL.md`, seção "Formato do artefato REL-SEG").
A tabela abaixo é a forma legível do corpo:


| Ferramenta | Versão | Comando | Achados | Falsos positivos descartados |
|---|---|---|---|---|
| Opengrep (SAST) | `opengrep --version` | `opengrep scan --config ./regras-abertas/ .` | 7 resultados -> 2 reais (ACH-06, ACH-07) | 5 (ver seção "Falsos positivos") |
| Trivy | `trivy --version` + data da base | `trivy fs --scanners vuln,secret,misconfig .` e `trivy image app-api:ci` | 3 vulns HIGH com correção -> ACH-08 | 2 |
| Gitleaks | `gitleaks version` | `gitleaks git .` e `gitleaks dir .` | 1 segredo real -> ACH-09 | 1 |

## 5. Falsos positivos: registro e critério

Para **cada** descarte, uma linha na seção "Falsos positivos descartados" do REL-SEG:

```markdown
- Opengrep `sql-concatenado-python`, `app/db/relatorio.py:41`: a parte concatenada é a constante
  `TABELA_RELATORIO`, definida no módulo e nunca alterada por entrada externa; sem caminho de
  dado controlado pelo usuário. Falso positivo.
- Trivy CVE-XXXX-NNNN, `libexemplo 1.2.3`: presente apenas no estágio `build` do Dockerfile;
  a imagem final (`FROM ... AS runtime`) não contém o pacote (`trivy image` confirma).
- Gitleaks `generic-api-key`, `app/docs/exemplo.md:12`: valor `SUA_CHAVE_AQUI`, placeholder
  documentado.
```

Critério de descarte aceitável: (a) **não alcançável** (código morto/teste/estágio de build)
com prova; (b) **entrada não controlada** (constante); (c) **mitigado por controle** que a
ferramenta não vê (e você cita onde está); (d) **placeholder** sem valor. "Parece ok" não é
justificativa.

## 6. Cobertura honesta

No REL-SEG, deixe claro **o que as ferramentas não cobrem**: lógica de negócio, IDOR sem padrão
sintático, falhas de desenho, configuração de runtime, comportamento sob carga. Isso é feito na
revisão manual (`owasp-top10-2025.md`, `controle-acesso.md`).

## 7. Reprodutibilidade

- Fixe versão da ferramenta e das regras; registre data da base do Trivy.
- Comandos copiáveis, partindo de `app/`, sem caminhos da sua máquina.
- Guarde saídas (`opengrep.json`, `trivy.json`, `gitleaks.json`) em `docs/seguranca/evidencias/` —
  **gitleaks sem valores** (redigidos).
- Instalação das ferramentas: seguir a documentação oficial de cada projeto; a forma exata
  (binário, contêiner) fica em `agentes/ambiente.md`. Executar em contêiner oficial é aceitável
  (`docker run --rm -v "$PWD:/src" ... <imagem>`; **conferir imagem e flags em `agentes/ambiente.md`**). Isso vale para CI/humano; no sandbox da plataforma o agente não tem Docker e usa os binários da allowlist da categoria `scanner`.

## 8. Anti-padrões

- Colar a saída inteira no ACH em vez da evidência mínima.
- Descartar falso positivo sem escrever o motivo.
- Rodar Gitleaks só nos arquivos atuais (segredo antigo no histórico passa).
- Usar regras de licença não aberta ou sem conferir a licença.
- Tratar CVSS como severidade do ACH sem considerar alcançabilidade.
- Incluir o valor do segredo na evidência.
- Rodar sem lockfile e concluir "sem vulnerabilidades".
