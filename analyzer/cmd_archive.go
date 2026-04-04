package main

import (
	"fmt"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/jmoiron/sqlx"
)

var archiveReasonLabels = map[string]string{
	"wf_purchase":          "WF purchase alerts      (ingested → DB)",
	"amazon_shipping":      "Amazon ship/deliver     (ingested → DB)",
	"usps_digest":          "USPS daily digests",
	"political":            "Political fundraising",
	"promotional":          "Promotional / marketing",
	"calendar_notification":"Google Calendar notices",
	"payment_reminder":     "Payment reminders       (ingested → DB)",
	"subscription_receipt": "Subscription receipts   (ingested → DB)",
	"social_notification":  "Social / app notifications",
	"medical_reminder":     "Medical reminders       (deduplicated)",
	"chewy_shipping":       "Chewy autoship updates  (ingested → DB)",
	"newsletter":           "Newsletters / digests",
}

func cmdArchive(db *sqlx.DB) {
	now := time.Now()

	fmt.Println()
	fmt.Println(hRule())
	fmt.Println(titleStyle.Render("Auto-Archive Transparency Report"))
	fmt.Println(hRule())

	periods := []struct {
		label string
		days  int
	}{
		{"Today", 1},
		{"Last 7 days", 7},
		{"Last 30 days", 30},
	}

	for _, p := range periods {
		since := now.AddDate(0, 0, -p.days).Format(time.RFC3339)
		total, _ := getArchiveTotalCount(db, since)
		if total == 0 {
			continue
		}
		stats, _ := getArchiveStats(db, since)

		fmt.Println()
		fmt.Printf("  %s  %s\n",
			sectionStyle.Foreground(lipgloss.Color("8")).Render(p.label),
			lipgloss.NewStyle().Bold(true).Render(fmt.Sprintf("%d emails silently handled", total)))
		fmt.Println("  " + thinRule())

		for _, s := range stats {
			label := s.Reason
			if l, ok := archiveReasonLabels[s.Reason]; ok {
				label = l
			}
			bar := barChart(float64(s.Count)/float64(total), 20, lipgloss.Color("8"))
			fmt.Printf("    %s  %s  %s\n",
				dimStyle.Render(fmt.Sprintf("%3d", s.Count)),
				bar,
				dimStyle.Render(label))
		}
	}

	// How much noise has been eliminated since the beginning
	var allTimeTotal int
	db.Get(&allTimeTotal, `SELECT COUNT(*) FROM archive_log`)
	if allTimeTotal > 0 {
		fmt.Println()
		fmt.Printf("  %s  %s\n",
			dimStyle.Render("All time:"),
			lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Bold(true).Render(
				fmt.Sprintf("%d emails auto-handled so you didn't have to", allTimeTotal)))
	}

	fmt.Println()
}
