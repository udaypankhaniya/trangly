package app

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

const bcryptCost = 12

// MaxPasswordLen is the maximum accepted password length in bytes, exported so
// HTTP handlers can reject oversized inputs before calling bcrypt.
const MaxPasswordLen = 1024

// ErrPasswordTooLong is returned when a password exceeds MaxPasswordLen bytes.
var ErrPasswordTooLong = errors.New("password must not exceed 1024 characters")

func bcryptHash(password string) (string, error) {
	if len(password) > MaxPasswordLen {
		return "", ErrPasswordTooLong
	}
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return "", fmt.Errorf("bcrypt: %w", err)
	}
	return string(h), nil
}

func bcryptCompare(hash, password string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
}

func newID() (string, error) {
	id, err := uuid.NewRandom()
	if err != nil {
		return "", fmt.Errorf("generate ID: %w", err)
	}
	return id.String(), nil
}

func base64URLJSON(v any) string {
	data, _ := json.Marshal(v)
	return base64.RawURLEncoding.EncodeToString(data)
}

func encodeBase64URL(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeBase64URL(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(s)
}

func splitDot(s string, n int) []string {
	parts := strings.Split(s, ".")
	if len(parts) != n {
		return nil
	}
	return parts
}

func parseJSONMap(data []byte) (map[string]any, error) {
	m := make(map[string]any)
	return m, json.Unmarshal(data, &m)
}

func stringVal(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}
