package api

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.kenn.io/msgvault/internal/config"
)

func TestEmailActionRoute(t *testing.T) {
	calls := 0
	server := NewServerWithOptions(ServerOptions{
		Logger: slog.Default(),
		Config: &config.Config{Server: config.ServerConfig{APIKey: "test-key", EmailActionAPIKey: "mailbox-key"}},
		EmailAction: func(ctx context.Context, id int64, patch EmailActionRequest) ([]string, error) {
			calls++
			assert.Equal(t, int64(42), id)
			require.NotNil(t, patch.Starred)
			assert.True(t, *patch.Starred)
			return []string{"INBOX", "STARRED"}, nil
		},
	})
	for _, tt := range []struct {
		body, key string
		status    int
	}{
		{`{"starred":true}`, "", 401},
		{`{"send":true}`, "test-key", 400},
		{`{}`, "test-key", 400},
		{`{"starred":true}`, "test-key", 200},
		{`{"starred":true}`, "mailbox-key", 200},
	} {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/messages/42/state", strings.NewReader(tt.body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-API-Key", tt.key)
		response := httptest.NewRecorder()
		server.Router().ServeHTTP(response, request)
		assert.Equal(t, tt.status, response.Code, response.Body.String())
	}
	assert.Equal(t, 2, calls)
	for _, path := range []string{"/api/v1/messages/42", "/api/v1/cli/run", "/api/v1/query", "/api/v1/messages/42/../state", "/api/v1/messages/0/state"} {
		request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
		request.Header.Set("X-API-Key", "mailbox-key")
		assert.False(t, server.apiRequestAuthorized(request), path)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/messages/42/state", nil)
	request.Header.Set("X-API-Key", "mailbox-key")
	assert.False(t, server.apiRequestAuthorized(request))
}
