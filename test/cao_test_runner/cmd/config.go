package cmd

const (
	scenarioKey             = "scenario"
	outputPathKey           = "output"
	triggerLogCollectionKey = "triggerLogCollection"
	scenarioTags            = "tags"
)

func buildConfigurator() (*RootConfig, error) {
	rootCfg, err := buildRootConfig()
	if err != nil {
		return nil, err
	}

	return rootCfg, err
}
