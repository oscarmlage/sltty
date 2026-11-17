package chat

// Sending files, the way Slack's own clients do now (files.upload is
// retired): ask for an upload URL, send the bytes there, then complete the
// upload into the conversation or thread, with the text as its comment.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/oscarmlage/sltty/internal/diag"
)

// uploadTimeout bounds sending a file's bytes.
const uploadTimeout = 5 * time.Minute

func (s *Slack) Upload(ctx context.Context, id, root, text string, f Upload) (Message, error) {
	var u struct {
		URL  string `json:"upload_url"`
		File string `json:"file_id"`
	}
	if err := s.call(ctx, "files.getUploadURLExternal", url.Values{"filename": {f.Name}, "length": {strconv.Itoa(len(f.Data))}}, &u); err != nil {
		return Message{}, err
	}
	if err := s.putFile(ctx, u.URL, f); err != nil {
		return Message{}, err
	}
	files, _ := json.Marshal([]map[string]string{{"id": u.File, "title": f.Name}})
	form := url.Values{"files": {string(files)}, "channel_id": {id}}
	if text != "" {
		form.Set("initial_comment", text)
	}
	if root != "" {
		form.Set("thread_ts", root)
	}
	var done struct{}
	if err := s.call(ctx, "files.completeUploadExternal", form, &done); err != nil {
		return Message{}, err
	}
	// Slack shares the file a moment later and does not say as which
	// message: it shows when the conversation is read again.
	return Message{}, nil
}

// putFile sends the bytes to the URL files.getUploadURLExternal gave.
func (s *Slack) putFile(ctx context.Context, to string, f Upload) error {
	if to == "" {
		return fmt.Errorf("Slack gave no upload URL")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, to, bytes.NewReader(f.Data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Authorization", "Bearer "+s.creds.Token)
	req.Header.Set("Cookie", "d="+s.creds.Cookie)
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Origin", "https://app.slack.com")
	diag.Printf("slack upload %s (%d bytes)", f.Name, len(f.Data))
	hc := *s.hc
	hc.Timeout = uploadTimeout // the API's 20 s are too few for a big file
	res, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 4<<10))
	if res.StatusCode != http.StatusOK {
		diag.Printf("slack upload HTTP %d: %s", res.StatusCode, StripControl(body))
		return fmt.Errorf("Slack upload: HTTP %d", res.StatusCode)
	}
	return nil
}
