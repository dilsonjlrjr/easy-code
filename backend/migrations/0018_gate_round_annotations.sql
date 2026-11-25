-- 0018 — anotações do protótipo na reprovação do gate.design (MARCO-10, US-57, PD-25 opção a).
-- O aprovador marca pinos com comentário sobre as telas do protótipo; eles vão junto com a reprovação,
-- ficam no registro da rodada (só inserção) e entram como entrada da onda de retrabalho do Designer. A
-- anotação nunca muda o TELAS (US-57 RN-4): guarda só a versão que a rodada decidiu.
SET search_path TO easycode, public;

CREATE TABLE gate_round_annotations (
  id                bigint           GENERATED ALWAYS AS IDENTITY,
  round_id          bigint           NOT NULL,
  number            smallint         NOT NULL,
  artifact_code     text             NOT NULL,
  artifact_version  integer          NOT NULL,
  screen            text             NOT NULL,
  screen_state      text             NULL,
  width             text             NOT NULL,
  x                 double precision NULL,
  y                 double precision NULL,
  comment           text             NOT NULL,
  created_by        bigint           NOT NULL,
  created_at        timestamptz      NOT NULL DEFAULT now(),
  CONSTRAINT gate_round_annotations_pkey PRIMARY KEY (id),
  CONSTRAINT gate_round_annotations_number_uk UNIQUE (round_id, number),
  CONSTRAINT gate_round_annotations_round_fk FOREIGN KEY (round_id) REFERENCES gate_rounds (id) ON DELETE RESTRICT,
  CONSTRAINT gate_round_annotations_created_by_fk FOREIGN KEY (created_by) REFERENCES users (id) ON DELETE RESTRICT,
  CONSTRAINT gate_round_annotations_number_ck CHECK (number BETWEEN 1 AND 50),
  CONSTRAINT gate_round_annotations_artifact_ck CHECK (artifact_code = 'TELAS' AND artifact_version >= 1),
  CONSTRAINT gate_round_annotations_screen_ck CHECK (screen ~ '^TL-[0-9]+$' AND (screen_state IS NULL OR screen_state ~ '^[a-z][a-z0-9_]*$')),
  CONSTRAINT gate_round_annotations_width_ck CHECK (width IN ('wide','narrow')),
  CONSTRAINT gate_round_annotations_point_ck CHECK ((x IS NULL) = (y IS NULL) AND (x IS NULL OR (x BETWEEN 0 AND 1 AND y BETWEEN 0 AND 1))),
  CONSTRAINT gate_round_annotations_comment_ck CHECK (char_length(btrim(comment)) BETWEEN 1 AND 2000)
);
COMMENT ON TABLE gate_round_annotations IS 'Anotações do protótipo gravadas com a reprovação humana do gate.design (US-57 CA-2); entram no prompt da onda de retrabalho do Designer como entrada não confiável (RN-2).';
COMMENT ON COLUMN gate_round_annotations.round_id IS 'Rodada do gate.design reprovada.';
COMMENT ON COLUMN gate_round_annotations.number IS 'Número do pino na rodada, a partir de 1.';
COMMENT ON COLUMN gate_round_annotations.artifact_code IS 'Artefato anotado (TELAS).';
COMMENT ON COLUMN gate_round_annotations.artifact_version IS 'Versão do TELAS que a rodada decidiu (US-57 CA-3).';
COMMENT ON COLUMN gate_round_annotations.screen IS 'Tela do protótipo (TL-n).';
COMMENT ON COLUMN gate_round_annotations.screen_state IS 'Estado exibido (?estado=); nulo no estado padrão.';
COMMENT ON COLUMN gate_round_annotations.width IS 'Largura do visualizador: wide ou narrow.';
COMMENT ON COLUMN gate_round_annotations.x IS 'Posição horizontal relativa ao quadro (0 a 1); nula na anotação sem ponto (US-57 CA-5).';
COMMENT ON COLUMN gate_round_annotations.y IS 'Posição vertical relativa ao quadro (0 a 1); nula na anotação sem ponto.';
COMMENT ON COLUMN gate_round_annotations.comment IS '[DP] Comentário do aprovador (texto livre humano); anonimizado com o projeto.';
COMMENT ON COLUMN gate_round_annotations.created_by IS 'Aprovador que reprovou a rodada.';
CREATE INDEX gate_round_annotations_created_by_idx ON gate_round_annotations (created_by);
COMMENT ON INDEX gate_round_annotations_created_by_idx IS 'Cobre a FK de created_by.';

GRANT SELECT, INSERT ON gate_round_annotations TO easycode_api;
GRANT SELECT ON gate_round_annotations TO easycode_worker;
GRANT SELECT ON gate_round_annotations TO easycode_retention, easycode_backup;
GRANT UPDATE (comment) ON gate_round_annotations TO easycode_retention;
