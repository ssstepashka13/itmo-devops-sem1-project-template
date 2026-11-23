CREATE TABLE IF NOT EXISTS prices (
    row_id BIGSERIAL PRIMARY KEY,
    id BIGINT NOT NULL,
    name TEXT NOT NULL,
    category TEXT NOT NULL,
    price NUMERIC(18,2) NOT NULL,
    create_date DATE NOT NULL
);
