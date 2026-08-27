package main

import (
	"encoding/json"
	"os"
	"time"
)

// Same file, same schema, as CloudKeyWizard.exe's Services/DeviceStateStore.cs -- both apps read
// and write this exact path so "what's installed" stays in sync regardless of which one someone
// happens to be using. See that file's doc comment for the full rationale.
const deviceStateFile = "/root/.cloudkey-wizard/fdtscout-state.json"

type DeviceState struct {
	ExtraStatuses   map[string]string `json:"extraStatuses"`
	ExtraDetails    map[string]string `json:"extraDetails"`
	DetectedModel   string            `json:"detectedModel"`
	LastUpdatedBy   string            `json:"lastUpdatedBy"`
	LastUpdatedAt   time.Time         `json:"lastUpdatedAt"`
}

func readDeviceState() (*DeviceState, error) {
	data, err := os.ReadFile(deviceStateFile)
	if err != nil {
		if os.IsNotExist(err) {
			return &DeviceState{ExtraStatuses: map[string]string{}, ExtraDetails: map[string]string{}}, nil
		}
		return nil, err
	}
	var state DeviceState
	if err := json.Unmarshal(data, &state); err != nil {
		return &DeviceState{ExtraStatuses: map[string]string{}, ExtraDetails: map[string]string{}}, nil
	}
	if state.ExtraStatuses == nil {
		state.ExtraStatuses = map[string]string{}
	}
	if state.ExtraDetails == nil {
		state.ExtraDetails = map[string]string{}
	}
	return &state, nil
}

// setAppStatus records one app's install status into the shared state file -- called whenever
// FDT.Scout's own Apps tab installs, removes, or otherwise changes something CloudKey Wizard also
// knows about as an Extra, so a later CloudKey Wizard session sees it without re-detecting.
func setAppStatus(id, status, detail string) error {
	state, err := readDeviceState()
	if err != nil {
		return err
	}
	state.ExtraStatuses[id] = status
	state.ExtraDetails[id] = detail
	state.LastUpdatedBy = "FDT.Scout " + Version
	state.LastUpdatedAt = time.Now().UTC()

	hostname, _ := os.Hostname()
	if state.DetectedModel == "" {
		state.DetectedModel = hostname
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	tmp := deviceStateFile + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, deviceStateFile)
}
