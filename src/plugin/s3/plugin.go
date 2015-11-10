package main

import (
	"fmt"
	"plugin"
)

func main() {
	p := S3Plugin{
		Name:    "S3 Backup + Storage Plugin",
		Author:  "Stark & Wayne",
		Version: "0.0.1",
		Features: plugin.PluginFeatures{
			Target: "yes",
			Store:  "yes",
		},
	}

	plugin.Run(p)
}

type S3Plugin plugin.PluginInfo

func (p S3Plugin) Meta() plugin.PluginInfo {
	return plugin.PluginInfo(p)
}

func (p S3Plugin) Backup(endpoint plugin.ShieldEndpoint) (int, error) {
	return plugin.UNSUPPORTED_ACTION, fmt.Errorf("Not yet implemented")
}

func (p S3Plugin) Restore(endpoint plugin.ShieldEndpoint) (int, error) {
	return plugin.UNSUPPORTED_ACTION, fmt.Errorf("Not yet implemented")
}

func (p S3Plugin) Store(endpoint plugin.ShieldEndpoint) (string, int, error) {
	return "", plugin.UNSUPPORTED_ACTION, fmt.Errorf("Not yet implemented")
}

func (p S3Plugin) Retrieve(endpoint plugin.ShieldEndpoint, file string) (int, error) {
	return plugin.UNSUPPORTED_ACTION, fmt.Errorf("Not yet implemented")
}

func (p S3Plugin) Purge(endpoint plugin.ShieldEndpoint, file string) (int, error) {
	return plugin.UNSUPPORTED_ACTION, fmt.Errorf("Not yet implemented")
}