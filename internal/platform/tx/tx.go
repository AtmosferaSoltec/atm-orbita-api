// Package tx es la única forma de abrir una transacción. La transacción viaja
// en el contexto, así un caso de uso puede llamar a varios repositorios (incluso
// de otros módulos, por sus interfaces) y que todo se guarde junto o nada.
package tx

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DBTX es lo que usan los repositorios (y el código que genera sqlc): lo
// cumplen tanto el pool como una pgx.Tx.
type DBTX interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type contextKey struct{}

// Runner abre transacciones sobre el pool.
type Runner struct {
	pool *pgxpool.Pool
}

// NewRunner crea el Runner.
func NewRunner(pool *pgxpool.Pool) Runner {
	return Runner{pool: pool}
}

// InTx corre fn dentro de una transacción. Si ya hay una en ctx, la reutiliza:
// un caso de uso que llama a otro no abre una segunda. Si fn devuelve error o
// hace panic, se deshace todo.
func (r Runner) InTx(ctx context.Context, fn func(ctx context.Context) error) (err error) {
	if _, ok := ctx.Value(contextKey{}).(pgx.Tx); ok {
		return fn(ctx)
	}
	transaction, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			_ = transaction.Rollback(ctx)
			panic(recovered)
		}
		if err != nil {
			if rollbackErr := transaction.Rollback(ctx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
				err = errors.Join(err, fmt.Errorf("rollback: %w", rollbackErr))
			}
		}
	}()
	if err = fn(context.WithValue(ctx, contextKey{}, transaction)); err != nil {
		return err
	}
	if err = transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

// DB devuelve la transacción en curso si la hay, o el pool.
func (r Runner) DB(ctx context.Context) DBTX {
	if transaction, ok := ctx.Value(contextKey{}).(pgx.Tx); ok {
		return transaction
	}
	return r.pool
}
