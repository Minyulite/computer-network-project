package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

const passwordMemory = 19 * 1024
const passwordIterations = 2
const passwordLanes = 1

var errInvalidHash = errors.New("invalid password hash")

func validPassword(password string) bool {
	if !utf8.ValidString(password) || len(password) > 512 {
		return false
	}
	count := utf8.RuneCountInString(password)
	return count >= 15 && count <= 128
}

func hashPassword(password string) (string, error) {
	var salt [16]byte
	if _, err := rand.Read(salt[:]); err != nil {
		return "", err
	}
	hash := argon2.IDKey([]byte(password), salt[:], passwordIterations, passwordMemory, passwordLanes, 32)
	return "$argon2id$v=19$m=19456,t=2,p=1$" +
		base64.RawStdEncoding.EncodeToString(salt[:]) + "$" + base64.RawStdEncoding.EncodeToString(hash), nil
}

func verifyPassword(encoded, password string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" || parts[2] != "v=19" ||
		parts[3] != "m=19456,t=2,p=1" {
		return false, errInvalidHash
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) != 16 {
		return false, errInvalidHash
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(expected) != 32 {
		return false, errInvalidHash
	}
	actual := argon2.IDKey([]byte(password), salt, passwordIterations, passwordMemory, passwordLanes, 32)
	return subtle.ConstantTimeCompare(actual, expected) == 1, nil
}
