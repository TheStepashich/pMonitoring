package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/proxy"
)

var ErrConflict = errors.New("409 conflict")

type Update struct {
	UpdateID int64    `json:"update_id"`
	Message  *Message `json:"message"`
}

type Message struct {
	MessageID int64  `json:"message_id"`
	Text      string `json:"text"`
	Chat      *Chat  `json:"chat"`
}

type Chat struct {
	ID int64 `json:"id"`
}

type apiResp struct {
	OK          bool            `json:"ok"`
	Description string          `json:"description"`
	Result      json.RawMessage `json:"result"`
}

type fileUpload struct {
	fieldName string
	filename  string
	mime      string
	data      []byte
}

type Telegram interface {
	SendText(text string, chatID *int64) error
	SendDocument(doc []byte, filename, caption string, chatID *int64) error
	SendPhoto(photo []byte, caption string, chatID *int64) error
	GetUpdates(offset, timeout int) ([]Update, error)
}

type Sender struct {
	cfg *Config
}

func NewSender(cfg *Config) *Sender {
	return &Sender{cfg: cfg}
}

func (s *Sender) chatID(chatID *int64) int64 {
	if chatID != nil {
		return *chatID
	}
	return s.cfg.Telegram.ChatID
}

func (s *Sender) socksClient() (*http.Client, error) {
	if s.cfg.Telegram.BotToken == "" {
		return nil, errors.New("не задан telegram.bot_token")
	}
	sc := s.cfg.Backends.Socks5
	if sc.Host == "" {
		return nil, errors.New("socks5-прокси не настроен")
	}
	addr := net.JoinHostPort(sc.Host, strconv.Itoa(sc.Port))
	var auth *proxy.Auth
	if sc.Username != "" {
		auth = &proxy.Auth{User: sc.Username, Password: sc.Password}
	}
	dialer, err := proxy.SOCKS5("tcp", addr, auth, proxy.Direct)
	if err != nil {
		return nil, err
	}
	cd, ok := dialer.(proxy.ContextDialer)
	if !ok {
		return nil, errors.New("прокси не поддерживает DialContext")
	}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			return cd.DialContext(ctx, network, address)
		},
	}
	return &http.Client{Timeout: 60 * time.Second, Transport: transport}, nil
}

func (s *Sender) post(method string, params url.Values, file *fileUpload) (*apiResp, error) {
	client, err := s.socksClient()
	if err != nil {
		return nil, err
	}
	u := fmt.Sprintf("https://api.telegram.org/bot%s/%s", s.cfg.Telegram.BotToken, method)
	var body io.Reader
	contentType := ""
	if file != nil {
		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)
		for k, vs := range params {
			for _, v := range vs {
				_ = w.WriteField(k, v)
			}
		}
		h := make(textproto.MIMEHeader)
		h.Set("Content-Disposition",
			fmt.Sprintf(`form-data; name="%s"; filename="%s"`, file.fieldName, file.filename))
		h.Set("Content-Type", file.mime)
		pw, err := w.CreatePart(h)
		if err == nil {
			_, _ = pw.Write(file.data)
		}
		_ = w.Close()
		body = &buf
		contentType = w.FormDataContentType()
	} else {
		body = strings.NewReader(params.Encode())
		contentType = "application/x-www-form-urlencoded"
	}
	resp, err := client.Post(u, contentType, body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusConflict {
		return nil, ErrConflict
	}
	var r apiResp
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, fmt.Errorf("Telegram API (%d): %v", resp.StatusCode, err)
	}
	if !r.OK {
		return nil, fmt.Errorf("Telegram API (%d): %s", resp.StatusCode, r.Description)
	}
	return &r, nil
}

func (s *Sender) SendText(text string, chatID *int64) error {
	params := url.Values{}
	params.Set("chat_id", strconv.FormatInt(s.chatID(chatID), 10))
	params.Set("text", text)
	_, err := s.post("sendMessage", params, nil)
	return err
}

func (s *Sender) SendDocument(doc []byte, filename, caption string, chatID *int64) error {
	params := url.Values{}
	params.Set("chat_id", strconv.FormatInt(s.chatID(chatID), 10))
	params.Set("caption", caption)
	file := &fileUpload{fieldName: "document", filename: filename, mime: "application/json", data: doc}
	_, err := s.post("sendDocument", params, file)
	return err
}

func (s *Sender) SendPhoto(photo []byte, caption string, chatID *int64) error {
	params := url.Values{}
	params.Set("chat_id", strconv.FormatInt(s.chatID(chatID), 10))
	if caption != "" {
		params.Set("caption", caption)
	}
	file := &fileUpload{fieldName: "photo", filename: "chart.png", mime: "image/png", data: photo}
	_, err := s.post("sendPhoto", params, file)
	return err
}

func (s *Sender) GetUpdates(offset int, timeout int) ([]Update, error) {
	client, err := s.socksClient()
	if err != nil {
		return nil, err
	}
	params := url.Values{}
	params.Set("offset", strconv.Itoa(offset))
	params.Set("timeout", strconv.Itoa(timeout))
	params.Set("allowed_updates", `["message"]`)
	u := fmt.Sprintf("https://api.telegram.org/bot%s/getUpdates?%s",
		s.cfg.Telegram.BotToken, params.Encode())
	resp, err := client.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusConflict {
		return nil, ErrConflict
	}
	var r apiResp
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, fmt.Errorf("Telegram API (%d): %v", resp.StatusCode, err)
	}
	if !r.OK {
		return nil, fmt.Errorf("Telegram API (%d): %s", resp.StatusCode, r.Description)
	}
	var updates []Update
	if len(r.Result) > 0 {
		if err := json.Unmarshal(r.Result, &updates); err != nil {
			return nil, fmt.Errorf("Telegram API (%d): %v", resp.StatusCode, err)
		}
	}
	return updates, nil
}
