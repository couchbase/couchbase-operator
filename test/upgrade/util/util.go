package util

import (
	"testing"

	"github.com/sirupsen/logrus"
)

func Assert(t *testing.T, err error) {
	if err != nil {
		logrus.Error(err)
		t.Fatal(err)
	}
}
