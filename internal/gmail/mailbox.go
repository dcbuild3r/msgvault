package gmail

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
)

// ModifyMessageLabels updates provider state and returns Gmail's confirmed labels.
func (c *Client) ModifyMessageLabels(ctx context.Context, id string, add, remove []string) ([]string, error) {
	body, err := json.Marshal(struct {
		Add    []string `json:"addLabelIds,omitempty"`
		Remove []string `json:"removeLabelIds,omitempty"`
	}{add, remove})
	if err != nil {
		return nil, err
	}
	return c.mailboxRequest(ctx, id, "modify", body)
}

// SetMessageTrash uses Gmail's recoverable trash operation, never permanent deletion.
func (c *Client) SetMessageTrash(ctx context.Context, id string, trashed bool) ([]string, error) {
	action := "untrash"
	if trashed {
		action = "trash"
	}
	return c.mailboxRequest(ctx, id, action, nil)
}

func (c *Client) mailboxRequest(ctx context.Context, id, action string, body []byte) ([]string, error) {
	data, err := c.request(ctx, OpMessagesTrash, "POST", fmt.Sprintf("/users/%s/messages/%s/%s", c.userID, url.PathEscape(id), action), body)
	if err != nil {
		return nil, err
	}
	var result struct {
		ID     string   `json:"id"`
		Labels []string `json:"labelIds"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	if result.ID != id {
		return nil, fmt.Errorf("Gmail did not confirm message state")
	}
	if result.Labels == nil {
		result.Labels = []string{}
	}
	return result.Labels, nil
}
