package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"testing"
)

func TestCheckSums(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sums := []byte("# hull v1.2.3\nabc123  hull-linux-x64\nDEF456 *hull-windows-x64.exe\n")
	sig := []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(priv, sums)) + "\n")

	got, err := checkSums(pub, sums, sig, "v1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if got["hull-linux-x64"] != "abc123" || got["hull-windows-x64.exe"] != "def456" {
		t.Errorf("sums = %v", got)
	}

	tampered := append([]byte(nil), sums...)
	tampered[len(tampered)-3] = 'X'
	if _, err := checkSums(pub, tampered, sig, "v1.2.3"); err == nil {
		t.Error("accepted checksums that changed after signing")
	}
	other, _, _ := ed25519.GenerateKey(rand.Reader)
	if _, err := checkSums(other, sums, sig, "v1.2.3"); err == nil {
		t.Error("accepted a signature from another key")
	}
	if _, err := checkSums(pub, sums, sig, "v9.9.9"); err == nil {
		t.Error("accepted an old signed release replayed under a newer tag")
	}
}
