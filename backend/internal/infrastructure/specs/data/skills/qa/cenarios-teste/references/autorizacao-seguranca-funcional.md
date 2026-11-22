# Cenários de autorização e segurança funcional

Segurança **funcional** = comportamentos de acesso e de entrada hostil que dá para provar com
um teste repetível e que fazem parte dos critérios de aceite. QA cobre isso nos CT (tipo
`segurança`). A auditoria da etapa Segurança é complementar (varreduras, revisão por categoria
OWASP, modelo de ameaças); não substitui estes testes.

## 1. Autenticação (quem é você)

Para toda rota/tela que exige login:

| Cenário | Esperado |
|---|---|
| Sem credencial (sem token/cookie) | 401 na API; redireciona ao login na tela; nenhum dado vaza no corpo |
| Token malformado | 401 |
| Token expirado | 401; a tela leva ao login e preserva a intenção quando o requisito pede |
| Token de outro ambiente/assinatura inválida | 401 |
| Logout invalida a sessão | reuso do token/cookie antigo após logout retorna 401 |
| Senha errada vs usuário inexistente | **mesma** mensagem e mesmo status (não enumera usuários) |
| Tentativas repetidas | após N falhas (valor do RNF): bloqueio/atraso conforme definido |
| Sessão no meio do fluxo expira | não perde dados silenciosamente; mensagem clara |

**Técnica:** gere a lista de rotas a partir do contrato (OpenAPI) e faça **um teste
parametrizado** "toda rota protegida sem token retorna 401", com lista explícita das rotas
públicas permitidas (login, health, assets). Rota nova fora da lista pública e sem proteção
quebra o teste.

```python
# exemplo ilustrativo (qualquer linguagem serve): lista explícita de públicas
PUBLICAS = {("POST", "/auth/login"), ("GET", "/health")}
for metodo, caminho in rotas_do_contrato():
    if (metodo, caminho) in PUBLICAS:
        continue
    r = cliente.request(metodo, caminho)  # sem credencial
    assert r.status_code == 401, f"{metodo} {caminho} deveria exigir login"
```

## 2. Autorização por papel

Monte a **matriz papel x ação** a partir do requisito/ADR e teste cada célula **negada** (as
permitidas já são os positivos das US):

| Ação | Anônimo | Funcionário | Admin |
|---|---|---|---|
| Listar salas | 401 | 200 | 200 |
| Criar reserva | 401 | 201 | 201 |
| Cancelar reserva própria | 401 | 204 | 204 |
| Cancelar reserva alheia | 401 | 403 | 204 |
| Criar/editar sala | 401 | 403 | 201/200 |
| Ver relatório de uso | 401 | 403 | 200 |

Cada célula "403" é um CT (ou linha de um teste parametrizado). **Esperado completo:** status
correto **e** nenhum efeito (a reserva continua existindo; a sala não foi criada).

## 3. Autorização por recurso (IDOR / BOLA)

O erro mais comum e mais grave em aplicações web: o sistema confere que o usuário está logado,
mas não que o recurso **é dele**. Cenário padrão:

1. Usuário A cria o recurso `X` (guarde o id).
2. Usuário B (mesmo papel) tenta ler, alterar e apagar `X` pelo id.
3. Esperado: 403 ou 404 (conforme decisão de design — registre qual; 404 evita revelar
   existência) e `X` intacto no banco.
4. Repita para **todas as operações** (GET, PUT/PATCH, DELETE, ações como `/cancelar`) e para
   **recursos aninhados** (`/reservas/{id}/participantes`) e para **listagens** (B não vê o item
   de A; filtros e paginação não vazam; busca por texto não vaza).

Pontos que costumam escapar:
- a **listagem** respeita o escopo, mas o **detalhe** por id não (ou o contrário);
- **exportações**, relatórios, anexos (`/arquivos/{id}`), notificações, endpoints de contagem;
- **mass assignment:** enviar `donoId` ou `papel` no corpo e ver se o servidor aceita;
- **IDs sequenciais** previsíveis; testar vizinhos `id±1`;
- **ação sobre estado** (admin de outra organização/tenant), se houver multiusuário por organização.

**Exemplo de CT**

CT-24 (campos próprios do CT, esquema `ct.schema.json`; o cabeçalho comum é o do template):

```yaml
titulo: Funcionário não cancela reserva de outro
cobre: {us: US-05, criterio: CA-3}
nivel: integracao
tipo_caso: seguranca
precondicoes:
  - Usuário A (funcionário) com reserva ativa id=R1 (sala "Azul", amanhã 10h–11h)
  - Usuário B (funcionário) autenticado
passos:
  - Com o token de B, enviar o pedido de cancelamento da reserva R1
resultado_esperado:
  - Resposta 403 (ou 404, conforme ADR), sem corpo com dados de R1
  - R1 continua ativa quando A consulta
```

Veja também `revisao-seguranca/references/controle-acesso.md` para como a Segurança audita o mesmo risco no código.

## 4. Entrada hostil (validação no servidor)

A validação de tela **não conta**: teste a API diretamente.

| Entrada | Esperado |
|---|---|
| Aspas e `' OR '1'='1` em campos de texto e filtros | tratada como texto; sem erro 500; sem mudar resultado |
| `<script>alert(1)</script>` no título | armazenado como texto; exibido escapado (e2e: nenhum diálogo/alerta, texto literal na tela) |
| Campo com 10 mil caracteres | 400/413, sem derrubar |
| JSON inválido, tipo errado (`"capacidade": "abc"`) | 400 com mensagem útil, sem stack trace |
| Campos extras (`"admin": true`) | ignorados ou 400, nunca aplicados |
| Parâmetro de ordenação/campo inexistente (`?sort=senha`) | 400, nunca expõe coluna |
| Upload: extensão falsa, tamanho acima do limite, tipo MIME divergente | recusado conforme RNF |
| Caminho `../../etc/passwd` em nome de arquivo | recusado/sanitizado |

Registre como CT apenas os que correspondem a requisitos/RNF; o restante fica a cargo da
auditoria de Segurança (A05 injeção), para não duplicar.

## 5. Vazamento de informação em respostas

- **Erros:** 500 não mostra stack trace nem SQL; mensagem genérica + id de correlação.
- **Respostas:** não incluem campos internos (hash de senha, tokens, ids de outros usuários, e-mail de terceiros).
- **404 vs 403:** coerente com o ADR; não revele "existe mas não é seu" se a política é esconder.
- **Cabeçalhos/CORS/cookies:** conferidos em `revisao-seguranca/references/cabecalhos-cors.md`; QA cobre só o que está num RNF (ex.: cookie `HttpOnly`).

## 6. Tipo "segurança" no CT: quando usar

Use `tipo_caso: seguranca` quando o resultado esperado é **uma recusa por política** (401, 403, 404
por escopo, validação hostil). Positivo comum continua `positivo`. Isso permite o gate filtrar
os CT de segurança e a Segurança reaproveitar a evidência.

## 7. Prioridade (risco)

| Cenário | Prioridade |
|---|---|
| IDOR em leitura/alteração de dado de outro | máxima (Crítico/Alto se falhar) |
| Rota protegida sem 401 | máxima |
| Matriz de papéis (negada) | alta |
| Enumeração de usuário no login | média |
| Mensagem de erro com detalhe | média |

Severidade de ACH resultante: ver `execucao-automacao-evidencia.md` seção 7 e a tabela do
`ACH.md`.

## 8. Anti-padrões

- Testar autorização só pela interface (esconder o botão não é controle de acesso).
- Testar só o papel admin e concluir que "funciona".
- Verificar 403 sem verificar que nada mudou.
- Usar o mesmo usuário para "dono" e "outro".
- Confundir 401 (não autenticado) com 403 (autenticado, sem permissão).
