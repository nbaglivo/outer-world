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
	service = "nmail"
	user    = "default"
)

// Keyring stores every remembered login as one keychain item.
type Keyring struct{}

// Load returns the remembered logins, if any.
func (Keyring) Load() ([]mailbox.Credentials, error) {
	s, err := keyring.Get(service, user)
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
func (Keyring) Save(list []mailbox.Credentials) error {
	if len(list) == 0 {
		err := keyring.Delete(service, user)
		if err != nil && !errors.Is(err, keyring.ErrNotFound) {
			return fmt.Errorf("forget logins: %w", err)
		}
		return nil
	}
	b, err := json.Marshal(list)
	if err != nil {
		return err
	}
	if err := keyring.Set(service, user, string(b)); err != nil {
		return fmt.Errorf("save logins: %w", err)
	}
	return nil
}
