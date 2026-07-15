// Package telegram provides a minimal Telegram Bot API client for the
// Master's long-polling loop.
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// Client is a Telegram Bot API client.
type Client struct {
	token  string
	apiURL string
	http   *http.Client
}

// New creates a Telegram client for the given bot token.
func New(token string) *Client {
	return &Client{
		token:  token,
		apiURL: "https://api.telegram.org/bot" + token,
		http:   &http.Client{Timeout: 60 * time.Second},
	}
}

// --- API types ---

// Update represents a Telegram update.
type Update struct {
	UpdateID      int64          `json:"update_id"`
	Message       *Message       `json:"message"`
	CallbackQuery *CallbackQuery `json:"callback_query"`
}

type Message struct {
	MessageID int64    `json:"message_id"`
	From      *User    `json:"from"`
	Chat      *Chat    `json:"chat"`
	Date      int64    `json:"date"`
	Text      string   `json:"text"`
	ReplyTo   *Message `json:"reply_to_message"`
}

type CallbackQuery struct {
	ID      string   `json:"id"`
	From    *User    `json:"from"`
	Message *Message `json:"message"`
	Data    string   `json:"data"`
}

type User struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	UserName  string `json:"username"`
}

type Chat struct {
	ID    int64  `json:"id"`
	Type  string `json:"type"`
	Title string `json:"title"`
}

// --- API methods ---

// GetUpdates long-polls for updates.
func (c *Client) GetUpdates(ctx context.Context, offset int64, timeout int) ([]Update, error) {
	v := url.Values{}
	v.Set("offset", fmt.Sprintf("%d", offset))
	v.Set("timeout", fmt.Sprintf("%d", timeout))
	v.Set("limit", "100")

	resp, err := c.apiWithForm(ctx, "getUpdates", v)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result struct {
		OK     bool     `json:"ok"`
		Result []Update `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("telegram: decode getUpdates: %w", err)
	}
	return result.Result, nil
}

// SendMessage sends a text message.
func (c *Client) SendMessage(ctx context.Context, chatID int64, text string) (*Message, error) {
	v := url.Values{}
	v.Set("chat_id", fmt.Sprintf("%d", chatID))
	v.Set("text", text)
	v.Set("parse_mode", "Markdown")

	resp, err := c.apiWithForm(ctx, "sendMessage", v)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result struct {
		OK     bool    `json:"ok"`
		Result Message `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("telegram: decode sendMessage: %w", err)
	}
	if !result.OK {
		return nil, fmt.Errorf("telegram: sendMessage failed")
	}
	return &result.Result, nil
}

// InlineKeyboardButton is a button in an inline keyboard.
type InlineKeyboardButton struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data"`
}

// InlineKeyboardMarkup is a set of inline keyboard rows.
type InlineKeyboardMarkup struct {
	InlineKeyboard [][]InlineKeyboardButton `json:"inline_keyboard"`
}

// SendMessageWithKeyboard sends a text message with an inline keyboard.
func (c *Client) SendMessageWithKeyboard(ctx context.Context, chatID int64, text string, kb InlineKeyboardMarkup) (*Message, error) {
	kbJSON, err := json.Marshal(kb)
	if err != nil {
		return nil, fmt.Errorf("telegram: marshal keyboard: %w", err)
	}
	v := url.Values{}
	v.Set("chat_id", fmt.Sprintf("%d", chatID))
	v.Set("text", text)
	v.Set("parse_mode", "Markdown")
	v.Set("reply_markup", string(kbJSON))

	resp, err := c.apiWithForm(ctx, "sendMessage", v)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result struct {
		OK     bool    `json:"ok"`
		Result Message `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("telegram: decode sendMessage: %w", err)
	}
	if !result.OK {
		return nil, fmt.Errorf("telegram: sendMessage failed")
	}
	return &result.Result, nil
}

// AnswerCallbackQuery acknowledges a callback query.
func (c *Client) AnswerCallbackQuery(ctx context.Context, callbackID, text string) error {
	v := url.Values{}
	v.Set("callback_query_id", callbackID)
	v.Set("text", text)
	v.Set("show_alert", "false")

	resp, err := c.apiWithForm(ctx, "answerCallbackQuery", v)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

// EditMessageText edits a previously sent message.
func (c *Client) EditMessageText(ctx context.Context, chatID, messageID int64, text string, kb *InlineKeyboardMarkup) error {
	v := url.Values{}
	v.Set("chat_id", fmt.Sprintf("%d", chatID))
	v.Set("message_id", fmt.Sprintf("%d", messageID))
	v.Set("text", text)
	v.Set("parse_mode", "Markdown")
	if kb != nil {
		kbJSON, _ := json.Marshal(kb)
		v.Set("reply_markup", string(kbJSON))
	}

	resp, err := c.apiWithForm(ctx, "editMessageText", v)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

// DeleteMessage deletes a message.
func (c *Client) DeleteMessage(ctx context.Context, chatID, messageID int64) error {
	v := url.Values{}
	v.Set("chat_id", fmt.Sprintf("%d", chatID))
	v.Set("message_id", fmt.Sprintf("%d", messageID))

	resp, err := c.apiWithForm(ctx, "deleteMessage", v)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

// --- internals ---

func (c *Client) apiWithForm(ctx context.Context, method string, v url.Values) (*http.Response, error) {
	u := c.apiURL + "/" + method
	req, err := http.NewRequestWithContext(ctx, "POST", u,
		bytes.NewBufferString(v.Encode()))
	if err != nil {
		return nil, fmt.Errorf("telegram: %s: %w", method, err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("telegram: %s: %w", method, err)
	}
	return resp, nil
}

// PollLoop runs the getUpdates long-polling loop, calling handler for
// each update. Blocks until ctx is cancelled.
func (c *Client) PollLoop(ctx context.Context, getOffset func() int64, setOffset func(int64), handler func(Update)) {
	for {
		if ctx.Err() != nil {
			return
		}
		offset := getOffset()
		updates, err := c.GetUpdates(ctx, offset, 30)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			time.Sleep(5 * time.Second)
			continue
		}
		for _, upd := range updates {
			handler(upd)
			if upd.UpdateID >= offset {
				offset = upd.UpdateID + 1
			}
		}
		if offset > getOffset() {
			setOffset(offset)
		}
	}
}
