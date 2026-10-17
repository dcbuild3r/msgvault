package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// EmailActionRequest contains only reversible provider mailbox changes.
type EmailActionRequest struct {
	Read     *bool `json:"read,omitempty"`
	Archived *bool `json:"archived,omitempty"`
	Starred  *bool `json:"starred,omitempty"`
	Trashed  *bool `json:"trashed,omitempty"`
}

type EmailActionResponse struct {
	Labels []string `json:"labels"`
}
type EmailAction func(context.Context, int64, EmailActionRequest) ([]string, error)

var ErrUnsupportedEmailSource = errors.New("provider actions are supported only for Gmail email")

func (s *Server) handleEmailAction(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, 400, "invalid_request", "Invalid message ID")
		return
	}
	var patch EmailActionRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&patch); err != nil {
		writeError(w, 400, "invalid_request", "Invalid mailbox action")
		return
	}
	if decoder.Decode(new(any)) != io.EOF || (patch.Read == nil && patch.Archived == nil && patch.Starred == nil && patch.Trashed == nil) {
		writeError(w, 400, "invalid_request", "Choose a mailbox action")
		return
	}
	if s.emailAction == nil {
		writeError(w, 503, "unavailable", "Provider actions are not configured")
		return
	}
	labels, err := s.emailAction(r.Context(), id, patch)
	if errors.Is(err, ErrUnsupportedEmailSource) {
		writeError(w, 422, "unsupported_source", err.Error())
		return
	}
	if err != nil {
		s.logger.Error("email provider update failed", "message_id", id, "error", err)
		writeError(w, 502, "provider_update_failed", "Gmail could not confirm this update. Refresh and check the account connection before retrying.")
		return
	}
	writeJSON(w, 200, EmailActionResponse{Labels: labels})
}

// Match the only operation available to the optional mailbox credential.
func isEmailActionRequest(r *http.Request) bool {
	if r.Method != http.MethodPost {
		return false
	}
	path, ok := strings.CutPrefix(r.URL.Path, "/api/v1/messages/")
	if !ok {
		return false
	}
	id, ok := strings.CutSuffix(path, "/state")
	if !ok {
		return false
	}
	number, err := strconv.ParseInt(id, 10, 64)
	return err == nil && number > 0
}
