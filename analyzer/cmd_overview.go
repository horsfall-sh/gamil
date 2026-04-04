package main

import (
	"fmt"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/jmoiron/sqlx"
)

func cmdOverview(db *sqlx.DB) {
	now := time.Now()
	thisMonth := now.Format("2006-01")
	monthStart := now.Format("2006-01") + "-01"
	yearStart := fmt.Sprintf("%d-01-01", now.Year())

	fmt.Println()
	fmt.Println(hRule())
	fmt.Println(titleStyle.Render(fmt.Sprintf("Financial Overview — %s %d", now.Month().String(), now.Year())))
	fmt.Println(hRule())

	// This month spending
	monthTotal, monthCount, _ := getGrandTotal(db, monthStart)
	yearTotal, yearCount, _ := getGrandTotal(db, yearStart)

	// Monthly average (from all data)
	monthly, _ := getMonthlyTotals(db)
	var monthlyAvg float64
	if len(monthly) > 0 {
		var sum float64
		for _, m := range monthly {
			sum += m.Total
		}
		monthlyAvg = sum / float64(len(monthly))
	}

	// Income this month
	var monthIncome float64
	incRows, _ := getIncome(db, monthStart)
	for _, inc := range incRows {
		monthIncome += inc.Amount
	}

	// Payments this month
	var monthPayments float64
	payRows, _ := getPayments(db, monthStart)
	for _, p := range payRows {
		monthPayments += p.Amount
	}

	// This month section
	fmt.Println()
	fmt.Println(sectionStyle.Foreground(lipgloss.Color("15")).Render(fmt.Sprintf("This Month (%s)", thisMonth)))
	fmt.Println()

	fmt.Printf("    Spending:       %s  %s\n",
		amountStyle(monthTotal).Render(fmt.Sprintf("$%.2f", monthTotal)),
		dimStyle.Render(fmt.Sprintf("(%d transactions)", monthCount)))

	if monthIncome > 0 {
		net := monthIncome - monthTotal
		netColor := successStyle
		if net < 0 {
			netColor = errorStyle
		}
		fmt.Printf("    Income:         %s\n",
			amountGreen.Render(fmt.Sprintf("$%.2f", monthIncome)))
		fmt.Printf("    Net:            %s\n",
			netColor.Render(fmt.Sprintf("$%.2f", net)))
	}

	if monthPayments > 0 {
		fmt.Printf("    Payments:       %s\n",
			dimStyle.Render(fmt.Sprintf("$%.2f (loan/CC)", monthPayments)))
	}

	// vs average
	if monthlyAvg > 0 {
		pctOfAvg := monthTotal / monthlyAvg * 100
		label := dimStyle.Render("on track")
		if pctOfAvg > 120 {
			label = errorStyle.Render("↑ above average")
		} else if pctOfAvg < 80 {
			label = successStyle.Render("↓ below average")
		}
		fmt.Printf("    vs Monthly avg: %s  %s\n",
			dimStyle.Render(fmt.Sprintf("$%.0f", monthlyAvg)),
			label)
	}

	// Year-to-date
	fmt.Println()
	fmt.Println(sectionStyle.Foreground(lipgloss.Color("15")).Render("Year to Date"))
	fmt.Println()

	monthsElapsed := float64(now.Month())
	ytdMonthlyAvg := yearTotal / monthsElapsed
	projectedYear := ytdMonthlyAvg * 12

	fmt.Printf("    Total:          %s  (%d transactions)\n",
		amountBig.Render(fmt.Sprintf("$%.2f", yearTotal)), yearCount)
	fmt.Printf("    Monthly avg:    %s\n",
		amountStyle(ytdMonthlyAvg).Render(fmt.Sprintf("$%.2f", ytdMonthlyAvg)))
	fmt.Printf("    Projected year: %s\n",
		amountStyle(projectedYear).Render(fmt.Sprintf("$%.0f", projectedYear)))

	// Top categories this month
	cats, _ := getCategoryTotals(db, monthStart)
	if len(cats) > 0 {
		fmt.Println()
		fmt.Println(sectionStyle.Foreground(lipgloss.Color("15")).Render("Top Categories (this month)"))
		fmt.Println()

		maxCat := cats[0].Total
		limit := 6
		if len(cats) < limit {
			limit = len(cats)
		}
		for i := 0; i < limit; i++ {
			c := cats[i]
			ratio := c.Total / maxCat
			color := catColor(c.Category)
			fmt.Printf("    %s  %s  %s\n",
				lipgloss.NewStyle().Bold(true).Foreground(color).Width(16).Render(c.Category),
				barChart(ratio, 20, color),
				amountStyle(c.Total).Render(fmt.Sprintf("$%.2f", c.Total)))
		}
	}

	// DB stats
	fmt.Println()
	fmt.Println("  " + thinRule())
	var txCount, ordCount, payCount, incCount int
	db.Get(&txCount, "SELECT COUNT(*) FROM transactions")
	db.Get(&ordCount, "SELECT COUNT(*) FROM orders")
	db.Get(&payCount, "SELECT COUNT(*) FROM payments")
	db.Get(&incCount, "SELECT COUNT(*) FROM income")
	fmt.Printf("  %s  %d transactions | %d orders | %d payments | %d income records\n",
		dimStyle.Render("DB:"), txCount, ordCount, payCount, incCount)

	fmt.Println()
}

