// Package ui renders Telegram inline keyboard panels for the Master bot.
package ui

import (
	"fmt"
	"strings"

	"github.com/justinwoo280/sentinel/internal/master/store"
	"github.com/justinwoo280/sentinel/internal/master/telegram"
)

// Callback data prefixes (SR-1: closed enum of callback actions).
const (
	CBMenu      = "menu"
	CBList      = "list"       // list nodes by region
	CBNode      = "node"       // node control panel (arg: uuid)
	CBRun       = "run"        // trigger keepalive cycle
	CBGoogle    = "google"     // trigger google module
	CBTrust     = "trust"      // trigger trust module
	CBQuality   = "quality"    // trigger quality check
	CBReport    = "report"     // generate report
	CBLog       = "log"        // fetch logs
	CBTrend     = "trend"      // view trend
	CBToggle    = "toggle"     // toggle module (arg: uuid|mod|state)
	CBRename    = "rename"     // rename (force_reply)
	CBOTA       = "ota"        // OTA confirm
	CBOTAYes    = "ota_yes"    // OTA confirmed
	CBDelete    = "delete"     // delete node confirm
	CBDeleteYes = "delete_yes" // delete confirmed
	CBSaveTrend = "svq"        // save quality metrics to trend DB (arg: token)
	CBBack      = "back"
)

// MainMenu renders the top-level menu.
func MainMenu(version string, nodeCount int) (string, telegram.InlineKeyboardMarkup) {
	text := fmt.Sprintf("*Sentinel Master*\nVersion: `%s`\nRegistered nodes: %d\n\nSelect an action:",
		version, nodeCount)
	kb := telegram.InlineKeyboardMarkup{
		InlineKeyboard: [][]telegram.InlineKeyboardButton{
			{
				{Text: "Node List", CallbackData: CBList},
			},
			{
				{Text: "Global Keepalive", CallbackData: CBRun},
				{Text: "Global Report", CallbackData: CBReport},
			},
			{
				{Text: "Global OTA", CallbackData: CBOTA},
			},
		},
	}
	return text, kb
}

// NodeList renders all nodes grouped by region.
func NodeList(nodes []store.Node) (string, telegram.InlineKeyboardMarkup) {
	if len(nodes) == 0 {
		return "No registered nodes.\n\nUse /register to add a new agent.",
			telegram.InlineKeyboardMarkup{
				InlineKeyboard: [][]telegram.InlineKeyboardButton{
					{{Text: "Back", CallbackData: CBMenu}},
				},
			}
	}

	byRegion := map[string][]store.Node{}
	var regions []string
	for _, n := range nodes {
		if _, ok := byRegion[n.Region]; !ok {
			regions = append(regions, n.Region)
		}
		byRegion[n.Region] = append(byRegion[n.Region], n)
	}

	var sb strings.Builder
	sb.WriteString("*Nodes by Region*\n\n")

	var rows [][]telegram.InlineKeyboardButton
	for _, region := range regions {
		flag := regionFlag(region)
		sb.WriteString(fmt.Sprintf("%s *%s* (%d)\n", flag, region, len(byRegion[region])))
		for _, n := range byRegion[region] {
			label := n.NodeName
			if n.NodeAlias != "" {
				label = n.NodeAlias
			}
			rows = append(rows, []telegram.InlineKeyboardButton{
				{
					Text:         fmt.Sprintf("%s %s — %s", flag, region, label),
					CallbackData: fmt.Sprintf("%s|%s", CBNode, n.UUID),
				},
			})
		}
	}
	rows = append(rows, []telegram.InlineKeyboardButton{
		{Text: "Back", CallbackData: CBMenu},
	})

	return sb.String(), telegram.InlineKeyboardMarkup{InlineKeyboard: rows}
}

// NodePanel renders the control panel for a single node.
func NodePanel(n *store.Node, online bool) (string, telegram.InlineKeyboardMarkup) {
	status := "offline"
	if online {
		status = "online"
	}
	alias := n.NodeAlias
	if alias == "" {
		alias = "(none)"
	}

	text := fmt.Sprintf("*Node: %s*\n\nUUID: `%s`\nAlias: %s\nRegion: %s\nIP: %s\nStatus: %s\nVersion: %s\n\nGoogle: %s | Trust: %s | OTA: %s",
		n.NodeName, n.UUID, alias, n.Region,
		ifEmpty(n.IP, "unknown"), status, ifEmpty(n.Version, "unknown"),
		onOff(n.EnableGoogle), onOff(n.EnableTrust), onOff(n.EnableOTA))

	gToggle := "Enable Google"
	if n.EnableGoogle {
		gToggle = "Disable Google"
	}
	tToggle := "Enable Trust"
	if n.EnableTrust {
		tToggle = "Disable Trust"
	}

	kb := telegram.InlineKeyboardMarkup{
		InlineKeyboard: [][]telegram.InlineKeyboardButton{
			{
				{Text: "Keepalive", CallbackData: fmt.Sprintf("%s|%s", CBRun, n.UUID)},
				{Text: "Report", CallbackData: fmt.Sprintf("%s|%s", CBReport, n.UUID)},
			},
			{
				{Text: "Google", CallbackData: fmt.Sprintf("%s|%s", CBGoogle, n.UUID)},
				{Text: "Trust", CallbackData: fmt.Sprintf("%s|%s", CBTrust, n.UUID)},
			},
			{
				{Text: "Quality", CallbackData: fmt.Sprintf("%s|%s", CBQuality, n.UUID)},
				{Text: "Trend", CallbackData: fmt.Sprintf("%s|%s", CBTrend, n.UUID)},
			},
			{
				{Text: "Log", CallbackData: fmt.Sprintf("%s|%s", CBLog, n.UUID)},
				{Text: gToggle, CallbackData: fmt.Sprintf("%s|%s|google|%v", CBToggle, n.UUID, !n.EnableGoogle)},
				{Text: tToggle, CallbackData: fmt.Sprintf("%s|%s|trust|%v", CBToggle, n.UUID, !n.EnableTrust)},
			},
			{
				{Text: "Rename", CallbackData: fmt.Sprintf("%s|%s", CBRename, n.UUID)},
				{Text: "OTA", CallbackData: fmt.Sprintf("%s|%s", CBOTA, n.UUID)},
			},
			{
				{Text: "Delete", CallbackData: fmt.Sprintf("%s|%s", CBDelete, n.UUID)},
			},
			{
				{Text: "Back", CallbackData: CBList},
			},
		},
	}
	return text, kb
}

// ConfirmDelete renders a deletion confirmation panel.
func ConfirmDelete(uuid, name string) (string, telegram.InlineKeyboardMarkup) {
	text := fmt.Sprintf("*Confirm deletion*\n\nDelete node `%s`?\nThis cannot be undone.", name)
	kb := telegram.InlineKeyboardMarkup{
		InlineKeyboard: [][]telegram.InlineKeyboardButton{
			{
				{Text: "Yes, delete", CallbackData: fmt.Sprintf("%s|%s", CBDeleteYes, uuid)},
				{Text: "Cancel", CallbackData: fmt.Sprintf("%s|%s", CBNode, uuid)},
			},
		},
	}
	return text, kb
}

// ConfirmOTA renders an OTA confirmation panel for a single node.
func ConfirmOTA(uuid, name string) (string, telegram.InlineKeyboardMarkup) {
	text := fmt.Sprintf("*Confirm OTA upgrade*\n\nTrigger self-update on node `%s`?\nThe agent will download, verify, and restart.", name)
	kb := telegram.InlineKeyboardMarkup{
		InlineKeyboard: [][]telegram.InlineKeyboardButton{
			{
				{Text: "Yes, upgrade", CallbackData: fmt.Sprintf("%s|%s", CBOTAYes, uuid)},
				{Text: "Cancel", CallbackData: fmt.Sprintf("%s|%s", CBNode, uuid)},
			},
		},
	}
	return text, kb
}

// ConfirmGlobalOTA renders an OTA confirmation for all online nodes.
func ConfirmGlobalOTA(onlineCount int) (string, telegram.InlineKeyboardMarkup) {
	text := fmt.Sprintf("*Confirm global OTA*\n\nTrigger self-update on all %d online nodes?", onlineCount)
	kb := telegram.InlineKeyboardMarkup{
		InlineKeyboard: [][]telegram.InlineKeyboardButton{
			{
				{Text: "Yes, upgrade all", CallbackData: CBOTAYes},
				{Text: "Cancel", CallbackData: CBMenu},
			},
		},
	}
	return text, kb
}

func ifEmpty(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

func onOff(b bool) string {
	if b {
		return "ON"
	}
	return "OFF"
}

// regionFlag converts a 2-letter ISO country code to a flag emoji.
// Works by mapping each letter to its regional indicator symbol.
func regionFlag(code string) string {
	if len(code) != 2 {
		return ""
	}
	// Special: UK is commonly used in this project but ISO is GB.
	if code == "UK" {
		code = "GB"
	}
	c := strings.ToUpper(code)
	var sb strings.Builder
	for _, r := range c {
		if r < 'A' || r > 'Z' {
			return ""
		}
		sb.WriteRune(0x1F1E6 + (r - 'A'))
	}
	return sb.String()
}
