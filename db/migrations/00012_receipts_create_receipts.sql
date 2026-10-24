-- Plan 18: recibos por honorarios (módulo receipts, solo de la web). Tablas
-- nuevas: la versión anterior del API no las conoce y sigue funcionando.

-- +goose Up
CREATE TABLE receipts (
    id                UUID          PRIMARY KEY,
    user_id           UUID          NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    series            VARCHAR(4)    NOT NULL,
    -- Sin ceros a la izquierda ("00058" se guarda "58").
    number            VARCHAR(8)    NOT NULL,
    issue_date        DATE          NOT NULL,
    client_name       VARCHAR(200)  NOT NULL,
    client_doc_type   VARCHAR(4)    NOT NULL,
    client_doc_number VARCHAR(15)   NOT NULL DEFAULT '',
    description       VARCHAR(500)  NOT NULL DEFAULT '',
    gross             NUMERIC(14,2) NOT NULL,
    -- Tal cual viene del XML: SUNAT ya calcula el 8 %.
    withholding       NUMERIC(14,2) NOT NULL DEFAULT 0,
    collected_on      DATE,
    source            VARCHAR(10)   NOT NULL,
    created_at        TIMESTAMPTZ   NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ   NOT NULL DEFAULT now(),
    CONSTRAINT receipts_doc_type_check CHECK (client_doc_type IN ('RUC', 'DNI', 'OTRO')),
    CONSTRAINT receipts_source_check CHECK (source IN ('xml', 'manual')),
    CONSTRAINT receipts_gross_positive CHECK (gross > 0),
    CONSTRAINT receipts_withholding_range CHECK (withholding >= 0 AND withholding < gross)
);
-- Un recibo (serie + número) se registra una sola vez por persona.
CREATE UNIQUE INDEX receipts_user_code_key ON receipts (user_id, upper(series), number);
CREATE INDEX receipts_user_issue_date_idx ON receipts (user_id, issue_date DESC);

-- Un año sin fila usa el tope por defecto (S/ 50,000.00).
CREATE TABLE receipt_year_limits (
    user_id    UUID          NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    year       SMALLINT      NOT NULL,
    amount     NUMERIC(14,2) NOT NULL,
    updated_at TIMESTAMPTZ   NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, year),
    CONSTRAINT receipt_year_limits_amount_positive CHECK (amount > 0)
);

-- Los bytes viven en R2 bajo storage_key; aquí solo lo que se muestra.
CREATE TABLE receipt_documents (
    id           UUID         PRIMARY KEY,
    receipt_id   UUID         NOT NULL REFERENCES receipts (id) ON DELETE CASCADE,
    user_id      UUID         NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    kind         VARCHAR(3)   NOT NULL,
    file_name    VARCHAR(255) NOT NULL,
    size_bytes   INTEGER      NOT NULL,
    content_type VARCHAR(100) NOT NULL,
    storage_key  VARCHAR(300) NOT NULL,
    uploaded_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CONSTRAINT receipt_documents_kind_check CHECK (kind IN ('xml', 'pdf')),
    CONSTRAINT receipt_documents_size_positive CHECK (size_bytes > 0)
);
CREATE UNIQUE INDEX receipt_documents_receipt_kind_key ON receipt_documents (receipt_id, kind);
CREATE UNIQUE INDEX receipt_documents_storage_key_key ON receipt_documents (storage_key);

-- +goose Down
DROP TABLE receipt_documents;
DROP TABLE receipt_year_limits;
DROP TABLE receipts;
