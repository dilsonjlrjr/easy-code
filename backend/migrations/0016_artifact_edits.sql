-- 0016 — edição humana de Artefato validada pelo agente da etapa (MARCO-8, US-53, PD-03).
-- A api grava o pedido de edição (texto completo do arquivo, operação e motivo) e o comando edit_artifact; o
-- worker confere a guarda (estados.yaml, transição edit), invalida a rodada de gate aberta e cria a onda
-- edit_review do agente da etapa, que valida e formata a edição: só a versão que essa onda grava vale (autor
-- humano em author_user_id, agente em reviewed_by). Edição recusada pelo agente não vira versão (US-53 CA-2).
SET search_path TO easycode, public;

CREATE TABLE artifact_edits (
  id             bigint      GENERATED ALWAYS AS IDENTITY,
  public_id      uuid        NOT NULL DEFAULT gen_random_uuid(),
  project_id     bigint      NOT NULL,
  command_id     bigint      NOT NULL,
  wave_id        bigint      NULL,
  operation      text        NOT NULL,
  artifact_type  text        NOT NULL,
  artifact_code  text        NULL,
  base_version   integer     NULL,
  content        text        NULL,
  reason         text        NOT NULL,
  requested_by   bigint      NOT NULL,
  created_at     timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT artifact_edits_pkey PRIMARY KEY (id),
  CONSTRAINT artifact_edits_public_uk UNIQUE (public_id),
  CONSTRAINT artifact_edits_command_uk UNIQUE (command_id),
  CONSTRAINT artifact_edits_wave_uk UNIQUE (wave_id),
  CONSTRAINT artifact_edits_project_fk FOREIGN KEY (project_id) REFERENCES projects (id) ON DELETE RESTRICT,
  CONSTRAINT artifact_edits_command_fk FOREIGN KEY (command_id) REFERENCES commands (id) ON DELETE RESTRICT,
  CONSTRAINT artifact_edits_wave_fk FOREIGN KEY (wave_id) REFERENCES waves (id) ON DELETE RESTRICT,
  CONSTRAINT artifact_edits_requested_by_fk FOREIGN KEY (requested_by) REFERENCES users (id) ON DELETE RESTRICT,
  CONSTRAINT artifact_edits_operation_ck CHECK (operation IN ('create','modify','delete')
                                                AND (operation = 'create') = (artifact_code IS NULL)
                                                AND (operation = 'create') = (base_version IS NULL)
                                                AND (operation = 'delete' OR content IS NOT NULL)),
  CONSTRAINT artifact_edits_type_ck CHECK (artifact_type IN ('VISAO','US','RNF','BACKLOG','ADR')),
  CONSTRAINT artifact_edits_content_ck CHECK (content IS NULL OR char_length(content) <= 204800),
  CONSTRAINT artifact_edits_reason_ck CHECK (char_length(btrim(reason)) BETWEEN 3 AND 2000)
);
COMMENT ON TABLE artifact_edits IS 'Pedido de edição humana de Artefato (US-53, PD-03): o texto fica aqui e no prompt da onda edit_review, nunca em evento.';
COMMENT ON COLUMN artifact_edits.public_id IS 'Id público da edição (getArtifactEdit).';
COMMENT ON COLUMN artifact_edits.command_id IS 'Comando edit_artifact que o worker aplica.';
COMMENT ON COLUMN artifact_edits.wave_id IS 'Onda edit_review do agente da etapa; nula até o worker aplicar o comando.';
COMMENT ON COLUMN artifact_edits.operation IS 'create, modify ou delete (excluir item = nova versão sem o item, PL-41).';
COMMENT ON COLUMN artifact_edits.artifact_type IS 'Tipo com edição humana (x-human-edit-types).';
COMMENT ON COLUMN artifact_edits.artifact_code IS 'Artefato alterado; nulo na criação.';
COMMENT ON COLUMN artifact_edits.base_version IS 'Versão sobre a qual a pessoa editou; nula na criação.';
COMMENT ON COLUMN artifact_edits.content IS '[DP] Arquivo Markdown completo como a pessoa o deixou (texto livre humano); anonimizado com o projeto.';
COMMENT ON COLUMN artifact_edits.reason IS '[DP] Por que muda (texto livre humano); anonimizado com o projeto.';
COMMENT ON COLUMN artifact_edits.requested_by IS 'Pessoa que editou (editado_por da versão).';
CREATE INDEX artifact_edits_project_idx ON artifact_edits (project_id, created_at);
COMMENT ON INDEX artifact_edits_project_idx IS 'Edições do projeto e anonimização na exclusão; cobre a FK.';

GRANT SELECT, INSERT ON artifact_edits TO easycode_api;
GRANT SELECT ON artifact_edits TO easycode_worker;
GRANT UPDATE (wave_id) ON artifact_edits TO easycode_worker;
GRANT SELECT ON artifact_edits TO easycode_retention, easycode_backup;
GRANT UPDATE (content, reason) ON artifact_edits TO easycode_retention;
