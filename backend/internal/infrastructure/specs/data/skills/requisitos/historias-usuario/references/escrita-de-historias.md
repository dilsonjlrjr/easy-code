# Escrita de histórias: INVEST, forma e divisão

Como redigir cada campo do artefato US (template `agentes/templates/US.md`, esquema `specs/dominio/artefatos/us.schema.json`; vale o esquema) e como dividir histórias que não cabem em uma Onda.

## 1. Forma dos campos

O template separa a frase clássica "Como <ator>, quero <objetivo>, para <benefício>" em três
campos obrigatórios. Cada um tem regra própria.

| Campo | Regra | Bom | Ruim (por quê) |
|---|---|---|---|
| Título | Verbo no infinitivo + objeto, até ~6 palavras | "Cancelar reserva" | "Cancelamento" (sem ação); "Tela de cancelamento" (fala de tela) |
| Ator | Papel concreto do cliente | "Administrador" | "Usuário" (vago); "Sistema" (não é ator de negócio, salvo rotina automática declarada) |
| Quero | Um objetivo, voz do ator | "cancelar uma reserva minha" | "cancelar e remarcar" (dois objetivos); "ter um botão cancelar" (solução) |
| Para | Benefício ao ator ou ao negócio | "liberar a sala para outra pessoa" | "para o sistema ficar atualizado" (benefício do sistema); "para poder cancelar" (circular) |
| Origem | Trecho literal entre aspas | "cancelam as próprias" | "boa prática" (não é origem); parafraseado sem aspas |
| `tem_interface` | `true` se o ator interage por tela; `false` para rotina automática, integração, job | sim | omitir o campo (é obrigatório) |

**Teste do "Para":** se você apagar o "Para" e a US continuar fazendo sentido idêntico, ele era
circular. Reescreva até explicar *por que o ator se importa*.

## 2. INVEST aplicado

| Letra | Pergunta de teste | Sinal de falha | Correção |
|---|---|---|---|
| **I**ndependente | Dá para construir e entregar sem outra US pronta? | "Depende da US-04" em quase todo CA | Reagrupar, ou dividir pela fatia vertical; dependência real fica registrada em `observacao` do BACKLOG |
| **N**egociável | O texto diz o que, deixando o como em aberto? | CA prescreve campo, botão, tecnologia | Remover solução; mover para Design/Arquitetura |
| **V**aliosa | O ator percebe o resultado? | US só entrega infraestrutura | Juntar à primeira US que usa; ou é T, não US |
| **E**stimável | Um dev/agente consegue dimensionar? | Perguntas bloqueantes abertas, "e outras coisas" | Responder perguntas; dividir |
| **S**mall | Cabe em uma Onda de Dev? | Mais de ~8 CA, mais de 1 fluxo | Dividir (seção 3) |
| **T**estável | Dá para escrever CT de cada CA? | CA com adjetivo vago | Reescrever com resultado observável |

Quando INVEST conflita (ex.: independência vs. valor), priorize **Valiosa** e **Testável**: são
as que o gate cobra.

### Ordem de dependência real

Toda aplicação com login tem uma dependência estrutural: "Entrar no sistema" precede qualquer
US autenticada. Registre isso uma vez no backlog (coluna "Depende de") em vez de repetir em
cada CA. Dependências aceitáveis: de infraestrutura funcional (login, cadastro base). Dependências
que indicam divisão errada: US-B só faz sentido com a metade de US-A.

## 3. Dividir histórias grandes

Sinais de que deve dividir:

- Mais de 8 CA, ou CA de fluxos muito distintos.
- "Gerenciar X" (CRUD inteiro) em uma história.
- O "Quero" tem "e", "ou", "também".
- Duas pessoas discordariam do que está pronto.
- Parte dela é Must e parte é Could.

**Regra de ouro:** divida em **fatias verticais** (cada pedaço atravessa tela, regra e dados e
entrega valor), nunca em camadas ("só o banco", "só a tela").

### Padrões de divisão (split)

| Padrão | Quando usar | Exemplo |
|---|---|---|
| **Por passo do fluxo** | Fluxo longo com etapas | "Reservar sala" -> "Buscar sala disponível" + "Confirmar reserva" |
| **Por operação (CRUD)** | "Gerenciar X" | "Gerenciar salas" -> Cadastrar, Alterar, Desativar, Listar |
| **Por regra de negócio** | Regras variadas na mesma US | "Reservar" -> reserva simples; depois "impedir conflito"; depois "limite de duração" |
| **Por papel** | Papéis com comportamentos diferentes | "Cancelar reserva" -> funcionário cancela a própria; administrador cancela qualquer |
| **Por variação de dado** | Entrada com muitas variantes | "Filtrar salas" -> por data e horário; por capacidade; por recurso |
| **Por caminho feliz x exceções** | Exceções são caras e podem vir depois | Caminho feliz primeiro; tratamento de erro de rede como US própria, se relevante |
| **Por qualidade** | Primeiro funciona, depois melhora (cuidado: RNF Must não pode ser adiado) | Reserva simples agora; confirmação por e-mail depois |
| **Por simples -> complexo** | Há versão mínima útil | Disponibilidade por dia inteiro -> por faixa horária |
| **Por dados/interface** | Uma tela de busca, vários critérios | Começar com um critério |
| **Spike** | Há incerteza real de requisito | Pergunta ao cliente, não uma US de pesquisa; pesquisa técnica é T da Arquitetura |

### Exemplo completo de divisão

US grande (ruim): "Gerenciar reservas: criar, ver, alterar, cancelar, com notificação e
histórico".

Divisão:

1. US "Reservar sala" (Must): criar reserva, com CA de conflito.
2. US "Ver minhas reservas futuras" (Must): lista do próprio ator.
3. US "Cancelar reserva própria" (Must).
4. US "Cancelar qualquer reserva" (Must, ator Administrador).
5. US "Alterar horário da reserva" (Could): pode ser cancelar e reservar de novo; fica
   registrada como Could com a justificativa.
6. Notificação e histórico: **não estavam na ideia**; viram perguntas ou Won't registrado.

Note que nenhuma fatia é "camada": cada uma entrega algo que o ator usa.

## 4. Quando NÃO é uma US

| Item | Destino |
|---|---|
| "Criar tabela", "configurar pipeline", "escolher framework" | T ou ADR (Arquitetura) |
| "O sistema deve responder em 500 ms" | RNF |
| "Botão azul, ícone de lixeira" | Design |
| "Só entra com login" (sem função própria) | RNF de segurança + CA de redirecionamento nas US afetadas |
| Rotina automática (expirar convites) | US com ator "Sistema" e `tem_interface: false`, só se tem valor de negócio descrito |

## 5. Exemplos completos

### Bom

```yaml
ator: Funcionário
quero: cancelar uma reserva minha
para: liberar a sala para outras pessoas quando a reunião não acontecer
prioridade: must
origem: '"fazem reservas e cancelam as próprias"'
tem_interface: true
```

Por que é bom: um ator, um objetivo, benefício real, origem literal, escopo "minha" já limita
o poder (a outra metade é a US do Administrador).

### Ruim

```yaml
ator: Usuário
quero: poder gerenciar reservas e receber e-mail
para: ter um sistema completo
prioridade: must
origem: boa prática
tem_interface: true
```

Problemas: ator vago; "gerenciar" esconde 4 operações; "e receber e-mail" é outro objetivo e não
veio do cliente; "Para" vazio de valor; origem não é trecho do cliente; tudo Must sem critério.

## 6. Atores e papéis: armadilhas

- **"Usuário" como ator único** esconde permissão. Pergunte: existe algo que alguém não pode?
- **Administrador herda tudo?** Não assuma. Se for "sim", escreva uma vez em regra de negócio
  ("Administrador pode fazer tudo que Funcionário faz") em vez de duplicar US.
- **Ator externo sem login** (visitante) exige US de acesso público e RNF de segurança.
- **Ator "Sistema"**: só para rotinas de negócio com resultado visível a alguém.

## 7. Nomear com o vocabulário do cliente

Use o termo que o cliente usou ("reserva", não "booking", "agendamento" se ele disse reserva).
Um conceito = um nome em todos os artefatos. Variações entram no glossário como "não usar".

## 8. Heurísticas de especialista

- Se você não consegue escrever o "Então" sem inventar, falta pergunta.
- Se o título precisa de "e", são duas US.
- Se o CA diz "o sistema deve", reescreva em termos do que o ator **observa**.
- Cada US deve poder ser demonstrada em 2 minutos a um cliente.
- Prefira 8 US enxutas a 4 enormes: o gate mede cobertura por US.
- Uma US sem CA negativo é quase sempre uma US com bug escondido.
- O número de CA é um sinal: 1 = provavelmente superficial; 3–6 = saudável; >8 = divida.

## 9. Checklist de revisão da escrita

- [ ] Título: verbo no infinitivo, sem "e", sem termo técnico.
- [ ] Ator: papel concreto, existente no glossário.
- [ ] Quero: um único objetivo, na voz do ator.
- [ ] Para: benefício real, não circular.
- [ ] Origem: aspas, trecho existente no texto do cliente.
- [ ] Sem tecnologia, tabela, endpoint, tela.
- [ ] INVEST passou; se não, divisão aplicada com padrão nomeado.
- [ ] `tem_interface` coerente (rotina automática = `false`).
