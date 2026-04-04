package main

import (
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/jmoiron/sqlx"
)

// SenderGroup aggregates emails from a single sender within a category.
type SenderGroup struct {
	Sender     string
	SenderName string
	Count      int
	LatestDate string
	RepSubject string
	Emails     []EmailRow
}

// groupEmailsBySender collapses a flat email list into sender groups, sorted by count descending.
func groupEmailsBySender(emails []EmailRow) []SenderGroup {
	seen := map[string]int{} // sender -> index in result
	var groups []SenderGroup
	for _, e := range emails {
		idx, ok := seen[e.Sender]
		if !ok {
			idx = len(groups)
			seen[e.Sender] = idx
			name := e.SenderName
			if name == "" {
				name = e.Sender
			}
			groups = append(groups, SenderGroup{
				Sender:     e.Sender,
				SenderName: name,
				LatestDate: e.Date,
				RepSubject: e.Subject,
			})
		}
		groups[idx].Emails = append(groups[idx].Emails, e)
		groups[idx].Count++
	}
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].Count > groups[j].Count
	})
	return groups
}

func formatEmailDate(date string) string {
	t, err := time.Parse("2006-01-02", date)
	if err != nil {
		return date
	}
	return t.Format("Mon 1/2")
}

func cmdSummary(db *sqlx.DB) {
	// Get last sync time
	lastSync := getSyncState(db, "last_sync_epoch")
	syncLabel := "never"
	if lastSync != "" {
		if epoch, err := strconv.ParseInt(lastSync, 10, 64); err == nil {
			syncLabel = time.Unix(epoch, 0).Format("Jan 2 3:04pm")
		}
	}

	var totalUnread int
	db.Get(&totalUnread, `SELECT COUNT(*) FROM emails WHERE is_unread = 1`)

	fmt.Println()
	fmt.Println(hRule())
	fmt.Println(titleStyle.Render("Gmail Inbox Summary"))
	fmt.Println(hRule())
	fmt.Println(subtitleStyle.Render(fmt.Sprintf("Last synced: %s | Unread: ~%d", syncLabel, totalUnread)))

	type section struct {
		category string
		heading  string
		color    lipgloss.Color
	}
	sections := []section{
		{"needs_attention", "Needs Attention", lipgloss.Color("1")},
		{"calendar", "Calendar / Upcoming", lipgloss.Color("14")},
		{"kids", "Kids / School", lipgloss.Color("2")},
		{"financial", "Financial", lipgloss.Color("3")},
		{"deliveries", "Deliveries", lipgloss.Color("4")},
		{"security", "Security", lipgloss.Color("1")},
		{"newsletters", "Newsletters", lipgloss.Color("6")},
		{"social", "Social", lipgloss.Color("5")},
		{"government", "Government", lipgloss.Color("13")},
		{"promotions", "Promotions", lipgloss.Color("8")},
		{"other", "Other", lipgloss.Color("15")},
	}

	counter := 0
	var allDisplayed []EmailRow

	for _, sec := range sections {
		emails, err := getEmailsByCategory(db, sec.category)
		if err != nil || len(emails) == 0 {
			continue
		}

		style := lipgloss.NewStyle().Bold(true).Foreground(sec.color).PaddingLeft(1)
		ruleStyle := lipgloss.NewStyle().Foreground(sec.color)
		fmt.Println()

		const groupThreshold = 5

		if len(emails) > groupThreshold {
			// Grouped view: collapse by sender
			fmt.Println(style.Render(fmt.Sprintf("▌ %s (%d)", sec.heading, len(emails))))
			fmt.Println(ruleStyle.Render("  ──────────────────────────────────────────────"))

			groups := groupEmailsBySender(emails)
			for _, g := range groups {
				start := counter + 1
				for _, e := range g.Emails {
					counter++
					allDisplayed = append(allDisplayed, e)
				}
				end := counter

				nameBold := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15")).Render(pad(g.SenderName, 20))
				countLabel := fmt.Sprintf("%d email", g.Count)
				if g.Count > 1 {
					countLabel += "s"
				}
				desc := truncate(g.RepSubject, 40)
				dateStr := formatEmailDate(g.LatestDate)

				fmt.Printf("  %s %s — %s %s\n",
					nameBold,
					dimStyle.Render(pad(countLabel, 10)),
					desc, dimStyle.Render("("+dateStr+")"))

				var refLabel string
				if start == end {
					refLabel = fmt.Sprintf("ref %d", start)
				} else {
					refLabel = fmt.Sprintf("refs %d-%d", start, end)
				}
				fmt.Printf("    %s\n", dimStyle.Render(refLabel))
			}
		} else {
			// Flat view: one line per email (unchanged)
			fmt.Println(style.Render("▌ " + sec.heading))
			fmt.Println(ruleStyle.Render("  ──────────────────────────────────────────────"))

			for _, e := range emails {
				counter++
				allDisplayed = append(allDisplayed, e)

				name := e.SenderName
				if name == "" {
					name = e.Sender
				}
				bold := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15")).Render(name)

				desc := e.Subject
				if e.Snippet != "" && len(e.Snippet) > len(e.Subject) {
					desc = e.Snippet
				}
				desc = truncate(desc, 55)

				dateStr := formatEmailDate(e.Date)
				fmt.Printf("  %s %s — %s %s\n",
					dimStyle.Render(fmt.Sprintf("%3d.", counter)),
					bold, desc, dimStyle.Render("("+dateStr+")"))
			}
		}
	}

	if len(allDisplayed) > 0 {
		writeEmailRefs(allDisplayed)
		fmt.Println()
		fmt.Println(dimStyle.Render("  Ack: gmail ack 1  •  gmail ack 1-11  •  gmail ack financial:privacy  •  gmail ack from:amazon.com"))
	}
	fmt.Println()
}

func cmdUrgent(db *sqlx.DB) {
	emails, _ := getEmailsByCategory(db, "needs_attention")
	if len(emails) == 0 {
		fmt.Println()
		fmt.Println(successStyle.Render("  ✓ No urgent emails right now."))
		fmt.Println()
		return
	}

	fmt.Println()
	fmt.Println(urgentBadge.Render(" URGENT EMAILS "))
	fmt.Println()
	for i, e := range emails {
		name := e.SenderName
		if name == "" {
			name = e.Sender
		}
		fmt.Printf("  %s %s %s — %s\n",
			dimStyle.Render(fmt.Sprintf("%d.", i+1)),
			errorStyle.Render("!"),
			name, truncate(e.Subject, 55))
	}
	writeEmailRefs(emails)
	fmt.Println()
	fmt.Println(dimStyle.Render("  Ack: gmail ack 1  •  gmail ack 1,2"))
	fmt.Println()
}

func cmdHistory() {
	points, _ := loadTrajectory()
	if len(points) == 0 {
		fmt.Println(dimStyle.Render("  No trajectory data yet. Run: gmail sync"))
		return
	}

	fmt.Println()
	fmt.Println(titleStyle.Render("Unread Email Trajectory"))
	fmt.Println("  " + thinRule())
	fmt.Println()

	display := points
	if len(display) > 14 {
		display = display[len(display)-14:]
	}

	maxCount := 0
	for _, p := range display {
		if p.Count > maxCount {
			maxCount = p.Count
		}
	}
	if maxCount == 0 {
		maxCount = 1
	}

	barWidth := 30
	for _, p := range display {
		label := p.Timestamp.Format("Jan 02 15:04")
		ratio := float64(p.Count) / float64(maxCount)
		color := lipgloss.Color("2")
		if p.Count > 150 {
			color = lipgloss.Color("1")
		} else if p.Count > 80 {
			color = lipgloss.Color("3")
		}
		bar := barChart(ratio, barWidth, color)
		fmt.Printf("  %s  %s %s\n",
			dimStyle.Render(pad(label, 12)),
			bar,
			lipgloss.NewStyle().Bold(true).Render(fmt.Sprintf("%d", p.Count)))
	}

	if len(display) >= 2 {
		diff := display[len(display)-1].Count - display[0].Count
		var trend string
		if diff > 5 {
			trend = errorStyle.Render(fmt.Sprintf("↑ +%d trending up", diff))
		} else if diff < -5 {
			trend = successStyle.Render(fmt.Sprintf("↓ %d trending down", diff))
		} else {
			trend = warnStyle.Render("→ stable")
		}
		fmt.Println()
		fmt.Println("  " + dimStyle.Render("Trend: ") + trend)
	}
	fmt.Println()
}
