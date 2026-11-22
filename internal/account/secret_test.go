package account

import (
	"testing"

	"github.com/zalando/go-keyring"

	"nmail/internal/mailbox"
)

func TestSecretRoundTrip(t *testing.T) {
	keyring.MockInit()
	s := Secret{Service: "sip", Name: "mastodon-token"}
	if v, err := s.Get(); err != nil || v != "" {
		t.Fatalf("empty Get = %q, %v", v, err)
	}
	if err := s.Set("tok"); err != nil {
		t.Fatal(err)
	}
	if v, _ := s.Get(); v != "tok" {
		t.Fatalf("Get = %q, want tok", v)
	}
}

func TestSecretLeavesLoginsAlone(t *testing.T) {
	keyring.MockInit()
	k := Keyring{Service: "sip"}
	k.Save([]mailbox.Credentials{{Host: "h", Username: "u", Password: "pw"}})
	Secret{Service: "sip", Name: "mastodon-token"}.Set("tok")
	if list, err := k.Load(); err != nil || len(list) != 1 || list[0].Password != "pw" {
		t.Fatalf("logins = %+v, %v", list, err)
	}
}
