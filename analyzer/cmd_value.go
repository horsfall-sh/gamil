package main

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/jmoiron/sqlx"
)

func cmdValue(db *sqlx.DB, months int) {
	since := sinceDate(months)

	txs, _ := getTransactions(db, since)
	if len(txs) == 0 {
		fmt.Println(dimStyle.Render("  No transaction data."))
		return
	}

	fmt.Println()
	fmt.Println(hRule())
	fmt.Println(titleStyle.Render("Spending Value Analysis"))
	fmt.Println(hRule())

	// Count same-day transactions for impulse scoring
	dayCount := map[string]int{}
	for _, tx := range txs {
		dayCount[tx.Date[:10]]++
	}

	// Score all transactions
	var essentialTotal, intentionalTotal, impulsiveTotal float64
	var essentialCount, intentionalCount, impulsiveCount int
	catBuckets := map[string]map[string]float64{} // category -> bucket -> total

	for _, tx := range txs {
		score := scoreImpulse(tx, dayCount[tx.Date[:10]])
		bucket := valueBucket(score)

		switch bucket {
		case "essential":
			essentialTotal += tx.Amount
			essentialCount++
		case "intentional":
			intentionalTotal += tx.Amount
			intentionalCount++
		case "impulsive":
			impulsiveTotal += tx.Amount
			impulsiveCount++
		}

		if catBuckets[tx.Category] == nil {
			catBuckets[tx.Category] = map[string]float64{}
		}
		catBuckets[tx.Category][bucket] += tx.Amount
	}

	grandTotal := essentialTotal + intentionalTotal + impulsiveTotal

	// Summary
	fmt.Println()
	fmt.Println(sectionStyle.Foreground(lipgloss.Color("15")).Render("Value Breakdown"))
	fmt.Println()

	essentialPct := essentialTotal / grandTotal * 100
	intentionalPct := intentionalTotal / grandTotal * 100
	impulsivePct := impulsiveTotal / grandTotal * 100

	barW := 30
	fmt.Printf("    %s  %s  %s  %s\n",
		successStyle.Render(pad("Essential", 12)),
		barChart(essentialTotal/grandTotal, barW, lipgloss.Color("2")),
		amountGreen.Render(fmt.Sprintf("$%8.2f", essentialTotal)),
		dimStyle.Render(fmt.Sprintf("%.0f%% (%d txn)", essentialPct, essentialCount)))
	fmt.Printf("    %s  %s  %s  %s\n",
		warnStyle.Render(pad("Intentional", 12)),
		barChart(intentionalTotal/grandTotal, barW, lipgloss.Color("3")),
		amountYellow.Render(fmt.Sprintf("$%8.2f", intentionalTotal)),
		dimStyle.Render(fmt.Sprintf("%.0f%% (%d txn)", intentionalPct, intentionalCount)))
	fmt.Printf("    %s  %s  %s  %s\n",
		errorStyle.Render(pad("Impulsive", 12)),
		barChart(impulsiveTotal/grandTotal, barW, lipgloss.Color("1")),
		amountRed.Render(fmt.Sprintf("$%8.2f", impulsiveTotal)),
		dimStyle.Render(fmt.Sprintf("%.0f%% (%d txn)", impulsivePct, impulsiveCount)))

	// Per-category breakdown
	fmt.Println()
	fmt.Println(sectionStyle.Foreground(lipgloss.Color("15")).Render("Category Value Breakdown"))
	fmt.Println()

	// Sort categories by total
	type catEntry struct {
		Name    string
		Total   float64
		Buckets map[string]float64
	}
	var sortedCats []catEntry
	for cat, buckets := range catBuckets {
		total := buckets["essential"] + buckets["intentional"] + buckets["impulsive"]
		sortedCats = append(sortedCats, catEntry{cat, total, buckets})
	}
	sort.Slice(sortedCats, func(i, j int) bool { return sortedCats[i].Total > sortedCats[j].Total })

	for _, ce := range sortedCats {
		if ce.Total < 50 {
			continue
		}
		impPct := ce.Buckets["impulsive"] / ce.Total * 100
		intPct := ce.Buckets["intentional"] / ce.Total * 100
		essPct := ce.Buckets["essential"] / ce.Total * 100

		// Mini stacked bar
		totalW := 20
		essW := int(math.Round(essPct / 100 * float64(totalW)))
		intW := int(math.Round(intPct / 100 * float64(totalW)))
		impW := totalW - essW - intW
		if impW < 0 {
			impW = 0
		}

		stackedBar := lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Render(strings.Repeat("█", essW)) +
			lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Render(strings.Repeat("█", intW)) +
			lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Render(strings.Repeat("█", impW))

		label := ""
		if impPct > 60 {
			label = errorStyle.Render(" ← mostly impulse")
		} else if essPct > 70 {
			label = successStyle.Render(" ← essential")
		}

		color := catColor(ce.Name)
		fmt.Printf("    %s  %s  %s%s\n",
			lipgloss.NewStyle().Bold(true).Foreground(color).Width(16).Render(ce.Name),
			stackedBar,
			amountStyle(ce.Total).Render(fmt.Sprintf("$%.0f", ce.Total)),
			label)
	}
	fmt.Printf("\n    %s %s %s %s\n",
		dimStyle.Render("Legend:"),
		successStyle.Render("██ essential"),
		warnStyle.Render("██ intentional"),
		errorStyle.Render("██ impulsive"))

	// Amazon deep dive
	fmt.Println()
	fmt.Println(sectionStyle.Foreground(lipgloss.Color("15")).Render("Amazon Deep Dive"))
	fmt.Println()

	var amazonTxs []TxRow
	for _, tx := range txs {
		if tx.Category == "Amazon" {
			amazonTxs = append(amazonTxs, tx)
		}
	}
	if len(amazonTxs) > 0 {
		var amzTotal float64
		for _, tx := range amazonTxs {
			amzTotal += tx.Amount
		}
		avgOrder := amzTotal / float64(len(amazonTxs))

		// Small Amazon orders (< $25) — likely impulse
		var smallAmz int
		var smallAmzTotal float64
		for _, tx := range amazonTxs {
			if tx.Amount < 25 {
				smallAmz++
				smallAmzTotal += tx.Amount
			}
		}
		smallPct := float64(smallAmz) / float64(len(amazonTxs)) * 100

		fmt.Printf("    Total: %s across %d orders (avg %s)\n",
			amountStyle(amzTotal).Render(fmt.Sprintf("$%.2f", amzTotal)),
			len(amazonTxs),
			amountStyle(avgOrder).Render(fmt.Sprintf("$%.2f", avgOrder)))
		fmt.Printf("    Small orders (<$25): %d (%.0f%%) totaling %s\n",
			smallAmz, smallPct,
			amountStyle(smallAmzTotal).Render(fmt.Sprintf("$%.2f", smallAmzTotal)))
	}

	// Quick wins / savings suggestions
	fmt.Println()
	fmt.Println(flagBadge.Render(" Quick Wins to Spend Smarter "))
	fmt.Println()

	if impulsiveTotal > 0 {
		fmt.Printf("    %s Impulse spending is %s — reducing by 25%% saves %s/year\n",
			warnStyle.Render("→"),
			amountRed.Render(fmt.Sprintf("$%.0f", impulsiveTotal)),
			successStyle.Render(fmt.Sprintf("$%.0f", impulsiveTotal*0.25*12/float64(months))))
	}

	_, otherTotal := countOther(txs)
	if otherTotal > 1000 {
		fmt.Printf("    %s Uncategorized 'Other' is %s — review to find hidden patterns\n",
			warnStyle.Render("→"),
			amountYellow.Render(fmt.Sprintf("$%.0f", otherTotal)))
	}

	if len(amazonTxs) > 0 {
		var smallAmzTotal float64
		for _, tx := range amazonTxs {
			if tx.Amount < 25 {
				smallAmzTotal += tx.Amount
			}
		}
		if smallAmzTotal > 500 {
			fmt.Printf("    %s Small Amazon orders add up: %s — consolidate or add friction\n",
				warnStyle.Render("→"),
				amountYellow.Render(fmt.Sprintf("$%.0f", smallAmzTotal)))
		}
	}

	fmt.Println()
}

func countOther(txs []TxRow) (int, float64) {
	var count int
	var total float64
	for _, tx := range txs {
		if tx.Category == "Other" {
			count++
			total += tx.Amount
		}
	}
	return count, total
}
