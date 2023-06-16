package urlencoding

import (
	"fmt"
	"log"
	"testing"
)

type Boo struct {
	a *int `url:"a,omitempty"`
}

func TestEncoding(t *testing.T) {
	zero := 0
	b := Boo{a: &zero}

	data, err := Marshal(b)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(data)
}
