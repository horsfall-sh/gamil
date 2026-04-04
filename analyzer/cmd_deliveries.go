package main

import (
	"fmt"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/jmoiron/sqlx"
)

func cmdDeliveries(db *sqlx.DB) {
	now := time.Now()
	today := now.Format("2006-01-02")

	fmt.Println()
	fmt.Println(hRule())
	fmt.Println(titleStyle.Render("Package Tracking"))
	fmt.Println(hRule())

	active, _ := getActiveDeliveries(db)

	fmt.Println()
	fmt.Println(sectionStyle.Foreground(lipgloss.Color("4")).Render("In Transit"))
	fmt.Println()

	if len(active) == 0 {
		fmt.Println("    " + dimStyle.Render("No active deliveries"))
	} else {
		for _, d := range active {
			statusIcon := statusIcon(d.Status)
			expectedStr := ""
			if d.ExpectedDate != "" {
				if d.ExpectedDate == today {
					expectedStr = successStyle.Render("  today")
				} else if d.ExpectedDate < today {
					expectedStr = warnStyle.Render("  overdue")
				} else {
					expectedStr = dimStyle.Render("  " + formatDeliveryDate(d.ExpectedDate))
				}
			}
			trackStr := ""
			if d.TrackingNum != "" {
				trackStr = dimStyle.Render("  " + truncate(d.TrackingNum, 20))
			}
			sourceStr := lipgloss.NewStyle().Foreground(sourceColor(d.Source)).Bold(true).Render(pad(d.Source, 6))
			fmt.Printf("    %s  %s  %s%s%s\n",
				statusIcon,
				sourceStr,
				truncate(d.Description, 38),
				expectedStr,
				trackStr)
		}
	}

	// Recently delivered (last 14 days)
	recent, _ := getRecentDeliveries(db, 14)
	if len(recent) > 0 {
		fmt.Println()
		fmt.Println(sectionStyle.Foreground(lipgloss.Color("8")).Render("Delivered (last 14 days)"))
		fmt.Println()
		for _, d := range recent {
			dateStr := ""
			if d.DeliveredDate != "" {
				dateStr = dimStyle.Render("  " + formatDeliveryDate(d.DeliveredDate))
			}
			fmt.Printf("    %s  %s%s\n",
				successStyle.Render("✓"),
				truncate(d.Description, 42),
				dateStr)
		}
	}

	fmt.Println()
}

func statusIcon(status string) string {
	switch status {
	case "delivered":
		return successStyle.Render("✓")
	case "out_for_delivery":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Bold(true).Render("→")
	case "shipped":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("4")).Render("·")
	default:
		return dimStyle.Render("○")
	}
}

func sourceColor(source string) lipgloss.Color {
	switch source {
	case "amazon":
		return lipgloss.Color("3") // yellow
	case "usps":
		return lipgloss.Color("4") // blue
	case "fedex":
		return lipgloss.Color("13") // magenta
	case "ups":
		return lipgloss.Color("3") // yellow
	default:
		return lipgloss.Color("8")
	}
}

func formatDeliveryDate(dateStr string) string {
	if len(dateStr) < 10 {
		return dateStr
	}
	t, err := time.Parse("2006-01-02", dateStr[:10])
	if err != nil {
		return dateStr
	}
	now := time.Now()
	diff := int(t.Sub(now).Hours() / 24)
	switch {
	case diff == 0:
		return "today"
	case diff == 1:
		return "tomorrow"
	case diff == -1:
		return "yesterday"
	case diff < 0:
		return t.Format("Jan 2") + fmt.Sprintf(" (%d days ago)", -diff)
	default:
		return t.Format("Mon Jan 2")
	}
}
