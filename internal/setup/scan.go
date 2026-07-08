package setup

func scanClients(home string, configPaths []string) []ClientAdapter {
	adapters := allAdapters(home)
	if len(configPaths) > 0 {
		adapters = make([]ClientAdapter, 0, len(configPaths))
		for _, path := range configPaths {
			adapters = append(adapters, newExplicitConfigAdapter(path))
		}
	}
	installed := make([]ClientAdapter, 0, len(adapters))
	for _, adapter := range adapters {
		if adapter.Installed() {
			installed = append(installed, adapter)
		}
	}
	return installed
}
