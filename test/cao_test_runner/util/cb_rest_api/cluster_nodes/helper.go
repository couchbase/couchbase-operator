package clusternodes

import (
	"strings"
)

func ConvertToOTPNames(cbPodNames []string) []string {
	for i, cbPodName := range cbPodNames {
		if !strings.HasPrefix(cbPodName, "ns_1@") {
			cbPodNames[i] = "ns_1@" + cbPodNames[i]
		}
	}

	return cbPodNames
}
