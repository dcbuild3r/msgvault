package matrix

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"go.kenn.io/msgvault/internal/store"
)

const (
	sourceType              = "matrix"
	messageType             = "matrix"
	participantIDType       = "matrix"
	defaultBackfillPageSize = 100
)

// ImportOptions controls a Matrix sync run.
type ImportOptions struct {
	Full            bool
	BackfillLimit   int
	LongPollTimeout time.Duration
	AttachmentsDir  string
	NoMedia         bool
	MaxMediaBytes   int64
}

// ImportSummary reports durable work performed by a sync.
type ImportSummary struct {
	SourceID              int64
	RoomsProcessed        int64
	MessagesProcessed     int64
	MessagesAdded         int64
	MessagesUpdated       int64
	AttachmentsDownloaded int64
	Errors                int64
	Duration              time.Duration
}

type roomState struct {
	BackfillToken string `json:"backfill_token,omitempty"`
	BackfillDone  bool   `json:"backfill_done,omitempty"`
	Title         string `json:"title,omitempty"`
}

type syncState struct {
	NextBatch string                `json:"next_batch,omitempty"`
	Rooms     map[string]*roomState `json:"rooms,omitempty"`
}

func newSyncState() *syncState { return &syncState{Rooms: map[string]*roomState{}} }

func loadState(raw string) (*syncState, error) {
	state := newSyncState()
	if raw == "" {
		return state, nil
	}
	if err := json.Unmarshal([]byte(raw), state); err != nil {
		return nil, err
	}
	if state.Rooms == nil {
		state.Rooms = map[string]*roomState{}
	}
	return state, nil
}

func (s *syncState) room(roomID string) *roomState {
	if s.Rooms[roomID] == nil {
		s.Rooms[roomID] = &roomState{}
	}
	return s.Rooms[roomID]
}

func (s *syncState) marshal() string {
	data, _ := json.Marshal(s)
	return string(data)
}

// Importer synchronizes a single Matrix archive identity into MsgVault.
type Importer struct {
	store    *store.Store
	client   *Client
	selfUser string
}

func NewImporter(s *store.Store, client *Client, selfUser string) *Importer {
	return &Importer{store: s, client: client, selfUser: selfUser}
}

// Sync performs one /sync and completes any outstanding room history
// backfills. Progress is checkpointed after every page.
func (imp *Importer) Sync(ctx context.Context, opts ImportOptions) (sum *ImportSummary, err error) {
	started := time.Now()
	if imp.store == nil || imp.client == nil {
		return nil, errors.New("Matrix importer requires store and client")
	}
	source, err := imp.store.GetOrCreateSource(sourceType, imp.selfUser)
	if err != nil {
		return nil, err
	}
	sum = &ImportSummary{SourceID: source.ID}
	state, err := imp.resumeState(source.ID)
	if err != nil {
		return nil, err
	}
	if opts.Full {
		state = newSyncState()
	}
	syncID, err := imp.store.StartSync(source.ID, sourceType)
	if err != nil {
		return nil, err
	}
	defer func() {
		sum.Duration = time.Since(started)
		if err != nil {
			_ = imp.store.FailSyncWithCheckpoint(syncID, err.Error(), imp.checkpoint(state, sum))
		}
	}()

	response, err := imp.client.Sync(ctx, state.NextBatch, opts.LongPollTimeout)
	if err != nil {
		return sum, err
	}
	for roomID, room := range response.Rooms.Join {
		rs := state.room(roomID)
		imp.applyRoomState(rs, room.State.Events)
		if err = imp.persistEvents(ctx, source.ID, roomID, rs.Title, room.Timeline.Events, sum, opts); err != nil {
			return sum, err
		}
		if !rs.BackfillDone {
			if rs.BackfillToken == "" {
				rs.BackfillToken = room.Timeline.PrevBatch
			}
			if err = imp.backfillRoom(ctx, syncID, source.ID, roomID, rs, state, sum, opts); err != nil {
				return sum, err
			}
		}
		sum.RoomsProcessed++
	}
	state.NextBatch = response.NextBatch
	if err = imp.store.RecomputeConversationStats(source.ID); err != nil {
		return sum, err
	}
	if err = imp.store.UpdateSyncCheckpoint(syncID, imp.checkpoint(state, sum)); err != nil {
		return sum, err
	}
	if err = imp.store.CompleteSync(syncID, state.marshal()); err != nil {
		return sum, err
	}
	return sum, nil
}

func (imp *Importer) resumeState(sourceID int64) (*syncState, error) {
	if run, err := imp.store.GetLatestCheckpointedSync(sourceID); err == nil && run.CursorBefore.Valid {
		return loadState(run.CursorBefore.String)
	}
	if run, err := imp.store.GetLastSuccessfulSync(sourceID); err == nil && run.CursorAfter.Valid {
		return loadState(run.CursorAfter.String)
	}
	return newSyncState(), nil
}

func (imp *Importer) checkpoint(state *syncState, sum *ImportSummary) *store.Checkpoint {
	return &store.Checkpoint{
		PageToken:         state.marshal(),
		MessagesProcessed: sum.MessagesProcessed,
		MessagesAdded:     sum.MessagesAdded,
		MessagesUpdated:   sum.MessagesUpdated,
		ErrorsCount:       sum.Errors,
	}
}

func (imp *Importer) applyRoomState(rs *roomState, events []Event) {
	for _, event := range events {
		if event.Type == "m.room.name" {
			if name := event.decodedContent().Name; name != "" {
				rs.Title = name
			}
		}
	}
}

func (imp *Importer) backfillRoom(ctx context.Context, syncID, sourceID int64, roomID string, rs *roomState, state *syncState, sum *ImportSummary, opts ImportOptions) error {
	if rs.BackfillToken == "" {
		rs.BackfillDone = true
		return nil
	}
	pages := 0
	for !rs.BackfillDone {
		if err := ctx.Err(); err != nil {
			return err
		}
		page, err := imp.client.Messages(ctx, roomID, rs.BackfillToken, defaultBackfillPageSize)
		if err != nil {
			return err
		}
		if err := imp.persistEvents(ctx, sourceID, roomID, rs.Title, page.Chunk, sum, opts); err != nil {
			return err
		}
		pages++
		if page.End == "" || page.End == rs.BackfillToken || len(page.Chunk) == 0 {
			rs.BackfillDone = true
		} else {
			rs.BackfillToken = page.End
		}
		if err := imp.store.UpdateSyncCheckpoint(syncID, imp.checkpoint(state, sum)); err != nil {
			return err
		}
		if opts.BackfillLimit > 0 && pages >= opts.BackfillLimit {
			break
		}
	}
	return nil
}

func (imp *Importer) persistEvents(ctx context.Context, sourceID int64, roomID, title string, events []Event, sum *ImportSummary, opts ImportOptions) error {
	for i := range events {
		if err := ctx.Err(); err != nil {
			return err
		}
		if events[i].EventID == "" {
			continue
		}
		if err := imp.persistEvent(ctx, sourceID, roomID, title, &events[i], sum, opts); err != nil {
			return err
		}
	}
	return nil
}

func (imp *Importer) persistEvent(ctx context.Context, sourceID int64, roomID, title string, event *Event, sum *ImportSummary, opts ImportOptions) error {
	convID, err := imp.store.EnsureConversationWithType(sourceID, roomID, "group_chat", title)
	if err != nil {
		return err
	}
	content := event.decodedContent()
	body, htmlBody := normalizedBody(event, content)
	senderID, err := imp.store.EnsureParticipantByIdentifier(participantIDType, event.Sender, event.Sender)
	if err != nil {
		return err
	}
	if err := imp.store.EnsureConversationParticipant(convID, senderID, "member"); err != nil {
		return err
	}
	existing, err := imp.store.MessageExistsBatch(sourceID, []string{event.EventID})
	if err != nil {
		return err
	}
	sentAt := time.UnixMilli(event.OriginServerTS).UTC()
	msg := &store.Message{
		ConversationID:  convID,
		SourceID:        sourceID,
		SourceMessageID: event.EventID,
		MessageType:     messageType,
		SentAt:          sql.NullTime{Time: sentAt, Valid: event.OriginServerTS > 0},
		ReceivedAt:      sql.NullTime{Time: sentAt, Valid: event.OriginServerTS > 0},
		SenderID:        sql.NullInt64{Int64: senderID, Valid: senderID != 0},
		IsFromMe:        event.Sender == imp.selfUser,
		Subject:         sql.NullString{String: event.Type, Valid: event.Type != ""},
		Snippet:         sql.NullString{String: snippet(body), Valid: body != ""},
	}
	messageID, err := imp.store.UpsertMessage(msg)
	if err != nil {
		return err
	}
	if err := imp.store.UpsertMessageBody(messageID,
		sql.NullString{String: body, Valid: body != ""},
		sql.NullString{String: htmlBody, Valid: htmlBody != ""}); err != nil {
		return err
	}
	if err := imp.store.UpsertMessageRawWithFormat(messageID, event.Raw, "matrix_json"); err != nil {
		return err
	}
	if err := imp.store.UpsertFTS(messageID, event.Type, body, event.Sender, "", ""); err != nil {
		return err
	}
	imp.persistMedia(ctx, messageID, content, opts, sum)
	if _, wasExisting := existing[event.EventID]; wasExisting {
		sum.MessagesUpdated++
	} else {
		sum.MessagesAdded++
	}
	sum.MessagesProcessed++

	target := relationTarget(content)
	if target != "" {
		_ = imp.store.SetReplyTo(sourceID, event.EventID, target)
	}
	if content.RelatesTo.RelType == "m.replace" && content.RelatesTo.EventID != "" {
		if targetRows, qerr := imp.store.MessageExistsBatch(sourceID, []string{content.RelatesTo.EventID}); qerr == nil {
			if targetID := targetRows[content.RelatesTo.EventID]; targetID != 0 {
				_ = imp.store.SetMessageEdited(targetID)
			}
		}
	}
	if event.Type == "m.room.redaction" && event.Redacts != "" {
		_ = imp.store.MarkMessageDeleted(sourceID, event.Redacts)
	}
	return nil
}

func normalizedBody(event *Event, content eventContent) (string, string) {
	if content.NewContent != nil && content.RelatesTo.RelType == "m.replace" {
		return content.NewContent.Body, content.NewContent.FormattedBody
	}
	if content.Body != "" || content.FormattedBody != "" {
		return content.Body, content.FormattedBody
	}
	switch event.Type {
	case "m.room.redaction":
		return "[redaction of " + event.Redacts + "]", ""
	case "m.reaction":
		return "[reaction " + content.RelatesTo.Key + " to " + content.RelatesTo.EventID + "]", ""
	case "m.room.encrypted":
		return "[encrypted Matrix event unavailable to archive]", ""
	case "m.room.member":
		return "[membership: " + content.Membership + "]", ""
	default:
		return "[" + event.Type + "]", ""
	}
}

func relationTarget(content eventContent) string {
	if content.RelatesTo.Reply != nil {
		return content.RelatesTo.Reply.EventID
	}
	return content.RelatesTo.EventID
}

func snippet(body string) string {
	runes := []rune(body)
	if len(runes) > 100 {
		return string(runes[:100])
	}
	return body
}

var _ = fmt.Sprintf
