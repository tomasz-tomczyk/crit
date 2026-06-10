package server

// IntegrationStatus describes a detected agent integration and whether it is current.
type IntegrationStatus struct {
	Agent    string `json:"agent"`
	Status   string `json:"status"`
	Location string `json:"location"`
	Hint     string `json:"hint"`
	Hash     string `json:"hash,omitempty"`
}

// AvailableIntegrationsFn returns sorted integration names for the config API.
var AvailableIntegrationsFn func() []string

// DetectInstalledIntegrationsFn scans installed integration files.
var DetectInstalledIntegrationsFn func(projectDir, homeDir string) []IntegrationStatus

func availableIntegrations() []string {
	if AvailableIntegrationsFn != nil {
		return AvailableIntegrationsFn()
	}
	return nil
}

func detectInstalledIntegrations(projectDir, homeDir string) []IntegrationStatus {
	if DetectInstalledIntegrationsFn != nil {
		return DetectInstalledIntegrationsFn(projectDir, homeDir)
	}
	return nil
}
