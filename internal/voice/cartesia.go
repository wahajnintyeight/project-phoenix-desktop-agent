package voice

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
	"time"
)

const (
	apiBase    = "https://api.cartesia.ai"
	apiVersion = "2026-08-14"
	voiceID    = "db6b0ed5-d5d3-463d-ae85-518a07d3c2b4"
)

type Cartesia struct{}

var cartesiaClient = &http.Client{
	Timeout: 40 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

func (Cartesia) Transcribe(ctx context.Context, key string, audio []byte, contentType string) (string, error) {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	ext := "webm"
	mimeType := "audio/webm"
	if strings.Contains(contentType, "ogg") {
		ext = "ogg"
		mimeType = "audio/ogg"
	}
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", `form-data; name="file"; filename="recording.`+ext+`"`)
	header.Set("Content-Type", mimeType)
	part, err := w.CreatePart(header)
	if err != nil {
		return "", err
	}
	if _, err := part.Write(audio); err != nil {
		return "", err
	}
	if err := w.WriteField("model", "ink-whisper"); err != nil {
		return "", err
	}
	if err := w.WriteField("language", "en"); err != nil {
		return "", err
	}
	if err := w.Close(); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiBase+"/stt", &body)
	if err != nil {
		return "", err
	}
	setHeaders(req, key)
	req.Header.Set("Content-Type", w.FormDataContentType())
	resp, err := cartesiaClient.Do(req)
	if err != nil {
		return "", errorsForRequest(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", responseError(resp.StatusCode)
	}
	var result struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result); err != nil {
		return "", errorsForRequest(err)
	}
	text := strings.TrimSpace(result.Text)
	if text == "" {
		return "", fmt.Errorf("Cartesia did not recognize any speech")
	}
	return text, nil
}

func (Cartesia) Speak(ctx context.Context, key, text string) ([]byte, error) {
	payload := struct {
		ModelID      string `json:"model_id"`
		Transcript   string `json:"transcript"`
		Voice        string `json:"voice"`
		OutputFormat struct {
			Container  string `json:"container"`
			Encoding   string `json:"encoding"`
			SampleRate int    `json:"sample_rate"`
		} `json:"output_format"`
		Language string `json:"language"`
	}{ModelID: "sonic-3.6", Transcript: text, Voice: voiceID, Language: "en"}
	payload.OutputFormat.Container = "wav"
	payload.OutputFormat.Encoding = "pcm_s16le"
	payload.OutputFormat.SampleRate = 24000
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiBase+"/tts/bytes", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	setHeaders(req, key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := cartesiaClient.Do(req)
	if err != nil {
		return nil, errorsForRequest(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, responseError(resp.StatusCode)
	}
	audio, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil {
		return nil, errorsForRequest(err)
	}
	if len(audio) == 0 || len(audio) > 1<<20 {
		return nil, fmt.Errorf("Cartesia returned invalid audio")
	}
	return audio, nil
}

func setHeaders(req *http.Request, key string) {
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Cartesia-Version", apiVersion)
}

func responseError(status int) error {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("Cartesia rejected the API key")
	case http.StatusTooManyRequests, http.StatusPaymentRequired:
		return fmt.Errorf("Cartesia rate limit or free credits reached")
	default:
		return fmt.Errorf("Cartesia request failed (HTTP %d)", status)
	}
}

func errorsForRequest(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("Cartesia request failed: %w", err)
}
