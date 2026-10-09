package memory

import "herdr-dots/internal/secrets"

// ScanSecrets reports whether text looks like it contains a credential, and
// which kind. Model-written memory that matches is refused; user-written
// memory that matches needs a confirmation. The patterns live in the leaf
// package internal/secrets (secrets.Scan), so packages memory imports, such
// as dots, can use them too.
func ScanSecrets(text string) (bool, string) { return secrets.Scan(text) }
