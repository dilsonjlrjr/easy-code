# Instalação da Easy Code

Guia do assistente de instalação (US-55, ESP-31, ADR-22). O assistente roda **uma vez**. Ele conecta o banco,
integra o Keycloak, cria o administrador e importa as skills da organização. Depois, a plataforma sobe pronta para o
primeiro login.

> Este guia é para **instalar a plataforma** num servidor. Para desenvolver, use o `compose.yaml` da raiz, que traz
> um realm com usuários de teste (README da raiz, "Desenvolvimento").

## Requisitos

- Linux x86_64 com **Docker** (Engine 24 ou mais novo, com o plugin `docker compose` 2.24 ou mais novo) e **make**.
- Portas livres em `127.0.0.1`: 8090 (assistente), 8080 (web), 8081 (api) e 8180 (Keycloak que vem na instalação).
- Para usar um PostgreSQL próprio: versão 15 ou mais nova e uma credencial que crie banco e papéis (`CREATEDB` e
  `CREATEROLE`, ou superusuário).
- Para usar um Keycloak próprio: versão 24 ou mais nova e uma de duas credenciais:
  - administrador do realm `master`, para o assistente criar um realm para a plataforma (recomendado);
  - conta com `realm-management` num realm que já existe.

## Instalar

```sh
cd easy-code-ai/project
make install
```

O comando faz quatro coisas, nesta ordem:

1. Grava `install/.env` com senhas aleatórias do PostgreSQL e do Keycloak que vêm na instalação. Isso só acontece na
   primeira vez. Guarde esse arquivo: ele não vai para o git.
2. Compila as imagens e sobe o PostgreSQL e o Keycloak. Na primeira vez, o Keycloak leva alguns minutos.
3. Abre o assistente e mostra no terminal o endereço e o **token de instalação**:

   ```text
   ================================================================
    Easy Code installation wizard

      Open:   http://localhost:8090
      Token:  3f9a1c2e-7b44d0aa-91e3c5f2-0d6b8e17
   ================================================================
   ```

4. Quando você conclui o assistente, a plataforma sobe. O terminal avisa quando a api estiver pronta.

### Servidor remoto

O assistente só escuta em `127.0.0.1`. Use um túnel SSH a partir do seu computador:

```sh
ssh -L 8090:127.0.0.1:8090 usuario@servidor
```

Depois abra `http://localhost:8090`. Para expor a porta, defina `EASYCODE_INSTALLER_BIND=0.0.0.0` antes do
`make install`. Mesmo assim, ninguém entra no assistente sem o token.

### PostgreSQL e Keycloak próprios

Na primeira execução, desligue os serviços que vêm na instalação:

```sh
EASYCODE_BUNDLED=false make install
```

No assistente, informe os endereços e as credenciais dos seus servidores. A escolha fica gravada em `install/.env`.

## As etapas do assistente

| # | Etapa | O que você informa | O que o teste confere |
|---|---|---|---|
| 1 | Boas-vindas | o token do terminal | token e volume de configuração |
| 2 | Banco de dados | servidor, credencial de administrador, nome do banco, SSL | conexão, permissão, banco sem tabelas da plataforma |
| 3 | Organização | nome, moeda, endereço público da web, idioma | endereço válido |
| 4 | Keycloak | endereço público e interno, modo (realm novo ou existente), credencial | login; o que será criado no realm; conflito de client |
| 5 | Administrador | usuário novo (senha temporária) ou usuário do realm | usuário e e-mail livres, ou usuário ativo |
| 6 | Usuário comum (opcional) | mesmo da etapa 5, sem papel de administrador | igual à etapa 5 |
| 7 | Skills (opcional) | pacotes `.zip` com `SKILL.md` | formato, tamanho e nome (o catálogo nativo já vem incluído) |
| 8 | Revisão | — | — |
| 9 | Instalação | — | passos com progresso; um passo que falha pode ser repetido |
| 10 | Concluído | — | resumo para baixar, sem senha |

Até a revisão, nada é gravado. Os testes só leem. Depois de **Instalar**, não há volta: só **Tentar de novo** no
passo que falhou.

### O que o assistente cria

- **No PostgreSQL:**
  - o banco, com dono `easycode_migrator`;
  - os papéis da ADR-18 (`easycode_api`, `easycode_worker` e os demais);
  - os logins `easycode_migrator`, `easycode_api_login` e `easycode_worker_login`, com senhas geradas;
  - o esquema completo da plataforma.
- **No Keycloak, num realm novo:**
  - o realm;
  - o papel `easycode-administrator`;
  - os clients `easycode-api`, `easycode-events` e `easycode-desktop`;
  - o client scope `easycode-read`;
  - os eventos de login.
- **No Keycloak, num realm existente:**
  - só o que falta da lista acima, mostrado antes de aplicar;
  - um client que já existe com outra configuração é recusado, e nada é alterado nele.
- **Pessoas:** o administrador, com o papel, e o usuário comum, se você informar um. A senha é temporária, e o
  Keycloak pede uma nova no primeiro login.
- **Configuração:** fica no volume `easycode-platform_easycode-config`:
  - `easycode.env`, sem segredos;
  - `secrets/`, com um arquivo por segredo;
  - `installed.json`, a trava.

As credenciais de administrador do banco e do Keycloak ficam só na memória do assistente. Elas não vão para arquivo,
log, nem resumo.

## Depois de instalar

1. Abra o endereço público da web e entre como o administrador.
2. Em **Configurações**, cadastre a chave do provedor de IA e a tabela de preços. A chave só é gravada depois de
   passar no teste pelo gateway.

Por padrão, o gateway roda sem rede, com um provedor falso. Para usar provedores reais:

```sh
make install-up COMPOSE_EXTRA="-f install/compose.providers.yaml"
```

> **Atenção:** o proxy de saída com lista de destinos permitidos (ADR-16) ainda não faz parte da stack. Com esse
> override, o gateway alcança qualquer endereço.

### Operação

```sh
make install-up     # sobe de novo (após reiniciar o servidor ou atualizar as imagens)
make install-logs   # acompanha os logs
make install-down   # para, mantendo os dados
```

### HTTPS

A stack ainda não tem proxy reverso com TLS (ADR-16). Antes de abrir a web (8080) e o Keycloak (8180) para outras
máquinas, coloque um proxy TLS na frente e defina:

- `EASYCODE_SSO_PUBLIC_URL`, com o endereço público do Keycloak;
- o endereço público da web, no assistente.

O realm recusa HTTP simples vindo de endereços públicos (`sslRequired: external`).

## Instalar de novo

O assistente não roda sobre uma instalação concluída. Para recomeçar do zero, apague os dados:

> **Atenção:** os comandos abaixo apagam o banco, os repositórios e a configuração. Não há como desfazer.

```sh
make install-down
docker compose --env-file install/.env -f install/compose.yaml --profile bundled down -v
rm install/.env
make install
```

Com PostgreSQL e Keycloak próprios, apague também o banco e o realm (ou os clients) criados na instalação anterior.

## Problemas comuns

| Sintoma | Causa provável |
|---|---|
| "Não foi possível conectar" no banco | Host ou porta errados. No compose, o PostgreSQL que vem na instalação é `postgres:5432`. |
| "Esta credencial não pode criar banco e papéis" | Falta `CREATEDB` e `CREATEROLE` à credencial. |
| "Já existe um realm com este nome" | Escolha outro nome ou use o modo "realm que já existe". |
| "O client … já existe com outra configuração" | Ajuste ou remova o client no Keycloak e teste de novo. |
| O terminal não mostra o token | O token aparece quando o assistente abre: rode `make install` de novo. |
| A api não fica pronta | `make install-logs`, e confira os serviços `migrate` e `api`. |
