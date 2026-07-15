package matrix_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	msgmatrix "go.kenn.io/msgvault/internal/matrix"
	"go.kenn.io/msgvault/internal/testutil"
)

func TestImporterArchivesRawEventAndDeduplicatesByEventID(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/_matrix/client/v3/sync":
			_, _ = w.Write([]byte(`{
                  "next_batch":"s1",
                  "rooms":{"join":{"!room:example.test":{
                    "state":{"events":[{"type":"m.room.name","state_key":"","content":{"name":"Archive Test"}}]},
                    "timeline":{"prev_batch":"back1","limited":true,"events":[{
                      "type":"m.room.message","event_id":"$event1","sender":"@alice:example.test",
                      "origin_server_ts":1784000000000,
                      "content":{"msgtype":"m.text","body":"hello matrix","extra_field":"preserve me"}
                    }]}
                  }}}
                }`))
		case r.URL.Path == "/_matrix/client/v3/rooms/!room:example.test/messages":
			_, _ = w.Write([]byte(`{"chunk":[],"end":""}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	st := testutil.NewTestStore(t)
	client, err := msgmatrix.NewClient(server.URL, "archive-token")
	require.NoError(t, err)
	importer := msgmatrix.NewImporter(st, client, "@archive:example.test")

	for range 2 {
		_, err = importer.Sync(context.Background(), msgmatrix.ImportOptions{})
		require.NoError(t, err)
	}

	var count int
	require.NoError(t, st.DB().QueryRow(`SELECT COUNT(*) FROM messages WHERE message_type='matrix'`).Scan(&count))
	assert.Equal(t, 1, count)

	var body, rawFormat string
	require.NoError(t, st.DB().QueryRow(`
      SELECT b.body_text, r.raw_format
      FROM messages m
      JOIN message_bodies b ON b.message_id=m.id
      JOIN message_raw r ON r.message_id=m.id
      WHERE m.source_message_id='$event1'`).Scan(&body, &rawFormat))
	assert.Equal(t, "hello matrix", body)
	assert.Equal(t, "matrix_json", rawFormat)

	raw, err := st.GetMessageRaw(1)
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"extra_field":"preserve me"`)
}

func TestImporterPreservesOriginalContentAndAppendsEditAndRedactionEvents(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/_matrix/client/v3/sync" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{
          "next_batch":"s2","rooms":{"join":{"!room:example.test":{"timeline":{"events":[
            {"type":"m.room.message","event_id":"$original","sender":"@alice:example.test","origin_server_ts":1000,"content":{"msgtype":"m.text","body":"original text"}},
            {"type":"m.room.message","event_id":"$edit","sender":"@alice:example.test","origin_server_ts":2000,"content":{"msgtype":"m.text","body":"* corrected text","m.new_content":{"msgtype":"m.text","body":"corrected text"},"m.relates_to":{"rel_type":"m.replace","event_id":"$original"}}},
            {"type":"m.room.redaction","event_id":"$redaction","sender":"@alice:example.test","origin_server_ts":3000,"redacts":"$original","content":{"reason":"cleanup"}}
          ]}}}}}`))
	}))
	t.Cleanup(server.Close)

	st := testutil.NewTestStore(t)
	client, err := msgmatrix.NewClient(server.URL, "archive-token")
	require.NoError(t, err)
	_, err = msgmatrix.NewImporter(st, client, "@archive:example.test").Sync(context.Background(), msgmatrix.ImportOptions{})
	require.NoError(t, err)

	var originalBody string
	var edited bool
	var deletedAt any
	require.NoError(t, st.DB().QueryRow(`
      SELECT b.body_text, m.is_edited, m.deleted_from_source_at
      FROM messages m JOIN message_bodies b ON b.message_id=m.id
      WHERE m.source_message_id='$original'`).Scan(&originalBody, &edited, &deletedAt))
	assert.Equal(t, "original text", originalBody)
	assert.True(t, edited)
	assert.NotNil(t, deletedAt)

	var editBody, redactionBody string
	require.NoError(t, st.DB().QueryRow(`SELECT b.body_text FROM messages m JOIN message_bodies b ON b.message_id=m.id WHERE m.source_message_id='$edit'`).Scan(&editBody))
	require.NoError(t, st.DB().QueryRow(`SELECT b.body_text FROM messages m JOIN message_bodies b ON b.message_id=m.id WHERE m.source_message_id='$redaction'`).Scan(&redactionBody))
	assert.Equal(t, "corrected text", editBody)
	assert.Equal(t, "[redaction of $original]", redactionBody)
}

func TestImporterDownloadsMatrixMediaIntoAttachmentStore(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/_matrix/client/v3/sync":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"next_batch":"s3","rooms":{"join":{"!media:example.test":{"timeline":{"events":[{"type":"m.room.message","event_id":"$media","sender":"@alice:example.test","origin_server_ts":1000,"content":{"msgtype":"m.image","body":"photo.jpg","url":"mxc://media.example/abc123","info":{"mimetype":"image/jpeg","size":12,"w":640,"h":480}}}]}}}}}`))
		case "/_matrix/client/v1/media/download/media.example/abc123":
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write([]byte("image-bytes!"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	st := testutil.NewTestStore(t)
	client, err := msgmatrix.NewClient(server.URL, "archive-token")
	require.NoError(t, err)
	_, err = msgmatrix.NewImporter(st, client, "@archive:example.test").Sync(context.Background(), msgmatrix.ImportOptions{
		AttachmentsDir: t.TempDir(),
		MaxMediaBytes:  1 << 20,
	})
	require.NoError(t, err)

	var filename, mimeType, contentHash string
	var size int
	require.NoError(t, st.DB().QueryRow(`
      SELECT a.filename, a.mime_type, a.content_hash, a.size
      FROM attachments a JOIN messages m ON m.id=a.message_id
      WHERE m.source_message_id='$media'`).Scan(&filename, &mimeType, &contentHash, &size))
	assert.Equal(t, "photo.jpg", filename)
	assert.Equal(t, "image/jpeg", mimeType)
	assert.NotEmpty(t, contentHash)
	assert.Equal(t, len("image-bytes!"), size)
}
