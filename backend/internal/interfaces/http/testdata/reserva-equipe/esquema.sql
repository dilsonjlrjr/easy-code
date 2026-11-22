CREATE EXTENSION IF NOT EXISTS btree_gist;
CREATE TABLE reservas (
  id bigint GENERATED ALWAYS AS IDENTITY,
  sala text NOT NULL,
  periodo tstzrange NOT NULL,
  autor text NOT NULL,
  CONSTRAINT reservas_pkey PRIMARY KEY (id),
  CONSTRAINT reservas_sem_conflito EXCLUDE USING gist (sala WITH =, periodo WITH &&)
);
CREATE INDEX reservas_sala_periodo_idx ON reservas USING gist (sala, periodo);
