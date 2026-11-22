-- 0006 — sessão web no servidor e search_path fixo nas funções (MARCO-2).
-- (1) A ADR-12 (opção B) guarda a sessão web no PostgreSQL: o cookie ec_session leva só um valor opaco
--     aleatório; a tabela guarda o resumo SHA-256 dele, o token anti-CSRF, o instante da última autenticação
--     no SSO (auth_time, step-up da ADR-19) e o refresh token do Keycloak selado (AES-GCM com a chave de
--     sessão da api), usado para conferir a cada 60 s que a sessão do SSO segue válida (token revogado ou
--     conta desativada recusados em até 60 s, RNF-06) e para o logout de retaguarda.
-- (2) Nota do DBA do MARCO-0: as funções da 0001 dependiam do search_path da conexão. Agora cada função do
--     esquema easycode fixa search_path = easycode, pg_catalog (proteção contra objeto homônimo em outro
--     esquema, RNF-13: a trilha só de inclusão depende desses gatilhos).
-- (3) O worker desativa e anonimiza quem sai da organização (anonymize_offboarded_user, D-31, RNF-20) e
--     encerra as sessões dessa pessoa.
SET search_path TO easycode, public;

CREATE TABLE web_sessions (
  id                    bigint      GENERATED ALWAYS AS IDENTITY,
  token_hash            bytea       NOT NULL,
  user_id               bigint      NOT NULL,
  csrf_token            text        NOT NULL,
  idp_session_id        text        NULL,
  sealed_refresh_token  bytea       NULL,
  authenticated_at      timestamptz NOT NULL,
  issued_at             timestamptz NOT NULL DEFAULT now(),
  last_seen_at          timestamptz NOT NULL DEFAULT now(),
  idp_checked_at        timestamptz NOT NULL DEFAULT now(),
  ended_at              timestamptz NULL,
  end_reason            text        NULL,
  CONSTRAINT web_sessions_pkey PRIMARY KEY (id),
  CONSTRAINT web_sessions_token_uk UNIQUE (token_hash),
  CONSTRAINT web_sessions_user_fk FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE RESTRICT,
  CONSTRAINT web_sessions_token_ck CHECK (octet_length(token_hash) = 32),
  CONSTRAINT web_sessions_csrf_ck CHECK (csrf_token ~ '^[0-9a-f]{64}$'),
  CONSTRAINT web_sessions_idp_session_ck CHECK (idp_session_id IS NULL OR char_length(idp_session_id) BETWEEN 1 AND 128),
  CONSTRAINT web_sessions_times_ck CHECK (last_seen_at >= issued_at AND authenticated_at <= last_seen_at + interval '5 minutes'),
  CONSTRAINT web_sessions_end_ck CHECK ((ended_at IS NULL) = (end_reason IS NULL) AND (end_reason IS NULL OR end_reason IN ('logout','revoked','offboarded')))
);
COMMENT ON TABLE web_sessions IS 'Sessão web no servidor (ADR-12 opção B, RNF-06): 30 min de inatividade e 8 h no total, contadas pela api. O cookie ec_session leva só o valor opaco; nenhum token em claro fica na base.';
COMMENT ON COLUMN web_sessions.id IS 'Identificador interno; não sai do banco.';
COMMENT ON COLUMN web_sessions.token_hash IS 'SHA-256 do valor do cookie ec_session (o valor em si nunca é gravado).';
COMMENT ON COLUMN web_sessions.user_id IS 'Pessoa da sessão.';
COMMENT ON COLUMN web_sessions.csrf_token IS 'Token anti-CSRF da sessão (X-CSRF-Token nas decisões críticas, ADR-19); devolvido só em getCurrentUser.';
COMMENT ON COLUMN web_sessions.idp_session_id IS 'Claim sid do ID token (sessão do Keycloak), para correlacionar com os eventos do SSO.';
COMMENT ON COLUMN web_sessions.sealed_refresh_token IS 'Refresh token do Keycloak selado com AES-256-GCM pela chave de sessão da api; serve à conferência de revogação a cada 60 s e ao logout de retaguarda. Nulo quando o SSO não devolveu refresh token.';
COMMENT ON COLUMN web_sessions.authenticated_at IS 'Última autenticação no SSO (claim auth_time); decisão crítica exige no máximo 10 min (ADR-19).';
COMMENT ON COLUMN web_sessions.issued_at IS 'Criação da sessão (início das 8 h).';
COMMENT ON COLUMN web_sessions.last_seen_at IS 'Última requisição autenticada (início dos 30 min de inatividade).';
COMMENT ON COLUMN web_sessions.idp_checked_at IS 'Última conferência da sessão no Keycloak (refresh); a api confere de novo depois de 60 s.';
COMMENT ON COLUMN web_sessions.ended_at IS 'Fim da sessão; nulo enquanto vale (a expiração por tempo não precisa gravar).';
COMMENT ON COLUMN web_sessions.end_reason IS 'logout (encerrarSessao), revoked (SSO recusou a sessão) ou offboarded (pessoa desativada).';
COMMENT ON CONSTRAINT web_sessions_token_uk ON web_sessions IS 'Um valor de cookie por sessão.';
COMMENT ON CONSTRAINT web_sessions_token_ck ON web_sessions IS 'SHA-256 tem 32 bytes.';
COMMENT ON CONSTRAINT web_sessions_csrf_ck ON web_sessions IS '32 bytes aleatórios em hexadecimal.';
COMMENT ON CONSTRAINT web_sessions_idp_session_ck ON web_sessions IS 'Tamanho do id de sessão do SSO.';
COMMENT ON CONSTRAINT web_sessions_times_ck ON web_sessions IS 'Instantes coerentes (folga de relógio de 5 min para o auth_time do SSO).';
COMMENT ON CONSTRAINT web_sessions_end_ck ON web_sessions IS 'Sessão encerrada tem motivo, e só os motivos conhecidos.';
CREATE INDEX web_sessions_user_idx ON web_sessions (user_id) WHERE ended_at IS NULL;
COMMENT ON INDEX web_sessions_user_idx IS 'Sessões abertas de uma pessoa (encerrar ao desligar, RNF-20); cobre a FK.';

GRANT SELECT, INSERT ON web_sessions TO easycode_api;
GRANT UPDATE (last_seen_at, idp_checked_at, sealed_refresh_token, ended_at, end_reason) ON web_sessions TO easycode_api;
GRANT SELECT ON web_sessions TO easycode_worker;
GRANT UPDATE (ended_at, end_reason) ON web_sessions TO easycode_worker;
GRANT SELECT ON web_sessions TO easycode_backup;
-- Desligamento (D-31, RNF-20): o worker desativa e anonimiza o nome de exibição; o sub fica como id opaco.
GRANT UPDATE (display_name, active, anonymized_at) ON users TO easycode_worker;

-- search_path fixo em toda função do esquema que ainda não o fixa (nota do DBA do MARCO-0).
DO $$
DECLARE f regprocedure;
BEGIN
  FOR f IN SELECT p.oid::regprocedure FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
            WHERE n.nspname = 'easycode' AND p.prokind = 'f'
              AND NOT EXISTS (SELECT 1 FROM unnest(coalesce(p.proconfig, '{}')) c WHERE c LIKE 'search_path=%')
  LOOP
    EXECUTE format('ALTER FUNCTION %s SET search_path = easycode, pg_catalog', f);
  END LOOP;
END $$;
