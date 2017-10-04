package retryutil

// logging for retry util
import (
	"github.com/sirupsen/logrus"
)

func Log(clusterName string) *logrus.Entry {
	return logrus.WithFields(logrus.Fields{
		"module":       "retryutil",
		"cluster-name": clusterName,
	})
}
