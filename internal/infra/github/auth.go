package github

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"log/slog"
	"time"
)

// App holds a parsed GitHub App identity for JWT-authenticated API calls.
// Create via NewApp; intended to be short-lived (per-request scope).
// Never cache the decrypted key in memory long-term.
type App struct {
	AppID int64
	Key   *rsa.PrivateKey
}

// APIError represents a non-success HTTP response from the GitHub API.
type APIError struct {
	Status     int
	Body       string
	RetryAfter time.Duration // Parsed from Retry-After header on 429 responses.
}

func (e *APIError) Error() string {
	return fmt.Sprintf("github: HTTP %d: %s", e.Status, e.Body)
}

// PEMInfo contains non-sensitive metadata about a parsed PEM private key.
type PEMInfo struct {
	BlockType string
	KeyBits   int
}

// ValidatePEM checks that pemBytes is a valid PEM-encoded RSA private key
// that can be used for JWT signing. Returns key metadata for diagnostics.
// This MUST be called before storing a PEM to prevent persisting unusable keys.
func ValidatePEM(pemBytes []byte) (*PEMInfo, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("github: pem.Decode failed — data is not PEM-encoded (len=%d)", len(pemBytes))
	}

	key, err := parseRSAKeyFromBlock(block)
	if err != nil {
		return nil, fmt.Errorf("github: PEM block type %q is not a valid RSA key: %w", block.Type, err)
	}

	if err := key.Validate(); err != nil {
		return nil, fmt.Errorf("github: RSA key validation failed: %w", err)
	}

	bits := key.N.BitLen()
	if bits < 2048 {
		return nil, fmt.Errorf("github: RSA key too small (%d bits), minimum 2048", bits)
	}

	return &PEMInfo{BlockType: block.Type, KeyBits: bits}, nil
}

// pubKeyFingerprint returns a hex-encoded SHA256 hash of the DER-encoded
// RSA public key. This fingerprint uniquely identifies the key pair and can
// be compared between store-time and use-time to detect data corruption.
func pubKeyFingerprint(key *rsa.PrivateKey) string {
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return "<marshal-error>"
	}
	h := sha256.Sum256(der)
	return hex.EncodeToString(h[:8]) // first 8 bytes = 16 hex chars
}

// NewApp parses the PEM-encoded RSA private key and returns an App ready for JWT signing.
// Logs key metadata (block type, bit length) for diagnostics — never the key itself.
func NewApp(appID int64, pemBytes []byte) (*App, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("github: pem.Decode failed — data is not PEM-encoded (len=%d)", len(pemBytes))
	}

	key, err := parseRSAKeyFromBlock(block)
	if err != nil {
		return nil, fmt.Errorf("github: parse private key (block_type=%s): %w", block.Type, err)
	}

	slog.Info("github: RSA key loaded",
		"app_id", appID,
		"block_type", block.Type,
		"key_bits", key.N.BitLen(),
		"pubkey_fp", pubKeyFingerprint(key),
	)

	return &App{AppID: appID, Key: key}, nil
}

// signAppJWT creates a GitHub App JWT (RS256) valid for 9 minutes.
// GitHub App JWTs must not exceed 10 minutes.
// The iss claim MUST be an int64 — GitHub rejects string-typed iss with 401.
func signAppJWT(appID int64, privateKey *rsa.PrivateKey) (string, error) {
	now := time.Now()
	iat := now.Add(-60 * time.Second).Unix() // buffer for clock skew
	exp := now.Add(9 * time.Minute).Unix()

	headerJSON, err := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT"})
	if err != nil {
		return "", fmt.Errorf("marshal JWT header: %w", err)
	}
	claimsJSON, err := json.Marshal(map[string]any{
		"iss": appID, // int64 — GitHub requires a JSON number, not a string
		"iat": iat,
		"exp": exp,
	})
	if err != nil {
		return "", fmt.Errorf("marshal JWT claims: %w", err)
	}

	header := base64.RawURLEncoding.EncodeToString(headerJSON)
	claims := base64.RawURLEncoding.EncodeToString(claimsJSON)
	signingInput := header + "." + claims

	digest := sha256.Sum256([]byte(signingInput))

	sig, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, digest[:])
	if err != nil {
		return "", fmt.Errorf("sign JWT: %w", err)
	}

	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

// parseRSAKeyFromBlock extracts an RSA private key from a decoded PEM block.
// Supports PKCS#1 ("RSA PRIVATE KEY") and PKCS#8 ("PRIVATE KEY") formats.
func parseRSAKeyFromBlock(block *pem.Block) (*rsa.PrivateKey, error) {
	switch block.Type {
	case "RSA PRIVATE KEY":
		return x509.ParsePKCS1PrivateKey(block.Bytes)
	case "PRIVATE KEY":
		key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, err
		}
		rsaKey, ok := key.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("expected RSA private key, got %T", key)
		}
		return rsaKey, nil
	default:
		return nil, fmt.Errorf("unsupported PEM type: %s", block.Type)
	}
}
