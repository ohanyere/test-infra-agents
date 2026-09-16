package auth

import (
	"strings"
	"testing"
)

var testPasswordParams = PasswordParams{Memory: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}

func TestPasswordHashAndVerify(t *testing.T) {
	encoded, err := HashPassword("correct horse battery staple", testPasswordParams)
	if err != nil {
		t.Fatal(err)
	}
	ok, err := VerifyPassword("correct horse battery staple", encoded)
	if err != nil || !ok {
		t.Fatalf("verify ok=%v err=%v", ok, err)
	}
	ok, err = VerifyPassword("wrong password", encoded)
	if err != nil || ok {
		t.Fatalf("wrong verify ok=%v err=%v", ok, err)
	}
}
func TestPasswordHashesUseDistinctSalts(t *testing.T) {
	a, _ := HashPassword("correct horse battery staple", testPasswordParams)
	b, _ := HashPassword("correct horse battery staple", testPasswordParams)
	if a == b {
		t.Fatal("hashes unexpectedly equal")
	}
}
func TestMalformedHashesFailSafely(t *testing.T) {
	values := []string{"", "$argon2i$v=19$m=8192,t=1,p=1$bad$bad", "$argon2id$v=18$m=8192,t=1,p=1$YWJjZGVmZ2g$YWJjZGVmZ2hpamtsbW5vcA", "$argon2id$v=19$m=1,t=1,p=1$YWJjZGVmZ2g$YWJjZGVmZ2hpamtsbW5vcA"}
	for _, value := range values {
		if ok, err := VerifyPassword("anything", value); err == nil || ok {
			t.Fatalf("hash %q accepted", value)
		}
	}
}
func TestPasswordLengthBoundaries(t *testing.T) {
	if err := ValidatePassword(strings.Repeat("a", 11)); err == nil {
		t.Fatal("short password accepted")
	}
	if err := ValidatePassword(strings.Repeat("a", 12)); err != nil {
		t.Fatal(err)
	}
	if err := ValidatePassword(strings.Repeat("a", 128)); err != nil {
		t.Fatal(err)
	}
	if err := ValidatePassword(strings.Repeat("a", 129)); err == nil {
		t.Fatal("long password accepted")
	}
}
func TestSessionToken(t *testing.T) {
	raw, digest, err := GenerateSessionToken()
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) != 43 {
		t.Fatalf("encoded token length=%d", len(raw))
	}
	if len(digest) != 32 {
		t.Fatalf("digest length=%d", len(digest))
	}
	if string(digest) == raw {
		t.Fatal("raw token stored as digest")
	}
	other, _, _ := GenerateSessionToken()
	if raw == other {
		t.Fatal("tokens unexpectedly equal")
	}
}
