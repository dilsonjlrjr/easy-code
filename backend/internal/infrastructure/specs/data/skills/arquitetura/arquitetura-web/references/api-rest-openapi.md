# Design de API REST e contrato OpenAPI 3

Entrega: `docs/arquitetura/api/openapi.yaml` (válido). É o contrato entre Dev backend, Dev front e QA.
Escreva-o **antes** do código; handlers e clientes derivam dele.

## Princípios

1. **Recurso, não ação.** Substantivo no plural; o método HTTP é o verbo.
2. **Previsível.** Mesma convenção em todos os endpoints (nomes, paginação, erros).
3. **Erros são parte do contrato.** Todo endpoint documenta os 4xx que pode devolver.
4. **Simples primeiro.** Sem HATEOAS, sem GraphQL, sem versionamento em URL em aplicação pequena.
5. **Contrato testável.** Todo exemplo no YAML deve ser aceito pelo esquema.

## Recursos e métodos

| Operação | Método e rota | Sucesso |
|---|---|---|
| Listar | `GET /reservas` | 200 |
| Criar | `POST /reservas` | 201 + `Location` |
| Obter | `GET /reservas/{id}` | 200 |
| Substituir | `PUT /reservas/{id}` | 200 |
| Alterar parcial | `PATCH /reservas/{id}` | 200 |
| Excluir | `DELETE /reservas/{id}` | 204 |
| Subcoleção | `GET /salas/{id}/reservas` | 200 |

Cancelar sem apagar histórico: duas opções válidas. (a) `DELETE` quando "cancelar" realmente
remove; (b) `POST /reservas/{id}/cancelamento` ou `PATCH` do `status` quando precisa manter
histórico. Escolha uma regra e use em todos os recursos. Evite `POST /cancelarReserva`.

Rotas: plural, minúsculas, `kebab-case` para multipalavra (`/tipos-de-sala`), no máximo 2
níveis de aninhamento. Campos JSON em `camelCase` ou `snake_case`, um só no projeto
(decidir na ADR de erros/contrato).

## Status HTTP

| Status | Uso |
|---|---|
| 200 / 201 / 204 | Sucesso com corpo / criado / sucesso sem corpo |
| 400 | Requisição malformada ou validação de formato |
| 401 | Sem autenticação ou credencial inválida |
| 403 | Autenticado sem permissão |
| 404 | Recurso inexistente (também para recurso de outro dono, se não revelar existência) |
| 409 | Conflito com estado atual (sobreposição, duplicidade) |
| 422 | Corpo válido sintaticamente mas semanticamente inválido (opcional; ver regra) |
| 429 | Limite de taxa |
| 500 | Falha inesperada; nunca detalhar interna ao cliente |

Regra de 400 vs 422: escolha **um** critério. Recomendado simples: 400 para qualquer
validação de entrada, 409 para conflito de regra/estado. Registre na ADR de erros.

## Problem Details (RFC 9457)

Media type `application/problem+json`. Campos padrão: `type`, `title`, `status`, `detail`,
`instance`; extensões livres (ex.: `errors`, `traceId`). Não vazar stack trace nem SQL.

```json
{
  "type": "https://exemplo.local/problemas/conflito-de-horario",
  "title": "Conflito de horário",
  "status": 409,
  "detail": "A sala 3 já está reservada entre 10:00 e 11:00.",
  "instance": "/reservas",
  "traceId": "9f2c..."
}
```

Erros de validação com extensão `errors`:

```json
{
  "type": "https://exemplo.local/problemas/validacao",
  "title": "Dados inválidos",
  "status": 400,
  "detail": "Um ou mais campos são inválidos.",
  "errors": [{ "campo": "fim", "mensagem": "deve ser posterior ao início" }]
}
```

O `type` é um identificador estável (URI não precisa resolver). Cada `type` usado entra no
contrato e na tabela de erros da ADR. Mensagens para humanos em `detail`; lógica do front
usa `type`, nunca o texto.

## Paginação, ordenação e filtros

| Estratégia | Quando | Parâmetros |
|---|---|---|
| Offset | Listas administrativas pequenas, saltar para página N | `?page=1&pageSize=20` |
| Cursor (keyset) | Listas grandes ou que mudam durante a leitura | `?limit=20&cursor=<opaco>` |

Resposta paginada:

```json
{ "itens": [ ], "pagina": 1, "tamanhoPagina": 20, "total": 134 }
```

ou, com cursor, `{ "itens": [ ], "proximoCursor": "..." }`. Limite máximo de `pageSize`
(ex.: 100) documentado e imposto. Detalhes de SQL em `modelagem-postgresql`.
Ordenação: `?ordem=inicio` crescente, `?ordem=-inicio` decrescente; liste os campos aceitos.
Filtros: parâmetros nomeados e tipados (`?salaId=3&de=2026-11-20T00:00:00-03:00&ate=...`);
nunca expressão livre.

## Idempotência

| Método | Idempotente? | Observação |
|---|---|---|
| GET, PUT, DELETE | Sim | `DELETE` repetido pode devolver 204 ou 404; documente |
| POST | Não | Repetição cria duplicata |
| PATCH | Depende | Defina |

Quando o cliente pode repetir um `POST` (rede instável, duplo clique) e duplicar for grave,
aceite o cabeçalho `Idempotency-Key`: guarde chave+resposta e devolva a mesma resposta na
repetição. Para aplicação pequena, a constraint do banco (ex.: sobreposição) geralmente basta; use
`Idempotency-Key` só se uma US exigir. Desabilitar o botão após o clique é complemento no
front, não substituto.

## Versionamento

Sem cliente externo, um único contrato e releases conjuntos de front e API: **sem versão em
URL**. Quando surgir cliente fora do seu controle: prefixo `/v1` e política de depreciação.
Mudança compatível (campo novo opcional) não exige versão; remover ou mudar tipo exige.
Decisão registrada em ADR quando mudar.

## Datas, números e identificadores

- Datas-hora ISO 8601 com fuso (`2026-11-20T10:00:00-03:00`) ou UTC (`Z`); banco em `timestamptz`.
- Datas sem hora: `date` (`2026-11-20`). Duração: ISO 8601 (`PT30M`) ou inteiro em unidade nomeada.
- Dinheiro: inteiro em centavos ou string decimal; nunca `float`.
- IDs: inteiros ou UUID conforme ADR; sempre string no JSON se UUID. Não exponha ID sequencial
  onde enumeração seja problema.
- `enum` no contrato = lista fechada; adicionar valor é mudança que o front precisa tratar.

## Esqueleto OpenAPI 3

```yaml
openapi: 3.0.3          # 3.1 se as ferramentas do projeto suportarem; conferir
info:
  title: Reserva de Salas
  version: 1.0.0
servers:
  - url: http://localhost:8080
tags: [{ name: reservas }]
paths:
  /reservas:
    post:
      operationId: criarReserva
      tags: [reservas]
      security: [{ sessaoCookie: [] }]
      requestBody:
        required: true
        content:
          application/json:
            schema: { $ref: '#/components/schemas/NovaReserva' }
            example: { salaId: 3, inicio: '2026-11-20T10:00:00-03:00', fim: '2026-11-20T11:00:00-03:00' }
      responses:
        '201':
          description: Reserva criada
          headers:
            Location: { schema: { type: string }, example: /reservas/42 }
          content:
            application/json:
              schema: { $ref: '#/components/schemas/Reserva' }
        '400': { $ref: '#/components/responses/Validacao' }
        '401': { $ref: '#/components/responses/NaoAutenticado' }
        '409': { $ref: '#/components/responses/Conflito' }
components:
  securitySchemes:
    sessaoCookie: { type: apiKey, in: cookie, name: SESSAO }
  schemas:
    NovaReserva:
      type: object
      required: [salaId, inicio, fim]
      properties:
        salaId: { type: integer, minimum: 1 }
        inicio: { type: string, format: date-time }
        fim: { type: string, format: date-time }
    Reserva:
      allOf:
        - $ref: '#/components/schemas/NovaReserva'
        - type: object
          required: [id, usuarioId]
          properties:
            id: { type: integer }
            usuarioId: { type: integer }
    Problema:
      type: object
      required: [type, title, status]
      properties:
        type: { type: string }
        title: { type: string }
        status: { type: integer }
        detail: { type: string }
        instance: { type: string }
  responses:
    Validacao:
      description: Dados inválidos
      content:
        application/problem+json: { schema: { $ref: '#/components/schemas/Problema' } }
    NaoAutenticado:
      description: Sem autenticação
      content:
        application/problem+json: { schema: { $ref: '#/components/schemas/Problema' } }
    Conflito:
      description: Conflito de horário
      content:
        application/problem+json: { schema: { $ref: '#/components/schemas/Problema' } }
```

Reutilize respostas de erro em `components/responses`; cada operação referencia só as que pode produzir.

## Validar o contrato

Use ferramentas open source de lint e validação de OpenAPI (por exemplo Spectral para regras e
um validador de esquema como o do `openapi-generator` ou `swagger-parser`; **conferir** o
que o projeto instala). Valide também que exemplos obedecem ao esquema. Em CI, falhar em
contrato inválido.

## Checklist

- [ ] Todo endpoint com `operationId` único e `tags`.
- [ ] Todo sucesso e todo 4xx relevante documentados, com `application/problem+json`.
- [ ] Todo corpo com `required` e restrições (`minLength`, `minimum`, `format`).
- [ ] Segurança declarada por operação; públicas explícitas (`security: []`).
- [ ] Paginação e ordenação padronizadas; limite máximo documentado.
- [ ] Exemplos válidos em requisições e respostas principais.
- [ ] Cada regra de negócio de US aparece como status/erro rastreável.

## Erros comuns

| Erro | Correção |
|---|---|
| Verbo na rota | Recurso + método |
| 200 com `{"erro": ...}` | Status HTTP correto + Problem Details |
| Lista sem paginação | Paginar desde o início, com limite |
| `PUT` parcial | `PATCH` ou enviar o recurso inteiro |
| Datas sem fuso | ISO 8601 com offset ou `Z` |
| Dinheiro em float | Inteiro em centavos |
| Contrato diferente do implementado | Teste de contrato no CI |
| 404 vs 403 inconsistentes | Regra única na ADR (ver `autenticacao-autorizacao.md`) |
