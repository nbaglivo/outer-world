package account

import (
	"errors"
	"fmt"

	"github.com/zalando/go-keyring"
)

// Secret is a single keychain item, such as an access token.
type Secret struct{ Service, Name string }

// Get returns the stored value, or "" when nothing is stored.
func (s Secret) Get() (string, error) {
	v, err := keyring.Get(s.Service, s.Name)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("load %s: %w", s.Name, err)
	}
	return v, nil
}

// Set stores the value in the keychain.
func (s Secret) Set(v string) error {
	if err := keyring.Set(s.Service, s.Name, v); err != nil {
		return fmt.Errorf("save %s: %w", s.Name, err)
	}
	return nil
}
