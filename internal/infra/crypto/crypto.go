// Package crypto provides AES-256-GCM encryption and Argon2id key derivation.
// All secrets stored by Trangly (GitHub App PEM, project env vars, webhook secrets)
// are encrypted with a master key derived from the admin password via Argon2id.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/argon2"
)

const (
	// Argon2id parameters — OWASP recommended minimum.
	argonTime    = 1
	argonMemory  = 64 * 1024 // 64 MB
	argonThreads = 4
	argonKeyLen  = 32 // 256-bit key for AES-256

	// SaltSize is the number of random bytes used as the Argon2id salt.
	SaltSize = 16
)

// DeriveKey derives a 32-byte AES key from a password and salt using Argon2id.
// The salt must be SaltSize bytes; generate it with RandomBytes(SaltSize) on first use.
func DeriveKey(password string, salt []byte) ([]byte, error) {
	if len(salt) != SaltSize {
		return nil, fmt.Errorf("crypto: salt must be %d bytes, got %d", SaltSize, len(salt))
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return key, nil
}

// Encrypt encrypts plaintext with AES-256-GCM using the provided 32-byte key.
// The returned ciphertext is: nonce (12 bytes) || encrypted data || GCM tag.
func Encrypt(key, plaintext []byte) ([]byte, error) {
	if len(key) != 32 {
		return nil, errors.New("crypto: key must be 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("crypto: aes.NewCipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("crypto: cipher.NewGCM: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("crypto: generating nonce: %w", err)
	}
	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)
	return ciphertext, nil
}

// Decrypt decrypts ciphertext produced by Encrypt using the provided 32-byte key.
// The ciphertext must contain the nonce as the first 12 bytes.
func Decrypt(key, ciphertext []byte) ([]byte, error) {
	if len(key) != 32 {
		return nil, errors.New("crypto: key must be 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("crypto: aes.NewCipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("crypto: cipher.NewGCM: %w", err)
	}
	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, errors.New("crypto: ciphertext too short")
	}
	nonce, data := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, data, nil)
	if err != nil {
		return nil, fmt.Errorf("crypto: decryption failed: %w", err)
	}
	return plaintext, nil
}

// RandomBytes returns n cryptographically random bytes.
func RandomBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return nil, fmt.Errorf("crypto: generating random bytes: %w", err)
	}
	return b, nil
}
