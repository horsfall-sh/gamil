package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/jmoiron/sqlx"
)

func cmdIgnore(db *sqlx.DB, args []string) {
	if len(args) == 0 {
		showIgnoreList(db)
		return
	}

	subcmd := strings.ToLower(args[0])

	switch {
	case subcmd == "remove" && len(args) > 1:
		combined := collectNumericArgs(args[1], args[2:])
		removeIgnored(db, combined)
	case subcmd == "help", subcmd == "-h":
		cmdIgnoreHelp()
	case isNumericRef(subcmd):
		combined := collectNumericArgs(subcmd, args[1:])
		addIgnoreByRef(db, combined)
	case strings.HasPrefix(subcmd, "from:"):
		addIgnoreByFrom(db, strings.TrimPrefix(subcmd, "from:"))
	default:
		addIgnoreByFrom(db, subcmd)
	}
}

// ── Show ignore list ────────────────────────────────────────────────────────

func showIgnoreList(db *sqlx.DB) {
	var rows []IgnoredSenderRow
	db.Select(&rows, `SELECT * FROM ignored_senders ORDER BY added_at DESC`)

	fmt.Println()
	fmt.Println(titleStyle.Render("Ignored Senders"))
	fmt.Println("  " + thinRule())

	if len(rows) == 0 {
		fmt.Println()
		fmt.Println("  " + dimStyle.Render("No ignored senders. Emails from ignored senders are auto-acked on sync."))
	} else {
		fmt.Println()
		for _, r := range rows {
			name := r.SenderName
			if name == "" {
				name = r.Sender
			}
			bold := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15")).Render(name)
			idLabel := dimStyle.Render(fmt.Sprintf("#%d", r.ID))
			reason := ""
			if r.Reason != "" {
				reason = dimStyle.Render(" — " + r.Reason)
			}
			fmt.Printf("  %s  %s  %s%s\n", idLabel, bold, dimStyle.Render(r.Sender), reason)
		}
	}

	fmt.Println()
	fmt.Println(dimStyle.Render("  Ignored senders are auto-acked during gmail sync."))
	fmt.Println(dimStyle.Render("  Remove: gmail ignore remove <id>"))
	fmt.Println()
}

// ── Add by summary ref number ───────────────────────────────────────────────

func addIgnoreByRef(db *sqlx.DB, refStr string) {
	refs := loadEmailRefs()
	if len(refs) == 0 {
		fmt.Println(errorStyle.Render("  No email refs — run gmail summary first"))
		return
	}

	nums := parseNumericRefs(refStr)

	for _, n := range nums {
		if n < 1 || n > len(refs) {
			fmt.Printf("  %s  #%d out of range (1-%d)\n", warnStyle.Render("⚠"), n, len(refs))
			continue
		}

		msgID := refs[n-1]
		var e EmailRow
		if err := db.Get(&e, `SELECT * FROM emails WHERE message_id = ?`, msgID); err != nil {
			fmt.Printf("  %s  #%d not found in DB\n", warnStyle.Render("⚠"), n)
			continue
		}

		addIgnoredSender(db, e.Sender, e.SenderName, "")
	}
}

// ── Add by sender ───────────────────────────────────────────────────────────

func addIgnoreByFrom(db *sqlx.DB, sender string) {
	sender = strings.ToLower(strings.TrimSpace(sender))

	var e EmailRow
	err := db.Get(&e, `SELECT * FROM emails WHERE sender LIKE ? ORDER BY date DESC LIMIT 1`,
		"%"+sender+"%")
	if err != nil {
		// No email in DB, but still allow adding raw sender
		addIgnoredSender(db, sender, "", "")
		return
	}

	addIgnoredSender(db, e.Sender, e.SenderName, "")
}

func addIgnoredSender(db *sqlx.DB, sender, senderName, reason string) {
	// Check if already ignored
	var exists int
	db.Get(&exists, `SELECT COUNT(*) FROM ignored_senders WHERE sender = ?`, sender)
	if exists > 0 {
		name := senderName
		if name == "" {
			name = sender
		}
		fmt.Printf("  %s  %s already ignored\n", dimStyle.Render("·"), name)
		return
	}

	now := time.Now().Format(time.RFC3339)
	db.Exec(`INSERT INTO ignored_senders (sender, sender_name, reason, added_at) VALUES (?, ?, ?, ?)`,
		sender, senderName, reason, now)

	name := senderName
	if name == "" {
		name = sender
	}
	fmt.Printf("  %s  Ignoring %s\n", successStyle.Render("✓"), name)

	// Auto-ack all existing emails from this sender
	acked := ackAllFromSender(db, sender)
	if acked > 0 {
		fmt.Printf("       %s\n", dimStyle.Render(fmt.Sprintf("acked %d emails", acked)))
	}
}

// ── Remove from ignore list ─────────────────────────────────────────────────

func removeIgnored(db *sqlx.DB, idStr string) {
	nums := parseNumericRefs(idStr)

	for _, id := range nums {
		var r IgnoredSenderRow
		err := db.Get(&r, `SELECT * FROM ignored_senders WHERE id = ?`, id)
		if err != nil {
			fmt.Printf("  %s  #%d not found\n", warnStyle.Render("⚠"), id)
			continue
		}
		db.Exec(`DELETE FROM ignored_senders WHERE id = ?`, id)
		name := r.SenderName
		if name == "" {
			name = r.Sender
		}
		fmt.Printf("  %s  Removed %s from ignore list\n", successStyle.Render("✓"), name)
	}
}

// ── Sync helper: load ignored senders as a set ──────────────────────────────

func loadIgnoredSenders(db *sqlx.DB) map[string]bool {
	var rows []IgnoredSenderRow
	db.Select(&rows, `SELECT * FROM ignored_senders`)
	m := make(map[string]bool, len(rows))
	for _, r := range rows {
		m[r.Sender] = true
	}
	return m
}

// ── Help ────────────────────────────────────────────────────────────────────

func cmdIgnoreHelp() {
	fmt.Println()
	fmt.Println(titleStyle.Render("gmail ignore — auto-silence senders"))
	fmt.Println()
	fmt.Println("  Emails from ignored senders are automatically marked as read during sync.")
	fmt.Println("  Use this for senders that don't have unsubscribe links.")
	fmt.Println()
	fmt.Println(sectionStyle.Render("  View:"))
	fmt.Printf("    gmail ignore              Show ignore list\n")
	fmt.Println()
	fmt.Println(sectionStyle.Render("  Add (from summary numbers):"))
	fmt.Printf("    gmail ignore 42           Ignore email #42's sender\n")
	fmt.Printf("    gmail ignore 42 43 44     Ignore several\n")
	fmt.Printf("    gmail ignore 40-50        Ignore a range\n")
	fmt.Println()
	fmt.Println(sectionStyle.Render("  Add (by sender):"))
	fmt.Printf("    gmail ignore from:noreply@example.com\n")
	fmt.Printf("    gmail ignore lockly       (partial match)\n")
	fmt.Println()
	fmt.Println(sectionStyle.Render("  Remove:"))
	fmt.Printf("    gmail ignore remove 1     Stop ignoring #1\n")
	fmt.Printf("    gmail ignore remove 1 2 3\n")
	fmt.Println()
}
