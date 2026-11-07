package avatar

import "testing"

func TestValidIdentifiers(t *testing.T) {
	for _, id := range []string{"p01", "p12", "p07"} {
		if !ValidPerson(id) || ValidBot(id) {
			t.Errorf("%s should be a person picture only", id)
		}
	}
	for _, id := range []string{"b01", "b08"} {
		if !ValidBot(id) || ValidPerson(id) {
			t.Errorf("%s should be a bot picture only", id)
		}
	}
	for _, id := range []string{"", "p", "p00", "p13", "b09", "p1", "p001", "px1", "P01", "../p01", "p0１"} {
		if ValidPerson(id) || ValidBot(id) {
			t.Errorf("%q was accepted", id)
		}
	}
}

func TestRandomPicksAreValid(t *testing.T) {
	for range 200 {
		if !ValidPerson(RandomPerson()) || !ValidBot(RandomBot()) {
			t.Fatal("random pick is not a valid identifier")
		}
	}
}
