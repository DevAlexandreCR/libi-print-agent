package config

// PlainProtector is an identity Protector: it stores the token unencrypted.
// It exists only for unit tests and non-Windows local development, where
// DPAPI is unavailable — never use it to run a paired agent for real.
type PlainProtector struct{}

func (PlainProtector) Protect(data []byte) ([]byte, error) {
	out := make([]byte, len(data))
	copy(out, data)
	return out, nil
}

func (PlainProtector) Unprotect(data []byte) ([]byte, error) {
	out := make([]byte, len(data))
	copy(out, data)
	return out, nil
}
