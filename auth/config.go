package auth

// Config configures an Authenticator.
type Config struct {
	// Verifiers maps each security scheme name to the function that validates
	// that scheme's credential. A route declaring openapi.Security("x") is
	// enforced by Verifiers["x"]; a scheme with no registered verifier can never
	// be satisfied (401).
	Verifiers map[string]Verifier
}
