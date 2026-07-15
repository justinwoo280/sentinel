// Package master implements the Master lifecycle: ties together the EWP
// control server, Telegram bot, SQLite store, and command dispatch.
package master

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
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

	m := &Master{
		cfg:           cfg,
		log:           log,
		store:         st,
		srv:           srv,
		tg:            tg,
		pendingRename: make(map[int64]string),
		qualityTokens: make(map[string]*qualityToken),
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
		go m.runTelegram(ctx)
		m.log.Info("telegram bot started")
	} else {
		m.log.Info("telegram not configured, skipping bot")
	}

	// Wait for shutdown.
	<-ctx.Done()
	m.store.Close()
	return ctx.Err()
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

	m.tg.PollLoop(ctx, getOffset, setOffset, m.handleUpdate)
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

	switch {
	case msg.Text == "/start" || msg.Text == "/menu":
		m.sendMenu(chatID)
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

	// Acknowledge the callback.
	_ = m.tg.AnswerCallbackQuery(ctx, cb.ID, "")

	parts := strings.Split(cb.Data, "|")
	action := parts[0]

	switch action {
	case ui.CBMenu:
		m.sendMenu(chatID)

	case ui.CBList:
		nodes, err := m.store.ListAllNodes(ctx)
		if err != nil {
			m.tg.SendMessage(ctx, chatID, "Error: "+err.Error())
			return
		}
		text, kb := ui.NodeList(nodes)
		m.tg.SendMessageWithKeyboard(ctx, chatID, text, kb)

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
		m.tg.SendMessageWithKeyboard(ctx, chatID, text, kb)

	case ui.CBRun, ui.CBGoogle, ui.CBTrust, ui.CBQuality, ui.CBReport, ui.CBLog:
		m.dispatchToAgent(ctx, chatID, action, parts)

	case ui.CBTrend:
		m.handleTrend(ctx, chatID, parts)

	case ui.CBToggle:
		m.handleToggle(ctx, chatID, parts)

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
		m.tg.SendMessageWithKeyboard(ctx, chatID, text, kb)

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
			m.tg.SendMessageWithKeyboard(ctx, chatID, text, kb)
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
			m.tg.SendMessageWithKeyboard(ctx, chatID, text, kb)
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

	if err := m.srv.SendCommand(uuidBytes, msg); err != nil {
		m.tg.SendMessage(ctx, chatID, "Failed to send command: "+err.Error())
		return
	}
	m.tg.SendMessage(ctx, chatID, "Command sent to node.")
}

func (m *Master) handleTrend(ctx context.Context, chatID int64, parts []string) {
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
		m.tg.SendMessage(ctx, chatID, "Error fetching trends: "+err.Error())
		return
	}
	if len(trends) == 0 {
		m.tg.SendMessage(ctx, chatID, "No trend data for this node yet.")
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

	m.tg.SendMessageWithKeyboard(ctx, chatID, report, telegram.InlineKeyboardMarkup{
		InlineKeyboard: [][]telegram.InlineKeyboardButton{
			{{Text: "Back", CallbackData: fmt.Sprintf("%s|%s", ui.CBNode, uuid)}},
		},
	})
}

func truncStr(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func (m *Master) handleToggle(ctx context.Context, chatID int64, parts []string) {
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
	m.tg.SendMessage(ctx, chatID, fmt.Sprintf("Module %s set to %v.", mod, state))
}

// handleRegister parses a SENTINEL-REG: registration blob.
func (m *Master) handleRegister(chatID int64, text string) {
	ctx := context.Background()

	reg, err := protocol.Decode(text)
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

func uuidToHex(u [ewp.UUIDLen]byte) string {
	return fmt.Sprintf("%x", u[:])
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
	uuidStr := uuidToHex(uuid)

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
	uuidStr := uuidToHex(uuid)

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
		"uuid", uuidToHex(uuid), "id", ev.ID,
		"data_len", len(ev.Data))
}

func (h *masterHandler) OnQuality(uuid [ewp.UUIDLen]byte, ev ctrl.EventMessage) {
	ctx := context.Background()
	uuidStr := uuidToHex(uuid)

	h.m.log.Info("quality result received", "uuid", uuidStr,
		"data_len", len(ev.Data))

	// Parse quality result to extract key metrics for trend logging.
	var qr struct {
		Score struct {
			SCAMALYTICS *string `json:"SCAMALYTICS"`
		} `json:"Score"`
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
	if err := json.Unmarshal(ev.Data, &qr); err != nil {
		h.m.log.Warn("failed to parse quality result", "err", err)
		return
	}

	// Get node name for trend log.
	node, err := h.m.store.GetNodeByUUID(ctx, uuidStr)
	if err != nil {
		return
	}

	// Extract metrics.
	scamScore := 0
	if qr.Score.SCAMALYTICS != nil {
		fmt.Sscanf(*qr.Score.SCAMALYTICS, "%d", &scamScore)
	}
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
	uuidStr := uuidToHex(uuid)

	h.m.log.Info("report received", "uuid", uuidStr)

	// Forward report to Telegram if bot is configured.
	node, err := h.m.store.GetNodeByUUID(ctx, uuidStr)
	if err != nil || h.m.tg == nil || node.ChatID == "" {
		return
	}
	var chatID int64
	fmt.Sscanf(node.ChatID, "%d", &chatID)
	if chatID != 0 {
		h.m.tg.SendMessage(ctx, chatID, string(ev.Data))
	}
}

func (h *masterHandler) OnLog(uuid [ewp.UUIDLen]byte, ev ctrl.EventMessage) {
	h.m.log.Debug("log received from agent",
		"uuid", uuidToHex(uuid), "data_len", len(ev.Data))
}

func (h *masterHandler) OnDisconnect(uuid [ewp.UUIDLen]byte) {
	h.m.log.Info("agent went offline", "uuid", uuidToHex(uuid))
}

// buildQualityTelegramReport formats a brief Telegram message from a
// quality check result for the node owner.
func buildQualityTelegramReport(node *store.Node, data json.RawMessage) string {
	return fmt.Sprintf("*Quality Report: %s*\n```\n%s\n```",
		node.NodeName, string(data))
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
