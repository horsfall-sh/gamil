package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/jmoiron/sqlx"
)

func cmdImpulse(db *sqlx.DB, months int) {
	since := sinceDate(months)

	txs, _ := getTransactions(db, since)
	if len(txs) == 0 {
		fmt.Println(dimStyle.Render("  No transaction data."))
		return
	}

	fmt.Println()
	fmt.Println(hRule())
	fmt.Println(titleStyle.Render("Impulse & Behavioral Analysis"))
	fmt.Println(hRule())

	// Day-of-week heatmap
	dowData, _ := getDowTotals(db, since)
	if len(dowData) > 0 {
		fmt.Println()
		fmt.Println(sectionStyle.Foreground(lipgloss.Color("15")).Render("Day-of-Week Spending"))
		fmt.Println()

		dowNames := []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}
		dowTotals := make(map[int]float64)
		dowCounts := make(map[int]int)
		for _, d := range dowData {
			dowTotals[d.Dow] = d.Total
			dowCounts[d.Dow] = d.Count
		}

		var maxDow float64
		for _, t := range dowTotals {
			if t > maxDow {
				maxDow = t
			}
		}

		// Count weeks for averaging
		dailies, _ := getDailyTotals(db, since)
		weekCount := float64(len(dailies)) / 7.0
		if weekCount < 1 {
			weekCount = 1
		}

		maxDowIdx := 0
		for i := 0; i < 7; i++ {
			if dowTotals[i] > dowTotals[maxDowIdx] {
				maxDowIdx = i
			}
		}

		for i := 0; i < 7; i++ {
			total := dowTotals[i]
			ratio := 0.0
			if maxDow > 0 {
				ratio = total / maxDow
			}
			weeklyAvg := total / weekCount

			label := ""
			if i == maxDowIdx {
				label = " ← peak"
			}

			color := lipgloss.Color("2")
			if weeklyAvg >= 1000 {
				color = lipgloss.Color("1")
			} else if weeklyAvg >= 500 {
				color = lipgloss.Color("3")
			}

			fmt.Printf("    %s  %s  %s  %s%s\n",
				dimStyle.Render(dowNames[i]),
				barChart(ratio, 20, color),
				amountStyle(weeklyAvg).Render(fmt.Sprintf("$%.0f/wk", weeklyAvg)),
				dimStyle.Render(fmt.Sprintf("(%d txn)", dowCounts[i])),
				warnStyle.Render(label))
		}
	}

	// Buying sprees (3+ transactions same day)
	fmt.Println()
	fmt.Println(sectionStyle.Foreground(lipgloss.Color("15")).Render("Buying Sprees (3+ purchases/day)"))
	fmt.Println()

	sessions := detectSpendingSessions(txs, 24, 3)
	if len(sessions) == 0 {
		fmt.Println("    " + dimStyle.Render("No buying sprees detected."))
	} else {
		limit := 12
		if len(sessions) < limit {
			limit = len(sessions)
		}
		for i := 0; i < limit; i++ {
			s := sessions[i]
			typeLabel := ""
			if s.Type == "spree" {
				typeLabel = errorStyle.Render(" SPREE")
			}
			cats := strings.Join(s.Categories, ", ")
			fmt.Printf("    %s  %s  %s  %s  %s%s\n",
				errorStyle.Render("!"),
				s.Date,
				amountStyle(s.Total).Render(fmt.Sprintf("$%.2f", s.Total)),
				dimStyle.Render(fmt.Sprintf("%d purchases", s.TxCount)),
				dimStyle.Render(truncate(cats, 25)),
				typeLabel)
		}
	}

	// Amazon frequency analysis
	fmt.Println()
	fmt.Println(sectionStyle.Foreground(lipgloss.Color("15")).Render("Amazon Ordering Frequency"))
	fmt.Println()

	var amazonTxs []TxRow
	for _, tx := range txs {
		if tx.Category == "Amazon" {
			amazonTxs = append(amazonTxs, tx)
		}
	}

	if len(amazonTxs) > 0 {
		// Orders per week
		dailies, _ := getDailyTotals(db, since)
		weeks := float64(len(dailies)) / 7.0
		if weeks < 1 {
			weeks = 1
		}
		ordersPerWeek := float64(len(amazonTxs)) / weeks

		var amazonTotal float64
		for _, tx := range amazonTxs {
			amazonTotal += tx.Amount
		}
		avgOrder := amazonTotal / float64(len(amazonTxs))

		weeklySpend := amazonTotal / weeks

		fmt.Printf("    Orders/week:   %s\n",
			lipgloss.NewStyle().Bold(true).Render(fmt.Sprintf("%.1f", ordersPerWeek)))
		fmt.Printf("    Avg order:     %s\n",
			amountStyle(avgOrder).Render(fmt.Sprintf("$%.2f", avgOrder)))
		fmt.Printf("    Weekly spend:  %s\n",
			amountStyle(weeklySpend).Render(fmt.Sprintf("$%.2f/wk", weeklySpend)))

		// Monthly Amazon trend
		amazonByMonth := map[string]int{}
		for _, tx := range amazonTxs {
			m := tx.Date[:7]
			amazonByMonth[m]++
		}
		var months []string
		for m := range amazonByMonth {
			months = append(months, m)
		}
		sort.Strings(months)

		if len(months) > 1 {
			fmt.Println()
			fmt.Println("    " + dimStyle.Render("Monthly order count:"))
			maxOrders := 0
			for _, c := range amazonByMonth {
				if c > maxOrders {
					maxOrders = c
				}
			}
			for _, m := range months {
				c := amazonByMonth[m]
				ratio := float64(c) / float64(maxOrders)
				color := lipgloss.Color("3")
				if c > 40 {
					color = lipgloss.Color("1")
				}
				fmt.Printf("    %s  %s  %s\n",
					dimStyle.Render(m),
					barChart(ratio, 15, color),
					dimStyle.Render(fmt.Sprintf("%d orders", c)))
			}
		}
	}

	// Comfort spending signals
	fmt.Println()
	fmt.Println(sectionStyle.Foreground(lipgloss.Color("15")).Render("Comfort Spending Signals"))
	fmt.Println()

	var foodDeliveryCount int
	var foodDeliveryTotal float64
	for _, tx := range txs {
		upper := strings.ToUpper(tx.Merchant)
		if strings.Contains(upper, "DOORDASH") || strings.Contains(upper, "UBEREATS") ||
			strings.Contains(upper, "GRUBHUB") {
			foodDeliveryCount++
			foodDeliveryTotal += tx.Amount
		}
	}
	if foodDeliveryCount > 0 {
		avgDelivery := foodDeliveryTotal / float64(foodDeliveryCount)
		fmt.Printf("    Food delivery: %d orders, avg %s, total %s\n",
			foodDeliveryCount,
			amountStyle(avgDelivery).Render(fmt.Sprintf("$%.2f", avgDelivery)),
			amountStyle(foodDeliveryTotal).Render(fmt.Sprintf("$%.2f", foodDeliveryTotal)))
	}

	// Small purchase ratio (< $20)
	var smallCount int
	var smallTotal float64
	for _, tx := range txs {
		if tx.Amount < 20 {
			smallCount++
			smallTotal += tx.Amount
		}
	}
	smallPct := float64(smallCount) / float64(len(txs)) * 100
	fmt.Printf("    Small purchases (<$20): %d (%.0f%% of all), total %s\n",
		smallCount, smallPct,
		amountStyle(smallTotal).Render(fmt.Sprintf("$%.2f", smallTotal)))

	fmt.Println()
}
