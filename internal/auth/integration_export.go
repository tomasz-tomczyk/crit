//go:build integration

package auth

// TokenResponse is the device-token poll result shape used by integration tests.
type TokenResponse = tokenResponse

// SaveAuthSession persists auth credentials for integration tests.
var SaveAuthSession = saveAuthSession
