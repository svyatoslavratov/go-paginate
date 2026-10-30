-- The demo catalog: 240 products generated from a series, the same on every run. Names include
-- "50% off" and "under_score" to show that % and _ are literal in searches.
CREATE TABLE IF NOT EXISTS products (
    id              integer PRIMARY KEY,
    sku             uuid NOT NULL,
    name            text NOT NULL,
    brand           text NOT NULL,
    category        text NOT NULL,
    price           numeric(10, 2) NOT NULL,
    rating          numeric(2, 1),
    stock           integer NOT NULL,
    tags            text[] NOT NULL DEFAULT '{}',
    released_at     timestamptz NOT NULL,
    discontinued_at timestamptz
);

INSERT INTO products (id, sku, name, brand, category, price, rating, stock, tags, released_at, discontinued_at)
SELECT i,
       md5('sku' || i)::uuid,
       (ARRAY['Aurora', 'Breeze', 'Cobalt', 'Drift', 'Ember', 'Fjord', 'Glacier', 'Harbor', 'Iris', 'Juniper'])[1 + i % 10]
           || ' ' || (ARRAY['lamp', 'chair', 'desk', 'sofa', 'shelf', 'rug', 'mirror', 'stool'])[1 + (i / 10) % 8]
           || CASE WHEN i % 17 = 0 THEN ' 50% off' WHEN i % 19 = 0 THEN ' under_score' ELSE '' END,
       (ARRAY['Nordwood', 'Lumen', 'Casa', 'Ferro', 'Oakline'])[1 + i % 5],
       (ARRAY['lighting', 'seating', 'tables', 'storage', 'decor'])[1 + (i / 3) % 5],
       round((5 + (i * 37) % 495)::numeric + (i % 100) / 100.0, 2),
       CASE WHEN i % 9 = 0 THEN NULL ELSE round(1 + ((i * 13) % 40) / 10.0, 1) END,
       (i * 17) % 120,
       ARRAY(SELECT tag FROM unnest(ARRAY['sale', 'new', 'eco', 'bestseller', 'limited']) WITH ORDINALITY AS t(tag, n)
             WHERE get_byte(decode(md5(tag || i), 'hex'), 0) < 80 ORDER BY n),
       timestamptz '2023-01-01 00:00:00+00' + (i * 3) * interval '1 day',
       CASE WHEN i % 13 = 0 THEN timestamptz '2025-06-01 00:00:00+00' + i * interval '1 day' END
FROM generate_series(1, 240) AS g(i)
ON CONFLICT (id) DO NOTHING;

CREATE INDEX IF NOT EXISTS products_category ON products (category);
CREATE INDEX IF NOT EXISTS products_price ON products (price);

-- Statistics for the planner, whose estimates count=estimate shows.
ANALYZE products;
