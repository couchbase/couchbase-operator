package oldvalidators

import "github.com/sirupsen/logrus"

func handlePanic() {
	if a := recover(); a != nil {
		logrus.Warn("recover", a)
	}
}
