-- Reversão manual da 0020 (desenvolvimento ou teste; o executor só anda para a frente, ADR-18 v2). Chamadas já
-- gravadas com preço herdado continuam válidas: o trigger só confere na escrita.
SET search_path TO easycode, public;

CREATE OR REPLACE FUNCTION model_calls_compute_cost() RETURNS trigger LANGUAGE plpgsql
  SET search_path = easycode, pg_catalog AS $$
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

DROP FUNCTION IF EXISTS price_in_force(bigint, bigint, timestamptz);
DROP FUNCTION IF EXISTS price_applies(bigint, bigint);
