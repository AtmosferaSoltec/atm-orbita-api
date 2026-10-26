-- +goose Up
CREATE TABLE loan_debtors (
 id UUID PRIMARY KEY,
 user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 name VARCHAR(60) NOT NULL CHECK (length(trim(name)) > 0),
 currency VARCHAR(3) NOT NULL CHECK (currency IN ('PEN','USD')),
 share_token VARCHAR(43) UNIQUE CHECK (share_token IS NULL OR length(share_token)=43),
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 UNIQUE(id,user_id)
);
CREATE INDEX loan_debtors_user_idx ON loan_debtors(user_id,name,id);
CREATE TABLE loan_movements (
 id UUID PRIMARY KEY,
 debtor_id UUID NOT NULL,
 user_id UUID NOT NULL,
 kind VARCHAR(7) NOT NULL CHECK (kind IN ('loan','payment')),
 amount NUMERIC(14,2) NOT NULL CHECK (amount > 0),
 occurred_on DATE NOT NULL CHECK (occurred_on BETWEEN '2000-01-01' AND '2100-12-31'),
 applied_month DATE CHECK (applied_month BETWEEN '2000-01-01' AND '2100-12-01' AND extract(day FROM applied_month)=1),
 title VARCHAR(100) NOT NULL DEFAULT '',
 note VARCHAR(500) NOT NULL DEFAULT '',
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 UNIQUE(id,debtor_id,user_id),
 FOREIGN KEY(debtor_id,user_id) REFERENCES loan_debtors(id,user_id) ON DELETE CASCADE,
 CHECK ((kind='loan' AND applied_month IS NULL) OR (kind='payment' AND applied_month IS NOT NULL))
);
CREATE INDEX loan_movements_debtor_idx ON loan_movements(user_id,debtor_id,occurred_on,created_at,id);
CREATE TABLE loan_month_plans (
 movement_id UUID NOT NULL,
 debtor_id UUID NOT NULL,
 user_id UUID NOT NULL,
 month DATE NOT NULL CHECK (month BETWEEN '2000-01-01' AND '2100-12-01' AND extract(day FROM month)=1),
 amount NUMERIC(14,2) NOT NULL CHECK(amount > 0),
 PRIMARY KEY(movement_id,month),
 FOREIGN KEY(movement_id,debtor_id,user_id) REFERENCES loan_movements(id,debtor_id,user_id) ON DELETE CASCADE
);
CREATE INDEX loan_month_plans_owner_idx ON loan_month_plans(user_id,debtor_id,month);
-- +goose Down
DROP TABLE loan_month_plans;
DROP TABLE loan_movements;
DROP TABLE loan_debtors;
