package repository

import (
	"context"
	"time"

	"github.com/bits-assignment/dating-platform/backend/internal/models"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

type UserRepo struct {
	pool *pgxpool.Pool
}

func NewUserRepo(pool *pgxpool.Pool) *UserRepo {
	return &UserRepo{pool: pool}
}

func HashPassword(password string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(b), err
}

func CheckPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

func (r *UserRepo) Create(ctx context.Context, email, password, name string, dob *time.Time) (*models.User, error) {
	hash, err := HashPassword(password)
	if err != nil {
		return nil, err
	}
	u := &models.User{
		ID:           uuid.New(),
		Email:        email,
		PasswordHash: hash,
		Name:         name,
		DateOfBirth:  dob,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	q := `INSERT INTO users (id, email, password_hash, name, date_of_birth, created_at, updated_at)
	      VALUES ($1, $2, $3, $4, $5, $6, $7)`
	_, err = r.pool.Exec(ctx, q, u.ID, u.Email, u.PasswordHash, u.Name, dob, u.CreatedAt, u.UpdatedAt)
	if err != nil {
		return nil, err
	}
	u.PasswordHash = ""
	return u, nil
}

func (r *UserRepo) GetByEmail(ctx context.Context, email string) (*models.User, error) {
	q := `SELECT id, email, password_hash, name, date_of_birth, created_at, updated_at FROM users WHERE email = $1`
	u := &models.User{}
	err := r.pool.QueryRow(ctx, q, email).Scan(
		&u.ID, &u.Email, &u.PasswordHash, &u.Name, &u.DateOfBirth, &u.CreatedAt, &u.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return u, nil
}

func (r *UserRepo) GetByID(ctx context.Context, id uuid.UUID) (*models.User, error) {
	q := `SELECT id, email, password_hash, name, date_of_birth, created_at, updated_at FROM users WHERE id = $1`
	u := &models.User{}
	err := r.pool.QueryRow(ctx, q, id).Scan(
		&u.ID, &u.Email, &u.PasswordHash, &u.Name, &u.DateOfBirth, &u.CreatedAt, &u.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	u.PasswordHash = ""
	return u, nil
}
