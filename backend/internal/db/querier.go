package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Querier is the read/write surface shared by *pgxpool.Pool, pgx.Tx and *pgx.Conn.
// Repositories accept a Querier so the same method works inside or outside a transaction.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Beginner starts transactions. Satisfied by *pgxpool.Pool and pgx.Tx.
type Beginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// InTx runs fn inside a transaction, committing on success and rolling back on
// any error or panic. Rollback uses a cancellation-free context so that a
// client disconnect mid-request still releases locks instead of leaking the
// transaction until the connection is reaped.
func InTx(ctx context.Context, b Beginner, fn func(pgx.Tx) error) error {
	tx, err := b.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(context.WithoutCancel(ctx))
		}
	}()
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	committed = true
	return nil
}

// Postgres error classes we branch on.
const (
	codeUniqueViolation     = "23505"
	codeForeignKeyViolation = "23503"
	codeCheckViolation      = "23514"
)

func pgErr(err error) *pgconn.PgError {
	var e *pgconn.PgError
	if errors.As(err, &e) {
		return e
	}
	return nil
}

func IsUniqueViolation(err error) bool {
	e := pgErr(err)
	return e != nil && e.Code == codeUniqueViolation
}

func IsForeignKeyViolation(err error) bool {
	e := pgErr(err)
	return e != nil && e.Code == codeForeignKeyViolation
}

func IsCheckViolation(err error) bool {
	e := pgErr(err)
	return e != nil && e.Code == codeCheckViolation
}

// ConstraintName returns the violated constraint, or "" when err is not a
// constraint violation. Used to distinguish which unique index tripped.
func ConstraintName(err error) string {
	if e := pgErr(err); e != nil {
		return e.ConstraintName
	}
	return ""
}

// IsNoRows reports whether a QueryRow found nothing.
func IsNoRows(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}
