-- +goose Up
CREATE TABLE services (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name VARCHAR(60) NOT NULL,
    color VARCHAR(7) NOT NULL,
    currency VARCHAR(3) NOT NULL CHECK (currency IN ('PEN', 'USD')),
    amount NUMERIC(14,2) NOT NULL CHECK (amount > 0),
    billing_day SMALLINT NOT NULL CHECK (billing_day BETWEEN 1 AND 31),
    started_on DATE NOT NULL,
    status VARCHAR(10) NOT NULL CHECK (status IN ('active', 'paused', 'cancelled')),
    ended_on DATE,
    note VARCHAR(500) NOT NULL DEFAULT '',
    shared BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (id, user_id),
    CHECK ((status = 'cancelled' AND ended_on >= started_on) OR (status <> 'cancelled' AND ended_on IS NULL)),
    CHECK (status <> 'cancelled' OR ended_on IS NOT NULL)
);
CREATE INDEX services_user_idx ON services(user_id, name, id);

CREATE TABLE service_members (
    id UUID PRIMARY KEY,
    service_id UUID NOT NULL,
    user_id UUID NOT NULL,
    name VARCHAR(60) NOT NULL,
    amount NUMERIC(14,2) NOT NULL CHECK (amount > 0),
    since_month DATE NOT NULL CHECK (extract(day FROM since_month) = 1),
    sort_order INTEGER NOT NULL DEFAULT 0,
    UNIQUE (id, service_id, user_id),
    FOREIGN KEY (service_id, user_id) REFERENCES services(id, user_id) ON DELETE CASCADE
);
CREATE INDEX service_members_service_idx ON service_members(user_id, service_id, sort_order);

CREATE TABLE service_member_payments (
    id UUID PRIMARY KEY,
    service_id UUID NOT NULL,
    user_id UUID NOT NULL,
    member_id UUID NOT NULL,
    month DATE NOT NULL CHECK (extract(day FROM month) = 1),
    paid_on DATE NOT NULL,
    amount NUMERIC(14,2) NOT NULL CHECK (amount > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (member_id, month),
    FOREIGN KEY (service_id, user_id) REFERENCES services(id, user_id) ON DELETE CASCADE,
    FOREIGN KEY (member_id, service_id, user_id) REFERENCES service_members(id, service_id, user_id) ON DELETE CASCADE
);
CREATE INDEX service_payments_service_idx ON service_member_payments(user_id, service_id, month);

-- +goose Down
DROP TABLE service_member_payments;
DROP TABLE service_members;
DROP TABLE services;
