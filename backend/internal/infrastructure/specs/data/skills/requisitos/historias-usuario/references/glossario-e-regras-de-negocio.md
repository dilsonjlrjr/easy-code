# Glossário de domínio e regras de negócio

Dois insumos que evitam a maior fonte de retrabalho: o mesmo termo com dois significados e a
regra que só existe na cabeça do autor. O glossário vai no `glossario` da VISAO (`VISAO.md`); as regras vão em
`regras_negocio` de cada US (e no corpo da `VISAO.md` se forem transversais).

## 1. Glossário de domínio

### Para que serve

- Alinha agentes e cliente: todos usam o mesmo nome para a mesma coisa.
- Remove ambiguidade antes de virar CA (o que é "conflito"? o que é "reserva futura"?).
- Alimenta Arquitetura (entidades), Design (rótulos) e QA (dados de teste).

### Como construir

1. Reúna os substantivos da ideia (leitura 1 de `elicitacao-e-perguntas.md`).
2. Para cada termo, escreva a definição **operacional**: como distinguir um exemplo de um
   não-exemplo.
3. Marque sinônimos usados pelo cliente e escolha **um termo canônico**; os outros entram
   como "não usar".
4. Inclua estados e atributos relevantes quando fazem parte da regra.
5. Revise ao final: todo termo usado nas US está no glossário? Todo termo do glossário é usado?

### Formato

O glossário é o campo `glossario` da VISAO (artefato `docs/requisitos/VISAO.md`, esquema
`specs/dominio/artefatos/visao.schema.json`, molde em `agentes/templates/VISAO.md`; vale o
esquema). Cada item tem `termo` e `definicao` (ambos obrigatórios; ao menos 1 item). Sinônimos a
evitar e exemplo/não-exemplo entram dentro da `definicao`, ou na tabela de apoio do corpo.

```yaml
---
id: VISAO
tipo: VISAO
titulo: Visão do produto — Reserva de salas
autor: requirements-agent
onda: REQ-01
versao: 1
estado: rascunho
vinculos: []
historico:
  - {versao: 1, data: "2026-11-19", onda: REQ-01, descricao: criação}
problema: Reservas de sala por planilha geram conflito de horário entre funcionários.
atores:
  - nome: Funcionário
    quem_e: Pessoa da empresa que usa salas de reunião
    necessidade: Garantir o espaço da reunião sem conflito
objetivo: Permitir reservar salas sem sobreposição de horário.
fora_de_escopo:
  - Lembrete por e-mail
glossario:
  - termo: Reserva
    definicao: Compromisso de uso de uma sala por um funcionário em um intervalo de data e horário
  - termo: Conflito
    definicao: Intervalos de duas reservas ativas da mesma sala que se sobrepõem em mais de um instante
  - termo: Horário encostado
    definicao: Reserva cujo fim coincide com o início de outra na mesma sala; não é conflito
---
```

Tabela de apoio no corpo (opcional, nunca contradiz o YAML):

```markdown
| Termo | Definição | Não usar | Exemplo / não-exemplo |
|---|---|---|---|
| Sala | Espaço físico cadastrado, reservável, com nome único, capacidade e recursos | ambiente, local | "Sala Atlântico" é sala; "Auditório externo" não está cadastrado |
| Horário encostado | Reserva cujo fim coincide com o início de outra na mesma sala | adjacente | 10h–11h e 11h–12h: não conflitam |
```

### Regras de qualidade das definições

| Ruim | Por quê | Bom |
|---|---|---|
| "Reserva: quando alguém reserva uma sala" | Circular | "Compromisso de uso de uma sala por um funcionário em um intervalo definido" |
| "Usuário: quem usa o sistema" | Vago, esconde papéis | Termos separados: Funcionário, Administrador |
| "Conflito: reservas que colidem" | Não diz se encostar colide | Definir sobreposição com exemplo de limite |
| Definição com solução ("registro na tabela reservas") | Mistura implementação | Descrever o conceito de negócio |

### Termos que quase todo domínio web precisa definir

Papel/ator, entidade principal, estado ativo/inativo/cancelado, o que conta como "duplicado",
período/janela de tempo (fuso, inclusivo/exclusivo), "passado" e "futuro" (em relação a quê),
"propriedade" (o que é "minha"), unidade (minutos, reais, pessoas).

## 2. Regras de negócio

Regra de negócio = condição ou política do cliente que o sistema precisa respeitar,
independente de tela ou tecnologia.

### Tipos de regra

| Tipo | Exemplo | Onde costuma virar CA |
|---|---|---|
| Restrição de integridade | Não pode haver duas reservas ativas da mesma sala no mesmo horário | Conflito, concorrência |
| Autorização | Só administrador cadastra sala; funcionário cancela só as próprias | Negativo por papel |
| Derivação/cálculo | Capacidade restante = capacidade - participantes | Valor calculado |
| Estado/transição | Reserva cancelada não volta a ativa | Transições proibidas |
| Limite/faixa | Duração entre 30 min e 8 h | Valor-limite |
| Temporal | Reserva só no futuro; cancelamento até o início | Passado, limite exato |
| Unicidade | Nome de sala único | Duplicidade |
| Obrigatoriedade | Capacidade obrigatória e inteira positiva | Validação |

### Como extrair

1. **Caça a modais:** procure "não pode", "só", "deve", "sempre", "nunca", "cada", "no
   máximo", "pelo menos", "apenas", "exceto".
2. **Interrogue os substantivos:** para cada entidade, quais atributos são obrigatórios,
   únicos, limitados, imutáveis?
3. **Interrogue os verbos:** quem pode, quando pode, o que precisa ser verdade antes
   (pré-condição) e o que fica verdade depois (pós-condição)?
4. **Interrogue as bordas:** primeiro e último valor aceito, vazio, duplicado, simultâneo.
5. **Registre origem:** regra do cliente, suposição ou lei.

### Como escrever

- **Afirmação única, verificável, sem ambiguidade:** sujeito + condição + consequência.
- **Numerar** quando referenciada (RN-1, RN-2) na US, para o CA citar ("exercita RN-2").
  A numeração é local à US (`regras_negocio[].id`); não é um tipo de artefato.
- **Dar a unidade e a inclusão/exclusão** dos limites.
- **Uma regra por linha**.

Bom:

```yaml
regras_negocio:
  - {id: RN-1, descricao: Duas reservas ativas da mesma sala não podem ter intervalos que se sobreponham.}
  - {id: RN-2, descricao: Reservas encostadas (fim de uma = início da outra) não se sobrepõem.}
  - {id: RN-3, descricao: A duração da reserva é múltipla de 30 minutos, entre 30 min e 8 h.}
  - {id: RN-4, descricao: Reserva só pode ser criada para início no futuro.}
```

Ruim:

```markdown
- O sistema deve evitar problemas de horário.
- Reservas devem ser coerentes.
- Respeitar as regras da empresa.
```

Por quê: não dá para testar, não define "problema", "coerente" ou "regras".

### Regra versus CA versus RNF

| Pergunta | Se sim |
|---|---|
| É política do negócio, válida em qualquer implementação? | Regra de negócio |
| É um exemplo concreto da regra em ação? | CA |
| É uma qualidade do sistema (desempenho, segurança, acessibilidade)? | RNF |
| É escolha de implementação? | T ou ADR |

## 3. Tabelas de decisão (regras combinadas)

Para regras com 3 ou mais condições, uma tabela é mais clara que prosa e se converte
diretamente em CA.

| Papel | Reserva é do ator? | Reserva já iniciou? | Pode cancelar? |
|---|---|---|---|
| Funcionário | sim | não | Sim |
| Funcionário | sim | sim | Não |
| Funcionário | não | - | Não |
| Administrador | - | não | Sim |
| Administrador | - | sim | Não (a menos que o cliente diga o contrário: pergunta) |

Verifique: todas as combinações cobertas? Linhas contraditórias? Cada linha tem CA?
O traço "-" significa "irrelevante": agrupe só quando é realmente indiferente.

## 4. Estados e transições

Entidade com ciclo de vida (reserva: ativa -> cancelada; ativa -> concluída por passagem do
tempo) merece uma lista de transições permitidas. Tudo que não está na lista é proibido.

| De | Para | Quem | Condição |
|---|---|---|---|
| (nova) | ativa | Funcionário/Administrador | RN-1..RN-4 |
| ativa | cancelada | dono ou Administrador | antes do início |
| ativa | concluída | passagem do tempo | fim < agora |

Cada transição permitida = CA positivo; cada proibida relevante = CA negativo.

## 5. Dados, privacidade e regras

Quando uma regra envolve dado pessoal (quem vê o nome de quem reservou), registre-a
como regra e **vincule o RNF** de privacidade correspondente em `rnf` da US. A
decisão de visibilidade é do cliente: pergunte ou assuma e registre.

## 6. Regras ocultas: onde elas se escondem

| Esconderijo | Como achar |
|---|---|
| "Óbvio" | Pergunte "e se não for assim?" |
| Exceções do cliente | "Sempre?" "Sem exceção?" |
| Tempo | Fuso, horário comercial, feriados, virada de dia |
| Identidade | O que é "o mesmo"? E-mail igual com maiúscula diferente? |
| Remoção | Apagar de verdade ou manter histórico? |
| Concorrência | "E se dois fizerem ao mesmo tempo?" |

## 7. Anti-padrões

- Regra embutida só no CA (some quando o CA muda): promova à seção de regras.
- Regra duplicada em várias US com redação diferente: centralize no corpo da `VISAO.md` ou numere e
  referencie.
- Termo novo no CA sem entrar no glossário.
- Definição que usa o próprio termo.
- Regra do tipo desejo ("o sistema deve ser consistente").
- Sinônimos soltos ("agendar" numa US, "reservar" na outra).

## 8. Perguntas-chave

1. Qual é a regra que, se esquecida, gera dano financeiro, legal ou de confiança?
2. Existe exceção? Quem decide?
3. O limite é inclusivo ou exclusivo?
4. O que acontece com o que já existe quando a regra mudar?
5. Dois termos diferentes estão significando a mesma coisa?

## 9. Checklist

- [ ] Todo termo de domínio das US está no glossário com definição operacional.
- [ ] Um termo canônico por conceito; sinônimos listados como "não usar".
- [ ] Cada regra é afirmação verificável, com unidade e limites inclusivos/exclusivos.
- [ ] Cada regra é exercitada por ao menos um CA (rastreável por RN-n).
- [ ] Regras de autorização estão explícitas por papel.
- [ ] Ciclos de vida têm transições permitidas listadas.
- [ ] Regra sobre dado pessoal está vinculada a RNF de privacidade.
