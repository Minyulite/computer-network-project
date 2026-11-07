// Package avatar lists the cartoon pictures people and shop assistants can
// choose. The artwork itself lives in the web app's public/avatars folder; only
// the identifiers are validated here.
package avatar

import (
	"fmt"
	"math/rand/v2"
)

const (
	PersonCount = 12 // p01 to p12
	BotCount    = 8  // b01 to b08
)

func valid(id string, prefix byte, count int) bool {
	if len(id) != 3 || id[0] != prefix {
		return false
	}
	for _, c := range []byte{id[1], id[2]} {
		if c < '0' || c > '9' {
			return false
		}
	}
	n := int(id[1]-'0')*10 + int(id[2]-'0')
	return n >= 1 && n <= count
}

// ValidPerson reports whether id names one of the pictures for people.
func ValidPerson(id string) bool { return valid(id, 'p', PersonCount) }

// ValidBot reports whether id names one of the pictures for shop assistants.
func ValidBot(id string) bool { return valid(id, 'b', BotCount) }

// RandomPerson picks a picture for someone who did not choose one.
func RandomPerson() string { return fmt.Sprintf("p%02d", rand.IntN(PersonCount)+1) }

// RandomBot picks a picture for a shop assistant created without one.
func RandomBot() string { return fmt.Sprintf("b%02d", rand.IntN(BotCount)+1) }
