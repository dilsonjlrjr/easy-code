# Cliente HTTP único e tipos a partir do OpenAPI

## Um cliente só

Toda chamada ao backend passa por **um módulo** (`api/cliente.ts`). Componente e store nunca
chamam `fetch` direto. Isso concentra: base URL, credenciais, 401, conversão de erro,
cancelamento, tempo limite e cabeçalhos.

`fetch` nativo basta; uma biblioteca (axios, ofetch, ky) só se a Arquitetura decidir por ADR.

```ts
// api/cliente.ts
import { config } from '@/config'
import { ErroApi, lerProblem } from './erros'

type Opcoes = Omit<RequestInit, 'body'> & { body?: unknown; timeoutMs?: number }

let aoNaoAutenticado: () => void = () => {}
export function registrarAoNaoAutenticado(fn: () => void) { aoNaoAutenticado = fn }

export async function requisitar<T>(caminho: string, op: Opcoes = {}): Promise<T> {
  const { body, timeoutMs = 15000, signal, headers, ...resto } = op
  const timeout = AbortSignal.timeout(timeoutMs)             // conferir suporte do alvo
  const resposta = await fetch(`${config.apiUrl}${caminho}`, {
    ...resto,
    credentials: 'include',                                  // cookie HttpOnly da sessão
    headers: {
      Accept: 'application/json',
      ...(body !== undefined ? { 'Content-Type': 'application/json' } : {}),
      ...headers,
    },
    body: body !== undefined ? JSON.stringify(body) : undefined,
    signal: signal ? AbortSignal.any([signal, timeout]) : timeout,   // conferir suporte
  })

  if (resposta.status === 401) { aoNaoAutenticado(); throw await lerProblem(resposta) }
  if (!resposta.ok) throw await lerProblem(resposta)
  if (resposta.status === 204) return undefined as T
  return (await resposta.json()) as T
}
```

Pontos:

- **Base URL** por configuração (`VITE_API_URL` em build, ou arquivo de config em runtime;
  ver `seguranca-build-docker.md`). Mesmo domínio com proxy reverso (`/api`) evita CORS.
- **Credenciais:** com cookie de sessão `HttpOnly`, `credentials: 'include'` (ou `same-origin`
  se mesmo domínio). Sem token manual no código. Se for token no cliente (ADR), cabeçalho
  `Authorization` montado aqui e **nunca** em `localStorage` sem justificativa.
- **Cabeçalho CSRF:** se a API usa cookie de sessão e exige token anti-CSRF, o cliente envia
  o cabeçalho definido pelo backend em métodos que alteram estado.
- **401:** chamar um callback registrado (o router leva ao login e guarda o `retorno`); não
  importe o router/store no cliente (evita ciclo). **403** é mensagem "sem permissão", não
  logout.
- **Tempo limite** em toda chamada; erro de rede (falha de `fetch`) vira mensagem amigável.
- `AbortSignal.timeout` e `AbortSignal.any` são recentes; **conferir suporte** nos
  navegadores-alvo, senão implemente com `AbortController` + `setTimeout`.
- Resposta com corpo é **dado externo**: tipada na fronteira (ver abaixo); validar em
  runtime só se o contrato não é confiável.

## Problem Details (RFC 9457, antes 7807) em mensagem

O contrato define erros como `application/problem+json`. Converta **em um lugar**.

```ts
// api/erros.ts
export interface ProblemDetails {
  type?: string; title?: string; status?: number; detail?: string; instance?: string
  errors?: Record<string, string[]> | { campo: string; mensagem: string }[]   // extensão: conferir o contrato
}

export class ErroApi extends Error {
  constructor(
    readonly status: number,
    readonly problem: ProblemDetails | null,
    mensagem: string,
  ) { super(mensagem) }

  /** erros por campo para o formulário */
  get campos(): Record<string, string> { return normalizarCampos(this.problem) }
}

export async function lerProblem(r: Response): Promise<ErroApi> {
  let problem: ProblemDetails | null = null
  try {
    if (r.headers.get('content-type')?.includes('json')) problem = await r.json()
  } catch { /* corpo inválido: segue com mensagem padrão */ }
  return new ErroApi(r.status, problem, mensagemPara(r.status, problem))
}

function mensagemPara(status: number, p: ProblemDetails | null): string {
  if (p?.detail) return p.detail                         // texto já pensado para o usuário (combinar com o back)
  switch (status) {
    case 400: case 422: return 'Confira os campos destacados.'
    case 403: return 'Você não tem permissão para isso.'
    case 404: return 'Não encontramos o que você procura.'
    case 409: return 'Essa ação conflita com o estado atual. Atualize e tente de novo.'
    default:  return status >= 500
      ? 'O serviço está com problemas. Tente de novo em instantes.'
      : 'Não foi possível concluir. Tente de novo.'
  }
}
```

Regras:

- **A mensagem exibida segue o texto do Design** para cada situação (tabela de mensagens do
  protótipo). `detail` do backend só aparece se o contrato diz que ele é seguro e escrito
  para o usuário; caso contrário use o texto do Design por `type`/código.
- **Nunca** mostrar stack, SQL, JSON cru ou `status` técnico para o usuário.
- 409 (ex.: sobreposição de reserva) tem **mensagem própria da regra**, mapeada por `type`
  (`/problemas/reserva-conflito`) para o texto do Design.
- 422/400 com erros por campo alimenta o formulário (`ErroApi.campos`) — ver
  `formularios-acessibilidade.md`.
- 5xx: mensagem genérica + id de correlação, se a API devolver (`instance`/cabeçalho), para
  o usuário informar ao suporte.

## Cancelamento

- Todo `requisitar` aceita `signal`. Telas que mudam de parâmetro (busca, troca de sala)
  cancelam a anterior (ver composable em `composition-api-componentes.md`).
- Ao desmontar o componente, cancele chamadas em curso.
- **Cancelamento não é erro**: `AbortError` é ignorado (sem mensagem).
- Cancelar no cliente **não** cancela o efeito no servidor: não use cancelamento como
  garantia em operações que gravam.

## Funções por recurso

```ts
// api/reservas.ts
import { requisitar } from './cliente'
import type { Reserva, NovaReserva } from './tipos'

export const listarReservas = (opts?: { signal?: AbortSignal }) =>
  requisitar<Reserva[]>('/reservas', { signal: opts?.signal })

export const criarReserva = (nova: NovaReserva) =>
  requisitar<Reserva>('/reservas', { method: 'POST', body: nova })

export const cancelarReserva = (id: string) =>
  requisitar<void>(`/reservas/${encodeURIComponent(id)}`, { method: 'DELETE' })
```

- Parâmetros de caminho com `encodeURIComponent`; query com `URLSearchParams`.
- Uma função por operação do OpenAPI, nome espelhando o `operationId` quando houver.
- Datas: a API trafega ISO 8601 com fuso; converter para `Date` na fronteira, exibir no
  fuso do Design, e enviar sempre instantes ISO.

## Tipos a partir do OpenAPI

O `openapi.yaml` é a fonte de verdade. Duas formas:

| Opção | Quando | Cuidados |
|---|---|---|
| **Gerar** os tipos (preferida) | Contrato extenso ou que muda | Arquivo gerado fica em `api/tipos.gen.ts`, **nunca editado à mão**; regerar quando o contrato mudar; gerar no `npm run` (script) para o CI reproduzir |
| **Escrever** à mão | Poucas operações em aplicação pequena | Conferir campo a campo com o contrato; diferença = bug; teste de contrato (QA) pega o resto |

Ferramentas open source de geração (conferir comando e versão antes de fixar):

- `openapi-typescript` (gera só tipos a partir do OpenAPI 3.x) combinado com `openapi-fetch`
  (cliente tipado leve que usa esses tipos), em conferir.
- `@hey-api/openapi-ts` ou OpenAPI Generator (`typescript-fetch`), geradores de cliente
  completo; mais código gerado a manter. (conferir)

Recomendação do projeto: **gerar só os tipos** e manter o **cliente único próprio** (acima),
para ter controle de 401, erros e cancelamento sem amarrar o front a um cliente gerado.

```json
{ "scripts": { "api:tipos": "openapi-typescript ../contrato/openapi.yaml -o src/api/tipos.gen.ts" } }
```

(O comando exato depende da versão da ferramenta e do local do `openapi.yaml`: **conferir**.)

```ts
// api/tipos.ts: nomes de domínio sobre o gerado
import type { components } from './tipos.gen'
export type Reserva = components['schemas']['Reserva']
export type NovaReserva = components['schemas']['NovaReserva']
```

Regras:

- **Contrato errado ou incompleto** (campo faltando, tipo diferente do real): não "corrija"
  no front. Abra **ACH para Arquitetura** (`etapa_destino: arquitetura`) com a evidência (request/response) em `evidencia`.
- Campos opcionais do contrato viram `| undefined` nos tipos: trate.
- Enumerações do contrato viram uniões; o front **não** inventa valor fora delas.
- O que o tipo não garante em runtime (o backend pode mentir): em fronteira de risco, validação
  em runtime (ex.: Zod, Valibot; conferir) é opcional; em aplicação pequena, teste de contrato do lado do
  backend basta.

## Erros comuns

| Erro | Efeito | Correção |
|---|---|---|
| `fetch` direto em componente | Tratamento divergente | Cliente único |
| Mostrar `detail`/JSON cru sem checar | Vaza ou confunde | Mensagem do Design por `type`/status |
| Cliente importa o router e a store | Ciclo de dependência | Callback registrado para 401 |
| 403 tratado como 401 | Logout indevido | 403 = mensagem de permissão |
| Sem tempo limite | Tela travada | `timeoutMs` padrão |
| `AbortError` mostrado como erro | Mensagem falsa | Ignorar cancelamento |
| Editar o arquivo gerado | Perde na próxima geração | Regerar; ajustar no contrato |
| Tipos escritos de memória | Divergem do contrato | Gerar ou conferir contra o OpenAPI |
| Corrigir contrato no front | Contrato vira ficção | ACH para Arquitetura |
| Token em `localStorage` | XSS rouba | Cookie `HttpOnly` |
