package version

// ReleaseKey is the base64 ed25519 public key whose private half signs each
// release's SHA256SUMS. Make the pair with `go run ./tools/sign genkey`: paste
// the public key here and store the private seed as the HULL_SIGNING_KEY
// repository secret. While it is empty, `hull update` refuses to install a
// download it cannot check, and the release workflow refuses to publish.
const ReleaseKey = "v91+k7ctXpXsa1VvbgXX8WXaKtcd9CMldO+LtupWYpI="
