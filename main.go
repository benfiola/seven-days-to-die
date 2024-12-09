package main

import (
	"os"
	"strings"

	"encoding/xml"
)

type serverSettings struct {
	XMLName    xml.Name   `xml:"ServerSettings"`
	Properties []property `xml:"property"`
}

type property struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}

func generateServerSettings() error {
	// build server settings from defaults
	defaultBytes, err := os.ReadFile("/server/serverconfig.xml")
	if err != nil {
		return err
	}
	defaults := serverSettings{}
	err = xml.Unmarshal(defaultBytes, &defaults)
	if err != nil {
		return err
	}
	settings := map[string]string{}
	for _, prop := range defaults.Properties {
		settings[prop.Name] = prop.Value
	}

	// add environment overrides
	prefix := "SDTD_"
	for _, envString := range os.Environ() {
		envSlice := strings.SplitN(envString, "=", 2)
		if !strings.HasPrefix(envSlice[0], prefix) {
			continue
		}
		setting := strings.TrimPrefix(envSlice[0], prefix)
		settings[setting] = envSlice[1]
	}

	// add hardcoded overrides
	settings["UserDataFolder"] = "/data"

	// convert into xml
	serverSettings := serverSettings{}
	for k, v := range settings {
		serverSettings.Properties = append(serverSettings.Properties, property{Name: k, Value: v})
	}
	xmlBytes, err := xml.MarshalIndent(serverSettings, "", "  ")
	if err != nil {
		return err
	}
	xmlBytes = []byte(xml.Header + string(xmlBytes))
	return os.WriteFile("/data/serverconfig.xml", xmlBytes, 0)
}

func main() {
	generateServerSettings()
}
