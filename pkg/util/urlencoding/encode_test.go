package urlencoding

import (
	"log"
	"testing"
)

type Boo struct {
	a *int `url:"a,omitempty"`
}

func TestEncoding(_ *testing.T) {
	zero := 0
	b := Boo{a: &zero}

	_, err := Marshal(b)
	if err != nil {
		log.Fatal(err)
	}
}
