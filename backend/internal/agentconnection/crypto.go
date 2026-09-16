package agentconnection

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"strings"
)

var envelopePrefix = []byte{'e', 'p', 'c', 1}

type Cipher struct{ aead cipher.AEAD }

func NewCipher(encodedKey string) (*Cipher, error) {
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encodedKey))
	if err != nil || len(key) != 32 {
		return nil, errors.New("AGENT_CREDENTIAL_ENCRYPTION_KEY must be standard-base64 encoding of exactly 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, errors.New("initialize agent credential encryption")
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, errors.New("initialize agent credential encryption")
	}
	return &Cipher{aead: aead}, nil
}

func (c *Cipher) Encrypt(secret string) ([]byte, error) {
	if c == nil || c.aead == nil || secret == "" {
		return nil, errors.New("credential encryption requires a nonempty secret")
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, errors.New("generate credential nonce")
	}
	out := append(append([]byte(nil), envelopePrefix...), nonce...)
	out = c.aead.Seal(out, nonce, []byte(secret), envelopePrefix)
	return out, nil
}

func (c *Cipher) Decrypt(envelope []byte) (string, error) {
	if c == nil || c.aead == nil || len(envelope) < len(envelopePrefix)+c.aead.NonceSize()+c.aead.Overhead() {
		return "", errors.New("invalid credential envelope")
	}
	for i := range envelopePrefix {
		if envelope[i] != envelopePrefix[i] {
			return "", errors.New("unsupported credential envelope version")
		}
	}
	nonceEnd := len(envelopePrefix) + c.aead.NonceSize()
	plain, err := c.aead.Open(nil, envelope[len(envelopePrefix):nonceEnd], envelope[nonceEnd:], envelopePrefix)
	if err != nil {
		return "", errors.New("decrypt credential: authentication failed")
	}
	return string(plain), nil
}
