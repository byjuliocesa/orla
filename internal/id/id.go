// Package id makes the identifiers Orla hands out: a type prefix and a ULID, so ids say what
// they name and sort by creation time.
package id

import (
	"crypto/rand"

	"github.com/oklog/ulid/v2"
)

func New(prefix string) string {
	return prefix + "_" + ulid.MustNew(ulid.Now(), rand.Reader).String()
}
