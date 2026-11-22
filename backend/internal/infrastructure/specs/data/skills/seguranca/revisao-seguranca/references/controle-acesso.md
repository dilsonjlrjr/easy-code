# Controle de acesso: IDOR/BOLA, checagem por recurso e testes

A01 é a categoria que mais produz achados Críticos/Altos em aplicações com dados de usuários.
Prompt do agente: "atenção especial a controle de acesso (quem pode ver, alterar e cancelar o
quê)".

## 1. Três perguntas por endpoint

1. **Autenticação:** exige identidade? (anônimo recebe 401)
2. **Autorização por função:** o **papel** pode usar esta operação? (comum recebe 403 em rota admin)
3. **Autorização por recurso:** este usuário pode agir sobre **este** objeto? (dono, mesma
   organização, participante)

A 3ª é a que falta. **IDOR** (Insecure Direct Object Reference) / **BOLA** (Broken Object Level
Authorization): acessar objeto de outro trocando o identificador.

## 2. Inventário de endpoints (matriz)

Passo 1 da revisão: montar a matriz a partir do contrato (OpenAPI) e do código das rotas.

| Endpoint | Auth | Papel exigido | Escopo do recurso | Onde é checado (arquivo:linha) | Testado |
|---|---|---|---|---|---|
| `GET /reservas` | sim | qualquer | só do usuário (admin: todas) | ReservaService:30 | sim |
| `GET /reservas/{id}` | sim | qualquer | dono ou admin | ReservaService:44 | sim |
| `DELETE /reservas/{id}` | sim | qualquer | dono ou admin | **ausente** | falha -> ACH |
| `POST /salas` | sim | admin | — | SalaResource:12 | sim |
| `GET /usuarios/{id}` | sim | admin | — | UsuarioResource:20 | sim |

Rotas **sem linha** na matriz = rota esquecida. Compare a matriz com a lista de rotas reais
(`graft`/grep do framework de rotas; no OpenAPI, `paths`) para achar endpoints não documentados
(ex.: ferramentas de debug, `/actuator`, `/admin`, `/internal`).

## 3. Onde a checagem deve morar

| Local | Avaliação |
|---|---|
| **Camada de serviço/regra de negócio**, perto dos dados | **Preferido**: vale para qualquer entrada (API, job, evento) |
| Política centralizada (filtro/anotação/policy) aplicada por padrão (negar se ausente) | Bom, desde que "sem anotação" = negar |
| Controlador/handler, repetido por rota | Frágil: rota nova esquece |
| Somente no front-end (botão escondido) | **Não é controle** |
| Consulta que **já filtra** por dono (`WHERE id=? AND dono_id=?`) | Ótima: não existe objeto alheio para vazar |

Padrão forte: o repositório recebe o **usuário** e filtra sempre — o objeto alheio sequer é
carregado.

```java
// Vulnerável (IDOR): carrega qualquer reserva e devolve
@GetMapping("/reservas/{id}")
Reserva ver(@PathVariable long id) { return repo.findById(id).orElseThrow(); }

// Corrigido: consulta já escopada ao dono (ou admin)
Reserva ver(long id, Usuario atual) {
  return (atual.ehAdmin() ? repo.findById(id) : repo.findByIdAndDonoId(id, atual.id()))
      .orElseThrow(NaoEncontrado::new);   // mesmo erro para "não existe" e "não é seu"
}
```

```js
// Express: vulnerável x corrigido
app.delete('/reservas/:id', auth, async (req, res) => {
  await db.query('DELETE FROM reservas WHERE id=$1', [req.params.id]);             // qualquer um apaga
  res.sendStatus(204);
});
app.delete('/reservas/:id', auth, async (req, res) => {
  const r = await db.query(
    'DELETE FROM reservas WHERE id=$1 AND (dono_id=$2 OR $3) RETURNING id',
    [req.params.id, req.user.id, req.user.admin]);
  res.sendStatus(r.rowCount ? 204 : 404);                                           // filtrado na consulta
});
```

**403 ou 404?** Responder **404** para recurso fora do escopo evita revelar existência; **403**
é mais explícito. Escolha vem do ADR/RNF; o importante é a **consistência** e o **estado final
intacto**.

## 4. Mass assignment (atribuição em massa)

O servidor copia o corpo da requisição para o objeto sem lista de campos permitidos: o cliente
envia `"donoId": 1` ou `"papel": "admin"`.

```java
// Vulnerável
Usuario u = mapper.convertValue(corpo, Usuario.class);   // inclui papel, id
repo.save(u);
// Corrigido: DTO de entrada com somente os campos editáveis; dono/papel vêm do contexto
record AtualizarPerfil(String nome) {}
```

**Provar:** enviar campos extras (`papel`, `donoId`, `id`, `criadoEm`) e conferir no GET se
mudaram. Mudou = achado.

## 5. Plano de teste com dois usuários (modo AUDITORIA)

Ambiente local: usuários `A`, `B` (mesmo papel) e `admin` do seed.

Procedimento por **tipo de recurso**:
1. A cria o recurso `X`; guarde o id.
2. Com a sessão/token de **B**, tentar: **ler** (GET por id), **listar** (X aparece na lista de
   B?), **atualizar** (PUT/PATCH), **apagar** (DELETE), **ações** (`/cancelar`, `/aprovar`),
   **sub-recursos** (`/X/anexos`), **exportar/baixar**, **buscar** por texto que ache X.
3. Com **sem token**: todas as operações -> 401.
4. Com **papel comum** em rotas de admin -> 403.
5. Ids vizinhos (`id±1`) e ids inexistentes: respostas coerentes, sem revelar dados.
6. Parâmetros alternativos: `?usuarioId=` ou `?donoId=` na query/corpo tentando ver o de outro.

```bash
# Prova local mínima (ajuste URL/tokens): B tenta apagar a reserva de A
curl -i -X DELETE "$API_URL/reservas/$ID_DE_A" -H "Authorization: Bearer $TOKEN_B"
# Estado final: A ainda vê a reserva?
curl -s "$API_URL/reservas/$ID_DE_A" -H "Authorization: Bearer $TOKEN_A"
```

**Evidência de falha:** a requisição de B, o status (204/200) e a prova de que o estado mudou
(ou de que o dado de A foi devolvido). Isso é o ACH-04 do exemplo em `escrever-ach-seguranca.md`.

Tabela de resultados a anexar ao REL-SEG ou ao ACH:

| Operação | Sem token | B (outro) | A (dono) | Admin |
|---|---|---|---|---|
| GET /reservas/{id} | 401 | 404 | 200 | 200 |
| DELETE /reservas/{id} | 401 | **204 (falha)** | 204 | 204 |

## 6. Padrões de falha para caçar no código

| Padrão | Sinal |
|---|---|
| `findById(id)` direto no controlador, sem usuário | IDOR |
| Autorização só em `@GetMapping` e não em `@PutMapping`/`@DeleteMapping` | verbo esquecido |
| Papel lido de `req.body`/`localStorage`/token sem assinatura verificada | confiança no cliente |
| `if (user.id == recurso.dono)` comparando tipos diferentes (string x int) | bypass por coerção |
| Checagem depois de executar a ação | corrida/efeito colateral |
| Endpoint de listagem com filtro opcional de dono | omitir parâmetro lista tudo |
| Rota nova copiada de outra sem a checagem | cobertura inconsistente |
| Funções administrativas protegidas só por "rota obscura" | segurança por obscuridade |
| Upload/download por caminho vindo do cliente | path traversal + IDOR |
| IDs sequenciais + sem escopo | enumeração |
| Políticas por tenant sem filtro de tenant nas consultas | vazamento entre organizações |

## 7. Controles adicionais

- Autorização **negar por padrão**: sem regra explícita, recusa.
- Identificadores não sequenciais (UUID) ajudam, mas **não substituem** a checagem.
- Registro de acesso negado (A09) com usuário, recurso e instante.
- Limite de taxa em endpoints que permitem enumeração.
- Testes automatizados de autorização (QA: `cenarios-teste/references/autorizacao-seguranca-funcional.md`); Segurança audita e confirma.

## 8. Severidade típica

| Situação | Severidade |
|---|---|
| Ler dado pessoal de outros usuários (IDOR de leitura) | Crítico (vazamento) ou Alto, conforme volume/sensibilidade |
| Alterar/apagar recurso de outro (IDOR de escrita) | Alto (Crítico se dado crítico/irreversível) |
| Função administrativa acessível a usuário comum | Alto/Crítico |
| Mass assignment que eleva papel | Crítico |
| Enumeração de ids sem vazamento de conteúdo | Baixo/Médio |
| 403 x 404 inconsistente | Baixo |

(Use a matriz probabilidade x impacto de `modelagem-ameacas-stride.md` e a tabela do ACH.)
