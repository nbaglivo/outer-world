package account

import (
	"errors"
	"fmt"

	"github.com/zalando/go-keyring"
)

// Secret is a single string kept in the OS keychain as its own item.
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

// Set stores v, replacing any previous value.
func (s Secret) Set(v string) error {
	if err := keyring.Set(s.Service, s.Name, v); err != nil {
		return fmt.Errorf("save %s: %w", s.Name, err)
	}
	return nil
}
