// Package matrix archives rooms from a Matrix homeserver through the
// authenticated client-server API.
package matrix

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const defaultRequestTimeout = 90 * time.Second

var ErrMediaTooLarge = errors.New("Matrix media exceeds download limit")

// Client is the narrow Matrix client-server API surface required by the
// archive importer.
type Client struct {
	baseURL     *url.URL
	accessToken string
	httpClient  *http.Client
}

// WhoAmIResponse is returned by the Matrix account/whoami endpoint.
type WhoAmIResponse struct {
	UserID   string `json:"user_id"`
	DeviceID string `json:"device_id"`
}

// NewClient validates homeserverURL and constructs an authenticated client.
func NewClient(homeserverURL, accessToken string) (*Client, error) {
	if strings.TrimSpace(accessToken) == "" {
		return nil, errors.New("Matrix access token required")
	}
	base, err := url.Parse(strings.TrimRight(homeserverURL, "/"))
	if err != nil {
		return nil, fmt.Errorf("parse Matrix homeserver URL: %w", err)
	}
	if base.Scheme != "http" && base.Scheme != "https" {
		return nil, fmt.Errorf("Matrix homeserver URL must use http or https")
	}
	if base.Host == "" {
		return nil, errors.New("Matrix homeserver URL requires a host")
	}
	return &Client{
		baseURL:     base,
		accessToken: accessToken,
		httpClient:  &http.Client{Timeout: defaultRequestTimeout},
	}, nil
}

// WithHTTPClient replaces the transport used by the client. It is intended
// for callers that need custom TLS or timeout behavior.
func (c *Client) WithHTTPClient(httpClient *http.Client) *Client {
	if httpClient != nil {
		c.httpClient = httpClient
	}
	return c
}

// WhoAmI validates the token and returns the Matrix account it represents.
func (c *Client) WhoAmI(ctx context.Context) (*WhoAmIResponse, error) {
	var out WhoAmIResponse
	if err := c.getJSON(ctx, "/_matrix/client/v3/account/whoami", nil, &out); err != nil {
		return nil, err
	}
	if out.UserID == "" {
		return nil, errors.New("Matrix whoami response omitted user_id")
	}
	return &out, nil
}

// SyncResponse is the subset of /sync required by the archive importer.
type SyncResponse struct {
	NextBatch string `json:"next_batch"`
	Rooms     struct {
		Join map[string]JoinedRoom `json:"join"`
	} `json:"rooms"`
}

// JoinedRoom contains state and timeline events for a joined room.
type JoinedRoom struct {
	State struct {
		Events []Event `json:"events"`
	} `json:"state"`
	Timeline struct {
		Events    []Event `json:"events"`
		PrevBatch string  `json:"prev_batch"`
		Limited   bool    `json:"limited"`
	} `json:"timeline"`
}

// MessagesResponse is a backwards pagination page from /rooms/{id}/messages.
type MessagesResponse struct {
	Chunk []Event `json:"chunk"`
	End   string  `json:"end"`
}

// Sync performs one incremental sync. timeout is a server-side long-poll
// duration; zero requests an immediate response.
func (c *Client) Sync(ctx context.Context, since string, timeout time.Duration) (*SyncResponse, error) {
	query := url.Values{}
	if since != "" {
		query.Set("since", since)
	}
	if timeout > 0 {
		query.Set("timeout", fmt.Sprintf("%d", timeout.Milliseconds()))
	}
	query.Set("set_presence", "offline")
	var out SyncResponse
	if err := c.getJSON(ctx, "/_matrix/client/v3/sync", query, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Messages walks a room backwards from a pagination token.
func (c *Client) Messages(ctx context.Context, roomID, from string, limit int) (*MessagesResponse, error) {
	if limit <= 0 {
		limit = 100
	}
	query := url.Values{
		"dir":   []string{"b"},
		"limit": []string{fmt.Sprintf("%d", limit)},
	}
	if from != "" {
		query.Set("from", from)
	}
	// Assign the raw Matrix room ID to URL.Path; url.URL.String escapes it
	// exactly once. Passing PathEscape here would double-escape the leading !.
	path := "/_matrix/client/v3/rooms/" + roomID + "/messages"
	var out MessagesResponse
	if err := c.getJSON(ctx, path, query, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DownloadMedia resolves an mxc:// URI through the authenticated Matrix media
// API. maxBytes is a hard response-body cap; zero disables the cap.
func (c *Client) DownloadMedia(ctx context.Context, mxcURI string, maxBytes int64) ([]byte, string, error) {
	mxc, err := url.Parse(mxcURI)
	if err != nil || mxc.Scheme != "mxc" || mxc.Host == "" || strings.Trim(mxc.Path, "/") == "" {
		return nil, "", fmt.Errorf("invalid Matrix media URI %q", mxcURI)
	}
	u := *c.baseURL
	u.Path = strings.TrimRight(c.baseURL.Path, "/") + "/_matrix/client/v1/media/download/" + mxc.Host + "/" + strings.TrimPrefix(mxc.Path, "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.accessToken)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("download Matrix media: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("download Matrix media: %s", resp.Status)
	}
	reader := io.Reader(resp.Body)
	if maxBytes > 0 {
		reader = io.LimitReader(resp.Body, maxBytes+1)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, "", fmt.Errorf("read Matrix media: %w", err)
	}
	if maxBytes > 0 && int64(len(data)) > maxBytes {
		return nil, "", ErrMediaTooLarge
	}
	return data, resp.Header.Get("Content-Type"), nil
}

func (c *Client) getJSON(ctx context.Context, path string, query url.Values, out any) error {
	u := *c.baseURL
	u.Path = strings.TrimRight(c.baseURL.Path, "/") + path
	u.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.accessToken)
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("Matrix GET %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var matrixErr struct {
			ErrCode string `json:"errcode"`
			Error   string `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&matrixErr)
		if matrixErr.Error == "" {
			matrixErr.Error = resp.Status
		}
		return fmt.Errorf("Matrix GET %s: %s: %s", path, matrixErr.ErrCode, matrixErr.Error)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode Matrix GET %s: %w", path, err)
	}
	return nil
}
