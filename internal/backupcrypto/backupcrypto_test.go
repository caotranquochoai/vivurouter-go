package backupcrypto

import (
	"bytes"
	"testing"
)

func TestEncryptDecryptRoundTrip(t *testing.T) {
	plain := []byte(`{"access_token":"secret","refresh_token":"refresh"}`)
	one, err := Encrypt(plain, "correct horse battery staple")
	if err != nil { t.Fatal(err) }
	two, err := Encrypt(plain, "correct horse battery staple")
	if err != nil { t.Fatal(err) }
	if one.Ciphertext == two.Ciphertext || one.KDF.Salt == two.KDF.Salt || one.Cipher.Nonce == two.Cipher.Nonce { t.Fatal("encryption must use fresh random salt and nonce") }
	got, err := Decrypt(one, "correct horse battery staple")
	if err != nil { t.Fatal(err) }
	if !bytes.Equal(got, plain) { t.Fatalf("plaintext = %q", got) }
}

func TestDecryptRejectsWrongPassphraseAndTampering(t *testing.T) {
	envelope, err := Encrypt([]byte("private credential payload"), "passphrase")
	if err != nil { t.Fatal(err) }
	if _, err := Decrypt(envelope, "wrong"); err == nil { t.Fatal("wrong passphrase accepted") }
	envelope.Ciphertext = envelope.Ciphertext[:len(envelope.Ciphertext)-1] + "A"
	if _, err := Decrypt(envelope, "passphrase"); err == nil { t.Fatal("tampered ciphertext accepted") }
}
