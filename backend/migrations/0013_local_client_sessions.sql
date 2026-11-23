-- 0013 — sessões do cliente local (MARCO-8, US-45; ADR-12, ADR-20).
-- O app desktop (cliente OIDC público easycode-desktop, device flow, escopo easycode-read) chama a api com o
-- token de acesso do Keycloak como Bearer. A api guarda uma linha por sessão do SSO do app: dispositivo
-- (User-Agent do app), último uso e último IP para a pessoa reconhecer a sessão em "Minha conta › Cliente
-- local" (US-45 CA-5), a conferência da sessão no Keycloak a cada 60 s (RNF-06), o aviso de nova sessão visto
-- pela pessoa (S-19, RK-16) e o fim (logout pelo app, revogação na web, desligamento). Nenhum token é gravado.
SET search_path TO easycode, public;

CREATE TABLE local_client_sessions (
  id               bigint      GENERATED ALWAYS AS IDENTITY,
  public_id        uuid        NOT NULL DEFAULT gen_random_uuid(),
  user_id          bigint      NOT NULL,
  idp_session_id   text        NOT NULL,
  device           text        NOT NULL,
  ip_address       text        NULL,
  started_at       timestamptz NOT NULL DEFAULT now(),
  last_used_at     timestamptz NOT NULL DEFAULT now(),
  idp_checked_at   timestamptz NOT NULL DEFAULT now(),
  acknowledged_at  timestamptz NULL,
  ended_at         timestamptz NULL,
  end_reason       text        NULL,
  CONSTRAINT local_client_sessions_pkey PRIMARY KEY (id),
  CONSTRAINT local_client_sessions_public_uk UNIQUE (public_id),
  CONSTRAINT local_client_sessions_idp_uk UNIQUE (user_id, idp_session_id),
  CONSTRAINT local_client_sessions_user_fk FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE RESTRICT,
  CONSTRAINT local_client_sessions_idp_session_ck CHECK (char_length(idp_session_id) BETWEEN 1 AND 128),
  CONSTRAINT local_client_sessions_device_ck CHECK (char_length(device) BETWEEN 1 AND 120),
  CONSTRAINT local_client_sessions_ip_ck CHECK (ip_address IS NULL OR char_length(ip_address) BETWEEN 1 AND 64),
  CONSTRAINT local_client_sessions_times_ck CHECK (last_used_at >= started_at),
  CONSTRAINT local_client_sessions_end_ck CHECK ((ended_at IS NULL) = (end_reason IS NULL) AND (end_reason IS NULL OR end_reason IN ('logout','revoked','offboarded')))
);
COMMENT ON TABLE local_client_sessions IS 'Sessão do app desktop do cliente local (ADR-20, US-45): uma por sessão do Keycloak do app; somente leitura (PD-07). Nenhum token fica na base.';
COMMENT ON COLUMN local_client_sessions.id IS 'Identificador interno; não sai do banco.';
COMMENT ON COLUMN local_client_sessions.public_id IS 'Id público da sessão (listLocalClientSessions, revokeLocalClientSession).';
COMMENT ON COLUMN local_client_sessions.user_id IS 'Pessoa da sessão.';
COMMENT ON COLUMN local_client_sessions.idp_session_id IS 'Claim sid do token de acesso (sessão do Keycloak em que o app entrou).';
COMMENT ON COLUMN local_client_sessions.device IS 'Dispositivo: User-Agent do app (easycode-desktop/<versão> (<so>; <arquitetura>)), só caracteres imprimíveis.';
COMMENT ON COLUMN local_client_sessions.ip_address IS 'Último IP visto, para a pessoa reconhecer a sessão (dado pessoal; anonimizado no desligamento).';
COMMENT ON COLUMN local_client_sessions.started_at IS 'Primeira chamada da sessão à api.';
COMMENT ON COLUMN local_client_sessions.last_used_at IS 'Última chamada autenticada (gravada no máximo a cada 30 s).';
COMMENT ON COLUMN local_client_sessions.idp_checked_at IS 'Última conferência da sessão no Keycloak; a api confere de novo depois de 60 s (RNF-06).';
COMMENT ON COLUMN local_client_sessions.acknowledged_at IS 'Quando a pessoa viu o aviso de nova sessão na web (S-19); nulo = aviso pendente.';
COMMENT ON COLUMN local_client_sessions.ended_at IS 'Fim da sessão; nulo enquanto vale.';
COMMENT ON COLUMN local_client_sessions.end_reason IS 'logout (endSession pelo app), revoked (revogada na web ou sessão do SSO encerrada) ou offboarded (pessoa desativada).';
COMMENT ON CONSTRAINT local_client_sessions_idp_uk ON local_client_sessions IS 'Uma linha por sessão do SSO da pessoa: sessão revogada continua recusada até o SSO abrir outra (US-45 CA-2).';
COMMENT ON CONSTRAINT local_client_sessions_end_ck ON local_client_sessions IS 'Sessão encerrada tem motivo, e só os motivos conhecidos.';
CREATE INDEX local_client_sessions_user_idx ON local_client_sessions (user_id) WHERE ended_at IS NULL;
COMMENT ON INDEX local_client_sessions_user_idx IS 'Sessões abertas de uma pessoa (Minha conta › Cliente local; encerrar ao desligar); cobre a FK.';

GRANT SELECT, INSERT ON local_client_sessions TO easycode_api;
GRANT UPDATE (last_used_at, ip_address, idp_checked_at, acknowledged_at, ended_at, end_reason) ON local_client_sessions TO easycode_api;
GRANT SELECT ON local_client_sessions TO easycode_worker;
GRANT UPDATE (ip_address, ended_at, end_reason) ON local_client_sessions TO easycode_worker;
GRANT SELECT ON local_client_sessions TO easycode_backup;
