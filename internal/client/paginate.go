package client

import (
	"context"
	"fmt"
	"net/http"
	"strings"
)

// FetchAllPages GETs every page of a list endpoint until a short page is
// returned. path is the resource path (no query). extraQuery is optional
// additional query string without a leading '?'.
func FetchAllPages[T any](ctx context.Context, c *Client, path string, perPage int, extraQueryParams string) ([]T, error) {
	var all []T

	for page := 0; ; page++ {
		reqPath := fmt.Sprintf("%s?page=%d&per_page=%d", path, page, perPage)

		if extraQueryParams != "" {
			reqPath += "&" + strings.TrimPrefix(extraQueryParams, "&")
		}

		var pageItems []T
		if err := c.Do(ctx, http.MethodGet, reqPath, nil, &pageItems); err != nil {
			return nil, err
		}

		all = append(all, pageItems...)

		if len(pageItems) < perPage {
			return all, nil
		}
	}
}
