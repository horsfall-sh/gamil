package main

import (
	"fmt"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/jmoiron/sqlx"
)

// cmdBrief is the tight morning view — only what needs attention right now.
// This is the new default command (replaces full inbox summary as default).
func cmdBrief(db *sqlx.DB) {
	now := time.Now()
	today := now.Format("2006-01-02")
	yesterday := now.AddDate(0, 0, -1).Format("2006-01-02")

	fmt.Println()
	fmt.Println(hRule())
	fmt.Println(titleStyle.Render(now.Format("Monday Jan 2") + " — Morning Brief"))
	fmt.Println(hRule())

	// ── URGENT ──────────────────────────────────────────────────────────────
	urgentEmails, _ := getEmailsByCategory(db, "needs_attention")
	fmt.Println()
	urgentHeader := sectionStyle.Foreground(lipgloss.Color("1")).Render("▌ Urgent")
	ruleRed := lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Render("  ──────────────────────────────────────────────")
	fmt.Println(urgentHeader)
	fmt.Println(ruleRed)
	if len(urgentEmails) == 0 {
		fmt.Println("    " + successStyle.Render("✓") + " " + dimStyle.Render("Nothing urgent"))
	} else {
		for i, e := range urgentEmails {
			name := e.SenderName
			if name == "" {
				name = e.Sender
			}
			fmt.Printf("    %s %s %s — %s\n",
				dimStyle.Render(fmt.Sprintf("%d.", i+1)),
				errorStyle.Render("!"), name, truncate(e.Subject, 45))
		}
		writeEmailRefs(urgentEmails)
	}

	// ── SECURITY ────────────────────────────────────────────────────────────
	securityEmails, _ := getEmailsByCategory(db, "security")
	if len(securityEmails) > 0 {
		fmt.Println()
		secHeader := sectionStyle.Foreground(lipgloss.Color("1")).Render("▌ Security Alerts")
		fmt.Println(secHeader)
		fmt.Println(ruleRed)
		for _, e := range securityEmails {
			name := e.SenderName
			if name == "" {
				name = e.Sender
			}
			fmt.Printf("    %s %s — %s\n", warnStyle.Render("⚠"), name, truncate(e.Subject, 50))
		}
	}

	// ── TODAY ────────────────────────────────────────────────────────────────
	fmt.Println()
	todayHeader := sectionStyle.Foreground(lipgloss.Color("14")).Render("▌ Today")
	ruleBlue := lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Render("  ──────────────────────────────────────────────")
	fmt.Println(todayHeader)
	fmt.Println(ruleBlue)

	// Deliveries expected today
	active, _ := getActiveDeliveries(db)
	var todayDeliveries []DeliveryRow
	var soonDeliveries []DeliveryRow
	for _, d := range active {
		if d.ExpectedDate == today {
			todayDeliveries = append(todayDeliveries, d)
		} else if d.ExpectedDate != "" && d.ExpectedDate > today {
			soonDeliveries = append(soonDeliveries, d)
		}
	}

	if len(todayDeliveries) > 0 {
		for _, d := range todayDeliveries {
			label := truncate(d.Description, 40)
			fmt.Printf("    %s  %s — expected today\n",
				lipgloss.NewStyle().Foreground(lipgloss.Color("4")).Render("📦"),
				label)
		}
	} else if len(soonDeliveries) > 0 {
		for _, d := range soonDeliveries {
			label := truncate(d.Description, 40)
			fmt.Printf("    %s  %s — %s\n",
				dimStyle.Render("·"),
				label,
				dimStyle.Render(d.ExpectedDate))
		}
	} else if len(active) > 0 {
		fmt.Printf("    %s  %d package(s) in transit\n",
			lipgloss.NewStyle().Foreground(lipgloss.Color("4")).Render("·"),
			len(active))
	} else {
		fmt.Println("    " + dimStyle.Render("No deliveries expected"))
	}

	// Calendar events from emails table
	calendarEmails, _ := getEmailsByCategory(db, "calendar")
	if len(calendarEmails) > 0 {
		for _, e := range calendarEmails {
			fmt.Println("    " + lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Render("◷") + " " + truncate(e.Subject, 55))
		}
	}

	// ── YESTERDAY ───────────────────────────────────────────────────────────
	fmt.Println()
	yestHeader := sectionStyle.Foreground(lipgloss.Color("3")).Render("▌ Yesterday's Spending")
	ruleYellow := lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Render("  ──────────────────────────────────────────────")
	fmt.Println(yestHeader)
	fmt.Println(ruleYellow)

	ySpend, yCount, _ := getSpendForDate(db, yesterday)
	dailyAvg, _ := getDailyAvg(db, 30)

	if yCount == 0 {
		fmt.Println("    " + dimStyle.Render("No spending yesterday"))
	} else {
		pct := 0.0
		label := ""
		if dailyAvg > 0 {
			pct = ((ySpend - dailyAvg) / dailyAvg) * 100
			if pct > 20 {
				label = errorStyle.Render(fmt.Sprintf("  ↑ +%.0f%% above 30d avg ($%.0f)", pct, dailyAvg))
			} else if pct < -20 {
				label = successStyle.Render(fmt.Sprintf("  ↓ %.0f%% below 30d avg ($%.0f)", pct, dailyAvg))
			} else {
				label = dimStyle.Render(fmt.Sprintf("  → near avg ($%.0f/day)", dailyAvg))
			}
		}
		fmt.Printf("    %s  (%d transactions)%s\n",
			amountStyle(ySpend).Render(fmt.Sprintf("$%.2f", ySpend)),
			yCount, label)
	}

	// Today's spend so far
	tSpend, tCount, _ := getSpendForDate(db, today)
	if tCount > 0 {
		fmt.Printf("    %s  today so far (%d)\n",
			dimStyle.Render(fmt.Sprintf("$%.2f", tSpend)),
			tCount)
	}

	fmt.Println()
	fmt.Printf("  %s  %s\n",
		dimStyle.Render("Full inbox:"),
		dimStyle.Render("gmail summary  •  gmail deliveries  •  gmail overview"))
	fmt.Println()
}
