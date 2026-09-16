package agentconnection

import (
	"encoding/base64"
	"testing"
)

func testCipher(t *testing.T, fill byte) *Cipher {
	t.Helper()
	key := make([]byte, 32)
	for i := range key {
		key[i] = fill
	}
	c, err := NewCipher(base64.StdEncoding.EncodeToString(key))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestCredentialEncryptionRoundTripAndRandomNonce(t *testing.T) {
	c := testCipher(t, 0)
	a, err := c.Encrypt("top-secret")
	if err != nil {
		t.Fatal(err)
	}
	b, err := c.Encrypt("top-secret")
	if err != nil {
		t.Fatal(err)
	}
	if string(a) == string(b) {
		t.Fatal("envelopes should differ")
	}
	got, err := c.Decrypt(a)
	if err != nil || got != "top-secret" {
		t.Fatalf("got=%q err=%v", got, err)
	}
}

func TestCredentialDecryptionRejectsMalformedAndWrongKey(t *testing.T) {
	a, _ := testCipher(t, 1).Encrypt("top-secret")
	if _, err := testCipher(t, 1).Decrypt([]byte("bad")); err == nil {
		t.Fatal("expected malformed failure")
	}
	if _, err := testCipher(t, 2).Decrypt(a); err == nil {
		t.Fatal("expected wrong-key failure")
	}
}

func TestCredentialKeyMustBeExactly32Bytes(t *testing.T) {
	if _, err := NewCipher(base64.StdEncoding.EncodeToString(make([]byte, 16))); err == nil {
		t.Fatal("expected invalid key")
	}
}
