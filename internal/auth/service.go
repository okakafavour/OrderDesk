package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidInput       = errors.New("invalid registration input")
	ErrEmailExists        = errors.New("email already registered")
	ErrInvalidCredentials = errors.New("invalid email or password")
)

type Service struct {
	DB        *pgxpool.Pool
	JWTSecret []byte
}

type RegisterInput struct {
	FullName     string
	Email        string
	Password     string
	BusinessName string
}

type Claims struct {
	UserID     string `json:"user_id"`
	BusinessID string `json:"business_id"`
	jwt.RegisteredClaims
}

func (s *Service) Register(ctx context.Context, input RegisterInput) (string, error) {
	input.FullName = strings.TrimSpace(input.FullName)
	input.Email = strings.ToLower(strings.TrimSpace(input.Email))
	input.BusinessName = strings.TrimSpace(input.BusinessName)

	if input.FullName == "" || input.Email == "" ||
		input.BusinessName == "" || len(input.Password) < 12 {
		return "", ErrInvalidInput
	}

	passwordHash, err := bcrypt.GenerateFromPassword(
		[]byte(input.Password),
		bcrypt.DefaultCost,
	)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}

	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("begin registration: %w", err)
	}
	defer tx.Rollback(ctx)

	var userID string
	err = tx.QueryRow(ctx, `
		INSERT INTO users (email, password_hash, full_name)
		VALUES ($1, $2, $3)
		RETURNING id
	`, input.Email, string(passwordHash), input.FullName).Scan(&userID)
	if err != nil {
		if strings.Contains(err.Error(), "users_email_unique_lower") {
			return "", ErrEmailExists
		}
		return "", fmt.Errorf("create user: %w", err)
	}

	var businessID string
	err = tx.QueryRow(ctx, `
		INSERT INTO businesses (name)
		VALUES ($1)
		RETURNING id
	`, input.BusinessName).Scan(&businessID)
	if err != nil {
		return "", fmt.Errorf("create business: %w", err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO business_memberships (business_id, user_id, role)
		VALUES ($1, $2, 'owner')
	`, businessID, userID)
	if err != nil {
		return "", fmt.Errorf("create business membership: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit registration: %w", err)
	}

	now := time.Now()
	claims := Claims{
		UserID:     userID,
		BusinessID: businessID,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(24 * time.Hour)),
			Subject:   userID,
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signedToken, err := token.SignedString(s.JWTSecret)
	if err != nil {
		return "", fmt.Errorf("sign authentication token: %w", err)
	}

	return signedToken, nil
}

func (s *Service) Login(
	ctx context.Context,
	email string,
	password string,
) (string, error) {
	email = strings.ToLower(strings.TrimSpace(email))

	if email == "" || password == "" || len(password) > 72 {
		return "", ErrInvalidInput
	}

	var userID string
	var passwordHash string
	var businessID string

	err := s.DB.QueryRow(ctx, `
		SELECT u.id, u.password_hash, bm.business_id
		FROM users u
		JOIN business_memberships bm ON bm.user_id = u.id
		WHERE LOWER(u.email) = $1
		ORDER BY bm.created_at ASC
		LIMIT 1
	`, email).Scan(&userID, &passwordHash, &businessID)

	if err != nil {
		return "", ErrInvalidCredentials
	}

	if err := bcrypt.CompareHashAndPassword(
		[]byte(passwordHash),
		[]byte(password),
	); err != nil {
		return "", ErrInvalidCredentials
	}

	now := time.Now()
	claims := Claims{
		UserID:     userID,
		BusinessID: businessID,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(24 * time.Hour)),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(s.JWTSecret)
}
