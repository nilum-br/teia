-- Application roles.
CREATE ROLE orders LOGIN PASSWORD 'orders';
CREATE ROLE reports LOGIN PASSWORD 'reports';

-- teia's login starts WITHOUT pg_monitor on purpose: the advisor should
-- notice it is blind inside this database and ask for exactly that grant.
CREATE ROLE teia LOGIN PASSWORD 'teia';

CREATE TABLE orders (
  id serial PRIMARY KEY,
  total numeric(10, 2) NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);
INSERT INTO orders (total) SELECT (random() * 100)::numeric(10, 2) FROM generate_series(1, 50);
GRANT SELECT ON orders TO orders, reports;
