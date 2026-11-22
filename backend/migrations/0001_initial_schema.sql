-- =============================================================================
-- Easy Code — esquema de referência do PostgreSQL da plataforma (passo E5, onda DBA-01)
-- =============================================================================
-- Natureza: DDL de referência, comentado, e fonte da primeira migração: project/backend/migrations/
-- 0001_initial_schema.sql é este arquivo (mesmo conteúdo). As migrações seguintes são arquivos SQL numerados,
-- aplicados em ordem pelo serviço de execução única `migrar` e registrados em schema_migrations (ADR-18 v2,
-- PERSIST seção 3); este arquivo passa a ser o retrato do esquema após a última migração. Executa em
-- PostgreSQL 17 limpo (testado em 17.6).
-- Versão 2 (2026-11-20, onda DBA-01): realinhado ao domínio após D-23 a D-35 de specs/dominio/README.md.
-- Versão 3 (2026-11-20, onda DBA-01): correções da revisão cruzada (onda 8): decisões humanas de gate e
--   achado síncronas pela api (A1); fila comandos com target_type/target_id/payload/idempotência e comando da
--   organização (A2, A4); idempotency_keys (A3); chave de provedor testing → ativa | descartada (A4);
--   id do projeto UUID opaco (A5); itens numerados da pergunta (A6); checkpoint apos e execução no sandbox
--   (M1, M6); correção de vigência futura de preço e fonte (M7, M8); project_attachments (M9, US-03/CA-4);
--   conclusão do projeto (US-52); regras de segurança com reflexo no banco (origem web, papel recente,
--   segregação de funções, skill sem colisão de nome com nativa).
-- Versão 5 (2026-11-21, onda DBA-01): decisão do dono sobre o banco (ADR-18 v2): PK só inteira (bigint identity),
--   public_id uuid nas entidades expostas, FKs na PK inteira; partições mensais por RANGE com partição padrão e
--   create_monthly_partitions em eventos, audit_records, model_calls, tool_executions e wave_checkpoints.
--   Este DDL é também a primeira migração (project/backend/migrations/0001_initial_schema.sql).
-- Versão 4 (2026-11-21, onda DBA-01): decisões do dono sobre as pendências das ESP (PENDENCIAS.md): achado
--   awaiting_scope e a decisão de mudança de escopo no escalonamento (PD-06); versão de artefato com autor
--   humano e agente revisor, anexos da versão (PD-03, PL-24); onda edit_review e tarefa (PD-03, PL-20); project_constraints
--   versionada (PL-13); comandos edit_artifact e origem só web, resposta de pergunta só web (cliente local
--   somente leitura, PD-07); resumo de chamada (PC-09); origem da dúvida no item de pergunta (PL-29);
--   external_event_id na auditoria para a trilha de login do Keycloak (PL-36); sem autorizar_excedente (PD-02).
-- Versão 6 (2026-11-21, onda DBA-01): decisão do dono (ADR-21): todo identificador de código em inglês
--   (docs/arquitetura/glossario-codigo.md é a fonte dos nomes); as sete tabelas de chave natural composta com
--   texto ganham PK inteira (id bigint identity) e a chave antiga vira UNIQUE; as FKs para stages passam a usar
--   stage_id (FK composta (stage_id, project_id, stage) para stages (id, project_id, stage), que mantém o texto
--   da etapa e o inteiro coerentes). Os comentários seguem em pt-BR.
--
-- Fontes:
--   - specs/dominio/estados/estados.yaml (sha256 27a3a8e21db426a7…, lido em 2026-11-21):
--     estados (CHECK) e transições (tabela state_transitions + gatilhos de transição), 8 máquinas.
--   - specs/dominio/eventos/ (envelope CloudEvents, sequencia por projeto ou pela organização, outbox).
--   - specs/dominio/governanca/ (gates, checagens, políticas, retenção LGPD).
--   - docs/requisitos/ (US-01..US-50, RNF-01..RNF-21).
--   - docs/arquitetura/ (ADR-11 posição global do outbox, ADR-16/ADR-18 serviço migrar, ARQ seção 13).
--   - spikes/onda-checkpoint/propria/schema.sql (lease, checkpoint idempotente, outbox).
--
-- Convenções:
--   - Tabelas no plural, snake_case; toda constraint com nome explícito (o Dev traduz a
--     violação pelo nome/SQLSTATE).
--   - Chave primária só inteira (decisão do dono de 2026-11-21, ADR-18 v2): bigint GENERATED ALWAYS AS
--     IDENTITY; nenhuma PK é uuid e toda FK aponta para a PK inteira. Entidade exposta na API ou em
--     eventos por id opaco (organização, projeto, anexo, importação de skill, artefato, onda, chamada de
--     modelo, execução de ferramenta, comando, evento, registro de auditoria) tem também
--     `public_id uuid NOT NULL UNIQUE DEFAULT gen_random_uuid()`; o id inteiro não sai do banco. O
--     `projetoid` dos eventos e das rotas é projects.public_id em texto (RNF-04); o nome é só atributo.
--   - Tabelas de alto volume só de inclusão (eventos, audit_records, model_calls,
--     tool_executions, wave_checkpoints) são particionadas por RANGE mensal na data, com partição
--     padrão e a função create_monthly_partitions (seção 11a). A PK inclui a chave de partição.
--   - Nada é apagado fisicamente pela aplicação (policy.history_preservation): FKs são
--     RESTRICT e os papéis de aplicação não têm DELETE nas tabelas de histórico. Exclusão de
--     projeto = anonimização (policy.data_retention), feita pelo papel easycode_retention.
--   - Comentário de coluna que começa com "[DP]" marca dado pessoal (DIC.dados_pessoais).
-- =============================================================================

CREATE SCHEMA IF NOT EXISTS easycode;
SET search_path TO easycode, public;

-- -----------------------------------------------------------------------------
-- Papéis de banco (privilégio mínimo; PERSIST seção 6). NOLOGIN: o login de cada processo
-- é um usuário membro do papel, criado na implantação.
-- -----------------------------------------------------------------------------
-- easycode_migration é o dono do esquema e o único com DDL (serviço migrar, ADR-18); os demais não têm DDL.
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'easycode_migration') THEN CREATE ROLE easycode_migration NOLOGIN; END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'easycode_api')      THEN CREATE ROLE easycode_api NOLOGIN; END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'easycode_worker')   THEN CREATE ROLE easycode_worker NOLOGIN; END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'easycode_retention') THEN CREATE ROLE easycode_retention NOLOGIN; END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'easycode_backup')   THEN CREATE ROLE easycode_backup NOLOGIN; END IF;
END $$;

-- =============================================================================
-- 1. Catálogo de transições (fonte: estados.yaml)
-- =============================================================================
CREATE TABLE state_transitions (
  id         bigint GENERATED ALWAYS AS IDENTITY,
  machine    text    NOT NULL,
  from_state         text    NULL,
  to_state       text    NOT NULL,
  events    text[]  NOT NULL,
  reserved  boolean NOT NULL DEFAULT false,
  CONSTRAINT state_transitions_pkey PRIMARY KEY (id),
  CONSTRAINT state_transitions_machine_ck CHECK (machine IN ('artifact','wave','stage','gate','finding','project','activity','skill')),
  CONSTRAINT state_transitions_uk UNIQUE NULLS NOT DISTINCT (machine, from_state, to_state)
);
COMMENT ON TABLE state_transitions IS 'Transição permitida de uma máquina de estados (uma linha por par de → para; de nulo = estado inicial na criação; de = para = autotransição, catalogada mas não conferida pelo gatilho porque a coluna não muda). Gerada de estados.yaml; a migração que muda estados.yaml regenera estas linhas.';
COMMENT ON COLUMN state_transitions.id IS 'Identificador interno.';
COMMENT ON COLUMN state_transitions.machine IS 'Máquina de estados (artifact, wave, stage, gate, finding, project, activity, skill).';
COMMENT ON COLUMN state_transitions.from_state IS 'Estado de origem; nulo quando a linha declara o estado inicial (ao_criar).';
COMMENT ON COLUMN state_transitions.to_state IS 'Estado de destino.';
COMMENT ON COLUMN state_transitions.events IS 'Gatilhos de estados.yaml que produzem a transição (comandos locais ou eventos easycode.*); (creation) no estado inicial.';
COMMENT ON COLUMN state_transitions.reserved IS 'Transição prevista fora das Fases 1-2 (reservado: true em estados.yaml); o gatilho a recusa.';
COMMENT ON CONSTRAINT state_transitions_machine_ck ON state_transitions IS 'Só as oito máquinas de estados.yaml.';
COMMENT ON CONSTRAINT state_transitions_uk ON state_transitions IS 'Um par de → para por máquina.';

-- Gerado de specs/dominio/estados/estados.yaml (sha256 27a3a8e21db426a7…): uma linha por (máquina, de, para),
-- gatilhos em ordem alfabética; inclui as autotransições (onda resume_after_crash/schedule_retry/record_checkpoint,
-- gate aprovar parcial do gate.release, etapa aprovada→aprovada, projeto alert_budget).
INSERT INTO state_transitions (machine, from_state, to_state, events, reserved) VALUES
  ('artifact', NULL, 'draft', '{(creation)}', false),
  ('artifact', 'draft', 'in_review', '{submit_for_review}', false),
  ('artifact', 'in_review', 'approved', '{easycode.gate.approved}', false),
  ('artifact', 'in_review', 'draft', '{easycode.finding.rework_started,easycode.gate.invalidated,easycode.gate.rejected,edit}', false),
  ('artifact', 'approved', 'draft', '{easycode.finding.rework_started,easycode.finding.scope_change_decided}', false),
  ('artifact', 'approved', 'replaced', '{replace}', false),
  ('artifact', 'draft', 'draft', '{edit}', false),
  ('wave', NULL, 'queued', '{(creation)}', false),
  ('wave', 'queued', 'running', '{dispatch}', false),
  ('wave', 'running', 'running', '{record_checkpoint,resume_after_crash,schedule_retry}', false),
  ('wave', 'running', 'awaiting_user', '{ask_user}', false),
  ('wave', 'awaiting_user', 'running', '{answer}', false),
  ('wave', 'running', 'completed', '{complete}', false),
  ('wave', 'running', 'failed', '{fail}', false),
  ('wave', 'running', 'pausing', '{pause,pause_by_policy}', false),
  ('wave', 'pausing', 'paused', '{checkpoint_reached,pause_now}', false),
  ('wave', 'running', 'paused', '{pause_now}', false),
  ('wave', 'paused', 'running', '{resume,resume_by_policy}', false),
  ('wave', 'failed', 'queued', '{restart}', false),
  ('wave', 'queued', 'canceled', '{cancel,cancel_by_policy}', false),
  ('wave', 'running', 'canceled', '{cancel,cancel_by_policy}', false),
  ('wave', 'awaiting_user', 'canceled', '{cancel,cancel_by_policy}', false),
  ('wave', 'pausing', 'canceled', '{cancel,cancel_by_policy}', false),
  ('wave', 'paused', 'canceled', '{cancel,cancel_by_policy}', false),
  ('wave', 'failed', 'canceled', '{cancel,cancel_by_policy}', false),
  ('stage', NULL, 'blocked', '{(creation)}', false),
  ('stage', 'blocked', 'ready', '{unblock}', false),
  ('stage', 'ready', 'running', '{easycode.wave.started}', false),
  ('stage', 'running', 'awaiting_gate', '{submit_to_gate}', false),
  ('stage', 'awaiting_gate', 'approved', '{easycode.gate.approved}', false),
  ('stage', 'awaiting_gate', 'returned', '{easycode.gate.rejected}', false),
  ('stage', 'returned', 'running', '{easycode.wave.started}', false),
  ('stage', 'reopened', 'running', '{easycode.wave.started}', false),
  ('stage', 'approved', 'reopened', '{easycode.finding.rework_started,easycode.finding.scope_change_decided}', false),
  ('stage', 'approved', 'approved', '{complete_reevaluation,easycode.finding.rework_started,easycode.stage.approved,easycode.stage.reopened}', false),
  ('stage', 'ready', 'blocked', '{easycode.stage.reopened}', false),
  ('stage', 'running', 'blocked', '{easycode.stage.reopened}', false),
  ('stage', 'awaiting_gate', 'blocked', '{easycode.stage.reopened}', false),
  ('stage', 'returned', 'blocked', '{easycode.stage.reopened}', false),
  ('stage', 'reopened', 'blocked', '{easycode.stage.reopened}', false),
  ('stage', 'blocked', 'skipped', '{skip}', true),
  ('stage', 'ready', 'skipped', '{skip}', true),
  ('stage', 'awaiting_gate', 'running', '{easycode.wave.started}', false),
  ('gate', NULL, 'evaluating', '{(creation)}', false),
  ('gate', 'evaluating', 'awaiting_human', '{complete_checks}', false),
  ('gate', 'evaluating', 'approved', '{complete_checks}', false),
  ('gate', 'evaluating', 'rejected', '{complete_checks}', false),
  ('gate', 'awaiting_human', 'approved', '{approve}', false),
  ('gate', 'awaiting_human', 'awaiting_human', '{approve}', false),
  ('gate', 'awaiting_human', 'rejected', '{reject}', false),
  ('gate', 'awaiting_human', 'evaluating', '{easycode.finding.opened}', false),
  ('gate', 'evaluating', 'invalidated', '{easycode.artifact.edited,easycode.stage.blocked}', false),
  ('gate', 'awaiting_human', 'invalidated', '{easycode.artifact.edited,easycode.stage.blocked}', false),
  ('gate', 'approved', 'invalidated', '{easycode.stage.reopened}', false),
  ('finding', NULL, 'open', '{(creation)}', false),
  ('finding', 'open', 'in_rework', '{start_rework}', false),
  ('finding', 'in_rework', 'revalidating', '{easycode.wave.completed}', false),
  ('finding', 'revalidating', 'closed', '{record_revalidation}', false),
  ('finding', 'revalidating', 'in_rework', '{record_revalidation}', false),
  ('finding', 'revalidating', 'escalated', '{record_revalidation}', false),
  ('finding', 'escalated', 'in_rework', '{authorize_new_rework}', false),
  ('finding', 'open', 'accepted', '{accept_risk}', false),
  ('finding', 'in_rework', 'accepted', '{accept_risk}', false),
  ('finding', 'revalidating', 'accepted', '{accept_risk}', false),
  ('finding', 'escalated', 'accepted', '{accept_risk}', false),
  ('finding', 'open', 'discarded', '{discard}', false),
  ('finding', 'in_rework', 'discarded', '{discard}', false),
  ('finding', 'escalated', 'discarded', '{discard}', false),
  ('finding', 'escalated', 'awaiting_scope', '{decide_scope_change}', false),
  ('finding', 'awaiting_scope', 'closed', '{easycode.gate.approved}', false),
  ('project', NULL, 'created', '{(creation)}', false),
  ('project', 'created', 'in_progress', '{start}', false),
  ('project', 'in_progress', 'paused', '{exhaust_budget,pause}', false),
  ('project', 'in_progress', 'in_progress', '{alert_budget}', false),
  ('project', 'paused', 'in_progress', '{resume}', false),
  ('project', 'in_progress', 'completed', '{record_publication}', false),
  ('project', 'created', 'canceled', '{cancel}', false),
  ('project', 'in_progress', 'canceled', '{cancel}', false),
  ('project', 'paused', 'canceled', '{cancel}', false),
  ('activity', NULL, 'blocked', '{(creation)}', false),
  ('activity', 'blocked', 'ready', '{unblock}', false),
  ('activity', 'ready', 'running', '{easycode.wave.started}', false),
  ('activity', 'completed', 'running', '{easycode.wave.started}', false),
  ('activity', 'running', 'completed', '{complete}', false),
  ('activity', 'ready', 'blocked', '{easycode.stage.reopened}', false),
  ('activity', 'running', 'blocked', '{easycode.stage.reopened}', false),
  ('activity', 'completed', 'blocked', '{easycode.stage.reopened}', false),
  ('skill', NULL, 'imported', '{(creation)}', false),
  ('skill', 'imported', 'scanning', '{start_scan}', false),
  ('skill', 'scanning', 'unblocked', '{complete_scan}', false),
  ('skill', 'scanning', 'blocked', '{complete_scan}', false),
  ('skill', 'blocked', 'unblocked', '{unblock}', false),
  ('skill', 'unblocked', 'deactivated', '{deactivate}', false),
  ('skill', 'blocked', 'deactivated', '{deactivate}', false);

-- Conferência de uma transição contra o catálogo (usada pelos gatilhos de transição).
-- p_old nulo = INSERT (p_new deve ser o estado inicial); p_old = p_new = autotransição, não conferida.
CREATE FUNCTION check_transition(p_machine text, p_old text, p_new text) RETURNS void LANGUAGE plpgsql AS $$
BEGIN
  IF p_old IS NULL THEN
    IF NOT EXISTS (SELECT 1 FROM state_transitions t
                    WHERE t.machine = p_machine AND t.from_state IS NULL AND t.to_state = p_new) THEN
      RAISE EXCEPTION 'estado inicial inválido para %: %', p_machine, p_new
        USING ERRCODE = '23514', CONSTRAINT = p_machine || '_valid_transition';
    END IF;
  ELSIF p_old IS DISTINCT FROM p_new THEN
    IF NOT EXISTS (SELECT 1 FROM state_transitions t
                    WHERE t.machine = p_machine AND t.from_state = p_old AND t.to_state = p_new
                      AND NOT t.reserved) THEN
      RAISE EXCEPTION 'transição inválida em %: % -> %', p_machine, p_old, p_new
        USING ERRCODE = '23514', CONSTRAINT = p_machine || '_valid_transition';
    END IF;
  END IF;
END $$;

-- Gatilho genérico: TG_ARGV[0] = máquina, TG_ARGV[1] = coluna de estado.
-- INSERT: estado deve ser o inicial. UPDATE com mudança: par (de, para) deve existir e não ser reservado.
CREATE FUNCTION validate_transition() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  PERFORM check_transition(TG_ARGV[0],
                            CASE WHEN TG_OP = 'INSERT' THEN NULL ELSE to_jsonb(OLD) ->> TG_ARGV[1] END,
                            to_jsonb(NEW) ->> TG_ARGV[1]);
  RETURN NEW;
END $$;

-- Bloqueio de DELETE para quem não é o papel de retenção (policy.history_preservation).
-- TG_ARGV[0], quando informado, é o nome da tabela-mãe: numa tabela particionada o gatilho roda na partição e
-- TG_TABLE_NAME seria o nome da partição (o nome da constraint na mensagem fica o da tabela-mãe).
CREATE FUNCTION block_delete() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE v_table text := coalesce(TG_ARGV[0], TG_TABLE_NAME);
BEGIN
  IF pg_has_role(current_user, 'easycode_retention', 'MEMBER') THEN
    RETURN OLD;
  END IF;
  RAISE EXCEPTION 'operação destrutiva recusada em % (preservação do histórico)', v_table
    USING ERRCODE = '42501', CONSTRAINT = v_table || '_no_delete';
END $$;

-- =============================================================================
-- 2. Organização, usuários e papéis
-- =============================================================================
CREATE TABLE organizations (
  id                          bigint        GENERATED ALWAYS AS IDENTITY,
  public_id                  uuid          NOT NULL DEFAULT gen_random_uuid(),
  name                        text          NOT NULL,
  oidc_issuer                text          NOT NULL,
  currency                       char(3)       NOT NULL DEFAULT 'BRL',
  stop_limit_ceiling          numeric(14,4) NULL,
  retention_period              interval      NOT NULL DEFAULT interval '1825 days',
  audit_period             interval      NOT NULL DEFAULT interval '1825 days',
  call_content_period      interval      NOT NULL DEFAULT interval '90 days',
  backup_period      interval      NOT NULL DEFAULT interval '30 days',
  created_at                   timestamptz   NOT NULL DEFAULT now(),
  CONSTRAINT organizations_pkey PRIMARY KEY (id),
  CONSTRAINT organizations_public_id_uk UNIQUE (public_id),
  CONSTRAINT organizations_name_uk UNIQUE (name),
  CONSTRAINT organizations_issuer_uk UNIQUE (oidc_issuer),
  CONSTRAINT organizations_name_ck CHECK (btrim(name) <> ''),
  CONSTRAINT organizations_currency_ck CHECK (currency ~ '^[A-Z]{3}$'),
  CONSTRAINT organizations_ceiling_ck CHECK (stop_limit_ceiling IS NULL OR stop_limit_ceiling >= 0.01),
  CONSTRAINT organizations_retention_period_ck CHECK (retention_period BETWEEN interval '30 days' AND interval '3650 days'),
  CONSTRAINT organizations_audit_period_ck CHECK (audit_period BETWEEN interval '365 days' AND interval '3650 days' AND audit_period >= retention_period),
  CONSTRAINT organizations_content_period_ck CHECK (call_content_period BETWEEN interval '7 days' AND interval '365 days'),
  CONSTRAINT organizations_backup_period_ck CHECK (backup_period BETWEEN interval '1 day' AND interval '30 days')
);
COMMENT ON TABLE organizations IS 'Organização cliente (inquilino). Isola projetos, usuários, preços, skills e chaves de provedor (RNF-04).';
COMMENT ON COLUMN organizations.id IS 'Identificador interno (chave primária inteira, alvo das FKs); não sai do banco: API e eventos usam public_id.';
COMMENT ON COLUMN organizations.public_id IS 'Identificador opaco exposto (organizacao.id da sessão na API).';
COMMENT ON COLUMN organizations.name IS 'Nome da organização.';
COMMENT ON COLUMN organizations.oidc_issuer IS 'Issuer OIDC do realm Keycloak da organização; com users.sub identifica a pessoa (RNF-06).';
COMMENT ON COLUMN organizations.currency IS 'Moeda ISO 4217 de orçamento e preços (policy.project_budget.currency).';
COMMENT ON COLUMN organizations.stop_limit_ceiling IS 'Teto do limite de parada dos projetos (policy.project_budget.stop_limit_ceiling); nulo = sem teto.';
COMMENT ON COLUMN organizations.retention_period IS 'Guarda dos dados do projeto após concluído/cancelado (policy.data_retention.retention_period, P30D–P3650D, padrão P1825D).';
COMMENT ON COLUMN organizations.audit_period IS 'Guarda da trilha de auditoria, dos eventos de domínio e dos metadados de chamadas (policy.data_retention.audit_period, P365D–P3650D, padrão P1825D = 5 anos; nunca menor que retention_period; RNF-13, D-30).';
COMMENT ON COLUMN organizations.call_content_period IS 'Guarda do conteúdo (prompt, resposta, estado da conversa) de chamadas de modelo e checkpoints; vencido, o efeito expire_content apaga só o conteúdo (policy.data_retention.call_content_period, P7D–P365D, padrão P90D; RNF-20, D-30).';
COMMENT ON COLUMN organizations.backup_period IS 'Guarda das cópias de segurança; dado anonimizado na base some das cópias neste prazo (policy.data_retention.backup_period, P1D–P30D, padrão P30D; D-30).';
COMMENT ON COLUMN organizations.created_at IS 'Instante de criação.';
COMMENT ON CONSTRAINT organizations_public_id_uk ON organizations IS 'Id público único.';
COMMENT ON CONSTRAINT organizations_name_ck ON organizations IS 'Nome não vazio.';
COMMENT ON CONSTRAINT organizations_currency_ck ON organizations IS 'Código ISO 4217.';
COMMENT ON CONSTRAINT organizations_ceiling_ck ON organizations IS 'Faixa de policy.project_budget.stop_limit_ceiling (mínimo 0,01).';
COMMENT ON CONSTRAINT organizations_retention_period_ck ON organizations IS 'Faixa de policy.data_retention.retention_period.';
COMMENT ON CONSTRAINT organizations_audit_period_ck ON organizations IS 'Faixa de policy.data_retention.audit_period e trilha nunca vence antes do projeto (D-30).';
COMMENT ON CONSTRAINT organizations_content_period_ck ON organizations IS 'Faixa de policy.data_retention.call_content_period.';
COMMENT ON CONSTRAINT organizations_backup_period_ck ON organizations IS 'Faixa de policy.data_retention.backup_period.';

CREATE TABLE users (
  id               bigint      GENERATED ALWAYS AS IDENTITY,
  organization_id   bigint      NOT NULL,
  sub              text        NOT NULL,
  display_name    text        NOT NULL,
  administrator    boolean     NOT NULL DEFAULT false,
  active            boolean     NOT NULL DEFAULT true,
  anonymized_at   timestamptz NULL,
  created_at        timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT users_pkey PRIMARY KEY (id),
  CONSTRAINT users_organization_fk FOREIGN KEY (organization_id) REFERENCES organizations (id) ON DELETE RESTRICT,
  CONSTRAINT users_sub_uk UNIQUE (organization_id, sub),
  CONSTRAINT users_sub_ck CHECK (char_length(sub) BETWEEN 1 AND 128),
  CONSTRAINT users_name_ck CHECK (btrim(display_name) <> ''),
  CONSTRAINT users_anonymized_ck CHECK (anonymized_at IS NULL OR NOT active)
);
COMMENT ON TABLE users IS 'Pessoa da organização autenticada pelo SSO. A plataforma não guarda senha nem e-mail (US-01/RN-1, minimização LGPD). Nunca é apagada: quem sai da organização é desativado e tem o nome de exibição anonimizado, com o id opaco mantido (anonymize_offboarded_user, sempre true; D-31).';
COMMENT ON COLUMN users.id IS 'Identificador interno; FKs de autoria (…_por) apontam para ele.';
COMMENT ON COLUMN users.organization_id IS 'Organização da pessoa.';
COMMENT ON COLUMN users.sub IS '[DP] Subject OIDC do Keycloak: id opaco que vai em eventos (ator.id) e na auditoria.';
COMMENT ON COLUMN users.display_name IS '[DP] Nome para exibir quem decidiu, respondeu ou aprovou; atualizado a cada login a partir do token.';
COMMENT ON COLUMN users.administrator IS 'Papel Administrador da organização (chaves de provedor, tabela de preços, skills; US-15, US-37, US-44).';
COMMENT ON COLUMN users.active IS 'Falso quando a conta foi desativada no SSO ou anonimizada.';
COMMENT ON COLUMN users.anonymized_at IS 'Instante da anonimização (nome de exibição substituído; sub mantido como id opaco da trilha, D-31); nulo se não anonimizado.';
COMMENT ON COLUMN users.created_at IS 'Primeiro login.';
COMMENT ON CONSTRAINT users_sub_uk ON users IS 'Uma linha por pessoa no emissor da organização.';
COMMENT ON CONSTRAINT users_sub_ck ON users IS 'Tamanho do id opaco igual ao de ator.id (1..128).';
COMMENT ON CONSTRAINT users_name_ck ON users IS 'Nome de exibição não vazio.';
COMMENT ON CONSTRAINT users_anonymized_ck ON users IS 'Usuário anonimizado não fica ativo.';

-- =============================================================================
-- 3. Projeto, papéis no projeto, contadores, anexos e configuração
-- =============================================================================
CREATE TABLE projects (
  id                        bigint        GENERATED ALWAYS AS IDENTITY,
  public_id                uuid          NOT NULL DEFAULT gen_random_uuid(),
  organization_id            bigint        NOT NULL,
  name                      text          NOT NULL,
  business_need       text          NOT NULL,
  state                    text          NOT NULL DEFAULT 'created',
  pause_reason              text          NULL,
  artifact_language          text          NOT NULL DEFAULT 'pt-BR',
  repository               text          NULL,
  budget_stop_limit   numeric(14,4) NULL,
  budget_alert_limits  smallint[]    NOT NULL DEFAULT '{50,80}',
  last_alert_percentage  smallint      NULL,
  provider_acknowledgment_by      bigint        NOT NULL,
  provider_acknowledgment_at       timestamptz   NOT NULL,
  created_by                bigint        NOT NULL,
  created_at                 timestamptz   NOT NULL DEFAULT now(),
  started_at               timestamptz   NULL,
  finished_at             timestamptz   NULL,
  deletion_requested_by       bigint        NULL,
  deletion_requested_at        timestamptz   NULL,
  data_anonymized_at     timestamptz   NULL,
  row_version              integer       NOT NULL DEFAULT 1,
  CONSTRAINT projects_pkey PRIMARY KEY (id),
  CONSTRAINT projects_public_id_uk UNIQUE (public_id),
  CONSTRAINT projects_organization_fk FOREIGN KEY (organization_id) REFERENCES organizations (id) ON DELETE RESTRICT,
  CONSTRAINT projects_acknowledgment_per_fk FOREIGN KEY (provider_acknowledgment_by) REFERENCES users (id) ON DELETE RESTRICT,
  CONSTRAINT projects_created_by_fk FOREIGN KEY (created_by) REFERENCES users (id) ON DELETE RESTRICT,
  CONSTRAINT projects_deletion_per_fk FOREIGN KEY (deletion_requested_by) REFERENCES users (id) ON DELETE RESTRICT,
  CONSTRAINT projects_name_ck CHECK (char_length(btrim(name)) BETWEEN 1 AND 140),
  CONSTRAINT projects_need_ck CHECK (btrim(business_need) <> ''),
  CONSTRAINT projects_state_ck CHECK (state IN ('created','in_progress','paused','completed','canceled')),
  CONSTRAINT projects_pause_reason_ck CHECK (pause_reason IN ('manual','budget')),
  CONSTRAINT projects_consistent_pause_ck CHECK ((state = 'paused') = (pause_reason IS NOT NULL)),
  CONSTRAINT projects_language_ck CHECK (artifact_language ~ '^[a-z]{2}(-[A-Z]{2})?$'),
  CONSTRAINT projects_started_ck CHECK ((state NOT IN ('in_progress','paused','completed') OR started_at IS NOT NULL)
                                         AND (state <> 'created' OR started_at IS NULL)),
  CONSTRAINT projects_repository_ck CHECK (started_at IS NULL OR repository IS NOT NULL),
  CONSTRAINT projects_budget_ck CHECK (budget_stop_limit IS NULL OR budget_stop_limit >= 0.01),
  CONSTRAINT projects_budget_set_ck CHECK (started_at IS NULL OR budget_stop_limit IS NOT NULL),
  CONSTRAINT projects_alerts_ck CHECK (1 <= ALL (budget_alert_limits) AND 99 >= ALL (budget_alert_limits)),
  CONSTRAINT projects_finished_ck CHECK ((state IN ('completed','canceled')) = (finished_at IS NOT NULL)),
  CONSTRAINT projects_deletion_ck CHECK ((deletion_requested_by IS NULL) = (deletion_requested_at IS NULL)),
  CONSTRAINT projects_deletion_state_ck CHECK (deletion_requested_at IS NULL OR state IN ('completed','canceled')),
  CONSTRAINT projects_anonymized_ck CHECK (data_anonymized_at IS NULL OR state IN ('completed','canceled'))
);
COMMENT ON TABLE projects IS 'Projeto: necessidade de negócio, configuração, orçamento, repositório e ciclo de vida (máquina projeto).';
COMMENT ON COLUMN projects.id IS 'Identificador interno (chave primária inteira, alvo das FKs); não sai do banco: API e eventos usam public_id.';
COMMENT ON COLUMN projects.public_id IS 'Identificador opaco exposto; é o projetoid da API e dos eventos (texto do uuid) e não revela o nome (RNF-04).';
COMMENT ON COLUMN projects.organization_id IS 'Organização dona do projeto.';
COMMENT ON COLUMN projects.name IS 'Nome do projeto, único na organização enquanto não excluído (US-03/RN-1).';
COMMENT ON COLUMN projects.business_need IS 'Necessidade de negócio em texto, entrada da etapa Requisitos (conteúdo do projeto, enviado ao provedor de IA).';
COMMENT ON COLUMN projects.state IS 'Estado na máquina projeto (estados.yaml).';
COMMENT ON COLUMN projects.pause_reason IS 'manual ou budget; presente só quando pausado (payload project.paused).';
COMMENT ON COLUMN projects.artifact_language IS 'Idioma dos artefatos gerados, padrão pt-BR (RNF-21).';
COMMENT ON COLUMN projects.repository IS 'Endereço interno do repositório git do projeto (fonte de verdade dos artefatos); exigido para iniciar.';
COMMENT ON COLUMN projects.budget_stop_limit IS 'Limite de parada do orçamento na currency da organização (policy.project_budget.stop_limit; US-39).';
COMMENT ON COLUMN projects.budget_alert_limits IS 'Percentuais de alerta do orçamento (padrão 50 e 80; US-39/CA-1).';
COMMENT ON COLUMN projects.last_alert_percentage IS 'Maior limite de alerta já emitido; evita alerta repetido (project.budget_alerted).';
COMMENT ON COLUMN projects.provider_acknowledgment_by IS 'Quem confirmou a ciência do envio de conteúdo ao provedor de IA (US-03/RN-2).';
COMMENT ON COLUMN projects.provider_acknowledgment_at IS 'Quando a ciência foi registrada (US-03/RN-2, RNF-20).';
COMMENT ON COLUMN projects.created_by IS 'Quem criou o projeto (vira responsável e aprovador; US-02/RN-1).';
COMMENT ON COLUMN projects.created_at IS 'Instante de criação.';
COMMENT ON COLUMN projects.started_at IS 'Instante da transição criado → in_progress.';
COMMENT ON COLUMN projects.finished_at IS 'Instante de conclusão ou cancelamento; conta o prazo de retenção.';
COMMENT ON COLUMN projects.deletion_requested_by IS 'Quem pediu a exclusão do projeto (US-04).';
COMMENT ON COLUMN projects.deletion_requested_at IS 'Quando a exclusão foi pedida; o projeto some das listas e do acesso de todos a partir daí (US-04/CA-1).';
COMMENT ON COLUMN projects.data_anonymized_at IS 'Quando o dado pessoal do projeto foi anonimizado (policy.data_retention; evento project.data_anonymized).';
COMMENT ON COLUMN projects.row_version IS 'Versão otimista para edição concorrente de configuração (UPDATE … WHERE row_version = $v).';
COMMENT ON CONSTRAINT projects_public_id_uk ON projects IS 'Id público único.';
COMMENT ON CONSTRAINT projects_name_ck ON projects IS 'Nome não vazio, até 140 caracteres.';
COMMENT ON CONSTRAINT projects_need_ck ON projects IS 'regra: US-03/CA-2 — necessidade de negócio obrigatória.';
COMMENT ON CONSTRAINT projects_state_ck ON projects IS 'Estados da máquina projeto.';
COMMENT ON CONSTRAINT projects_pause_reason_ck ON projects IS 'Enumeração pause_reason do payload project.paused.';
COMMENT ON CONSTRAINT projects_consistent_pause_ck ON projects IS 'Motivo de pausa existe só no estado paused.';
COMMENT ON CONSTRAINT projects_language_ck ON projects IS 'Etiqueta de idioma simples (RNF-21).';
COMMENT ON CONSTRAINT projects_started_ck ON projects IS 'Projeto em andamento, paused ou concluído foi iniciado; created não. Cancelado pode ter sido iniciado ou não (cancelar a partir de created, US-51/CA-4).';
COMMENT ON CONSTRAINT projects_repository_ck ON projects IS 'Guarda de iniciar: repositório do projeto criado (projeto cancelado antes de iniciar não tem).';
COMMENT ON CONSTRAINT projects_budget_ck ON projects IS 'Faixa de policy.project_budget.stop_limit.';
COMMENT ON CONSTRAINT projects_budget_set_ck ON projects IS 'Guarda de iniciar: orçamento definido (US-05/CA-2); projeto cancelado antes de iniciar não precisa.';
COMMENT ON CONSTRAINT projects_alerts_ck ON projects IS 'regra: US-39/CA-1 — limites de alerta entre 1 e 99%.';
COMMENT ON CONSTRAINT projects_finished_ck ON projects IS 'Data de finalização só em estado final.';
COMMENT ON CONSTRAINT projects_deletion_ck ON projects IS 'Pedido de exclusão registra quem e quando juntos (US-04/CA-3).';
COMMENT ON CONSTRAINT projects_deletion_state_ck ON projects IS 'policy.data_retention: projeto em andamento ou pausado não é excluído; cancela-se antes.';
COMMENT ON CONSTRAINT projects_anonymized_ck ON projects IS 'Anonimização só de projeto concluído ou canceled (registro anonymize_data).';

CREATE UNIQUE INDEX projects_name_org_uk ON projects (organization_id, lower(name)) WHERE deletion_requested_at IS NULL;
COMMENT ON INDEX projects_name_org_uk IS 'regra: US-03/RN-1 — nome único na organização, sem diferenciar maiúsculas, entre projetos não excluídos; serve também a lista de projetos da organização.';
CREATE INDEX projects_org_state_idx ON projects (organization_id, state) WHERE deletion_requested_at IS NULL;
COMMENT ON INDEX projects_org_state_idx IS 'Dashboard filtrado por estado do projeto (US-50/CA-2) e varredura de retenção por estado final.';

CREATE TRIGGER projects_valid_transition BEFORE INSERT OR UPDATE OF state ON projects
  FOR EACH ROW EXECUTE FUNCTION validate_transition('project', 'state');
COMMENT ON TRIGGER projects_valid_transition ON projects IS 'Estado inicial criado e só transições de estados.yaml (máquina projeto).';
CREATE TRIGGER projects_no_delete BEFORE DELETE ON projects
  FOR EACH ROW EXECUTE FUNCTION block_delete();
COMMENT ON TRIGGER projects_no_delete ON projects IS 'Projeto não é apagado: exclusão = anonimização (policy.data_retention).';

-- Guarda de concluir (US-52; record_publication na Fase 3): Release aprovada, nenhuma onda ativa e nenhum
-- ACH Crítico/Alto aberto (aceitos não bloqueiam). Pausar, retomar e cancelar (US-51) usam só o catálogo.
CREATE FUNCTION projects_valid_completion() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM stages e WHERE e.project_id = NEW.id AND e.stage = 'release' AND e.state = 'approved')
     OR NOT EXISTS (SELECT 1 FROM gate_rounds r
                     WHERE r.project_id = NEW.id AND r.stage = 'release' AND r.state = 'approved'
                       AND r.number = (SELECT max(number) FROM gate_rounds x WHERE x.project_id = NEW.id AND x.stage = 'release')) THEN
    RAISE EXCEPTION 'projeto só conclui com o gate.release aprovado' USING ERRCODE = '23514', CONSTRAINT = 'projects_valid_completion';
  END IF;
  IF EXISTS (SELECT 1 FROM waves o WHERE o.project_id = NEW.id
                AND o.state IN ('queued','running','awaiting_user','pausing','paused')) THEN
    RAISE EXCEPTION 'projeto com onda ativa não conclui' USING ERRCODE = '23514', CONSTRAINT = 'projects_valid_completion';
  END IF;
  IF EXISTS (SELECT 1 FROM findings a WHERE a.project_id = NEW.id AND a.severity IN ('critical','high')
                AND a.status IN ('open','in_rework','revalidating','escalated','awaiting_scope')) THEN
    RAISE EXCEPTION 'há ACH Crítico ou Alto aberto' USING ERRCODE = '23514', CONSTRAINT = 'projects_valid_completion';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER projects_valid_completion BEFORE UPDATE OF state ON projects
  FOR EACH ROW WHEN (NEW.state = 'completed' AND OLD.state <> 'completed') EXECUTE FUNCTION projects_valid_completion();
COMMENT ON TRIGGER projects_valid_completion ON projects IS 'regra: US-52/CA-1 — projeto conclui só com a etapa release aprovada no último gate.release, sem onda ativa e sem ACH Crítico/Alto aberto (guarda de estados.yaml).';

CREATE TABLE project_constraints (
  project_id    bigint      NOT NULL,
  version        integer     NOT NULL,
  items         jsonb       NOT NULL,
  defined_by  bigint      NOT NULL,
  defined_at   timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT project_constraints_pkey PRIMARY KEY (project_id, version),
  CONSTRAINT project_constraints_project_fk FOREIGN KEY (project_id) REFERENCES projects (id) ON DELETE RESTRICT,
  CONSTRAINT project_constraints_defined_by_fk FOREIGN KEY (defined_by) REFERENCES users (id) ON DELETE RESTRICT,
  CONSTRAINT project_constraints_version_ck CHECK (version >= 1),
  CONSTRAINT project_constraints_items_ck CHECK (jsonb_typeof(items) = 'array' AND jsonb_array_length(items) <= 100)
);
COMMENT ON TABLE project_constraints IS 'Restrições técnicas do projeto (linguagem, front-end, banco, integrações, infraestrutura, norma), versionadas: cada alteração grava nova versão (US-54, PL-13). Entrada {RESTRICOES} da Etapa de Arquitetura; a ADR do tipo imposta cita o item RESTR-NN (PD-03).';
COMMENT ON COLUMN project_constraints.project_id IS 'Projeto.';
COMMENT ON COLUMN project_constraints.version IS 'Versão das restrições (1 na criação do projeto).';
COMMENT ON COLUMN project_constraints.items IS 'Lista de {id RESTR-NN, categoria, descricao}; texto técnico humano, sem dado pessoal (não confiável para o agente, RNF-05).';
COMMENT ON COLUMN project_constraints.defined_by IS 'Quem gravou a versão (responsável; auditoria restricoes.alterar).';
COMMENT ON COLUMN project_constraints.defined_at IS 'Quando gravou.';
COMMENT ON CONSTRAINT project_constraints_project_fk ON project_constraints IS 'Restrições de projeto existente.';
COMMENT ON CONSTRAINT project_constraints_defined_by_fk ON project_constraints IS 'Autor é usuário existente.';
COMMENT ON CONSTRAINT project_constraints_version_ck ON project_constraints IS 'Versão positiva.';
COMMENT ON CONSTRAINT project_constraints_items_ck ON project_constraints IS 'regra: US-54/RN-1 — lista de até 100 itens.';
-- Versão não muda nem é apagada: alteração = nova versão; a API só tem INSERT (policy.history_preservation).

CREATE TABLE project_members (
  id bigint GENERATED ALWAYS AS IDENTITY,
  project_id     bigint      NOT NULL,
  user_id     bigint      NOT NULL,
  role          text        NOT NULL,
  assigned_by  bigint      NOT NULL,
  assigned_at   timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT project_members_pkey PRIMARY KEY (id),
  CONSTRAINT project_members_project_user_role_uk UNIQUE (project_id, user_id, role),
  CONSTRAINT project_members_project_fk FOREIGN KEY (project_id) REFERENCES projects (id) ON DELETE RESTRICT,
  CONSTRAINT project_members_user_fk FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE RESTRICT,
  CONSTRAINT project_members_assigned_by_fk FOREIGN KEY (assigned_by) REFERENCES users (id) ON DELETE RESTRICT,
  CONSTRAINT project_members_role_ck CHECK (role IN ('lead','approver','member'))
);
COMMENT ON TABLE project_members IS 'Papel de uma pessoa num projeto (uma linha por papel). Sem papel = sem acesso (US-02/CA-3, RNF-04). Administrador é da organização (users.administrator).';
COMMENT ON COLUMN project_members.id IS 'Identificador interno (PK inteira, ADR-18 v2).';
COMMENT ON COLUMN project_members.project_id IS 'Projeto.';
COMMENT ON COLUMN project_members.user_id IS 'Pessoa.';
COMMENT ON COLUMN project_members.role IS 'lead, approver ou member.';
COMMENT ON COLUMN project_members.assigned_by IS 'Quem atribuiu o papel (auditoria, US-02/CA-1).';
COMMENT ON COLUMN project_members.assigned_at IS 'Quando atribuiu.';
COMMENT ON CONSTRAINT project_members_project_user_role_uk ON project_members IS 'Chave natural: uma linha por (project_id, user_id, role); antiga PK, mantida como UNIQUE.';
COMMENT ON CONSTRAINT project_members_role_ck ON project_members IS 'Papéis de pessoas no projeto (US-02).';

CREATE UNIQUE INDEX project_members_lead_uk ON project_members (project_id) WHERE role = 'lead';
COMMENT ON INDEX project_members_lead_uk IS 'regra: US-02/RN-3 — no máximo um responsável por projeto (o gatilho project_require_lead garante ao menos um).';
CREATE INDEX project_members_user_idx ON project_members (user_id, project_id);
COMMENT ON INDEX project_members_user_idx IS 'Projetos de uma pessoa: dashboard (US-50/CA-3), Pendências filtradas por papel (US-48/CA-3), autorização de cada rota (RNF-04); cobre a FK user_id.';

-- Todo projeto mantém ao menos um aprovador (constraint trigger adiado para o fim da transação,
-- permite trocar o aprovador numa transação só; trava a linha do projeto para serializar remoções).
CREATE FUNCTION project_members_require_approver() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  PERFORM 1 FROM projects WHERE id = OLD.project_id FOR UPDATE;
  IF NOT EXISTS (SELECT 1 FROM project_members
                  WHERE project_id = OLD.project_id AND role = 'approver') THEN
    RAISE EXCEPTION 'o projeto precisa de ao menos um aprovador'
      USING ERRCODE = '23514', CONSTRAINT = 'project_members_require_approver';
  END IF;
  RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER project_members_require_approver AFTER DELETE OR UPDATE OF role ON project_members
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW
  WHEN (OLD.role = 'approver') EXECUTE FUNCTION project_members_require_approver();
COMMENT ON TRIGGER project_members_require_approver ON project_members IS 'regra: US-02/RN-2 — todo projeto tem ao menos um aprovador (US-02/CA-4).';

-- Todo projeto tem exatamente um responsável (o índice único garante no máximo um; este gatilho, ao menos um).
-- Adiado para o fim da transação: a criação do projeto grava o responsável na mesma transação, e a
-- transferência (só pelo administrador) troca o papel numa transação só. Violação = 409 responsavel-obrigatorio.
CREATE FUNCTION project_require_lead() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE v_project bigint;
BEGIN
  IF TG_TABLE_NAME = 'projects' THEN v_project := NEW.id; ELSE v_project := OLD.project_id; END IF;
  PERFORM 1 FROM projects WHERE id = v_project FOR UPDATE;
  IF NOT EXISTS (SELECT 1 FROM project_members
                  WHERE project_id = v_project AND role = 'lead') THEN
    RAISE EXCEPTION 'o projeto precisa de um responsável'
      USING ERRCODE = '23514', CONSTRAINT = 'project_require_lead';
  END IF;
  RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER project_members_require_lead AFTER DELETE OR UPDATE OF role ON project_members
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW
  WHEN (OLD.role = 'lead') EXECUTE FUNCTION project_require_lead();
COMMENT ON TRIGGER project_members_require_lead ON project_members IS 'regra: US-02/RN-3 — o responsável não sai sem outro no lugar, na mesma transação (transferência só pelo administrador, US-02/CA-5, D-E4-10).';
CREATE CONSTRAINT TRIGGER projects_require_lead AFTER INSERT ON projects
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION project_require_lead();
COMMENT ON TRIGGER projects_require_lead ON projects IS 'regra: US-02/RN-3 — projeto nasce com o responsável gravado na mesma transação (quem cria, US-02/RN-1).';

CREATE TABLE project_counters (
  id bigint GENERATED ALWAYS AS IDENTITY,
  project_id  bigint NOT NULL,
  name        text   NOT NULL,
  value       bigint NOT NULL DEFAULT 0,
  CONSTRAINT project_counters_pkey PRIMARY KEY (id),
  CONSTRAINT project_counters_project_name_uk UNIQUE (project_id, name),
  CONSTRAINT project_counters_project_fk FOREIGN KEY (project_id) REFERENCES projects (id) ON DELETE RESTRICT,
  CONSTRAINT project_counters_name_ck CHECK (name ~ '^(event|finding|question|wave:(REQ|ARQ|DBA|DSG|VAL|DEV|QA|SEG|REL))$'),
  CONSTRAINT project_counters_value_ck CHECK (value >= 0)
);
COMMENT ON TABLE project_counters IS 'Último número usado por série no projeto: sequencia de eventos, ACH-NN, PERG-n e ondas por sigla. UPDATE … SET valor = valor + 1 RETURNING na mesma transação dá números sem lacuna e na ordem de commit.';
COMMENT ON COLUMN project_counters.id IS 'Identificador interno (PK inteira, ADR-18 v2).';
COMMENT ON COLUMN project_counters.project_id IS 'Projeto.';
COMMENT ON COLUMN project_counters.name IS 'Série: event, finding, question ou wave:<sigla>.';
COMMENT ON COLUMN project_counters.value IS 'Último valor emitido.';
COMMENT ON CONSTRAINT project_counters_project_name_uk ON project_counters IS 'Chave natural: uma linha por (project_id, name); antiga PK, mantida como UNIQUE.';
COMMENT ON CONSTRAINT project_counters_name_ck ON project_counters IS 'Séries conhecidas; siglas de onda de x-siglas-onda do cabeçalho comum (D-33); PERG-n por projeto (D-34).';
COMMENT ON CONSTRAINT project_counters_value_ck ON project_counters IS 'Contador não negativo.';

CREATE TABLE organization_counters (
  id bigint GENERATED ALWAYS AS IDENTITY,
  organization_id  bigint NOT NULL,
  name            text   NOT NULL,
  value           bigint NOT NULL DEFAULT 0,
  CONSTRAINT organization_counters_pkey PRIMARY KEY (id),
  CONSTRAINT organization_counters_organization_name_uk UNIQUE (organization_id, name),
  CONSTRAINT organization_counters_organization_fk FOREIGN KEY (organization_id) REFERENCES organizations (id) ON DELETE RESTRICT,
  CONSTRAINT organization_counters_name_ck CHECK (name = 'event'),
  CONSTRAINT organization_counters_value_ck CHECK (value >= 0)
);
COMMENT ON TABLE organization_counters IS 'Último número usado por série da organização: sequencia dos eventos de skill (fluxo /organizacao, D-26). Mesmo uso de project_counters (UPDATE … RETURNING na transação da mudança).';
COMMENT ON COLUMN organization_counters.id IS 'Identificador interno (PK inteira, ADR-18 v2).';
COMMENT ON COLUMN organization_counters.organization_id IS 'Organização.';
COMMENT ON COLUMN organization_counters.name IS 'Série: event.';
COMMENT ON COLUMN organization_counters.value IS 'Último valor emitido.';
COMMENT ON CONSTRAINT organization_counters_organization_name_uk ON organization_counters IS 'Chave natural: uma linha por (organization_id, name); antiga PK, mantida como UNIQUE.';
COMMENT ON CONSTRAINT organization_counters_name_ck ON organization_counters IS 'Séries conhecidas da organização.';
COMMENT ON CONSTRAINT organization_counters_value_ck ON organization_counters IS 'Contador não negativo.';

CREATE TABLE project_attachments (
  id                     bigint      GENERATED ALWAYS AS IDENTITY,
  public_id             uuid        NOT NULL DEFAULT gen_random_uuid(),
  project_id             bigint      NOT NULL,
  file_name           text        NOT NULL,
  media_type             text        NOT NULL,
  size_bytes          bigint      NOT NULL,
  sha256                 text        NOT NULL,
  path                text        NOT NULL,
  submitted_by            bigint      NOT NULL,
  submitted_at             timestamptz NOT NULL DEFAULT now(),
  content_purged_at  timestamptz NULL,
  CONSTRAINT project_attachments_pkey PRIMARY KEY (id),
  CONSTRAINT project_attachments_public_id_uk UNIQUE (public_id),
  CONSTRAINT project_attachments_project_fk FOREIGN KEY (project_id) REFERENCES projects (id) ON DELETE RESTRICT,
  CONSTRAINT project_attachments_submitted_by_fk FOREIGN KEY (submitted_by) REFERENCES users (id) ON DELETE RESTRICT,
  CONSTRAINT project_attachments_path_uk UNIQUE (path),
  CONSTRAINT project_attachments_name_ck CHECK (char_length(btrim(file_name)) BETWEEN 1 AND 255 AND file_name !~ '[/\\]'),
  CONSTRAINT project_attachments_type_ck CHECK (media_type ~ '^[a-z]+/[a-z0-9.+-]+$'),
  CONSTRAINT project_attachments_size_ck CHECK (size_bytes >= 0),
  CONSTRAINT project_attachments_sha256_ck CHECK (sha256 ~ '^[0-9a-f]{64}$'),
  CONSTRAINT project_attachments_path_ck CHECK (path ~ '^attachments/[0-9a-f-]{36}/[0-9a-f-]{36}$')
);
COMMENT ON TABLE project_attachments IS 'Arquivo anexado ao projeto como entrada da etapa Requisitos (US-03; listarAnexos e anexarArquivo da API). O conteúdo fica no volume arquivos (caminho), não no banco; é conteúdo não confiável (RNF-05) enviado ao provedor de IA. Só se anexa com o projeto em criado; o limite de tamanho é da configuração (413 anexo-muito-grande, US-03/CA-4).';
COMMENT ON COLUMN project_attachments.id IS 'Identificador interno (chave primária inteira, alvo das FKs); não sai do banco: API e eventos usam public_id.';
COMMENT ON COLUMN project_attachments.public_id IS 'Identificador opaco exposto (Anexo.id da API).';
COMMENT ON COLUMN project_attachments.project_id IS 'Projeto.';
COMMENT ON COLUMN project_attachments.file_name IS 'Nome do arquivo enviado (filename da parte multipart), sem diretório; só exibição.';
COMMENT ON COLUMN project_attachments.media_type IS 'Tipo de mídia detectado no servidor (não o declarado pelo cliente).';
COMMENT ON COLUMN project_attachments.size_bytes IS 'Tamanho em bytes (já conferido contra o limite configurado).';
COMMENT ON COLUMN project_attachments.sha256 IS 'SHA-256 do conteúdo (integridade e reprodutibilidade da entrada, RNF-15).';
COMMENT ON COLUMN project_attachments.path IS 'Caminho no volume arquivos gerado pela plataforma (anexos/<projeto>/<anexo>), nunca derivado do nome enviado.';
COMMENT ON COLUMN project_attachments.submitted_by IS 'Quem anexou.';
COMMENT ON COLUMN project_attachments.submitted_at IS 'Quando anexou.';
COMMENT ON COLUMN project_attachments.content_purged_at IS 'Quando o arquivo foi apagado do volume pela anonimização do projeto (policy.data_retention); metadados e hash ficam.';
COMMENT ON CONSTRAINT project_attachments_public_id_uk ON project_attachments IS 'Id público único.';
COMMENT ON CONSTRAINT project_attachments_path_uk ON project_attachments IS 'Um arquivo do volume por anexo.';
COMMENT ON CONSTRAINT project_attachments_name_ck ON project_attachments IS 'Nome de 1 a 255 caracteres, sem separador de diretório (Anexo.file_name).';
COMMENT ON CONSTRAINT project_attachments_type_ck ON project_attachments IS 'Formato tipo/subtipo.';
COMMENT ON CONSTRAINT project_attachments_size_ck ON project_attachments IS 'Tamanho não negativo.';
COMMENT ON CONSTRAINT project_attachments_sha256_ck ON project_attachments IS 'SHA-256 em hexadecimal.';
COMMENT ON CONSTRAINT project_attachments_path_ck ON project_attachments IS 'Caminho gerado pela plataforma, sem .. nem nome do usuário.';
CREATE INDEX project_attachments_project_idx ON project_attachments (project_id, submitted_at);
COMMENT ON INDEX project_attachments_project_idx IS 'Anexos do projeto em ordem de envio (listarAnexos, entrada da onda de Requisitos); cobre a FK.';

-- Anexo só entra com o projeto em criado; depois de gravado, só a anonimização marca o expurgo do conteúdo.
CREATE FUNCTION project_attachments_rules() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'INSERT' THEN
    IF NOT EXISTS (SELECT 1 FROM projects p WHERE p.id = NEW.project_id AND p.state = 'created' FOR SHARE) THEN
      RAISE EXCEPTION 'anexo só com o projeto em criado' USING ERRCODE = '23514', CONSTRAINT = 'project_attachments_rules';
    END IF;
    RETURN NEW;
  END IF;
  IF (to_jsonb(NEW) - 'content_purged_at') <> (to_jsonb(OLD) - 'content_purged_at') OR OLD.content_purged_at IS NOT NULL THEN
    RAISE EXCEPTION 'anexo é imutável (só o expurgo do conteúdo, uma vez)' USING ERRCODE = '23514', CONSTRAINT = 'project_attachments_rules';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER project_attachments_rules BEFORE INSERT OR UPDATE ON project_attachments
  FOR EACH ROW EXECUTE FUNCTION project_attachments_rules();
COMMENT ON TRIGGER project_attachments_rules ON project_attachments IS 'regra: US-03/CA-4 — anexo entra só no projeto em criado (entrada de Requisitos) e não muda depois; expurgo do conteúdo só pela anonimização.';
CREATE TRIGGER project_attachments_no_delete BEFORE DELETE ON project_attachments
  FOR EACH ROW EXECUTE FUNCTION block_delete();
COMMENT ON TRIGGER project_attachments_no_delete ON project_attachments IS 'Registro do anexo não se apaga (entrada auditável da onda); o arquivo sai na anonimização.';

CREATE TABLE workflow_configurations (
  project_id        bigint      NOT NULL,
  workflow_id       text        NOT NULL DEFAULT 'workflow-padrao',
  workflow_version   integer     NOT NULL,
  mode              text        NOT NULL DEFAULT 'fixed',
  definition         jsonb       NOT NULL,
  definition_hash    text        NOT NULL,
  pinned_at         timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT workflow_configurations_pkey PRIMARY KEY (project_id),
  CONSTRAINT workflow_configurations_project_fk FOREIGN KEY (project_id) REFERENCES projects (id) ON DELETE RESTRICT,
  CONSTRAINT workflow_configurations_version_ck CHECK (workflow_version >= 1),
  CONSTRAINT workflow_configurations_mode_ck CHECK (mode = 'fixed'),
  CONSTRAINT workflow_configurations_hash_ck CHECK (definition_hash ~ '^[0-9a-f]{64}$')
);
COMMENT ON TABLE workflow_configurations IS 'Workflow fixado no projeto: cópia da definição (workflow-padrao.yaml) usada do início ao fim, para reprodutibilidade (RNF-15).';
COMMENT ON COLUMN workflow_configurations.project_id IS 'Projeto.';
COMMENT ON COLUMN workflow_configurations.workflow_id IS 'Id do workflow (workflow-padrao).';
COMMENT ON COLUMN workflow_configurations.workflow_version IS 'Versão do workflow.';
COMMENT ON COLUMN workflow_configurations.mode IS 'fixed nas Fases 1-2 (US-05/RN-1); configurável é Fase 4.';
COMMENT ON COLUMN workflow_configurations.definition IS 'Definição do workflow em JSON (etapas, depende_de, saídas, gates, retornos).';
COMMENT ON COLUMN workflow_configurations.definition_hash IS 'SHA-256 da definição.';
COMMENT ON COLUMN workflow_configurations.pinned_at IS 'Quando foi fixado.';
COMMENT ON CONSTRAINT workflow_configurations_version_ck ON workflow_configurations IS 'Versão positiva.';
COMMENT ON CONSTRAINT workflow_configurations_mode_ck ON workflow_configurations IS 'regra: US-05/RN-1 — workflow fixed, nenhuma etapa pulada nas Fases 1 e 2.';
COMMENT ON CONSTRAINT workflow_configurations_hash_ck ON workflow_configurations IS 'SHA-256 em hexadecimal.';

CREATE TABLE gate_configurations (
  id bigint GENERATED ALWAYS AS IDENTITY,
  project_id            bigint      NOT NULL,
  gate                  text        NOT NULL,
  approval_mode        text        NOT NULL,
  promoted_checks  text[]      NOT NULL DEFAULT '{}',
  defined_by          bigint      NULL,
  defined_at           timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT gate_configurations_pkey PRIMARY KEY (id),
  CONSTRAINT gate_configurations_project_gate_uk UNIQUE (project_id, gate),
  CONSTRAINT gate_configurations_project_fk FOREIGN KEY (project_id) REFERENCES projects (id) ON DELETE RESTRICT,
  CONSTRAINT gate_configurations_defined_by_fk FOREIGN KEY (defined_by) REFERENCES users (id) ON DELETE RESTRICT,
  CONSTRAINT gate_configurations_gate_ck CHECK (gate IN ('gate.requirements','gate.architecture','gate.data','gate.design','gate.validation','gate.implementation','gate.qa','gate.security','gate.release')),
  CONSTRAINT gate_configurations_mode_ck CHECK (approval_mode IN ('human','automatic')),
  CONSTRAINT gate_configurations_allowed_mode_ck CHECK (approval_mode = 'human' OR gate IN ('gate.validation','gate.implementation'))
);
COMMENT ON TABLE gate_configurations IS 'Configuração do gate no projeto: modo de aprovação (só onde gates.yaml permite) e checagens de alerta promovidas a bloqueante.';
COMMENT ON COLUMN gate_configurations.id IS 'Identificador interno (PK inteira, ADR-18 v2).';
COMMENT ON COLUMN gate_configurations.project_id IS 'Projeto.';
COMMENT ON COLUMN gate_configurations.gate IS 'Id do gate (gates.yaml).';
COMMENT ON COLUMN gate_configurations.approval_mode IS 'human ou automatic (US-07).';
COMMENT ON COLUMN gate_configurations.promoted_checks IS 'Checagens de alerta tornadas bloqueantes no projeto (configuracao_por_projeto.promover_alerta; nunca o contrário).';
COMMENT ON COLUMN gate_configurations.defined_by IS 'Responsável que definiu; nulo = padrão de gates.yaml.';
COMMENT ON COLUMN gate_configurations.defined_at IS 'Quando definiu.';
COMMENT ON CONSTRAINT gate_configurations_project_gate_uk ON gate_configurations IS 'Chave natural: uma linha por (project_id, gate); antiga PK, mantida como UNIQUE.';
COMMENT ON CONSTRAINT gate_configurations_gate_ck ON gate_configurations IS 'Ids de gate do workflow padrão.';
COMMENT ON CONSTRAINT gate_configurations_mode_ck ON gate_configurations IS 'Modos de aprovação.';
COMMENT ON CONSTRAINT gate_configurations_allowed_mode_ck ON gate_configurations IS 'regra: US-07/CA-3 — automático só em gate.validation e gate.implementation; Release sempre humano (US-07/RN-1).';

CREATE TABLE policy_parameters (
  id              bigint      GENERATED ALWAYS AS IDENTITY,
  organization_id  bigint      NOT NULL,
  project_id      bigint      NULL,
  stage           text        NULL,
  policy        text        NOT NULL,
  parameter       text        NOT NULL,
  value           jsonb       NOT NULL,
  defined_by    bigint      NOT NULL,
  defined_at     timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT policy_parameters_pkey PRIMARY KEY (id),
  CONSTRAINT policy_parameters_organization_fk FOREIGN KEY (organization_id) REFERENCES organizations (id) ON DELETE RESTRICT,
  CONSTRAINT policy_parameters_project_fk FOREIGN KEY (project_id) REFERENCES projects (id) ON DELETE RESTRICT,
  CONSTRAINT policy_parameters_defined_by_fk FOREIGN KEY (defined_by) REFERENCES users (id) ON DELETE RESTRICT,
  CONSTRAINT policy_parameters_uk UNIQUE NULLS NOT DISTINCT (organization_id, project_id, stage, policy, parameter),
  CONSTRAINT policy_parameters_policy_ck CHECK (policy ~ '^policy\.[a-z][a-z0-9_]*$'),
  CONSTRAINT policy_parameters_parameter_ck CHECK (parameter ~ '^[a-z][a-z0-9_]*$'),
  CONSTRAINT policy_parameters_stage_ck CHECK (stage IS NULL OR (project_id IS NOT NULL AND stage IN ('requirements','architecture','data','design','validation','implementation','qa','security','release','qa-plan','security-threats')))
);
COMMENT ON TABLE policy_parameters IS 'Valor de parâmetro de política sobrescrito pela organização, pelo projeto ou pela etapa do projeto (modifiable_by de politicas.yaml). Sem linha vale o padrão do YAML; a faixa é conferida pelo motor de governança.';
COMMENT ON COLUMN policy_parameters.id IS 'Identificador interno.';
COMMENT ON COLUMN policy_parameters.organization_id IS 'Organização.';
COMMENT ON COLUMN policy_parameters.project_id IS 'Projeto; nulo = valor da organização.';
COMMENT ON COLUMN policy_parameters.stage IS 'Etapa do projeto (ex.: execution_timeout por etapa); nulo = projeto inteiro.';
COMMENT ON COLUMN policy_parameters.policy IS 'Id da política (policy.wave_timeout…).';
COMMENT ON COLUMN policy_parameters.parameter IS 'Nome do parâmetro (max_retries…).';
COMMENT ON COLUMN policy_parameters.value IS 'Valor em JSON no tipo declarado no YAML.';
COMMENT ON COLUMN policy_parameters.defined_by IS 'Quem definiu.';
COMMENT ON COLUMN policy_parameters.defined_at IS 'Quando definiu.';
COMMENT ON CONSTRAINT policy_parameters_uk ON policy_parameters IS 'Um valor por escopo e parâmetro.';
COMMENT ON CONSTRAINT policy_parameters_policy_ck ON policy_parameters IS 'Formato do id de política.';
COMMENT ON CONSTRAINT policy_parameters_parameter_ck ON policy_parameters IS 'Formato do nome de parâmetro.';
COMMENT ON CONSTRAINT policy_parameters_stage_ck ON policy_parameters IS 'Etapa só no escopo de projeto e com id do workflow.';
CREATE INDEX policy_parameters_project_idx ON policy_parameters (project_id) WHERE project_id IS NOT NULL;
COMMENT ON INDEX policy_parameters_project_idx IS 'Carga dos parâmetros efetivos do projeto no despacho de cada onda (timeouts, retries, parada do modo automático); cobre a FK project_id.';

-- =============================================================================
-- 4. Etapas
-- =============================================================================
CREATE TABLE stages (
  id bigint GENERATED ALWAYS AS IDENTITY,
  project_id              bigint      NOT NULL,
  stage                   text        NOT NULL,
  type                    text        NOT NULL,
  state                  text        NOT NULL DEFAULT 'blocked',
  required_for        text        NULL,
  required_for_stage_id bigint      NULL,
  reevaluation_pending    boolean     NOT NULL DEFAULT false,
  reopened_by_finding_id  bigint      NULL,
  completed_at            timestamptz NULL,
  updated_at           timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT stages_pkey PRIMARY KEY (id),
  CONSTRAINT stages_project_stage_uk UNIQUE (project_id, stage),
  CONSTRAINT stages_id_project_stage_uk UNIQUE (id, project_id, stage),
  CONSTRAINT stages_project_fk FOREIGN KEY (project_id) REFERENCES projects (id) ON DELETE RESTRICT,
  CONSTRAINT stages_required_for_fk FOREIGN KEY (required_for_stage_id, project_id, required_for) REFERENCES stages (id, project_id, stage) ON DELETE RESTRICT,
  CONSTRAINT stages_stage_ck CHECK (stage IN ('requirements','architecture','data','design','validation','implementation','qa','security','release','qa-plan','security-threats')),
  CONSTRAINT stages_type_ck CHECK ((type = 'parallel_activity') = (stage IN ('qa-plan','security-threats')) AND type IN ('stage','parallel_activity')),
  CONSTRAINT stages_state_ck CHECK (CASE type
      WHEN 'stage' THEN state IN ('blocked','ready','running','awaiting_gate','approved','returned','reopened','skipped')
      ELSE state IN ('blocked','ready','running','completed') END),
  CONSTRAINT stages_required_for_ck CHECK ((type = 'parallel_activity') = (required_for IS NOT NULL) AND (required_for IS NULL) = (required_for_stage_id IS NULL) AND required_for IS DISTINCT FROM stage AND required_for_stage_id IS DISTINCT FROM id),
  CONSTRAINT stages_reevaluation_ck CHECK (NOT reevaluation_pending OR state = 'approved'),
  CONSTRAINT stages_reopened_ck CHECK (state IS DISTINCT FROM 'reopened' OR reopened_by_finding_id IS NOT NULL),
  CONSTRAINT stages_completed_ck CHECK ((completed_at IS NULL OR type = 'parallel_activity') AND (state <> 'completed' OR completed_at IS NOT NULL))
);
COMMENT ON TABLE stages IS 'Etapa do workflow (máquina etapa) ou atividade paralela (qa-plan, security-threats; máquina atividade, D-27) num projeto. A atividade não tem gate próprio nem eventos: seu estado serve à guarda de submit_to_gate da etapa de required_for.';
COMMENT ON COLUMN stages.id IS 'Identificador interno (PK inteira, ADR-18 v2).';
COMMENT ON COLUMN stages.project_id IS 'Projeto.';
COMMENT ON COLUMN stages.stage IS 'Id da etapa ou atividade no workflow.';
COMMENT ON COLUMN stages.type IS 'stage ou parallel_activity.';
COMMENT ON COLUMN stages.state IS 'Estado na máquina etapa (tipo etapa) ou na máquina atividade (tipo parallel_activity).';
COMMENT ON COLUMN stages.required_for_stage_id IS 'stages.id da etapa exigida (FK composta com project_id e required_for: o par texto/inteiro nunca diverge).';
COMMENT ON COLUMN stages.required_for IS 'Atividade paralela: etapa cujo submit_to_gate exige esta atividade concluida (required_for de workflow-padrao.yaml, copiado ao instanciar); nulo em etapa.';
COMMENT ON COLUMN stages.reevaluation_pending IS 'Etapa aprovada marcada para reavaliação por mudança em dependência (stage.reevaluation_pending).';
COMMENT ON COLUMN stages.reopened_by_finding_id IS 'ACH que reabriu a etapa; guarda de submit_to_gate confere os ACH da reabertura.';
COMMENT ON COLUMN stages.completed_at IS 'Atividade paralela: instante da última transição para concluida (marca de tempo; fica ao voltar a executar).';
COMMENT ON COLUMN stages.updated_at IS 'Última mudança.';
COMMENT ON CONSTRAINT stages_project_stage_uk ON stages IS 'Chave natural: uma linha por (project_id, stage); antiga PK, mantida como UNIQUE.';
COMMENT ON CONSTRAINT stages_required_for_fk ON stages IS 'A etapa exigida existe no projeto: referencia stages.id e confere project_id e required_for.';
COMMENT ON CONSTRAINT stages_id_project_stage_uk ON stages IS 'Alvo das FKs de stage_id (waves, gate_rounds, stages.required_for_stage_id); garante que id, projeto e etapa nunca divergem.';
COMMENT ON CONSTRAINT stages_stage_ck ON stages IS 'Ids de etapa e atividade paralela de workflow-padrao.yaml.';
COMMENT ON CONSTRAINT stages_type_ck ON stages IS 'Tipo coerente com o id.';
COMMENT ON CONSTRAINT stages_state_ck ON stages IS 'Estados da máquina etapa ou da máquina atividade, conforme o tipo (D-27).';
COMMENT ON CONSTRAINT stages_required_for_ck ON stages IS 'Só atividade paralela tem required_for, e nunca ela mesma.';
COMMENT ON CONSTRAINT stages_reevaluation_ck ON stages IS 'Reavaliação pendente só em etapa approved.';
COMMENT ON CONSTRAINT stages_reopened_ck ON stages IS 'regra: US-34/CA-1 — etapa reopened aponta o ACH que a reabriu.';
COMMENT ON CONSTRAINT stages_completed_ck ON stages IS 'completed_at só em atividade paralela e presente quando completed.';

-- Transição de etapa ou atividade: a máquina vem do tipo da linha (D-27).
-- Guarda de submit_to_gate (running → awaiting_gate): atividades de required_for em concluida.
CREATE FUNCTION stages_valid_transition() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  PERFORM check_transition(CASE NEW.type WHEN 'stage' THEN 'stage' ELSE 'activity' END,
                            CASE WHEN TG_OP = 'INSERT' THEN NULL ELSE OLD.state END, NEW.state);
  IF NEW.type = 'stage' AND NEW.state = 'awaiting_gate' AND (TG_OP = 'INSERT' OR OLD.state <> 'awaiting_gate')
     AND EXISTS (SELECT 1 FROM stages a
                  WHERE a.project_id = NEW.project_id AND a.required_for_stage_id = NEW.id AND a.state <> 'completed') THEN
    RAISE EXCEPTION 'atividade paralela obrigatória não concluída' USING ERRCODE = '23514', CONSTRAINT = 'stages_valid_transition';
  END IF;
  -- A1: a api aprova a rodada; o worker, ao avançar a etapa, depende do gate aprovado na última rodada.
  IF NEW.type = 'stage' AND NEW.state = 'approved' AND TG_OP = 'UPDATE' AND OLD.state = 'awaiting_gate'
     AND NOT EXISTS (SELECT 1 FROM gate_rounds r
                      WHERE r.project_id = NEW.project_id AND r.stage = NEW.stage AND r.state = 'approved'
                        AND r.number = (SELECT max(number) FROM gate_rounds x WHERE x.project_id = NEW.project_id AND x.stage = NEW.stage)) THEN
    RAISE EXCEPTION 'etapa só fica aprovada com a última rodada do gate aprovada' USING ERRCODE = '23514', CONSTRAINT = 'stages_valid_transition';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER stages_valid_transition BEFORE INSERT OR UPDATE OF state ON stages
  FOR EACH ROW EXECUTE FUNCTION stages_valid_transition();
COMMENT ON TRIGGER stages_valid_transition ON stages IS 'Estado inicial bloqueada e só transições de estados.yaml (máquina etapa ou atividade, pelo tipo); pulada é reservada (Fase 4) e recusada; submit_to_gate exige as atividades de required_for concluídas (D-27); awaiting_gate → aprovada exige a última rodada do gate aprovada (decisão síncrona da API, avanço pelo worker).';
CREATE INDEX stages_required_for_idx ON stages (required_for_stage_id) WHERE required_for_stage_id IS NOT NULL;
COMMENT ON INDEX stages_required_for_idx IS 'Guarda de submit_to_gate: atividades obrigatórias da etapa (D-27); cobre a FK required_for_stage_id.';

-- =============================================================================
-- 5. Modelos, tabela de preços, par de modelos e segredos de provedor
-- =============================================================================
CREATE TABLE models (
  id               bigint  GENERATED ALWAYS AS IDENTITY,
  provider         text    NOT NULL,
  name             text    NOT NULL,
  tool_use  boolean NOT NULL,
  context_tokens  integer NOT NULL,
  prompt_cache     boolean NOT NULL,
  image_input   boolean NOT NULL,
  active            boolean NOT NULL DEFAULT true,
  CONSTRAINT models_pkey PRIMARY KEY (id),
  CONSTRAINT models_provider_name_uk UNIQUE (provider, name),
  CONSTRAINT models_provider_ck CHECK (provider ~ '^[a-z][a-z0-9_-]*$'),
  CONSTRAINT models_name_ck CHECK (char_length(name) BETWEEN 1 AND 128),
  CONSTRAINT models_context_ck CHECK (context_tokens > 0)
);
COMMENT ON TABLE models IS 'Matriz de capacidades: um modelo de um provedor do gateway (PROJETO 9.2; US-24/CA-4). Preço fica em model_prices.';
COMMENT ON COLUMN models.id IS 'Identificador interno.';
COMMENT ON COLUMN models.provider IS 'Provedor no gateway (anthropic, openai).';
COMMENT ON COLUMN models.name IS 'Nome do modelo como o gateway registra.';
COMMENT ON COLUMN models.tool_use IS 'Suporta uso de ferramentas (capacidade exigida por policy.eligible_model).';
COMMENT ON COLUMN models.context_tokens IS 'Tamanho de contexto em tokens.';
COMMENT ON COLUMN models.prompt_cache IS 'Suporta cache de prompt.';
COMMENT ON COLUMN models.image_input IS 'Aceita imagem na entrada.';
COMMENT ON COLUMN models.active IS 'Disponível para novas escolhas de par.';
COMMENT ON CONSTRAINT models_provider_name_uk ON models IS 'Um registro por modelo do provedor.';
COMMENT ON CONSTRAINT models_provider_ck ON models IS 'Formato do provedor do payload cost.model_call_recorded.';
COMMENT ON CONSTRAINT models_name_ck ON models IS 'Tamanho do nome do modelo (1..128).';
COMMENT ON CONSTRAINT models_context_ck ON models IS 'Contexto positivo.';

CREATE TABLE model_prices (
  id                   bigint        GENERATED ALWAYS AS IDENTITY,
  organization_id       bigint        NOT NULL,
  model_id            bigint        NOT NULL,
  effective_since        timestamptz   NOT NULL,
  currency                char(3)       NOT NULL,
  input_price        numeric(18,8) NOT NULL,
  output_price          numeric(18,8) NOT NULL,
  read_cache_price  numeric(18,8) NOT NULL,
  write_cache_price  numeric(18,8) NOT NULL,
  source                text          NOT NULL,
  recorded_by       bigint        NOT NULL,
  recorded_at        timestamptz   NOT NULL DEFAULT now(),
  fixed_at         timestamptz   NULL,
  CONSTRAINT model_prices_pkey PRIMARY KEY (id),
  CONSTRAINT model_prices_organization_fk FOREIGN KEY (organization_id) REFERENCES organizations (id) ON DELETE RESTRICT,
  CONSTRAINT model_prices_model_fk FOREIGN KEY (model_id) REFERENCES models (id) ON DELETE RESTRICT,
  CONSTRAINT model_prices_recorded_by_fk FOREIGN KEY (recorded_by) REFERENCES users (id) ON DELETE RESTRICT,
  CONSTRAINT model_prices_effective_period_uk UNIQUE (organization_id, model_id, effective_since),
  CONSTRAINT model_prices_currency_ck CHECK (currency ~ '^[A-Z]{3}$'),
  CONSTRAINT model_prices_values_ck CHECK (input_price >= 0 AND output_price >= 0 AND read_cache_price >= 0 AND write_cache_price >= 0),
  CONSTRAINT model_prices_source_ck CHECK (char_length(btrim(source)) BETWEEN 1 AND 500)
);
COMMENT ON TABLE model_prices IS 'Tabela de preços versionada (PROJETO 10): cada linha é uma vigência do preço de um modelo na organização. Vigência futura ainda não usada pode ser corrigida (US-37/CA-3); depois de começar a valer, é imutável. Preço vigente numa data = linha com o maior effective_since <= data. A versão citada nos eventos (tabela_precos_versao) é o id da linha em texto.';
COMMENT ON COLUMN model_prices.id IS 'Identificador; é a tabela_precos_versao dos eventos de custo.';
COMMENT ON COLUMN model_prices.organization_id IS 'Organização (preço negociado pode variar).';
COMMENT ON COLUMN model_prices.model_id IS 'Modelo.';
COMMENT ON COLUMN model_prices.effective_since IS 'Início da vigência; nunca no passado, nem na correção (não muda custo já calculado).';
COMMENT ON COLUMN model_prices.currency IS 'Moeda do preço (a da organização; ver lacuna de câmbio).';
COMMENT ON COLUMN model_prices.input_price IS 'Preço por 1.000.000 de tokens de entrada.';
COMMENT ON COLUMN model_prices.output_price IS 'Preço por 1.000.000 de tokens de saída.';
COMMENT ON COLUMN model_prices.read_cache_price IS 'Preço por 1.000.000 de tokens lidos do cache de prompt.';
COMMENT ON COLUMN model_prices.write_cache_price IS 'Preço por 1.000.000 de tokens gravados no cache de prompt.';
COMMENT ON COLUMN model_prices.recorded_by IS 'Administrador que registrou.';
COMMENT ON COLUMN model_prices.recorded_at IS 'Quando registrou.';
COMMENT ON COLUMN model_prices.source IS 'Origem do valor (página oficial de preços e data da consulta; PrecoModelo.fonte da API, PROJETO 10).';
COMMENT ON COLUMN model_prices.fixed_at IS 'Última correção da vigência ainda futura (corrigirPrecoModelo); nulo se nunca corrigida. Quem corrigiu fica na auditoria.';
COMMENT ON CONSTRAINT model_prices_source_ck ON model_prices IS 'regra: US-37/CA-1 — fonte do preço obrigatória (até 500 caracteres).';
COMMENT ON CONSTRAINT model_prices_effective_period_uk ON model_prices IS 'regra: US-37/CA-1 — uma vigência por modelo e instante; serve a busca do preço vigente.';
COMMENT ON CONSTRAINT model_prices_currency_ck ON model_prices IS 'Código ISO 4217.';
COMMENT ON CONSTRAINT model_prices_values_ck ON model_prices IS 'Preços não negativos.';

-- Correção (UPDATE) só de vigência que ainda não começou e nunca foi citada por chamada, e para um início ainda
-- futuro (nenhuma chamada pode usá-la durante a correção); organização, modelo e autoria não mudam.
CREATE FUNCTION model_prices_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'UPDATE' THEN
    IF OLD.effective_since <= now() OR NEW.effective_since <= now()
       OR EXISTS (SELECT 1 FROM model_calls c WHERE c.price_id = OLD.id)
       OR (NEW.organization_id, NEW.model_id, NEW.recorded_by, NEW.recorded_at)
          IS DISTINCT FROM (OLD.organization_id, OLD.model_id, OLD.recorded_by, OLD.recorded_at) THEN
      RAISE EXCEPTION 'vigência já em vigor ou usada é imutável; registre nova vigência'
        USING ERRCODE = '23514', CONSTRAINT = 'model_prices_immutable';
    END IF;
    NEW.fixed_at := now();
  END IF;
  IF NEW.effective_since < now() - interval '1 minute' THEN
    RAISE EXCEPTION 'vigência de preço não pode começar no passado'
      USING ERRCODE = '23514', CONSTRAINT = 'model_prices_immutable';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER model_prices_immutable BEFORE INSERT OR UPDATE ON model_prices
  FOR EACH ROW EXECUTE FUNCTION model_prices_immutable();
COMMENT ON TRIGGER model_prices_immutable ON model_prices IS 'regra: US-37/CA-3 — vigência em vigor ou já usada não se altera, só se cria nova; vigência futura não usada pode ser corrigida; nenhuma retroage (US-37/CA-1, RNF-16).';
CREATE TRIGGER model_prices_no_delete BEFORE DELETE ON model_prices
  FOR EACH ROW EXECUTE FUNCTION block_delete();
COMMENT ON TRIGGER model_prices_no_delete ON model_prices IS 'Vigência citada por chamadas não se apaga (RNF-16).';

CREATE TABLE model_pairs (
  id                   bigint      GENERATED ALWAYS AS IDENTITY,
  project_id           bigint      NOT NULL,
  stage                text        NULL,
  reasoning_model_id bigint      NOT NULL,
  execution_model_id   bigint      NOT NULL,
  defined_by         bigint      NOT NULL,
  defined_at          timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT model_pairs_pkey PRIMARY KEY (id),
  CONSTRAINT model_pairs_project_fk FOREIGN KEY (project_id) REFERENCES projects (id) ON DELETE RESTRICT,
  CONSTRAINT model_pairs_reasoning_fk FOREIGN KEY (reasoning_model_id) REFERENCES models (id) ON DELETE RESTRICT,
  CONSTRAINT model_pairs_execution_fk FOREIGN KEY (execution_model_id) REFERENCES models (id) ON DELETE RESTRICT,
  CONSTRAINT model_pairs_defined_by_fk FOREIGN KEY (defined_by) REFERENCES users (id) ON DELETE RESTRICT,
  CONSTRAINT model_pairs_scope_uk UNIQUE NULLS NOT DISTINCT (project_id, stage),
  CONSTRAINT model_pairs_stage_ck CHECK (stage IS NULL OR stage IN ('requirements','architecture','data','design','validation','implementation','qa','security','release','qa-plan','security-threats'))
);
COMMENT ON TABLE model_pairs IS 'Par de modelos (raciocínio × execução) do projeto ou de uma etapa (US-16). A onda grava o par com que começou em wave_models (US-16/CA-4).';
COMMENT ON COLUMN model_pairs.id IS 'Identificador interno.';
COMMENT ON COLUMN model_pairs.project_id IS 'Projeto.';
COMMENT ON COLUMN model_pairs.stage IS 'Etapa com ajuste próprio; nulo = par do projeto (US-16/CA-3).';
COMMENT ON COLUMN model_pairs.reasoning_model_id IS 'Modelo de raciocínio (planeja, decide, revisa).';
COMMENT ON COLUMN model_pairs.execution_model_id IS 'Modelo de execução (produz em volume).';
COMMENT ON COLUMN model_pairs.defined_by IS 'Responsável que definiu.';
COMMENT ON COLUMN model_pairs.defined_at IS 'Quando definiu.';
COMMENT ON CONSTRAINT model_pairs_scope_uk ON model_pairs IS 'Um par por projeto e um por etapa.';
COMMENT ON CONSTRAINT model_pairs_stage_ck ON model_pairs IS 'Ids do workflow.';

CREATE TABLE provider_keys (
  id                  bigint      GENERATED ALWAYS AS IDENTITY,
  organization_id      bigint      NOT NULL,
  provider            text        NOT NULL,
  state              text        NOT NULL DEFAULT 'testing',
  encrypted_value       bytea       NULL,
  encryption_key_ref  text        NOT NULL,
  last4            text        NOT NULL,
  test_result     text        NULL,
  test_expires_at     timestamptz NOT NULL DEFAULT now() + interval '15 seconds',
  tested_at          timestamptz NULL,
  created_by          bigint      NOT NULL,
  created_at           timestamptz NOT NULL DEFAULT now(),
  removed_at         timestamptz NULL,
  CONSTRAINT provider_keys_pkey PRIMARY KEY (id),
  CONSTRAINT provider_keys_organization_fk FOREIGN KEY (organization_id) REFERENCES organizations (id) ON DELETE RESTRICT,
  CONSTRAINT provider_keys_created_by_fk FOREIGN KEY (created_by) REFERENCES users (id) ON DELETE RESTRICT,
  CONSTRAINT provider_keys_provider_ck CHECK (provider ~ '^[a-z][a-z0-9_-]*$'),
  CONSTRAINT provider_keys_state_ck CHECK (state IN ('testing','active','discarded','replaced','removed')),
  CONSTRAINT provider_keys_encrypted_ck CHECK ((state IN ('testing','active')) = (encrypted_value IS NOT NULL)),
  CONSTRAINT provider_keys_last4_ck CHECK (char_length(last4) = 4),
  CONSTRAINT provider_keys_removed_ck CHECK ((state = 'removed') = (removed_at IS NOT NULL)),
  CONSTRAINT provider_keys_test_ck CHECK ((state = 'testing') = (tested_at IS NULL)
                                               AND (state <> 'discarded' OR btrim(coalesce(test_result, '')) <> '')
                                               AND test_expires_at > created_at AND test_expires_at <= created_at + interval '15 seconds')
);
COMMENT ON TABLE provider_keys IS 'Chave de provedor de IA da organização (BYOK), cifrada em repouso por envelope: a API sela com a chave pública; só o processo easycode-gateway tem a chave privada, fora do banco e das cópias de segurança (RNF-01, ADR-13). Ciclo (US-15): a API grava a linha testing com o valor selado e o comando test_provider_key; o teste ok leva a ativa (a ativa anterior do provedor vai a substituida na mesma transação); falha ou prazo de 15 s vencido leva a descartada com o valor cifrado apagado. Nenhum estado guarda chave inválida. Demais tabelas só referenciam o provedor.';
COMMENT ON COLUMN provider_keys.id IS 'Identificador.';
COMMENT ON COLUMN provider_keys.organization_id IS 'Organização dona da chave.';
COMMENT ON COLUMN provider_keys.provider IS 'Provedor do gateway.';
COMMENT ON COLUMN provider_keys.state IS 'testing (chamada mínima pendente), active (teste ok; usada pelo gateway), discarded (teste failed ou expirou; sem valor), replaced ou removed (US-15).';
COMMENT ON COLUMN provider_keys.encrypted_value IS 'Chave selada (envelope); presente só em testing e ativa, apagada ao descartar, substituir ou remover; o papel da API não a lê (privilégio por coluna) e ela nunca vai ao payload do comando.';
COMMENT ON COLUMN provider_keys.encryption_key_ref IS 'Id e versão da chave de cifragem usada (rotação), nunca a chave.';
COMMENT ON COLUMN provider_keys.last4 IS 'Últimos 4 caracteres exibíveis (US-15/CA-1).';
COMMENT ON COLUMN provider_keys.test_result IS 'Resultado resumido do teste, sem a chave: erro do provedor ou tempo esgotado; obrigatório em descartada (US-15/CA-3).';
COMMENT ON COLUMN provider_keys.test_expires_at IS 'Prazo do teste (15 s após gravar); vencido sem resultado, a linha vai a descartada (a API responde sem salvar a chave).';
COMMENT ON COLUMN provider_keys.tested_at IS 'Quando o teste terminou (ok, falha ou prazo vencido); nulo só em testing.';
COMMENT ON COLUMN provider_keys.created_by IS 'Administrador que cadastrou.';
COMMENT ON COLUMN provider_keys.created_at IS 'Quando cadastrou.';
COMMENT ON COLUMN provider_keys.removed_at IS 'Quando foi removida.';
COMMENT ON CONSTRAINT provider_keys_provider_ck ON provider_keys IS 'Formato do provedor.';
COMMENT ON CONSTRAINT provider_keys_state_ck ON provider_keys IS 'Ciclo da chave (US-15).';
COMMENT ON CONSTRAINT provider_keys_encrypted_ck ON provider_keys IS 'regra: US-15/CA-3 — chave inválida não fica salva; só a chave em teste ou active guarda o valor cifrado.';
COMMENT ON CONSTRAINT provider_keys_test_ck ON provider_keys IS 'Teste terminado tem data; descarte tem o motivo resumido; prazo do teste até 15 s.';
COMMENT ON CONSTRAINT provider_keys_last4_ck ON provider_keys IS 'regra: US-15/CA-1 — exibe só os 4 últimos caracteres.';
COMMENT ON CONSTRAINT provider_keys_removed_ck ON provider_keys IS 'Data de remoção só no estado removed.';
CREATE UNIQUE INDEX provider_keys_active_uk ON provider_keys (organization_id, provider) WHERE state = 'active';
COMMENT ON INDEX provider_keys_active_uk IS 'Uma chave ativa por provedor e organização; busca do gateway a cada despacho (US-15/CA-4 "provedor sem chave").';
CREATE UNIQUE INDEX provider_keys_testing_uk ON provider_keys (organization_id, provider) WHERE state = 'testing';
COMMENT ON INDEX provider_keys_testing_uk IS 'Um teste de chave por vez por provedor e organização; serve a varredura de testes vencidos (test_expires_at).';

-- Ciclo da chave: nasce testing; testing → ativa | descartada; ativa → substituida | removida; os demais são finais.
CREATE FUNCTION provider_keys_cycle() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'INSERT' THEN
    IF NEW.state <> 'testing' THEN
      RAISE EXCEPTION 'chave nasce testing' USING ERRCODE = '23514', CONSTRAINT = 'provider_keys_cycle';
    END IF;
  ELSIF NEW.state IS DISTINCT FROM OLD.state
        AND NOT ((OLD.state = 'testing' AND NEW.state IN ('active','discarded'))
                 OR (OLD.state = 'active' AND NEW.state IN ('replaced','removed'))) THEN
    RAISE EXCEPTION 'transição inválida da chave: % -> %', OLD.state, NEW.state USING ERRCODE = '23514', CONSTRAINT = 'provider_keys_cycle';
  ELSIF NEW.state = 'active' AND OLD.state = 'testing' AND NEW.tested_at > OLD.test_expires_at THEN
    RAISE EXCEPTION 'teste da chave terminou depois do prazo' USING ERRCODE = '23514', CONSTRAINT = 'provider_keys_cycle';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER provider_keys_cycle BEFORE INSERT OR UPDATE ON provider_keys
  FOR EACH ROW EXECUTE FUNCTION provider_keys_cycle();
COMMENT ON TRIGGER provider_keys_cycle ON provider_keys IS 'regra: US-15/CA-1 — chave só fica ativa após o teste ok dentro do prazo; falha ou prazo vencido descarta (US-15/CA-3).';
CREATE TRIGGER provider_keys_no_delete BEFORE DELETE ON provider_keys
  FOR EACH ROW EXECUTE FUNCTION block_delete();
COMMENT ON TRIGGER provider_keys_no_delete ON provider_keys IS 'Linha da chave não se apaga (auditoria da troca); o valor cifrado é que sai.';

-- =============================================================================
-- 6. Agentes e skills
-- =============================================================================
CREATE TABLE agent_configurations (
  id                bigint      GENERATED ALWAYS AS IDENTITY,
  project_id        bigint      NOT NULL,
  agent            text        NOT NULL,
  version            integer     NOT NULL,
  instruction_version  text        NOT NULL,
  instruction_hash    text        NOT NULL,
  tools       jsonb       NOT NULL,
  limits           jsonb       NOT NULL,
  output_criteria   jsonb       NOT NULL DEFAULT '[]',
  created_by        bigint      NULL,
  created_at         timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT agent_configurations_pkey PRIMARY KEY (id),
  CONSTRAINT agent_configurations_project_fk FOREIGN KEY (project_id) REFERENCES projects (id) ON DELETE RESTRICT,
  CONSTRAINT agent_configurations_created_by_fk FOREIGN KEY (created_by) REFERENCES users (id) ON DELETE RESTRICT,
  CONSTRAINT agent_configurations_version_uk UNIQUE (project_id, agent, version),
  CONSTRAINT agent_configurations_agent_ck CHECK (agent IN ('requirements-agent','architect-agent','dba-agent','designer-agent','developer-agent','qa-agent','security-agent')),
  CONSTRAINT agent_configurations_version_ck CHECK (version >= 1),
  CONSTRAINT agent_configurations_hash_ck CHECK (instruction_hash ~ '^[0-9a-f]{64}$')
);
COMMENT ON TABLE agent_configurations IS 'Versão imutável da configuração de um agente no projeto: instruções, ferramentas (recorte de permissoes.yaml, nunca ampliação), limites e critérios de saída (US-25).';
COMMENT ON COLUMN agent_configurations.id IS 'Identificador; a onda aponta a versão usada (US-25/CA-4).';
COMMENT ON COLUMN agent_configurations.project_id IS 'Projeto.';
COMMENT ON COLUMN agent_configurations.agent IS 'Agente.';
COMMENT ON COLUMN agent_configurations.version IS 'Versão; salvar cria nova (US-25/CA-2).';
COMMENT ON COLUMN agent_configurations.instruction_version IS 'Versão do arquivo de instrução (ex.: agentes/instrucoes/dba.md v0).';
COMMENT ON COLUMN agent_configurations.instruction_hash IS 'SHA-256 do texto da instrução entregue (RNF-15).';
COMMENT ON COLUMN agent_configurations.tools IS 'Ferramentas, caminhos e comandos autorizados (RNF-07).';
COMMENT ON COLUMN agent_configurations.limits IS 'Limites de CPU, memória, disco e tempo (RNF-03).';
COMMENT ON COLUMN agent_configurations.output_criteria IS 'Critérios de saída exibidos (US-25/CA-1).';
COMMENT ON COLUMN agent_configurations.created_by IS 'Responsável que salvou; nulo = padrão da plataforma.';
COMMENT ON COLUMN agent_configurations.created_at IS 'Quando salvou.';
COMMENT ON CONSTRAINT agent_configurations_version_uk ON agent_configurations IS 'regra: US-25/CA-2 — versões numeradas por agente e projeto.';
COMMENT ON CONSTRAINT agent_configurations_agent_ck ON agent_configurations IS 'Os 7 agentes.';
COMMENT ON CONSTRAINT agent_configurations_version_ck ON agent_configurations IS 'Versão positiva.';
COMMENT ON CONSTRAINT agent_configurations_hash_ck ON agent_configurations IS 'SHA-256 em hexadecimal.';

CREATE FUNCTION block_update() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION 'registro imutável em %', TG_TABLE_NAME
    USING ERRCODE = '23514', CONSTRAINT = TG_TABLE_NAME || '_immutable';
END $$;
CREATE TRIGGER agent_configurations_immutable BEFORE UPDATE ON agent_configurations
  FOR EACH ROW EXECUTE FUNCTION block_update();
COMMENT ON TRIGGER agent_configurations_immutable ON agent_configurations IS 'regra: US-25/CA-2 — versão salva não muda; ondas anteriores mantêm a delas.';

CREATE TABLE skills (
  id              bigint      GENERATED ALWAYS AS IDENTITY,
  organization_id  bigint      NULL,
  name            text        NOT NULL,
  origin          text        NOT NULL,
  created_by      bigint      NULL,
  created_at       timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT skills_pkey PRIMARY KEY (id),
  CONSTRAINT skills_organization_fk FOREIGN KEY (organization_id) REFERENCES organizations (id) ON DELETE RESTRICT,
  CONSTRAINT skills_created_by_fk FOREIGN KEY (created_by) REFERENCES users (id) ON DELETE RESTRICT,
  CONSTRAINT skills_name_uk UNIQUE NULLS NOT DISTINCT (organization_id, name),
  CONSTRAINT skills_name_ck CHECK (name ~ '^[a-z][a-z0-9-]{0,63}$'),
  CONSTRAINT skills_origin_ck CHECK (origin IN ('native','organization')),
  CONSTRAINT skills_origin_org_ck CHECK ((organization_id IS NULL) = (origin = 'native') AND (origin = 'native') = (created_by IS NULL))
);
COMMENT ON TABLE skills IS 'Skill (Agent Skills): do catálogo nativo (organização nula, já liberada, fora da máquina skill) ou da biblioteca da organização (PROJETO 8.2). A origem do envio (upload ou git_repository) é de cada versão.';
COMMENT ON COLUMN skills.id IS 'Identificador.';
COMMENT ON COLUMN skills.organization_id IS 'Organização dona; nulo = catálogo nativo.';
COMMENT ON COLUMN skills.name IS 'Nome do front-matter (name); é o skill dos eventos e do subject skills/{skill}/versoes/{n}.';
COMMENT ON COLUMN skills.origin IS 'native ou organization (Skill.origem da API).';
COMMENT ON COLUMN skills.created_by IS 'Administrador que enviou a primeira versão; nulo no catálogo nativo.';
COMMENT ON COLUMN skills.created_at IS 'Quando foi criada.';
COMMENT ON CONSTRAINT skills_name_uk ON skills IS 'Nome único no catálogo nativo e em cada organização.';
COMMENT ON CONSTRAINT skills_name_ck ON skills IS 'Formato de name do Agent Skills, igual ao padrão skill dos payloads de evento.';
COMMENT ON CONSTRAINT skills_origin_ck ON skills IS 'Origens da skill.';
COMMENT ON CONSTRAINT skills_origin_org_ck ON skills IS 'Só a skill native não tem organização nem autor.';

-- Nome de skill da organização não repete nome do catálogo nativo (e vice-versa): o nome identifica a skill na rota
-- /skills/{skill} e nos eventos sem a origem.
CREATE FUNCTION skills_name_no_collision() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF EXISTS (SELECT 1 FROM skills k
              WHERE k.name = NEW.name AND k.id <> NEW.id
                AND (k.organization_id IS NULL OR NEW.organization_id IS NULL)) THEN
    RAISE EXCEPTION 'nome de skill já usado no catálogo nativo ou numa organização: %', NEW.name
      USING ERRCODE = '23505', CONSTRAINT = 'skills_name_no_collision';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER skills_name_no_collision BEFORE INSERT OR UPDATE OF name ON skills
  FOR EACH ROW EXECUTE FUNCTION skills_name_no_collision();
COMMENT ON TRIGGER skills_name_no_collision ON skills IS 'regra: US-44/CA-1 — skill da organização não usa nome de skill nativa (id de skill único para a organização).';

CREATE TABLE skill_versions (
  id                         bigint      GENERATED ALWAYS AS IDENTITY,
  skill_id                   bigint      NOT NULL,
  version                     integer     NOT NULL,
  submission_origin               text        NULL,
  origin_url                 text        NULL,
  description                  text        NOT NULL,
  stages                     text[]      NOT NULL,
  agents                    text[]      NOT NULL,
  size_tokens             integer     NOT NULL,
  content                   bytea       NOT NULL,
  content_hash              text        NOT NULL,
  status                   text        NOT NULL DEFAULT 'imported',
  block_reasons           text[]      NULL,
  scan_report        jsonb       NULL,
  unblocked_by               bigint      NULL,
  unblocked_at                timestamptz NULL,
  unblock_justification    text        NULL,
  deactivated_by             bigint      NULL,
  deactivated_at              timestamptz NULL,
  deactivation_justification  text        NULL,
  submitted_by                bigint      NULL,
  submitted_at                 timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT skill_versions_pkey PRIMARY KEY (id),
  CONSTRAINT skill_versions_skill_fk FOREIGN KEY (skill_id) REFERENCES skills (id) ON DELETE RESTRICT,
  CONSTRAINT skill_versions_unblocked_by_fk FOREIGN KEY (unblocked_by) REFERENCES users (id) ON DELETE RESTRICT,
  CONSTRAINT skill_versions_deactivated_by_fk FOREIGN KEY (deactivated_by) REFERENCES users (id) ON DELETE RESTRICT,
  CONSTRAINT skill_versions_submitted_by_fk FOREIGN KEY (submitted_by) REFERENCES users (id) ON DELETE RESTRICT,
  CONSTRAINT skill_versions_version_uk UNIQUE (skill_id, version),
  CONSTRAINT skill_versions_id_skill_uk UNIQUE (skill_id, id),
  CONSTRAINT skill_versions_version_ck CHECK (version >= 1),
  CONSTRAINT skill_versions_origin_ck CHECK ((submission_origin IS NULL) = (submitted_by IS NULL)
                                            AND (submission_origin IS NULL OR submission_origin IN ('upload','git_repository'))
                                            AND (submission_origin IS NOT DISTINCT FROM 'git_repository') = (origin_url IS NOT NULL)
                                            AND (origin_url IS NULL OR origin_url ~ '^https://')),
  CONSTRAINT skill_versions_size_ck CHECK (size_tokens BETWEEN 1 AND 200000),
  CONSTRAINT skill_versions_hash_ck CHECK (content_hash ~ '^[0-9a-f]{64}$'),
  CONSTRAINT skill_versions_status_ck CHECK (status IN ('imported','scanning','unblocked','blocked','deactivated')),
  CONSTRAINT skill_versions_reasons_ck CHECK ((block_reasons IS NULL OR (cardinality(block_reasons) > 0 AND block_reasons <@ ARRAY['format','size','malicious_content']))
                                             AND (status <> 'blocked' OR block_reasons IS NOT NULL)
                                             AND (status NOT IN ('imported','scanning') OR block_reasons IS NULL)),
  CONSTRAINT skill_versions_unblock_ck CHECK ((unblocked_by IS NULL) = (unblocked_at IS NULL)
                                               AND (unblocked_by IS NULL) = (unblock_justification IS NULL)
                                               AND (unblock_justification IS NULL OR btrim(unblock_justification) <> '')
                                               AND (unblocked_by IS NULL OR (status IN ('unblocked','deactivated') AND block_reasons = ARRAY['malicious_content']))
                                               AND (status <> 'unblocked' OR block_reasons IS NULL OR unblocked_by IS NOT NULL)),
  CONSTRAINT skill_versions_deactivation_ck CHECK ((status = 'deactivated') = (deactivated_by IS NOT NULL)
                                                 AND (deactivated_by IS NULL) = (deactivated_at IS NULL)
                                                 AND (deactivated_by IS NULL) = (deactivation_justification IS NULL)
                                                 AND (deactivation_justification IS NULL OR btrim(deactivation_justification) <> ''))
);
COMMENT ON TABLE skill_versions IS 'Versão de skill (instância da máquina skill, D-26): metadados da plataforma (etapas, agentes, tamanho), pacote guardado como dado e resultado da varredura (policy.imported_skill). Recusa no envio (400 sem SKILL.md/name/description, 413 acima do limite de transporte) não cria versão: fica em skill_imports.';
COMMENT ON COLUMN skill_versions.id IS 'Identificador.';
COMMENT ON COLUMN skill_versions.skill_id IS 'Skill.';
COMMENT ON COLUMN skill_versions.version IS 'Número da versão (primeira importação = 1; US-44/CA-1); o n de skills/{skill}/versoes/{n}.';
COMMENT ON COLUMN skill_versions.submission_origin IS 'upload ou git_repository (origem dos eventos skill.imported e submission_origin da API); nulo no catálogo nativo.';
COMMENT ON COLUMN skill_versions.origin_url IS 'URL https do repositório git de origem (só git_repository).';
COMMENT ON COLUMN skill_versions.description IS 'Descrição do front-matter; entra primeiro no contexto (US-43/RN-1).';
COMMENT ON COLUMN skill_versions.stages IS 'Etapas onde se aplica.';
COMMENT ON COLUMN skill_versions.agents IS 'Agentes onde se aplica.';
COMMENT ON COLUMN skill_versions.size_tokens IS 'Tamanho estimado em tokens (US-43/CA-1).';
COMMENT ON COLUMN skill_versions.content IS 'Pacote normalizado (pasta compactada); dado não confiável, nunca executado (RNF-05). Toda versão tem pacote, inclusive a bloqueada.';
COMMENT ON COLUMN skill_versions.content_hash IS 'SHA-256 do pacote (sha256 do evento skill.imported; reprodutibilidade, RNF-15).';
COMMENT ON COLUMN skill_versions.status IS 'Estado na máquina skill: imported, scanning, unblocked, blocked ou deactivated (US-44, D-26). Nativa nasce unblocked.';
COMMENT ON COLUMN skill_versions.block_reasons IS 'Motivos do bloqueio na varredura: formato (inclusive SKILL.md inválido baixado do git_repository), tamanho, malicious_content (payload skill.blocked; US-44/CA-2, CA-3). Fica após liberar ou desativar.';
COMMENT ON COLUMN skill_versions.scan_report IS 'Resultado da varredura de conteúdo (achados de padrão suspeito, sem trecho executável).';
COMMENT ON COLUMN skill_versions.unblocked_by IS 'Administrador que liberou a versão bloqueada por conteúdo suspeito (US-44/CA-4); nulo na liberação pela varredura.';
COMMENT ON COLUMN skill_versions.unblocked_at IS 'Quando o administrador liberou.';
COMMENT ON COLUMN skill_versions.unblock_justification IS '[DP] Justificativa da liberação pelo administrador (texto livre humano; US-44/CA-4).';
COMMENT ON COLUMN skill_versions.deactivated_by IS 'Administrador que desativou a versão.';
COMMENT ON COLUMN skill_versions.deactivated_at IS 'Quando desativou.';
COMMENT ON COLUMN skill_versions.deactivation_justification IS '[DP] Justificativa da desativação (texto livre humano).';
COMMENT ON COLUMN skill_versions.submitted_by IS 'Quem enviou; nulo no catálogo nativo.';
COMMENT ON COLUMN skill_versions.submitted_at IS 'Quando a versão foi gravada (publicada_em da API).';
COMMENT ON CONSTRAINT skill_versions_version_uk ON skill_versions IS 'Versões numeradas por skill.';
COMMENT ON CONSTRAINT skill_versions_id_skill_uk ON skill_versions IS 'Alvo da FK composta de project_skills (versão é da skill associada).';
COMMENT ON CONSTRAINT skill_versions_version_ck ON skill_versions IS 'Versão positiva.';
COMMENT ON CONSTRAINT skill_versions_origin_ck ON skill_versions IS 'Origem do envio coerente: nativa sem origem nem autor; URL https só no git_repository.';
COMMENT ON CONSTRAINT skill_versions_size_ck ON skill_versions IS 'regra: US-44/CA-3 — faixa máxima de policy.imported_skill.max_size_tokens (o limite da organização é conferido na varredura).';
COMMENT ON CONSTRAINT skill_versions_hash_ck ON skill_versions IS 'SHA-256 em hexadecimal.';
COMMENT ON CONSTRAINT skill_versions_status_ck ON skill_versions IS 'Estados da máquina skill.';
COMMENT ON CONSTRAINT skill_versions_reasons_ck ON skill_versions IS 'regra: US-44/CA-2 — bloqueio sempre com motivo da enumeração do evento skill.blocked.';
COMMENT ON CONSTRAINT skill_versions_unblock_ck ON skill_versions IS 'regra: US-44/CA-4 — liberação de versão bloqueada registra administrador, data e justificativa, e só quando o único motivo é malicious_content (format ou size: envia-se nova versão).';
COMMENT ON CONSTRAINT skill_versions_deactivation_ck ON skill_versions IS 'Desativação registra administrador, data e justificativa.';
CREATE INDEX skill_versions_blocked_idx ON skill_versions (submitted_at) WHERE status = 'blocked';
COMMENT ON INDEX skill_versions_blocked_idx IS 'Pendências do administrador: versões bloqueadas por idade (US-44/CA-4).';

-- Transição da versão de skill (máquina skill). Versão do catálogo nativo nasce liberada e não muda (fora da máquina).
CREATE FUNCTION skill_versions_valid_transition() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF EXISTS (SELECT 1 FROM skills s WHERE s.id = NEW.skill_id AND s.origin = 'native') THEN
    IF NEW.status <> 'unblocked' THEN
      RAISE EXCEPTION 'skill nativa é sempre liberada' USING ERRCODE = '23514', CONSTRAINT = 'skill_valid_transition';
    END IF;
    RETURN NEW;
  END IF;
  PERFORM check_transition('skill', CASE WHEN TG_OP = 'INSERT' THEN NULL ELSE OLD.status END, NEW.status);
  RETURN NEW;
END $$;
CREATE TRIGGER skill_versions_valid_transition BEFORE INSERT OR UPDATE OF status ON skill_versions
  FOR EACH ROW EXECUTE FUNCTION skill_versions_valid_transition();
COMMENT ON TRIGGER skill_versions_valid_transition ON skill_versions IS 'Estado inicial importada e só transições de estados.yaml (máquina skill, D-26); nativa sempre liberada.';
CREATE TRIGGER skill_versions_no_delete BEFORE DELETE ON skill_versions
  FOR EACH ROW EXECUTE FUNCTION block_delete();
COMMENT ON TRIGGER skill_versions_no_delete ON skill_versions IS 'Versão de skill não se apaga: desativa-se (reprodutibilidade das ondas que a carregaram, RNF-15).';

CREATE TABLE skill_imports (
  id                 bigint      GENERATED ALWAYS AS IDENTITY,
  public_id         uuid        NOT NULL DEFAULT gen_random_uuid(),
  organization_id     bigint      NOT NULL,
  origin             text        NOT NULL,
  git_url            text        NULL,
  git_reference     text        NULL,
  subfolder           text        NULL,
  package             bytea       NULL,
  state             text        NOT NULL DEFAULT 'received',
  refusal             jsonb       NULL,
  skill_version_id    bigint      NULL,
  idempotency_key text        NULL,
  requested_by         bigint      NOT NULL,
  received_at        timestamptz NOT NULL DEFAULT now(),
  completed_at       timestamptz NULL,
  CONSTRAINT skill_imports_pkey PRIMARY KEY (id),
  CONSTRAINT skill_imports_public_id_uk UNIQUE (public_id),
  CONSTRAINT skill_imports_organization_fk FOREIGN KEY (organization_id) REFERENCES organizations (id) ON DELETE RESTRICT,
  CONSTRAINT skill_imports_version_fk FOREIGN KEY (skill_version_id) REFERENCES skill_versions (id) ON DELETE RESTRICT,
  CONSTRAINT skill_imports_requested_by_fk FOREIGN KEY (requested_by) REFERENCES users (id) ON DELETE RESTRICT,
  CONSTRAINT skill_imports_idempotency_uk UNIQUE (organization_id, idempotency_key),
  CONSTRAINT skill_imports_version_uk UNIQUE (skill_version_id),
  CONSTRAINT skill_imports_origin_ck CHECK (origin IN ('upload','git_repository')
                                                AND (origin = 'git_repository') = (git_url IS NOT NULL)
                                                AND (git_url IS NULL OR (git_url ~ '^https://' AND char_length(git_url) <= 2048))
                                                AND (origin = 'git_repository' OR (git_reference IS NULL AND subfolder IS NULL))),
  CONSTRAINT skill_imports_state_ck CHECK (state IN ('received','refused','version_created')),
  CONSTRAINT skill_imports_refusal_ck CHECK ((state = 'refused') = (refusal IS NOT NULL) AND (refusal IS NULL OR jsonb_typeof(refusal) = 'object')),
  CONSTRAINT skill_imports_version_ck CHECK ((state = 'version_created') = (skill_version_id IS NOT NULL)),
  CONSTRAINT skill_imports_completion_ck CHECK ((state = 'received') = (completed_at IS NULL) AND (state = 'received' OR package IS NULL))
);
COMMENT ON TABLE skill_imports IS 'Pedido de importação de skill da organização (ImportacaoSkill da API, US-44): recebida (na fila ou baixando do repositório git), recusada (sem versão, com o Problema) ou version_created (a versão segue na máquina skill).';
COMMENT ON COLUMN skill_imports.id IS 'Identificador interno (chave primária inteira, alvo das FKs); não sai do banco: API e eventos usam public_id.';
COMMENT ON COLUMN skill_imports.public_id IS 'Identificador opaco exposto (importacaoid).';
COMMENT ON COLUMN skill_imports.organization_id IS 'Organização.';
COMMENT ON COLUMN skill_imports.origin IS 'upload ou git_repository.';
COMMENT ON COLUMN skill_imports.git_url IS 'URL https do repositório (só git_repository; egress controlado).';
COMMENT ON COLUMN skill_imports.git_reference IS 'Ramo, marcação ou commit; nulo = ramo principal.';
COMMENT ON COLUMN skill_imports.subfolder IS 'Subpasta da skill no repositório.';
COMMENT ON COLUMN skill_imports.package IS 'Pacote enviado por upload enquanto recebida; esvaziado ao concluir (o pacote aceito vai para skill_versions.content). Dado não confiável, nunca executado.';
COMMENT ON COLUMN skill_imports.state IS 'received, refused ou version_created.';
COMMENT ON COLUMN skill_imports.refusal IS 'Problema (RFC 9457) da recusa sem versão (ex.: download falhou, pacote sem SKILL.md após baixar); sem conteúdo do pacote.';
COMMENT ON COLUMN skill_imports.skill_version_id IS 'Versão criada.';
COMMENT ON COLUMN skill_imports.idempotency_key IS 'Idempotency-Key do pedido (repetição devolve a mesma importação).';
COMMENT ON COLUMN skill_imports.requested_by IS 'Administrador que pediu.';
COMMENT ON COLUMN skill_imports.received_at IS 'Quando foi recebida.';
COMMENT ON COLUMN skill_imports.completed_at IS 'Quando saiu de recebida.';
COMMENT ON CONSTRAINT skill_imports_public_id_uk ON skill_imports IS 'Id público único.';
COMMENT ON CONSTRAINT skill_imports_idempotency_uk ON skill_imports IS 'Uma importação por chave de idempotência na organização.';
COMMENT ON CONSTRAINT skill_imports_version_uk ON skill_imports IS 'Uma versão nasce de uma importação.';
COMMENT ON CONSTRAINT skill_imports_origin_ck ON skill_imports IS 'Origem coerente: URL https (até 2048) só no git_repository.';
COMMENT ON CONSTRAINT skill_imports_state_ck ON skill_imports IS 'Estados do pedido de importação.';
COMMENT ON CONSTRAINT skill_imports_refusal_ck ON skill_imports IS 'regra: US-44/CA-2 — recusa sempre com o Problema.';
COMMENT ON CONSTRAINT skill_imports_version_ck ON skill_imports IS 'Versão criada presente exatamente em version_created.';
COMMENT ON CONSTRAINT skill_imports_completion_ck ON skill_imports IS 'Conclusão datada; pacote não fica após concluir.';
CREATE INDEX skill_imports_received_idx ON skill_imports (received_at) WHERE state = 'received';
COMMENT ON INDEX skill_imports_received_idx IS 'Fila do worker: importações recebidas em ordem de chegada.';
CREATE INDEX skill_imports_organization_idx ON skill_imports (organization_id, received_at);
COMMENT ON INDEX skill_imports_organization_idx IS 'Importações da organização (obterImportacaoSkill, tela de skills); cobre a FK organization_id.';

CREATE TABLE project_skills (
  project_id              bigint      NOT NULL,
  skill_id                bigint      NOT NULL,
  skill_version_id         bigint      NOT NULL,
  pending_version_id      bigint      NULL,
  agents                 text[]      NOT NULL,
  associated_by           bigint      NOT NULL,
  associated_at            timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT project_skills_pkey PRIMARY KEY (project_id, skill_id),
  CONSTRAINT project_skills_project_fk FOREIGN KEY (project_id) REFERENCES projects (id) ON DELETE RESTRICT,
  CONSTRAINT project_skills_version_fk FOREIGN KEY (skill_id, skill_version_id) REFERENCES skill_versions (skill_id, id) ON DELETE RESTRICT,
  CONSTRAINT project_skills_pending_fk FOREIGN KEY (skill_id, pending_version_id) REFERENCES skill_versions (skill_id, id) ON DELETE RESTRICT,
  CONSTRAINT project_skills_associated_by_fk FOREIGN KEY (associated_by) REFERENCES users (id) ON DELETE RESTRICT,
  CONSTRAINT project_skills_agents_ck CHECK (cardinality(agents) > 0),
  CONSTRAINT project_skills_pending_ck CHECK (pending_version_id IS DISTINCT FROM skill_version_id)
);
COMMENT ON TABLE project_skills IS 'Skill associada ao projeto com versão fixada; atualização fica pendente até o aceite do responsável (US-43/CA-3, policy.imported_skill). Versão fixada que for desativada fica na linha: a onda que a carregaria não é despachada e o projeto é alertado (D-26).';
COMMENT ON COLUMN project_skills.project_id IS 'Projeto.';
COMMENT ON COLUMN project_skills.skill_id IS 'Skill.';
COMMENT ON COLUMN project_skills.skill_version_id IS 'Versão fixada em uso.';
COMMENT ON COLUMN project_skills.pending_version_id IS 'Versão nova liberada aguardando aceite (project.skill_updated com versao e versao_anterior inteiras, aceite pendente; registro update_skill_version).';
COMMENT ON COLUMN project_skills.agents IS 'Agentes que carregam a skill no projeto.';
COMMENT ON COLUMN project_skills.associated_by IS 'Responsável que associou ou aceitou a versão.';
COMMENT ON COLUMN project_skills.associated_at IS 'Quando.';
COMMENT ON CONSTRAINT project_skills_version_fk ON project_skills IS 'A versão fixada pertence à skill associada.';
COMMENT ON CONSTRAINT project_skills_pending_fk ON project_skills IS 'A versão pendente pertence à skill associada.';
COMMENT ON CONSTRAINT project_skills_agents_ck ON project_skills IS 'Associação a ao menos um agente.';
COMMENT ON CONSTRAINT project_skills_pending_ck ON project_skills IS 'Pendente difere da fixada.';
CREATE INDEX project_skills_skill_idx ON project_skills (skill_id, skill_version_id);
COMMENT ON INDEX project_skills_skill_idx IS 'Ao publicar nova versão, achar os projetos que usam a skill para avisar (US-43/CA-3); cobre a FK composta.';

CREATE FUNCTION project_skills_usable_version() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  -- FOR SHARE: desativação concorrente da versão espera esta transação (e vice-versa)
  PERFORM 1 FROM skill_versions v WHERE v.id IN (NEW.skill_version_id, NEW.pending_version_id) FOR SHARE;
  IF NOT EXISTS (SELECT 1 FROM skill_versions v WHERE v.id = NEW.skill_version_id AND v.status = 'unblocked')
     OR (NEW.pending_version_id IS NOT NULL
         AND NOT EXISTS (SELECT 1 FROM skill_versions v WHERE v.id = NEW.pending_version_id AND v.status = 'unblocked')) THEN
    RAISE EXCEPTION 'versão de skill não liberada para associação'
      USING ERRCODE = '23514', CONSTRAINT = 'project_skills_usable_version';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER project_skills_usable_version BEFORE INSERT OR UPDATE OF skill_version_id, pending_version_id ON project_skills
  FOR EACH ROW EXECUTE FUNCTION project_skills_usable_version();
COMMENT ON TRIGGER project_skills_usable_version ON project_skills IS 'regra: US-44/CA-4 — só versão liberada é associada, fixada ou oferecida como pendente; importada, em varredura, bloqueada ou desativada não (D-26).';

-- =============================================================================
-- 7. Artefatos (índice do repositório git) e achados
-- =============================================================================
CREATE TABLE artifacts (
  id                  bigint      GENERATED ALWAYS AS IDENTITY,
  public_id          uuid        NOT NULL DEFAULT gen_random_uuid(),
  project_id          bigint      NOT NULL,
  code              text        NOT NULL,
  type                text        NOT NULL,
  owner_stage          text        GENERATED ALWAYS AS (CASE
                         WHEN type IN ('VISAO','US','RNF','BACKLOG') THEN 'requirements'
                         WHEN type IN ('ARQ','ADR','T') THEN 'architecture'
                         WHEN type IN ('DIC','PERSIST') THEN 'data'
                         WHEN type = 'TELAS' THEN 'design'
                         WHEN type IN ('VALIDACAO','CT','AMEACAS') THEN 'validation'
                         WHEN type = 'REL-QA' THEN 'qa'
                         WHEN type = 'REL-SEG' THEN 'security'
                         WHEN type = 'RELEASE' THEN 'release'
                       END) STORED,
  title              text        NOT NULL,
  state              text        NOT NULL DEFAULT 'draft',
  current_version        integer     NOT NULL DEFAULT 1,
  replaced_by_id  bigint      NULL,
  created_at           timestamptz NOT NULL DEFAULT now(),
  updated_at       timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT artifacts_pkey PRIMARY KEY (id),
  CONSTRAINT artifacts_public_id_uk UNIQUE (public_id),
  CONSTRAINT artifacts_project_fk FOREIGN KEY (project_id) REFERENCES projects (id) ON DELETE RESTRICT,
  CONSTRAINT artifacts_code_uk UNIQUE (project_id, code),
  CONSTRAINT artifacts_id_type_uk UNIQUE (id, type),
  CONSTRAINT artifacts_replaced_fk FOREIGN KEY (replaced_by_id, type) REFERENCES artifacts (id, type) ON DELETE RESTRICT,
  CONSTRAINT artifacts_type_ck CHECK (type IN ('VISAO','US','RNF','BACKLOG','ARQ','ADR','T','DIC','PERSIST','VALIDACAO','CT','AMEACAS','ACH','REL-QA','REL-SEG','RELEASE','TELAS')),
  CONSTRAINT artifacts_code_ck CHECK (code ~ '^([A-Z]+(-[A-Z]+)?-[0-9]{2,}|VISAO|ARQ|DIC|PERSIST|AMEACAS|BACKLOG|TELAS)$'),
  CONSTRAINT artifacts_code_type_ck CHECK (CASE WHEN type IN ('VISAO','ARQ','DIC','PERSIST','AMEACAS','BACKLOG','TELAS') THEN code = type
                                                  ELSE code ~ ('^' || type || '-[0-9]{2,}$') END),
  CONSTRAINT artifacts_title_ck CHECK (char_length(title) BETWEEN 3 AND 140),
  CONSTRAINT artifacts_state_ck CHECK (state IN ('draft','in_review','approved','replaced')),
  CONSTRAINT artifacts_version_ck CHECK (current_version >= 1),
  CONSTRAINT artifacts_replaced_ck CHECK ((state = 'replaced') = (replaced_by_id IS NOT NULL) AND replaced_by_id IS DISTINCT FROM id)
);
COMMENT ON TABLE artifacts IS 'Índice de um artefato do projeto (id, tipo, estado, versão atual). A fonte de verdade é o arquivo no repositório git do projeto; o banco indexa para consulta, gates e motor de contexto.';
COMMENT ON COLUMN artifacts.id IS 'Identificador interno (chave primária inteira, alvo das FKs); não sai do banco: API e eventos usam public_id.';
COMMENT ON COLUMN artifacts.public_id IS 'Identificador opaco estável do artefato; a API e os eventos o identificam pelo código no projeto (US-03).';
COMMENT ON COLUMN artifacts.project_id IS 'Projeto.';
COMMENT ON COLUMN artifacts.code IS 'Id do artefato no projeto (US-03, DIC, ACH-07), padrão do cabeçalho comum.';
COMMENT ON COLUMN artifacts.type IS 'Tipo do artefato (17 tipos do cabeçalho comum).';
COMMENT ON COLUMN artifacts.owner_stage IS 'Etapa cujo gate aprova o tipo (artefatos/README.md); nula para ACH (sem gate próprio).';
COMMENT ON COLUMN artifacts.title IS 'Título do cabeçalho.';
COMMENT ON COLUMN artifacts.state IS 'Estado na máquina artefato.';
COMMENT ON COLUMN artifacts.current_version IS 'Versão vigente (sobe a cada retrabalho).';
COMMENT ON COLUMN artifacts.replaced_by_id IS 'Artefato do mesmo tipo que o substitui; obrigatório em substituido.';
COMMENT ON COLUMN artifacts.created_at IS 'Criação (versão 1).';
COMMENT ON COLUMN artifacts.updated_at IS 'Última mudança de estado ou versão.';
COMMENT ON CONSTRAINT artifacts_public_id_uk ON artifacts IS 'Id público único.';
COMMENT ON CONSTRAINT artifacts_code_uk ON artifacts IS 'Id único no projeto (checagem artifact.ids_unique); serve a busca por id.';
COMMENT ON CONSTRAINT artifacts_id_type_uk ON artifacts IS 'Alvo das FKs compostas (substituto do mesmo tipo; achados só de tipo ACH).';
COMMENT ON CONSTRAINT artifacts_replaced_fk ON artifacts IS 'Guarda de substituir: substituido_por aponta artefato existente do mesmo tipo.';
COMMENT ON CONSTRAINT artifacts_type_ck ON artifacts IS 'Enumeração tipo do cabeçalho comum.';
COMMENT ON CONSTRAINT artifacts_code_ck ON artifacts IS 'Padrão de id do cabeçalho comum.';
COMMENT ON CONSTRAINT artifacts_code_type_ck ON artifacts IS 'Prefixo do id coerente com o tipo; tipos únicos usam id fixo.';
COMMENT ON CONSTRAINT artifacts_title_ck ON artifacts IS 'Tamanho do título do cabeçalho comum.';
COMMENT ON CONSTRAINT artifacts_state_ck ON artifacts IS 'Estados da máquina artefato.';
COMMENT ON CONSTRAINT artifacts_version_ck ON artifacts IS 'Versão positiva.';
COMMENT ON CONSTRAINT artifacts_replaced_ck ON artifacts IS 'replaced exige substituido_por (cabeçalho comum), e nunca o próprio.';
CREATE INDEX artifacts_project_type_idx ON artifacts (project_id, type, state);
COMMENT ON INDEX artifacts_project_type_idx IS 'Saídas da etapa por tipo e estado nas guardas de submit_to_gate e nas checagens do gate; lista de artefatos por tipo (US-26).';
CREATE INDEX artifacts_replaced_idx ON artifacts (replaced_by_id) WHERE replaced_by_id IS NOT NULL;
COMMENT ON INDEX artifacts_replaced_idx IS 'Cobre a FK de substituição e a navegação "substituído por" no histórico (US-26).';

CREATE TRIGGER artifacts_valid_transition BEFORE INSERT OR UPDATE OF state ON artifacts
  FOR EACH ROW EXECUTE FUNCTION validate_transition('artifact', 'state');
COMMENT ON TRIGGER artifacts_valid_transition ON artifacts IS 'Estado inicial rascunho e só transições de estados.yaml (máquina artefato).';
CREATE TRIGGER artifacts_no_delete BEFORE DELETE ON artifacts
  FOR EACH ROW EXECUTE FUNCTION block_delete();
COMMENT ON TRIGGER artifacts_no_delete ON artifacts IS 'Artefato não se apaga (policy.history_preservation; registro destructive_operation).';

CREATE TABLE findings (
  artifact_id           bigint      NOT NULL,
  type                  text        NOT NULL DEFAULT 'ACH',
  project_id            bigint      NOT NULL,
  origin_wave_id        bigint      NOT NULL,
  origin_stage          text        NOT NULL,
  destination_stage         text        NOT NULL,
  severity            text        NOT NULL,
  category             text        NOT NULL,
  status              text        NOT NULL DEFAULT 'open',
  resolution             text        NULL,
  modified_artifacts   text[]      NOT NULL DEFAULT '{}',
  accepted_by            bigint      NULL,
  accepted_at             timestamptz NULL,
  acceptance_justification  text        NULL,
  acceptance_valid_until     date        NULL,
  discard_reason       text        NULL,
  duplicate_of          bigint      NULL,
  opened_at             timestamptz NOT NULL DEFAULT now(),
  updated_at         timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT findings_pkey PRIMARY KEY (artifact_id),
  CONSTRAINT findings_artifact_fk FOREIGN KEY (artifact_id, type) REFERENCES artifacts (id, type) ON DELETE RESTRICT,
  CONSTRAINT findings_project_fk FOREIGN KEY (project_id) REFERENCES projects (id) ON DELETE RESTRICT,
  CONSTRAINT findings_accepted_by_fk FOREIGN KEY (accepted_by) REFERENCES users (id) ON DELETE RESTRICT,
  CONSTRAINT findings_duplicate_fk FOREIGN KEY (duplicate_of) REFERENCES findings (artifact_id) ON DELETE RESTRICT,
  CONSTRAINT findings_type_ck CHECK (type = 'ACH'),
  CONSTRAINT findings_origin_stage_ck CHECK (origin_stage IN ('requirements','architecture','data','design','validation','implementation','qa','security','release','qa-plan','security-threats')),
  CONSTRAINT findings_destination_stage_ck CHECK (destination_stage IN ('requirements','architecture','data','design','validation','implementation')),
  CONSTRAINT findings_severity_ck CHECK (severity IN ('critical','high','medium','low')),
  CONSTRAINT findings_category_ck CHECK (category IN ('defect','ambiguous_requirement','vulnerability','architecture_deviation','data_inconsistency','design_inconsistency','technical_debt','contract_gap')),
  CONSTRAINT findings_status_ck CHECK (status IN ('open','in_rework','revalidating','closed','escalated','awaiting_scope','accepted','discarded')),
  CONSTRAINT findings_resolution_ck CHECK ((status = 'closed') = (resolution IS NOT NULL) AND (resolution IS NULL OR resolution IN ('fixed','scope_change'))),
  CONSTRAINT findings_scope_change_ck CHECK (resolution IS DISTINCT FROM 'scope_change' OR cardinality(modified_artifacts) > 0),
  CONSTRAINT findings_acceptance_ck CHECK ((status = 'accepted') = (accepted_by IS NOT NULL)
                                       AND (accepted_by IS NULL) = (accepted_at IS NULL)
                                       AND (accepted_by IS NULL) = (acceptance_justification IS NULL)
                                       AND (acceptance_justification IS NULL OR btrim(acceptance_justification) <> '')
                                       AND (acceptance_valid_until IS NULL OR accepted_by IS NOT NULL)),
  CONSTRAINT findings_discard_ck CHECK ((status = 'discarded') = (discard_reason IS NOT NULL AND btrim(discard_reason) <> '')),
  CONSTRAINT findings_duplicate_ck CHECK (duplicate_of IS NULL OR (status = 'discarded' AND duplicate_of <> artifact_id))
);
COMMENT ON TABLE findings IS 'Dados estruturados de um ACH (artefato de tipo ACH) para gates, roteamento e Pendências: severidade, destino, situação (máquina achado), aceite e descarte.';
COMMENT ON COLUMN findings.artifact_id IS 'Artefato ACH (mesmo id).';
COMMENT ON COLUMN findings.type IS 'Sempre ACH; fixa a FK composta.';
COMMENT ON COLUMN findings.project_id IS 'Projeto.';
COMMENT ON COLUMN findings.origin_wave_id IS 'Onda que emitiu o ACH.';
COMMENT ON COLUMN findings.origin_stage IS 'Etapa ou atividade da onda emissora; preenchida pelo orquestrador.';
COMMENT ON COLUMN findings.destination_stage IS 'Etapa que corrige.';
COMMENT ON COLUMN findings.severity IS 'critical, high, medium ou low.';
COMMENT ON COLUMN findings.category IS 'Natureza do problema (destino padrão em workflow-padrao.yaml).';
COMMENT ON COLUMN findings.status IS 'Situação na máquina achado.';
COMMENT ON COLUMN findings.resolution IS 'fixed ou scope_change; obrigatória em fechado.';
COMMENT ON COLUMN findings.modified_artifacts IS 'Ids alterados pela mudança de escopo; preenchido pelo orquestrador quando o gate.requirements aprova a alteração (awaiting_scope → fechado, PD-06).';
COMMENT ON COLUMN findings.accepted_by IS 'Pessoa que aceitou o risco (só humano).';
COMMENT ON COLUMN findings.accepted_at IS 'Quando aceitou.';
COMMENT ON COLUMN findings.acceptance_justification IS '[DP] Justificativa do aceite (texto livre humano; US-33/CA-2).';
COMMENT ON COLUMN findings.acceptance_valid_until IS 'aceite.valido_ate do esquema ACH (US-33/CA-1, D-29): último dia de validade do aceite; nulo = sem prazo. Vencido, o ACH segue aceito e release.accepted_risks_complete bloqueia o gate.release; a renovação (novo accepted_at e acceptance_valid_until em ACH aceito) é atualização de acompanhamento, registrada na auditoria.';
COMMENT ON COLUMN findings.discard_reason IS '[DP] Motivo do descarte (sem evidência ou duplicado; texto livre).';
COMMENT ON COLUMN findings.duplicate_of IS 'ACH original quando descartado por duplicidade.';
COMMENT ON COLUMN findings.opened_at IS 'Quando foi registrado.';
COMMENT ON COLUMN findings.updated_at IS 'Última mudança.';
COMMENT ON CONSTRAINT findings_artifact_fk ON findings IS 'O achado é um artefato de tipo ACH.';
COMMENT ON CONSTRAINT findings_duplicate_fk ON findings IS 'Duplicado cita ACH existente.';
COMMENT ON CONSTRAINT findings_type_ck ON findings IS 'Tipo fixo ACH.';
COMMENT ON CONSTRAINT findings_origin_stage_ck ON findings IS 'Enumeração origin_stage do esquema ACH.';
COMMENT ON CONSTRAINT findings_destination_stage_ck ON findings IS 'Enumeração destination_stage do esquema ACH.';
COMMENT ON CONSTRAINT findings_severity_ck ON findings IS 'Enumeração severidade.';
COMMENT ON CONSTRAINT findings_category_ck ON findings IS 'Enumeração categoria.';
COMMENT ON CONSTRAINT findings_status_ck ON findings IS 'Estados da máquina achado.';
COMMENT ON CONSTRAINT findings_resolution_ck ON findings IS 'Fechado sempre com resolução.';
COMMENT ON CONSTRAINT findings_scope_change_ck ON findings IS 'Mudança de escopo lista os artefatos alterados.';
COMMENT ON CONSTRAINT findings_acceptance_ck ON findings IS 'regra: US-33/CA-2 — aceite completo (por, quando, justificativa) existe exatamente quando accepted; validade só com aceite.';
COMMENT ON CONSTRAINT findings_discard_ck ON findings IS 'regra: US-32/CA-3 — descarte sempre com motivo.';
COMMENT ON CONSTRAINT findings_duplicate_ck ON findings IS 'Duplicado só em discarded e nunca de si mesmo.';
CREATE INDEX findings_open_idx ON findings (project_id, origin_stage, severity)
  WHERE status IN ('open','in_rework','revalidating','escalated','awaiting_scope');
COMMENT ON INDEX findings_open_idx IS 'ACH abertos: guarda "sem ACH Crítico/Alto bloqueante" do gate e da Release (US-31/CA-3, US-08/CA-2) e contagem de achados abertos no dashboard (US-50/CA-1).';
CREATE INDEX findings_acceptance_validity_idx ON findings (project_id, acceptance_valid_until) WHERE status = 'accepted' AND acceptance_valid_until IS NOT NULL;
COMMENT ON INDEX findings_acceptance_validity_idx IS 'regra: US-33/CA-1 — checagem release.accepted_risks_complete: aceites vencidos do projeto bloqueiam o gate.release (D-29).';
CREATE INDEX findings_escalated_idx ON findings (project_id, updated_at) WHERE status = 'escalated';
COMMENT ON INDEX findings_escalated_idx IS 'Pendências: achados escalados por idade (US-48/CA-1, US-32/CA-4).';
CREATE INDEX findings_origin_wave_idx ON findings (origin_wave_id);
COMMENT ON INDEX findings_origin_wave_idx IS 'Achados emitidos pela onda (payload onda.concluida.achados; visão da etapa US-49/CA-2); cobre a FK.';

CREATE TRIGGER findings_valid_transition BEFORE INSERT OR UPDATE OF status ON findings
  FOR EACH ROW EXECUTE FUNCTION validate_transition('finding', 'status');
COMMENT ON TRIGGER findings_valid_transition ON findings IS 'Situação inicial aberto e só transições de estados.yaml (máquina achado).';

-- ACH bloqueante aberto e decisão de gate disputam a mesma etapa: os dois travam a linha da etapa.
CREATE FUNCTION findings_stage_lock() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  PERFORM 1 FROM stages WHERE project_id = NEW.project_id AND stage = NEW.origin_stage FOR UPDATE;
  RETURN NEW;
END $$;
CREATE TRIGGER findings_stage_lock BEFORE INSERT ON findings
  FOR EACH ROW WHEN (NEW.severity IN ('critical','high')) EXECUTE FUNCTION findings_stage_lock();
COMMENT ON TRIGGER findings_stage_lock ON findings IS 'regra: US-31/CA-3 — ACH Crítico/Alto aberto serializa com a aprovação do gate da etapa (trava da linha em etapas).';

CREATE TABLE finding_affected_artifacts (
  id bigint GENERATED ALWAYS AS IDENTITY,
  finding_id    bigint NOT NULL,
  reference   text NOT NULL,
  artifact_id  bigint NULL,
  CONSTRAINT finding_affected_artifacts_pkey PRIMARY KEY (id),
  CONSTRAINT finding_affected_artifacts_finding_reference_uk UNIQUE (finding_id, reference),
  CONSTRAINT finding_affected_artifacts_finding_fk FOREIGN KEY (finding_id) REFERENCES findings (artifact_id) ON DELETE RESTRICT,
  CONSTRAINT finding_affected_artifacts_artifact_fk FOREIGN KEY (artifact_id) REFERENCES artifacts (id) ON DELETE RESTRICT,
  CONSTRAINT finding_affected_artifacts_reference_ck CHECK (btrim(reference) <> '')
);
COMMENT ON TABLE finding_affected_artifacts IS 'Onde está o problema do ACH (artefatos_afetados): artefato, item (US-03/CA-2) ou caminho de arquivo.';
COMMENT ON COLUMN finding_affected_artifacts.id IS 'Identificador interno (PK inteira, ADR-18 v2).';
COMMENT ON COLUMN finding_affected_artifacts.finding_id IS 'ACH.';
COMMENT ON COLUMN finding_affected_artifacts.reference IS 'Referência como escrita no ACH.';
COMMENT ON COLUMN finding_affected_artifacts.artifact_id IS 'Artefato resolvido da referência; nulo para caminho de código.';
COMMENT ON CONSTRAINT finding_affected_artifacts_finding_reference_uk ON finding_affected_artifacts IS 'Chave natural: uma linha por (finding_id, reference); antiga PK, mantida como UNIQUE.';
COMMENT ON CONSTRAINT finding_affected_artifacts_reference_ck ON finding_affected_artifacts IS 'Referência não vazia.';
CREATE INDEX finding_affected_artifacts_artifact_idx ON finding_affected_artifacts (artifact_id) WHERE artifact_id IS NOT NULL;
COMMENT ON INDEX finding_affected_artifacts_artifact_idx IS 'Artefatos a reabrir em finding.rework_started e ACH ligados a um requisito na rastreabilidade (US-28/CA-1, US-34/CA-1); cobre a FK.';

-- =============================================================================
-- 8. Ondas, checkpoints, chamadas de modelo, ferramentas, perguntas, comandos e idempotência
-- =============================================================================
CREATE TABLE waves (
  id                      bigint        GENERATED ALWAYS AS IDENTITY,
  public_id              uuid          NOT NULL DEFAULT gen_random_uuid(),
  project_id              bigint        NOT NULL,
  stage                   text          NOT NULL,
  stage_id                bigint        NOT NULL,
  code                  text          NOT NULL,
  agent                  text          NOT NULL,
  purpose              text          NOT NULL DEFAULT 'execution',
  finding_id               bigint        NULL,
  task                  text          NULL,
  state                  text          NOT NULL DEFAULT 'queued',
  agent_configuration_id  bigint        NULL,
  plan_version            integer       NOT NULL DEFAULT 1,
  current_step             integer       NOT NULL DEFAULT 0,
  attempt               integer       NOT NULL DEFAULT 1,
  retries_used          integer       NOT NULL DEFAULT 0,
  max_retries             integer       NOT NULL,
  execution_timeout        interval      NOT NULL,
  call_timeout         interval      NOT NULL,
  lease_owner              text          NULL,
  lease_until               timestamptz   NULL,
  lease_epoch             bigint        NOT NULL DEFAULT 0,
  heartbeat_at            timestamptz   NULL,
  pause_mode              text          NULL,
  pause_cause             text          NULL,
  failure_cause             text          NULL,
  input_tokens          bigint        NOT NULL DEFAULT 0,
  output_tokens            bigint        NOT NULL DEFAULT 0,
  read_cache_tokens    bigint        NOT NULL DEFAULT 0,
  write_cache_tokens    bigint        NOT NULL DEFAULT 0,
  useful_cost              numeric(18,8) NOT NULL DEFAULT 0,
  lost_cost           numeric(18,8) NOT NULL DEFAULT 0,
  estimated_lost_cost  boolean       NOT NULL DEFAULT false,
  currency                   char(3)       NOT NULL,
  cost_estimate        numeric(18,8) NULL,
  estimate_basis         jsonb         NULL,
  execution_duration_ms     bigint        NOT NULL DEFAULT 0,
  created_at               timestamptz   NOT NULL DEFAULT now(),
  started_at             timestamptz   NULL,
  finished_at           timestamptz   NULL,
  CONSTRAINT waves_pkey PRIMARY KEY (id),
  CONSTRAINT waves_public_id_uk UNIQUE (public_id),
  CONSTRAINT waves_id_created_uk UNIQUE (id, created_at),
  CONSTRAINT waves_stage_fk FOREIGN KEY (stage_id, project_id, stage) REFERENCES stages (id, project_id, stage) ON DELETE RESTRICT,
  CONSTRAINT waves_finding_fk FOREIGN KEY (finding_id) REFERENCES findings (artifact_id) ON DELETE RESTRICT,
  CONSTRAINT waves_configuration_fk FOREIGN KEY (agent_configuration_id) REFERENCES agent_configurations (id) ON DELETE RESTRICT,
  CONSTRAINT waves_code_uk UNIQUE (project_id, code),
  CONSTRAINT waves_code_ck CHECK (code ~ ('^' || CASE stage
      WHEN 'requirements' THEN 'REQ' WHEN 'architecture' THEN 'ARQ' WHEN 'data' THEN 'DBA'
      WHEN 'design' THEN 'DSG' WHEN 'validation' THEN 'VAL' WHEN 'implementation' THEN 'DEV'
      WHEN 'qa' THEN 'QA' WHEN 'qa-plan' THEN 'QA' WHEN 'security' THEN 'SEG'
      WHEN 'security-threats' THEN 'SEG' WHEN 'release' THEN 'REL' END || '-[0-9]{2,}$')),
  CONSTRAINT waves_agent_ck CHECK (agent IN ('requirements-agent','architect-agent','dba-agent','designer-agent','developer-agent','qa-agent','security-agent','orchestrator')),
  CONSTRAINT waves_configuration_ck CHECK ((agent = 'orchestrator') = (agent_configuration_id IS NULL)),
  CONSTRAINT waves_purpose_ck CHECK (purpose IN ('execution','rework','revalidation','reevaluation','edit_review')),
  CONSTRAINT waves_finding_ck CHECK (purpose = 'edit_review' OR (purpose IN ('rework','revalidation')) = (finding_id IS NOT NULL)),
  CONSTRAINT waves_task_ck CHECK (task IS NULL OR (task ~ '^T-[0-9]{2,}$' AND stage = 'implementation')),
  CONSTRAINT waves_state_ck CHECK (state IN ('queued','running','awaiting_user','pausing','paused','completed','failed','canceled')),
  CONSTRAINT waves_counters_ck CHECK (plan_version >= 1 AND current_step >= 0 AND attempt >= 1 AND retries_used >= 0),
  CONSTRAINT waves_max_retries_ck CHECK (max_retries BETWEEN 0 AND 5),
  CONSTRAINT waves_timeouts_ck CHECK (execution_timeout BETWEEN interval '5 minutes' AND interval '8 hours'
                                      AND call_timeout BETWEEN interval '1 minute' AND interval '30 minutes'),
  CONSTRAINT waves_lease_ck CHECK ((lease_owner IS NULL) = (lease_until IS NULL)),
  CONSTRAINT waves_lease_state_ck CHECK (lease_owner IS NULL OR state IN ('running','pausing')),
  CONSTRAINT waves_pause_ck CHECK ((state IN ('pausing','paused')) = (pause_cause IS NOT NULL)
                                   AND (pause_mode IS NULL OR (state = 'paused' AND pause_mode IN ('cooperative','immediate')))),
  CONSTRAINT waves_failure_ck CHECK ((state = 'failed') = (failure_cause IS NOT NULL)
                                   AND (failure_cause IS NULL OR failure_cause IN ('retries_exhausted','execution_timeout','unrecoverable_error','limit_violation'))),
  CONSTRAINT waves_costs_ck CHECK (input_tokens >= 0 AND output_tokens >= 0 AND read_cache_tokens >= 0 AND write_cache_tokens >= 0
                                    AND useful_cost >= 0 AND lost_cost >= 0 AND execution_duration_ms >= 0),
  CONSTRAINT waves_currency_ck CHECK (currency ~ '^[A-Z]{3}$'),
  CONSTRAINT waves_estimate_ck CHECK ((state = 'queued' AND attempt = 1 AND started_at IS NULL) OR cost_estimate IS NOT NULL),
  CONSTRAINT waves_estimate_value_ck CHECK (cost_estimate IS NULL OR cost_estimate >= 0),
  CONSTRAINT waves_finished_ck CHECK ((state IN ('completed','canceled')) = (finished_at IS NOT NULL))
);
COMMENT ON TABLE waves IS 'Onda: uma execução de um agente numa etapa (máquina onda), com plano versionado, cursor de checkpoint, lease do worker e custo acumulado. O id exposto nos eventos é codigo (DBA-01).';
COMMENT ON COLUMN waves.id IS 'Identificador interno (chave primária inteira, alvo das FKs); não sai do banco: API e eventos usam public_id.';
COMMENT ON COLUMN waves.public_id IS 'Identificador opaco estável da onda; a API e os eventos a identificam pelo código no projeto (ondaid).';
COMMENT ON COLUMN waves.project_id IS 'Projeto.';
COMMENT ON COLUMN waves.stage_id IS 'stages.id da etapa (FK; project_id e stage acompanham).';
COMMENT ON COLUMN waves.stage IS 'Etapa ou atividade paralela da onda.';
COMMENT ON COLUMN waves.code IS 'Id da onda no projeto: sigla da etapa + número (ondaid dos eventos e campo onda do cabeçalho).';
COMMENT ON COLUMN waves.agent IS 'Agente que executa (orchestrator na Release).';
COMMENT ON COLUMN waves.purpose IS 'execution, rework, revalidation, reevaluation ou edit_review (o agente da etapa valida e formata uma edição humana, US-53; payload de onda).';
COMMENT ON COLUMN waves.finding_id IS 'ACH da onda de retrabalho ou revalidação; na edit_review, o ACH em awaiting_scope, se houver.';
COMMENT ON COLUMN waves.task IS 'T da onda da Implementação (uma onda por tarefa; OndaResumo.tarefa, PL-20).';
COMMENT ON COLUMN waves.state IS 'Estado na máquina onda.';
COMMENT ON COLUMN waves.agent_configuration_id IS 'Versão da configuração do agente usada (US-25/CA-4, RNF-15).';
COMMENT ON COLUMN waves.plan_version IS 'Versão do plano de passos; onda em curso mantém o plano com que começou (spike onda-checkpoint).';
COMMENT ON COLUMN waves.current_step IS 'Cursor: último passo com checkpoint gravado na tentativa atual; retomada parte dele (RNF-12).';
COMMENT ON COLUMN waves.attempt IS 'Tentativa (reiniciar soma 1; anteriores ficam consultáveis, US-13/CA-1).';
COMMENT ON COLUMN waves.retries_used IS 'Retries automáticos consumidos na tentativa (US-13/CA-3).';
COMMENT ON COLUMN waves.max_retries IS 'Cópia de policy.wave_timeout.max_retries no despacho.';
COMMENT ON COLUMN waves.execution_timeout IS 'Cópia de policy.wave_timeout.execution_timeout (espera humana não conta).';
COMMENT ON COLUMN waves.call_timeout IS 'Cópia de policy.wave_timeout.call_timeout.';
COMMENT ON COLUMN waves.lease_owner IS 'Worker dono da execução; nulo fora de running/pausando.';
COMMENT ON COLUMN waves.lease_until IS 'Fim do lease; vencido, outro worker reivindica (RNF-12).';
COMMENT ON COLUMN waves.lease_epoch IS 'Token de fencing: sobe a cada reivindicação; checkpoint de worker antigo é recusado.';
COMMENT ON COLUMN waves.heartbeat_at IS 'Último heartbeat do worker.';
COMMENT ON COLUMN waves.pause_mode IS 'cooperative ou immediate (payload wave.paused).';
COMMENT ON COLUMN waves.pause_cause IS 'usuario ou política que pausou (budget_exhausted, dependency_reopened…).';
COMMENT ON COLUMN waves.failure_cause IS 'Causa da falha (payload wave.failed).';
COMMENT ON COLUMN waves.input_tokens IS 'Tokens de entrada acumulados (todas as tentativas).';
COMMENT ON COLUMN waves.output_tokens IS 'Tokens de saída acumulados.';
COMMENT ON COLUMN waves.read_cache_tokens IS 'Tokens lidos do cache acumulados.';
COMMENT ON COLUMN waves.write_cache_tokens IS 'Tokens gravados no cache acumulados.';
COMMENT ON COLUMN waves.useful_cost IS 'Custo das chamadas concluídas (soma de model_calls não perdidas; US-36/CA-1).';
COMMENT ON COLUMN waves.lost_cost IS 'Custo de chamadas abortadas ou repetidas (pausar agora, cancelar, queda; US-11/CA-2).';
COMMENT ON COLUMN waves.estimated_lost_cost IS 'Verdadeiro quando lost_cost inclui estimativa de chamada em voo na queda do worker (resume_after_crash; estimated_lost_cost do evento wave.resumed_after_crash, RNF-16, D-23); policy.project_budget conta o estimado.';
COMMENT ON COLUMN waves.currency IS 'Moeda dos custos (da organização).';
COMMENT ON COLUMN waves.cost_estimate IS 'Estimativa antes de rodar (US-40); guarda de despachar.';
COMMENT ON COLUMN waves.estimate_basis IS 'Base do cálculo: tamanho da entrada, preço usado, média histórica ou sem_historico (US-40/CA-1, CA-2).';
COMMENT ON COLUMN waves.execution_duration_ms IS 'Tempo de execução sem espera humana (US-38/CA-3).';
COMMENT ON COLUMN waves.created_at IS 'Entrada na fila; início do tempo de ciclo.';
COMMENT ON COLUMN waves.started_at IS 'Primeiro despacho.';
COMMENT ON COLUMN waves.finished_at IS 'Conclusão ou cancelamento; fim do tempo de ciclo.';
COMMENT ON CONSTRAINT waves_public_id_uk ON waves IS 'Id público único.';
COMMENT ON CONSTRAINT waves_stage_fk ON waves IS 'A onda é de uma etapa existente do mesmo projeto.';
COMMENT ON CONSTRAINT waves_finding_fk ON waves IS 'ACH de retrabalho existente.';
COMMENT ON CONSTRAINT waves_configuration_fk ON waves IS 'Configuração de agente existente.';
COMMENT ON CONSTRAINT waves_code_uk ON waves IS 'Id de onda único no projeto; busca pelo ondaid.';
COMMENT ON CONSTRAINT waves_id_created_uk ON waves IS 'Alvo da FK composta de wave_checkpoints (wave_id, wave_created_at), que leva a chave de partição.';
COMMENT ON CONSTRAINT waves_code_ck ON waves IS 'Sigla do id coerente com a etapa (padrão onda do cabeçalho comum).';
COMMENT ON CONSTRAINT waves_agent_ck ON waves IS 'Agentes e orchestrator.';
COMMENT ON CONSTRAINT waves_configuration_ck ON waves IS 'Agente sempre com versão de configuração; orchestrator sem.';
COMMENT ON CONSTRAINT waves_purpose_ck ON waves IS 'Enumeração finalidade.';
COMMENT ON CONSTRAINT waves_finding_ck ON waves IS 'regra: US-32/CA-1 — onda de rework ou revalidação aponta o ACH.';
COMMENT ON CONSTRAINT waves_task_ck ON waves IS 'Tarefa só na Implementação, com id de T.';
COMMENT ON CONSTRAINT waves_state_ck ON waves IS 'Estados da máquina onda.';
COMMENT ON CONSTRAINT waves_counters_ck ON waves IS 'Contadores não negativos.';
COMMENT ON CONSTRAINT waves_max_retries_ck ON waves IS 'Faixa de policy.wave_timeout.max_retries.';
COMMENT ON CONSTRAINT waves_timeouts_ck ON waves IS 'Faixas de policy.wave_timeout.';
COMMENT ON CONSTRAINT waves_lease_ck ON waves IS 'Dono e prazo do lease juntos.';
COMMENT ON CONSTRAINT waves_lease_state_ck ON waves IS 'Lease só enquanto o worker executa.';
COMMENT ON CONSTRAINT waves_pause_ck ON waves IS 'regra: US-12/CA-2 — pausa registra causa; modo cooperative/immediate quando paused.';
COMMENT ON CONSTRAINT waves_failure_ck ON waves IS 'Falha sempre com causa da enumeração do evento.';
COMMENT ON CONSTRAINT waves_costs_ck ON waves IS 'Tokens, custos e duração não negativos.';
COMMENT ON CONSTRAINT waves_currency_ck ON waves IS 'Código ISO 4217.';
COMMENT ON CONSTRAINT waves_estimate_ck ON waves IS 'regra: US-40/CA-1 — guarda de despachar: estimativa registrada antes de executar.';
COMMENT ON CONSTRAINT waves_estimate_value_ck ON waves IS 'Estimativa não negativa.';
COMMENT ON CONSTRAINT waves_finished_ck ON waves IS 'Fim do ciclo só em estado final.';
CREATE INDEX waves_task_idx ON waves (project_id, task) WHERE task IS NOT NULL;
COMMENT ON INDEX waves_task_idx IS 'Ondas de uma T (listarOndas?tarefa=, PL-20).';
CREATE INDEX waves_queue_idx ON waves (created_at) WHERE state = 'queued';
COMMENT ON INDEX waves_queue_idx IS 'Despacho FIFO: SELECT … WHERE estado = queued ORDER BY created_at FOR UPDATE SKIP LOCKED.';
CREATE INDEX waves_lease_idx ON waves (lease_until) WHERE lease_owner IS NOT NULL;
COMMENT ON INDEX waves_lease_idx IS 'Reivindicação de ondas com lease vencido após queda do worker (RNF-12, US-11/CA-1).';
CREATE INDEX waves_project_stage_idx ON waves (project_id, stage, created_at);
COMMENT ON INDEX waves_project_stage_idx IS 'Ondas da etapa na visão de workflow (US-49/CA-2), custo e tempo agregados por etapa (US-38) e custo acumulado do projeto (orçamento, dashboard); a FK de etapa nunca é apagada nem reescrita, sem índice próprio.';
CREATE INDEX waves_active_idx ON waves (project_id, agent) WHERE state IN ('queued','running','awaiting_user','pausing','paused');
COMMENT ON INDEX waves_active_idx IS 'Agentes em execução no dashboard (US-50/CA-1), pausa/cancelamento por política do projeto e recusa de exclusão com onda ativa (US-04/CA-2).';
CREATE INDEX waves_finding_idx ON waves (finding_id) WHERE finding_id IS NOT NULL;
COMMENT ON INDEX waves_finding_idx IS 'Ondas de retrabalho e revalidação de um ACH (ciclos, cancel_by_policy ao aceitar/descartar); cobre a FK.';

CREATE TRIGGER waves_valid_transition BEFORE INSERT OR UPDATE OF state ON waves
  FOR EACH ROW EXECUTE FUNCTION validate_transition('wave', 'state');
COMMENT ON TRIGGER waves_valid_transition ON waves IS 'Estado inicial queued e só transições de estados.yaml (máquina onda).';
CREATE TRIGGER waves_no_delete BEFORE DELETE ON waves
  FOR EACH ROW EXECUTE FUNCTION block_delete();
COMMENT ON TRIGGER waves_no_delete ON waves IS 'Onda e tentativas anteriores não se apagam (policy.history_preservation).';

-- Reivindicação do lease (despacho, retomada e resume_after_crash, D-23): a época só sobe, de 1 em 1, e um
-- worker só toma uma onda de outro dono com o lease dele vencido (guarda "lease vencido e nenhuma instância
-- viva com a época atual"). A retomada após queda é autotransição running → running, fora de max_retries.
CREATE FUNCTION waves_lease_claim() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.lease_epoch < OLD.lease_epoch OR NEW.lease_epoch > OLD.lease_epoch + 1 THEN
    RAISE EXCEPTION 'época do lease só sobe de 1 em 1' USING ERRCODE = '23514', CONSTRAINT = 'waves_lease_claim';
  END IF;
  IF NEW.lease_epoch = OLD.lease_epoch + 1 AND OLD.lease_owner IS NOT NULL AND OLD.lease_until > now() THEN
    RAISE EXCEPTION 'lease ainda vigente: onda não pode ser reivindicada' USING ERRCODE = '23514', CONSTRAINT = 'waves_lease_claim';
  END IF;
  IF NEW.lease_epoch = OLD.lease_epoch AND NEW.lease_owner IS DISTINCT FROM OLD.lease_owner AND NEW.lease_owner IS NOT NULL THEN
    RAISE EXCEPTION 'novo dono do lease exige nova época (fencing)' USING ERRCODE = '23514', CONSTRAINT = 'waves_lease_claim';
  END IF;
  IF NEW.retries_used <> OLD.retries_used AND NEW.lease_epoch <> OLD.lease_epoch AND OLD.state = 'running' AND NEW.state = 'running' THEN
    RAISE EXCEPTION 'retomada após queda não conta em max_retries' USING ERRCODE = '23514', CONSTRAINT = 'waves_lease_claim';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER waves_lease_claim BEFORE UPDATE OF lease_epoch, lease_owner ON waves
  FOR EACH ROW EXECUTE FUNCTION waves_lease_claim();
COMMENT ON TRIGGER waves_lease_claim ON waves IS 'regra: US-11/CA-1 — retomada após queda só com lease vencido, nova época (fencing) e sem consumir retry (resume_after_crash, D-23; RNF-12).';

-- FKs que dependem de ondas e achados (referência circular).
ALTER TABLE findings ADD CONSTRAINT findings_origin_wave_fk FOREIGN KEY (origin_wave_id) REFERENCES waves (id) ON DELETE RESTRICT;
COMMENT ON CONSTRAINT findings_origin_wave_fk ON findings IS 'ACH emitido por onda existente.';
ALTER TABLE stages ADD CONSTRAINT stages_reopened_per_fk FOREIGN KEY (reopened_by_finding_id) REFERENCES findings (artifact_id) ON DELETE RESTRICT;
COMMENT ON CONSTRAINT stages_reopened_per_fk ON stages IS 'ACH da reabertura existente.';

CREATE TABLE wave_models (
  id bigint GENERATED ALWAYS AS IDENTITY,
  wave_id         bigint      NOT NULL,
  role           text        NOT NULL,
  model_id       bigint      NOT NULL,
  mode            text        NOT NULL,
  reason          text        NOT NULL,
  selected_at  timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT wave_models_pkey PRIMARY KEY (id),
  CONSTRAINT wave_models_wave_role_uk UNIQUE (wave_id, role),
  CONSTRAINT wave_models_wave_fk FOREIGN KEY (wave_id) REFERENCES waves (id) ON DELETE RESTRICT,
  CONSTRAINT wave_models_model_fk FOREIGN KEY (model_id) REFERENCES models (id) ON DELETE RESTRICT,
  CONSTRAINT wave_models_role_ck CHECK (role IN ('reasoning','execution')),
  CONSTRAINT wave_models_mode_ck CHECK (mode IN ('manual','automatic')),
  CONSTRAINT wave_models_reason_ck CHECK (reason IN ('project_configuration','stage_default','automatic_selection','failure_escalation','finding_escalation'))
);
COMMENT ON TABLE wave_models IS 'Modelo escolhido para cada papel do par na onda, com o motivo (registro select_model, evento wave.model_selected; US-16/CA-1, CA-4).';
COMMENT ON COLUMN wave_models.id IS 'Identificador interno (PK inteira, ADR-18 v2).';
COMMENT ON COLUMN wave_models.wave_id IS 'Onda.';
COMMENT ON COLUMN wave_models.role IS 'reasoning ou execution.';
COMMENT ON COLUMN wave_models.model_id IS 'Modelo usado.';
COMMENT ON COLUMN wave_models.mode IS 'manual ou automatic (Fase 4).';
COMMENT ON COLUMN wave_models.reason IS 'Motivo da escolha (payload wave.model_selected).';
COMMENT ON COLUMN wave_models.selected_at IS 'Quando foi escolhido (antes do despacho).';
COMMENT ON CONSTRAINT wave_models_wave_role_uk ON wave_models IS 'Chave natural: uma linha por (wave_id, role); antiga PK, mantida como UNIQUE.';
COMMENT ON CONSTRAINT wave_models_role_ck ON wave_models IS 'Papéis do par de modelos.';
COMMENT ON CONSTRAINT wave_models_mode_ck ON wave_models IS 'Modos de seleção.';
COMMENT ON CONSTRAINT wave_models_reason_ck ON wave_models IS 'Enumeração motivo do evento.';

CREATE TABLE wave_skills (
  wave_id          bigint NOT NULL,
  skill_version_id  bigint NOT NULL,
  load            text   NOT NULL,
  CONSTRAINT wave_skills_pkey PRIMARY KEY (wave_id, skill_version_id),
  CONSTRAINT wave_skills_wave_fk FOREIGN KEY (wave_id) REFERENCES waves (id) ON DELETE RESTRICT,
  CONSTRAINT wave_skills_version_fk FOREIGN KEY (skill_version_id) REFERENCES skill_versions (id) ON DELETE RESTRICT,
  CONSTRAINT wave_skills_load_ck CHECK (load IN ('description','complete'))
);
COMMENT ON TABLE wave_skills IS 'Versão de skill entregue à onda e até onde foi carregada (US-43/CA-2, RNF-15).';
COMMENT ON COLUMN wave_skills.wave_id IS 'Onda.';
COMMENT ON COLUMN wave_skills.skill_version_id IS 'Versão de skill.';
COMMENT ON COLUMN wave_skills.load IS 'description (só a descrição no contexto) ou complete (conteúdo carregado; US-43/RN-1).';
COMMENT ON CONSTRAINT wave_skills_load_ck ON wave_skills IS 'regra: US-43/RN-1 — carga por descrição primeiro, complete sob demanda.';
CREATE FUNCTION wave_skills_unblocked_version() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM skill_versions v WHERE v.id = NEW.skill_version_id AND v.status = 'unblocked' FOR SHARE) THEN
    RAISE EXCEPTION 'onda não carrega versão de skill fora de liberada'
      USING ERRCODE = '23514', CONSTRAINT = 'wave_skills_unblocked_version';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER wave_skills_unblocked_version BEFORE INSERT ON wave_skills
  FOR EACH ROW EXECUTE FUNCTION wave_skills_unblocked_version();
COMMENT ON TRIGGER wave_skills_unblocked_version ON wave_skills IS 'Onda só carrega versão liberada; desativada impede o despacho (máquina skill, D-26).';
CREATE INDEX wave_skills_version_idx ON wave_skills (skill_version_id);
COMMENT ON INDEX wave_skills_version_idx IS 'Ondas que carregaram uma versão de skill (reprodutibilidade RNF-15, impacto da desativação); cobre a FK.';

CREATE TABLE wave_inputs (
  wave_id             bigint NOT NULL,
  artifact_version_id  bigint NOT NULL,
  CONSTRAINT wave_inputs_pkey PRIMARY KEY (wave_id, artifact_version_id)
);
COMMENT ON TABLE wave_inputs IS 'Versão de artefato entregue como entrada à onda (RNF-15; base da análise de impacto e da reavaliação).';
COMMENT ON COLUMN wave_inputs.wave_id IS 'Onda.';
COMMENT ON COLUMN wave_inputs.artifact_version_id IS 'Versão de artefato de entrada.';

CREATE TABLE wave_checkpoints (
  wave_id               bigint        NOT NULL,
  attempt             integer       NOT NULL,
  step                 integer       NOT NULL,
  plan_version          integer       NOT NULL,
  after                  text          NOT NULL,
  lease_epoch           bigint        NOT NULL,
  worker                text          NOT NULL,
  agent_state         bytea         NULL,
  state_hash           text          NOT NULL,
  input_tokens        bigint        NOT NULL,
  output_tokens          bigint        NOT NULL,
  read_cache_tokens  bigint        NOT NULL,
  write_cache_tokens  bigint        NOT NULL,
  useful_cost            numeric(18,8) NOT NULL,
  lost_cost         numeric(18,8) NOT NULL,
  written_at            timestamptz   NOT NULL DEFAULT now(),
  content_purged_at timestamptz   NULL,
  wave_created_at        timestamptz   NOT NULL,
  CONSTRAINT wave_checkpoints_pkey PRIMARY KEY (wave_id, attempt, step, wave_created_at),
  CONSTRAINT wave_checkpoints_wave_fk FOREIGN KEY (wave_id, wave_created_at) REFERENCES waves (id, created_at) ON DELETE RESTRICT,
  CONSTRAINT wave_checkpoints_numbers_ck CHECK (attempt >= 1 AND step >= 1 AND plan_version >= 1),
  CONSTRAINT wave_checkpoints_after_ck CHECK (after IN ('model_call','tool')),
  CONSTRAINT wave_checkpoints_hash_ck CHECK (state_hash ~ '^[0-9a-f]{64}$'),
  CONSTRAINT wave_checkpoints_costs_ck CHECK (input_tokens >= 0 AND output_tokens >= 0 AND read_cache_tokens >= 0 AND write_cache_tokens >= 0 AND useful_cost >= 0 AND lost_cost >= 0),
  CONSTRAINT wave_checkpoints_purge_ck CHECK ((agent_state IS NULL) = (content_purged_at IS NOT NULL))
) PARTITION BY RANGE (wave_created_at);
COMMENT ON TABLE wave_checkpoints IS 'Checkpoint da onda após cada chamada de modelo ou execução de ferramenta (RNF-12, US-11/CA-3). Gravado na mesma transação do avanço do cursor, da auditoria e do outbox.';
COMMENT ON COLUMN wave_checkpoints.wave_id IS 'Onda.';
COMMENT ON COLUMN wave_checkpoints.attempt IS 'Tentativa da onda.';
COMMENT ON COLUMN wave_checkpoints.step IS 'Número do passo (payload checkpoint_gravado.checkpoint).';
COMMENT ON COLUMN wave_checkpoints.plan_version IS 'Plano em que o passo foi executado.';
COMMENT ON COLUMN wave_checkpoints.after IS 'model_call ou tool (mesma enumeração do payload onda.checkpoint_gravado.apos).';
COMMENT ON COLUMN wave_checkpoints.lease_epoch IS 'Época do lease de quem gravou (fencing).';
COMMENT ON COLUMN wave_checkpoints.worker IS 'Worker que gravou.';
COMMENT ON COLUMN wave_checkpoints.agent_state IS '[DP] Estado serializado e comprimido do agente (conversa, plano) para retomar; contém conteúdo do projeto e respostas humanas. Expurgado após a retenção de conteúdo.';
COMMENT ON COLUMN wave_checkpoints.state_hash IS 'SHA-256 do estado (reprodutibilidade; fica após o expurgo).';
COMMENT ON COLUMN wave_checkpoints.input_tokens IS 'Tokens de entrada acumulados até o passo.';
COMMENT ON COLUMN wave_checkpoints.output_tokens IS 'Tokens de saída acumulados.';
COMMENT ON COLUMN wave_checkpoints.read_cache_tokens IS 'Tokens lidos do cache acumulados.';
COMMENT ON COLUMN wave_checkpoints.write_cache_tokens IS 'Tokens gravados no cache acumulados.';
COMMENT ON COLUMN wave_checkpoints.useful_cost IS 'Custo útil acumulado (fonte de verdade de custo: payload custo_acumulado).';
COMMENT ON COLUMN wave_checkpoints.lost_cost IS 'Custo perdido acumulado.';
COMMENT ON COLUMN wave_checkpoints.written_at IS 'Instante da gravação.';
COMMENT ON COLUMN wave_checkpoints.content_purged_at IS 'Quando agent_state foi apagado pela retenção (metadados ficam).';
COMMENT ON COLUMN wave_checkpoints.wave_created_at IS 'Cópia de waves.created_at, garantida pela FK composta; é a chave de partição e depende só da onda, então a idempotência por (wave_id, tentativa, passo) vale entre partições. O worker a informa ao gravar.';
COMMENT ON CONSTRAINT wave_checkpoints_pkey ON wave_checkpoints IS 'Idempotência: passo concluído existe uma vez só por tentativa (retomada não repete passo; spike). Inclui a chave de partição wave_created_at, que é função de wave_id: a unicidade continua por (wave_id, tentativa, passo).';
COMMENT ON CONSTRAINT wave_checkpoints_wave_fk ON wave_checkpoints IS 'Onda existente, com a data de criação igual à da onda (chave de partição coerente).';
COMMENT ON CONSTRAINT wave_checkpoints_numbers_ck ON wave_checkpoints IS 'Números positivos.';
COMMENT ON CONSTRAINT wave_checkpoints_after_ck ON wave_checkpoints IS 'Enumeração apos do evento wave.checkpoint_written.';
COMMENT ON CONSTRAINT wave_checkpoints_hash_ck ON wave_checkpoints IS 'SHA-256 em hexadecimal.';
COMMENT ON CONSTRAINT wave_checkpoints_costs_ck ON wave_checkpoints IS 'Acumulados não negativos.';
COMMENT ON CONSTRAINT wave_checkpoints_purge_ck ON wave_checkpoints IS 'Conteúdo presente ou expurgo registrado, nunca os dois.';
CREATE INDEX wave_checkpoints_purge_idx ON wave_checkpoints (written_at) WHERE agent_state IS NOT NULL;
COMMENT ON INDEX wave_checkpoints_purge_idx IS 'Job de retenção: checkpoints com conteúdo mais velhos que call_content_period (efeito expire_content, RNF-20, D-30).';

CREATE TABLE model_calls (
  id                    bigint        GENERATED ALWAYS AS IDENTITY,
  public_id            uuid          NOT NULL DEFAULT gen_random_uuid(),
  wave_id               bigint        NOT NULL,
  attempt             integer       NOT NULL,
  step                 integer       NOT NULL,
  model_id             bigint        NOT NULL,
  role                 text          NOT NULL,
  price_id              bigint        NOT NULL,
  input_tokens        bigint        NOT NULL DEFAULT 0,
  output_tokens          bigint        NOT NULL DEFAULT 0,
  read_cache_tokens  bigint        NOT NULL DEFAULT 0,
  write_cache_tokens  bigint        NOT NULL DEFAULT 0,
  cost                 numeric(18,8) NOT NULL DEFAULT 0,
  currency                 char(3)       NOT NULL,
  lost               boolean       NOT NULL DEFAULT false,
  estimated_cost        boolean       NOT NULL DEFAULT false,
  result             text          NULL,
  input_hash          text          NOT NULL,
  content              bytea         NULL,
  content_purged_at timestamptz   NULL,
  error_summary           text          NULL,
  summary                text          NULL,
  trace_id              text          NULL,
  started_at           timestamptz   NOT NULL DEFAULT now(),
  finished_at          timestamptz   NULL,
  CONSTRAINT model_calls_pkey PRIMARY KEY (id, started_at),
  CONSTRAINT model_calls_public_id_uk UNIQUE (public_id, started_at),
  CONSTRAINT model_calls_wave_fk FOREIGN KEY (wave_id) REFERENCES waves (id) ON DELETE RESTRICT,
  CONSTRAINT model_calls_model_fk FOREIGN KEY (model_id) REFERENCES models (id) ON DELETE RESTRICT,
  CONSTRAINT model_calls_price_fk FOREIGN KEY (price_id) REFERENCES model_prices (id) ON DELETE RESTRICT,
  CONSTRAINT model_calls_numbers_ck CHECK (attempt >= 1 AND step >= 1),
  CONSTRAINT model_calls_role_ck CHECK (role IN ('reasoning','execution')),
  CONSTRAINT model_calls_tokens_ck CHECK (input_tokens >= 0 AND output_tokens >= 0 AND read_cache_tokens >= 0 AND write_cache_tokens >= 0 AND cost >= 0),
  CONSTRAINT model_calls_result_ck CHECK (result IN ('success','error','aborted')),
  CONSTRAINT model_calls_end_ck CHECK ((result IS NULL) = (finished_at IS NULL)),
  CONSTRAINT model_calls_lost_ck CHECK (result IS DISTINCT FROM 'aborted' OR lost),
  CONSTRAINT model_calls_estimated_ck CHECK (NOT estimated_cost OR lost),
  CONSTRAINT model_calls_hash_ck CHECK (input_hash ~ '^[0-9a-f]{64}$'),
  CONSTRAINT model_calls_trace_ck CHECK (trace_id IS NULL OR trace_id ~ '^[0-9a-f]{32}$'),
  CONSTRAINT model_calls_purge_ck CHECK (content IS NULL OR content_purged_at IS NULL),
  CONSTRAINT model_calls_currency_ck CHECK (currency ~ '^[A-Z]{3}$'),
  CONSTRAINT model_calls_summary_ck CHECK (summary IS NULL OR char_length(summary) BETWEEN 1 AND 200)
) PARTITION BY RANGE (started_at);
COMMENT ON TABLE model_calls IS 'Registro de cada chamada de modelo pelo gateway (RNF-13): tokens, preço vigente usado, custo calculado pelo banco, resultado e, por 90 dias, o conteúdo. Linha nasce no início da chamada (resultado nulo = em curso) para que queda não perca o custo.';
COMMENT ON COLUMN model_calls.id IS 'Identificador interno (chave primária inteira, alvo das FKs); não sai do banco: API e eventos usam public_id.';
COMMENT ON COLUMN model_calls.public_id IS 'Id gerado pelo worker antes de chamar; com started_at, também fixada antes de chamar, é a chave de idempotência do registro.';
COMMENT ON COLUMN model_calls.wave_id IS 'Onda.';
COMMENT ON COLUMN model_calls.attempt IS 'Tentativa da onda.';
COMMENT ON COLUMN model_calls.step IS 'Passo do checkpoint a que a chamada pertence.';
COMMENT ON COLUMN model_calls.model_id IS 'Modelo chamado.';
COMMENT ON COLUMN model_calls.role IS 'Papel no par de modelos.';
COMMENT ON COLUMN model_calls.price_id IS 'Vigência de preço aplicada (preço vigente no início da chamada; tabela_precos_versao).';
COMMENT ON COLUMN model_calls.input_tokens IS 'Tokens de entrada reportados pelo provedor.';
COMMENT ON COLUMN model_calls.output_tokens IS 'Tokens de saída reportados.';
COMMENT ON COLUMN model_calls.read_cache_tokens IS 'Tokens lidos do cache.';
COMMENT ON COLUMN model_calls.write_cache_tokens IS 'Tokens gravados no cache (sem campo no evento de custo; lacuna registrada).';
COMMENT ON COLUMN model_calls.cost IS 'Custo calculado pelo gatilho a partir dos tokens e do preço (RNF-16).';
COMMENT ON COLUMN model_calls.currency IS 'Moeda do preço aplicado.';
COMMENT ON COLUMN model_calls.lost IS 'Custo entra em lost_cost (abortada, ou chamada em voo repetida após queda).';
COMMENT ON COLUMN model_calls.estimated_cost IS 'Chamada em voo na queda do worker sem uso informado pelo provedor: tokens de entrada contados antes da chamada x preço vigente (RNF-16, D-23).';
COMMENT ON COLUMN model_calls.result IS 'success, error ou aborted; nulo enquanto em curso.';
COMMENT ON COLUMN model_calls.input_hash IS 'SHA-256 da entrada entregue ao modelo (RNF-15).';
COMMENT ON COLUMN model_calls.content IS '[DP] Prompt e resposta comprimidos (registro de chamada; conteúdo do projeto). Nunca vai a evento. Expira após call_content_period (efeito expire_content); metadados ficam pelo audit_period (D-30).';
COMMENT ON COLUMN model_calls.content_purged_at IS 'Quando o conteúdo foi apagado pela retenção.';
COMMENT ON COLUMN model_calls.error_summary IS 'Erro do provedor resumido, sem conteúdo nem segredo.';
COMMENT ON COLUMN model_calls.summary IS 'Frase curta gerada pelo runtime a partir da ação, sem prompt nem resposta (RegistroChamada.resumo, PC-09).';
COMMENT ON CONSTRAINT model_calls_summary_ck ON model_calls IS 'Resumo curto (até 200 caracteres).';
COMMENT ON COLUMN model_calls.trace_id IS 'Trace OpenTelemetry da chamada (RNF-14).';
COMMENT ON COLUMN model_calls.started_at IS 'Início; data que escolhe o preço vigente.';
COMMENT ON COLUMN model_calls.finished_at IS 'Fim.';
COMMENT ON CONSTRAINT model_calls_public_id_uk ON model_calls IS 'Id público único com a chave de partição (exigência do particionamento; o uuid aleatório torna repetição impraticável).';
COMMENT ON CONSTRAINT model_calls_numbers_ck ON model_calls IS 'Números positivos.';
COMMENT ON CONSTRAINT model_calls_role_ck ON model_calls IS 'Papéis do par.';
COMMENT ON CONSTRAINT model_calls_tokens_ck ON model_calls IS 'Tokens e custo não negativos.';
COMMENT ON CONSTRAINT model_calls_result_ck ON model_calls IS 'Enumeração resultado do evento de custo.';
COMMENT ON CONSTRAINT model_calls_end_ck ON model_calls IS 'Chamada terminada tem resultado.';
COMMENT ON CONSTRAINT model_calls_lost_ck ON model_calls IS 'regra: US-12/CA-2 — chamada aborted é custo perdido, registrado e cobrado.';
COMMENT ON CONSTRAINT model_calls_estimated_ck ON model_calls IS 'Só custo perdido é estimado (chamada em voo na queda, D-23).';
COMMENT ON CONSTRAINT model_calls_hash_ck ON model_calls IS 'SHA-256 em hexadecimal.';
COMMENT ON CONSTRAINT model_calls_trace_ck ON model_calls IS 'trace-id W3C.';
COMMENT ON CONSTRAINT model_calls_purge_ck ON model_calls IS 'Conteúdo expurgado não volta.';
COMMENT ON CONSTRAINT model_calls_currency_ck ON model_calls IS 'Código ISO 4217.';
CREATE INDEX model_calls_wave_idx ON model_calls (wave_id, started_at);
COMMENT ON INDEX model_calls_wave_idx IS 'Chamadas da onda na página da onda e no custo por onda (US-09/CA-1, US-36/CA-1), soma por modelo e provedor (US-38/CA-1, US-24/CA-3); cobre a FK.';
CREATE INDEX model_calls_purge_idx ON model_calls (started_at) WHERE content IS NOT NULL;
COMMENT ON INDEX model_calls_purge_idx IS 'Job de retenção do conteúdo de chamadas (RNF-20: 90 dias).';

-- Custo calculado no banco: tokens × preço da vigência (por milhão de tokens). Diferença zero com o recálculo (RNF-16).
CREATE FUNCTION model_calls_compute_cost() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE p model_prices%ROWTYPE;
BEGIN
  SELECT * INTO p FROM model_prices WHERE id = NEW.price_id;
  IF p.model_id <> NEW.model_id OR p.effective_since > NEW.started_at THEN
    RAISE EXCEPTION 'preço não é do modelo ou não vigia no início da chamada'
      USING ERRCODE = '23514', CONSTRAINT = 'model_calls_compute_cost';
  END IF;
  NEW.currency := p.currency;
  NEW.cost := (NEW.input_tokens * p.input_price + NEW.output_tokens * p.output_price
              + NEW.read_cache_tokens * p.read_cache_price + NEW.write_cache_tokens * p.write_cache_price) / 1000000;
  IF TG_OP = 'UPDATE' AND OLD.result IS NOT NULL
     AND NOT pg_has_role(current_user, 'easycode_retention', 'MEMBER') THEN
    RAISE EXCEPTION 'chamada terminada é imutável'
      USING ERRCODE = '23514', CONSTRAINT = 'model_calls_immutable';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER model_calls_compute_cost BEFORE INSERT OR UPDATE ON model_calls
  FOR EACH ROW EXECUTE FUNCTION model_calls_compute_cost();
COMMENT ON TRIGGER model_calls_compute_cost ON model_calls IS 'regra: US-36/CA-3 — custo pelo preço vigente na data da chamada, calculado no banco (RNF-16); chamada terminada não muda (só expurgo de conteúdo).';
CREATE TRIGGER model_calls_no_delete BEFORE DELETE ON model_calls
  FOR EACH ROW EXECUTE FUNCTION block_delete('model_calls');
COMMENT ON TRIGGER model_calls_no_delete ON model_calls IS 'Registro de chamada não se apaga (RNF-13); só o conteúdo expira.';

CREATE TABLE tool_executions (
  id                bigint      GENERATED ALWAYS AS IDENTITY,
  public_id        uuid        NOT NULL DEFAULT gen_random_uuid(),
  wave_id           bigint      NOT NULL,
  attempt         integer     NOT NULL,
  step             integer     NOT NULL,
  tool        text        NOT NULL,
  permission         text        NOT NULL,
  refusal_reason     text        NULL,
  status          text        NULL,
  category         text        NULL,
  profile            text        NULL,
  image_digest     text        NULL,
  end_reason        text        NULL,
  command           text        NULL,
  summary            text        NULL,
  exit_code      integer     NULL,
  duration_ms        bigint      NULL,
  output             bytea       NULL,
  output_start      text        NULL,
  output_bytes       bigint      NULL,
  output_sha256      text        NULL,
  tests_total      integer     NULL,
  tests_passed   integer     NULL,
  tests_failed   integer     NULL,
  started_at       timestamptz NOT NULL DEFAULT now(),
  finished_at      timestamptz NULL,
  CONSTRAINT tool_executions_pkey PRIMARY KEY (id, started_at),
  CONSTRAINT tool_executions_public_id_uk UNIQUE (public_id, started_at),
  CONSTRAINT tool_executions_wave_fk FOREIGN KEY (wave_id) REFERENCES waves (id) ON DELETE RESTRICT,
  CONSTRAINT tool_executions_numbers_ck CHECK (attempt >= 1 AND step >= 1),
  CONSTRAINT tool_executions_name_ck CHECK (tool ~ '^[a-z][a-z0-9_]*$'),
  CONSTRAINT tool_executions_summary_ck CHECK (summary IS NULL OR char_length(summary) BETWEEN 1 AND 200),
  CONSTRAINT tool_executions_permission_ck CHECK (permission IN ('allowed','refused')),
  CONSTRAINT tool_executions_refusal_ck CHECK ((permission = 'refused') = (refusal_reason IS NOT NULL)
      AND (refusal_reason IS NULL OR refusal_reason IN ('tool_not_allowed','path_not_allowed','command_not_allowed','network_not_allowed','git_not_allowed','foreign_artifact_write'))),
  CONSTRAINT tool_executions_status_ck CHECK ((permission = 'allowed') = (status IS NOT NULL)
      AND (status IS NULL OR status IN ('ongoing','success','error','limit_exceeded'))),
  CONSTRAINT tool_executions_category_ck CHECK (category IS NULL OR (permission = 'allowed' AND category IN ('build','test','lint','scanner','validate_openapi','dependencies','other'))),
  CONSTRAINT tool_executions_end_reason_ck CHECK ((category IS NOT NULL AND status <> 'ongoing') = (end_reason IS NOT NULL)
      AND (end_reason IS NULL OR end_reason IN ('completed','time_limit','memory_limit','process_limit','output_limit','aborted','executor_error'))),
  CONSTRAINT tool_executions_limit_ck CHECK (end_reason IS NULL OR ((status = 'limit_exceeded') = (end_reason IN ('time_limit','memory_limit','process_limit','output_limit')))),
  CONSTRAINT tool_executions_output_meta_ck CHECK ((output_bytes IS NULL) = (output_sha256 IS NULL) AND (output_bytes IS NULL OR output_bytes >= 0)
      AND (output_sha256 IS NULL OR output_sha256 ~ '^[0-9a-f]{64}$') AND (image_digest IS NULL OR image_digest ~ '^sha256:[0-9a-f]{64}$')
      AND (output_start IS NULL OR char_length(output_start) <= 65536)),
  CONSTRAINT tool_executions_tests_ck CHECK (tests_total IS NULL OR (tests_passed + tests_failed <= tests_total AND tests_passed >= 0 AND tests_failed >= 0)),
  CONSTRAINT tool_executions_output_ck CHECK (output IS NULL OR octet_length(output) <= 16777216)
) PARTITION BY RANGE (started_at);
COMMENT ON TABLE tool_executions IS 'Chamada de ferramenta pelo agente (registro call_tool, evento wave.tool_called): allowed ou refused por privilégio mínimo, e a evidência da execução no sandbox (US-41, US-30, RNF-07, RNF-13).';
COMMENT ON COLUMN tool_executions.id IS 'Identificador interno (chave primária inteira, alvo das FKs); não sai do banco: API e eventos usam public_id.';
COMMENT ON COLUMN tool_executions.public_id IS 'Identificador opaco exposto (execucaoid, ExecucaoSandbox.id da API).';
COMMENT ON COLUMN tool_executions.wave_id IS 'Onda.';
COMMENT ON COLUMN tool_executions.attempt IS 'Tentativa da onda.';
COMMENT ON COLUMN tool_executions.step IS 'Passo do checkpoint.';
COMMENT ON COLUMN tool_executions.tool IS 'Nome da ferramenta (read_file, execute_command…).';
COMMENT ON COLUMN tool_executions.permission IS 'allowed ou refused (US-30/CA-1).';
COMMENT ON COLUMN tool_executions.refusal_reason IS 'Motivo da recusa (payload wave.tool_called).';
COMMENT ON COLUMN tool_executions.status IS 'ongoing, success, error ou limit_exceeded; nula se recusada.';
COMMENT ON COLUMN tool_executions.category IS 'Categoria do comando no sandbox (build, test, lint, scanner, validate_openapi, dependencies, other; PedidoExecucao do runner e ExecucaoSandbox da API); nula para ferramenta que não roda no sandbox.';
COMMENT ON COLUMN tool_executions.profile IS 'Perfil de execução usado (imagem da lista permitida e serviços de apoio declarados, ex.: PostgreSQL efêmero na rede interna do sandbox).';
COMMENT ON COLUMN tool_executions.image_digest IS 'Digest da imagem resolvida pelo runner (reprodutibilidade, RNF-15).';
COMMENT ON COLUMN tool_executions.end_reason IS 'Como terminou a execução no sandbox (MotivoFim do runner): completed, time_limit, memory_limit, process_limit, output_limit, aborted ou executor_error; o limite estourado é o recurso de US-41/CA-3.';
COMMENT ON COLUMN tool_executions.command IS 'Comando executado no sandbox, exibido pela API com autorização (nunca em evento).';
COMMENT ON COLUMN tool_executions.summary IS 'Frase curta gerada pelo runtime a partir da ação (ex.: gravou T-03.md; rodou testes: 42 ok, 1 falha), sem argumentos brutos (PC-09).';
COMMENT ON CONSTRAINT tool_executions_summary_ck ON tool_executions IS 'Resumo curto (até 200 caracteres).';
COMMENT ON COLUMN tool_executions.exit_code IS 'Código de saída (US-41/CA-1).';
COMMENT ON COLUMN tool_executions.duration_ms IS 'Duração.';
COMMENT ON COLUMN tool_executions.output IS 'Saída completa comprimida para baixar (US-41/CA-1), até 16 MiB.';
COMMENT ON COLUMN tool_executions.output_start IS 'Primeiros 64 KiB da saída para exibição (conteúdo não confiável).';
COMMENT ON COLUMN tool_executions.output_bytes IS 'Tamanho total da saída produzida.';
COMMENT ON COLUMN tool_executions.output_sha256 IS 'SHA-256 da saída completa (evidência; confere o download).';
COMMENT ON COLUMN tool_executions.tests_total IS 'Total de testes (US-41/CA-2).';
COMMENT ON COLUMN tool_executions.tests_passed IS 'Testes que passaram.';
COMMENT ON COLUMN tool_executions.tests_failed IS 'Testes que falharam.';
COMMENT ON COLUMN tool_executions.started_at IS 'Início.';
COMMENT ON COLUMN tool_executions.finished_at IS 'Fim.';
COMMENT ON CONSTRAINT tool_executions_public_id_uk ON tool_executions IS 'Id público único com a chave de partição (exigência do particionamento; o uuid aleatório torna repetição impraticável).';
COMMENT ON CONSTRAINT tool_executions_numbers_ck ON tool_executions IS 'Números positivos.';
COMMENT ON CONSTRAINT tool_executions_name_ck ON tool_executions IS 'Formato do nome de ferramenta.';
COMMENT ON CONSTRAINT tool_executions_permission_ck ON tool_executions IS 'Resultado da conferência de permissão.';
COMMENT ON CONSTRAINT tool_executions_refusal_ck ON tool_executions IS 'regra: US-30/CA-1 — chamada bloqueada é registrada com o motivo.';
COMMENT ON CONSTRAINT tool_executions_status_ck ON tool_executions IS 'Só a chamada allowed executa.';
COMMENT ON CONSTRAINT tool_executions_category_ck ON tool_executions IS 'Categorias do runner; só em chamada allowed.';
COMMENT ON CONSTRAINT tool_executions_end_reason_ck ON tool_executions IS 'Execução no sandbox terminada registra o motivo do fim (enumeração MotivoFim única com o runner e a API).';
COMMENT ON CONSTRAINT tool_executions_limit_ck ON tool_executions IS 'regra: US-41/CA-3 — limite excedido registra o recurso (end_reason limite_*).';
COMMENT ON CONSTRAINT tool_executions_output_meta_ck ON tool_executions IS 'Tamanho e hash da saída juntos; formatos de hash e digest; início da saída até 64 KiB.';
COMMENT ON CONSTRAINT tool_executions_tests_ck ON tool_executions IS 'Contagem de testes coerente.';
COMMENT ON CONSTRAINT tool_executions_output_ck ON tool_executions IS 'Saída guardada até 16 MiB (suposição; acima disso só o resumo).';
CREATE INDEX tool_executions_wave_idx ON tool_executions (wave_id, started_at);
COMMENT ON INDEX tool_executions_wave_idx IS 'Comandos da onda com evidência (US-41/CA-1), contagem de violações por onda (policy.least_privilege.max_violations_per_wave); cobre a FK.';
CREATE TRIGGER tool_executions_no_delete BEFORE DELETE ON tool_executions
  FOR EACH ROW EXECUTE FUNCTION block_delete('tool_executions');
COMMENT ON TRIGGER tool_executions_no_delete ON tool_executions IS 'Registro de ferramenta não se apaga (RNF-13).';

CREATE TABLE wave_questions (
  id             bigint      GENERATED ALWAYS AS IDENTITY,
  project_id     bigint      NOT NULL,
  wave_id        bigint      NOT NULL,
  code         text        NOT NULL,
  opened_at      timestamptz NOT NULL DEFAULT now(),
  answered_by bigint      NULL,
  answered_at  timestamptz NULL,
  origin         text        NULL,
  CONSTRAINT wave_questions_pkey PRIMARY KEY (id),
  CONSTRAINT wave_questions_project_fk FOREIGN KEY (project_id) REFERENCES projects (id) ON DELETE RESTRICT,
  CONSTRAINT wave_questions_wave_fk FOREIGN KEY (wave_id) REFERENCES waves (id) ON DELETE RESTRICT,
  CONSTRAINT wave_questions_answered_by_fk FOREIGN KEY (answered_by) REFERENCES users (id) ON DELETE RESTRICT,
  CONSTRAINT wave_questions_code_uk UNIQUE (project_id, code),
  CONSTRAINT wave_questions_code_ck CHECK (code ~ '^PERG-[0-9]+$'),
  CONSTRAINT wave_questions_origin_ck CHECK (origin = 'web'),
  CONSTRAINT wave_questions_answer_ck CHECK ((answered_by IS NULL) = (answered_at IS NULL)
                                               AND (answered_by IS NULL) = (origin IS NULL))
);
COMMENT ON TABLE wave_questions IS 'Pergunta do agente (PERG-n) que deixa a onda em awaiting_user: um ou mais itens numerados, cada um com sugestão e resposta própria (question_items; Pergunta.itens e RespostaPergunta.respostas da API). A resposta é um envio só, que responde todos os itens (US-10, US-17/CA-2).';
COMMENT ON COLUMN wave_questions.id IS 'Identificador.';
COMMENT ON COLUMN wave_questions.project_id IS 'Projeto.';
COMMENT ON COLUMN wave_questions.wave_id IS 'Onda que perguntou.';
COMMENT ON COLUMN wave_questions.code IS 'Id da pergunta no projeto (PERG-n, payload wave.question_opened).';
COMMENT ON COLUMN wave_questions.opened_at IS 'Quando foi feita; idade em Pendências (US-10/CA-2).';
COMMENT ON COLUMN wave_questions.answered_by IS 'Quem respondeu (US-10/RN-1).';
COMMENT ON COLUMN wave_questions.answered_at IS 'Quando respondeu.';
COMMENT ON COLUMN wave_questions.origin IS 'De onde respondeu: sempre web (cliente local somente leitura, PD-07).';
COMMENT ON CONSTRAINT wave_questions_code_uk ON wave_questions IS 'Id único no projeto.';
COMMENT ON CONSTRAINT wave_questions_code_ck ON wave_questions IS 'Formato PERG-n.';
COMMENT ON CONSTRAINT wave_questions_origin_ck ON wave_questions IS 'Resposta só pela web: o cliente local é somente leitura (PD-07, ADR-20).';
COMMENT ON CONSTRAINT wave_questions_answer_ck ON wave_questions IS 'regra: US-10/RN-1 — resposta registra quem, quando e de onde.';
CREATE INDEX wave_questions_open_idx ON wave_questions (opened_at) WHERE answered_at IS NULL;
COMMENT ON INDEX wave_questions_open_idx IS 'Pendências: perguntas abertas da mais antiga para a mais nova (US-48/CA-1, US-10/CA-2) e lembrete de espera humana (policy.wave_timeout).';
CREATE INDEX wave_questions_wave_idx ON wave_questions (wave_id);
COMMENT ON INDEX wave_questions_wave_idx IS 'Perguntas e respostas da onda para retomar com as respostas no contexto (US-10/CA-1); cobre a FK.';

CREATE TABLE question_items (
  question_id  bigint  NOT NULL,
  number       integer NOT NULL,
  text        text    NOT NULL,
  suggestion     text    NULL,
  answer     text    NULL,
  origin_source text    NULL,
  origin_excerpt text   NULL,
  CONSTRAINT question_items_pkey PRIMARY KEY (question_id, number),
  CONSTRAINT question_items_question_fk FOREIGN KEY (question_id) REFERENCES wave_questions (id) ON DELETE RESTRICT,
  CONSTRAINT question_items_number_ck CHECK (number >= 1),
  CONSTRAINT question_items_text_ck CHECK (btrim(text) <> ''),
  CONSTRAINT question_items_answer_ck CHECK (answer IS NULL OR char_length(btrim(answer)) BETWEEN 1 AND 10000),
  CONSTRAINT question_items_origin_ck CHECK ((origin_excerpt IS NULL OR origin_source IS NOT NULL) AND (origin_excerpt IS NULL OR char_length(origin_excerpt) <= 1000))
);
COMMENT ON TABLE question_items IS 'Item numerado de uma pergunta do agente, com a sugestão do agente e a resposta humana do item (US-17/CA-2, US-10/CA-1).';
COMMENT ON COLUMN question_items.question_id IS 'Pergunta.';
COMMENT ON COLUMN question_items.number IS 'Número do item na pergunta (1, 2, 3… sem lacuna; ItemResposta.numero).';
COMMENT ON COLUMN question_items.text IS 'Texto do item (conteúdo do projeto; não vai a evento).';
COMMENT ON COLUMN question_items.suggestion IS 'Sugestão de resposta do agente para o item.';
COMMENT ON COLUMN question_items.answer IS '[DP] Resposta humana do item (texto livre, até 10.000 caracteres).';
COMMENT ON COLUMN question_items.origin_source IS 'De onde veio a dúvida: id do artefato ou nome do anexo (Pergunta.itens[].origem, PL-29).';
COMMENT ON COLUMN question_items.origin_excerpt IS 'Trecho da fonte que motivou a dúvida (gerado pelo agente; até 1.000 caracteres).';
COMMENT ON CONSTRAINT question_items_origin_ck ON question_items IS 'Trecho só com a fonte e até 1.000 caracteres.';
COMMENT ON CONSTRAINT question_items_number_ck ON question_items IS 'Número positivo.';
COMMENT ON CONSTRAINT question_items_text_ck ON question_items IS 'Item não vazio.';
COMMENT ON CONSTRAINT question_items_answer_ck ON question_items IS 'Tamanho de ItemResposta.texto (1..10.000).';

-- Item nasce sem resposta, numerado em sequência, enquanto a pergunta está aberta; texto e sugestão não mudam;
-- a resposta é gravada uma vez (a retenção pode anonimizá-la).
CREATE FUNCTION question_items_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF pg_has_role(current_user, 'easycode_retention', 'MEMBER') AND TG_OP = 'UPDATE'
     AND (to_jsonb(NEW) - 'answer') = (to_jsonb(OLD) - 'answer') THEN
    RETURN NEW;
  END IF;
  IF TG_OP = 'INSERT' THEN
    PERFORM 1 FROM wave_questions WHERE id = NEW.question_id FOR UPDATE;
    IF NEW.answer IS NOT NULL
       OR EXISTS (SELECT 1 FROM wave_questions p WHERE p.id = NEW.question_id AND p.answered_at IS NOT NULL)
       OR NEW.number <> 1 + coalesce((SELECT max(number) FROM question_items WHERE question_id = NEW.question_id), 0) THEN
      RAISE EXCEPTION 'item nasce sem resposta, em sequência, com a pergunta aberta' USING ERRCODE = '23514', CONSTRAINT = 'question_items_immutable';
    END IF;
  ELSIF (NEW.question_id, NEW.number, NEW.text, NEW.suggestion) IS DISTINCT FROM (OLD.question_id, OLD.number, OLD.text, OLD.suggestion)
        OR OLD.answer IS NOT NULL THEN
    RAISE EXCEPTION 'item de pergunta é imutável; a resposta é gravada uma vez' USING ERRCODE = '23514', CONSTRAINT = 'question_items_immutable';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER question_items_immutable BEFORE INSERT OR UPDATE ON question_items
  FOR EACH ROW EXECUTE FUNCTION question_items_immutable();
COMMENT ON TRIGGER question_items_immutable ON question_items IS 'regra: US-10/RN-1 — itens numerados em sequência; resposta gravada uma vez, sem reescrever a pergunta (só a retenção anonimiza).';

-- Fim da transação: pergunta tem ao menos um item; respondida ⇔ todos os itens com resposta.
CREATE FUNCTION wave_questions_complete_answers() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE v_id bigint; v_answered_at timestamptz; v_items int; v_answered_items int;
BEGIN
  IF TG_TABLE_NAME = 'wave_questions' THEN v_id := NEW.id; ELSE v_id := NEW.question_id; END IF;
  SELECT answered_at INTO v_answered_at FROM wave_questions WHERE id = v_id;
  SELECT count(*), count(answer) INTO v_items, v_answered_items FROM question_items WHERE question_id = v_id;
  IF v_items = 0 OR (v_answered_at IS NULL AND v_answered_items > 0) OR (v_answered_at IS NOT NULL AND v_answered_items < v_items) THEN
    RAISE EXCEPTION 'pergunta precisa de itens, e a resposta responde todos os itens (itens %, respondidos %)', v_items, v_answered_items
      USING ERRCODE = '23514', CONSTRAINT = 'wave_questions_complete_answers';
  END IF;
  RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER wave_questions_complete_answers AFTER INSERT OR UPDATE ON wave_questions
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION wave_questions_complete_answers();
COMMENT ON TRIGGER wave_questions_complete_answers ON wave_questions IS 'regra: US-17/CA-2 — pergunta com itens numerados; responder grava a resposta de cada item na mesma transação (US-10/CA-1).';
CREATE CONSTRAINT TRIGGER question_items_complete_answers AFTER INSERT OR UPDATE ON question_items
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION wave_questions_complete_answers();
COMMENT ON TRIGGER question_items_complete_answers ON question_items IS 'regra: US-10/CA-1 — resposta de item só junto com a resposta da pergunta inteira (todos os itens).';

CREATE TABLE commands (
  id                  bigint        GENERATED ALWAYS AS IDENTITY,
  public_id          uuid          NOT NULL DEFAULT gen_random_uuid(),
  organization_id      bigint        NOT NULL,
  project_id          bigint        NULL,
  target_type           text          NOT NULL,
  wave_id             bigint        NULL,
  provider_key_id          bigint        NULL,
  artifact_id         bigint        NULL,
  target_id             bigint        GENERATED ALWAYS AS (coalesce(wave_id, provider_key_id, artifact_id, project_id)) STORED,
  type                text          NOT NULL,
  payload             jsonb         NOT NULL DEFAULT '{}',
  confirmed          boolean       NOT NULL DEFAULT false,
  requested_by      bigint        NOT NULL,
  origin              text          NOT NULL,
  idempotency_key  uuid          NOT NULL,
  body_hash          text          NOT NULL,
  requested_at       timestamptz   NOT NULL DEFAULT now(),
  status            text          NOT NULL DEFAULT 'pending',
  processed_at       timestamptz   NULL,
  refusal_reason       text          NULL,
  lost_cost       numeric(18,8) NULL,
  CONSTRAINT commands_pkey PRIMARY KEY (id),
  CONSTRAINT commands_public_id_uk UNIQUE (public_id),
  CONSTRAINT commands_organization_fk FOREIGN KEY (organization_id) REFERENCES organizations (id) ON DELETE RESTRICT,
  CONSTRAINT commands_project_fk FOREIGN KEY (project_id) REFERENCES projects (id) ON DELETE RESTRICT,
  CONSTRAINT commands_wave_fk FOREIGN KEY (wave_id) REFERENCES waves (id) ON DELETE RESTRICT,
  CONSTRAINT commands_provider_key_fk FOREIGN KEY (provider_key_id) REFERENCES provider_keys (id) ON DELETE RESTRICT,
  CONSTRAINT commands_artifact_fk FOREIGN KEY (artifact_id) REFERENCES artifacts (id) ON DELETE RESTRICT,
  CONSTRAINT commands_requested_by_fk FOREIGN KEY (requested_by) REFERENCES users (id) ON DELETE RESTRICT,
  CONSTRAINT commands_target_ck CHECK (target_type IN ('wave','project','provider_key','artifact')
                                     AND (target_type = 'wave') = (wave_id IS NOT NULL)
                                     AND (artifact_id IS NULL OR target_type = 'artifact')
                                     AND (target_type = 'provider_key') = (provider_key_id IS NOT NULL)
                                     AND (target_type = 'provider_key') = (project_id IS NULL)),
  CONSTRAINT commands_type_ck CHECK ((target_type = 'wave' AND type IN ('pause','pause_now','resume','restart','cancel','answer'))
                                     OR (target_type = 'project' AND type IN ('start','pause','resume','cancel','record_publication'))
                                     OR (target_type = 'provider_key' AND type = 'test_provider_key')
                                     OR (target_type = 'artifact' AND type = 'edit_artifact')),
  CONSTRAINT commands_confirmation_ck CHECK (type NOT IN ('pause_now','cancel','restart') OR confirmed),
  CONSTRAINT commands_payload_ck CHECK (jsonb_typeof(payload) = 'object' AND NOT (payload ?| ARRAY['key','value','encrypted_value','api_key'])),
  CONSTRAINT commands_hash_ck CHECK (body_hash ~ '^[0-9a-f]{64}$'),
  CONSTRAINT commands_origin_ck CHECK (origin = 'web'),
  CONSTRAINT commands_status_ck CHECK (status IN ('pending','applied','refused')),
  CONSTRAINT commands_processed_ck CHECK ((status = 'pending') = (processed_at IS NULL)),
  CONSTRAINT commands_refusal_ck CHECK ((status = 'refused') = (refusal_reason IS NOT NULL)),
  CONSTRAINT commands_cost_ck CHECK (lost_cost IS NULL OR (lost_cost >= 0 AND status = 'applied'))
);
COMMENT ON TABLE commands IS 'Fila de comandos que a API grava e o worker aplica (LISTEN/NOTIFY + SKIP LOCKED), reconferindo a guarda completa e gravando transição, auditoria e outbox na mesma transação: controle humano de onda e de projeto (só pela web: o cliente local é somente leitura, PD-07; pausar, retomar, cancelar e concluir projeto, US-51, US-52) e o teste de chave de provedor da organização (test_provider_key, US-15) e a edição humana de artefato (edit_artifact, US-53). Decisões humanas de gate e de achado (approve, reject, aceitar risco, descartar, novo retrabalho, mudança de escopo) não passam pela fila: a API as aplica de forma síncrona, com o outbox na mesma transação, e o worker reconfere as checagens bloqueantes antes de avançar a etapa. O servidor é a fonte de verdade (US-46/RN-1).';
COMMENT ON COLUMN commands.id IS 'Identificador interno (chave primária inteira, alvo das FKs); não sai do banco: API e eventos usam public_id.';
COMMENT ON COLUMN commands.public_id IS 'UUID v7 gerado pela API; é o id do Comando da API e o correlationid de todos os eventos que o comando causa.';
COMMENT ON COLUMN commands.organization_id IS 'Organização (escopo do comando sem projeto e isolamento RNF-04).';
COMMENT ON COLUMN commands.project_id IS 'Projeto; nulo só em comando da organização (alvo provider_key).';
COMMENT ON COLUMN commands.target_type IS 'wave, project, provider_key ou artifact.';
COMMENT ON COLUMN commands.wave_id IS 'Onda alvo (target_type onda).';
COMMENT ON COLUMN commands.provider_key_id IS 'Chave de provedor alvo (target_type provider_key).';
COMMENT ON COLUMN commands.artifact_id IS 'Artefato alvo da edição humana (target_type artefato); nulo na criação de artefato.';
COMMENT ON COLUMN commands.target_id IS 'Id do alvo, derivado da coluna tipada (wave, key de provedor, artefato ou projeto); target_type + target_id do contrato api → worker.';
COMMENT ON COLUMN commands.type IS 'Gatilho humano de estados.yaml: pause, pause_now, resume, restart, cancel ou answer (onda); start, pause, resume, cancel ou record_publication (projeto; concluirProjeto); edit_artifact (artefato, US-53); ou test_provider_key.';
COMMENT ON COLUMN commands.payload IS 'Corpo validado pela API (motivo, confirmações, texto da edição humana…; sem autorização de excedente desde a PD-02); texto livre fica aqui, nunca no evento. Nunca leva a chave do provedor (o valor selado fica só em provider_keys).';
COMMENT ON COLUMN commands.confirmed IS 'Confirmação explícita do usuário (pausar agora, cancelar, reiniciar).';
COMMENT ON COLUMN commands.requested_by IS 'Quem pediu.';
COMMENT ON COLUMN commands.origin IS 'Sempre web: o token do cliente local é somente leitura (PD-07, ADR-20).';
COMMENT ON COLUMN commands.idempotency_key IS 'Idempotency-Key do pedido (ADR-17); a resposta guardada por 24 h fica em idempotency_keys.';
COMMENT ON COLUMN commands.body_hash IS 'SHA-256 do corpo do pedido (mesma chave com outro corpo = 409 idempotencia-conflito).';
COMMENT ON COLUMN commands.requested_at IS 'Quando pediu (início da medição do RNF-10).';
COMMENT ON COLUMN commands.status IS 'pending, applied ou refused.';
COMMENT ON COLUMN commands.processed_at IS 'Quando o worker aplicou ou recusou.';
COMMENT ON COLUMN commands.refusal_reason IS 'Por que foi recusado (ex.: estado_invalido).';
COMMENT ON COLUMN commands.lost_cost IS 'Custo perdido pela ação (US-12/RN-1).';
COMMENT ON CONSTRAINT commands_public_id_uk ON commands IS 'Id público único.';
COMMENT ON CONSTRAINT commands_target_ck ON commands IS 'Alvo tipado coerente; só o comando da organização (provider_key) não tem project.';
COMMENT ON CONSTRAINT commands_artifact_fk ON commands IS 'Artefato alvo existente.';
COMMENT ON CONSTRAINT commands_type_ck ON commands IS 'Gatilhos humanos das máquinas wave e project, a edição humana de artifact e o teste de chave (US-51, US-52, US-53, US-15).';
COMMENT ON CONSTRAINT commands_confirmation_ck ON commands IS 'regra: US-12/CA-2 — pausar agora, cancel e restart só com confirmação (US-14/CA-1, US-13/CA-1).';
COMMENT ON CONSTRAINT commands_payload_ck ON commands IS 'Payload é objeto JSON e não carrega a chave do provedor (RNF-01).';
COMMENT ON CONSTRAINT commands_hash_ck ON commands IS 'SHA-256 em hexadecimal.';
COMMENT ON CONSTRAINT commands_origin_ck ON commands IS 'regra: US-12/RN-1 — ação de controle registra de onde; só web (cliente local somente leitura, PD-07).';
COMMENT ON CONSTRAINT commands_status_ck ON commands IS 'Ciclo do comando na fila.';
COMMENT ON CONSTRAINT commands_processed_ck ON commands IS 'Processado tem data.';
COMMENT ON CONSTRAINT commands_refusal_ck ON commands IS 'Recusa com motivo.';
COMMENT ON CONSTRAINT commands_cost_ck ON commands IS 'Custo perdido só em comando applied.';
CREATE INDEX commands_pending_idx ON commands (requested_at) WHERE status = 'pending';
COMMENT ON INDEX commands_pending_idx IS 'Worker consome a fila em ordem: … WHERE situacao = pendente ORDER BY requested_at FOR UPDATE SKIP LOCKED (RNF-10: até 5 s).';
CREATE INDEX commands_wave_idx ON commands (wave_id, requested_at) WHERE wave_id IS NOT NULL;
COMMENT ON INDEX commands_wave_idx IS 'Histórico de ações de controle da onda (quem, quando, de onde, custo perdido); cobre a FK.';
CREATE INDEX commands_project_idx ON commands (project_id, requested_at) WHERE project_id IS NOT NULL;
COMMENT ON INDEX commands_project_idx IS 'Cobre a FK project_id e lista ações de controle do projeto.';
CREATE INDEX commands_provider_key_idx ON commands (provider_key_id) WHERE provider_key_id IS NOT NULL;
COMMENT ON INDEX commands_provider_key_idx IS 'Teste da chave de provedor: a API espera o resultado do comando da linha testing (US-15/CA-1); cobre a FK.';

-- Alvo do comando pertence ao escopo: projeto da organização, onda do projeto, chave da organização ainda em teste.
-- Depois de gravado, só o processamento muda (situação, data, recusa, custo perdido).
CREATE FUNCTION commands_consistent() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'UPDATE' THEN
    IF (to_jsonb(NEW) - 'status' - 'processed_at' - 'refusal_reason' - 'lost_cost' - 'target_id')
       <> (to_jsonb(OLD) - 'status' - 'processed_at' - 'refusal_reason' - 'lost_cost' - 'target_id')
       OR OLD.status <> 'pending' THEN
      RAISE EXCEPTION 'comando é imutável; só o processamento pendente → aplicado | recusado' USING ERRCODE = '23514', CONSTRAINT = 'commands_consistent';
    END IF;
    RETURN NEW;
  END IF;
  IF NEW.project_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM projects p WHERE p.id = NEW.project_id AND p.organization_id = NEW.organization_id)
     OR NEW.wave_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM waves o WHERE o.id = NEW.wave_id AND o.project_id = NEW.project_id)
     OR NEW.provider_key_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM provider_keys k WHERE k.id = NEW.provider_key_id AND k.organization_id = NEW.organization_id AND k.state = 'testing') THEN
    RAISE EXCEPTION 'alvo do comando fora do escopo (organização, projeto ou chave em teste)' USING ERRCODE = '23514', CONSTRAINT = 'commands_consistent';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER commands_consistent BEFORE INSERT OR UPDATE ON commands
  FOR EACH ROW EXECUTE FUNCTION commands_consistent();
COMMENT ON TRIGGER commands_consistent ON commands IS 'Alvo no escopo da organização (RNF-04) e comando imutável depois de gravado, salvo o processamento.';
CREATE TRIGGER commands_no_delete BEFORE DELETE ON commands
  FOR EACH ROW EXECUTE FUNCTION block_delete();
COMMENT ON TRIGGER commands_no_delete ON commands IS 'Comando não se apaga (histórico de ações de controle, RNF-13).';

CREATE TABLE idempotency_keys (
  id           bigint      GENERATED ALWAYS AS IDENTITY,
  user_id   bigint      NOT NULL,
  key        uuid        NOT NULL,
  operation     text        NOT NULL,
  body_hash   text        NOT NULL,
  http_status  smallint    NOT NULL,
  answer     jsonb       NOT NULL,
  resource_id   text        NULL,
  created_at    timestamptz NOT NULL DEFAULT now(),
  expires_at    timestamptz NOT NULL DEFAULT now() + interval '24 hours',
  CONSTRAINT idempotency_keys_pkey PRIMARY KEY (id),
  CONSTRAINT idempotency_keys_key_uk UNIQUE (user_id, key),
  CONSTRAINT idempotency_keys_user_fk FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE RESTRICT,
  CONSTRAINT idempotency_keys_operation_ck CHECK (operation ~ '^[a-z][A-Za-z0-9]{1,63}$'),
  CONSTRAINT idempotency_keys_hash_ck CHECK (body_hash ~ '^[0-9a-f]{64}$'),
  CONSTRAINT idempotency_keys_status_ck CHECK (http_status BETWEEN 200 AND 599),
  CONSTRAINT idempotency_keys_expires_ck CHECK (expires_at > created_at AND expires_at <= created_at + interval '24 hours')
);
COMMENT ON TABLE idempotency_keys IS 'Idempotency-Key de todo POST de comando e de criação (ADR-17): a API grava chave, usuário, operação, hash do corpo e a resposta na mesma transação do efeito; a repetição com o mesmo corpo devolve a mesma resposta, e com outro corpo responde 409 idempotencia-conflito. Vale 24 h; a tarefa de retenção apaga as vencidas.';
COMMENT ON COLUMN idempotency_keys.id IS 'Identificador interno (a chave de idempotência é uuid do cliente e não serve de PK, ADR-18 v2).';
COMMENT ON COLUMN idempotency_keys.user_id IS 'Quem enviou (a chave é única por usuário).';
COMMENT ON COLUMN idempotency_keys.key IS 'Idempotency-Key (UUID gerado pelo cliente por intenção de ação).';
COMMENT ON COLUMN idempotency_keys.operation IS 'operationId da API (criarProjeto, pausarOnda, aprovarGate…); mesma chave em outra operação é conflito.';
COMMENT ON COLUMN idempotency_keys.body_hash IS 'SHA-256 do corpo normalizado do pedido.';
COMMENT ON COLUMN idempotency_keys.http_status IS 'Status da primeira resposta, devolvido igual na repetição.';
COMMENT ON COLUMN idempotency_keys.answer IS '[DP] Corpo da primeira resposta (pode conter nome e texto do projeto); expira em 24 h.';
COMMENT ON COLUMN idempotency_keys.resource_id IS 'Id do recurso criado ou do comando gravado, quando houver.';
COMMENT ON COLUMN idempotency_keys.created_at IS 'Quando a chave foi usada pela primeira vez.';
COMMENT ON COLUMN idempotency_keys.expires_at IS 'Fim da janela de 24 h; vencida, a chave pode ser reusada (a linha é sobrescrita) ou apagada pela retenção.';
COMMENT ON CONSTRAINT idempotency_keys_key_uk ON idempotency_keys IS 'Uma chave por usuário: serve a busca da repetição e o alvo do ON CONFLICT (a PK é inteira, ADR-18 v2; a chave é uuid do cliente).';
COMMENT ON CONSTRAINT idempotency_keys_operation_ck ON idempotency_keys IS 'Formato de operationId.';
COMMENT ON CONSTRAINT idempotency_keys_hash_ck ON idempotency_keys IS 'SHA-256 em hexadecimal.';
COMMENT ON CONSTRAINT idempotency_keys_status_ck ON idempotency_keys IS 'Status HTTP de resposta final.';
COMMENT ON CONSTRAINT idempotency_keys_expires_ck ON idempotency_keys IS 'Janela de idempotência de até 24 h (ADR-17).';
CREATE INDEX idempotency_keys_expires_idx ON idempotency_keys (expires_at);
COMMENT ON INDEX idempotency_keys_expires_idx IS 'Tarefa de retenção: apaga as chaves vencidas (ADR-17, 24 h).';

CREATE FUNCTION idempotency_keys_window() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF OLD.expires_at > now() THEN
    RAISE EXCEPTION 'chave de idempotência em vigor não muda: repetição devolve a resposta guardada' USING ERRCODE = '23514', CONSTRAINT = 'idempotency_keys_window';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER idempotency_keys_window BEFORE UPDATE ON idempotency_keys
  FOR EACH ROW EXECUTE FUNCTION idempotency_keys_window();
COMMENT ON TRIGGER idempotency_keys_window ON idempotency_keys IS 'Dentro das 24 h a chave não é reescrita (INSERT … ON CONFLICT DO UPDATE … WHERE expires_at <= now() reusa só a vencida; ADR-17).';

-- =============================================================================
-- 9. Versões de artefato e vínculos
-- =============================================================================
CREATE TABLE artifact_versions (
  id             bigint      GENERATED ALWAYS AS IDENTITY,
  artifact_id    bigint      NOT NULL,
  version         integer     NOT NULL,
  wave_id        bigint      NULL,
  author          text        NOT NULL,
  author_user_id bigint    NULL,
  reviewed_by   text        NULL,
  attachments         jsonb       NOT NULL DEFAULT '[]',
  wave_code    text        NOT NULL,
  description      text        NOT NULL,
  finding_id      bigint      NULL,
  path        text        NOT NULL,
  git_commit     text        NULL,
  header      jsonb       NOT NULL,
  body          text        NOT NULL,
  body_tsv      tsvector    GENERATED ALWAYS AS (to_tsvector('portuguese', body)) STORED,
  content_hash  text        NOT NULL,
  created_at      timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT artifact_versions_pkey PRIMARY KEY (id),
  CONSTRAINT artifact_versions_artifact_fk FOREIGN KEY (artifact_id) REFERENCES artifacts (id) ON DELETE RESTRICT,
  CONSTRAINT artifact_versions_wave_fk FOREIGN KEY (wave_id) REFERENCES waves (id) ON DELETE RESTRICT,
  CONSTRAINT artifact_versions_finding_fk FOREIGN KEY (finding_id) REFERENCES findings (artifact_id) ON DELETE RESTRICT,
  CONSTRAINT artifact_versions_author_user_fk FOREIGN KEY (author_user_id) REFERENCES users (id) ON DELETE RESTRICT,
  CONSTRAINT artifact_versions_version_uk UNIQUE (artifact_id, version),
  CONSTRAINT artifact_versions_version_ck CHECK (version >= 1),
  CONSTRAINT artifact_versions_author_ck CHECK (author IN ('requirements-agent','architect-agent','dba-agent','designer-agent','developer-agent','qa-agent','security-agent','orchestrator','human')),
  CONSTRAINT artifact_versions_human_ck CHECK ((author = 'human') = (author_user_id IS NOT NULL)
                                               AND (reviewed_by IS NULL OR author = 'human')
                                               AND (reviewed_by IS NULL OR reviewed_by IN ('requirements-agent','architect-agent','dba-agent','designer-agent','developer-agent','qa-agent','security-agent'))),
  CONSTRAINT artifact_versions_attachments_ck CHECK (jsonb_typeof(attachments) = 'array'),
  CONSTRAINT artifact_versions_wave_ck CHECK (wave_id IS NOT NULL OR author = 'orchestrator'),
  CONSTRAINT artifact_versions_wave_code_ck CHECK (wave_code ~ '^(REQ|ARQ|DBA|DSG|VAL|DEV|QA|SEG|REL)-[0-9]{2,}$'),
  CONSTRAINT artifact_versions_description_ck CHECK (char_length(btrim(description)) >= 3),
  CONSTRAINT artifact_versions_commit_ck CHECK (git_commit IS NULL OR git_commit ~ '^([0-9a-f]{40}|[0-9a-f]{64})$'),
  CONSTRAINT artifact_versions_hash_ck CHECK (content_hash ~ '^[0-9a-f]{64}$'),
  CONSTRAINT artifact_versions_header_ck CHECK (jsonb_typeof(header) = 'object' AND (header ->> 'versao')::int = version)
);
COMMENT ON TABLE artifact_versions IS 'Versão de um artefato: entrada de historico, commit git, cabeçalho YAML (JSONB, validado no esquema do tipo) e cópia do corpo para leitura e busca textual (motor de contexto). Versão commitada é imutável, salvo campos de acompanhamento e estado.';
COMMENT ON COLUMN artifact_versions.id IS 'Identificador interno.';
COMMENT ON COLUMN artifact_versions.artifact_id IS 'Artefato.';
COMMENT ON COLUMN artifact_versions.version IS 'Número da versão (US-26/CA-2).';
COMMENT ON COLUMN artifact_versions.wave_id IS 'Onda que produziu (US-26/CA-3); nulo só para o orquestrador.';
COMMENT ON COLUMN artifact_versions.author IS 'Autor do cabeçalho: agent, orchestrator ou human (edição humana, US-53, PD-03).';
COMMENT ON COLUMN artifact_versions.author_user_id IS 'Pessoa autora da edição humana (editado_por do cabeçalho; autor humano); a onda de edit_review do agente da etapa registra reviewed_by.';
COMMENT ON COLUMN artifact_versions.reviewed_by IS 'Agente da etapa que validou e formatou a edição humana (nulo até a onda edit_review concluir; a edição só vale depois).';
COMMENT ON COLUMN artifact_versions.attachments IS 'Demais arquivos da versão na pasta do artefato: lista de {caminho, media_type, size_bytes, sha256} (Artefato.anexos, PL-24); conteúdo no git.';
COMMENT ON COLUMN artifact_versions.wave_code IS 'Campo onda do cabeçalho e do historico.';
COMMENT ON COLUMN artifact_versions.description IS 'historico[].descricao da versão.';
COMMENT ON COLUMN artifact_versions.finding_id IS 'ACH que motivou a versão (historico[].achado; US-27/CA-2).';
COMMENT ON COLUMN artifact_versions.path IS 'Caminho do arquivo no repositório.';
COMMENT ON COLUMN artifact_versions.git_commit IS 'Commit que gravou a versão (SHA-1 ou SHA-256); preenchido no commit da onda (US-42/CA-2).';
COMMENT ON COLUMN artifact_versions.header IS 'Cabeçalho YAML em JSON.';
COMMENT ON COLUMN artifact_versions.body IS 'Corpo Markdown (cópia; a fonte de verdade é o git, conferida por content_hash).';
COMMENT ON COLUMN artifact_versions.body_tsv IS 'Vetor de busca textual em português do corpo (gerado).';
COMMENT ON COLUMN artifact_versions.content_hash IS 'SHA-256 do arquivo completo.';
COMMENT ON COLUMN artifact_versions.created_at IS 'Quando a versão foi gravada (historico[].data).';
COMMENT ON CONSTRAINT artifact_versions_version_uk ON artifact_versions IS 'regra: US-26/RN-1 — cada versão existe uma vez; mudança gera nova versão.';
COMMENT ON CONSTRAINT artifact_versions_version_ck ON artifact_versions IS 'Versão positiva.';
COMMENT ON CONSTRAINT artifact_versions_author_ck ON artifact_versions IS 'Enumeração autor do cabeçalho comum (com human, PD-03).';
COMMENT ON CONSTRAINT artifact_versions_author_user_fk ON artifact_versions IS 'Autor humano é usuário existente.';
COMMENT ON CONSTRAINT artifact_versions_human_ck ON artifact_versions IS 'regra: US-53/RN-1 — versão de autor human tem a pessoa (editado_por); reviewed_by só nela, gravado pela onda de revisão da edição.';
COMMENT ON CONSTRAINT artifact_versions_attachments_ck ON artifact_versions IS 'Anexos em lista JSON.';
COMMENT ON CONSTRAINT artifact_versions_wave_ck ON artifact_versions IS 'Versão de agente sempre ligada à onda.';
COMMENT ON CONSTRAINT artifact_versions_wave_code_ck ON artifact_versions IS 'Padrão onda do cabeçalho comum.';
COMMENT ON CONSTRAINT artifact_versions_description_ck ON artifact_versions IS 'Descrição com ao menos 3 caracteres.';
COMMENT ON CONSTRAINT artifact_versions_commit_ck ON artifact_versions IS 'Hash de commit git.';
COMMENT ON CONSTRAINT artifact_versions_hash_ck ON artifact_versions IS 'SHA-256 em hexadecimal.';
COMMENT ON CONSTRAINT artifact_versions_header_ck ON artifact_versions IS 'Cabeçalho é objeto e sua versao é a da linha (historico tem a versão atual).';
CREATE INDEX artifact_versions_body_tsv_idx ON artifact_versions USING gin (body_tsv);
COMMENT ON INDEX artifact_versions_body_tsv_idx IS 'Motor de contexto: busca textual no corpo dos artefatos do projeto (decisão de arquitetura do motor de contexto, sem embeddings).';
CREATE INDEX artifact_versions_header_idx ON artifact_versions USING gin (header jsonb_path_ops);
COMMENT ON INDEX artifact_versions_header_idx IS 'Motor de contexto e checagens: busca por conteúdo do cabeçalho (@>), ex.: US com prioridade must, T que cobre US-03.';
CREATE INDEX artifact_versions_wave_idx ON artifact_versions (wave_id) WHERE wave_id IS NOT NULL;
COMMENT ON INDEX artifact_versions_wave_idx IS 'Saídas da onda (guarda de concluir, payload onda.concluida.artefatos, navegação US-26/CA-3); cobre a FK.';
CREATE INDEX artifact_versions_finding_idx ON artifact_versions (finding_id) WHERE finding_id IS NOT NULL;
COMMENT ON INDEX artifact_versions_finding_idx IS 'Versões motivadas por um ACH (ciclos do achado, US-27/CA-2); cobre a FK.';

ALTER TABLE wave_inputs
  ADD CONSTRAINT wave_inputs_wave_fk FOREIGN KEY (wave_id) REFERENCES waves (id) ON DELETE RESTRICT,
  ADD CONSTRAINT wave_inputs_version_fk FOREIGN KEY (artifact_version_id) REFERENCES artifact_versions (id) ON DELETE RESTRICT;
COMMENT ON CONSTRAINT wave_inputs_wave_fk ON wave_inputs IS 'Onda existente.';
COMMENT ON CONSTRAINT wave_inputs_version_fk ON wave_inputs IS 'Versão existente.';
CREATE INDEX wave_inputs_version_idx ON wave_inputs (artifact_version_id);
COMMENT ON INDEX wave_inputs_version_idx IS 'Análise de impacto: ondas que consumiram uma versão (reavaliação após reabertura); cobre a FK.';

-- Versão commitada: só mudam estado/substituido_por e os campos de acompanhamento do tipo no cabeçalho,
-- e git_commit passa de nulo a valor uma vez. Antes do commit a onda reescreve livremente (US-29/CA-1).
CREATE FUNCTION artifact_versions_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
  v_type   text;
  v_free_keys text[] := ARRAY['estado','substituido_por','revisado_por'];
BEGIN
  IF pg_has_role(current_user, 'easycode_retention', 'MEMBER') OR OLD.git_commit IS NULL THEN
    RETURN NEW;
  END IF;
  SELECT type INTO v_type FROM artifacts WHERE id = OLD.artifact_id;
  v_free_keys := v_free_keys || CASE v_type
      WHEN 'T'       THEN ARRAY['situacao','justificativa','registro_dev']
      WHEN 'CT'      THEN ARRAY['execucao']
      WHEN 'ACH'     THEN ARRAY['situacao','ciclos','aceite','escalonamentos','resolucao','artefatos_alterados','motivo_descarte']
      WHEN 'AMEACAS' THEN ARRAY['conferencia']
      ELSE ARRAY[]::text[] END;
  IF NEW.artifact_id <> OLD.artifact_id OR NEW.version <> OLD.version OR NEW.body <> OLD.body
     OR NEW.git_commit IS DISTINCT FROM OLD.git_commit OR NEW.path <> OLD.path
     OR (NEW.header - v_free_keys) <> (OLD.header - v_free_keys) THEN
    RAISE EXCEPTION 'versão commitada de artefato é imutável (fora dos campos de acompanhamento)'
      USING ERRCODE = '23514', CONSTRAINT = 'artifact_versions_immutable';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER artifact_versions_immutable BEFORE UPDATE ON artifact_versions
  FOR EACH ROW EXECUTE FUNCTION artifact_versions_immutable();
COMMENT ON TRIGGER artifact_versions_immutable ON artifact_versions IS 'regra: US-26/RN-1 — versão aprovada nunca é alterada; só campos de acompanhamento (artefatos/README.md) e estado.';
CREATE TRIGGER artifact_versions_no_delete BEFORE DELETE ON artifact_versions
  FOR EACH ROW EXECUTE FUNCTION block_delete();
COMMENT ON TRIGGER artifact_versions_no_delete ON artifact_versions IS 'Versão anterior nunca se apaga (policy.history_preservation).';

CREATE TABLE artifact_links (
  id                       bigint  GENERATED ALWAYS AS IDENTITY,
  project_id               bigint  NOT NULL,
  origin_version_id         bigint  NOT NULL,
  field                    text    NOT NULL,
  edge                   text    NULL,
  strong                    boolean NOT NULL,
  destination_type             text    NOT NULL,
  destination                  text    NOT NULL,
  destination_artifact_code  text    NULL,
  CONSTRAINT artifact_links_pkey PRIMARY KEY (id),
  CONSTRAINT artifact_links_project_fk FOREIGN KEY (project_id) REFERENCES projects (id) ON DELETE RESTRICT,
  CONSTRAINT artifact_links_origin_fk FOREIGN KEY (origin_version_id) REFERENCES artifact_versions (id) ON DELETE RESTRICT,
  CONSTRAINT artifact_links_uk UNIQUE (origin_version_id, field, destination),
  CONSTRAINT artifact_links_field_ck CHECK (field ~ '^[a-z_][a-z0-9_\[\]\.]*$'),
  CONSTRAINT artifact_links_edge_ck CHECK (edge IS NULL OR edge ~ '^E-[0-9]{2,}$'),
  CONSTRAINT artifact_links_destination_type_ck CHECK (destination_type IN ('artifact','item','code')),
  CONSTRAINT artifact_links_destination_ck CHECK (btrim(destination) <> '' AND ((destination_type = 'code') = (destination_artifact_code IS NULL)))
);
COMMENT ON TABLE artifact_links IS 'Aresta do grafo de rastreabilidade extraída do cabeçalho de uma versão (rastreabilidade.md seção 1). Índice derivado: recalculado do cabeçalho quando muda campo de acompanhamento.';
COMMENT ON COLUMN artifact_links.id IS 'Identificador interno.';
COMMENT ON COLUMN artifact_links.project_id IS 'Projeto (busca reversa por destino).';
COMMENT ON COLUMN artifact_links.origin_version_id IS 'Versão de origem que referencia o destino.';
COMMENT ON COLUMN artifact_links.field IS 'Campo de origem no cabeçalho (vinculos, cobre[].us, historico[].achado…).';
COMMENT ON COLUMN artifact_links.edge IS 'Id da aresta na tabela de rastreabilidade (E-01…).';
COMMENT ON COLUMN artifact_links.strong IS 'Falso para vinculos genéricos (não contam completude).';
COMMENT ON COLUMN artifact_links.destination_type IS 'artifact, item (US-03/CA-2) ou code (commit, caminho, teste).';
COMMENT ON COLUMN artifact_links.destination IS 'Referência como escrita.';
COMMENT ON COLUMN artifact_links.destination_artifact_code IS 'Id do artefato de destino (parte antes de /); nulo para código.';
COMMENT ON CONSTRAINT artifact_links_uk ON artifact_links IS 'Uma aresta por origem, campo e destino; serve a leitura das arestas de uma versão.';
COMMENT ON CONSTRAINT artifact_links_field_ck ON artifact_links IS 'Formato do caminho de campo.';
COMMENT ON CONSTRAINT artifact_links_edge_ck ON artifact_links IS 'Formato do id de aresta.';
COMMENT ON CONSTRAINT artifact_links_destination_type_ck ON artifact_links IS 'Tipos de destino do grafo.';
COMMENT ON CONSTRAINT artifact_links_destination_ck ON artifact_links IS 'Destino não vazio; artefato de destino exceto para código.';
CREATE INDEX artifact_links_destination_idx ON artifact_links (project_id, destination_artifact_code);
COMMENT ON INDEX artifact_links_destination_idx IS 'Rastreabilidade reversa: o que aponta para US-03 (US-28/CA-1, matriz US-28/CA-2) e análise de impacto (rastreabilidade.md seção 4); cobre a FK project_id.';

-- =============================================================================
-- 10. Gate: rodadas, checagens executadas, confirmações humanas; ciclos e escalonamentos do achado
-- =============================================================================
CREATE TABLE gate_rounds (
  id                   bigint      GENERATED ALWAYS AS IDENTITY,
  project_id           bigint      NOT NULL,
  stage                text        NOT NULL,
  stage_id             bigint      NOT NULL,
  gate                 text        GENERATED ALWAYS AS ('gate.' || stage) STORED,
  number               integer     NOT NULL,
  state               text        NOT NULL DEFAULT 'evaluating',
  approval_mode       text        NOT NULL,
  required_approvals  smallint    NOT NULL DEFAULT 1,
  current_evaluation      integer     NOT NULL DEFAULT 1,
  decided_by_type        text        NULL,
  decided_at          timestamptz NULL,
  reason               text        NULL,
  opened_at            timestamptz NOT NULL DEFAULT now(),
  invalidated_at        timestamptz NULL,
  CONSTRAINT gate_rounds_pkey PRIMARY KEY (id),
  CONSTRAINT gate_rounds_stage_fk FOREIGN KEY (stage_id, project_id, stage) REFERENCES stages (id, project_id, stage) ON DELETE RESTRICT,
  CONSTRAINT gate_rounds_number_uk UNIQUE (project_id, stage, number),
  CONSTRAINT gate_rounds_stage_ck CHECK (stage IN ('requirements','architecture','data','design','validation','implementation','qa','security','release')),
  CONSTRAINT gate_rounds_number_ck CHECK (number >= 1 AND current_evaluation >= 1),
  CONSTRAINT gate_rounds_state_ck CHECK (state IN ('evaluating','awaiting_human','approved','rejected','invalidated')),
  CONSTRAINT gate_rounds_mode_ck CHECK (approval_mode IN ('human','automatic') AND (approval_mode = 'human' OR stage IN ('validation','implementation'))),
  CONSTRAINT gate_rounds_approvals_ck CHECK (required_approvals BETWEEN 1 AND 3 AND (required_approvals = 1 OR stage = 'release')),
  CONSTRAINT gate_rounds_decision_ck CHECK ((decided_by_type IS NULL) = (decided_at IS NULL)
                                            AND (decided_by_type IS NULL OR decided_by_type IN ('human','orchestrator'))
                                            AND (state NOT IN ('approved','rejected') OR decided_by_type IS NOT NULL)),
  CONSTRAINT gate_rounds_automatic_ck CHECK (NOT (state = 'approved' AND decided_by_type = 'orchestrator') OR approval_mode = 'automatic'),
  CONSTRAINT gate_rounds_reason_ck CHECK (NOT (state = 'rejected' AND decided_by_type = 'human') OR btrim(coalesce(reason, '')) <> ''),
  CONSTRAINT gate_rounds_invalidated_ck CHECK ((state = 'invalidated') = (invalidated_at IS NOT NULL))
);
COMMENT ON TABLE gate_rounds IS 'Rodada de gate de uma etapa (máquina gate): checagens, espera humana e decisão. Subject dos eventos: gates/<gate>/rodadas/<numero>.';
COMMENT ON COLUMN gate_rounds.id IS 'Identificador interno.';
COMMENT ON COLUMN gate_rounds.project_id IS 'Projeto.';
COMMENT ON COLUMN gate_rounds.stage_id IS 'stages.id da etapa (FK; project_id e stage acompanham).';
COMMENT ON COLUMN gate_rounds.stage IS 'Etapa dona da rodada (atividade paralela não tem gate).';
COMMENT ON COLUMN gate_rounds.gate IS 'Id do gate, derivado da etapa (gate.<etapa>).';
COMMENT ON COLUMN gate_rounds.number IS 'Número da rodada na etapa, a partir de 1.';
COMMENT ON COLUMN gate_rounds.state IS 'Estado na máquina gate.';
COMMENT ON COLUMN gate_rounds.approval_mode IS 'Modo copiado da configuração do gate na abertura.';
COMMENT ON COLUMN gate_rounds.required_approvals IS 'Aprovadores distintos exigidos, copiados na abertura (aprovacoes_minimas da API): 1; no gate.release, policy.required_human_approval.production_min_approvals (1 a 3, D-28).';
COMMENT ON COLUMN gate_rounds.current_evaluation IS 'Número da avaliação de checagens em curso (sobe em gate.reevaluation_started).';
COMMENT ON COLUMN gate_rounds.decided_by_type IS 'human ou orchestrator (aprovação automática; US-07/CA-1).';
COMMENT ON COLUMN gate_rounds.decided_at IS 'Quando foi decidida.';
COMMENT ON COLUMN gate_rounds.reason IS '[DP] Motivo humano da reprovação (texto livre; US-06/CA-2). Reprovação por checagem usa as checagens falhas.';
COMMENT ON COLUMN gate_rounds.opened_at IS 'Abertura (idade em Pendências).';
COMMENT ON COLUMN gate_rounds.invalidated_at IS 'Quando foi invalidada por bloqueio ou reabertura da etapa.';
COMMENT ON CONSTRAINT gate_rounds_stage_fk ON gate_rounds IS 'Rodada de etapa existente do projeto.';
COMMENT ON CONSTRAINT gate_rounds_number_uk ON gate_rounds IS 'Rodadas numeradas por etapa.';
COMMENT ON CONSTRAINT gate_rounds_stage_ck ON gate_rounds IS 'Só etapas com gate.';
COMMENT ON CONSTRAINT gate_rounds_number_ck ON gate_rounds IS 'Números positivos.';
COMMENT ON CONSTRAINT gate_rounds_state_ck ON gate_rounds IS 'Estados da máquina gate.';
COMMENT ON CONSTRAINT gate_rounds_mode_ck ON gate_rounds IS 'regra: US-07/CA-3 — automático só em validation e implementation.';
COMMENT ON CONSTRAINT gate_rounds_approvals_ck ON gate_rounds IS 'Faixa de production_min_approvals; mais de uma só no gate.release (D-28).';
COMMENT ON CONSTRAINT gate_rounds_decision_ck ON gate_rounds IS 'Gate decidido tem quem e quando (invariante da máquina gate).';
COMMENT ON CONSTRAINT gate_rounds_automatic_ck ON gate_rounds IS 'regra: US-07/RN-1 — orchestrator só aprova em modo automático; Release sempre humana.';
COMMENT ON CONSTRAINT gate_rounds_reason_ck ON gate_rounds IS 'regra: US-06/CA-2 — reprovação humana exige motivo.';
COMMENT ON CONSTRAINT gate_rounds_invalidated_ck ON gate_rounds IS 'Data de invalidação só em invalidated.';
CREATE UNIQUE INDEX gate_rounds_active_uk ON gate_rounds (project_id, stage) WHERE state IN ('evaluating','awaiting_human');
COMMENT ON INDEX gate_rounds_active_uk IS 'Invariante: no máximo uma rodada ativa por etapa; serve a busca da rodada corrente da etapa (visão de workflow, decisão).';
CREATE INDEX gate_rounds_awaiting_idx ON gate_rounds (opened_at) WHERE state = 'awaiting_human';
COMMENT ON INDEX gate_rounds_awaiting_idx IS 'Pendências e dashboard: gates aguardando aprovação, do mais antigo ao mais novo (US-48/CA-1, US-50/CA-1).';

CREATE TRIGGER gate_rounds_valid_transition BEFORE INSERT OR UPDATE OF state ON gate_rounds
  FOR EACH ROW EXECUTE FUNCTION validate_transition('gate', 'state');
COMMENT ON TRIGGER gate_rounds_valid_transition ON gate_rounds IS 'Estado inicial avaliando e só transições de estados.yaml (máquina gate).';

CREATE TABLE gate_round_artifacts (
  round_id           bigint NOT NULL,
  artifact_version_id  bigint NOT NULL,
  CONSTRAINT gate_round_artifacts_pkey PRIMARY KEY (round_id, artifact_version_id),
  CONSTRAINT gate_round_artifacts_round_fk FOREIGN KEY (round_id) REFERENCES gate_rounds (id) ON DELETE RESTRICT,
  CONSTRAINT gate_round_artifacts_version_fk FOREIGN KEY (artifact_version_id) REFERENCES artifact_versions (id) ON DELETE RESTRICT
);
COMMENT ON TABLE gate_round_artifacts IS 'Versões de artefato avaliadas na rodada; o que o gate aprovou fica preso à versão (guarda in_review → aprovado; base para saber se a aprovação vale após reabertura).';
COMMENT ON COLUMN gate_round_artifacts.round_id IS 'Rodada.';
COMMENT ON COLUMN gate_round_artifacts.artifact_version_id IS 'Versão avaliada.';
CREATE INDEX gate_round_artifacts_version_idx ON gate_round_artifacts (artifact_version_id);
COMMENT ON INDEX gate_round_artifacts_version_idx IS 'Em que rodada uma versão foi aprovada (consulta de artefato US-26/CA-1); cobre a FK.';

CREATE TABLE check_executions (
  id             bigint      GENERATED ALWAYS AS IDENTITY,
  project_id     bigint      NOT NULL,
  round_id      bigint      NULL,
  evaluation      integer     NULL,
  wave_id        bigint      NULL,
  check_id       text        NOT NULL,
  type           text        NOT NULL,
  severity     text        NOT NULL,
  result      text        NOT NULL,
  evidence      jsonb       NULL,
  confirmed_by bigint      NULL,
  confirmed_at  timestamptz NULL,
  executed_at   timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT check_executions_pkey PRIMARY KEY (id),
  CONSTRAINT check_executions_project_fk FOREIGN KEY (project_id) REFERENCES projects (id) ON DELETE RESTRICT,
  CONSTRAINT check_executions_round_fk FOREIGN KEY (round_id) REFERENCES gate_rounds (id) ON DELETE RESTRICT,
  CONSTRAINT check_executions_wave_fk FOREIGN KEY (wave_id) REFERENCES waves (id) ON DELETE RESTRICT,
  CONSTRAINT check_executions_confirmed_by_fk FOREIGN KEY (confirmed_by) REFERENCES users (id) ON DELETE RESTRICT,
  CONSTRAINT check_executions_uk UNIQUE (round_id, evaluation, check_id),
  CONSTRAINT check_executions_target_ck CHECK (num_nonnulls(round_id, wave_id) = 1 AND (round_id IS NULL) = (evaluation IS NULL)),
  CONSTRAINT check_executions_name_ck CHECK (check_id ~ '^[a-z][a-z0-9_]*\.[a-z0-9_.]+$'),
  CONSTRAINT check_executions_type_ck CHECK (type IN ('automatic','human')),
  CONSTRAINT check_executions_severity_ck CHECK (severity IN ('blocks','alert')),
  CONSTRAINT check_executions_result_ck CHECK (result IN ('passed','failed','error','pending') AND (result <> 'pending' OR type = 'human')),
  CONSTRAINT check_executions_confirmation_ck CHECK ((type = 'human' AND result <> 'pending') = (confirmed_by IS NOT NULL AND confirmed_at IS NOT NULL)
                                                      AND (confirmed_by IS NULL) = (confirmed_at IS NULL))
);
COMMENT ON TABLE check_executions IS 'Execução de uma checagem nomeada (checagens.yaml) numa avaliação de rodada de gate ou numa guarda de onda (concluir, submit_for_review), com resultado e evidência; checagem humana guarda a confirmação do aprovador (US-31/CA-1).';
COMMENT ON COLUMN check_executions.id IS 'Identificador interno.';
COMMENT ON COLUMN check_executions.project_id IS 'Projeto.';
COMMENT ON COLUMN check_executions.round_id IS 'Rodada de gate; nula em guarda de onda.';
COMMENT ON COLUMN check_executions.evaluation IS 'Avaliação da rodada (1 + reavaliações).';
COMMENT ON COLUMN check_executions.wave_id IS 'Onda cuja guarda rodou a checagem.';
COMMENT ON COLUMN check_executions.check_id IS 'Id da checagem (artifact.schema_valid…).';
COMMENT ON COLUMN check_executions.type IS 'automatic ou human.';
COMMENT ON COLUMN check_executions.severity IS 'blocks ou alert, já com promoções do projeto.';
COMMENT ON COLUMN check_executions.result IS 'passed, failed, error ou pending (humana aguardando o aprovador).';
COMMENT ON COLUMN check_executions.evidence IS 'Relatório: caminhos de erro, ids e contagens; sem conteúdo de artefato.';
COMMENT ON COLUMN check_executions.confirmed_by IS 'Aprovador que confirmou a checagem humana (checagens_humanas_confirmadas).';
COMMENT ON COLUMN check_executions.confirmed_at IS 'Quando confirmou.';
COMMENT ON COLUMN check_executions.executed_at IS 'Quando rodou.';
COMMENT ON CONSTRAINT check_executions_uk ON check_executions IS 'Uma execução por checagem em cada avaliação; serve a leitura do resultado da rodada.';
COMMENT ON CONSTRAINT check_executions_target_ck ON check_executions IS 'Pertence a uma rodada (com avaliação) ou a uma onda.';
COMMENT ON CONSTRAINT check_executions_name_ck ON check_executions IS 'Formato do id de checagem.';
COMMENT ON CONSTRAINT check_executions_type_ck ON check_executions IS 'Tipos de checagem.';
COMMENT ON CONSTRAINT check_executions_severity_ck ON check_executions IS 'Severidades de falha.';
COMMENT ON CONSTRAINT check_executions_result_ck ON check_executions IS 'Só checagem human fica pending.';
COMMENT ON CONSTRAINT check_executions_confirmation_ck ON check_executions IS 'Checagem human decidida registra quem e quando.';
CREATE INDEX check_executions_wave_idx ON check_executions (wave_id) WHERE wave_id IS NOT NULL;
COMMENT ON INDEX check_executions_wave_idx IS 'Erros de esquema devolvidos ao agente na mesma onda (US-29/CA-1, CA-2); cobre a FK.';
CREATE INDEX check_executions_project_idx ON check_executions (project_id, executed_at);
COMMENT ON INDEX check_executions_project_idx IS 'Cobre a FK project_id e indicadores de qualidade do dashboard (checagens falhas por projeto, US-50).';

CREATE TABLE gate_confirmations (
  id           bigint      GENERATED ALWAYS AS IDENTITY,
  round_id    bigint      NOT NULL,
  evaluation    integer     NOT NULL,
  user_id   bigint      NOT NULL,
  decision      text        NOT NULL,
  reason       text        NULL,
  origin       text        NOT NULL,
  decided_at  timestamptz NOT NULL DEFAULT now(),
  discarded_at timestamptz NULL,
  CONSTRAINT gate_confirmations_pkey PRIMARY KEY (id),
  CONSTRAINT gate_confirmations_round_fk FOREIGN KEY (round_id) REFERENCES gate_rounds (id) ON DELETE RESTRICT,
  CONSTRAINT gate_confirmations_user_fk FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE RESTRICT,
  CONSTRAINT gate_confirmations_evaluation_ck CHECK (evaluation >= 1),
  CONSTRAINT gate_confirmations_decision_ck CHECK (decision IN ('approve','reject')),
  CONSTRAINT gate_confirmations_reason_ck CHECK (decision = 'approve' OR btrim(coalesce(reason, '')) <> ''),
  CONSTRAINT gate_confirmations_origin_ck CHECK (origin = 'web'),
  CONSTRAINT gate_confirmations_discard_ck CHECK (discarded_at IS NULL OR (decision = 'approve' AND discarded_at >= decided_at))
);
COMMENT ON TABLE gate_confirmations IS 'Decisão humana individual numa rodada de gate (approve ou reject), com quem, quando e de onde (US-06/CA-1). No gate.release, aprovações antes da que completa required_approvals são parciais (gate.approval_recorded; aprovacoes_parciais da API, D-28); a reavaliação as marca como descartadas, sem apagar.';
COMMENT ON COLUMN gate_confirmations.id IS 'Identificador interno; a ordem dá a ordem das decisões.';
COMMENT ON COLUMN gate_confirmations.round_id IS 'Rodada.';
COMMENT ON COLUMN gate_confirmations.evaluation IS 'Avaliação da rodada em que a decisão foi dada (gate_rounds.current_evaluation no ato).';
COMMENT ON COLUMN gate_confirmations.user_id IS 'Aprovador.';
COMMENT ON COLUMN gate_confirmations.decision IS 'approve ou reject.';
COMMENT ON COLUMN gate_confirmations.reason IS '[DP] Motivo da reprovação (texto livre humano).';
COMMENT ON COLUMN gate_confirmations.origin IS 'Sempre web: decisão de gate é crítica (anti-CSRF e reautenticação recente; US-06/RN-2); o cliente local não decide gate.';
COMMENT ON COLUMN gate_confirmations.decided_at IS 'Quando decidiu.';
COMMENT ON COLUMN gate_confirmations.discarded_at IS 'Quando a aprovação parcial foi descartada pela reavaliação (awaiting_human → avaliando); nulo = vale.';
COMMENT ON CONSTRAINT gate_confirmations_evaluation_ck ON gate_confirmations IS 'Avaliação positiva.';
COMMENT ON CONSTRAINT gate_confirmations_decision_ck ON gate_confirmations IS 'Decisões possíveis.';
COMMENT ON CONSTRAINT gate_confirmations_reason_ck ON gate_confirmations IS 'regra: US-06/CA-2 — reprovar exige motivo.';
COMMENT ON CONSTRAINT gate_confirmations_origin_ck ON gate_confirmations IS 'regra: US-06/CA-6 — decisão de gate só pela web; pelo cliente local é recusada (US-08/CA-4).';
COMMENT ON CONSTRAINT gate_confirmations_discard_ck ON gate_confirmations IS 'Só aprovação (parcial) é descartada, depois de dada.';
CREATE UNIQUE INDEX gate_confirmations_person_uk ON gate_confirmations (round_id, user_id) WHERE discarded_at IS NULL;
COMMENT ON INDEX gate_confirmations_person_uk IS 'Uma decisão válida por pessoa e rodada: aprovações mínimas contam pessoas distintas (D-28); serve a contagem das aprovações da rodada.';
CREATE INDEX gate_confirmations_user_idx ON gate_confirmations (user_id);
COMMENT ON INDEX gate_confirmations_user_idx IS 'Decisões de uma pessoa (trilha por usuário, anonimização); cobre a FK.';

CREATE FUNCTION gate_confirmations_require_approver() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE r gate_rounds%ROWTYPE;
BEGIN
  SELECT * INTO r FROM gate_rounds WHERE id = NEW.round_id FOR UPDATE;
  IF r.state <> 'awaiting_human' THEN
    RAISE EXCEPTION 'rodada não aguarda decisão humana' USING ERRCODE = '23514', CONSTRAINT = 'gate_confirmations_require_approver';
  END IF;
  IF NOT EXISTS (SELECT 1 FROM project_members m
                  WHERE m.project_id = r.project_id AND m.user_id = NEW.user_id AND m.role = 'approver') THEN
    RAISE EXCEPTION 'sem permissão' USING ERRCODE = '42501', CONSTRAINT = 'gate_confirmations_require_approver';
  END IF;
  NEW.evaluation := r.current_evaluation;
  RETURN NEW;
END $$;
CREATE TRIGGER gate_confirmations_require_approver BEFORE INSERT ON gate_confirmations
  FOR EACH ROW EXECUTE FUNCTION gate_confirmations_require_approver();
COMMENT ON TRIGGER gate_confirmations_require_approver ON gate_confirmations IS 'regra: US-02/CA-2 — só aprovador do projeto decide, e só com a rodada aguardando humano.';

-- Carência de 24 h (policy.separation_of_duties): quem atribuiu a si mesmo o papel aprovador não aprova gate do
-- projeto antes de 24 h. O papel que o criador recebe na criação do projeto (mesma transação) não conta.
CREATE FUNCTION gate_confirmations_recent_role() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF EXISTS (SELECT 1 FROM gate_rounds r
               JOIN project_members m ON m.project_id = r.project_id AND m.user_id = NEW.user_id AND m.role = 'approver'
               JOIN projects p ON p.id = r.project_id
              WHERE r.id = NEW.round_id
                AND m.assigned_by = NEW.user_id
                AND m.assigned_at > now() - interval '24 hours'
                AND NOT (p.created_by = NEW.user_id AND m.assigned_at = p.created_at)) THEN
    RAISE EXCEPTION 'papel atribuído há menos de 24 horas' USING ERRCODE = '42501', CONSTRAINT = 'gate_confirmations_recent_role';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER gate_confirmations_recent_role BEFORE INSERT ON gate_confirmations
  FOR EACH ROW WHEN (NEW.decision = 'approve') EXECUTE FUNCTION gate_confirmations_recent_role();
COMMENT ON TRIGGER gate_confirmations_recent_role ON gate_confirmations IS 'regra: US-02/CA-8 — quem se atribuiu o papel aprovador há menos de 24 h não aprova gate do projeto (US-02/RN-4, policy.separation_of_duties).';

-- Segregação de funções no gate.release: quem aceitou risco no ciclo corrente (depois da última Release aprovada
-- do projeto) não aprova o gate.release desse ciclo.
CREATE FUNCTION gate_confirmations_separation() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE r gate_rounds%ROWTYPE; v_start timestamptz;
BEGIN
  SELECT * INTO r FROM gate_rounds WHERE id = NEW.round_id;
  IF r.stage <> 'release' THEN
    RETURN NEW;
  END IF;
  SELECT coalesce(max(x.decided_at), '-infinity') INTO v_start FROM gate_rounds x
   WHERE x.project_id = r.project_id AND x.stage = 'release' AND x.state = 'approved' AND x.id <> r.id;
  IF EXISTS (SELECT 1 FROM findings a
              WHERE a.project_id = r.project_id AND a.status = 'accepted'
                AND a.accepted_by = NEW.user_id AND a.accepted_at > v_start) THEN
    RAISE EXCEPTION 'quem aceitou risco não aprova a Release do mesmo ciclo' USING ERRCODE = '42501', CONSTRAINT = 'gate_confirmations_separation';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER gate_confirmations_separation BEFORE INSERT ON gate_confirmations
  FOR EACH ROW WHEN (NEW.decision = 'approve') EXECUTE FUNCTION gate_confirmations_separation();
COMMENT ON TRIGGER gate_confirmations_separation ON gate_confirmations IS 'regra: US-08/CA-5 — quem aceitou risco no ciclo não aprova o gate.release do mesmo ciclo (policy.separation_of_duties).';

-- Fecho da decisão (fim da transação): reprovação leva a rodada a reprovado; aprovação que completa o mínimo,
-- a aprovado; aprovação abaixo do mínimo (parcial, só no gate.release) mantém awaiting_human (D-28).
CREATE FUNCTION gate_confirmations_effect() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE r gate_rounds%ROWTYPE; v_approvals int;
BEGIN
  SELECT * INTO r FROM gate_rounds WHERE id = NEW.round_id;
  SELECT count(*) INTO v_approvals FROM gate_confirmations
   WHERE round_id = NEW.round_id AND decision = 'approve' AND discarded_at IS NULL;
  IF (NEW.decision = 'reject' AND r.state <> 'rejected')
     OR (NEW.decision = 'approve' AND v_approvals >= r.required_approvals AND r.state <> 'approved')
     OR (NEW.decision = 'approve' AND v_approvals < r.required_approvals AND r.state NOT IN ('awaiting_human','evaluating','invalidated')) THEN
    RAISE EXCEPTION 'decisão humana incoerente com o estado da rodada (aprovações %/%, estado %)', v_approvals, r.required_approvals, r.state
      USING ERRCODE = '23514', CONSTRAINT = 'gate_confirmations_effect';
  END IF;
  RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER gate_confirmations_effect AFTER INSERT ON gate_confirmations
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION gate_confirmations_effect();
COMMENT ON TRIGGER gate_confirmations_effect ON gate_confirmations IS 'regra: US-06/CA-1 — decisão registrada e transição do gate na mesma transação: reprovação reprova; aprovação completa o mínimo e aprova; parcial (gate.release, D-28) segue awaiting_human.';

-- Reavaliação (awaiting_human → avaliando) descarta as aprovações parciais, sem apagar (D-28).
CREATE FUNCTION gate_rounds_discard_partials() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  UPDATE gate_confirmations SET discarded_at = now()
   WHERE round_id = NEW.id AND decision = 'approve' AND discarded_at IS NULL;
  RETURN NULL;
END $$;
CREATE TRIGGER gate_rounds_discard_partials AFTER UPDATE OF state ON gate_rounds
  FOR EACH ROW WHEN (OLD.state = 'awaiting_human' AND NEW.state = 'evaluating')
  EXECUTE FUNCTION gate_rounds_discard_partials();
COMMENT ON TRIGGER gate_rounds_discard_partials ON gate_rounds IS 'Reavaliação da rodada descarta as aprovações parciais (ficam no registro, marcadas; D-28).';

-- Aprovar uma rodada: confirmações suficientes, nenhuma checagem bloqueante falha na avaliação atual,
-- nenhum ACH Crítico/Alto aberto da etapa (na Release, de qualquer etapa). Trava a linha da etapa.
CREATE FUNCTION gate_rounds_valid_approval() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  PERFORM 1 FROM stages WHERE project_id = NEW.project_id AND stage = NEW.stage FOR UPDATE;
  IF EXISTS (SELECT 1 FROM check_executions c
              WHERE c.round_id = NEW.id AND c.evaluation = NEW.current_evaluation
                AND c.severity = 'blocks' AND c.result <> 'passed') THEN
    RAISE EXCEPTION 'checagem bloqueante não passou' USING ERRCODE = '23514', CONSTRAINT = 'gate_rounds_valid_approval';
  END IF;
  IF EXISTS (SELECT 1 FROM findings a
              WHERE a.project_id = NEW.project_id
                AND a.status IN ('open','in_rework','revalidating','escalated','awaiting_scope')
                AND a.severity IN ('critical','high')
                AND (a.origin_stage = NEW.stage OR NEW.stage = 'release')) THEN
    RAISE EXCEPTION 'há ACH Crítico ou Alto aberto' USING ERRCODE = '23514', CONSTRAINT = 'gate_rounds_valid_approval';
  END IF;
  IF NEW.decided_by_type = 'human' AND
     (SELECT count(*) FROM gate_confirmations f WHERE f.round_id = NEW.id AND f.decision = 'approve' AND f.discarded_at IS NULL) < NEW.required_approvals THEN
    RAISE EXCEPTION 'aprovações humanas insuficientes' USING ERRCODE = '23514', CONSTRAINT = 'gate_rounds_valid_approval';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER gate_rounds_valid_approval BEFORE UPDATE OF state ON gate_rounds
  FOR EACH ROW WHEN (NEW.state = 'approved' AND OLD.state <> 'approved') EXECUTE FUNCTION gate_rounds_valid_approval();
COMMENT ON TRIGGER gate_rounds_valid_approval ON gate_rounds IS 'regra: US-06/RN-1 — aprovação nunca passa por cima de checagem bloqueante nem de ACH Crítico/Alto aberto (US-31/RN-1, US-08/CA-2); aprovação humana exige required_approvals aprovadores distintos não descartados (D-28).';

CREATE TABLE finding_cycles (
  finding_id            bigint      NOT NULL,
  number               integer     NOT NULL,
  rework_wave_id   bigint      NOT NULL,
  commit_or_version     text        NULL,
  revalidation_wave_id  bigint      NULL,
  result            text        NULL,
  started_at          timestamptz NOT NULL DEFAULT now(),
  revalidated_at        timestamptz NULL,
  CONSTRAINT finding_cycles_pkey PRIMARY KEY (finding_id, number),
  CONSTRAINT finding_cycles_finding_fk FOREIGN KEY (finding_id) REFERENCES findings (artifact_id) ON DELETE RESTRICT,
  CONSTRAINT finding_cycles_rework_fk FOREIGN KEY (rework_wave_id) REFERENCES waves (id) ON DELETE RESTRICT,
  CONSTRAINT finding_cycles_revalidation_fk FOREIGN KEY (revalidation_wave_id) REFERENCES waves (id) ON DELETE RESTRICT,
  CONSTRAINT finding_cycles_number_ck CHECK (number >= 1),
  CONSTRAINT finding_cycles_result_ck CHECK ((result IS NULL) = (revalidated_at IS NULL)
                                               AND (result IS NULL OR (result IN ('fixed','not_fixed') AND revalidation_wave_id IS NOT NULL)))
);
COMMENT ON TABLE finding_cycles IS 'Ciclo de retrabalho e revalidação de um ACH (ciclos[] do esquema ACH).';
COMMENT ON COLUMN finding_cycles.finding_id IS 'ACH.';
COMMENT ON COLUMN finding_cycles.number IS 'Número do ciclo (1 a 3; acima só com rodada extra autorizada).';
COMMENT ON COLUMN finding_cycles.rework_wave_id IS 'Onda de retrabalho na etapa destino.';
COMMENT ON COLUMN finding_cycles.commit_or_version IS 'Commit (código) ou versão do artefato que trouxe a correção.';
COMMENT ON COLUMN finding_cycles.revalidation_wave_id IS 'Onda em que o emissor revalidou.';
COMMENT ON COLUMN finding_cycles.result IS 'fixed ou not_fixed.';
COMMENT ON COLUMN finding_cycles.started_at IS 'Início do ciclo.';
COMMENT ON COLUMN finding_cycles.revalidated_at IS 'Registro da revalidação.';
COMMENT ON CONSTRAINT finding_cycles_number_ck ON finding_cycles IS 'Número positivo.';
COMMENT ON CONSTRAINT finding_cycles_result_ck ON finding_cycles IS 'Resultado só com onda de revalidação e data.';

CREATE TABLE finding_escalations (
  finding_id       bigint      NOT NULL,
  number          integer     NOT NULL,
  escalated_at     timestamptz NOT NULL DEFAULT now(),
  reason          text        NOT NULL,
  decision         text        NULL,
  authorized_by  bigint      NULL,
  decided_at     timestamptz NULL,
  modified_by   text        NULL,
  starting_point_artifacts text[] NOT NULL DEFAULT '{}',
  CONSTRAINT finding_escalations_pkey PRIMARY KEY (finding_id, number),
  CONSTRAINT finding_escalations_finding_fk FOREIGN KEY (finding_id) REFERENCES findings (artifact_id) ON DELETE RESTRICT,
  CONSTRAINT finding_escalations_authorized_fk FOREIGN KEY (authorized_by) REFERENCES users (id) ON DELETE RESTRICT,
  CONSTRAINT finding_escalations_number_ck CHECK (number >= 1),
  CONSTRAINT finding_escalations_reason_ck CHECK (btrim(reason) <> ''),
  CONSTRAINT finding_escalations_decision_ck CHECK ((decision IS NULL) = (authorized_by IS NULL)
                                                     AND (decision IS NULL) = (decided_at IS NULL)
                                                     AND (decision IS NULL OR decision IN ('new_rework','accept_risk','scope_change'))),
  CONSTRAINT finding_escalations_scope_ck CHECK ((decision = 'scope_change') = (modified_by IS NOT NULL)
                                                    AND (modified_by IS NULL OR modified_by IN ('requirements-agent','human'))
                                                    AND (decision IS DISTINCT FROM 'scope_change') = (cardinality(starting_point_artifacts) = 0))
);
COMMENT ON TABLE finding_escalations IS 'Escalonamento de ACH para decisão humana, em ordem (escalonamentos[] do esquema ACH; US-32/CA-4).';
COMMENT ON COLUMN finding_escalations.finding_id IS 'ACH.';
COMMENT ON COLUMN finding_escalations.number IS 'Ordem do escalonamento.';
COMMENT ON COLUMN finding_escalations.escalated_at IS 'Quando escalou.';
COMMENT ON COLUMN finding_escalations.reason IS 'Por que escalou (gerado pelo orquestrador).';
COMMENT ON COLUMN finding_escalations.decision IS 'new_rework, accept_risk ou scope_change; nula enquanto pendente.';
COMMENT ON COLUMN finding_escalations.authorized_by IS 'Pessoa que decidiu (sempre humana: FK para usuário).';
COMMENT ON COLUMN finding_escalations.decided_at IS 'Quando decidiu.';
COMMENT ON COLUMN finding_escalations.modified_by IS 'Na mudança de escopo: quem altera os requisitos, requirements-agent (onda de retrabalho) ou human (edição humana revisada pelo agente; PD-06, escalonamentos[].modified_by do ACH).';
COMMENT ON COLUMN finding_escalations.starting_point_artifacts IS 'Na mudança de escopo: códigos dos artefatos de requisitos reabertos como ponto de partida (escalonamentos[].starting_point_artifacts).';
COMMENT ON CONSTRAINT finding_escalations_number_ck ON finding_escalations IS 'Número positivo.';
COMMENT ON CONSTRAINT finding_escalations_reason_ck ON finding_escalations IS 'Motivo não vazio.';
COMMENT ON CONSTRAINT finding_escalations_decision_ck ON finding_escalations IS 'Decisão humana registra authorized_by e decided_at juntos.';
COMMENT ON CONSTRAINT finding_escalations_scope_ck ON finding_escalations IS 'regra: US-32/CA-4 — mudança de escopo registra quem altera e os artefatos de partida, e só ela (PD-06).';
CREATE UNIQUE INDEX finding_escalations_pending_uk ON finding_escalations (finding_id) WHERE decision IS NULL;
COMMENT ON INDEX finding_escalations_pending_uk IS 'No máximo um escalonamento pendente por ACH; serve a busca da decisão pendente em Pendências.';

-- Limite de ciclos: número = anterior + 1 e no máximo 3 + rodadas extras autorizadas (uma por decisão new_rework).
CREATE FUNCTION finding_cycles_limit() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE v_last int; v_extra_rounds int;
BEGIN
  PERFORM 1 FROM findings WHERE artifact_id = NEW.finding_id FOR UPDATE;
  SELECT coalesce(max(number), 0) INTO v_last FROM finding_cycles WHERE finding_id = NEW.finding_id;
  SELECT count(*) INTO v_extra_rounds FROM finding_escalations WHERE finding_id = NEW.finding_id AND decision = 'new_rework';
  IF NEW.number <> v_last + 1 THEN
    RAISE EXCEPTION 'ciclo fora de ordem' USING ERRCODE = '23514', CONSTRAINT = 'finding_cycles_limit';
  END IF;
  IF NEW.number > 3 + v_extra_rounds THEN
    RAISE EXCEPTION 'limite de ciclos do achado atingido sem autorização humana' USING ERRCODE = '23514', CONSTRAINT = 'finding_cycles_limit';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER finding_cycles_limit BEFORE INSERT ON finding_cycles
  FOR EACH ROW EXECUTE FUNCTION finding_cycles_limit();
COMMENT ON TRIGGER finding_cycles_limit ON finding_cycles IS 'regra: US-32/RN-1 — limite de 3 ciclos por achado; rodada extra só com decisão humana new_rework (policy.rework_cycle_limit).';

-- =============================================================================
-- 11. Outbox de eventos de domínio e trilha de auditoria
-- =============================================================================
CREATE TABLE events (
  id              bigint      GENERATED ALWAYS AS IDENTITY,
  public_id      uuid        NOT NULL DEFAULT gen_random_uuid(),
  organization_id  bigint      NOT NULL,
  project_id      bigint      NULL,
  sequence       bigint      NOT NULL,
  type            text        NOT NULL,
  subject         text        NOT NULL,
  waveid          text        NULL,
  correlationid   uuid        NOT NULL,
  causationid     uuid        NULL,
  occurred_at     timestamptz NOT NULL,
  envelope        jsonb       NOT NULL,
  written_at      timestamptz NOT NULL DEFAULT now(),
  published_at    timestamptz NULL,
  global_position  bigint      NULL,
  CONSTRAINT events_pkey PRIMARY KEY (id, occurred_at),
  CONSTRAINT events_public_id_uk UNIQUE (public_id, occurred_at),
  CONSTRAINT events_organization_fk FOREIGN KEY (organization_id) REFERENCES organizations (id) ON DELETE RESTRICT,
  CONSTRAINT events_project_fk FOREIGN KEY (project_id) REFERENCES projects (id) ON DELETE RESTRICT,
  CONSTRAINT events_sequence_ck CHECK (sequence >= 1),
  CONSTRAINT events_type_ck CHECK (type ~ '^br\.easycode\.(project|release|stage|gate|artifact|finding|wave|skill|member)\.[a-z][a-z0-9_]*\.v[1-9][0-9]*$'),
  CONSTRAINT events_flow_ck CHECK ((project_id IS NULL) = (type ~ '^br\.easycode\.skill\.')
                                     AND (project_id IS NOT NULL OR (waveid IS NULL AND (envelope ->> 'source') = '/organization'))),
  CONSTRAINT events_waveId_ck CHECK (waveid IS NULL OR waveid ~ '^(REQ|ARQ|DBA|DSG|VAL|DEV|QA|SEG|REL)-[0-9]{2,}$'),
  CONSTRAINT events_envelope_ck CHECK ((envelope ->> 'id') = public_id::text AND (envelope ->> 'sequence')::bigint = sequence
                                        AND ((envelope ->> 'projectid') IS NULL) = (project_id IS NULL)),
  CONSTRAINT events_publication_ck CHECK ((published_at IS NULL) = (global_position IS NULL) AND (global_position IS NULL OR global_position >= 1))
) PARTITION BY RANGE (occurred_at);
COMMENT ON TABLE events IS 'Outbox de eventos de domínio (CloudEvents): gravado na mesma transação da mudança de estado, publicado depois (pelo menos uma vez). É também trilha de auditoria: nunca é apagado nem limpo (guarda pelo audit_period; D-32), e a anonimização do projeto não o toca (payload só com ids opacos). Fonte do histórico da onda, do replay SSE por projeto (Last-Event-ID = sequencia; janela de 24 h é filtro por occurred_at na leitura, ADR-11) e do stream /eu/eventos (global_position). Telemetria não passa aqui.';
COMMENT ON COLUMN events.id IS 'Identificador interno (chave primária inteira, alvo das FKs); não sai do banco: API e eventos usam public_id.';
COMMENT ON COLUMN events.public_id IS 'id do CloudEvent (UUIDv7 gerado pela aplicação); repetido igual em toda reentrega.';
COMMENT ON COLUMN events.organization_id IS 'Organização do fluxo; nos eventos de skill (source /organizacao) é o fluxo inteiro.';
COMMENT ON COLUMN events.project_id IS 'Projeto (projetoid); nulo só nos eventos de skill da organização (D-26).';
COMMENT ON COLUMN events.sequence IS 'Posição no fluxo, de 1 em 1 sem lacuna: por projeto (project_counters) ou, nos eventos de skill, pela organização (organization_counters), na mesma transação.';
COMMENT ON COLUMN events.type IS 'type versionado (br.easycode.<entidade>.<acao>.vN); só domínio.';
COMMENT ON COLUMN events.subject IS 'Instância afetada (projeto, ondas/DBA-01, gates/gate.data/rodadas/2, skills/padrao-go/versoes/3…).';
COMMENT ON COLUMN events.waveid IS 'Onda envolvida, quando houver (filtro do histórico da onda).';
COMMENT ON COLUMN events.correlationid IS 'Gatilho raiz da cadeia.';
COMMENT ON COLUMN events.causationid IS 'Evento que causou esta transição.';
COMMENT ON COLUMN events.occurred_at IS 'time do CloudEvent (instante da transição); limita a janela de replay do SSE.';
COMMENT ON COLUMN events.envelope IS 'Envelope completo validado (envelope.schema.json); payload fino, sem conteúdo nem dado pessoal além do ator.id opaco.';
COMMENT ON COLUMN events.written_at IS 'Commit no outbox.';
COMMENT ON COLUMN events.published_at IS 'Quando o publicador entregou ao barramento; nulo = pendente.';
COMMENT ON COLUMN events.global_position IS 'Posição global do outbox (id SSE de /eu/eventos): atribuída pelo publicador ao publicar, um por vez sob trava consultiva, em ordem de publicação de eventos já confirmados; nula até publicar (ADR-11).';
COMMENT ON CONSTRAINT events_public_id_uk ON events IS 'Id público único com a chave de partição (exigência do particionamento; o uuid aleatório torna repetição impraticável).';
COMMENT ON CONSTRAINT events_sequence_ck ON events IS 'Sequência a partir de 1.';
COMMENT ON CONSTRAINT events_type_ck ON events IS 'Padrão type do envelope, restrito às entidades de domínio (inclui skill, D-26).';
COMMENT ON CONSTRAINT events_flow_ck ON events IS 'Sem projeto só os eventos br.easycode.skill.* da organização (source /organizacao, sem ondaid; D-26).';
COMMENT ON CONSTRAINT events_waveId_ck ON events IS 'Padrão ondaid.';
COMMENT ON CONSTRAINT events_envelope_ck ON events IS 'Colunas iguais aos atributos do envelope (projetoid ausente quando não há projeto); o valor de projetoid (public_id do projeto) é conferido pelo gatilho events_sequence_no_gap.';
COMMENT ON CONSTRAINT events_publication_ck ON events IS 'Posição global existe exatamente quando publicado (ADR-11).';
-- Tabela particionada: índice único precisa conter a chave de partição (occurred_at), então a unicidade de
-- sequencia por fluxo e de global_position não é índice único. Ela vem da construção (contador travado na mesma
-- transação; posição global sob trava consultiva) e é conferida pelos gatilhos events_sequence_no_gap e
-- events_immutable. Os índices abaixo servem às leituras.
CREATE INDEX events_sequence_idx ON events (project_id, sequence) WHERE project_id IS NOT NULL;
COMMENT ON INDEX events_sequence_idx IS 'Replay SSE do projeto em ordem de sequencia (Last-Event-ID; US-09/CA-3) e conferência de lacuna e repetição pelo gatilho; cobre a FK.';
CREATE INDEX events_sequence_organization_idx ON events (organization_id, sequence) WHERE project_id IS NULL;
COMMENT ON INDEX events_sequence_organization_idx IS 'Replay em ordem do fluxo da organização (eventos de skill) e conferência de lacuna e repetição pelo gatilho.';
CREATE INDEX events_global_position_idx ON events (global_position) WHERE global_position IS NOT NULL;
COMMENT ON INDEX events_global_position_idx IS 'Stream /eu/eventos: retomada por global_position > Last-Event-ID (ADR-11); serve o maior valor ao publicar. Sem duplicata pela trava do publicador e pelo gatilho events_immutable (posição crescente).';
CREATE INDEX events_public_id_idx ON events (public_id);
COMMENT ON INDEX events_public_id_idx IS 'Busca do evento pelo id do CloudEvent sem a data (causationid, reentrega).';
CREATE INDEX events_pending_idx ON events (organization_id, project_id, sequence) WHERE published_at IS NULL;
COMMENT ON INDEX events_pending_idx IS 'Publicador do outbox: pendentes em ordem de sequencia em cada fluxo (projeto ou organização); a posição global segue a ordem de publicação.';
CREATE INDEX events_wave_idx ON events (project_id, waveid, sequence) WHERE waveid IS NOT NULL;
COMMENT ON INDEX events_wave_idx IS 'Histórico completo de eventos da onda (US-09/CA-4) e replay filtrado por onda.';

CREATE SEQUENCE events_global_position_seq AS bigint START 1 NO CYCLE;
COMMENT ON SEQUENCE events_global_position_seq IS 'Fonte da global_position; usada só pelo publicador do outbox, sob trava consultiva (ADR-11).';

CREATE FUNCTION events_sequence_no_gap() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF EXISTS (
       SELECT 1 FROM events e
        WHERE e.sequence = NEW.sequence
          AND CASE WHEN NEW.project_id IS NULL THEN e.project_id IS NULL AND e.organization_id = NEW.organization_id
                   ELSE e.project_id = NEW.project_id END) THEN
    RAISE EXCEPTION 'sequencia repetida no fluxo de eventos' USING ERRCODE = '23505', CONSTRAINT = 'events_sequence_no_gap';
  END IF;
  IF NEW.sequence > 1 AND NOT EXISTS (
       SELECT 1 FROM events e
        WHERE e.sequence = NEW.sequence - 1
          AND CASE WHEN NEW.project_id IS NULL THEN e.project_id IS NULL AND e.organization_id = NEW.organization_id
                   ELSE e.project_id = NEW.project_id END) THEN
    RAISE EXCEPTION 'lacuna na sequencia de eventos do fluxo' USING ERRCODE = '23514', CONSTRAINT = 'events_sequence_no_gap';
  END IF;
  IF NEW.project_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM projects p WHERE p.id = NEW.project_id AND p.organization_id = NEW.organization_id) THEN
    RAISE EXCEPTION 'projeto de outra organização' USING ERRCODE = '23514', CONSTRAINT = 'events_sequence_no_gap';
  END IF;
  IF NEW.project_id IS NOT NULL AND (NEW.envelope ->> 'projectid') IS DISTINCT FROM (SELECT p.public_id::text FROM projects p WHERE p.id = NEW.project_id) THEN
    RAISE EXCEPTION 'projetoid do envelope não é o id público do projeto' USING ERRCODE = '23514', CONSTRAINT = 'events_envelope_ck';
  END IF;
  IF NEW.published_at IS NOT NULL OR NEW.global_position IS NOT NULL THEN
    RAISE EXCEPTION 'evento nasce pendente: posição global só na publicação' USING ERRCODE = '23514', CONSTRAINT = 'events_sequence_no_gap';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER events_sequence_no_gap BEFORE INSERT ON events
  FOR EACH ROW EXECUTE FUNCTION events_sequence_no_gap();
COMMENT ON TRIGGER events_sequence_no_gap ON events IS 'regra: US-09/CA-3 — sequencia única e sem lacuna no fluxo do projeto ou da organização (garantia 3 do catálogo de eventos; a tabela particionada não tem índice único sem a data); projetoid do envelope = id público do projeto; evento nasce não publicado.';

CREATE FUNCTION events_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF OLD.published_at IS NULL AND NEW.published_at IS NOT NULL AND NEW.global_position IS NOT NULL
     AND (to_jsonb(NEW) - 'published_at' - 'global_position') = (to_jsonb(OLD) - 'published_at' - 'global_position') THEN
    IF NEW.global_position <= coalesce((SELECT max(global_position) FROM events WHERE global_position IS NOT NULL), 0) THEN
      RAISE EXCEPTION 'posição global fora de ordem de publicação' USING ERRCODE = '23514', CONSTRAINT = 'events_immutable';
    END IF;
    RETURN NEW;
  END IF;
  RAISE EXCEPTION 'evento é imutável (só published_at e global_position, uma vez)' USING ERRCODE = '23514', CONSTRAINT = 'events_immutable';
END $$;
CREATE TRIGGER events_immutable BEFORE UPDATE ON events
  FOR EACH ROW EXECUTE FUNCTION events_immutable();
COMMENT ON TRIGGER events_immutable ON events IS 'Fato não se reescreve; só a marca de publicação com a posição global, uma vez e crescente (ADR-11).';

CREATE FUNCTION events_never_delete() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION 'evento de domínio não se apaga (trilha de auditoria, D-32)'
    USING ERRCODE = '42501', CONSTRAINT = 'events_no_delete';
END $$;
CREATE TRIGGER events_no_delete BEFORE DELETE ON events
  FOR EACH ROW EXECUTE FUNCTION events_never_delete();
COMMENT ON TRIGGER events_no_delete ON events IS 'Evento de domínio não se apaga, nem pelo papel de retenção (histórico da onda, auditoria; D-32).';

CREATE TABLE audit_records (
  id              bigint        GENERATED ALWAYS AS IDENTITY,
  public_id      uuid          NOT NULL DEFAULT gen_random_uuid(),
  organization_id  bigint        NOT NULL,
  project_id      bigint        NULL,
  recorded_at   timestamptz   NOT NULL DEFAULT now(),
  actor_type       text          NOT NULL,
  actor_id         text          NOT NULL,
  origin          text          NULL,
  policy        text          NULL,
  action            text          NOT NULL,
  target            text          NOT NULL,
  decision         text          NULL,
  justification   text          NULL,
  reference_list     jsonb         NOT NULL DEFAULT '[]',
  lost_cost   numeric(18,8) NULL,
  event_id       bigint        NULL,
  correlationid   uuid          NULL,
  external_event_id text        NULL,
  CONSTRAINT audit_records_pkey PRIMARY KEY (id, recorded_at),
  CONSTRAINT audit_records_public_id_uk UNIQUE (public_id, recorded_at),
  CONSTRAINT audit_records_external_event_uk UNIQUE (external_event_id, recorded_at),
  CONSTRAINT audit_records_organization_fk FOREIGN KEY (organization_id) REFERENCES organizations (id) ON DELETE RESTRICT,
  CONSTRAINT audit_records_project_fk FOREIGN KEY (project_id) REFERENCES projects (id) ON DELETE RESTRICT,
  CONSTRAINT audit_records_actor_type_ck CHECK (actor_type IN ('human','agent','orchestrator')),
  CONSTRAINT audit_records_actor_id_ck CHECK (char_length(actor_id) BETWEEN 1 AND 128),
  CONSTRAINT audit_records_origin_ck CHECK ((actor_type = 'human') = (origin IS NOT NULL) AND (origin IS NULL OR origin IN ('web','local_client'))),
  CONSTRAINT audit_records_policy_ck CHECK (policy IS NULL OR (actor_type = 'orchestrator' AND policy ~ '^[a-z][a-z0-9_.]*$')),
  CONSTRAINT audit_records_action_ck CHECK (action ~ '^[a-z][a-z0-9_]*\.[a-z][a-z0-9_]*$'),
  CONSTRAINT audit_records_target_ck CHECK (btrim(target) <> ''),
  CONSTRAINT audit_records_reference_list_ck CHECK (jsonb_typeof(reference_list) = 'array'),
  CONSTRAINT audit_records_cost_ck CHECK (lost_cost IS NULL OR lost_cost >= 0)
) PARTITION BY RANGE (recorded_at);
COMMENT ON TABLE audit_records IS 'Trilha de auditoria só de inclusão (RNF-13): decisões, aprovações, ações de controle, atribuições de papel, exclusões e decisões por política (policy.decision_record). Fica após a anonimização do projeto, com o ator por id opaco.';
COMMENT ON COLUMN audit_records.id IS 'Identificador interno (chave primária inteira, alvo das FKs); não sai do banco: API e eventos usam public_id.';
COMMENT ON COLUMN audit_records.public_id IS 'Identificador opaco exposto (RegistroAuditoria.id da API).';
COMMENT ON COLUMN audit_records.organization_id IS 'Organização (trilha da organização; US-04/CA-3).';
COMMENT ON COLUMN audit_records.project_id IS 'Projeto; nulo em ação da organização (chave, preço, skill).';
COMMENT ON COLUMN audit_records.recorded_at IS 'Quando (US-35/CA-1); chave de partição. Nas ações lidas do Keycloak (external_event_id), é o instante do evento externo.';
COMMENT ON COLUMN audit_records.actor_type IS 'human, agent ou orchestrator.';
COMMENT ON COLUMN audit_records.actor_id IS '[DP] Quem: sub opaco do Keycloak (humano), id do agente ou orquestrador; nunca nome ou e-mail.';
COMMENT ON COLUMN audit_records.origin IS 'De onde (web ou local_client) na ação humana; local_client só nas ações de sessão (sessao.login, sessao.logout, sessao_cliente.revogar), porque o cliente local é somente leitura (PD-07).';
COMMENT ON COLUMN audit_records.policy IS 'Política que motivou a ação do orquestrador.';
COMMENT ON COLUMN audit_records.action IS 'Ação (gate.aprovar, onda.pause_now, membro.atribuir, projeto.excluir, artefato.editar, restricoes.alterar, projeto.acessar_como_administrador, sessao.login…); filtro por tipo (US-35/CA-2).';
COMMENT ON COLUMN audit_records.target IS 'Alvo (subject ou id): projeto, ondas/DBA-01, gates/gate.data/rodadas/2, achados/ACH-03.';
COMMENT ON COLUMN audit_records.decision IS 'Decisão tomada, quando a ação é decisão.';
COMMENT ON COLUMN audit_records.justification IS '[DP] Motivo ou justificativa (texto livre humano); anonimizado com o projeto.';
COMMENT ON COLUMN audit_records.reference_list IS 'Ids de artefatos, achados e ondas citados.';
COMMENT ON COLUMN audit_records.lost_cost IS 'Custo perdido da ação de controle.';
COMMENT ON COLUMN audit_records.event_id IS 'Evento de domínio correspondente (events.id). Sem FK: eventos é particionada e nenhuma FK aponta para tabela particionada (ADR-18); a api e o worker gravam os dois na mesma transação.';
COMMENT ON COLUMN audit_records.correlationid IS 'Cadeia de causalidade (liga ao trace da onda).';
COMMENT ON COLUMN audit_records.external_event_id IS 'Id do evento do Keycloak nas ações sessao.login, sessao.logout e sessao.login_falhou (PL-36): repetir a leitura não duplica.';
COMMENT ON CONSTRAINT audit_records_public_id_uk ON audit_records IS 'Id público único com a chave de partição (exigência do particionamento; o uuid aleatório torna repetição impraticável).';
COMMENT ON CONSTRAINT audit_records_external_event_uk ON audit_records IS 'Um registro por evento externo. Inclui a chave de partição: nas ações de sessão, recorded_at é o instante do evento do Keycloak, então reler o mesmo evento repete o par e é recusado.';
COMMENT ON CONSTRAINT audit_records_actor_type_ck ON audit_records IS 'Tipos de ator do payload comum.';
COMMENT ON CONSTRAINT audit_records_actor_id_ck ON audit_records IS 'Tamanho de ator.id.';
COMMENT ON CONSTRAINT audit_records_origin_ck ON audit_records IS 'regra: US-12/RN-1 — ação humana registra de onde.';
COMMENT ON CONSTRAINT audit_records_policy_ck ON audit_records IS 'Política só em ação do orchestrator.';
COMMENT ON CONSTRAINT audit_records_action_ck ON audit_records IS 'Ação no formato entidade.acao.';
COMMENT ON CONSTRAINT audit_records_target_ck ON audit_records IS 'Alvo não vazio.';
COMMENT ON CONSTRAINT audit_records_reference_list_ck ON audit_records IS 'Referências em lista.';
COMMENT ON CONSTRAINT audit_records_cost_ck ON audit_records IS 'Custo não negativo.';
CREATE INDEX audit_records_org_idx ON audit_records (organization_id, recorded_at);
COMMENT ON INDEX audit_records_org_idx IS 'Trilha da organização em ordem e por período (US-35/CA-1, CA-2; US-04/CA-3) e expurgo por retenção.';
CREATE INDEX audit_records_action_idx ON audit_records (organization_id, action, recorded_at);
COMMENT ON INDEX audit_records_action_idx IS 'Filtro por tipo de ação e período (US-35/CA-2).';
CREATE INDEX audit_records_project_idx ON audit_records (project_id, recorded_at) WHERE project_id IS NOT NULL;
COMMENT ON INDEX audit_records_project_idx IS 'Trilha do projeto e anonimização dos textos do projeto; cobre a FK.';

CREATE FUNCTION audit_records_insert_only() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE v_retention interval;
BEGIN
  IF NOT pg_has_role(current_user, 'easycode_retention', 'MEMBER') THEN
    RAISE EXCEPTION 'trilha de auditoria só aceita inclusão' USING ERRCODE = '42501', CONSTRAINT = 'audit_records_insert_only';
  END IF;
  IF TG_OP = 'UPDATE' THEN
    -- anonimização: só a justificativa muda
    IF (to_jsonb(NEW) - 'justification') <> (to_jsonb(OLD) - 'justification') THEN
      RAISE EXCEPTION 'anonimização só altera a justificativa' USING ERRCODE = '42501', CONSTRAINT = 'audit_records_insert_only';
    END IF;
    RETURN NEW;
  END IF;
  SELECT audit_period INTO v_retention FROM organizations WHERE id = OLD.organization_id;
  IF OLD.recorded_at > now() - v_retention THEN
    RAISE EXCEPTION 'registro dentro do prazo de retenção' USING ERRCODE = '42501', CONSTRAINT = 'audit_records_insert_only';
  END IF;
  RETURN OLD;
END $$;
CREATE TRIGGER audit_records_insert_only BEFORE UPDATE OR DELETE ON audit_records
  FOR EACH ROW EXECUTE FUNCTION audit_records_insert_only();
COMMENT ON TRIGGER audit_records_insert_only ON audit_records IS 'regra: US-35/CA-3 — trilha não se altera nem apaga; só a retenção expira registros vencidos (audit_period, D-30) e anonimiza a justificativa.';

-- =============================================================================
-- 11a. Partições mensais das tabelas de alto volume (decisão do dono de 2026-11-21, ADR-18)
-- =============================================================================
-- eventos, audit_records, model_calls, tool_executions e wave_checkpoints são particionadas por
-- RANGE na data, uma partição por mês civil em UTC (<tabela>_pAAAAMM), mais a partição padrão (<tabela>_default),
-- que recebe a linha de um mês ainda sem partição e deve ficar vazia. A chave primária inclui a chave de partição
-- e nenhuma FK aponta para tabela particionada. A função create_monthly_partitions cria os meses que faltam; o worker
-- a chama na partida e uma vez por dia (job agendado), sempre com folga de meses à frente.
CREATE TABLE events_default PARTITION OF events DEFAULT;
CREATE TABLE audit_records_default PARTITION OF audit_records DEFAULT;
CREATE TABLE model_calls_default PARTITION OF model_calls DEFAULT;
CREATE TABLE tool_executions_default PARTITION OF tool_executions DEFAULT;
CREATE TABLE wave_checkpoints_default PARTITION OF wave_checkpoints DEFAULT;
COMMENT ON TABLE events_default IS 'Partição padrão de eventos: linha fora das partições mensais; deve ficar vazia (PERSIST seção 4.1).';
COMMENT ON TABLE audit_records_default IS 'Partição padrão de audit_records: linha fora das partições mensais; deve ficar vazia (PERSIST seção 4.1).';
COMMENT ON TABLE model_calls_default IS 'Partição padrão de model_calls: linha fora das partições mensais; deve ficar vazia (PERSIST seção 4.1).';
COMMENT ON TABLE tool_executions_default IS 'Partição padrão de tool_executions: linha fora das partições mensais; deve ficar vazia (PERSIST seção 4.1).';
COMMENT ON TABLE wave_checkpoints_default IS 'Partição padrão de wave_checkpoints: linha fora das partições mensais; deve ficar vazia (PERSIST seção 4.1).';

-- Cria, para cada tabela particionada, as partições mensais de date_trunc('month', p_since) até o mês atual +
-- p_months_ahead (limites em UTC). Idempotente: mês que já tem partição é pulado. Se a partição padrão já tiver
-- linhas no intervalo de um mês novo, recusa (SQLSTATE 55000) em vez de deixar o PostgreSQL varrer e falhar no
-- meio: a correção é manual, pelo papel de migração (PERSIST seção 4.1). Devolve quantas partições criou.
-- SECURITY DEFINER: roda com o dono do esquema (easycode_migration), o único com DDL; o worker só tem EXECUTE.
CREATE FUNCTION create_monthly_partitions(p_months_ahead integer DEFAULT 3, p_since date DEFAULT current_date)
RETURNS integer LANGUAGE plpgsql SECURITY DEFINER
SET search_path = easycode, pg_catalog
SET lock_timeout = '5s'
AS $$
DECLARE
  v_tables  text[][] := ARRAY[['events','occurred_at'], ['audit_records','recorded_at'],
                                ['model_calls','started_at'], ['tool_executions','started_at'],
                                ['wave_checkpoints','wave_created_at']];
  v_month      date;
  v_last   date;
  v_start      timestamptz;
  v_end      timestamptz;
  v_name     text;
  v_created  integer := 0;
  v_has      boolean;
  i          integer;
BEGIN
  IF p_months_ahead IS NULL OR p_months_ahead < 0 OR p_months_ahead > 24 THEN
    RAISE EXCEPTION 'p_months_ahead fora de 0..24: %', p_months_ahead USING ERRCODE = '22023';
  END IF;
  -- uma execução por vez (worker na partida e job diário podem coincidir)
  PERFORM pg_advisory_xact_lock(hashtext('easycode.create_monthly_partitions'));
  v_last := (date_trunc('month', (now() AT TIME ZONE 'UTC')) + make_interval(months => p_months_ahead))::date;
  FOR i IN 1 .. array_length(v_tables, 1) LOOP
    v_month := date_trunc('month', p_since)::date;
    WHILE v_month <= v_last LOOP
      v_name := v_tables[i][1] || '_p' || to_char(v_month, 'YYYYMM');
      IF to_regclass(v_name) IS NULL THEN
        v_start := v_month::timestamp AT TIME ZONE 'UTC';
        v_end := (v_month + interval '1 month')::timestamp AT TIME ZONE 'UTC';
        -- lido pela tabela-mãe: sem partição do mês, linha do intervalo só pode estar na padrão anexada
        -- (destacada para a correção manual, ela não conta)
        EXECUTE format('SELECT EXISTS (SELECT 1 FROM %I WHERE %I >= $1 AND %I < $2)',
                       v_tables[i][1], v_tables[i][2], v_tables[i][2])
          INTO v_has USING v_start, v_end;
        IF v_has THEN
          RAISE EXCEPTION 'partição padrão % tem linhas de %; mover antes de criar %',
                          v_tables[i][1] || '_default', to_char(v_month, 'YYYY-MM'), v_name
            USING ERRCODE = '55000', HINT = 'PERSIST seção 4.1: correção manual pelo papel de migração.';
        END IF;
        EXECUTE format('CREATE TABLE %I PARTITION OF %I FOR VALUES FROM (%L) TO (%L)',
                       v_name, v_tables[i][1], v_start, v_end);
        EXECUTE format('COMMENT ON TABLE %I IS %L', v_name,
                       'Partição mensal de ' || v_tables[i][1] || ' (' || to_char(v_month, 'YYYY-MM') || ', UTC), criada por create_monthly_partitions.');
        EXECUTE format('GRANT SELECT ON %I TO easycode_backup', v_name);
        v_created := v_created + 1;
      END IF;
      v_month := (v_month + interval '1 month')::date;
    END LOOP;
  END LOOP;
  RETURN v_created;
END $$;
COMMENT ON FUNCTION create_monthly_partitions(integer, date) IS 'Cria as partições mensais (UTC) que faltam nas cinco tabelas particionadas, do mês de p_since até o mês atual + p_months_ahead (padrão 3); idempotente; recusa se a partição padrão tiver linhas do mês novo. Chamada pelo worker na partida e por job diário (ADR-18).';
REVOKE ALL ON FUNCTION create_monthly_partitions(integer, date) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION create_monthly_partitions(integer, date) TO easycode_worker;

-- Partições iniciais: mês atual e os 3 seguintes. A partir daí, o worker mantém a folga.
DO $$ BEGIN PERFORM create_monthly_partitions(3); END $$;

-- =============================================================================
-- 12. Privilégios (privilégio mínimo; nenhum papel de aplicação tem DDL)
-- =============================================================================
REVOKE ALL ON SCHEMA easycode FROM PUBLIC;
GRANT USAGE, CREATE ON SCHEMA easycode TO easycode_migration;
GRANT USAGE ON SCHEMA easycode TO easycode_api, easycode_worker, easycode_retention, easycode_backup;

-- Leitura geral (provider_keys tratado à parte, por coluna).
GRANT SELECT ON organizations, users, projects, project_constraints, project_members, project_attachments, project_counters, organization_counters, workflow_configurations,
  gate_configurations, policy_parameters, stages, models, model_prices, model_pairs, agent_configurations,
  skills, skill_versions, skill_imports, project_skills, artifacts, findings, finding_affected_artifacts, waves, wave_models,
  wave_skills, wave_inputs, wave_checkpoints, model_calls, tool_executions, wave_questions, question_items,
  commands, idempotency_keys, artifact_versions, artifact_links, gate_rounds, gate_round_artifacts, check_executions, gate_confirmations,
  finding_cycles, finding_escalations, events, audit_records, state_transitions
  TO easycode_api, easycode_worker, easycode_retention;

-- API: ações humanas e configuração. Decisões humanas de gate e de achado e transições humanas de skill são da API,
-- síncronas e com o outbox na mesma transação (A1). Controle de onda, transições de projeto (start, pause, resume,
-- cancelar, record_publication) e teste de chave vão pela fila comandos ao worker: a API não muda projects.state.
GRANT INSERT, UPDATE ON users, gate_configurations, policy_parameters, model_pairs, project_skills TO easycode_api;
GRANT INSERT ON projects TO easycode_api;
GRANT UPDATE (name, business_need, artifact_language, budget_stop_limit, budget_alert_limits,
  deletion_requested_by, deletion_requested_at, row_version) ON projects TO easycode_api;
GRANT INSERT, DELETE ON project_members TO easycode_api;
GRANT INSERT ON model_prices, models, skills, skill_imports, agent_configurations, commands, gate_confirmations, audit_records, events,
  project_attachments, project_constraints TO easycode_api;
-- correção de vigência futura ainda não usada (corrigirPrecoModelo; o gatilho model_prices_immutable decide)
GRANT UPDATE (effective_since, input_price, output_price, read_cache_price, write_cache_price, source, fixed_at) ON model_prices TO easycode_api;
-- idempotência (ADR-17): grava e, vencida, reusa a chave
GRANT INSERT, UPDATE ON idempotency_keys TO easycode_api;
-- decisões humanas de gate e achado (approve, reject, aceitar e renovar risco, descartar, decidir escalonamento)
GRANT UPDATE ON gate_rounds, findings, project_counters, organization_counters TO easycode_api;
GRANT INSERT ON project_counters, organization_counters TO easycode_api;
-- máquina skill, transições humanas: liberar (bloqueada → liberada) e desativar
GRANT UPDATE (status, unblocked_by, unblocked_at, unblock_justification, deactivated_by, deactivated_at, deactivation_justification) ON skill_versions TO easycode_api;
GRANT UPDATE (answered_by, answered_at, origin) ON wave_questions TO easycode_api;
GRANT UPDATE (answer) ON question_items TO easycode_api;
GRANT UPDATE (confirmed_by, confirmed_at, result) ON check_executions TO easycode_api;
GRANT UPDATE (decision, authorized_by, decided_at, modified_by, starting_point_artifacts) ON finding_escalations TO easycode_api;
-- configuração da organização pela web: só o teto do limite de parada (PL-07, opção c)
GRANT UPDATE (stop_limit_ceiling) ON organizations TO easycode_api;
-- só para a trava da linha da etapa (SELECT … FOR UPDATE) que a aprovação do gate faz em gate_rounds_valid_approval;
-- o estado da etapa continua só do worker.
GRANT UPDATE (updated_at) ON stages TO easycode_api;
GRANT SELECT (id, organization_id, provider, state, encryption_key_ref, last4, test_result, test_expires_at, tested_at, created_by, created_at, removed_at)
  ON provider_keys TO easycode_api;
-- grava a chave selada em testing (com o comando test_provider_key) e remove a ativa (ativa → removida, valor apagado)
GRANT INSERT ON provider_keys TO easycode_api;
GRANT UPDATE (state, removed_at, encrypted_value) ON provider_keys TO easycode_api;

-- Worker: orquestrador, motor de governança, harness, importação e varredura de skill, consumidor da fila comandos
-- e publicador do outbox (published_at e global_position). O valor cifrado da chave só tem uso no easycode-gateway,
-- único processo com a chave privada (S-03); o worker o lê para repassar e grava o resultado do teste.
GRANT INSERT, UPDATE ON project_counters, organization_counters, workflow_configurations, stages, artifacts, findings, waves, wave_checkpoints,
  model_calls, tool_executions, wave_questions, artifact_versions, gate_rounds, check_executions,
  finding_cycles, finding_escalations, events, projects TO easycode_worker;
GRANT INSERT ON finding_affected_artifacts, wave_models, wave_skills, wave_inputs, gate_round_artifacts,
  audit_records, agent_configurations, skills, skill_versions, question_items TO easycode_worker;
GRANT UPDATE (status, processed_at, refusal_reason, lost_cost) ON commands TO easycode_worker;
GRANT UPDATE (state, encrypted_value, test_result, tested_at) ON provider_keys TO easycode_worker;
GRANT UPDATE (status, block_reasons, scan_report, size_tokens) ON skill_versions TO easycode_worker;
GRANT UPDATE (state, refusal, skill_version_id, package, completed_at) ON skill_imports TO easycode_worker;
GRANT UPDATE (discarded_at) ON gate_confirmations TO easycode_worker;
GRANT INSERT, DELETE ON artifact_links TO easycode_worker;
GRANT SELECT ON provider_keys TO easycode_worker;
GRANT USAGE ON SEQUENCE events_global_position_seq TO easycode_worker;

-- Retenção: expiração de conteúdo (expire_content), anonimização, expiração da auditoria e limpeza das chaves de
-- idempotência vencidas. Sem DELETE em eventos (o outbox nunca é apagado, D-32) e sem UPDATE em eventos (payload só com ids opacos).
GRANT UPDATE ON users, projects, question_items, gate_rounds, gate_confirmations, findings, wave_checkpoints,
  model_calls, artifact_versions, skill_versions, audit_records TO easycode_retention;
GRANT UPDATE (content_purged_at) ON project_attachments TO easycode_retention;
GRANT DELETE ON audit_records, idempotency_keys TO easycode_retention;

-- Cópia de segurança lógica: leitura de tudo, inclusive o valor cifrado (inútil sem a chave privada, que fica fora do banco).
GRANT SELECT ON ALL TABLES IN SCHEMA easycode TO easycode_backup;
GRANT SELECT ON ALL SEQUENCES IN SCHEMA easycode TO easycode_backup;
