package logutil

import (
	"sync"

	"github.com/sirupsen/logrus"
)

var lock sync.Mutex = sync.Mutex{}

func LogLevel() logrus.Level {
	lock.Lock()
	defer lock.Unlock()
	return logrus.GetLevel()
}

func SetLogLevel(level logrus.Level) {
	lock.Lock()
	defer lock.Unlock()
	logrus.SetLevel(level)
}
