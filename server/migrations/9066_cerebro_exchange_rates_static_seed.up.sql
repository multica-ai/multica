-- FIR-40: temporary static USD->{DKK,EUR} seed.
--
-- Ships the display-currency feature with usable rates *now*, before the
-- dynamic daily fetch is wired up. Finding a way to fetch + keep these rates
-- current via Hatchet (runs.firtal.com) is tracked as a separate sub-issue;
-- once that lands it overwrites these rows with live rates.
--
-- ON CONFLICT DO NOTHING: idempotent on re-run, and a no-op if a row already
-- exists (so a future live fetch is never clobbered by this seed).
INSERT INTO cerebro_exchange_rates (base_currency, target_currency, rate, fetched_at, updated_at)
VALUES
    ('USD', 'DKK', 6.85, now(), now()),
    ('USD', 'EUR', 0.92, now(), now())
ON CONFLICT (base_currency, target_currency) DO NOTHING;
