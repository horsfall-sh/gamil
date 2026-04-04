package main

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/jmoiron/sqlx"
)

type auditMatch struct {
	wf       TxRow
	order    *OrderRow
	daysDiff int
}

func cmdAudit(db *sqlx.DB, months int) {
	since := time.Now().AddDate(0, -months, 0).Format("2006-01-02")

	// Get all Amazon WF charges
	var amazonTxs []TxRow
	err := db.Select(&amazonTxs,
		`SELECT * FROM transactions WHERE category = 'Amazon' AND date >= ? ORDER BY date`, since)
	if err != nil {
		fmt.Println(errorStyle.Render("  DB error: " + err.Error()))
		return
	}

	// Get all Amazon orders
	orders, _ := getOrders(db, since)

	fmt.Println()
	fmt.Println(hRule())
	fmt.Println(titleStyle.Render("Amazon Purchase Audit"))
	fmt.Println(hRule())
	fmt.Printf("  %s  WF Amazon charges: %s  |  Amazon order emails: %s\n",
		dimStyle.Render("Scope:"),
		lipgloss.NewStyle().Bold(true).Render(fmt.Sprintf("%d", len(amazonTxs))),
		lipgloss.NewStyle().Bold(true).Render(fmt.Sprintf("%d", len(orders))))
	fmt.Println()

	// Greedy matching: amount exact match + date proximity (within 3 days)
	orderUsed := make([]bool, len(orders))
	var matched []auditMatch
	var unmatchedWF []TxRow

	for _, wf := range amazonTxs {
		wfDate, _ := time.Parse("2006-01-02", wf.Date[:10])
		bestIdx := -1
		bestDays := 999

		for i, order := range orders {
			if orderUsed[i] {
				continue
			}
			if math.Abs(wf.Amount-order.Total) > 0.01 {
				continue
			}
			orderDate, _ := time.Parse("2006-01-02", order.Date[:10])
			daysDiff := int(math.Abs(wfDate.Sub(orderDate).Hours() / 24))
			if daysDiff <= 3 && daysDiff < bestDays {
				bestIdx = i
				bestDays = daysDiff
			}
		}

		if bestIdx >= 0 {
			orderUsed[bestIdx] = true
			o := orders[bestIdx]
			matched = append(matched, auditMatch{wf: wf, order: &o, daysDiff: bestDays})
		} else {
			unmatchedWF = append(unmatchedWF, wf)
		}
	}

	// Unmatched orders
	var unmatchedOrders []OrderRow
	for i, order := range orders {
		if !orderUsed[i] {
			unmatchedOrders = append(unmatchedOrders, order)
		}
	}

	// Sort matched by date
	sort.Slice(matched, func(i, j int) bool { return matched[i].wf.Date < matched[j].wf.Date })

	// Display matched
	if len(matched) > 0 {
		fmt.Println(matchBadge.Render(fmt.Sprintf(" Matched: %d ", len(matched))))
		fmt.Println()
		for _, m := range matched {
			items := parseItems(m.order.Items)
			itemStr := dimStyle.Render("no items listed")
			if len(items) > 0 {
				itemStr = dimStyle.Render(truncate(strings.Join(items, ", "), 55))
			}
			fmt.Printf("    %s  %s  %s  ↔  %s\n",
				successStyle.Render("✓"),
				amountGreen.Render(fmt.Sprintf("$%-8.2f", m.wf.Amount)),
				dimStyle.Render(m.wf.Date[:10]),
				lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Render(m.order.OrderNum))
			fmt.Printf("      %s\n", itemStr)
		}
	}

	// Unmatched WF — the important flags
	if len(unmatchedWF) > 0 {
		fmt.Println()
		fmt.Println(flagBadge.Render(fmt.Sprintf(" Unmatched WF Charges: %d ", len(unmatchedWF))))
		fmt.Printf("  %s\n\n",
			dimStyle.Render("WF charged Amazon but NO matching Amazon order email found"))
		for _, wf := range unmatchedWF {
			fmt.Printf("    %s  %s  %s  %s\n",
				errorStyle.Render("✗"),
				amountRed.Render(fmt.Sprintf("$%-8.2f", wf.Amount)),
				wf.Date[:10],
				dimStyle.Render(wf.Merchant))
		}
	} else {
		fmt.Println()
		fmt.Println(successStyle.Render("  ✓ All WF Amazon charges have matching order emails"))
	}

	// Unmatched orders
	if len(unmatchedOrders) > 0 {
		fmt.Println()
		fmt.Println(warnStyle.Render(fmt.Sprintf("  ? Unmatched Amazon Orders: %d", len(unmatchedOrders))))
		fmt.Printf("  %s\n\n",
			dimStyle.Render("Amazon order emails with no WF charge alert"))
		for _, o := range unmatchedOrders {
			items := parseItems(o.Items)
			itemStr := ""
			if len(items) > 0 {
				itemStr = dimStyle.Render(truncate(strings.Join(items, ", "), 55))
			}
			fmt.Printf("    %s  %s  %s  %s\n",
				warnStyle.Render("?"),
				amountYellow.Render(fmt.Sprintf("$%-8.2f", o.Total)),
				o.Date[:10],
				lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Render(o.OrderNum))
			if itemStr != "" {
				fmt.Printf("      %s\n", itemStr)
			}
		}
	}

	// Summary
	matchPct := 0.0
	totalAmazon := len(amazonTxs)
	if totalAmazon > 0 {
		matchPct = float64(len(matched)) / float64(totalAmazon) * 100
	}

	// Total Amazon spend
	var amazonTotal float64
	for _, tx := range amazonTxs {
		amazonTotal += tx.Amount
	}

	fmt.Println()
	fmt.Println(hRule())
	fmt.Printf("  %s %.0f%% reconciled  |  %d matched  |  %d unmatched WF  |  %d unmatched orders\n",
		dimStyle.Render("Result:"), matchPct, len(matched), len(unmatchedWF), len(unmatchedOrders))
	fmt.Printf("  %s %s\n",
		dimStyle.Render("Amazon total:"),
		amountBig.Render(fmt.Sprintf("$%.2f", amazonTotal)))
	fmt.Println()
}

func parseItems(itemsJSON string) []string {
	var items []string
	json.Unmarshal([]byte(itemsJSON), &items)
	return items
}
