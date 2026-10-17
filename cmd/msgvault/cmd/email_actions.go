package cmd

import (
	"context"
	"fmt"
	"slices"

	"go.kenn.io/msgvault/internal/api"
	"go.kenn.io/msgvault/internal/gmail"
	"go.kenn.io/msgvault/internal/oauth"
	"go.kenn.io/msgvault/internal/store"
)

func emailActionWriter(s *store.Store, getManager func(string) (*oauth.Manager, error)) api.EmailAction {
	return func(ctx context.Context, id int64, patch api.EmailActionRequest) ([]string, error) {
		message, err := s.GetMessage(id)
		if err != nil {
			return nil, err
		}
		if message == nil || message.MessageType != "email" {
			return nil, api.ErrUnsupportedEmailSource
		}
		source, err := s.GetSourceByID(message.SourceID)
		if err != nil {
			return nil, err
		}
		if source == nil || source.SourceType != "gmail" || message.SourceMessageID == "" {
			return nil, api.ErrUnsupportedEmailSource
		}
		manager, err := getManager(sourceOAuthApp(source))
		if err != nil {
			return nil, err
		}
		tokens, err := manager.TokenSource(ctx, source.Identifier)
		if err != nil {
			return nil, fmt.Errorf("reconnect Gmail account: %w", err)
		}
		client := gmail.NewClient(tokens)
		defer client.Close()
		labels := message.Labels
		if patch.Trashed != nil {
			labels, err = client.SetMessageTrash(ctx, message.SourceMessageID, *patch.Trashed)
			if err != nil {
				return nil, err
			}
		}
		var add, remove []string
		change := func(value *bool, label string, invert bool) {
			if value == nil {
				return
			}
			if *value != invert {
				add = append(add, label)
			} else {
				remove = append(remove, label)
			}
		}
		change(patch.Read, "UNREAD", true)
		change(patch.Archived, "INBOX", true)
		change(patch.Starred, "STARRED", false)
		if len(add)+len(remove) > 0 {
			labels, err = client.ModifyMessageLabels(ctx, message.SourceMessageID, add, remove)
			if err != nil {
				return nil, err
			}
		}
		// Preserve all archived custom labels; only reconcile the four controlled system labels.
		for _, label := range []string{"INBOX", "UNREAD", "STARRED", "TRASH"} {
			labelID, err := s.EnsureLabel(source.ID, label, label, "system")
			if err != nil {
				return nil, err
			}
			if slices.Contains(labels, label) {
				err = s.AddMessageLabels(id, []int64{labelID})
			} else {
				err = s.RemoveMessageLabels(id, []int64{labelID})
			}
			if err != nil {
				return nil, err
			}
		}
		return labels, nil
	}
}
