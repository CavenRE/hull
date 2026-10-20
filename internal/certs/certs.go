package certs

import (
	"crypto/x509"
	"fmt"
	"os"
	"path/filepath"

	"github.com/smallstep/truststore"
)

// RootCertPath is where the embedded router's local CA root lives inside
// the router data directory (Caddy PKI layout).
func RootCertPath(routerDataDir string) string {
	return filepath.Join(routerDataDir, "pki", "authorities", "local", "root.crt")
}

// InstallTrust installs Hull's root certificate into the system trust
// store (and Firefox's NSS store where applicable) , the mkcert approach,
// via smallstep/truststore. On Windows this triggers the standard
// certificate-confirmation dialog; on unix it may require sudo.
func InstallTrust(routerDataDir string) error {
	path := RootCertPath(routerDataDir)
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("root certificate not found at %s , run `hull daemon run` once (or `hull trust` provisions it) so the CA exists", path)
	}
	if err := truststore.InstallFile(path); err != nil {
		return fmt.Errorf("installing trust: %w", err)
	}
	// Best effort for Firefox; ignore when not present.
	_ = truststore.InstallFile(path, truststore.WithFirefox())
	return nil
}

// UninstallTrust removes Hull's root certificate from the trust stores.
func UninstallTrust(routerDataDir string) error {
	path := RootCertPath(routerDataDir)
	if _, err := os.Stat(path); err != nil {
		return nil // nothing installed from this machine's CA
	}
	if err := truststore.UninstallFile(path); err != nil {
		return fmt.Errorf("removing trust: %w", err)
	}
	_ = truststore.UninstallFile(path, truststore.WithFirefox())
	return nil
}

// RootPool returns a certificate pool holding only Hull's local CA root, for
// clients that should trust the router and nothing else.
func RootPool(routerDataDir string) (*x509.CertPool, error) {
	data, err := os.ReadFile(RootCertPath(routerDataDir))
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(data) {
		return nil, fmt.Errorf("no certificate in %s", RootCertPath(routerDataDir))
	}
	return pool, nil
}

// Trusted reports whether the root certificate file exists (presence in
// the OS store is checked by the browser; doctor reports the file +
// install state guidance).
func Trusted(routerDataDir string) bool {
	_, err := os.Stat(RootCertPath(routerDataDir))
	return err == nil
}
