-- +goose Up
DROP TABLE loan_month_plans;
CREATE TABLE loan_month_plans (
	debtor_id UUID NOT NULL,
	user_id UUID NOT NULL,
	month DATE NOT NULL CHECK (month BETWEEN '2000-01-01' AND '2100-12-01' AND extract(day FROM month)=1),
	amount NUMERIC(14,2) NOT NULL CHECK(amount > 0),
	created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	PRIMARY KEY(debtor_id, month),
	FOREIGN KEY(debtor_id, user_id) REFERENCES loan_debtors(id, user_id) ON DELETE CASCADE
);
CREATE INDEX loan_month_plans_owner_idx ON loan_month_plans(user_id, debtor_id, month);

-- +goose Down
DROP TABLE loan_month_plans;
CREATE TABLE loan_month_plans (
	movement_id UUID NOT NULL,
	debtor_id UUID NOT NULL,
	user_id UUID NOT NULL,
	month DATE NOT NULL CHECK (month BETWEEN '2000-01-01' AND '2100-12-01' AND extract(day FROM month)=1),
	amount NUMERIC(14,2) NOT NULL CHECK(amount > 0),
	PRIMARY KEY(movement_id, month),
	FOREIGN KEY(movement_id, debtor_id, user_id) REFERENCES loan_movements(id, debtor_id, user_id) ON DELETE CASCADE
);
CREATE INDEX loan_month_plans_owner_idx ON loan_month_plans(user_id, debtor_id, month);

