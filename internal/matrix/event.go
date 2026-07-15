package matrix

import "encoding/json"

// Event preserves the verbatim Matrix JSON alongside the normalized fields
// needed by MsgVault.
type Event struct {
	Type           string          `json:"type"`
	EventID        string          `json:"event_id"`
	Sender         string          `json:"sender"`
	OriginServerTS int64           `json:"origin_server_ts"`
	StateKey       *string         `json:"state_key,omitempty"`
	Content        json.RawMessage `json:"content"`
	Redacts        string          `json:"redacts,omitempty"`
	Raw            json.RawMessage `json:"-"`
}

// UnmarshalJSON captures unknown fields exactly as received.
func (e *Event) UnmarshalJSON(data []byte) error {
	type eventAlias Event
	var decoded eventAlias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*e = Event(decoded)
	e.Raw = append(e.Raw[:0], data...)
	return nil
}

type eventContent struct {
	Body          string `json:"body"`
	FormattedBody string `json:"formatted_body"`
	MsgType       string `json:"msgtype"`
	Membership    string `json:"membership"`
	Name          string `json:"name"`
	URL           string `json:"url"`
	Info          struct {
		MimeType string `json:"mimetype"`
		Size     int64  `json:"size"`
		Width    int64  `json:"w"`
		Height   int64  `json:"h"`
		Duration int64  `json:"duration"`
	} `json:"info"`
	NewContent *struct {
		Body          string `json:"body"`
		FormattedBody string `json:"formatted_body"`
		MsgType       string `json:"msgtype"`
		URL           string `json:"url"`
	} `json:"m.new_content"`
	RelatesTo struct {
		EventID string `json:"event_id"`
		RelType string `json:"rel_type"`
		Key     string `json:"key"`
		Reply   *struct {
			EventID string `json:"event_id"`
		} `json:"m.in_reply_to"`
	} `json:"m.relates_to"`
}

func (e Event) decodedContent() eventContent {
	var content eventContent
	_ = json.Unmarshal(e.Content, &content)
	return content
}
