# easycode-desktop — cliente local (app desktop, somente leitura)

App desktop do Easy Code para **acompanhar ondas ao vivo e receber notificações do sistema operacional**.

- **Wails 3** (`github.com/wailsapp/wails/v3` v3.1.0): processo Go + WebView do sistema; interface em Vue 3 (`ui/`),
  embutida no binário. A WebView não carrega conteúdo remoto nem fala com a api: só o processo Go fala com a api e
  com o Keycloak; a interface recebe dados pelos métodos ligados (`internal/app.Service`) e pelos eventos do Wails.
- **Somente leitura**: não há controle de onda, projeto ou achado nem decisão. Toda ação é o botão
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
  `easycode.ended` tratados como na API web.
- **Notificações nativas** para gate aguardando aprovação, pergunta, achado escalado, alerta de orçamento e papel
  atribuído (texto completo: tipo, projeto e etapa); onda concluída, falha ou cancelada quando a vista está aberta
  ou a pessoa pediu para seguir. Clicar abre a tela web (ou a vista da onda). Som desligável na Conta.
- Fechar a janela mantém o app na bandeja do sistema; "Sair do app" no menu da bandeja encerra.
- **Interface**: segue a página "Cliente desktop" do canvas "EasyCode — Interface" (mesmos fundamentos da web:
  fundo, cartões, estados e botões; fonte Inter empacotada). Janela de tamanho fixo (1024×720); o menu lateral
  recolhe (ícone centralizado e nome abaixo) e lembra o estado neste computador. O tema segue o do sistema,
  escuro ou claro.

## Estrutura

| Caminho | Conteúdo |
|---|---|
| `main.go` | Inicialização do Wails: janela de tamanho fixo (1024×720), bandeja, notificações, ligação do serviço |
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
make -C desktop ci        # gofmt, vet, testes Go com -race, lint, type-check e testes da UI, binário Linux
make -C desktop test      # testes Go
make -C desktop ui-test   # testes Vitest (inclui acessibilidade)
make -C desktop build     # bin/easycode-desktop (linux/amd64); VERSION=1.2.3 para gravar a versão
```

Rodar no Linux exige GTK ≥ 4.10 e WebKitGTK 6 (Debian 13, Ubuntu 24.04 ou mais novos) e um serviço Secret
Service (GNOME Keyring ou KWallet). Para distribuições mais antigas (GTK 3 e WebKitGTK 4.1, por exemplo Ubuntu
22.04), compilar com `-tags gtk3` numa imagem com `libgtk-3-dev libwebkit2gtk-4.1-dev`.

### macOS e Windows

O Wails usa cgo no macOS (WebKit) e a WebView2 no Windows; a compilação cruzada a partir do Linux não é
suportada de forma confiável. Compilar no próprio SO, com Go 1.26 e Node 24:

```sh
cd desktop/ui && npm ci && npm run build && cd ..
go build -tags production -trimpath -ldflags "-s -w -X main.version=$VERSION" -o bin/easycode-desktop .
# Windows: acrescentar -ldflags "-H windowsgui" e gerar easycode-desktop.exe
```

macOS precisa do Xcode Command Line Tools; Windows precisa do runtime WebView2 (já presente no Windows 11).

### Ícone do produto

Os ícones saem dos SVG de `project/brand/` (`make -C project icons`) e ficam versionados. Cada sistema recebe
o ícone assim:

| Onde | macOS | Windows | Linux |
|---|---|---|---|
| App, Dock, barra de tarefas, alternador | `EasyCode.app` com `icons.icns` | `.exe` com `icon.ico` (`rsrc_windows_*.syso`) | atalho `br.easycode.desktop.desktop` e ícones hicolor |
| Janela | ícone do bundle | ícone do `.exe` | ícone embutido no binário |
| Bandeja / barra de menus | ícone template (o macOS pinta claro/escuro) | ícone colorido | ícone colorido |
| Notificações | ícone do bundle (exige o identificador `br.easycode.desktop`) | logo do toast (`appLogoOverride`) | imagem da notificação (`image-path`) |
| Cabeçalho do app | marca no topo | marca no topo | marca no topo |

```sh
make package-darwin        # no Mac (Apple Silicon): bin/EasyCode.app e bin/EasyCode-macos-arm64.zip
make build install-linux   # no Linux: binário em ~/.local/bin, atalho e ícones em ~/.local/share
make syso                  # depois de mudar o ícone: recria os recursos do .exe
```

Sem assinatura Developer ID, o macOS bloqueia a primeira abertura: `xattr -dr com.apple.quarantine EasyCode.app`.
Instalador assinado (.dmg, MSIX) e atualização automática ainda não existem.

## Estado da validação

| Item | Situação |
|---|---|
| Wails 3 estável | `v3.1.0`, com bandeja, notificações e eventos na API estável |
| Linux | Compila com GTK 4 e WebKitGTK 6. Login, cofre, eventos ao vivo com retomada e notificações têm testes automáticos contra servidores falsos |
| Keycloak real, macOS e Windows | Ainda não verificados. Pedem roteiro manual em cada sistema |
| Leitores de tela (Orca, VoiceOver, Narrador) | Ainda não verificados. A interface passa na varredura automática de acessibilidade (axe) |
| Tamanho | Binário Linux amd64 com a interface embutida: 11 MB |

O Wails 3.1 exige GTK 4.10 ou mais novo. Com o GTK 4.8 do Debian 12, a compilação falha. Por isso a imagem de
build usa Debian 13 (trixie).
