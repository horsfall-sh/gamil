package main

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/jmoiron/sqlx"
)

func cmdCashflow(db *sqlx.DB, months int) {
	fmt.Println()
	fmt.Println(hRule())
	fmt.Println(titleStyle.Render("Cash Flow Analysis"))
	fmt.Println(hRule())

	monthlySpend, _ := getMonthlyTotals(db)
	monthlyInc, _ := getMonthlyIncome(db)
	monthlyPay, _ := getMonthlyPayments(db)

	// Build maps
	incMap := map[string]float64{}
	for _, m := range monthlyInc {
		incMap[m.Month] = m.Total
	}
	payMap := map[string]float64{}
	for _, m := range monthlyPay {
		payMap[m.Month] = m.Total
	}

	if len(monthlySpend) == 0 {
		fmt.Println(dimStyle.Render("  No spending data."))
		fmt.Println()
		return
	}

	// Show only recent months based on param
	display := monthlySpend
	if months < 120 && len(display) > months {
		display = display[len(display)-months:]
	}

	fmt.Println()
	fmt.Printf("    %-8s  %10s  %10s  %10s  %s\n",
		dimStyle.Render("Month"),
		dimStyle.Render("Income"),
		dimStyle.Render("Spending"),
		dimStyle.Render("Net"),
		dimStyle.Render(""))

	fmt.Println("    " + thinRule())

	for _, m := range display {
		inc := incMap[m.Month]
		net := inc - m.Total

		incStr := dimStyle.Render("     —    ")
		if inc > 0 {
			incStr = amountGreen.Render(fmt.Sprintf("$%9.0f", inc))
		}

		spendStr := amountStyle(m.Total).Render(fmt.Sprintf("$%9.0f", m.Total))

		netStr := dimStyle.Render("     —    ")
		if inc > 0 {
			if net >= 0 {
				netStr = successStyle.Render(fmt.Sprintf("+$%8.0f", net))
			} else {
				netStr = errorStyle.Render(fmt.Sprintf("-$%8.0f", -net))
			}
		}

		payStr := ""
		if p, ok := payMap[m.Month]; ok && p > 0 {
			payStr = dimStyle.Render(fmt.Sprintf("(+$%.0f payments)", p))
		}

		fmt.Printf("    %-8s  %s  %s  %s  %s\n",
			dimStyle.Render(m.Month), incStr, spendStr, netStr, payStr)
	}

	// Fixed obligations summary
	fmt.Println()
	fmt.Println(sectionStyle.Foreground(lipgloss.Color("15")).Render("Fixed Monthly Obligations (estimated)"))
	fmt.Println()

	// Detect from recurring patterns
	since := sinceDate(months)
	txs, _ := getTransactions(db, since)
	recurring := detectRecurring(txs, 3)
	var fixedTotal float64
	for _, r := range recurring {
		if r.IsActive && r.FreqDays <= 45 {
			fixedTotal += r.MonthlyCost
			fmt.Printf("    %s  %s\n",
				dimStyle.Render(pad(truncate(r.Merchant, 28), 30)),
				amountStyle(r.MonthlyCost).Render(fmt.Sprintf("$%.2f/mo", r.MonthlyCost)))
		}
	}
	fmt.Println("    " + thinRule())
	fmt.Printf("    %s  %s\n",
		dimStyle.Render(pad("Total fixed", 30)),
		amountBig.Render(fmt.Sprintf("$%.2f/mo", fixedTotal)))

	fmt.Println()
}
