// Package account remembers logins in the OS keychain.
package account

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/zalando/go-keyring"

	"nmail/internal/mailbox"
)

const (
	defaultService = "nmail"
	user           = "default"
)

// Keyring stores every remembered login as one keychain item. An empty
// Service means "nmail".
type Keyring struct{ Service string }

func (k Keyring) service() string {
	if k.Service == "" {
		return defaultService
	}
	return k.Service
}

// Load returns the remembered logins, if any.
func (k Keyring) Load() ([]mailbox.Credentials, error) {
	s, err := keyring.Get(k.service(), user)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load saved logins: %w", err)
	}
	var list []mailbox.Credentials
	if err := json.Unmarshal([]byte(s), &list); err == nil {
		return list, nil
	}
	// Older versions saved a single login.
	var c mailbox.Credentials
	if err := json.Unmarshal([]byte(s), &c); err != nil {
		return nil, fmt.Errorf("load saved logins: %w", err)
	}
	return []mailbox.Credentials{c}, nil
}

// Save replaces the remembered logins; an empty list removes the item.
func (k Keyring) Save(list []mailbox.Credentials) error {
	if len(list) == 0 {
		err := keyring.Delete(k.service(), user)
		if err != nil && !errors.Is(err, keyring.ErrNotFound) {
			return fmt.Errorf("forget logins: %w", err)
		}
		return nil
	}
	b, err := json.Marshal(list)
	if err != nil {
		return err
	}
	if err := keyring.Set(k.service(), user, string(b)); err != nil {
		return fmt.Errorf("save logins: %w", err)
	}
	return nil
}
