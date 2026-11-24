-- 0017 — trava da instalação (MARCO-9, US-55, ADR-22). Uma linha só, gravada pelo easycode-installer no último
-- passo; com ela presente, o assistente só responde "já instalado". Nenhum segredo fica aqui.
SET search_path TO easycode, public;

CREATE TABLE platform_installation (
  id                 smallint    NOT NULL DEFAULT 1,
  installed_at       timestamptz NOT NULL DEFAULT now(),
  sso_mode           text        NOT NULL,
  realm              text        NOT NULL,
  administrator_sub  text        NOT NULL,
  installer_version  text        NOT NULL,
  CONSTRAINT platform_installation_pkey PRIMARY KEY (id),
  CONSTRAINT platform_installation_single_ck CHECK (id = 1),
  CONSTRAINT platform_installation_sso_mode_ck CHECK (sso_mode IN ('new_realm','existing_realm')),
  CONSTRAINT platform_installation_realm_ck CHECK (btrim(realm) <> ''),
  CONSTRAINT platform_installation_administrator_ck CHECK (char_length(administrator_sub) BETWEEN 1 AND 128)
);
COMMENT ON TABLE platform_installation IS 'Trava da instalação (US-55 RN-1, ADR-22): uma linha, gravada pelo assistente ao concluir.';
COMMENT ON COLUMN platform_installation.id IS 'Sempre 1: a plataforma tem uma instalação.';
COMMENT ON COLUMN platform_installation.installed_at IS 'Quando a instalação terminou.';
COMMENT ON COLUMN platform_installation.sso_mode IS 'Modo do Keycloak escolhido: realm novo ou realm existente.';
COMMENT ON COLUMN platform_installation.realm IS 'Nome do realm da plataforma.';
COMMENT ON COLUMN platform_installation.administrator_sub IS 'Subject (id do usuário no realm) do administrador definido na instalação.';
COMMENT ON COLUMN platform_installation.installer_version IS 'Versão do easycode-installer que instalou.';
COMMENT ON CONSTRAINT platform_installation_single_ck ON platform_installation IS 'Uma linha só.';

GRANT SELECT ON platform_installation TO easycode_api, easycode_worker, easycode_backup;
