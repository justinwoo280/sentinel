// Package telegram provides a minimal Telegram Bot API client for the
// Master's long-polling loop.
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
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
		OK          bool     `json:"ok"`
		ErrorCode   int      `json:"error_code"`
		Description string   `json:"description"`
		Result      []Update `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("telegram: decode getUpdates: %w", err)
	}
	// Telegram returns HTTP 200 with ok:false for auth/conflict errors
	// (e.g. 401 invalid token, 409 another poller running). Surface them.
	if !result.OK {
		return nil, fmt.Errorf("telegram: getUpdates rejected (code %d): %s",
			result.ErrorCode, result.Description)
	}
	return result.Result, nil
}

// GetMe returns the bot's own identity, used to validate the token at
// startup and to log which bot is running.
func (c *Client) GetMe(ctx context.Context) (*User, error) {
	resp, err := c.apiWithForm(ctx, "getMe", url.Values{})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var result struct {
		OK          bool   `json:"ok"`
		ErrorCode   int    `json:"error_code"`
		Description string `json:"description"`
		Result      *User  `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("telegram: decode getMe: %w", err)
	}
	if !result.OK {
		return nil, fmt.Errorf("telegram: getMe rejected (code %d): %s",
			result.ErrorCode, result.Description)
	}
	return result.Result, nil
}

// SendMessage sends a text message. It tries Markdown first and falls
// back to plain text if Telegram rejects the markup, so a message with
// unbalanced markdown characters is never silently dropped.
func (c *Client) SendMessage(ctx context.Context, chatID int64, text string) (*Message, error) {
	return c.sendMessage(ctx, chatID, text, nil)
}

// sendMessage is the shared implementation for SendMessage and
// SendMessageWithKeyboard. kbJSON may be nil (no keyboard).
func (c *Client) sendMessage(ctx context.Context, chatID int64, text string, kbJSON []byte) (*Message, error) {
	send := func(markdown bool) (*Message, string, error) {
		v := url.Values{}
		v.Set("chat_id", fmt.Sprintf("%d", chatID))
		v.Set("text", text)
		if markdown {
			v.Set("parse_mode", "Markdown")
		}
		if kbJSON != nil {
			v.Set("reply_markup", string(kbJSON))
		}
		resp, err := c.apiWithForm(ctx, "sendMessage", v)
		if err != nil {
			return nil, "", err
		}
		defer resp.Body.Close()
		var result struct {
			OK          bool    `json:"ok"`
			ErrorCode   int     `json:"error_code"`
			Description string  `json:"description"`
			Result      Message `json:"result"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return nil, "", fmt.Errorf("telegram: decode sendMessage: %w", err)
		}
		if !result.OK {
			return nil, result.Description, fmt.Errorf(
				"telegram: sendMessage failed (code %d): %s", result.ErrorCode, result.Description)
		}
		return &result.Result, "", nil
	}

	msg, _, err := send(true)
	if err == nil {
		return msg, nil
	}
	// Markdown rejected (typically a 400 "can't parse entities"): retry as
	// plain text so the user still receives the message.
	msg2, _, err2 := send(false)
	if err2 != nil {
		return nil, err // report the original error
	}
	return msg2, nil
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
	return c.sendMessage(ctx, chatID, text, kbJSON)
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

// BotCommand is an entry in the Telegram command menu.
type BotCommand struct {
	Command     string `json:"command"`
	Description string `json:"description"`
}

// SetMyCommands registers the bot's command menu (the "/" suggestions).
func (c *Client) SetMyCommands(ctx context.Context, cmds []BotCommand) error {
	body, err := json.Marshal(cmds)
	if err != nil {
		return err
	}
	v := url.Values{}
	v.Set("commands", string(body))
	resp, err := c.apiWithForm(ctx, "setMyCommands", v)
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
// each update. Blocks until ctx is cancelled. Errors are logged (with
// backoff) so a bad token or a conflicting poller is visible.
func (c *Client) PollLoop(ctx context.Context, getOffset func() int64, setOffset func(int64), handler func(Update), log *slog.Logger) {
	if log == nil {
		log = slog.Default()
	}
	var failures int
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
			failures++
			// Log the first failure immediately and then periodically to
			// avoid flooding, so operators see *why* the bot is silent.
			if failures == 1 || failures%12 == 0 {
				log.Error("telegram getUpdates failed", "err", err, "consecutive_failures", failures)
			}
			time.Sleep(5 * time.Second)
			continue
		}
		failures = 0
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
