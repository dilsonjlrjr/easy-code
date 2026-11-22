# Troubleshooting do ambiente Docker

> **Onde isto roda.** `docker-compose.yml` e Dockerfiles do projeto gerado continuam sendo artefatos entregues, para CI e para humanos. Os comandos `docker compose ...` e `docker run ...` deste arquivo são **para CI/humano**. No sandbox da plataforma o agente não tem Docker nem rede: testes de integração/E2E rodam contra os **serviços de apoio declarados no perfil** (ex.: PostgreSQL efêmero) que o runner sobe, e só com os comandos da allowlist da sua categoria (`testes`, `migracoes`, `banco_descartavel`).

Método: **ler o log do serviço que falhou antes de mudar qualquer coisa.** A maioria dos
problemas é configuração (ordem, nome de host, variável, porta, permissão).

## Comandos de diagnóstico

```bash
docker compose ps                         # estado e saúde (healthy/unhealthy/exited)
docker compose logs --tail=100 api        # últimas linhas de um serviço
docker compose logs -f db api             # acompanhar vários
docker compose config                     # arquivo final com variáveis resolvidas (valida sintaxe)
docker compose exec api sh                # entrar no contêiner em execução (se houver shell)
docker compose run --rm api <comando>     # rodar um comando avulso
docker inspect --format '{{json .State.Health}}' <contêiner>   # detalhes do healthcheck
docker compose down -v && docker compose up --build            # recriar do zero
docker system df                          # uso de disco por imagens/volumes/cache
```

Ao diagnosticar, **registre o comando e o trecho do log** no relatório (`registro_dev` da T).

## Tabela de sintomas

| Sintoma | Causa provável | Verificar e corrigir |
|---|---|---|
| API cai ao subir com "connection refused" ao banco | Banco ainda não pronto, ou host errado | `depends_on: service_healthy`; usar `db`, não `localhost`; retry de conexão no pool |
| `password authentication failed` | Senha do `.env` diferente da que o volume foi criado | Banco guarda a senha da **primeira** criação: `make reset` (apaga volume) ou alinhar a senha |
| Alterei `POSTGRES_DB/USER` e nada mudou | Volume já inicializado | `docker compose down -v` |
| `init.sql` não executou | `/docker-entrypoint-initdb.d` só roda com data dir vazio | `down -v`, ou use migração por ferramenta |
| `port is already allocated` / `address already in use` | Porta do host ocupada | Mudar `*_PORT` no `.env`; `lsof -i :PORTA` para achar o processo |
| Contêiner `unhealthy` | Healthcheck falha | Ver `docker inspect ... .State.Health` e o log; conferir ferramenta do teste (`wget`/`curl` existe?) |
| Healthcheck "not found" no contêiner | Imagem sem `curl`/`wget`/shell | Instalar o mínimo, usar alpine, ou subcomando do binário |
| Navegador não alcança a API | Front chamando `http://api:8080` | Usar URL pública (`localhost:PORTA`) ou proxy no servidor do front |
| Erro de CORS | Origem do front não autorizada na API | Lista fechada de origens com a URL do front; ou proxy `/api` |
| Front mostra versão antiga | Cache do build/imagem ou do navegador | `docker compose up --build`, limpar cache do navegador, `--no-cache` se preciso |
| Mudança no código não aparece | Imagem não reconstruída / sem bind mount | `--build`; no perfil dev, bind mount |
| Build lento a cada mudança | Cache de camadas invalidado | Manifesto + lock antes do código; `.dockerignore`; BuildKit |
| Contexto de build enorme (`Sending build context`) | Sem `.dockerignore` | Criar excluindo `node_modules`, `.git`, `target` |
| `permission denied` em volume/arquivo | Dono/UID divergente (usuário não root) | `chown` na imagem; ajustar permissões do bind mount; preferir volume nomeado |
| Ao escrever em bind mount, arquivos viram root no host | Processo como root no contêiner | Rodar com UID do host em dev (`user: "${UID}:${GID}"`) |
| `exec format error` | Arquitetura diferente (ARM x AMD64) | Imagem multi-arch ou `platform:` explícito; reconstruir na arquitetura certa |
| Variável vazia no contêiner | `.env` ausente ou nome diferente | `docker compose config`; copiar `.env.example` |
| Aviso "variable is not set" | Falta no `.env` | Acrescentar ao `.env` e ao `.env.example` |
| Senha com `@`, `#`, `/` quebra a URL do banco | Caracteres especiais na `DATABASE_URL` | Codificar (percent-encoding) ou usar senha simples em dev |
| Migração falha por extensão (`btree_gist`) | Sem permissão ou extensão indisponível | Usuário com permissão; imagem oficial inclui `contrib`; conferir em serviço gerenciado |
| Migração roda duas vezes/competição | Várias réplicas migrando | Ferramenta com lock; ou serviço de migração à parte |
| API lenta/OOM | Limite de memória baixo ou pool de conexões grande | Ajustar limites; pool menor; `MaxRAMPercentage` (JVM) |
| Banco lotado de conexões | Pool grande × réplicas | Reduzir pool; `max_connections` |
| Disco cheio | Imagens/volumes antigos | `docker system df`; `docker image prune`; `docker volume prune` (cuidado: apaga dados) |
| `docker compose` não reconhecido | Compose V1 (`docker-compose`) ou plugin ausente | Instalar Compose V2; a documentação usa `docker compose` |
| Teste e2e flaky | Espera por `sleep`, seed não determinístico | Esperas por condição; seed fixo; ambiente efêmero |
| Time zone/horas deslocadas | Imagens em UTC, app esperando local | Guardar `timestamptz`, converter na borda; `TZ` só se necessário e documentado |
| `no space left on device` no build | Cache do BuildKit cheio | `docker builder prune` |
| Mudou o `.env` e nada mudou | Contêineres antigos mantêm o ambiente | `docker compose up -d --force-recreate` |
| `unhealthy` só no começo, depois ok | `start_period` curto | Aumentar `start_period`/`retries` |

## Árvore rápida: "não sobe"

1. `docker compose ps`: qual serviço está `exited`/`unhealthy`?
2. `docker compose logs --tail=100 <serviço>`: qual a primeira mensagem de erro (a primeira, não a última)?
3. Conexão ao banco? -> host `db`, porta 5432, usuário/senha/banco do `.env`, volume antigo.
4. Variável faltando? -> `docker compose config`.
5. Porta ocupada? -> trocar no `.env`.
6. Build falhou? -> logs do `docker compose build`, contexto, `.dockerignore`, versão da base.
7. Ainda sem pista? -> `docker compose down -v` e subir do zero para eliminar estado velho.

## Boas práticas para evitar o problema

- `docker compose config` e `docker compose build` na T-01 e no CI.
- README com os 3 comandos essenciais e as portas.
- Healthchecks reais (que testam a dependência) em vez de "processo está vivo".
- Retentativa de conexão ao banco na aplicação.
- Mensagens de erro de configuração claras (qual variável falta).
- Nada de estado manual: tudo que o dev faz uma vez vira `make`.

## Cuidados com comandos destrutivos

`down -v`, `volume prune` e `system prune` **apagam dados**. Em ambiente de dev/teste são
rotina; nunca executar em ambiente com dados que importem. Sempre confirmar o contexto
(`docker context ls`, nome do projeto `-p`).

## Checklist

- [ ] Log lido antes de qualquer alteração.
- [ ] Causa identificada (variável, ordem, host, porta, permissão, volume antigo).
- [ ] Correção aplicada na configuração versionada, não só localmente.
- [ ] Do zero (`down -v` + `up`) funciona.
- [ ] Causa recorrente registrada no README/`estrutura.md`.

## Erros comuns

| Erro | Correção |
|---|---|
| Reiniciar tudo sem ler o log | Ler primeiro |
| Corrigir só na máquina, não no repositório | Versionar a correção |
| `prune` em ambiente com dados | Conferir contexto antes |
| Aumentar `retries` para esconder falha real | Entender por que o serviço não fica pronto |
| Culpar o Docker por erro de configuração | Verificar `compose config` |
