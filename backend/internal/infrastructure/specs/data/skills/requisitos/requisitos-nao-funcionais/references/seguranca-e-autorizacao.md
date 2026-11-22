# RNF de segurança e autorização

Categoria (`categoria`): `seguranca`. Objetivo: transformar "seja seguro" em condições objetivas
e verificáveis, que a Arquitetura implementa e a Segurança audita.

Referências de conteúdo (cite pelo nome): **OWASP Top 10**, **OWASP ASVS**, **OWASP Cheat Sheet
Series**. Não fixe a edição sem conferir a vigente.

## 1. Perguntas-chave

**Identidade e autenticação**
- Quem entra? Contas criadas por quem? Há auto-cadastro?
- Login próprio ou integração com diretório/SSO da empresa?
- Há segundo fator? Para quais papéis (ao menos administrador)?
- Como recupera acesso (esqueci a senha)? Quem pode redefinir?
- Quantas tentativas erradas antes de bloquear ou atrasar?

**Sessão**
- Quanto tempo uma sessão vive? Expira por inatividade? Logout invalida de verdade?

**Autorização**
- Quais papéis existem e o que cada um pode? Alguém pode agir sobre dado de outro?
- A regra é verificada onde? (sempre no servidor; esconder botão não é autorização)

**Dados e comunicação**
- Há dado sensível (financeiro, pessoal, credencial)? Trafega só criptografado?
- O que é registrado em log de auditoria (quem fez o quê, quando)?

**Entrada e saída**
- Há upload de arquivo? Campo livre exibido a outros usuários?

**Superfície**
- Há API pública? Integrações externas? Limites de uso?

## 2. Métricas e verificações típicas

| Tema | Métrica (condição objetiva) | Como verificar |
|---|---|---|
| Autenticação obrigatória | 100% das rotas da API, exceto login e health check, retornam 401 sem credencial válida | Teste automatizado que percorre a lista de rotas sem token |
| Autorização por papel | Cada operação restrita retorna 403 para papel não autorizado (matriz papel x operação, 100% das células testadas) | Teste automatizado a partir da matriz |
| Autorização por objeto (IDOR) | Funcionário que tenta ler/alterar recurso de outro recebe 403 ou 404 | Teste automatizado com dois usuários |
| Senha armazenada | Apenas hash adaptativo com salt (argon2id, bcrypt ou scrypt); nenhuma senha em texto claro no banco nem nos logs | Revisão de código + consulta ao banco de teste |
| Política de senha | Mínimo de comprimento definido, sem regra de composição arbitrária, checagem contra senhas comuns, quando o cliente tiver política própria usar a dele | Teste automatizado de validação |
| Bloqueio de força bruta | Após N tentativas inválidas, atraso progressivo ou bloqueio temporário (valor definido) | Teste automatizado |
| Sessão | Expira após N min de inatividade; cookie com `HttpOnly`, `Secure`, `SameSite` | Teste automatizado + inspeção de cabeçalhos |
| Logout | Token/sessão invalidados no servidor; reuso retorna 401 | Teste automatizado |
| Transporte | Tráfego só por HTTPS (TLS 1.2 ou superior); HTTP redireciona; HSTS ativo em produção | Varredura de configuração |
| Cabeçalhos | `Content-Security-Policy`, `X-Content-Type-Options: nosniff`, política de frames (`frame-ancestors`) e `Referrer-Policy` presentes | Varredura (ex.: OWASP ZAP baseline) |
| Injeção | Nenhuma consulta SQL montada por concatenação; entradas validadas por esquema | Revisão de código + análise estática (ex.: Opengrep) + teste com payloads |
| XSS | Saída escapada por padrão; nenhum `innerHTML` com dado do usuário sem sanitização | Revisão + varredura dinâmica |
| CSRF | Operações que alteram estado protegidas (token ou `SameSite` + verificação de origem) | Teste automatizado |
| Segredos | Nenhuma credencial, chave ou token no repositório; configuração por ambiente | Varredura de segredos (ex.: gitleaks) |
| Dependências | Nenhuma vulnerabilidade conhecida de severidade alta/crítica nas dependências no momento do release | Varredura de dependências (ex.: Trivy, OSV-Scanner) |
| Erros | Respostas de erro não expõem stack trace, SQL nem caminhos internos | Teste automatizado de erro 500 |
| Auditoria | Operações administrativas (criar sala, cancelar reserva alheia) registram autor, ação, alvo e instante | Teste automatizado + consulta ao log |
| Limite de taxa | Endpoints de login e escrita limitam requisições por origem (valor definido) | Teste de carga/script |
| Upload (se houver) | Tipos e tamanho máximo restritos; arquivo servido sem execução | Teste automatizado |

## 3. Matriz de autorização (técnica central)

Antes de escrever os RNF de autorização, monte a matriz **papel x operação** a partir das US.
Cada célula é "permite", "nega" ou "só o próprio".

| Operação | Anônimo | Funcionário | Administrador |
|---|---|---|---|
| Ver salas e disponibilidade | nega | permite | permite |
| Criar reserva | nega | permite | permite |
| Cancelar reserva | nega | só a própria | qualquer |
| Cadastrar/alterar sala | nega | nega | permite |
| Ver lista de reservas de todos | nega | nega ou só leitura (decisão do cliente) | permite |

Cada célula "nega" ou "só o próprio" gera um teste negativo. O RNF diz: "toda operação segue a
matriz do Anexo; 100% das células negativas testadas".

## 4. Exemplos completos

Bom:

```yaml
categoria: seguranca
requisito: "a API aplica a matriz papel x operação no servidor; funcionário não executa operações de administrador nem age sobre recurso de outro funcionário"
metrica: "100% das células \"nega\" e \"só o próprio\" da matriz retornam 403 (ou 404 para ocultar a existência); 0 operações administrativas acessíveis a funcionário"
verificacao:
  tipo: teste_automatizado
  detalhe: "teste automatizado gerado a partir da matriz, com um usuário de cada papel"
prioridade: must
origem:
  tipo: boa_pratica
  detalhe: "\"podem cancelar as próprias\" / \"Administradores ... podem cancelar qualquer reserva\""
```

Ruim:

```yaml
requisito: "o sistema deve ser seguro e proteger os dados dos usuários"
metrica: "nível de segurança alto"
verificacao:
  tipo: teste_automatizado
  detalhe: "auditoria"
```

Por quê: nenhuma condição objetiva, "alto" não mede, "auditoria" não diz o quê nem como.

## 5. Heurísticas de especialista

- **Autorização no servidor, sempre.** RNF que menciona "ocultar o botão" está errado.
- **Teste de negação** é metade do valor: o que a matriz proíbe precisa de teste.
- **Quebra de controle de acesso por objeto** (IDOR) é a falha mais comum: trate "recurso de
  outro usuário" como categoria própria de teste.
- **404 versus 403:** se a existência do recurso é sensível, preferir 404; senão, 403. Decisão
  consciente, não acaso.
- **Mensagem de login genérica** ("usuário ou senha inválidos"), sem revelar qual falhou.
- **Resposta de recuperação de senha** idêntica para e-mail existente ou não (evita enumeração).
- **Política de senha:** orientação atual de organismos como NIST favorece comprimento e
  verificação contra senhas vazadas em vez de regras de composição; adote a política do
  cliente se existir, senão proponha com origem "boa prática".
- **Menor privilégio:** conta de banco da aplicação sem permissão de DDL; admin não é
  superusuário do banco.
- **Registro para auditoria** é diferente de log de depuração: quem, o quê, quando, resultado.
- **Segurança é Must por padrão** quando há login; o que varia é a profundidade.

## 6. Anti-padrões

- "Seguir as boas práticas do OWASP": não é verificável. Cite o item e a condição.
- Lista de cabeçalhos sem forma de verificação.
- Criptografia "própria" ou "ofuscação" como requisito.
- Aceitar "senha forte" sem definir.
- Exigir mudança periódica de senha sem motivo (prática desencorajada), a menos que o cliente
  imponha.
- Tratar autenticação como autorização.
- Deixar toda segurança para a etapa de Segurança: os RNF precisam existir antes para que a
  Arquitetura e o Dev construam.

## 7. Origem típica

| Origem | Exemplo |
|---|---|
| cliente | "Funcionários entram com login" |
| lei | LGPD, art. 46 (medidas de segurança) |
| boa prática | OWASP ASVS/Top 10, orientações de senha |
| norma interna | Política de segurança da empresa, se existir (perguntar) |

## 8. Checklist de revisão

- [ ] Matriz papel x operação montada; cada célula negativa tem teste previsto.
- [ ] Autenticação: rotas protegidas, política de senha, hash adaptativo, recuperação.
- [ ] Sessão: expiração e invalidação no logout.
- [ ] Transporte: HTTPS/TLS; cabeçalhos de segurança.
- [ ] Entrada/saída: injeção, XSS, CSRF tratados com condição verificável.
- [ ] Segredos e dependências com varredura.
- [ ] Auditoria de operações administrativas.
- [ ] Métricas são condições objetivas, não adjetivos.
- [ ] Verificação é executável (script, varredura, revisão listada).
