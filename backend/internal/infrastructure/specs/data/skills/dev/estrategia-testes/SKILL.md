---
name: estrategia-testes
description: Como escolher o nível de cada teste (unitário, integração, contrato, e2e), praticar TDD sem cerimônia, escrever testes unitários determinísticos, usar dublês só quando valem (banco real via Testcontainers), testar API e banco com isolamento, validar contrato contra o OpenAPI, cobrir concorrência, medir cobertura com sensatez, escrever teste de regressão para cada achado (ACH) e manter a suíte rápida e estável. Agnóstica de linguagem. Use no Dev ao implementar uma T, na onda de retrabalho e no QA ao executar/automatizar CT.
---

# Estratégia de testes

Teste existe para **permitir mudar o código sem medo**, não para atingir número. Uma boa suíte
é pequena, rápida, falha por um motivo só e quebra quando o comportamento quebra, nunca quando
a implementação é reorganizada. Vale para qualquer aplicação web; os exemplos usam reserva de
salas apenas como ilustração e mostram a ideia em mais de uma linguagem.

## Princípios

1. **Teste comportamento observável**, não implementação. Se refatorar sem mudar comportamento
   quebra teste, o teste está acoplado demais.
2. **Nível mais baixo que prova o comportamento.** Regra pura: unitário. SQL, constraint,
   transação, contrato HTTP: integração com banco real. Jornada do usuário: e2e (poucos).
3. **Banco é parte do sistema.** Não simule o que você quer provar. Constraint de sobreposição,
   índice único e transação só existem no PostgreSQL de verdade.
4. **Determinismo antes de tudo.** Tempo, aleatoriedade, ordem e rede são controlados. Teste que
   às vezes falha é teste quebrado (conta como falha até estabilizar).
5. **Vermelho de verdade.** Todo teste novo foi visto falhando pelo motivo certo antes de passar.
6. **Um motivo para falhar.** Um comportamento por teste, nome que diz qual.
7. **Custo proporcional ao risco.** Dinheiro, permissão, concorrência e dado pessoal recebem
   mais testes; getter, DTO e configuração, nenhum.
8. **Cada bug vira teste de regressão** antes da correção (achado ACH).

## Regras de decisão: qual nível?

| O que estou provando | Nível | Banco | Referência |
|---|---|---|---|
| Regra de negócio pura (cálculo, validação, estado) | Unitário | não | tdd-e-unitarios |
| Orquestração com dependência externa lenta/instável (e-mail, pagamento) | Unitário com dublê (fake/stub) na borda | não | dubles-e-isolamento |
| Query, constraint, migration, transação, mapeamento | Integração | PostgreSQL real | integracao-banco-api-contrato |
| Endpoint: status, corpo, autenticação, erro (Problem Details) | Integração da API | real | integracao-banco-api-contrato |
| Resposta bate com o `openapi.yaml` | Contrato | real | integracao-banco-api-contrato |
| Duas requisições disputando o mesmo recurso | Integração concorrente | real | concorrencia-cobertura-flakiness |
| Fluxo ponta a ponta no navegador | e2e (QA, `playwright-e2e`) | real | skill `playwright-e2e` |
| Componente Vue: renderização, evento, acessibilidade | Unitário de componente | não (API simulada) | skill `frontend-vue` |

Regra de bolso: se a falha do teste não aponta a linha suspeita em 1 minuto, ele está alto
demais para o que prova. Se ele precisa de 15 dublês para rodar, está baixo demais.

## Forma da suíte (pirâmide ou troféu?)

- **Backend com regra rica:** pirâmide. Muito unitário na regra, integração na borda com banco.
- **Backend CRUD fino ou front:** troféu. Poucos unitários, integração forte (API + banco real;
  componente com DOM), pouco e2e.
- Em ambos: **e2e só para as jornadas críticas**; o QA escolhe (skill `cenarios-teste`).
- Proporção alvo para aplicação pequena: regra no unitário, uma integração por endpoint/consulta
  relevante, e o resto fica com o QA. Não construa pirâmide de cerimônia.

Detalhe e como escolher: `references/piramide-e-escolha-nivel.md`.

## Fluxo de trabalho (Dev, por T)

1. Ler a T, o(s) CA/CT citados (`cobre`, `cts`) e `criterios_pronto`. Cada item de `criterios_pronto` precisa
   de um teste ou de uma verificação objetiva.
2. Escolher o nível de cada comportamento (tabela acima). Listar os casos em uma linha cada
   (feliz, limite, recusa, erro), podando os que não mudam decisão.
3. **Vermelho:** escrever o teste, rodar, ver falhar pelo motivo esperado (não por erro de
   compilação ou de setup).
4. **Verde:** o mínimo de código que passa. Sem generalizar antes da hora.
5. **Refatorar** com a suíte verde (skill `qualidade-codigo`).
6. Repetir por comportamento. Ao fim, rodar a suíte inteira, o lint e a checagem de tipos.
7. Registrar em `registro_dev` da T (`testes`, `desvios`) os testes criados e qualquer desvio; `situacao: concluida`.

### Onda de retrabalho (ACH)

1. Ler `evidencia` do ACH; reproduzir. O ciclo de `etapa_destino: implementacao` é interno (sem reabrir gate); o emissor revalida (`specs/dominio/achado.md`, seção 7).
2. Escrever **primeiro o teste que reproduz** o problema, no nível mais baixo que o captura.
   Ver vermelho.
3. Corrigir; ver verde; rodar tudo.
4. Commit `fix(ACH-NN): <resumo>` contendo teste e correção juntos.
5. O teste fica na suíte para sempre, nomeado pelo comportamento (não pelo número do achado
   apenas; o ACH vai em comentário curto ou no nome do commit).

### QA no modo EXECUÇÃO

- Ao automatizar um CT, usar o **`nivel` indicado no CT** (`unitario | integracao | e2e | nao_funcional`); descer de nível se um comportamento
  provado em e2e pode ser provado em integração.
- Dados do CT com valores literais, ambiente limpo, sem depender de outro CT.
- Falha reproduzida duas vezes é achado; intermitência não explicada é falha até estabilizar
  (`concorrencia-cobertura-flakiness`).

## Checklist de revisão de testes (autorrevisão do Dev, revisão do QA)

- [ ] Cada item de `criterios_pronto` da T tem teste ou verificação objetiva.
- [ ] Cada teste novo foi visto falhando antes de passar.
- [ ] Nome diz comportamento e condição, não nome de método.
- [ ] Estrutura Arrange-Act-Assert visível; um comportamento por teste.
- [ ] Sem `sleep` fixo, sem dependência de data/hora real, sem ordem entre testes.
- [ ] Banco e SQL testados com PostgreSQL real; dublê só nas bordas externas.
- [ ] Teste de recusa confere o estado final (nada gravado), não só o status.
- [ ] Autorização coberta (sem login, outro usuário, papel errado) onde há dado de terceiros.
- [ ] Regra de unicidade/vaga/saldo tem teste de concorrência ou risco registrado.
- [ ] Teste não repete a regra do código para calcular o esperado (valor calculado à mão).
- [ ] Sem segredo, sem URL de ambiente real, sem dado pessoal real.
- [ ] Suíte roda do zero com um comando documentado no `app/README.md`.

## Erros comuns

| Erro | Efeito | Correção |
|---|---|---|
| Mockar o repositório e declarar "testado" | Constraint, SQL e mapeamento nunca rodam | Integração com PostgreSQL real |
| Mock com `verify` de cada chamada interna | Refatorar quebra todos os testes | Afirmar resultado/estado, não conversa |
| Teste escrito depois e nunca visto falhar | Pode passar sem provar nada | Vermelho antes do verde |
| `sleep(2000)` para esperar | Lento e intermitente | Esperar a condição (polling com limite) ou controlar o relógio |
| Compartilhar dados entre testes | Ordem muda o resultado | Cada teste cria seu dado; isolamento por transação/schema |
| Cobertura de 90% sem afirmação | Número bonito, proteção nula | Revisar asserções; mutação como sinal |
| Testar getter/DTO/framework | Custo sem risco | Não testar |
| Teste de regressão só no e2e | Lento, falha cedo demais ou tarde demais | Nível mais baixo que reproduz |
| Teste que lê relógio/aleatório real | Falha uma vez por semana | Injetar relógio e semente |
| Suíte de 20 min | Ninguém roda antes do commit | Paralelizar, reaproveitar container, cortar e2e redundante |

## Índice dos references

| Arquivo | Ler quando |
|---|---|
| `references/piramide-e-escolha-nivel.md` | Para decidir a forma da suíte e o nível de um comportamento; organização de pastas e nomes |
| `references/tdd-e-unitarios.md` | Para praticar vermelho-verde-refatorar, saber quando TDD não vale e escrever unitário bom (AAA, nomes, tempo e aleatoriedade) |
| `references/dubles-e-isolamento.md` | Para escolher stub/fake/mock/spy e decidir quando NÃO mockar |
| `references/integracao-banco-api-contrato.md` | Para teste com PostgreSQL real, isolamento, dados, teste de API e contrato contra o OpenAPI |
| `references/concorrencia-cobertura-flakiness.md` | Para teste de concorrência, cobertura e mutação, regressão de ACH, velocidade e testes intermitentes |

Skills vizinhas: `cenarios-teste` e `playwright-e2e` (lado QA), `padroes-arquitetura-codigo`
(testabilidade do desenho), `modelagem-postgresql`, `ambiente-docker` (Testcontainers/Compose),
`qualidade-codigo` (refatoração segura), `frontend-vue` (testes de front).
