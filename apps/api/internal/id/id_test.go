package id

import "testing"

func TestNewIsValidAndUnique(t *testing.T) {
	a, err := New()
	if err != nil {
		t.Fatal(err)
	}
	if !Valid(a) || a[14] != '4' {
		t.Fatalf("not a v4 UUID: %s", a)
	}
	if b := MustNew(); a == b {
		t.Fatal("two random IDs are equal")
	}
}

func TestFromSeedIsStable(t *testing.T) {
	a, b := FromSeed("bot-reply:x"), FromSeed("bot-reply:x")
	if a != b || !Valid(a) {
		t.Fatalf("%s %s", a, b)
	}
	if FromSeed("bot-reply:y") == a {
		t.Fatal("different seeds collide")
	}
}

func TestCanonical(t *testing.T) {
	if got, ok := Canonical("0F8FAD5B-D9CB-469F-A165-70867728950E"); !ok || got != "0f8fad5b-d9cb-469f-a165-70867728950e" {
		t.Fatalf("%s %v", got, ok)
	}
	for _, bad := range []string{"", "not-a-uuid", "0f8fad5bd9cb469fa16570867728950e", "0f8fad5b-d9cb-469f-a165-70867728950e "} {
		if _, ok := Canonical(bad); ok {
			t.Errorf("accepted %q", bad)
		}
	}
}
