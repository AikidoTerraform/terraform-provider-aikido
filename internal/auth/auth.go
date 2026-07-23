// Package auth handles Aikido API authentication. It exchanges OAuth 2.0
// client credentials for a bearer token and returns an HTTP client that
// injects and refreshes that token transparently.
package auth

import (
	"context"
	"net/http"
	"strings"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"
)

// tokenPath is the OAuth 2.0 token endpoint, appended to the API base URL so
// authentication targets the same environment as the API calls.
const tokenPath = "/oauth/token"

// NewHTTPClient returns an *http.Client that authenticates every request with a
// bearer token obtained via the client-credentials grant. The underlying
// oauth2 transport fetches the token on first use and refreshes it when it
// expires, so callers never manage token lifetime themselves.
//
// Uses context.Background() for the token source on purpose: Terraform cancels
// the Configure RPC context when Configure returns, and oauth2 retains that
// context for later token fetches. Passing it would make the first API call
// fail with "context canceled". Per-request cancellation still comes from the
// context on each http.Request (see client.Do).
func NewHTTPClient(clientID, clientSecret, baseURL string) *http.Client {
	cfg := clientcredentials.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		TokenURL:     strings.TrimRight(baseURL, "/") + tokenPath,
		// Aikido expects Basic auth: base64(client_id:client_secret) in the header.
		AuthStyle: oauth2.AuthStyleInHeader,
	}
	return cfg.Client(context.Background())
}
