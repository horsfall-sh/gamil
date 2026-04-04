package main

import (
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/jmoiron/sqlx"
)

func cmdSpending(db *sqlx.DB, months int) {
	since := time.Now().AddDate(0, -months, 0).Format("2006-01-02")
	periodLabel := fmt.Sprintf("last %d months", months)
	if months == 1 {
		periodLabel = "last 30 days"
	} else if months == 12 {
		periodLabel = "last year"
	}

	total, count, err := getGrandTotal(db, since)
	if err != nil || count == 0 {
		fmt.Println(dimStyle.Render("  No transaction data. Run: gmail ingest"))
		return
	}

	// Header
	fmt.Println()
	fmt.Println(hRule())
	fmt.Println(titleStyle.Render(fmt.Sprintf("Spending Analysis — %s", periodLabel)))
	fmt.Println(hRule())
	fmt.Printf("  %s  %s  %s\n",
		dimStyle.Render("Total:"),
		amountBig.Render(fmt.Sprintf("$%.2f", total)),
		dimStyle.Render(fmt.Sprintf("across %d transactions", count)))
	fmt.Println()

	// Category breakdown
	cats, _ := getCategoryTotals(db, since)
	if len(cats) > 0 {
		fmt.Println(sectionStyle.Foreground(lipgloss.Color("15")).Render("Category Breakdown"))
		fmt.Println()

		maxCat := cats[0].Total
		barWidth := 28
		for _, c := range cats {
			ratio := c.Total / maxCat
			color := catColor(c.Category)
			pct := c.Total / total * 100
			fmt.Printf("  %s %s %s %s\n",
				lipgloss.NewStyle().Bold(true).Foreground(color).Width(18).Render(c.Category),
				barChart(ratio, barWidth, color),
				amountStyle(c.Total).Render(fmt.Sprintf("$%8.2f", c.Total)),
				dimStyle.Render(fmt.Sprintf("(%d, %.0f%%)", c.Count, pct)))
		}
	}

	// Monthly trend
	monthlyData, _ := getMonthlyTotals(db)
	if len(monthlyData) > 1 {
		fmt.Println()
		fmt.Println(sectionStyle.Foreground(lipgloss.Color("15")).Render("Monthly Spending"))
		fmt.Println()

		var maxMonth float64
		for _, m := range monthlyData {
			if m.Total > maxMonth {
				maxMonth = m.Total
			}
		}

		barWidth := 28
		for _, m := range monthlyData {
			ratio := m.Total / maxMonth
			color := lipgloss.Color("14")
			if m.Total > 5000 {
				color = lipgloss.Color("1")
			} else if m.Total > 2000 {
				color = lipgloss.Color("3")
			} else {
				color = lipgloss.Color("2")
			}
			fmt.Printf("  %s %s %s %s\n",
				dimStyle.Render(pad(m.Month, 8)),
				barChart(ratio, barWidth, color),
				amountStyle(m.Total).Render(fmt.Sprintf("$%8.2f", m.Total)),
				dimStyle.Render(fmt.Sprintf("(%d txn)", m.Count)))
		}

		// Monthly average
		var sum float64
		for _, m := range monthlyData {
			sum += m.Total
		}
		avg := sum / float64(len(monthlyData))
		fmt.Println()
		fmt.Printf("  %s %s\n",
			dimStyle.Render("Monthly avg:"),
			lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("14")).Render(
				fmt.Sprintf("$%.2f", avg)))
	}

	// Daily sparkline (last 60 days)
	dailySince := time.Now().AddDate(0, 0, -60).Format("2006-01-02")
	dailyData, _ := getDailyTotals(db, dailySince)
	if len(dailyData) > 5 {
		fmt.Println()
		fmt.Println(sectionStyle.Foreground(lipgloss.Color("15")).Render("Daily Spending (60 days)"))
		fmt.Println()

		// Fill in all 60 days
		dailyMap := map[string]float64{}
		for _, d := range dailyData {
			dailyMap[d.Day] = d.Total
		}
		values := make([]float64, 60)
		var maxDaily float64
		now := time.Now()
		for i := 59; i >= 0; i-- {
			day := now.AddDate(0, 0, -i).Format("2006-01-02")
			v := dailyMap[day]
			values[59-i] = v
			if v > maxDaily {
				maxDaily = v
			}
		}

		spark := sparkline(values, maxDaily, func(v float64) lipgloss.Color {
			if v >= 500 {
				return lipgloss.Color("1")
			}
			if v >= 100 {
				return lipgloss.Color("3")
			}
			return lipgloss.Color("2")
		})
		fmt.Println("  " + spark)
		startLabel := now.AddDate(0, 0, -59).Format("Jan 02")
		endLabel := now.Format("Jan 02")
		gap := 60 - len(startLabel) - len(endLabel)
		if gap < 1 {
			gap = 1
		}
		fmt.Printf("  %s%s%s\n", dimStyle.Render(startLabel),
			dimStyle.Render(padCenter("", gap)), dimStyle.Render(endLabel))
	}

	// Top transactions
	txs, _ := getTransactions(db, since)
	if len(txs) > 0 {
		fmt.Println()
		fmt.Println(sectionStyle.Foreground(lipgloss.Color("15")).Render("Largest Transactions"))
		fmt.Println()

		sort.Slice(txs, func(i, j int) bool { return txs[i].Amount > txs[j].Amount })
		limit := 8
		if len(txs) < limit {
			limit = len(txs)
		}
		for i := 0; i < limit; i++ {
			tx := txs[i]
			fmt.Printf("    %s  %-32s %s\n",
				amountStyle(tx.Amount).Render(fmt.Sprintf("$%8.2f", tx.Amount)),
				truncate(tx.Merchant, 32),
				dimStyle.Render(tx.Date[:10]))
		}

		// Anomaly detection: >2σ
		if len(txs) > 5 {
			var sum, sumSq float64
			for _, tx := range txs {
				sum += tx.Amount
				sumSq += tx.Amount * tx.Amount
			}
			n := float64(len(txs))
			mean := sum / n
			stddev := math.Sqrt(sumSq/n - mean*mean)
			threshold := mean + 2*stddev

			var anomalies []TxRow
			for _, tx := range txs {
				if tx.Amount > threshold {
					anomalies = append(anomalies, tx)
				}
			}
			if len(anomalies) > 0 {
				fmt.Println()
				fmt.Println(flagBadge.Render(fmt.Sprintf(" Anomalies (>2σ, mean $%.0f ± $%.0f) ", mean, stddev)))
				fmt.Println()
				for _, tx := range anomalies {
					fmt.Printf("    %s %s at %s on %s\n",
						errorStyle.Render("!"),
						amountRed.Render(fmt.Sprintf("$%.2f", tx.Amount)),
						tx.Merchant, tx.Date[:10])
				}
			}
		}
	}

	fmt.Println()
}

