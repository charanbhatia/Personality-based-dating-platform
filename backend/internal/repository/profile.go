package repository

import (
	"context"
	"time"

	"github.com/bits-assignment/dating-platform/backend/internal/models"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ProfileRepo struct {
	pool *pgxpool.Pool
}

func NewProfileRepo(pool *pgxpool.Pool) *ProfileRepo {
	return &ProfileRepo{pool: pool}
}

func (r *ProfileRepo) Create(ctx context.Context, userID uuid.UUID) error {
	q := `INSERT INTO profiles (user_id, bio, gender, location, photo_url, created_at, updated_at)
	      VALUES ($1, '', '', '', '', now(), now())`
	_, err := r.pool.Exec(ctx, q, userID)
	return err
}

func (r *ProfileRepo) CreateWith(ctx context.Context, userID uuid.UUID, bio, gender, location, photoURL string) error {
	q := `INSERT INTO profiles (user_id, bio, gender, location, photo_url, created_at, updated_at)
	      VALUES ($1, $2, $3, $4, $5, now(), now())`
	_, err := r.pool.Exec(ctx, q, userID, bio, gender, location, photoURL)
	return err
}

func (r *ProfileRepo) GetByUserID(ctx context.Context, userID uuid.UUID) (*models.Profile, error) {
	q := `SELECT id, user_id, bio, gender, location, photo_url, created_at, updated_at FROM profiles WHERE user_id = $1`
	p := &models.Profile{}
	err := r.pool.QueryRow(ctx, q, userID).Scan(
		&p.ID, &p.UserID, &p.Bio, &p.Gender, &p.Location, &p.PhotoURL, &p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return p, nil
}

func (r *ProfileRepo) Update(ctx context.Context, p *models.Profile) error {
	q := `UPDATE profiles SET bio=$2, gender=$3, location=$4, photo_url=$5, updated_at=now() WHERE user_id=$1`
	_, err := r.pool.Exec(ctx, q, p.UserID, p.Bio, p.Gender, p.Location, p.PhotoURL)
	return err
}

func (r *ProfileRepo) UpsertPersonality(ctx context.Context, userID uuid.UUID, traitsJSON []byte) error {
	q := `INSERT INTO personality_scores (user_id, traits, created_at, updated_at) VALUES ($1, $2, now(), now())
	      ON CONFLICT (user_id) DO UPDATE SET traits = $2, updated_at = now()`
	_, err := r.pool.Exec(ctx, q, userID, traitsJSON)
	return err
}

func (r *ProfileRepo) GetPersonality(ctx context.Context, userID uuid.UUID) ([]byte, error) {
	q := `SELECT traits FROM personality_scores WHERE user_id = $1`
	var traits []byte
	err := r.pool.QueryRow(ctx, q, userID).Scan(&traits)
	return traits, err
}

func (r *ProfileRepo) ListCandidates(ctx context.Context, excludeUserID uuid.UUID, limit, offset int) ([]Candidate, error) {
	q := `SELECT u.id, u.email, u.name, u.date_of_birth, p.id, p.user_id, p.bio, p.gender, p.location, p.photo_url,
	             COALESCE(ps.traits::text, '{}') as traits
	      FROM users u
	      JOIN profiles p ON p.user_id = u.id
	      LEFT JOIN personality_scores ps ON ps.user_id = u.id
	      WHERE u.id != $1
	      ORDER BY u.created_at DESC
	      LIMIT $2 OFFSET $3`
	rows, err := r.pool.Query(ctx, q, excludeUserID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Candidate
	for rows.Next() {
		var c Candidate
		var profileUserID uuid.UUID
		err := rows.Scan(&c.UserID, &c.Email, &c.Name, &c.DateOfBirth, &c.ProfileID, &profileUserID, &c.Bio, &c.Gender, &c.Location, &c.PhotoURL, &c.TraitsJSON)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

type Candidate struct {
	UserID      uuid.UUID
	Email       string
	Name        string
	DateOfBirth *time.Time
	ProfileID   uuid.UUID
	Bio         string
	Gender      string
	Location    string
	PhotoURL    string
	TraitsJSON  string
}
