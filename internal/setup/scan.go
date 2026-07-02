package setup

func scanClients(home string) []ClientAdapter {
	adapters := allAdapters(home)
	installed := make([]ClientAdapter, 0, len(adapters))
	for _, adapter := range adapters {
		if adapter.Installed() {
			installed = append(installed, adapter)
		}
	}
	return installed
}
