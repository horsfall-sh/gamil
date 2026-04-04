package main

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/jmoiron/sqlx"
)

var unsubURLRe = regexp.MustCompile(`<(https?://[^>]+)>`)
var unsubMailRe = regexp.MustCompile(`<mailto:([^>]+)>`)

func cmdUnsub(db *sqlx.DB, args []string) {
	if len(args) == 0 {
		showUnsubList(db)
		return
	}

	subcmd := strings.ToLower(args[0])

	switch {
	case subcmd == "done" && len(args) > 1:
		combined := collectNumericArgs(args[1], args[2:])
		markUnsubDone(db, combined)
	case subcmd == "export":
		exportUnsubList(db)
	case subcmd == "clear":
		clearCompletedUnsubs(db)
	case subcmd == "help", subcmd == "-h":
		cmdUnsubHelp()
	case isNumericRef(subcmd):
		combined := collectNumericArgs(subcmd, args[1:])
		addUnsubByRef(db, combined)
	case strings.HasPrefix(subcmd, "from:"):
		addUnsubByFrom(db, strings.TrimPrefix(subcmd, "from:"))
	default:
		addUnsubByFrom(db, subcmd)
	}
}

// ── Show pending list ───────────────────────────────────────────────────────

func showUnsubList(db *sqlx.DB) {
	var pending []UnsubRow
	db.Select(&pending, `SELECT * FROM unsubscribes WHERE status = 'pending' ORDER BY added_at DESC`)

	var done []UnsubRow
	db.Select(&done, `SELECT * FROM unsubscribes WHERE status = 'done' ORDER BY completed_at DESC LIMIT 10`)

	fmt.Println()
	fmt.Println(titleStyle.Render("Unsubscribe List"))
	fmt.Println("  " + thinRule())

	if len(pending) == 0 {
		fmt.Println()
		fmt.Println("  " + successStyle.Render("✓") + " " + dimStyle.Render("No pending unsubscribes"))
	} else {
		fmt.Println()
		style := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("1")).PaddingLeft(1)
		fmt.Println(style.Render(fmt.Sprintf("▌ Pending (%d)", len(pending))))
		fmt.Println()
		for _, u := range pending {
			name := u.SenderName
			if name == "" {
				name = u.Sender
			}
			bold := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15")).Render(name)
			idLabel := dimStyle.Render(fmt.Sprintf("#%d", u.ID))

			link := ""
			if u.UnsubURL != "" {
				link = dimStyle.Render("  URL: " + truncate(u.UnsubURL, 60))
			} else if u.UnsubEmail != "" {
				link = dimStyle.Render("  mailto: " + u.UnsubEmail)
			} else {
				link = warnStyle.Render("  (no link found)")
			}

			fmt.Printf("  %s  %s  %s\n", idLabel, bold, dimStyle.Render(u.Sender))
			fmt.Printf("        %s\n", link)
		}
	}

	if len(done) > 0 {
		fmt.Println()
		fmt.Println("  " + dimStyle.Render(fmt.Sprintf("Recently completed: %d", len(done))))
		for _, u := range done {
			name := u.SenderName
			if name == "" {
				name = u.Sender
			}
			fmt.Printf("    %s %s %s\n",
				successStyle.Render("✓"),
				dimStyle.Render(name),
				dimStyle.Render(u.CompletedAt[:10]))
		}
	}

	fmt.Println()
	if len(pending) > 0 {
		fmt.Println(dimStyle.Render("  Export for Claude: gmail unsub export"))
		fmt.Println(dimStyle.Render("  Mark done:         gmail unsub done <id>"))
		fmt.Println()
	}
}

// ── Add by summary ref number ───────────────────────────────────────────────

func addUnsubByRef(db *sqlx.DB, refStr string) {
	refs := loadEmailRefs()
	if len(refs) == 0 {
		fmt.Println(errorStyle.Render("  No email refs — run gmail summary first"))
		return
	}

	nums := parseNumericRefs(refStr)
	added := 0

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

		// Skip if already in list
		var exists int
		db.Get(&exists, `SELECT COUNT(*) FROM unsubscribes WHERE sender = ? AND status = 'pending'`, e.Sender)
		if exists > 0 {
			name := e.SenderName
			if name == "" {
				name = e.Sender
			}
			fmt.Printf("  %s  %s already on the list\n", dimStyle.Render("·"), name)
			continue
		}

		// Fetch unsubscribe link from Gmail
		unsubURL, unsubEmail := fetchUnsubLink(msgID)

		now := time.Now().Format(time.RFC3339)
		db.Exec(`INSERT INTO unsubscribes (sender, sender_name, unsub_url, unsub_email, status, added_at, message_id)
			VALUES (?, ?, ?, ?, 'pending', ?, ?)`,
			e.Sender, e.SenderName, unsubURL, unsubEmail, now, msgID)

		name := e.SenderName
		if name == "" {
			name = e.Sender
		}
		linkInfo := ""
		if unsubURL != "" {
			linkInfo = dimStyle.Render(" (link found)")
		} else if unsubEmail != "" {
			linkInfo = dimStyle.Render(" (mailto found)")
		} else {
			linkInfo = warnStyle.Render(" (no unsub link)")
		}
		fmt.Printf("  %s  Added %s%s\n", successStyle.Render("+"), name, linkInfo)

		// Auto-ack all emails from this sender
		acked := ackAllFromSender(db, e.Sender)
		if acked > 0 {
			fmt.Printf("       %s\n", dimStyle.Render(fmt.Sprintf("acked %d emails from %s", acked, e.Sender)))
		}
		added++
	}

	if added > 0 {
		fmt.Printf("\n  %s  View list: gmail unsub\n", dimStyle.Render("→"))
	}
}

// ── Add by sender address ───────────────────────────────────────────────────

func addUnsubByFrom(db *sqlx.DB, sender string) {
	sender = strings.ToLower(strings.TrimSpace(sender))

	// Find a matching email in DB to get sender_name and a message for unsub link
	var e EmailRow
	err := db.Get(&e, `SELECT * FROM emails WHERE sender LIKE ? ORDER BY date DESC LIMIT 1`,
		"%"+sender+"%")
	if err != nil {
		fmt.Printf("  %s  No emails found from '%s'\n", warnStyle.Render("⚠"), sender)
		return
	}

	// Skip if already pending
	var exists int
	db.Get(&exists, `SELECT COUNT(*) FROM unsubscribes WHERE sender = ? AND status = 'pending'`, e.Sender)
	if exists > 0 {
		name := e.SenderName
		if name == "" {
			name = e.Sender
		}
		fmt.Printf("  %s  %s already on the list\n", dimStyle.Render("·"), name)
		return
	}

	unsubURL, unsubEmail := fetchUnsubLink(e.MessageID)

	now := time.Now().Format(time.RFC3339)
	db.Exec(`INSERT INTO unsubscribes (sender, sender_name, unsub_url, unsub_email, status, added_at, message_id)
		VALUES (?, ?, ?, ?, 'pending', ?, ?)`,
		e.Sender, e.SenderName, unsubURL, unsubEmail, now, e.MessageID)

	name := e.SenderName
	if name == "" {
		name = e.Sender
	}
	linkInfo := ""
	if unsubURL != "" {
		linkInfo = dimStyle.Render(" (link found)")
	} else if unsubEmail != "" {
		linkInfo = dimStyle.Render(" (mailto found)")
	} else {
		linkInfo = warnStyle.Render(" (no unsub link)")
	}
	fmt.Printf("  %s  Added %s%s\n", successStyle.Render("+"), name, linkInfo)

	// Auto-ack all emails from this sender
	acked := ackAllFromSender(db, e.Sender)
	if acked > 0 {
		fmt.Printf("       %s\n", dimStyle.Render(fmt.Sprintf("acked %d emails from %s", acked, e.Sender)))
	}
}

// ── Fetch List-Unsubscribe header from Gmail ────────────────────────────────

func fetchUnsubLink(messageID string) (url, email string) {
	svc, err := getGmailService()
	if err != nil {
		return "", ""
	}

	msg, err := svc.Users.Messages.Get("me", messageID).
		Format("metadata").
		MetadataHeaders("List-Unsubscribe", "List-Unsubscribe-Post").
		Do()
	if err != nil {
		return "", ""
	}

	for _, h := range msg.Payload.Headers {
		if strings.EqualFold(h.Name, "List-Unsubscribe") {
			// Parse: <https://...>, <mailto:...>
			if m := unsubURLRe.FindStringSubmatch(h.Value); m != nil {
				url = m[1]
			}
			if m := unsubMailRe.FindStringSubmatch(h.Value); m != nil {
				email = m[1]
			}
		}
	}
	return
}

// ── Mark as done ────────────────────────────────────────────────────────────

func markUnsubDone(db *sqlx.DB, idStr string) {
	nums := parseNumericRefs(idStr)
	now := time.Now().Format(time.RFC3339)

	for _, id := range nums {
		var u UnsubRow
		err := db.Get(&u, `SELECT * FROM unsubscribes WHERE id = ? AND status = 'pending'`, id)
		if err != nil {
			fmt.Printf("  %s  #%d not found or already done\n", warnStyle.Render("⚠"), id)
			continue
		}
		db.Exec(`UPDATE unsubscribes SET status = 'done', completed_at = ? WHERE id = ?`, now, id)
		name := u.SenderName
		if name == "" {
			name = u.Sender
		}
		fmt.Printf("  %s  %s marked done\n", successStyle.Render("✓"), name)
	}
}

// ── Clear completed ─────────────────────────────────────────────────────────

func clearCompletedUnsubs(db *sqlx.DB) {
	res, _ := db.Exec(`DELETE FROM unsubscribes WHERE status = 'done'`)
	rows, _ := res.RowsAffected()
	fmt.Printf("  %s  Cleared %d completed entries\n", successStyle.Render("✓"), rows)
}

// ── Export for Claude ───────────────────────────────────────────────────────

func exportUnsubList(db *sqlx.DB) {
	var pending []UnsubRow
	db.Select(&pending, `SELECT * FROM unsubscribes WHERE status = 'pending' ORDER BY id`)

	if len(pending) == 0 {
		fmt.Println("No pending unsubscribes.")
		return
	}

	fmt.Println("## Pending Unsubscribes")
	fmt.Println()
	for i, u := range pending {
		name := u.SenderName
		if name == "" {
			name = u.Sender
		}
		fmt.Printf("%d. **%s** (%s)\n", i+1, name, u.Sender)
		if u.UnsubURL != "" {
			fmt.Printf("   URL: %s\n", u.UnsubURL)
		}
		if u.UnsubEmail != "" {
			fmt.Printf("   Email: %s\n", u.UnsubEmail)
		}
		if u.UnsubURL == "" && u.UnsubEmail == "" {
			fmt.Printf("   ⚠ No unsubscribe link found — visit sender's site manually\n")
		}
		fmt.Println()
	}
	fmt.Printf("---\n")
	fmt.Printf("For each URL above, visit the link and complete the unsubscribe flow.\n")
	fmt.Printf("For mailto entries, send a blank email to that address.\n")
	fmt.Printf("After completing each, run: gmail unsub done <id>\n")
}

// ── Help ────────────────────────────────────────────────────────────────────

func cmdUnsubHelp() {
	fmt.Println()
	fmt.Println(titleStyle.Render("gmail unsub — manage unsubscribe list"))
	fmt.Println()
	fmt.Println(sectionStyle.Render("  View & export:"))
	fmt.Printf("    gmail unsub               Show pending list\n")
	fmt.Printf("    gmail unsub export        Export for Claude to process\n")
	fmt.Println()
	fmt.Println(sectionStyle.Render("  Add (from summary numbers):"))
	fmt.Printf("    gmail unsub 42            Add email #42 from last summary\n")
	fmt.Printf("    gmail unsub 110,112,115   Add several\n")
	fmt.Printf("    gmail unsub 110-120       Add a range\n")
	fmt.Println()
	fmt.Println(sectionStyle.Render("  Add (by sender):"))
	fmt.Printf("    gmail unsub from:quickway.com\n")
	fmt.Printf("    gmail unsub hancock       (partial match)\n")
	fmt.Println()
	fmt.Println(sectionStyle.Render("  After unsubscribing:"))
	fmt.Printf("    gmail unsub done 1        Mark #1 as completed\n")
	fmt.Printf("    gmail unsub done 1,2,3    Mark several as done\n")
	fmt.Printf("    gmail unsub clear         Remove completed entries\n")
	fmt.Println()
}
