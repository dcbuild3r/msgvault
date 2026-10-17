package gmail

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMailboxChanges(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		paths = append(paths, r.URL.Path)
		if r.URL.Path == "/gmail/v1/users/me/messages/test123/modify" {
			var body map[string][]string
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			assert.Equal(t, []string{"STARRED"}, body["addLabelIds"])
			assert.Equal(t, []string{"UNREAD", "INBOX"}, body["removeLabelIds"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"test123","labelIds":["STARRED"]}`))
	}))
	defer server.Close()
	client := &Client{httpClient: &http.Client{Transport: &rewriteTransport{base: server.URL, wrapped: http.DefaultTransport}}, userID: "me", logger: slog.Default(), rateLimiter: NewRateLimiter(100)}
	labels, err := client.ModifyMessageLabels(context.Background(), "test123", []string{"STARRED"}, []string{"UNREAD", "INBOX"})
	require.NoError(t, err)
	assert.Equal(t, []string{"STARRED"}, labels)
	_, err = client.SetMessageTrash(context.Background(), "test123", true)
	require.NoError(t, err)
	_, err = client.SetMessageTrash(context.Background(), "test123", false)
	require.NoError(t, err)
	assert.Equal(t, []string{"/gmail/v1/users/me/messages/test123/modify", "/gmail/v1/users/me/messages/test123/trash", "/gmail/v1/users/me/messages/test123/untrash"}, paths)
}
