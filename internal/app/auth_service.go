package app

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/udaypankhaniya/trangly/internal/infra/crypto"
	"github.com/udaypankhaniya/trangly/internal/infra/db"
	"github.com/udaypankhaniya/trangly/pkg/version"
)

// AuthService handles admin login, JWT issuance, and token validation.
// There is exactly one admin in Trangly v1.0.
type AuthService struct {
	db        *db.DB
	masterKey []byte
	log       *slog.Logger
}

// Claims holds the verified payload of a Trangly JWT.
type Claims struct {
	UserID   string
	Username string
	IssuedAt time.Time
	ExpireAt time.Time
}

// NewAuthService creates an AuthService. masterKey is the 32-byte AES key used to
// store the JWT secret — it is NOT stored in the service; only the derived HMAC key is held.
func NewAuthService(database *db.DB, masterKey []byte) (*AuthService, error) {
	svc := &AuthService{
		db:        database,
		masterKey: masterKey,
		log:       slog.Default().With("service", "auth"),
	}
	// Ensure a JWT secret is present; seed one on first run.
	if _, err := database.GetJWTSecret(); errors.Is(err, db.ErrNotFound) {
		raw, err := crypto.RandomBytes(32)
		if err != nil {
			return nil, fmt.Errorf("auth: generate JWT secret: %w", err)
		}
		if err := database.UpsertJWTSecret(hex.EncodeToString(raw)); err != nil {
			return nil, fmt.Errorf("auth: store JWT secret: %w", err)
		}
		svc.log.Info("generated new JWT signing secret")
	}
	return svc, nil
}

// CreateAdmin creates the initial admin account on first run.
// Returns an error if any user already exists.
func (s *AuthService) CreateAdmin(username, email, password string) error {
	exists, err := s.db.UserExists()
	if err != nil {
		return fmt.Errorf("auth: check user exists: %w", err)
	}
	if exists {
		return errors.New("auth: admin account already exists")
	}
	hash, err := bcryptHash(password)
	if err != nil {
		return fmt.Errorf("auth: hash password: %w", err)
	}
	id, err := newID()
	if err != nil {
		return err
	}
	if err := s.db.InsertUser(id, username, email, hash); err != nil {
		return fmt.Errorf("auth: insert user: %w", err)
	}
	s.log.Info("admin account created", "username", username, "email", email)
	return nil
}

// Login verifies credentials and returns a signed JWT on success.
// Returns ErrUnauthorized on bad credentials to prevent timing-based enumeration.
func (s *AuthService) Login(username, password string) (string, error) {
	user, err := s.db.GetUserByUsername(username)
	if errors.Is(err, db.ErrNotFound) {
		// Always do the hash comparison even on not-found to prevent timing attacks.
		bcryptCompare("$2a$12$notarealhashjustpadding", password) //nolint:errcheck
		return "", ErrUnauthorized
	}
	if err != nil {
		return "", fmt.Errorf("auth: get user: %w", err)
	}
	if err := bcryptCompare(user.PasswordHash, password); err != nil {
		return "", ErrUnauthorized
	}
	token, err := s.signJWT(user.ID, user.Username)
	if err != nil {
		return "", fmt.Errorf("auth: sign token: %w", err)
	}
	s.log.Info("user logged in", "username", username)
	return token, nil
}

// ValidateToken parses and verifies a JWT, returning the decoded claims.
func (s *AuthService) ValidateToken(tokenStr string) (*Claims, error) {
	return s.verifyJWT(tokenStr)
}

// ErrUnauthorized is returned when login credentials are invalid or the current
// password supplied to ChangePassword does not match.
var ErrUnauthorized = errors.New("auth: invalid credentials")

// ErrUsernameTaken is returned when UpdateProfile is called with a username
// that is already held by another account (SQLite UNIQUE constraint).
var ErrUsernameTaken = errors.New("auth: username already taken")

// --- JWT implementation (HS256, stdlib only) ---

func (s *AuthService) jwtKey() ([]byte, error) {
	row, err := s.db.GetJWTSecret()
	if err != nil {
		return nil, fmt.Errorf("auth: get JWT secret: %w", err)
	}
	return hex.DecodeString(row.SecretHex)
}

func (s *AuthService) signJWT(userID, username string) (string, error) {
	key, err := s.jwtKey()
	if err != nil {
		return "", err
	}
	now := time.Now().UTC()
	exp := now.Add(24 * time.Hour)

	header := base64URLJSON(map[string]string{"alg": "HS256", "typ": "JWT"})
	payload := base64URLJSON(map[string]any{
		"sub":      userID,
		"username": username,
		"iat":      now.Unix(),
		"exp":      exp.Unix(),
		"iss":      version.AppSlug,
	})

	signingInput := header + "." + payload
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(signingInput))
	sig := encodeBase64URL(mac.Sum(nil))

	return signingInput + "." + sig, nil
}

func (s *AuthService) verifyJWT(tokenStr string) (*Claims, error) {
	parts := splitDot(tokenStr, 3)
	if parts == nil {
		return nil, errors.New("auth: malformed token")
	}
	key, err := s.jwtKey()
	if err != nil {
		return nil, err
	}

	signingInput := parts[0] + "." + parts[1]
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(signingInput))
	expected := mac.Sum(nil)

	sig, err := decodeBase64URL(parts[2])
	if err != nil {
		return nil, errors.New("auth: malformed token signature")
	}
	if !hmac.Equal(expected, sig) {
		return nil, errors.New("auth: invalid token signature")
	}

	claimsData, err := decodeBase64URL(parts[1])
	if err != nil {
		return nil, errors.New("auth: malformed token payload")
	}
	m, err := parseJSONMap(claimsData)
	if err != nil {
		return nil, errors.New("auth: malformed token claims")
	}

	exp, _ := m["exp"].(float64)
	iat, _ := m["iat"].(float64)
	if time.Now().UTC().Unix() > int64(exp) {
		return nil, errors.New("auth: token expired")
	}

	// Validate issuer — reject tokens created by a different installation.
	if iss, _ := m["iss"].(string); iss != version.AppSlug {
		return nil, errors.New("auth: invalid token issuer")
	}

	return &Claims{
		UserID:   stringVal(m, "sub"),
		Username: stringVal(m, "username"),
		IssuedAt: time.Unix(int64(iat), 0),
		ExpireAt: time.Unix(int64(exp), 0),
	}, nil
}

// --- Profile management ---

// GetProfile returns the user row for the given user ID.
// The caller must never expose the PasswordHash field in API responses.
func (s *AuthService) GetProfile(userID string) (*db.UserRow, error) {
	user, err := s.db.GetUserByID(userID)
	if err != nil {
		return nil, fmt.Errorf("auth: get profile: %w", err)
	}
	return user, nil
}

// UpdateProfile updates the username and email for the given user.
// Returns ErrUsernameTaken when the desired username is already held by another account.
func (s *AuthService) UpdateProfile(userID, username, email string) error {
	username = strings.TrimSpace(username)
	email = strings.TrimSpace(email)
	if username == "" {
		return fmt.Errorf("auth: username must not be empty")
	}
	err := s.db.UpdateUserProfile(userID, username, email)
	if err != nil && strings.Contains(err.Error(), "UNIQUE constraint") {
		return ErrUsernameTaken
	}
	if err != nil {
		return fmt.Errorf("auth: update profile: %w", err)
	}
	s.log.Info("profile updated", "user_id", userID)
	return nil
}

// ChangePassword verifies the current password then replaces it with a new bcrypt hash.
// Returns ErrUnauthorized if currentPassword does not match the stored hash.
// The new password must be at least 8 characters and at most maxPasswordLen bytes.
func (s *AuthService) ChangePassword(userID, currentPassword, newPassword string) error {
	user, err := s.db.GetUserByID(userID)
	if err != nil {
		return fmt.Errorf("auth: change password: %w", err)
	}
	if err := bcryptCompare(user.PasswordHash, currentPassword); err != nil {
		return ErrUnauthorized
	}
	if len(newPassword) < 8 {
		return fmt.Errorf("auth: new password must be at least 8 characters")
	}
	hash, err := bcryptHash(newPassword)
	if err != nil {
		return fmt.Errorf("auth: hash new password: %w", err)
	}
	if err := s.db.UpdateUserPassword(userID, hash); err != nil {
		return fmt.Errorf("auth: update password: %w", err)
	}
	s.log.Info("password changed", "user_id", userID)
	return nil
}
