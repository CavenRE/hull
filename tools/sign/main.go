// Command sign makes and checks the ed25519 signature on a release's
// SHA256SUMS:
//
//	go run ./tools/sign genkey        print a new key pair
//	go run ./tools/sign sign FILE     write FILE.sig with the key in $HULL_SIGNING_KEY
//	go run ./tools/sign verify FILE   check FILE.sig against version.ReleaseKey
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/CavenRE/hull/internal/version"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "sign:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	switch {
	case len(args) == 1 && args[0] == "genkey":
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return err
		}
		fmt.Println("Public key, for ReleaseKey in internal/version/release_key.go:")
		fmt.Println("  " + base64.StdEncoding.EncodeToString(pub))
		fmt.Println("Private seed, for the HULL_SIGNING_KEY repository secret only:")
		fmt.Println("  " + base64.StdEncoding.EncodeToString(priv.Seed()))
		return nil
	case len(args) == 2 && args[0] == "sign":
		seed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(os.Getenv("HULL_SIGNING_KEY")))
		if err != nil || len(seed) != ed25519.SeedSize {
			return errors.New("HULL_SIGNING_KEY must hold the base64 private seed from genkey")
		}
		data, err := os.ReadFile(args[1])
		if err != nil {
			return err
		}
		sig := ed25519.Sign(ed25519.NewKeyFromSeed(seed), data)
		return os.WriteFile(args[1]+".sig", []byte(base64.StdEncoding.EncodeToString(sig)+"\n"), 0o644)
	case len(args) == 2 && args[0] == "verify":
		key, err := base64.StdEncoding.DecodeString(version.ReleaseKey)
		if err != nil || len(key) != ed25519.PublicKeySize {
			return errors.New("version.ReleaseKey is not set; run genkey and paste the public key into internal/version/release_key.go")
		}
		data, err := os.ReadFile(args[1])
		if err != nil {
			return err
		}
		sigText, err := os.ReadFile(args[1] + ".sig")
		if err != nil {
			return err
		}
		sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sigText)))
		if err != nil || !ed25519.Verify(key, data, sig) {
			return fmt.Errorf("%s.sig does not verify against version.ReleaseKey", args[1])
		}
		fmt.Println("signature OK")
		return nil
	}
	return errors.New("usage: sign genkey | sign sign FILE | sign verify FILE")
}
