package backupcrypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"

	"golang.org/x/crypto/argon2"
)

const (
	Kind             = "vivurouter.encrypted-account-backup"
	Version          = 1
	maxPlaintextLen  = 4 << 20
	maxCiphertextLen = maxPlaintextLen + 1024
)

var errInvalidBundle = errors.New("invalid encrypted backup or passphrase")

type KDF struct {
	Name        string `json:"name"`
	MemoryKiB   uint32 `json:"memory_kib"`
	Iterations  uint32 `json:"iterations"`
	Parallelism uint8  `json:"parallelism"`
	Salt        string `json:"salt"`
}

type Cipher struct {
	Name  string `json:"name"`
	Nonce string `json:"nonce"`
}

type Envelope struct {
	Kind       string `json:"kind"`
	Version    int    `json:"version"`
	KDF        KDF    `json:"kdf"`
	Cipher     Cipher `json:"cipher"`
	Ciphertext string `json:"ciphertext"`
}

func Encrypt(plaintext []byte, passphrase string) (Envelope, error) {
	if len(plaintext) == 0 || len(plaintext) > maxPlaintextLen || passphrase == "" {
		return Envelope{}, errInvalidBundle
	}
	salt := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return Envelope{}, err
	}
	params := KDF{Name: "argon2id", MemoryKiB: 65536, Iterations: 3, Parallelism: 2, Salt: base64.StdEncoding.EncodeToString(salt)}
	key := argon2.IDKey([]byte(passphrase), salt, params.Iterations, params.MemoryKiB, params.Parallelism, 32)
	defer clear(key)
	block, err := aes.NewCipher(key)
	if err != nil {
		return Envelope{}, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return Envelope{}, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return Envelope{}, err
	}
	envelope := Envelope{Kind: Kind, Version: Version, KDF: params, Cipher: Cipher{Name: "aes-256-gcm", Nonce: base64.StdEncoding.EncodeToString(nonce)}}
	sealed := gcm.Seal(nil, nonce, plaintext, aad(envelope.Kind, envelope.Version))
	envelope.Ciphertext = base64.StdEncoding.EncodeToString(sealed)
	return envelope, nil
}

func Decrypt(envelope Envelope, passphrase string) ([]byte, error) {
	if envelope.Kind != Kind || envelope.Version != Version || envelope.KDF.Name != "argon2id" || envelope.KDF.MemoryKiB != 65536 || envelope.KDF.Iterations != 3 || envelope.KDF.Parallelism != 2 || envelope.Cipher.Name != "aes-256-gcm" || passphrase == "" {
		return nil, errInvalidBundle
	}
	salt, err := base64.StdEncoding.DecodeString(envelope.KDF.Salt)
	if err != nil || len(salt) != 16 {
		return nil, errInvalidBundle
	}
	nonce, err := base64.StdEncoding.DecodeString(envelope.Cipher.Nonce)
	if err != nil || len(nonce) != 12 {
		return nil, errInvalidBundle
	}
	ciphertext, err := base64.StdEncoding.DecodeString(envelope.Ciphertext)
	if err != nil || len(ciphertext) < 16 || len(ciphertext) > maxCiphertextLen {
		return nil, errInvalidBundle
	}
	key := argon2.IDKey([]byte(passphrase), salt, envelope.KDF.Iterations, envelope.KDF.MemoryKiB, envelope.KDF.Parallelism, 32)
	defer clear(key)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, errInvalidBundle
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil || len(nonce) != gcm.NonceSize() {
		return nil, errInvalidBundle
	}
	plaintext, err := gcm.Open(nil, nonce, ciphertext, aad(envelope.Kind, envelope.Version))
	if err != nil || len(plaintext) == 0 || len(plaintext) > maxPlaintextLen {
		return nil, errInvalidBundle
	}
	return plaintext, nil
}

func Marshal(envelope Envelope) ([]byte, error) { return json.Marshal(envelope) }

func Parse(data []byte) (Envelope, error) {
	if len(data) == 0 || len(data) > maxCiphertextLen*2 {
		return Envelope{}, errInvalidBundle
	}
	var envelope Envelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return Envelope{}, errInvalidBundle
	}
	return envelope, nil
}

func aad(kind string, version int) []byte { return []byte(kind + ":" + string(rune(version))) }
func clear(data []byte) {
	for i := range data {
		data[i] = 0
	}
}
