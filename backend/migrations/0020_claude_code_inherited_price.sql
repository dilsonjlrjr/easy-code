-- 0020 — Preço do Claude Code herdado da Anthropic (US-58, ADR-14, ADR-23). O provedor claude-code chama os mesmos
-- modelos da Anthropic: um modelo claude-code sem preço próprio vigente usa o preço vigente do modelo anthropic de
-- mesmo nome, na mesma organização. Preço próprio vigente do modelo claude-code é exceção e vence. Assim a mudança
-- de preço da Anthropic vale para os dois provedores sem cópia, e a chamada guarda o preço que de fato usou.
SET search_path TO easycode, public;

CREATE FUNCTION price_applies(p_price_model bigint, p_call_model bigint) RETURNS boolean
  LANGUAGE sql STABLE SET search_path = easycode, pg_catalog AS $$
  SELECT p_price_model = p_call_model OR EXISTS (
    SELECT 1 FROM easycode.models c JOIN easycode.models a ON a.name = c.name
     WHERE c.id = p_call_model AND c.provider = 'claude-code' AND a.id = p_price_model AND a.provider = 'anthropic')
$$;
COMMENT ON FUNCTION price_applies(bigint, bigint) IS 'regra: ADR-23/US-58 — o preço vale para a chamada quando é do mesmo modelo ou, para um modelo claude-code, do modelo anthropic de mesmo nome.';

CREATE FUNCTION price_in_force(p_organization bigint, p_model bigint, p_at timestamptz) RETURNS bigint
  LANGUAGE sql STABLE SET search_path = easycode, pg_catalog AS $$
  SELECT coalesce(
    (SELECT mp.id FROM easycode.model_prices mp
      WHERE mp.organization_id = p_organization AND mp.model_id = p_model AND mp.effective_since <= p_at
      ORDER BY mp.effective_since DESC LIMIT 1),
    (SELECT mp.id FROM easycode.models c
       JOIN easycode.models a ON a.provider = 'anthropic' AND a.name = c.name
       JOIN easycode.model_prices mp ON mp.model_id = a.id AND mp.organization_id = p_organization AND mp.effective_since <= p_at
      WHERE c.id = p_model AND c.provider = 'claude-code'
      ORDER BY mp.effective_since DESC LIMIT 1))
$$;
COMMENT ON FUNCTION price_in_force(bigint, bigint, timestamptz) IS 'regra: ADR-14/ADR-23 — id do preço vigente do modelo na organização no instante; modelo claude-code sem preço próprio vigente herda o do modelo anthropic de mesmo nome; nulo sem preço.';

REVOKE ALL ON FUNCTION price_applies(bigint, bigint) FROM PUBLIC;
REVOKE ALL ON FUNCTION price_in_force(bigint, bigint, timestamptz) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION price_applies(bigint, bigint) TO easycode_api, easycode_worker, easycode_retention;
GRANT EXECUTE ON FUNCTION price_in_force(bigint, bigint, timestamptz) TO easycode_api, easycode_worker, easycode_retention;

-- Custo calculado no banco (RNF-16): o preço da chamada pode ser o herdado.
CREATE OR REPLACE FUNCTION model_calls_compute_cost() RETURNS trigger LANGUAGE plpgsql
  SET search_path = easycode, pg_catalog AS $$
DECLARE p model_prices%ROWTYPE;
BEGIN
  SELECT * INTO p FROM model_prices WHERE id = NEW.price_id;
  IF NOT price_applies(p.model_id, NEW.model_id) OR p.effective_since > NEW.started_at THEN
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
