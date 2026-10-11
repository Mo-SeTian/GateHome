package cmn

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"golang.org/x/crypto/bcrypt"
	"strings"
)

// New passwords use a random salt and an adaptive work factor.
func HashPassword(password string) (string, error) {
	if len(password) < 8 || len(password) > 72 {
		return "", errors.New("密码长度须为 8–72 字节")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(hash), err
}
func VerifyPassword(hash, password string) bool {
	if strings.HasPrefix(hash, "bcrypt-sha256:") {
		digest := sha256.Sum256([]byte(password))
		return bcrypt.CompareHashAndPassword([]byte(strings.TrimPrefix(hash, "bcrypt-sha256:")), digest[:]) == nil
	}
	if strings.HasPrefix(hash, "$2") {
		if len(password) > 72 {
			return false
		}
		return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
	}
	if !LegacyPassword(hash) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(hash), []byte(PasswordEncryption(password))) == 1
}
func LegacyPassword(hash string) bool {
	_, err := hex.DecodeString(hash)
	return len(hash) == 32 && err == nil
}

// Old versions accepted up to 50 Unicode characters, which can exceed bcrypt's byte limit.
func UpgradeLegacyPassword(password string) (string, error) {
	input := []byte(password)
	prefix := ""
	if len(input) > 72 {
		digest := sha256.Sum256(input)
		input = digest[:]
		prefix = "bcrypt-sha256:"
	}
	hash, err := bcrypt.GenerateFromPassword(input, bcrypt.DefaultCost)
	return prefix + string(hash), err
}
