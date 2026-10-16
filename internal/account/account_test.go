package account

import (
	"slices"
	"testing"

	"github.com/zalando/go-keyring"

	"nmail/internal/mailbox"
)

func TestSaveLoadForget(t *testing.T) {
	keyring.MockInit()
	var k Keyring

	if list, err := k.Load(); len(list) != 0 || err != nil {
		t.Fatalf("empty keyring: %v %v", list, err)
	}

	want := []mailbox.Credentials{
		{Host: "imap.example.com", Port: "993", Username: "me", Password: "p\"w"},
		{Host: "imap.other.com", Port: "993", Username: "work", Password: "x"},
	}
	if err := k.Save(want); err != nil {
		t.Fatal(err)
	}
	got, err := k.Load()
	if err != nil || !slices.Equal(got, want) {
		t.Fatalf("got %+v err=%v", got, err)
	}

	for range 2 {
		if err := k.Save(nil); err != nil {
			t.Fatal(err)
		}
	}
	if list, _ := k.Load(); len(list) != 0 {
		t.Fatal("logins still saved after clearing")
	}
}

func TestLoadSingleLoginFormat(t *testing.T) {
	keyring.MockInit()
	keyring.Set(defaultService, user, `{"Host":"h","Port":"993","Username":"u","Password":"pw"}`)
	got, err := Keyring{}.Load()
	want := []mailbox.Credentials{{Host: "h", Port: "993", Username: "u", Password: "pw"}}
	if err != nil || !slices.Equal(got, want) {
		t.Fatalf("got %+v err=%v", got, err)
	}
}

func TestServicesAreSeparate(t *testing.T) {
	keyring.MockInit()
	nm, sip := Keyring{}, Keyring{Service: "sip"}
	a := []mailbox.Credentials{{Host: "h", Port: "993", Username: "me", Password: "a"}}
	if err := sip.Save(a); err != nil {
		t.Fatal(err)
	}
	if list, _ := nm.Load(); len(list) != 0 {
		t.Fatalf("nmail sees sip's logins: %+v", list)
	}
	if got, _ := (Keyring{Service: "nmail"}).Load(); len(got) != 0 {
		t.Fatal("explicit nmail service differs from the default")
	}
	if got, _ := sip.Load(); !slices.Equal(got, a) {
		t.Fatalf("sip load = %+v", got)
	}
}
