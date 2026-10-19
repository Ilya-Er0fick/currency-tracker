package auth

import (
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestHashPasswordVerifies(t *testing.T) {
	hash, err := HashPassword("correct horse battery")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	if strings.Contains(hash, "correct horse battery") {
		t.Fatal("the hash must not contain the password itself")
	}
	if !CheckPassword(hash, "correct horse battery") {
		t.Error("the correct password was rejected")
	}
	if CheckPassword(hash, "wrong password") {
		t.Error("a wrong password was accepted")
	}
}

func TestHashPasswordIsSaltedPerCall(t *testing.T) {
	first, _ := HashPassword("same password")
	second, _ := HashPassword("same password")

	// Одинаковые пароли дают разные хеши: иначе по совпадению хешей было бы
	// видно, что два пользователя выбрали один и тот же пароль.
	if first == second {
		t.Error("two hashes of the same password must differ")
	}
	if !CheckPassword(second, "same password") {
		t.Error("the second hash does not verify")
	}
}

func TestIssueAndParse(t *testing.T) {
	issuer := NewTokenIssuer(strings.Repeat("s", 32), time.Hour)

	token, expires, err := issuer.Issue(42)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if !expires.After(time.Now()) {
		t.Error("the expiry must be in the future")
	}

	userID, err := issuer.Parse(token)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if userID != 42 {
		t.Errorf("user id = %d, want 42", userID)
	}
}

func TestParseRejectsBadTokens(t *testing.T) {
	secret := strings.Repeat("s", 32)
	issuer := NewTokenIssuer(secret, time.Hour)

	valid, _, err := issuer.Issue(7)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	expiredIssuer := NewTokenIssuer(secret, -time.Minute)
	expired, _, err := expiredIssuer.Issue(7)
	if err != nil {
		t.Fatalf("Issue expired: %v", err)
	}

	// Токен без подписи: если метод подписи не проверять, он пройдёт как валидный.
	unsigned, err := jwt.NewWithClaims(jwt.SigningMethodNone, jwt.RegisteredClaims{
		Subject:   "7",
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	}).SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("build unsigned token: %v", err)
	}

	foreign, _, err := NewTokenIssuer(strings.Repeat("x", 32), time.Hour).Issue(7)
	if err != nil {
		t.Fatalf("Issue foreign: %v", err)
	}

	tests := []struct {
		name  string
		token string
	}{
		{name: "empty", token: ""},
		{name: "not a token", token: "abc.def.ghi"},
		{name: "expired", token: expired},
		{name: "signed by another secret", token: foreign},
		{name: "unsigned", token: unsigned},
		{name: "tampered payload", token: valid[:len(valid)-3] + "aaa"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := issuer.Parse(tt.token); err == nil {
				t.Error("the token was accepted but must be rejected")
			}
		})
	}
}
