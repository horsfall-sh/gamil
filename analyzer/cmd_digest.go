package main

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/jmoiron/sqlx"
)

// cmdAutoDispatch is the default (no-args) behavior.
// First run after 8am: last 24 hours. Subsequent runs: since last run. 10-min suppression.
func cmdAutoDispatch(db *sqlx.DB) {
	if os.Getenv("GMAIL_NO_AUTORUN") == "1" {
		return
	}

	now := time.Now()
	today := now.Format("2006-01-02")

	var since time.Time
	lastDayStr := getSyncState(db, "last_dispatch_date")
	lastEpochStr := getSyncState(db, "last_dispatch_epoch")

	if now.Hour() >= 8 && lastDayStr != today {
		// First run after 8am — show last 24 hours
		since = now.Add(-24 * time.Hour)
		setSyncState(db, "last_dispatch_date", today)
	} else if lastEpochStr != "" {
		epoch, err := strconv.ParseInt(lastEpochStr, 10, 64)
		if err == nil {
			since = time.Unix(epoch, 0)
		} else {
			since = now.Add(-1 * time.Hour)
		}
		// Suppress if <10 min since last run
		if now.Sub(since) < 10*time.Minute {
			return
		}
	} else {
		// No previous run at all
		since = now.Add(-1 * time.Hour)
	}

	setSyncState(db, "last_dispatch_epoch", strconv.FormatInt(now.Unix(), 10))
	cmdDigestWindow(db, since, now)
}

// cmdDigest is the explicit `gmail digest` entry point — always shows last hour, no suppression.
func cmdDigest(db *sqlx.DB) {
	cmdDigestWindow(db, time.Now().Add(-1*time.Hour), time.Now())
}

// cmdDigestWindow renders an email digest for a given time window.
func cmdDigestWindow(db *sqlx.DB, since, until time.Time) {
	emails, _ := getEmailsSince(db, since)

	var totalUnread int
	db.Get(&totalUnread, `SELECT COUNT(*) FROM emails WHERE is_unread = 1`)

	fmt.Println()

	// Build human-readable window label
	windowLabel := formatWindow(since, until)

	if len(emails) == 0 {
		fmt.Printf("  %s  No new emails %s — %s unread total\n",
			successStyle.Render("✓"),
			dimStyle.Render(windowLabel),
			dimStyle.Render(fmt.Sprintf("%d", totalUnread)))
		printDigestFooter(db)
		return
	}

	// Header
	header := fmt.Sprintf("Inbox — %s (%d new", windowLabel, len(emails))
	if totalUnread > 0 {
		header += fmt.Sprintf(" · %d unread", totalUnread)
	}
	header += ")"
	fmt.Println(titleStyle.Render(header))
	fmt.Println("  " + thinRule())

	if len(emails) <= 15 {
		fmt.Println()
		for i, e := range emails {
			name := e.SenderName
			if name == "" {
				name = e.Sender
			}
			prefix := dimStyle.Render("·")
			if e.Category == "needs_attention" {
				prefix = errorStyle.Render("!")
			}
			fmt.Printf("  %s %s %s — %s\n",
				dimStyle.Render(fmt.Sprintf("%2d.", i+1)),
				prefix, name, truncate(e.Subject, 45))
		}
		writeEmailRefs(emails)
	} else {
		// Category counts
		counts := make(map[string]int)
		hasUrgent := 0
		for _, e := range emails {
			counts[e.Category]++
			if e.Category == "needs_attention" {
				hasUrgent++
			}
		}

		fmt.Println()
		order := []string{"needs_attention", "financial", "deliveries", "calendar", "kids", "security", "newsletters", "social", "government", "promotions", "other"}
		catLabels := map[string]string{
			"needs_attention": "urgent",
			"financial":       "financial",
			"deliveries":      "deliveries",
			"calendar":        "calendar",
			"kids":            "kids",
			"security":        "security",
			"newsletters":     "newsletters",
			"social":          "social",
			"government":      "government",
			"promotions":      "promotions",
			"other":           "other",
		}
		line := " "
		for _, cat := range order {
			if c, ok := counts[cat]; ok && c > 0 {
				line += fmt.Sprintf("  %d %s", c, catLabels[cat])
			}
		}
		fmt.Println(line)

		if hasUrgent > 0 {
			fmt.Printf("\n  %s %s\n",
				errorStyle.Render("!"),
				lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Render(
					fmt.Sprintf("%d email(s) need attention — run: gmail urgent", hasUrgent)))
		}
	}

	printDigestFooter(db)
}

// formatWindow returns a human-readable label like "last 24h" or "8:01 AM – 9:24 AM".
func formatWindow(since, until time.Time) string {
	dur := until.Sub(since)
	if dur >= 23*time.Hour && dur <= 25*time.Hour {
		return "last 24h"
	}
	return since.Format("3:04 PM") + " – " + until.Format("3:04 PM")
}

func printDigestFooter(db *sqlx.DB) {
	// Stale sync warning
	epochStr := getSyncState(db, "last_sync_epoch")
	if epochStr != "" {
		epoch, err := strconv.ParseInt(epochStr, 10, 64)
		if err == nil {
			age := time.Since(time.Unix(epoch, 0))
			if age > 2*time.Hour {
				hours := int(age.Hours())
				fmt.Printf("\n  %s\n", dimStyle.Render(fmt.Sprintf("· synced %dh ago — run: gmail sync", hours)))
			}
		}
	}

	fmt.Println()
	fmt.Printf("  %s\n", dimStyle.Render("gmail summary  ·  gmail ack 1"))
	fmt.Println()
}
