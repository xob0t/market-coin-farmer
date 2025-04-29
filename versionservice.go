package main

import (
	"embed"
	"encoding/json"
)

//go:embed build/windows/info.json
var content embed.FS

type VersionService struct{}

type VersionInfo struct {
	Fixed struct {
		FileVersion string `json:"file_version"`
	} `json:"fixed"`
	Info struct {
		Zero struct {
			ProductVersion string `json:"ProductVersion"`
		} `json:"0000"`
	} `json:"info"`
}

func (v *VersionService) GetVersion() (string, error) {
	// Read the embedded file
	data, err := content.ReadFile("build/windows/info.json")
	if err != nil {
		return "", err
	}

	// Parse the JSON
	var versionInfo VersionInfo
	err = json.Unmarshal(data, &versionInfo)
	if err != nil {
		return "", err
	}

	// Return the version from either location (they should be the same)
	// Prefer the one in "fixed" as it seems to be the primary location
	return versionInfo.Fixed.FileVersion, nil
}
