# easycode-desktop — cliente local (app desktop, somente leitura)

App desktop do Easy Code para **acompanhar ondas ao vivo e receber notificações do sistema operacional**
(US-45, US-46, RNF-19). Decisão e desenho: [ADR-20](../../docs/arquitetura/adr/ADR-20.md),
[cliente-local.md](../../docs/arquitetura/api/cliente-local.md), [ESP-28](../../docs/requisitos/especificacoes/transversal/ESP-28-cliente-local.md).

- **Wails 3** (`github.com/wailsapp/wails/v3` v3.1.0): processo Go + WebView do sistema; interface em Vue 3 (`ui/`),
  embutida no binário. A WebView não carrega conteúdo remoto nem fala com a api: só o processo Go fala com a api e
  com o Keycloak; a interface recebe dados pelos métodos ligados (`internal/app.Service`) e pelos eventos do Wails.
- **Somente leitura** (PD-07): não há controle de onda, projeto ou achado nem decisão. Toda ação é o botão
  "Abrir na web", que abre no navegador padrão o endereço montado no Go a partir do servidor configurado
  (`internal/weblink`), nunca uma URL vinda da interface.
- **Servidor**: informado no primeiro login ou por `EASYCODE_SERVER`; gravado em `desktop.json` na pasta de
  configuração do usuário (XDG no Linux, `Application Support` no macOS, `%APPDATA%\easycode` no Windows). Exige
  `https`, exceto em `localhost` (pilha de desenvolvimento).
- **Login**: device flow (RFC 8628) com o cliente público `easycode-desktop`, escopo `easycode-read`; parâmetros
  lidos de `getLocalClientConfiguration`. O refresh token fica **só no cofre do SO** (Secret Service no Linux,
  Keychain no macOS, Credential Manager no Windows, via `go-keyring`); sem cofre o login falha com mensagem e
  nada é gravado em arquivo. O token de acesso fica só em memória e é renovado quando faltam menos de 30 s ou
  após `401`; refresh recusado apaga o cofre e volta ao login com "sessão revogada".
- **Ao vivo**: `watchNotices` (`/me/events`) aberto enquanto há sessão; `watchProjectEvents` filtrado pela onda
  com a vista da onda aberta, a partir de `last_sequence`. Cliente SSE próprio (`internal/sse`): Bearer,
  `Last-Event-ID`, backoff de 100 ms a 5 s com variação, conexão morta após 45 s sem quadro, `easycode.gap` e
  `easycode.ended` tratados como na sse.md seção 5.
- **Notificações nativas** para gate aguardando aprovação, pergunta, achado escalado, alerta de orçamento e papel
  atribuído (texto completo: tipo, projeto e etapa); onda concluída, falha ou cancelada quando a vista está aberta
  ou a pessoa pediu para seguir. Clicar abre a tela web (ou a vista da onda). Som desligável na Conta.
- Fechar a janela mantém o app na bandeja do sistema; "Sair do app" no menu da bandeja encerra.

## Estrutura

| Caminho | Conteúdo |
|---|---|
| `main.go` | Inicialização do Wails: janela (mínimo 800×600), bandeja, notificações, ligação do serviço |
| `internal/config` | Preferências locais (servidor, idioma, som) |
| `internal/keychain` | Adaptador do cofre do SO, sem fallback em arquivo |
| `internal/oidc` | Device flow, refresh e revogação |
| `internal/session` | Token de acesso em memória e refresh token no cofre |
| `internal/api` | Cliente REST só de leitura (+ `endSession`), Problem Details por `type` |
| `internal/sse` | Cliente SSE com retomada sem perda |
| `internal/notice` | Evento → notificação nativa (pt-BR e en) |
| `internal/weblink` | Endereços das telas web |
| `internal/app` | Núcleo ligado à interface (métodos e eventos) |
| `ui/` | Interface Vue 3 + vue-i18n (pt-BR padrão, en), testes Vitest com varredura axe |
| `build/Dockerfile` | Imagem de build e teste (Go 1.26 + GTK 4 + WebKitGTK 6, Debian trixie) |

## Comandos (só Docker no host)

```sh
make -C project/desktop ci        # gofmt, vet, testes Go com -race, lint, type-check e testes da UI, binário Linux
make -C project/desktop test      # testes Go
make -C project/desktop ui-test   # testes Vitest (inclui acessibilidade)
make -C project/desktop build     # bin/easycode-desktop (linux/amd64); VERSION=1.2.3 para gravar a versão
```

Rodar no Linux exige GTK ≥ 4.10 e WebKitGTK 6 (Debian 13, Ubuntu 24.04 ou mais novos) e um serviço Secret
Service (GNOME Keyring ou KWallet). Para distribuições mais antigas (GTK 3 e WebKitGTK 4.1, por exemplo Ubuntu
22.04), compilar com `-tags gtk3` numa imagem com `libgtk-3-dev libwebkit2gtk-4.1-dev`.

### macOS e Windows

O Wails usa cgo no macOS (WebKit) e a WebView2 no Windows; a compilação cruzada a partir do Linux não é
suportada de forma confiável. Compilar no próprio SO, com Go 1.26 e Node 24:

```sh
cd project/desktop/ui && npm ci && npm run build && cd ..
go build -tags production -trimpath -ldflags "-s -w -X main.version=$VERSION" -o bin/easycode-desktop .
# Windows: acrescentar -ldflags "-H windowsgui" e gerar easycode-desktop.exe
```

macOS precisa do Xcode Command Line Tools; Windows precisa do runtime WebView2 (já presente no Windows 11).
Empacotamento (AppImage/.deb, .dmg assinado, instalador assinado) e atualização automática ficam para a T do app
(cliente-local.md seção 6), por exemplo com `wails3 package`.

## Viabilidade do Wails 3 (spike da ADR-20)

Conferido em 2026-11-23, antes da construção:

| Critério da ADR-20 | Resultado |
|---|---|
| 1. Versão estável publicada | **Sim**: `v3.0.0` (2026-10-26) e `v3.1.0` (2026-11-01) no proxy de módulos Go; `@wailsio/runtime` 3.1.0 no npm. Bandeja, notificações (`pkg/services/notifications`) e eventos presentes na API estável |
| 2. App mínimo com device flow, cofre, `watchNotices` com reconexão e notificação | **Linux**: compila e liga com GTK 4/WebKitGTK 6 (`make build`); device flow, cofre (fake), SSE com retomada e notificações testados contra servidores falsos. Contra o Keycloak real e nos três SO: **não verificado** (sem interface gráfica nem macOS/Windows neste host) |
| 3. Leitor de tela (Orca, VoiceOver, Narrador) | **Não verificado** (pede roteiro manual); a interface passa na varredura axe automática |
| 4. Auxiliar `git-credential` | **Não construído** (US-42 fora deste recorte) |
| 5. Tamanho e memória | Binário Linux amd64 (`-s -w`, UI embutida): **11 MB**, dentro do alvo de 40 MB; memória ociosa não medida (sem interface gráfica no host de build) |

Achado: o Wails 3.1 usa GTK 4 por padrão e exige GTK ≥ 4.10 (`GtkFileDialog`); com o GTK 4.8 do Debian 12 a
compilação falha. A imagem de build usa Debian 13 (trixie). Nenhum critério reprovou; a troca para Tauri 2 não
se aplica. Os itens não verificados ficam como roteiro manual por SO.
