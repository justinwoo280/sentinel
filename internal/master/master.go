// Package master implements the Master lifecycle: ties together the EWP
// control server, Telegram bot, SQLite store, and command dispatch.
package master

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/justinwoo280/sentinel/internal/config"
	"github.com/justinwoo280/sentinel/internal/ctrl"
	"github.com/justinwoo280/sentinel/internal/master/store"
	"github.com/justinwoo280/sentinel/internal/master/telegram"
	"github.com/justinwoo280/sentinel/internal/master/ui"
	"github.com/justinwoo280/sentinel/internal/protocol"
	ewp "github.com/justinwoo280/sing-ewp"
)

// helpText is shown by /help. It explains the text commands and what each
// node-panel button does.
const helpText = "*Sentinel Master — Help*\n\n" +
	"*Commands*\n" +
	"/start or /menu — open the main panel\n" +
	"/help — show this help\n" +
	"/register <SENTINEL-REG:...> — register a new agent (paste the blob printed by `sentinel install`)\n\n" +
	"*Main menu*\n" +
	"Node List — browse nodes by region\n" +
	"Global Keepalive — run one keepalive cycle on every node\n" +
	"Global Report — collect a status report from every node\n" +
	"Global OTA — self-update every online node (confirm required)\n\n" +
	"*Node panel*\n" +
	"Keepalive — run one keepalive cycle now (70% Google / 30% Trust, same as the automatic timer)\n" +
	"Google — force a Google region-correction session now (fingerprint + coords + Search/News/Maps + CN-detection probes)\n" +
	"Trust — force a reputation-warmup session now (visits regional high-reputation sites)\n" +
	"Quality — run a full IP quality check and return the report\n" +
	"Trend — view recorded quality trend history\n" +
	"Log — fetch the node's recent logs\n" +
	"Google/Trust ON·OFF — enable/disable that keepalive module\n" +
	"Rename — change the node's display alias\n" +
	"OTA — trigger self-update (confirm required)\n" +
	"Delete — remove the node (confirm required)\n\n" +
	"Google/Trust/Keepalive are the maintenance actions that gradually improve a mis-located ('sent to CN') IP; " +
	"the agent also runs them automatically on a timer — the buttons just trigger one immediately."

// Master is the control-plane runtime.
type Master struct {
	cfg           config.MasterConfig
	log           *slog.Logger
	store         *store.Store
	srv           *ctrl.Server
	tg            *telegram.Client
	pendingRename map[int64]string // chat_id → UUID (awaiting rename reply)
	renameMu      sync.Mutex
	qualityTokens map[string]*qualityToken // short token → quality metrics (for "save to trend DB" button)
	qualityMu     sync.Mutex
	qualitySeq    uint64
	admins        map[int64]bool // authorized Telegram user IDs (allowlist)
}

// isAuthorized reports whether the given Telegram user ID is on the admin
// allowlist. An empty allowlist is fail-closed: nobody is authorized.
func (m *Master) isAuthorized(userID int64) bool {
	return m.admins[userID]
}

// qualityToken holds metrics extracted from a quality result, keyed by
// a short token embedded in the Telegram callback_data (DESIGN.md §6.3).
type qualityToken struct {
	NodeName   string
	ScamScore  int
	GoogStatus string
	NfStatus   string
	GptStatus  string
	ChatID     int64
	CreatedAt  time.Time
}

// New creates and initialises a Master.
func New(cfg config.MasterConfig, log *slog.Logger) (*Master, error) {
	if log == nil {
		log = slog.Default()
	}

	st, err := store.Open(cfg.Store.Path)
	if err != nil {
		return nil, fmt.Errorf("master: open store: %w", err)
	}

	// Load or generate static private key.
	privB64, err := os.ReadFile(cfg.Control.StaticKeyPath)
	if err != nil {
		st.Close()
		return nil, fmt.Errorf("master: read static key (run 'sentinel master init' first): %w", err)
	}
	privStr := string(privB64)

	srv, err := ctrl.NewServer(ctrl.ServerConfig{
		ListenAddr:    cfg.Control.Listen,
		StaticPrivB64: privStr,
	}, log)
	if err != nil {
		st.Close()
		return nil, fmt.Errorf("master: create ctrl server: %w", err)
	}

	// Load all registered UUIDs into EWP whitelist.
	uuids, err := st.ListAllUUIDs(context.Background())
	if err != nil {
		st.Close()
		return nil, fmt.Errorf("master: load uuids: %w", err)
	}
	for _, u := range uuids {
		if err := srv.AddAgent(u); err != nil {
			log.Warn("failed to add agent to whitelist", "uuid", u, "err", err)
		}
	}
	log.Info("loaded agents into whitelist", "count", len(uuids))

	var tg *telegram.Client
	if cfg.Telegram.Token != "" {
		tg = telegram.New(cfg.Telegram.Token)
	}

	admins := make(map[int64]bool, len(cfg.Telegram.AdminIDs))
	for _, id := range cfg.Telegram.AdminIDs {
		admins[id] = true
	}
	if tg != nil && len(admins) == 0 {
		log.Warn("telegram admin allowlist is EMPTY — the bot is fail-closed and " +
			"will deny everyone; set telegram.admin_ids in the config")
	}

	m := &Master{
		cfg:           cfg,
		log:           log,
		store:         st,
		srv:           srv,
		tg:            tg,
		pendingRename: make(map[int64]string),
		qualityTokens: make(map[string]*qualityToken),
		admins:        admins,
	}

	// Wire server event handler.
	srv.SetHandler(&masterHandler{m: m})

	return m, nil
}

// Run starts the EWP server and Telegram bot (if configured). Blocks
// until ctx is cancelled.
func (m *Master) Run(ctx context.Context) error {
	// Start EWP server in background.
	go func() {
		if err := m.srv.Listen(ctx); err != nil && ctx.Err() == nil {
			m.log.Error("ctrl server stopped", "err", err)
		}
	}()

	// Start Telegram bot if configured.
	if m.tg != nil {
		// Validate the token up front so a bad token is obvious in the
		// logs instead of silently failing to receive messages.
		if me, err := m.tg.GetMe(ctx); err != nil {
			m.log.Error("telegram token validation failed (bot will not work)", "err", err)
		} else {
			m.log.Info("telegram bot authenticated",
				"username", me.UserName, "id", me.ID, "admins", len(m.admins))
			if len(m.admins) == 0 {
				m.log.Warn("telegram admin allowlist is EMPTY — every message will be denied; " +
					"set telegram.admin_ids and restart")
			}
			// Register the command menu (the "/" suggestions).
			if err := m.tg.SetMyCommands(ctx, []telegram.BotCommand{
				{Command: "start", Description: "Open the main panel"},
				{Command: "menu", Description: "Open the main panel"},
				{Command: "help", Description: "Show help"},
				{Command: "register", Description: "Register a new agent (paste SENTINEL-REG blob)"},
			}); err != nil {
				m.log.Warn("failed to set telegram command menu", "err", err)
			}
		}
		go m.runTelegram(ctx)
	} else {
		m.log.Info("telegram not configured, skipping bot")
	}

	// Wait for shutdown.
	<-ctx.Done()
	m.log.Info("shutting down")
	m.store.Close()
	// A context cancellation is a normal, clean shutdown (e.g. SIGTERM
	// from systemd) — do not report it as an error / non-zero exit.
	if err := ctx.Err(); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

// runTelegram runs the Telegram long-polling loop.
func (m *Master) runTelegram(ctx context.Context) {
	getOffset := func() int64 {
		off, _ := m.store.GetTGOffset(ctx)
		return off
	}
	setOffset := func(off int64) {
		_ = m.store.SetTGOffset(ctx, off)
	}

	m.tg.PollLoop(ctx, getOffset, setOffset, m.handleUpdate, m.log)
}

// handleUpdate routes a Telegram update to the appropriate handler.
func (m *Master) handleUpdate(upd telegram.Update) {
	if upd.CallbackQuery != nil {
		m.handleCallback(upd.CallbackQuery)
		return
	}
	if upd.Message != nil {
		m.handleMessage(upd.Message)
		return
	}
}

func (m *Master) handleMessage(msg *telegram.Message) {
	if msg.Chat == nil {
		return
	}
	chatID := msg.Chat.ID

	// Authorization gate (fail-closed): only allowlisted user IDs may
	// operate the bot. Reply to unauthorized users with their ID so they
	// (or the operator) can add it to telegram.admin_ids.
	if msg.From == nil || !m.isAuthorized(msg.From.ID) {
		var uid int64
		if msg.From != nil {
			uid = msg.From.ID
		}
		m.log.Warn("rejected unauthorized telegram message",
			"user_id", uid, "chat_id", chatID)
		m.tg.SendMessage(context.Background(), chatID, fmt.Sprintf(
			"Unauthorized. Your Telegram user ID is `%d`.\n"+
				"Ask the operator to add it to `telegram.admin_ids` in the master config.", uid))
		return
	}

	switch {
	case msg.Text == "/start" || msg.Text == "/menu":
		m.sendMenu(chatID)
	case msg.Text == "/help":
		m.tg.SendMessage(context.Background(), chatID, helpText)
	case strings.HasPrefix(msg.Text, "/register"):
		m.handleRegister(chatID, msg.Text)
	case msg.ReplyTo != nil && strings.HasPrefix(msg.ReplyTo.Text, "Enter new alias"):
		m.handleRenameReply(chatID, msg)
	default:
		// Check if there's a pending rename (no reply_to but next message after rename prompt).
		m.renameMu.Lock()
		pendingUUID, hasPending := m.pendingRename[chatID]
		m.renameMu.Unlock()
		if hasPending && msg.Text != "" {
			m.processRename(chatID, pendingUUID, msg.Text)
		} else {
			m.sendMenu(chatID)
		}
	}
}

func (m *Master) handleCallback(cb *telegram.CallbackQuery) {
	ctx := context.Background()
	if cb.Message == nil || cb.Message.Chat == nil {
		return
	}
	chatID := cb.Message.Chat.ID

	// Authorization gate (fail-closed): gate on the pressing user (cb.From),
	// not the message chat. Ack first so the button spinner stops.
	if cb.From == nil || !m.isAuthorized(cb.From.ID) {
		var uid int64
		if cb.From != nil {
			uid = cb.From.ID
		}
		_ = m.tg.AnswerCallbackQuery(ctx, cb.ID, "Unauthorized")
		m.log.Warn("rejected unauthorized telegram callback",
			"user_id", uid, "chat_id", chatID, "data", cb.Data)
		return
	}

	// Acknowledge the callback.
	_ = m.tg.AnswerCallbackQuery(ctx, cb.ID, "")

	// Navigation actions edit the current message in place (rather than
	// spamming a new message each time), by editing cb.Message.
	msgID := cb.Message.MessageID
	edit := func(text string, kb telegram.InlineKeyboardMarkup) {
		if err := m.tg.EditMessageText(ctx, chatID, msgID, text, &kb); err != nil {
			// Fall back to a new message if the edit fails (e.g. message
			// too old to edit, or identical content).
			m.tg.SendMessageWithKeyboard(ctx, chatID, text, kb)
		}
	}

	parts := strings.Split(cb.Data, "|")
	action := parts[0]

	switch action {
	case ui.CBMenu:
		nodes, _ := m.store.ListAllNodes(ctx)
		text, kb := ui.MainMenu(m.cfg.Master.Version, len(nodes))
		edit(text, kb)

	case ui.CBList:
		nodes, err := m.store.ListAllNodes(ctx)
		if err != nil {
			m.tg.SendMessage(ctx, chatID, "Error: "+err.Error())
			return
		}
		text, kb := ui.NodeList(nodes)
		edit(text, kb)

	case ui.CBNode:
		if len(parts) < 2 {
			return
		}
		uuid := parts[1]
		node, err := m.store.GetNodeByUUID(ctx, uuid)
		if err != nil {
			m.tg.SendMessage(ctx, chatID, "Node not found")
			return
		}
		online := m.srv.IsOnline(parseUUID(uuid))
		text, kb := ui.NodePanel(node, online)
		edit(text, kb)

	case ui.CBRun, ui.CBGoogle, ui.CBTrust, ui.CBQuality, ui.CBReport, ui.CBLog:
		m.dispatchToAgent(ctx, chatID, action, parts)

	case ui.CBTrend:
		m.handleTrend(ctx, chatID, msgID, parts)

	case ui.CBToggle:
		m.handleToggle(ctx, chatID, msgID, parts)

	case ui.CBRename:
		if len(parts) < 2 {
			return
		}
		uuid := parts[1]
		m.renameMu.Lock()
		m.pendingRename[chatID] = uuid
		m.renameMu.Unlock()
		m.tg.SendMessage(ctx, chatID, "Enter new alias (reply to this message with the new name):")

	case ui.CBDelete:
		if len(parts) < 2 {
			return
		}
		uuid := parts[1]
		node, err := m.store.GetNodeByUUID(ctx, uuid)
		if err != nil {
			m.tg.SendMessage(ctx, chatID, "Node not found")
			return
		}
		text, kb := ui.ConfirmDelete(uuid, node.NodeName)
		edit(text, kb)

	case ui.CBDeleteYes:
		if len(parts) < 2 {
			return
		}
		uuid := parts[1]
		m.srv.RemoveAgent(uuid)
		if err := m.store.DeleteNode(ctx, uuid); err != nil {
			m.tg.SendMessage(ctx, chatID, "Delete failed: "+err.Error())
			return
		}
		m.tg.SendMessage(ctx, chatID, "Node deleted.")

	case ui.CBOTA:
		if len(parts) < 2 {
			// Global OTA: confirm for all online nodes.
			nodes, err := m.store.ListAllNodes(ctx)
			if err != nil {
				m.tg.SendMessage(ctx, chatID, "Error: "+err.Error())
				return
			}
			online := 0
			for _, n := range nodes {
				if m.srv.IsOnline(parseUUID(n.UUID)) {
					online++
				}
			}
			if online == 0 {
				m.tg.SendMessage(ctx, chatID, "No online nodes to upgrade.")
				return
			}
			text, kb := ui.ConfirmGlobalOTA(online)
			edit(text, kb)
		} else {
			// Single-node OTA confirmation.
			uuid := parts[1]
			node, err := m.store.GetNodeByUUID(ctx, uuid)
			if err != nil {
				m.tg.SendMessage(ctx, chatID, "Node not found")
				return
			}
			if !m.srv.IsOnline(parseUUID(uuid)) {
				m.tg.SendMessage(ctx, chatID, "Node is offline.")
				return
			}
			if !node.EnableOTA {
				m.tg.SendMessage(ctx, chatID, "OTA is disabled for this node.")
				return
			}
			text, kb := ui.ConfirmOTA(uuid, node.NodeName)
			edit(text, kb)
		}

	case ui.CBOTAYes:
		if len(parts) < 2 {
			// Global OTA: send to all online nodes.
			nodes, _ := m.store.ListAllNodes(ctx)
			sent, failed := 0, 0
			for _, n := range nodes {
				if !m.srv.IsOnline(parseUUID(n.UUID)) {
					continue
				}
				if !n.EnableOTA {
					continue
				}
				msg := ctrl.CommandMessage{
					ID:  fmt.Sprintf("%d", time.Now().UnixNano()),
					Cmd: ctrl.CmdOTA,
				}
				if err := m.srv.SendCommand(parseUUID(n.UUID), msg); err != nil {
					failed++
					m.log.Warn("global OTA: send failed", "node", n.NodeName, "err", err)
				} else {
					sent++
				}
			}
			m.tg.SendMessage(ctx, chatID,
				fmt.Sprintf("Global OTA: %d nodes notified, %d failed.", sent, failed))
		} else {
			// Single-node OTA.
			uuid := parts[1]
			node, err := m.store.GetNodeByUUID(ctx, uuid)
			if err != nil {
				m.tg.SendMessage(ctx, chatID, "Node not found")
				return
			}
			if !node.EnableOTA {
				m.tg.SendMessage(ctx, chatID, "OTA is disabled for this node.")
				return
			}
			msg := ctrl.CommandMessage{
				ID:  fmt.Sprintf("%d", time.Now().UnixNano()),
				Cmd: ctrl.CmdOTA,
			}
			if err := m.srv.SendCommand(parseUUID(uuid), msg); err != nil {
				m.tg.SendMessage(ctx, chatID, "Failed to send OTA command: "+err.Error())
				return
			}
			m.tg.SendMessage(ctx, chatID,
				fmt.Sprintf("OTA command sent to %s.", node.NodeName))
		}

	case ui.CBSaveTrend:
		if len(parts) < 2 {
			return
		}
		token := parts[1]
		m.qualityMu.Lock()
		qt, ok := m.qualityTokens[token]
		if ok {
			delete(m.qualityTokens, token)
		}
		m.qualityMu.Unlock()
		if !ok {
			m.tg.SendMessage(ctx, chatID, "Token expired or invalid.")
			return
		}
		if err := m.store.InsertTrend(ctx, store.TrendEntry{
			NodeName:   qt.NodeName,
			ScamScore:  qt.ScamScore,
			GoogStatus: qt.GoogStatus,
			NfStatus:   qt.NfStatus,
			GptStatus:  qt.GptStatus,
		}); err != nil {
			m.tg.SendMessage(ctx, chatID, "Failed to save trend: "+err.Error())
			return
		}
		m.tg.SendMessage(ctx, chatID,
			fmt.Sprintf("Trend saved for %s (scam=%d, google=%s, netflix=%s, chatgpt=%s).",
				qt.NodeName, qt.ScamScore, qt.GoogStatus, qt.NfStatus, qt.GptStatus))

	case ui.CBBack:
		m.sendMenu(chatID)

	default:
		m.log.Warn("unknown callback action", "data", cb.Data)
	}
}

// dispatchToAgent sends a command to an agent via the control channel.
func (m *Master) dispatchToAgent(ctx context.Context, chatID int64, action string, parts []string) {
	if len(parts) < 2 {
		m.tg.SendMessage(ctx, chatID, "Missing node UUID")
		return
	}
	uuid := parts[1]
	uuidBytes := parseUUID(uuid)

	if !m.srv.IsOnline(uuidBytes) {
		m.tg.SendMessage(ctx, chatID, "Node is offline.")
		return
	}

	cmd, err := actionToCommand(action)
	if err != nil {
		m.tg.SendMessage(ctx, chatID, err.Error())
		return
	}

	msg := ctrl.CommandMessage{
		ID:  fmt.Sprintf("%d", time.Now().UnixNano()),
		Cmd: cmd,
	}

	m.log.Info("dispatching command to agent", "uuid", uuid, "cmd", cmd, "id", msg.ID)
	if err := m.srv.SendCommand(uuidBytes, msg); err != nil {
		m.log.Warn("dispatch failed", "uuid", uuid, "cmd", cmd, "err", err)
		m.tg.SendMessage(ctx, chatID, "Failed to send command: "+err.Error())
		return
	}
	m.tg.SendMessage(ctx, chatID, "Command sent to node.")
}

func (m *Master) handleTrend(ctx context.Context, chatID, msgID int64, parts []string) {
	back := telegram.InlineKeyboardMarkup{}
	if len(parts) >= 2 {
		back = telegram.InlineKeyboardMarkup{
			InlineKeyboard: [][]telegram.InlineKeyboardButton{
				{{Text: "Back", CallbackData: fmt.Sprintf("%s|%s", ui.CBNode, parts[1])}},
			},
		}
	}
	editOrSend := func(text string) {
		if err := m.tg.EditMessageText(ctx, chatID, msgID, text, &back); err != nil {
			m.tg.SendMessageWithKeyboard(ctx, chatID, text, back)
		}
	}

	if len(parts) < 2 {
		m.tg.SendMessage(ctx, chatID, "Missing node UUID")
		return
	}
	uuid := parts[1]
	node, err := m.store.GetNodeByUUID(ctx, uuid)
	if err != nil {
		m.tg.SendMessage(ctx, chatID, "Node not found")
		return
	}
	trends, err := m.store.GetTrends(ctx, node.NodeName, 10)
	if err != nil {
		editOrSend("Error fetching trends: " + err.Error())
		return
	}
	if len(trends) == 0 {
		editOrSend(fmt.Sprintf("*Trend: %s*\n\nNo trend data for this node yet.", node.NodeName))
		return
	}

	report := fmt.Sprintf("*Trend: %s*\n\n", node.NodeName)
	report += "```\n"
	report += "Time                Scam  Google  Netflix  ChatGPT\n"
	for _, t := range trends {
		report += fmt.Sprintf("%-19s %4d  %-6s  %-8s  %s\n",
			"recent", t.ScamScore,
			truncStr(t.GoogStatus, 6),
			truncStr(t.NfStatus, 8),
			truncStr(t.GptStatus, 7),
		)
	}
	report += "```\n"

	editOrSend(report)
}

func truncStr(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func (m *Master) handleToggle(ctx context.Context, chatID, msgID int64, parts []string) {
	if len(parts) < 4 {
		m.tg.SendMessage(ctx, chatID, "Invalid toggle parameters")
		return
	}
	uuid := parts[1]
	mod := parts[2]
	state := parts[3] == "true"

	if err := m.store.ToggleModule(ctx, uuid, mod, state); err != nil {
		m.tg.SendMessage(ctx, chatID, "Toggle failed: "+err.Error())
		return
	}

	// Also send config.toggle to the agent if online.
	uuidBytes := parseUUID(uuid)
	if m.srv.IsOnline(uuidBytes) {
		stateBool := state
		if err := m.srv.SendCommand(uuidBytes, ctrl.CommandMessage{
			ID:     fmt.Sprintf("%d", time.Now().UnixNano()),
			Cmd:    ctrl.CmdConfigToggle,
			Params: ctrl.Params{Mod: mod, State: &stateBool},
		}); err != nil {
			m.log.Warn("failed to push toggle to agent", "uuid", uuid, "err", err)
		}
	}

	// Refresh the node panel in place so the ON/OFF buttons update.
	node, err := m.store.GetNodeByUUID(ctx, uuid)
	if err != nil {
		m.tg.SendMessage(ctx, chatID, fmt.Sprintf("Module %s set to %v.", mod, state))
		return
	}
	online := m.srv.IsOnline(uuidBytes)
	text, kb := ui.NodePanel(node, online)
	if err := m.tg.EditMessageText(ctx, chatID, msgID, text, &kb); err != nil {
		m.tg.SendMessageWithKeyboard(ctx, chatID, text, kb)
	}
}

// extractRegBlob pulls the SENTINEL-REG: blob out of a raw message. The
// message may be "/register SENTINEL-REG:...", a bare "SENTINEL-REG:...",
// or have surrounding whitespace/newlines. Returns the text from the
// prefix onward so protocol.Decode (which requires the string to start
// with the prefix) accepts it. If the prefix is absent, returns the
// trimmed input unchanged (Decode will then produce a clear error).
func extractRegBlob(text string) string {
	s := strings.TrimSpace(text)
	if idx := strings.Index(s, protocol.RegPrefix); idx >= 0 {
		return strings.TrimSpace(s[idx:])
	}
	return s
}

func (m *Master) handleRegister(chatID int64, text string) {
	ctx := context.Background()

	reg, err := protocol.Decode(extractRegBlob(text))
	if err != nil {
		m.tg.SendMessage(ctx, chatID, "Invalid registration: "+err.Error())
		return
	}

	// Add to EWP whitelist.
	if err := m.srv.AddAgent(reg.UUID); err != nil {
		m.tg.SendMessage(ctx, chatID, "Failed to add agent: "+err.Error())
		return
	}

	// Insert into DB.
	err = m.store.RegisterNode(ctx, store.Node{
		UUID:         reg.UUID,
		ChatID:       fmt.Sprintf("%d", chatID),
		NodeName:     reg.Node,
		NodeAlias:    reg.Alias,
		Region:       reg.Region,
		EnableGoogle: true,
		EnableTrust:  true,
		EnableOTA:    reg.OTA,
	})
	if err != nil {
		m.tg.SendMessage(ctx, chatID, "Registration failed (may already exist): "+err.Error())
		return
	}

	m.tg.SendMessage(ctx, chatID,
		fmt.Sprintf("Agent registered: %s (%s) in %s", reg.Node, reg.Alias, reg.Region))
}

func (m *Master) handleRenameReply(chatID int64, msg *telegram.Message) {
	ctx := context.Background()
	m.renameMu.Lock()
	uuid, ok := m.pendingRename[chatID]
	m.renameMu.Unlock()
	if !ok {
		m.tg.SendMessage(ctx, chatID, "No pending rename. Use the Rename button first.")
		return
	}
	m.processRename(chatID, uuid, msg.Text)
}

func (m *Master) processRename(chatID int64, uuid, alias string) {
	ctx := context.Background()
	defer func() {
		m.renameMu.Lock()
		delete(m.pendingRename, chatID)
		m.renameMu.Unlock()
	}()

	alias = strings.TrimSpace(alias)
	if len([]rune(alias)) > 20 {
		m.tg.SendMessage(ctx, chatID, "Alias too long (max 20 characters).")
		return
	}

	// Update store.
	if err := m.store.UpdateAlias(ctx, uuid, alias); err != nil {
		m.tg.SendMessage(ctx, chatID, "Rename failed: "+err.Error())
		return
	}

	// Send config.rename to agent if online.
	uuidBytes := parseUUID(uuid)
	if m.srv.IsOnline(uuidBytes) {
		if err := m.srv.SendCommand(uuidBytes, ctrl.CommandMessage{
			ID:     fmt.Sprintf("%d", time.Now().UnixNano()),
			Cmd:    ctrl.CmdConfigRename,
			Params: ctrl.Params{Alias: alias},
		}); err != nil {
			m.log.Warn("failed to push rename to agent", "uuid", uuid, "err", err)
		}
	}

	m.tg.SendMessage(ctx, chatID, fmt.Sprintf("Alias updated to: %s", alias))
}

func (m *Master) sendMenu(chatID int64) {
	ctx := context.Background()
	nodes, _ := m.store.ListAllNodes(ctx)
	text, kb := ui.MainMenu(m.cfg.Master.Version, len(nodes))
	m.tg.SendMessageWithKeyboard(ctx, chatID, text, kb)
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func actionToCommand(action string) (ctrl.Command, error) {
	switch action {
	case ui.CBRun:
		return ctrl.CmdRun, nil
	case ui.CBGoogle:
		return ctrl.CmdModGoogle, nil
	case ui.CBTrust:
		return ctrl.CmdModTrust, nil
	case ui.CBQuality:
		return ctrl.CmdModQuality, nil
	case ui.CBReport:
		return ctrl.CmdReport, nil
	case ui.CBLog:
		return ctrl.CmdLog, nil
	default:
		return "", fmt.Errorf("unknown action: %s", action)
	}
}

func parseUUID(s string) [ewp.UUIDLen]byte {
	uuid, err := ewp.ParseUUID(s)
	if err != nil {
		return [ewp.UUIDLen]byte{}
	}
	return uuid
}

// uuidCanonical formats 16 UUID bytes in the canonical hyphenated form
// (8-4-4-4-12), matching what the registration blob stores in the DB.
// Using this consistently avoids the mismatch between the hyphenated
// string from registration and the raw bytes from the EWP handshake.
func uuidCanonical(u [ewp.UUIDLen]byte) string {
	return fmt.Sprintf("%x-%x-%x-%x-%x", u[0:4], u[4:6], u[6:8], u[8:10], u[10:16])
}

// extractScamScore returns a normalised 0-100 risk score from the quality
// Score map. Sources report in different formats, so it tries them in
// priority order and scales to 0-100:
//   - Scamalytics / IPQS / AbuseIPDB / IP2Location: integer 0-100
//   - ipapi abuser_score: float 0.0-1.0 (e.g. "0.0868 (High)") → ×100
//
// Returns 0 if no source yields a parseable number.
func extractScamScore(score map[string]*string) int {
	get := func(k string) string {
		if v, ok := score[k]; ok && v != nil {
			return *v
		}
		return ""
	}
	// Commercial 0-100 sources first (present only with API keys).
	for _, k := range []string{"SCAMALYTICS", "IPQS", "AbuseIPDB", "IP2LOCATION"} {
		if s := get(k); s != "" {
			if n, ok := parseLeadingFloat(s); ok {
				return clampScore(n)
			}
		}
	}
	// Free ipapi abuser_score is a 0.0-1.0 fraction → scale to 0-100.
	if s := get("ipapi"); s != "" {
		if n, ok := parseLeadingFloat(s); ok {
			if n <= 1.0 {
				n *= 100
			}
			return clampScore(n)
		}
	}
	return 0
}

// parseLeadingFloat parses the leading numeric part of a string like
// "0.0868 (High)" or "37%" → 0.0868 / 37.
func parseLeadingFloat(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	end := 0
	for end < len(s) {
		c := s[end]
		if (c >= '0' && c <= '9') || c == '.' || (end == 0 && (c == '-' || c == '+')) {
			end++
			continue
		}
		break
	}
	if end == 0 {
		return 0, false
	}
	f, err := strconv.ParseFloat(s[:end], 64)
	if err != nil {
		return 0, false
	}
	return f, true
}

func clampScore(f float64) int {
	n := int(f + 0.5)
	if n < 0 {
		n = 0
	}
	if n > 100 {
		n = 100
	}
	return n
}

// unwrapResultData extracts the inner payload from a ctrl.Result envelope
// ({ok, msg, data}). Agent command results wrap their real payload under
// "data"; the quality/report handlers need that inner JSON, not the
// envelope. If the input is not an envelope with a "data" field, the
// original bytes are returned unchanged.
func unwrapResultData(raw json.RawMessage) json.RawMessage {
	var env struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err == nil && len(env.Data) > 0 {
		return env.Data
	}
	return raw
}

// ---------------------------------------------------------------------------
// masterHandler implements ctrl.ServerHandler to receive agent events
// and sync them to the store / Telegram.
// ---------------------------------------------------------------------------

type masterHandler struct {
	m *Master
}

func (h *masterHandler) OnHello(uuid [ewp.UUIDLen]byte, hd ctrl.HelloData) {
	ctx := context.Background()
	uuidStr := uuidCanonical(uuid)

	// Try to find the node by UUID. If it exists, update its info.
	// If it doesn't exist (unregistered), log a warning.
	node, err := h.m.store.GetNodeByUUID(ctx, uuidStr)
	if err != nil {
		h.m.log.Warn("hello from unregistered agent",
			"uuid", uuidStr, "node", hd.Node)
		return
	}

	// Update node info from hello.
	node.IP = hd.IP
	node.Region = hd.Region
	if hd.Alias != "" {
		node.NodeAlias = hd.Alias
	}
	node.Version = hd.Version
	node.EnableGoogle = hd.Google
	node.EnableTrust = hd.Trust
	node.LastSeen = time.Now()

	if err := h.m.store.UpdateNode(ctx, uuidStr, *node); err != nil {
		h.m.log.Warn("failed to update node on hello", "err", err)
	} else {
		h.m.log.Info("node info updated from hello",
			"node", hd.Node, "ip", hd.IP, "region", hd.Region)
	}
}

func (h *masterHandler) OnHeartbeat(uuid [ewp.UUIDLen]byte) {
	ctx := context.Background()
	uuidStr := uuidCanonical(uuid)

	// Update last_seen only (lightweight).
	node, err := h.m.store.GetNodeByUUID(ctx, uuidStr)
	if err != nil {
		return
	}
	node.LastSeen = time.Now()
	if err := h.m.store.UpdateNode(ctx, uuidStr, *node); err != nil {
		h.m.log.Debug("failed to update last_seen on heartbeat", "err", err)
	}
}

func (h *masterHandler) OnResult(uuid [ewp.UUIDLen]byte, ev ctrl.EventMessage) {
	h.m.log.Info("command result received",
		"uuid", uuidCanonical(uuid), "id", ev.ID,
		"data_len", len(ev.Data))
}

func (h *masterHandler) OnQuality(uuid [ewp.UUIDLen]byte, ev ctrl.EventMessage) {
	ctx := context.Background()
	uuidStr := uuidCanonical(uuid)

	h.m.log.Info("quality result received", "uuid", uuidStr,
		"data_len", len(ev.Data))

	// The event payload is a ctrl.Result envelope: {ok, msg, data}. The
	// actual quality JSON is nested under .data — unwrap it first.
	qualityJSON := unwrapResultData(ev.Data)

	// Parse quality result to extract key metrics for trend logging.
	var qr struct {
		Score map[string]*string `json:"Score"`
		Media struct {
			Youtube struct {
				Status *string `json:"Status"`
				Region *string `json:"Region"`
			} `json:"Youtube"`
			Netflix struct {
				Status *string `json:"Status"`
			} `json:"Netflix"`
			ChatGPT struct {
				Status *string `json:"Status"`
			} `json:"ChatGPT"`
		} `json:"Media"`
	}
	if err := json.Unmarshal(qualityJSON, &qr); err != nil {
		h.m.log.Warn("failed to parse quality result", "err", err)
		return
	}

	// Get node name for trend log.
	node, err := h.m.store.GetNodeByUUID(ctx, uuidStr)
	if err != nil {
		return
	}

	// Extract a 0-100 risk score, preferring commercial sources but
	// falling back to the free ones so the trend is not always 0 when no
	// API keys are configured.
	scamScore := extractScamScore(qr.Score)
	googStatus := ptrStrOr(qr.Media.Youtube.Region, "")
	if qr.Media.Youtube.Status != nil {
		googStatus = *qr.Media.Youtube.Status
	}
	nfStatus := ""
	if qr.Media.Netflix.Status != nil {
		nfStatus = *qr.Media.Netflix.Status
	}
	gptStatus := ""
	if qr.Media.ChatGPT.Status != nil {
		gptStatus = *qr.Media.ChatGPT.Status
	}

	// Forward quality report to Telegram if bot is configured.
	if h.m.tg != nil && node.ChatID != "" {
		var chatID int64
		fmt.Sscanf(node.ChatID, "%d", &chatID)
		if chatID != 0 {
			// Store metrics in token registry for later "save to trend DB".
			token := h.m.genQualityToken()
			h.m.qualityMu.Lock()
			h.m.qualityTokens[token] = &qualityToken{
				NodeName:   node.NodeName,
				ScamScore:  scamScore,
				GoogStatus: googStatus,
				NfStatus:   nfStatus,
				GptStatus:  gptStatus,
				ChatID:     chatID,
				CreatedAt:  time.Now(),
			}
			h.m.qualityMu.Unlock()

			// Clean up old tokens (>10 minutes).
			h.m.cleanupQualityTokens()

			report := buildQualityTelegramReport(node, ev.Data)
			kb := telegram.InlineKeyboardMarkup{
				InlineKeyboard: [][]telegram.InlineKeyboardButton{
					{{Text: "Save to Trend DB", CallbackData: fmt.Sprintf("%s|%s", ui.CBSaveTrend, token)}},
				},
			}
			h.m.tg.SendMessageWithKeyboard(ctx, chatID, report, kb)
		}
	}
}

func (h *masterHandler) OnReport(uuid [ewp.UUIDLen]byte, ev ctrl.EventMessage) {
	ctx := context.Background()
	uuidStr := uuidCanonical(uuid)

	h.m.log.Info("report received", "uuid", uuidStr)

	// Forward report to Telegram if bot is configured.
	node, err := h.m.store.GetNodeByUUID(ctx, uuidStr)
	if err != nil || h.m.tg == nil || node.ChatID == "" {
		return
	}
	var chatID int64
	fmt.Sscanf(node.ChatID, "%d", &chatID)
	if chatID != 0 {
		h.m.tg.SendMessage(ctx, chatID, "*Report: "+node.NodeName+"*\n```\n"+resultText(ev.Data)+"\n```")
	}
}

// resultText pulls the human-readable text out of a ctrl.Result envelope.
// A report/log payload carries its text in "msg" (and sometimes "data").
func resultText(raw json.RawMessage) string {
	var env struct {
		Msg  string          `json:"msg"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return string(raw)
	}
	// Prefer data if it's a JSON string, else msg.
	if len(env.Data) > 0 {
		var s string
		if json.Unmarshal(env.Data, &s) == nil && s != "" {
			return s
		}
	}
	if env.Msg != "" {
		return env.Msg
	}
	return string(raw)
}

func (h *masterHandler) OnLog(uuid [ewp.UUIDLen]byte, ev ctrl.EventMessage) {
	ctx := context.Background()
	uuidStr := uuidCanonical(uuid)
	h.m.log.Info("log received from agent", "uuid", uuidStr, "data_len", len(ev.Data))

	node, err := h.m.store.GetNodeByUUID(ctx, uuidStr)
	if err != nil || h.m.tg == nil || node.ChatID == "" {
		return
	}
	var chatID int64
	fmt.Sscanf(node.ChatID, "%d", &chatID)
	if chatID != 0 {
		h.m.tg.SendMessage(ctx, chatID, "*Logs: "+node.NodeName+"*\n```\n"+resultText(ev.Data)+"\n```")
	}
}

func (h *masterHandler) OnDisconnect(uuid [ewp.UUIDLen]byte) {
	h.m.log.Info("agent went offline", "uuid", uuidCanonical(uuid))
}

// buildQualityTelegramReport formats a brief Telegram message from a
// quality check result for the node owner.
// buildQualityTelegramReport formats the quality JSON into a compact,
// readable Telegram report (not a raw JSON dump). `data` is the ctrl.Result
// envelope; the quality JSON is unwrapped from its .data field.
func buildQualityTelegramReport(node *store.Node, data json.RawMessage) string {
	var q struct {
		Info struct {
			ASN          *string `json:"ASN"`
			Organization *string `json:"Organization"`
			TimeZone     *string `json:"TimeZone"`
			Region       struct {
				Name *string `json:"Name"`
			} `json:"Region"`
			Continent struct {
				Name *string `json:"Name"`
			} `json:"Continent"`
		} `json:"Info"`
		Score map[string]*string `json:"Score"`
		Media map[string]struct {
			Status *string `json:"Status"`
			Region *string `json:"Region"`
		} `json:"Media"`
		Mail struct {
			Port25       *bool `json:"Port25"`
			DNSBlacklist struct {
				Total       int `json:"Total"`
				Clean       int `json:"Clean"`
				Marked      int `json:"Marked"`
				Blacklisted int `json:"Blacklisted"`
			} `json:"DNSBlacklist"`
		} `json:"Mail"`
	}
	if err := json.Unmarshal(unwrapResultData(data), &q); err != nil {
		// Fallback: at least don't crash — show a short note.
		return fmt.Sprintf("*Quality Report: %s*\n(could not parse result)", node.NodeName)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "*Quality Report: %s*\n", node.NodeName)

	// Info
	fmt.Fprintf(&b, "\n*Info*\n")
	fmt.Fprintf(&b, "ASN: %s\n", ptrStrOr(q.Info.ASN, "N/A"))
	fmt.Fprintf(&b, "Org: %s\n", ptrStrOr(q.Info.Organization, "N/A"))
	fmt.Fprintf(&b, "Region: %s / %s\n",
		ptrStrOr(q.Info.Region.Name, "N/A"), ptrStrOr(q.Info.Continent.Name, "N/A"))
	fmt.Fprintf(&b, "TimeZone: %s\n", ptrStrOr(q.Info.TimeZone, "N/A"))

	// Score (only non-null sources)
	fmt.Fprintf(&b, "\n*Risk Score*\n")
	scoreOrder := []string{"SCAMALYTICS", "IPQS", "AbuseIPDB", "IP2LOCATION", "DBIP", "ipapi"}
	any := false
	for _, k := range scoreOrder {
		if v, ok := q.Score[k]; ok && v != nil && *v != "" {
			fmt.Fprintf(&b, "%s: %s\n", k, *v)
			any = true
		}
	}
	if !any {
		fmt.Fprintf(&b, "(no scores — configure API keys)\n")
	}

	// Media
	fmt.Fprintf(&b, "\n*Streaming / AI*\n")
	mediaOrder := []string{"Netflix", "Youtube", "ChatGPT", "DisneyPlus", "AmazonPrimeVideo", "TikTok", "Reddit"}
	for _, k := range mediaOrder {
		if m, ok := q.Media[k]; ok {
			status := ptrStrOr(m.Status, "?")
			region := ptrStrOr(m.Region, "")
			if region != "" && region != "N/A" {
				fmt.Fprintf(&b, "%s: %s (%s)\n", k, status, region)
			} else {
				fmt.Fprintf(&b, "%s: %s\n", k, status)
			}
		}
	}

	// Mail
	p25 := "no"
	if q.Mail.Port25 != nil && *q.Mail.Port25 {
		p25 = "yes"
	}
	fmt.Fprintf(&b, "\n*Mail*\nPort25: %s | DNSBL: %d/%d clean, %d blacklisted\n",
		p25, q.Mail.DNSBlacklist.Clean, q.Mail.DNSBlacklist.Total, q.Mail.DNSBlacklist.Blacklisted)

	return b.String()
}

func ptrStrOr(s *string, fallback string) string {
	if s == nil || *s == "" {
		return fallback
	}
	return *s
}

// genQualityToken returns a short hex token for the quality-token registry.
func (m *Master) genQualityToken() string {
	m.qualityMu.Lock()
	defer m.qualityMu.Unlock()
	m.qualitySeq++
	return fmt.Sprintf("%x", m.qualitySeq)
}

// cleanupQualityTokens removes tokens older than 10 minutes.
func (m *Master) cleanupQualityTokens() {
	cutoff := time.Now().Add(-10 * time.Minute)
	m.qualityMu.Lock()
	for token, qt := range m.qualityTokens {
		if qt.CreatedAt.Before(cutoff) {
			delete(m.qualityTokens, token)
		}
	}
	m.qualityMu.Unlock()
}
