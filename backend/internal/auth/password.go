package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

const MinPasswordLength = 12
const MaxPasswordLength = 128

type PasswordParams struct {
	Memory      uint32
	Iterations  uint32
	Parallelism uint8
	SaltLength  uint32
	KeyLength   uint32
}

var DefaultPasswordParams = PasswordParams{Memory: 64 * 1024, Iterations: 3, Parallelism: 2, SaltLength: 16, KeyLength: 32}

func ValidatePassword(password string) error {
	length := len([]byte(password))
	if length < MinPasswordLength {
		return fmt.Errorf("password must be at least %d characters", MinPasswordLength)
	}
	if length > MaxPasswordLength {
		return fmt.Errorf("password must be at most %d bytes", MaxPasswordLength)
	}
	return nil
}
func HashPassword(password string, params PasswordParams) (string, error) {
	if err := ValidatePassword(password); err != nil {
		return "", err
	}
	if err := validateParams(params); err != nil {
		return "", err
	}
	salt := make([]byte, params.SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}
	hash := argon2.IDKey([]byte(password), salt, params.Iterations, params.Memory, params.Parallelism, params.KeyLength)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, params.Memory, params.Iterations, params.Parallelism, base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(hash)), nil
}
func VerifyPassword(password, encoded string) (bool, error) {
	params, salt, expected, err := parseHash(encoded)
	if err != nil {
		return false, err
	}
	actual := argon2.IDKey([]byte(password), salt, params.Iterations, params.Memory, params.Parallelism, uint32(len(expected)))
	return subtle.ConstantTimeCompare(actual, expected) == 1, nil
}
func parseHash(encoded string) (PasswordParams, []byte, []byte, error) {
	var p PasswordParams
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return p, nil, nil, errors.New("invalid password hash format")
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return p, nil, nil, errors.New("unsupported argon2 version")
	}
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.Memory, &p.Iterations, &p.Parallelism); err != nil {
		return p, nil, nil, errors.New("invalid argon2 parameters")
	}
	if err := validateParams(p); err != nil {
		return p, nil, nil, err
	}
	salt, err := base64.RawStdEncoding.Strict().DecodeString(parts[4])
	if err != nil || len(salt) < 8 {
		return p, nil, nil, errors.New("invalid password hash salt")
	}
	hash, err := base64.RawStdEncoding.Strict().DecodeString(parts[5])
	if err != nil || len(hash) < 16 || len(hash) > 64 {
		return p, nil, nil, errors.New("invalid password hash value")
	}
	return p, salt, hash, nil
}
func validateParams(p PasswordParams) error {
	if p.Memory < 8*1024 || p.Memory > 1024*1024 || p.Iterations < 1 || p.Iterations > 10 || p.Parallelism < 1 || p.Parallelism > 16 {
		return errors.New("invalid argon2 parameters")
	}
	if p.SaltLength != 0 && (p.SaltLength < 8 || p.SaltLength > 64) {
		return errors.New("invalid salt length")
	}
	if p.KeyLength != 0 && (p.KeyLength < 16 || p.KeyLength > 64) {
		return errors.New("invalid key length")
	}
	return nil
}
