package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/jmoiron/sqlx"
)

func cmdPatterns(db *sqlx.DB, months int) {
	since := sinceDate(months)

	txs, _ := getTransactions(db, since)
	if len(txs) == 0 {
		fmt.Println(dimStyle.Render("  No transaction data. Run: gmail ingest"))
		return
	}

	recurring := detectRecurring(txs, 3)

	fmt.Println()
	fmt.Println(hRule())
	fmt.Println(titleStyle.Render("Recurring Charges & Subscriptions"))
	fmt.Println(hRule())

	// Separate subscriptions (freq <= 45 days) from periodic (45-200) and annual (>200)
	var subs, periodic, annual []RecurringCharge
	var totalMonthlySubs float64
	var activeSubs int

	for _, r := range recurring {
		if r.FreqDays <= 45 {
			subs = append(subs, r)
			if r.IsActive {
				totalMonthlySubs += r.MonthlyCost
				activeSubs++
			}
		} else if r.FreqDays <= 200 {
			periodic = append(periodic, r)
			if r.IsActive {
				totalMonthlySubs += r.MonthlyCost
			}
		} else {
			annual = append(annual, r)
			if r.IsActive {
				totalMonthlySubs += r.MonthlyCost
			}
		}
	}

	fmt.Printf("  %s  Active subscriptions: %s  |  Est. monthly burn: %s\n\n",
		dimStyle.Render("Summary:"),
		lipgloss.NewStyle().Bold(true).Render(fmt.Sprintf("%d", activeSubs)),
		amountRed.Render(fmt.Sprintf("$%.2f/mo", totalMonthlySubs)))

	// Monthly subscriptions
	if len(subs) > 0 {
		fmt.Println(sectionStyle.Foreground(lipgloss.Color("15")).Render("Monthly Subscriptions"))
		fmt.Println()
		for _, r := range subs {
			status := successStyle.Render("✓")
			if !r.IsActive {
				status = dimStyle.Render("✗")
			}
			priceNote := ""
			if r.PriceChanged {
				priceNote = warnStyle.Render(" ↑ price changed")
			}
			activeNote := ""
			if !r.IsActive {
				activeNote = dimStyle.Render(" INACTIVE")
			}
			fmt.Printf("    %s  %-28s  %s  ~%s%s%s\n",
				status,
				truncate(r.Merchant, 28),
				amountStyle(r.AvgAmount).Render(fmt.Sprintf("$%.2f", r.AvgAmount)),
				dimStyle.Render(fmt.Sprintf("every %dd", r.FreqDays)),
				priceNote, activeNote)
		}
	}

	// Periodic (bi-monthly, quarterly, semi-annual)
	if len(periodic) > 0 {
		fmt.Println()
		fmt.Println(sectionStyle.Foreground(lipgloss.Color("15")).Render("Periodic Charges"))
		fmt.Println()
		for _, r := range periodic {
			status := successStyle.Render("✓")
			if !r.IsActive {
				status = dimStyle.Render("✗")
			}
			freq := fmt.Sprintf("every %dd", r.FreqDays)
			if r.FreqDays > 150 && r.FreqDays < 200 {
				freq = "~semi-annual"
			} else if r.FreqDays > 80 && r.FreqDays < 100 {
				freq = "~quarterly"
			}
			fmt.Printf("    %s  %-28s  %s  %s  %s\n",
				status,
				truncate(r.Merchant, 28),
				amountStyle(r.AvgAmount).Render(fmt.Sprintf("$%.2f", r.AvgAmount)),
				dimStyle.Render(freq),
				dimStyle.Render(fmt.Sprintf("(%dx)", r.Occurrences)))
		}
	}

	// Annual
	if len(annual) > 0 {
		fmt.Println()
		fmt.Println(sectionStyle.Foreground(lipgloss.Color("15")).Render("Annual Charges"))
		fmt.Println()
		for _, r := range annual {
			status := successStyle.Render("✓")
			if !r.IsActive {
				status = dimStyle.Render("✗")
			}
			fmt.Printf("    %s  %-28s  %s  %s\n",
				status,
				truncate(r.Merchant, 28),
				amountStyle(r.AvgAmount).Render(fmt.Sprintf("$%.2f", r.AvgAmount)),
				dimStyle.Render(fmt.Sprintf("~every %d days", r.FreqDays)))
		}
	}

	// Top repeat merchants (non-subscription — just frequent)
	fmt.Println()
	fmt.Println(sectionStyle.Foreground(lipgloss.Color("15")).Render("Most Frequent Merchants"))
	fmt.Println()

	merchFreq, _ := getMerchantFrequency(db, since)
	shown := 0
	for _, m := range merchFreq {
		if shown >= 10 {
			break
		}
		// Skip if already shown as recurring
		isRecurring := false
		for _, r := range recurring {
			if r.Merchant == m.Merchant {
				isRecurring = true
				break
			}
		}
		if isRecurring {
			continue
		}
		catColor := catColor(m.Category)
		fmt.Printf("    %s  %-28s  %s  %s  %s\n",
			lipgloss.NewStyle().Foreground(catColor).Render("•"),
			truncate(m.Merchant, 28),
			amountStyle(m.Total).Render(fmt.Sprintf("$%.2f", m.Total)),
			dimStyle.Render(fmt.Sprintf("%dx", m.Count)),
			dimStyle.Render(fmt.Sprintf("avg $%.2f", m.AvgAmount)))
		shown++
	}

	// "Other" category deep dive
	var otherCount int
	var otherTotal float64
	var otherMerchants []string
	for _, tx := range txs {
		if tx.Category == "Other" {
			otherCount++
			otherTotal += tx.Amount
		}
	}
	if otherCount > 0 {
		// Find unique Other merchants
		otherSet := map[string]float64{}
		for _, tx := range txs {
			if tx.Category == "Other" {
				otherSet[tx.Merchant] += tx.Amount
			}
		}
		type kv struct {
			K string
			V float64
		}
		var sorted []kv
		for k, v := range otherSet {
			sorted = append(sorted, kv{k, v})
		}
		for i := 0; i < len(sorted); i++ {
			for j := i + 1; j < len(sorted); j++ {
				if sorted[j].V > sorted[i].V {
					sorted[i], sorted[j] = sorted[j], sorted[i]
				}
			}
		}
		for i, kv := range sorted {
			if i >= 5 {
				break
			}
			otherMerchants = append(otherMerchants, fmt.Sprintf("%s ($%.0f)", kv.K, kv.V))
		}

		fmt.Println()
		fmt.Println(warnStyle.Render(fmt.Sprintf("  ⚠ Uncategorized 'Other': $%.2f across %d transactions", otherTotal, otherCount)))
		fmt.Printf("    %s\n", dimStyle.Render("Top: "+strings.Join(otherMerchants, ", ")))
	}

	fmt.Println()
}
