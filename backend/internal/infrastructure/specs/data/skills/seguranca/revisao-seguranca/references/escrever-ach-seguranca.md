# Como escrever um ACH de segurança

Regra do projeto: **achado sem evidência é descartado** (PROJETO.md, seção 6.2) e **não se
abre achado por suspeita**. O ACH de segurança é lido pelo Dev (que precisa reproduzir e
corrigir em minutos) e pelo Orquestrador/aprovador humano (que decide a onda de retrabalho). Template:
`agentes/templates/ACH.md`; esquema `specs/dominio/artefatos/ach.schema.json`.

## 1. Anatomia, campo a campo

Cabeçalho YAML validado por `specs/dominio/artefatos/ach.schema.json`; contrato e regras em
`specs/dominio/achado.md`. **Vale o esquema.** Arquivo `docs/achados/ACH-NN.md`.

| Campo | Como preencher (segurança) |
|---|---|
| `id` | `ACH-NN` sequencial no projeto |
| `titulo` | **O que acontece**, em uma frase, com o ator: "Funcionário consegue cancelar reserva de outro". Não: "IDOR no endpoint de reservas" (jargão sem consequência) nem "Falha de segurança" |
| `autor` | `security-agent` (quem emite é quem revalida) |
| `onda` | `SEG-<nn>` |
| `estado` | `rascunho` (ciclo do artefato); a situação do achado é `situacao: aberto` |
| `severidade` | `critico \| alto \| medio \| baixo`, pela tabela do ACH (seção 3) |
| `categoria` | `vulnerabilidade` (falha explorável no código ou na configuração). Falha de desenho: `desvio_arquitetura`; RNF faltando ou ambíguo: `requisito_ambiguo` |
| `etapa_destino` | `implementacao` (bug no código), `arquitetura` (falha de desenho/ADR), `requisitos` (RNF faltando), `dados` (modelagem) |
| `artefatos_afetados` | ao menos 1: `caminho:linha` (código), id de ADR/RNF/T/CT, ou item como `US-05/CA-3` |
| `evidencia` | seção 2 (obrigatória, não vazia) |
| `impacto` | o que um atacante ganha, em linguagem de negócio, sem exagero |
| `correcao_sugerida` | direção sem impor implementação; cita a prática (checar dono, parametrizar consulta) |
| `situacao` e `ciclos` | `aberto` e `[]` na abertura |

## 2. Evidência mínima convincente

Receita: **o menor conjunto que permite a outra pessoa reproduzir em 2 minutos e ver o
problema com os próprios olhos.**

1. **Pré-condição** (estado e usuários do seed local).
2. **Ação** reproduzível: o comando (`curl`) ou os passos exatos; sem segredos reais.
3. **Resultado obtido x esperado:** status, trecho do corpo.
4. **Prova de efeito:** estado final (a reserva sumiu; o dado de A veio na resposta de B).
5. **Causa localizada** (quando possível): `arquivo:linha` com o trecho mínimo.
6. **Ambiente e versão:** "Compose local do projeto, commit `abc123`".

| Tipo de achado | Evidência mínima |
|---|---|
| IDOR/BOLA | Requisição de B para recurso de A + resposta + estado final de A + trecho do código sem checagem |
| Injeção | Requisição com o payload + resposta que prova a execução (erro SQL, dado a mais, diferença de tempo) + trecho de código com a concatenação |
| XSS | Payload, local onde é exibido, prova de execução (print/DOM) ou, em teste automatizado, o alerta/efeito |
| Autenticação | Sequência de requisições (ex.: 30 tentativas, status), ou token adulterado aceito |
| Config/cabeçalho | `curl -i` mostrando o cabeçalho ausente/errado |
| Dependência (Trivy) | Pacote, versão instalada, versão corrigida, CVE, **e** prova de que o recurso vulnerável é usado (ou justificativa) |
| Segredo (Gitleaks) | Arquivo:linha, tipo, commit — **sem o valor**; indicar que precisa rotação |
| SAST (Opengrep) | Regra, arquivo:linha, trecho + **a prova de alcançabilidade** (requisição que chega ao ponto) |
| Falha de desenho | Referência ao ADR/fluxo + cenário de ataque passo a passo + o que falta |

**Nunca:** saída inteira da ferramenta; segredo real; credencial de terceiros; dados pessoais
reais; ataque fora do ambiente local; "o código parece vulnerável" sem demonstração.

## 3. Severidade

Tabela do ACH (a mesma de todos os agentes):

| Severidade | Quando |
|---|---|
| Crítico | Perda ou vazamento de dados, acesso indevido, sistema não sobe |
| Alto | Requisito Must não atendido, falha de segurança explorável com esforço |
| Médio | Requisito Should não atendido, comportamento errado com contorno |
| Baixo | Cosmético, melhoria, dívida técnica sem efeito visível |

Como decidir para segurança — pergunte, nesta ordem:

1. **Há vazamento de dado de outros usuários, acesso indevido ou perda de dados provados?**
   -> Crítico.
2. **É explorável por um atacante realista (usuário autenticado comum ou anônimo) com
   esforço razoável, afetando funcionalidade ou dado?** -> Alto.
3. **Exige condição adicional (privilégio, interação, config específica) ou o impacto é
   limitado a quem faz?** -> Médio.
4. **É endurecimento, defesa em profundidade ou informação sem impacto direto?** -> Baixo.

Ajustes (registre o raciocínio em uma linha):
- Dado pessoal sensível, dinheiro, irreversível: **sobe** um nível.
- Só alcançável com acesso físico/admin já comprometido: **desce**.
- Controle compensatório comprovado: **desce**, citando o controle.
- Mesma causa em N lugares: **um ACH**, severidade pela pior instância, lista dos lugares.

Correspondência com a matriz de ameaças: severidade do ACH = severidade da ameaça (`modelagem-ameacas-stride.md`).

## 4. Destino

| Destino | Quando |
|---|---|
| **Desenvolvimento** (`etapa_destino: implementacao`) | O desenho está certo e o código não o cumpre (checagem faltando, consulta concatenada, segredo no repo) |
| **Arquitetura** (`arquitetura`) | A ameaça não tem mitigação no desenho, ou o ADR/contrato produz a falha (ex.: sem controle de taxa previsto, JWT sem revogação onde o requisito exige) |
| **Requisitos** (`requisitos`) | Falta RNF de segurança ou há requisito ambíguo (política de senha, retenção de logs) |

Dúvida entre Desenvolvimento e Arquitetura: se corrigir exige **mudar uma decisão aprovada**,
é Arquitetura (reabre o gate daquela etapa).

## 5. Exemplo completo (bom)

```yaml
---
id: ACH-04
tipo: ACH
titulo: Funcionário consegue cancelar reserva de outro
autor: security-agent
onda: SEG-01
versao: 1
estado: rascunho
vinculos: []
historico:
  - {versao: 1, data: "2026-11-19", onda: SEG-01, descricao: criação}
severidade: alto
categoria: vulnerabilidade
etapa_destino: implementacao
artefatos_afetados:
  - US-05/CA-3
  - CT-24
  - backend/src/main/java/ReservaService.java:58
evidencia: |
  Ambiente: Compose local, commit a1b2c3d. Usuários do seed: funcionario (A), funcionario2 (B).
  1. A cria a reserva: POST /reservas (token A) -> 201, id=42.
  2. B cancela: curl -i -X DELETE "$API_URL/reservas/42" -H "Authorization: Bearer $TOKEN_B"
     -> HTTP/1.1 204 (esperado 403/404).
  3. A consulta: GET /reservas/42 (token A) -> 404 (a reserva foi apagada).
  Causa: ReservaService.cancelar (linha 58) chama repo.deleteById(id) sem comparar
  reserva.donoId com o usuário autenticado.
impacto: Qualquer funcionário autenticado apaga as reservas de todos (basta o id, sequencial); perda de dados e interrupção do serviço. OWASP:2025 A01.
correcao_sugerida: "Antes de cancelar, permitir apenas ao dono ou admin; responder 403 (ou 404, conforme a ADR) aos demais; cobrir com CT-24 e revisar os outros endpoints por id."
situacao: aberto
ciclos: []
---
```

Corpo Markdown (livre): passos de reprodução detalhados; o YAML guarda o resumo.

## 6. Exemplos ruins e o porquê

**Ruim 1 (sem evidência):**
```
Título: Possível SQL injection
Evidência: o código parece concatenar a entrada na query.
```
Suspeita; não prova alcance; o gate descarta.

**Ruim 2 (despejo de ferramenta):**
```
Evidência: <200 linhas de JSON do Trivy>
```
O Dev não sabe o que importa; falta alcançabilidade e correção.

**Ruim 3 (severidade sem lógica):**
```
Severidade: Crítico (porque é segurança)
```
Severidade vem de probabilidade x impacto e da tabela do ACH.

**Ruim 4 (vazando segredo):**
```
Evidência: chave encontrada: AKIA.... (valor completo)
```
O próprio ACH vira vazamento. Mostrar arquivo:linha e tipo; recomendar rotação.

**Ruim 5 (várias causas em um):**
Um ACH com IDOR, CORS e hash fraco. Fica impossível revalidar e fechar. **Um problema real = um ACH.**

## 7. Ciclo de vida e revalidação

- Estado inicial `aberto`; o Dev corrige (`em-retrabalho`); você revalida (`revalidando`) e fecha
  (`fechado`) ou reabre com nova evidência na tabela de ciclos.
- **Revalidação:** repetir **exatamente** a reprodução da evidência e a varredura ligada; passar
  a prova de efeito (o estado final) é o critério.
- Limite de 3 ciclos; estourou: `escalado` para o aprovador humano.
- `aceito`: risco assumido **explicitamente** pelo aprovador humano (registrar quem, quando, motivo e prazo
  de reavaliação). Só o aprovador humano aceita Crítico/Alto.
- Quem emitiu revalida.
- Correção que muda algo aprovado (ADR, contrato) reabre o gate daquela etapa.

## 8. Checklist antes de salvar o ACH

- [ ] Título diz o que acontece, não o nome da categoria.
- [ ] Reproduzível em 2 minutos por quem não viu nada antes.
- [ ] Mostra efeito (estado final / dado vazado), não só o status.
- [ ] Causa localizada em `arquivo:linha`, quando houver código.
- [ ] Severidade justificada (e ajustes explicados).
- [ ] Destino coerente.
- [ ] Sem segredo/dado pessoal real; ataque só no ambiente local.
- [ ] Uma causa por ACH; instâncias listadas.
- [ ] Categoria OWASP citada no Impacto.
- [ ] Cabeçalho YAML completo, `onda` `SEG-<nn>`.
