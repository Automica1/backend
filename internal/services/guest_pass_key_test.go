package services

import (
	"regexp"
	"strings"
	"testing"

	"chi-mongo-backend/internal/models"
)

var guestPassKeyPattern = regexp.MustCompile(
	`^[a-z]+-[a-z]+-[a-z]+-[abcdefghjkmnpqrstuvwxyz23456789]{8}-[abcdefghjkmnpqrstuvwxyz23456789]{8}$`,
)

func TestGenerateGuestPassKeyFormat(t *testing.T) {
	s := &guestPassService{}

	plaintext, keyHash, keyPrefix, err := s.generateGuestPassKey()
	if err != nil {
		t.Fatalf("generateGuestPassKey error: %v", err)
	}

	if !guestPassKeyPattern.MatchString(plaintext) {
		t.Fatalf("key %q does not match expected format word-word-word-xxxxxxxx-xxxxxxxx", plaintext)
	}

	if got := models.NormalizeGuestPassKey(plaintext); got != plaintext {
		t.Fatalf("key must survive normalization: %q != %q", got, plaintext)
	}

	if keyHash != s.hashGuestPassKey(plaintext) {
		t.Fatal("keyHash does not match hash of plaintext")
	}

	parts := strings.Split(plaintext, "-")
	wantPrefix := parts[0] + "-" + parts[1] + "-…"
	if keyPrefix != wantPrefix {
		t.Fatalf("keyPrefix = %q, want %q", keyPrefix, wantPrefix)
	}

	// The prefix shown in admin listings must not reveal the secret portion.
	if strings.Contains(keyPrefix, parts[3]) || strings.Contains(keyPrefix, parts[4]) {
		t.Fatalf("keyPrefix %q leaks secret segment", keyPrefix)
	}
}

func TestGenerateGuestPassKeyUniqueness(t *testing.T) {
	s := &guestPassService{}
	seen := make(map[string]bool, 1000)

	for i := 0; i < 1000; i++ {
		plaintext, _, _, err := s.generateGuestPassKey()
		if err != nil {
			t.Fatalf("generateGuestPassKey error: %v", err)
		}
		if seen[plaintext] {
			t.Fatalf("duplicate key generated after %d iterations: %q", i, plaintext)
		}
		seen[plaintext] = true
	}
}

func TestRandomGuestPassSecretEntropy(t *testing.T) {
	secret, err := randomGuestPassSecret(guestPassSecretLength)
	if err != nil {
		t.Fatalf("randomGuestPassSecret error: %v", err)
	}
	if len(secret) != guestPassSecretLength {
		t.Fatalf("secret length = %d, want %d", len(secret), guestPassSecretLength)
	}
	for _, c := range secret {
		if !strings.ContainsRune(guestPassSecretAlphabet, c) {
			t.Fatalf("secret %q contains character outside alphabet", secret)
		}
	}

	// Across many samples, the secret alphabet should be broadly exercised;
	// a tiny observed character set would indicate broken randomness.
	chars := make(map[byte]bool)
	for i := 0; i < 64; i++ {
		s, err := randomGuestPassSecret(guestPassSecretLength)
		if err != nil {
			t.Fatalf("randomGuestPassSecret error: %v", err)
		}
		for j := 0; j < len(s); j++ {
			chars[s[j]] = true
		}
	}
	if len(chars) < len(guestPassSecretAlphabet)/2 {
		t.Fatalf("only %d distinct characters seen across samples; randomness looks broken", len(chars))
	}
}
