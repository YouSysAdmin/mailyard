// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package mailyard

import (
	"sync"

	"github.com/yousysadmin/mailyard/sdk/go/api"
)

// API returns the generated client: one method per /api/v1 route.
//
// This package covers the calls most integrations make - sending,
// batching, typed errors, cursor paging - with signatures somebody
// chose. Everything else is in sdk/go/api, generated from the same
// metadata the OpenAPI document is built from, because the product
// surface is two hundred routes and hand-writing that is how a client
// falls behind its server.
//
//	c := mailyard.New("https://mail.example.com", "myk_...")
//	tpl, err := c.API().ListTemplates(ctx)
//
// It shares this client's base URL, credential and http.Client, so
// timeouts and proxies configured here apply there.
func (c *Client) API() *api.Client {
	c.apiOnce.Do(func() {
		opts := []api.ClientOption{api.WithHTTPClient(c.http)}
		if c.agent != "" {
			opts = append(opts, api.WithUserAgent(c.agent))
		}

		c.api = api.New(c.baseURL, c.apiKey, opts...)
	})

	return c.api
}

// apiHolder is embedded into Client. A separate type only so the
// once-and-pointer pair can be added without disturbing the struct
// literal in New.
type apiHolder struct {
	apiOnce sync.Once
	api     *api.Client
}
