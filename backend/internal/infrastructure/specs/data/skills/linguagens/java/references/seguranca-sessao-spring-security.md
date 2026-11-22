# Segurança (Java): sessão em cookie com Spring Security

Decisão e conceitos: `arquitetura-web` (autenticação e autorização) e `revisao-seguranca`
(autenticação/sessão, controle de acesso, cabeçalhos/CORS). Aqui: a implementação padrão em
Spring Security 6+ — login próprio (usuário/senha), sessão em cookie `HttpOnly` guardada no
PostgreSQL com Spring Session JDBC. Pacotes de segurança mudam entre majors: conferir a
documentação da versão fixada nos pontos marcados.

## Modelo

```
POST /api/auth/login  -> valida senha -> cria/rotaciona sessão -> Set-Cookie: SESSION=...; HttpOnly; Secure; SameSite=Lax
GET  /api/...         -> cookie -> Spring Session lê a sessão no PostgreSQL -> principal
POST /api/auth/logout -> invalida a sessão (some do banco) -> expira o cookie
```

- O navegador nunca vê dado de sessão além do identificador opaco (cookie `HttpOnly`).
- Revogação imediata: apagar a linha da sessão.
- Sessão guardada no PostgreSQL (`spring-session-jdbc`), compartilhada entre instâncias.

## Configuração da sessão (application.yml)

```yaml
server:
  servlet:
    session:
      timeout: 30m              # expiração por inatividade
      cookie:
        http-only: true
        secure: true            # atrás de HTTPS; em dev local sem TLS usar perfil que desliga
        same-site: lax
        name: SESSION
spring:
  session:
    jdbc:
      initialize-schema: never  # tabelas criadas por migração Flyway
      cleanup-cron: "0 */10 * * * *"   # remove sessões expiradas
```

- Em perfil `dev` sem HTTPS, `secure: false` **só** nesse perfil; em `prod`, `true`.
- `same-site: lax` bloqueia o cookie em POST cross-site e ainda permite navegação normal.
  `strict` é mais duro; `none` exige `Secure` e quase nunca é necessário aqui.
- Atrás de proxy reverso: `server.forward-headers-strategy=framework` para o Spring enxergar
  `https`.

## Hash de senha

```java
@Bean
PasswordEncoder passwordEncoder() {
  return PasswordEncoderFactories.createDelegatingPasswordEncoder(); // bcrypt por padrão, prefixo {bcrypt}
}
```

- `DelegatingPasswordEncoder` grava o algoritmo no hash (`{bcrypt}...`), permitindo migrar
  depois (`upgradeEncoding`). Alternativa direta: `new BCryptPasswordEncoder()`.
- argon2id: `Argon2PasswordEncoder.defaultsForSpringSecurity_v5_8()` (exige a biblioteca
  BouncyCastle no classpath; conferir). bcrypt é suficiente e mais simples.
- bcrypt considera só os primeiros 72 bytes: limite o tamanho da senha na validação (ex.:
  máx. 72) e exija mínimo (ex.: 10 caracteres).
- Guarde só o hash (`senha_hash`), nunca a senha; nunca logue senha.

## Usuário e principal

```java
public record UsuarioAutenticado(long id, String email, String papel) {}
```

```java
@Service
class UsuarioDetailsService implements UserDetailsService {
  private final UsuarioRepository usuarios;
  UsuarioDetailsService(UsuarioRepository usuarios) { this.usuarios = usuarios; }

  @Override
  public UserDetails loadUserByUsername(String email) {
    Usuario u = usuarios.buscarPorEmail(email.toLowerCase(Locale.ROOT))
        .orElseThrow(() -> new UsernameNotFoundException("credenciais inválidas"));
    return new UsuarioLogado(u.id(), u.email(), u.senhaHash(), u.papel(), u.ativo());
  }
}
```

`UsuarioLogado` implementa `UserDetails` (e `Serializable`, pois vai para a sessão) expondo
`id()`. Para injetar `UsuarioAutenticado` no controller, use `@AuthenticationPrincipal
UsuarioLogado u` ou um `HandlerMethodArgumentResolver` próprio; mantenha um só estilo.

## SecurityFilterChain

```java
@Configuration
@EnableMethodSecurity
class SegurancaConfig {

  @Bean
  SecurityFilterChain api(HttpSecurity http) throws Exception {
    http
      .authorizeHttpRequests(a -> a
          .requestMatchers("/actuator/health/**").permitAll()
          .requestMatchers(HttpMethod.POST, "/api/auth/login").permitAll()
          .requestMatchers("/api/**").authenticated()
          .anyRequest().denyAll())                       // o que não foi liberado, nega
      .exceptionHandling(e -> e
          .authenticationEntryPoint(new HttpStatusEntryPoint(HttpStatus.UNAUTHORIZED))  // 401, sem redirect
          .accessDeniedHandler((req, res, ex) -> res.setStatus(HttpStatus.FORBIDDEN.value())))
      .csrf(c -> c
          .csrfTokenRepository(CookieCsrfTokenRepository.withHttpOnlyFalse())
          .csrfTokenRequestHandler(new CsrfTokenRequestAttributeHandler()))
      .addFilterAfter(new CsrfCookieFilter(), BasicAuthenticationFilter.class)
      .sessionManagement(s -> s.sessionFixation(f -> f.changeSessionId()))
      .formLogin(AbstractHttpConfigurer::disable)
      .httpBasic(AbstractHttpConfigurer::disable)
      .logout(AbstractHttpConfigurer::disable);          // logout é endpoint próprio
    return http.build();
  }

  @Bean
  SecurityContextRepository securityContextRepository() {
    return new HttpSessionSecurityContextRepository();
  }

  @Bean
  AuthenticationManager authenticationManager(AuthenticationConfiguration cfg) throws Exception {
    return cfg.getAuthenticationManager(); // usa UserDetailsService + PasswordEncoder dos beans
  }
}
```

- `denyAll` no final: esquecer uma rota **falha fechada**.
- Sem entry point explícito, API sem sessão responderia 403 em vez de 401, ou redirecionaria
  a uma página de login: sempre defina o 401.
- Para o corpo do 401/403 em Problem Details, escreva o JSON no entry point/handler (ou
  delegue ao `HandlerExceptionResolver`); o `ErrosHandler` não vê exceções de filtro.

## Login e logout (endpoint próprio)

```java
@RestController
@RequestMapping("/api/auth")
class AuthController {
  private final AuthenticationManager manager;
  private final SecurityContextRepository repo;

  AuthController(AuthenticationManager manager, SecurityContextRepository repo) {
    this.manager = manager;
    this.repo = repo;
  }

  record LoginRequest(@NotBlank @Email String email, @NotBlank @Size(max = 72) String senha) {}

  @PostMapping("/login")
  ResponseEntity<Void> login(@Valid @RequestBody LoginRequest req,
                             HttpServletRequest request, HttpServletResponse response) {
    Authentication auth;
    try {
      auth = manager.authenticate(
          UsernamePasswordAuthenticationToken.unauthenticated(req.email().toLowerCase(Locale.ROOT), req.senha()));
    } catch (AuthenticationException e) {
      // mesma resposta para usuário inexistente, senha errada e conta inativa
      throw new BadCredentialsException("credenciais inválidas");
    }
    if (request.getSession(false) != null) {
      request.changeSessionId();                 // evita fixação de sessão
    }
    SecurityContext ctx = SecurityContextHolder.createEmptyContext();
    ctx.setAuthentication(auth);
    SecurityContextHolder.setContext(ctx);
    repo.saveContext(ctx, request, response);    // grava na sessão (obrigatório no Security 6+)
    return ResponseEntity.noContent().build();
  }

  @PostMapping("/logout")
  ResponseEntity<Void> logout(HttpServletRequest request) {
    HttpSession s = request.getSession(false);
    if (s != null) s.invalidate();               // apaga a linha em SPRING_SESSION
    SecurityContextHolder.clearContext();
    return ResponseEntity.noContent().build();
  }
}
```

- Falha de login: `BadCredentialsException` tratada no `ErrosHandler` como 401 com a **mesma**
  mensagem sempre ("e-mail ou senha incorretos"); não revela se o e-mail existe.
- Limite tentativas (força bruta): contador por e-mail/IP em tabela ou cache simples, com
  bloqueio temporário e resposta 429; em aplicação pequena, pode-se registrar como achado se ausente.
- Logout é `POST` (nunca `GET`) e protegido por CSRF.
- Rotacione o ID da sessão no login (`changeSessionId`) e mantenha a expiração por inatividade.

## CSRF

Cookie de sessão é enviado automaticamente pelo navegador, então a API precisa de defesa em
camadas:

1. `SameSite=Lax` no cookie de sessão (acima);
2. token CSRF em métodos que mudam estado (`POST/PUT/PATCH/DELETE`), padrão do Spring
   Security: cookie `XSRF-TOKEN` legível pelo JS, devolvido pelo front no cabeçalho
   `X-XSRF-TOKEN`;
3. CORS restritivo (`cabecalhos-cors` em `revisao-seguranca`): só a origem do front.

O token CSRF é carregado de forma tardia (deferred); sem forçar a leitura, o cookie
`XSRF-TOKEN` não é emitido. O filtro abaixo força a emissão:

```java
final class CsrfCookieFilter extends OncePerRequestFilter {
  @Override
  protected void doFilterInternal(HttpServletRequest req, HttpServletResponse res, FilterChain chain)
      throws ServletException, IOException {
    CsrfToken token = (CsrfToken) req.getAttribute(CsrfToken.class.getName());
    if (token != null) {
      token.getToken();             // força a escrita do cookie XSRF-TOKEN
    }
    chain.doFilter(req, res);
  }
}
```

- O front (Vue) lê o cookie `XSRF-TOKEN` e envia `X-XSRF-TOKEN` em requisições que mudam
  estado (axios faz isso sozinho com o mesmo nome; com `fetch`, manualmente). Ver a skill
  `frontend-vue`, cliente HTTP.
- A rota de login pode ser isenta de CSRF (não há sessão ainda) com
  `csrf.ignoringRequestMatchers("/api/auth/login")`; mitigue com `SameSite` e CORS. Se o
  projeto preferir não isentar, o front faz um `GET` anterior para receber o cookie.
- Tokens CSRF mais fortes contra BREACH: ver o `SpaCsrfTokenRequestHandler` na documentação
  do Spring Security (conferir); em aplicação pequena o handler simples basta.
- Em API puramente com `Authorization: Bearer` (sem cookie) o CSRF não se aplica; não é o caso
  aqui.

## Autorização: papel e dono do recurso

Papel por rota não basta: A não pode cancelar a reserva de B (IDOR). Checar **no serviço**,
perto do dado:

```java
@Transactional
public void cancelar(long reservaId, UsuarioAutenticado u) {
  Reserva r = reservas.buscar(reservaId)
      .orElseThrow(() -> new NaoEncontradoException("reserva " + reservaId));
  boolean dono = r.usuarioId() == u.id();
  boolean admin = "ADMIN".equals(u.papel());
  if (!dono && !admin) {
    throw new NaoEncontradoException("reserva " + reservaId); // 404: não revela que existe
  }
  reservas.remover(reservaId);
}
```

- Decida e documente (ADR/openapi): 403 ou 404 para recurso de outro usuário. 404 evita
  enumerar IDs.
- Alternativa declarativa, quando a regra é reutilizada:
  `@PreAuthorize("@acessoReserva.podeCancelar(#id, authentication)")` com
  `@EnableMethodSecurity`; o bean `acessoReserva` consulta o dono. Mantenha a regra em um
  lugar só.
- Papéis simples (`USER`, `ADMIN`) numa coluna/tabela; `hasRole("ADMIN")` nas rotas
  administrativas **e** checagem de dono nas demais.
- Listagens filtram por dono no SQL (`WHERE usuario_id = :u`), não no front.
- Mass assignment: o request não tem `usuarioId`; o dono vem **sempre** da sessão.

## Cabeçalhos e outras proteções

- Spring Security já envia `X-Content-Type-Options`, `Cache-Control` (sem cache em respostas
  autenticadas) e `X-Frame-Options`. Adicione CSP/HSTS no proxy ou via
  `http.headers(...)` conforme `cabecalhos-cors`.
- CORS só se front e API tiverem origens diferentes; com proxy reverso mesma origem, nem
  precisa. Nunca `allowedOrigins("*")` com credenciais.
- Segredos (senha do banco) por variável de ambiente; nunca em `application.yml` versionado.
- Logs: nunca senha, cookie de sessão, token CSRF ou e-mail completo em nível `info`.

## Erros comuns

| Erro | Correção |
|---|---|
| Login que só chama `authenticate` e não grava o contexto | `SecurityContextRepository.saveContext` |
| Não rotacionar a sessão no login | `changeSessionId()` |
| Mensagens diferentes para "usuário não existe" e "senha errada" | Mesma mensagem, mesmo 401 |
| 403 no lugar de 401 sem sessão | `HttpStatusEntryPoint(UNAUTHORIZED)` |
| Desligar CSRF "porque é API" com cookie de sessão | CSRF ligado + `SameSite` |
| Só `hasRole` na rota | Checar dono do recurso no serviço |
| Hash com SHA-256 | `PasswordEncoder` (bcrypt/argon2) |
| `permitAll` por padrão com exceções | `denyAll` no fim e liberar o mínimo |
| Cookie sem `Secure` em produção | `secure: true` no perfil `prod` |
