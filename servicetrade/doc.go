// Package servicetrade is a client for the ServiceTrade REST API
// (https://api.servicetrade.com/api/docs). It mirrors the official Python
// SDK: a thin transport over the JSON endpoints with OAuth2 authentication,
// lazy login, automatic token refresh, retries on transient server errors,
// multipart attachment uploads and a paginator.
//
// Construct a client with one of the supported credential types. A refresh
// token takes priority over client credentials when both are supplied; a
// bearer token alone is used as-is and cannot be refreshed.
//
//	client, err := servicetrade.NewClient(
//		servicetrade.WithClientCredentials(os.Getenv("ST_CLIENT_ID"), os.Getenv("ST_CLIENT_SECRET")),
//	)
//
// Requests decode the response's "data" envelope (or the whole body when no
// envelope is present) into the out argument:
//
//	var job struct {
//		ID          int    `json:"id"`
//		Description string `json:"description"`
//	}
//	_, err = client.Get(ctx, "/job/123", nil, &job)
//
//	_, err = client.Post(ctx, "/job", nil, map[string]any{
//		"type":        "inspection",
//		"description": "Quarterly HVAC Inspection",
//		"locationId":  123,
//	}, &job)
//
// Collections are paged with Paginate, which walks every page and yields
// items one at a time:
//
//	for job, err := range servicetrade.Paginate[Job](ctx, client, "/job", "jobs", url.Values{"status": {"scheduled"}}) {
//		if err != nil {
//			return err
//		}
//		fmt.Println(job.ID)
//	}
//
// Non-2xx responses are returned as an *APIError carrying the status, body
// and any structured messages; authentication failures are an *AuthError.
package servicetrade
