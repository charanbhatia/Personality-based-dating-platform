package messaging

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrMatchNotFound = errors.New("match not found")
	// ErrMatchTableMissing means Person B has not shipped the matches table
	// yet, so mutual-match authorisation cannot be evaluated.
	ErrMatchTableMissing = errors.New("matches table not available")
)

const undefinedTable = "42P01"

// MatchGate reads Person B's tables to authorise conversations. It never writes
// to them.
type MatchGate struct {
	pool *pgxpool.Pool
}

func NewMatchGate(pool *pgxpool.Pool) *MatchGate {
	return &MatchGate{pool: pool}
}

// Participants resolves a match to the two users it belongs to.
func (g *MatchGate) Participants(ctx context.Context, matchID uuid.UUID) (uuid.UUID, uuid.UUID, error) {
	const q = `SELECT user_a_id, user_b_id FROM matches WHERE id = $1`

	var a, b uuid.UUID
	err := g.pool.QueryRow(ctx, q, matchID).Scan(&a, &b)
	switch {
	case err == nil:
		return a, b, nil
	case errors.Is(err, pgx.ErrNoRows):
		return uuid.Nil, uuid.Nil, ErrMatchNotFound
	case isUndefinedTable(err):
		return uuid.Nil, uuid.Nil, ErrMatchTableMissing
	default:
		return uuid.Nil, uuid.Nil, err
	}
}

// IsBlocked reports whether either user has blocked the other. The blocks table
// arrives with Person B's safety work; until then nobody is blocked.
func (g *MatchGate) IsBlocked(ctx context.Context, a, b uuid.UUID) (bool, error) {
	const q = `SELECT EXISTS (
	    SELECT 1 FROM blocks
	    WHERE (blocker_id = $1 AND blocked_id = $2)
	       OR (blocker_id = $2 AND blocked_id = $1)
	)`

	var blocked bool
	err := g.pool.QueryRow(ctx, q, a, b).Scan(&blocked)
	if err != nil {
		if isUndefinedTable(err) {
			return false, nil
		}
		return false, err
	}
	return blocked, nil
}

func isUndefinedTable(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == undefinedTable
}
