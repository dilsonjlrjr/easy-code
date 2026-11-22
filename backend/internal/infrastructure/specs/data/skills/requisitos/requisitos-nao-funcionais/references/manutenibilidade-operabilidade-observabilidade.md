# RNF de manutenibilidade, operabilidade e observabilidade

Categoria (`categoria`): `manutenibilidade` (cobre testabilidade, operabilidade e observabilidade
operacional). São RNF para o **time e o operador**, não para o usuário final, mas determinam
quanto custa mudar e operar o sistema. Em um processo com agentes e Ondas, são ainda mais
importantes: o agente seguinte precisa **rodar e testar** o que o anterior entregou.

## 1. Perguntas-chave

**Construir e rodar**
- Como o ambiente local sobe? Quantos comandos? Que pré-requisitos?
- Como roda a suíte de testes? Quanto tempo leva?
- Como é a configuração (variáveis de ambiente, arquivos)? Segredos ficam fora do repositório?
- Existe ambiente de teste/homologação? Como o banco é populado?

**Operar**
- Como saber se está saudável? Quem é avisado quando falha?
- Como investigar um erro reportado (logs correlacionados por requisição)?
- Como se faz uma atualização e um retorno (rollback)?
- Como as migrações de banco são aplicadas?

**Evoluir**
- Quem vai mantê-lo? Qual conhecimento mínimo?
- Que partes tendem a mudar (regras de negócio, tela)?
- Há padrão de código/estilo obrigatório da empresa?

## 2. Subtemas, métricas e verificação

### Construção, execução e testes (testabilidade)

| Tema | Métrica | Verificação |
|---|---|---|
| Subir o ambiente | Ambiente local completo (app + banco) sobe com **um comando** a partir de clone limpo | Executar em máquina limpa/CI e registrar |
| Rodar testes | Todos os testes rodam com **um comando**; falha retorna código diferente de zero | Execução em CI |
| Tempo da suíte | Testes unitários <= 2 min; suíte completa <= 10 min | Medição |
| Cobertura de regras | Cada CA tem ao menos um teste automatizado rastreável (CT) | Matriz CA x CT |
| Cobertura de código | Meta numérica opcional (ex.: >= 70% das linhas de regra de negócio) | Relatório de cobertura; meta de piso, não de qualidade |
| Dados de teste | Script de seed reproduzível gera dados coerentes | Executar o seed |
| Determinismo | Testes não dependem de hora real nem de ordem | Executar em ordem aleatória / relógio injetável |
| Pipeline | Integração contínua executa build, testes e análises a cada mudança | Presença e execução do pipeline |

### Qualidade do código e dependências

| Tema | Métrica | Verificação |
|---|---|---|
| Análise estática | 0 erros do analisador/linter configurado | Execução em CI |
| Formatação | Formatador automático aplicado; diff de estilo vazio | Verificação em CI |
| Complexidade | Funções com complexidade ciclomática <= 10 (ou limite acordado), com exceções justificadas | Ferramenta de análise |
| Dependências | Versões fixadas por arquivo de bloqueio (lockfile); atualização de segurança em até N dias | Revisão do lockfile + varredura |
| Camadas | Dependências entre módulos respeitam a arquitetura (sem acesso do controlador ao banco) | Teste de arquitetura/regra de lint |
| Documentação | README com como subir, testar e configurar; contrato da API versionado (OpenAPI) | Revisão |

### Configuração e implantação (operabilidade)

| Tema | Métrica | Verificação |
|---|---|---|
| Configuração | Toda configuração por variável de ambiente ou arquivo externo; nenhum segredo no repositório | Varredura de segredos + revisão |
| Ambientes | Mesma imagem/artefato roda em teste e produção, mudando só a configuração | Revisão do pipeline |
| Migrações | Mudanças de esquema versionadas e aplicadas automaticamente; reaplicar não quebra | Teste de migração em banco vazio e em banco com dados |
| Implantação | Implantação por comando único/pipeline; retorno à versão anterior em até X min | Execução em ambiente de teste |
| Dados iniciais | Criação do primeiro administrador por procedimento documentado, sem senha padrão em produção | Revisão |

### Observabilidade

| Tema | Métrica | Verificação |
|---|---|---|
| Logs estruturados | Logs em formato estruturado (JSON), com nível, instante, identificador de requisição | Inspeção de amostra |
| Correlação | Todo log de uma requisição carrega o mesmo identificador de correlação, também devolvido ao cliente em cabeçalho | Teste automatizado |
| Conteúdo seguro | Sem senha, token ou dado pessoal desnecessário nos logs (ver privacidade) | Teste com valores sentinela |
| Níveis | Erros inesperados em nível erro; regras de negócio rejeitadas (conflito) em nível informativo | Revisão |
| Métricas | Contadores e tempos por rota expostos (taxa de requisições, erros, latência) | Chamada ao endpoint de métricas |
| Health check | Endpoint de saúde distingue "vivo" de "pronto" (dependências, banco) | Teste automatizado |
| Alertas | Alerta quando taxa de erro 5xx > X% por Y min (se houver operação real) | Revisão da regra de alerta |
| Rastreamento | (opcional) Rastros distribuídos para chamadas entre serviços | Revisão |

Ferramentas open source de referência: OpenTelemetry (instrumentação), Prometheus (métricas),
Grafana (painéis), Loki ou OpenSearch (logs). **Não são requisito da aplicação pequena**: o RNF descreve
o comportamento; a escolha é da Arquitetura.

## 3. Exemplos completos

Bom:

```yaml
categoria: manutenibilidade
requisito: "qualquer pessoa (ou agente) consegue subir o sistema e rodar os testes a partir de um clone limpo"
metrica: "ambiente completo (aplicação e banco) sobe com um comando; testes automatizados rodam com um comando, em até 10 min, com código de saída diferente de zero em falha; README descreve pré-requisitos e configuração"
verificacao:
  tipo: teste_automatizado
  detalhe: "execução dos dois comandos em ambiente limpo (CI) a cada release"
prioridade: must
origem:
  tipo: boa_pratica
  detalhe: "boa prática"
```

Bom (observabilidade):

```yaml
categoria: manutenibilidade
requisito: "cada requisição pode ser rastreada nos logs de ponta a ponta sem expor dados sensíveis"
metrica: "100% das linhas de log de uma requisição têm o mesmo `request_id`, devolvido no cabeçalho da resposta; 0 ocorrências de senha, token ou e-mail em amostra de logs após executar a suíte de testes"
verificacao:
  tipo: teste_automatizado
  detalhe: "teste automatizado com valores sentinela e busca nos logs"
prioridade: should
origem:
  tipo: boa_pratica
  detalhe: "boa prática; LGPD (minimização em logs)"
```

Ruim:

```yaml
requisito: "código limpo e fácil de manter, com boa observabilidade"
metrica: "alta qualidade de código"
verificacao:
  tipo: teste_automatizado
  detalhe: "code review"
```

Por quê: nenhuma condição objetiva; "alta qualidade" não mede.

## 4. Relação com o processo de Ondas

- O agente de Dev e o de QA dependem de **um comando** para subir e testar: sem isso, cada Onda
  perde tempo e gera Achado de ambiente.
- **Rastreabilidade US -> CT -> teste** (`US/CA <- CT <- resultado`) é um RNF de processo:
  declare "cada CA tem teste identificado pelo id".
- O RNF de contrato (OpenAPI versionado) liga Arquitetura, Design e Dev.

## 5. Heurísticas de especialista

- **Um comando** para subir e um para testar: o RNF de manutenibilidade de melhor retorno.
- **Medir complexidade e cobertura** como piso, nunca como meta de qualidade em si
  (cobertura 100% sem asserções não protege nada).
- **Configuração fora do código** é pré-requisito de segurança e de implantação.
- **Observabilidade proporcional:** projeto interno precisa de logs com correlação e health
  check; painéis e alertas só com operação real.
- **Migração reversível ou ao menos reaplicável** evita implantação manual.
- **Documentação mínima e verificada:** o README é testado seguindo-o em máquina limpa.
- **Relógio injetável** torna testes de regra temporal determinísticos (reservas no passado).

## 6. Anti-padrões

- "Código limpo", "boas práticas", "fácil de manter" sem número.
- Cobertura de código como meta única.
- Observabilidade pesada para app de 20 usuários, ou nenhuma para app crítico.
- Logs não estruturados, sem correlação.
- Segredos em arquivo versionado "só no começo".
- Testes que dependem de ordem, de hora real ou de dado deixado por outro teste.
- Ambiente que só funciona na máquina de quem escreveu.

## 7. Checklist

- [ ] Subir ambiente e rodar testes em um comando cada, verificado em máquina limpa.
- [ ] Configuração externa; nenhum segredo no repositório (varredura).
- [ ] Análise estática, formatação e testes em pipeline.
- [ ] Rastreabilidade CA -> teste definida.
- [ ] Logs estruturados com correlação e sem dado sensível.
- [ ] Health check com distinção vivo/pronto.
- [ ] Migrações versionadas e reaplicáveis.
- [ ] Métricas objetivas; nada de adjetivo.
