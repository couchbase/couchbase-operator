package jsonpatch_test

import (
	"fmt"

	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
)

func ExampleApply() {
	s := struct {
		Str string
		Arr []int
		Map map[string]string
	}{
		Str: "test1",
		Arr: []int{1, 2, 3},
		Map: map[string]string{
			"dog": "woof",
			"cat": "meow",
		},
	}

	patchset := jsonpatch.NewPatchSet().
		Remove("/Arr/1").
		Add("/Arr/-", 7).
		Replace("/Str", "test2").
		Test("/Map/dog", "woof").
		Remove("/Map/cat")

	if err := jsonpatch.Apply(&s, patchset.Patches()); err != nil {
		fmt.Println(err)
		return
	}

	fmt.Println(s)

	// Output:
	// {test2 [1 3 7] map[dog:woof]}
}
