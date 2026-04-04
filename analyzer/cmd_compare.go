package main

import (
	"fmt"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/jmoiron/sqlx"
)

// cmdCompare compares two equal-length time windows side by side.
//
//	gmail compare        → this month vs last month
//	gmail compare 3      → last 3 months vs prior 3 months
//	gmail compare year   → this year YTD vs same YTD last year
func cmdCompare(db *sqlx.DB, months int) {
	now := time.Now()

	var labelA, labelB string
	var sinceA, untilA, sinceB, untilB string

	if months == 0 {
		// Default: this month vs last month
		months = 1
		thisStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.Local)
		lastStart := thisStart.AddDate(0, -1, 0)
		lastEnd := thisStart.AddDate(0, 0, -1)

		sinceA = lastStart.Format("2006-01-02")
		untilA = lastEnd.Format("2006-01-02")
		sinceB = thisStart.Format("2006-01-02")
		untilB = now.Format("2006-01-02")

		labelA = lastStart.Format("Jan 2006")
		labelB = thisStart.Format("Jan 2006")
	} else if months == 12 {
		// Year mode: this year YTD vs same period last year
		ytdStart := time.Date(now.Year(), 1, 1, 0, 0, 0, 0, time.Local)
		lastYearStart := ytdStart.AddDate(-1, 0, 0)
		lastYearSameDay := now.AddDate(-1, 0, 0)

		sinceA = lastYearStart.Format("2006-01-02")
		untilA = lastYearSameDay.Format("2006-01-02")
		sinceB = ytdStart.Format("2006-01-02")
		untilB = now.Format("2006-01-02")

		labelA = fmt.Sprintf("%d YTD", now.Year()-1)
		labelB = fmt.Sprintf("%d YTD", now.Year())
	} else {
		// N months: last N months vs prior N months
		periodEnd := now
		periodStart := now.AddDate(0, -months, 0)
		priorEnd := periodStart.AddDate(0, 0, -1)
		priorStart := priorEnd.AddDate(0, -months, 0)

		sinceA = priorStart.Format("2006-01-02")
		untilA = priorEnd.Format("2006-01-02")
		sinceB = periodStart.Format("2006-01-02")
		untilB = periodEnd.Format("2006-01-02")

		labelA = fmt.Sprintf("Prior %dmo", months)
		labelB = fmt.Sprintf("Last %dmo", months)
	}

	totalA, countA, _ := getGrandTotalRange(db, sinceA, untilA)
	totalB, countB, _ := getGrandTotalRange(db, sinceB, untilB)
	catsA, _ := getCategoryTotalsRange(db, sinceA, untilA)
	catsB, _ := getCategoryTotalsRange(db, sinceB, untilB)

	fmt.Println()
	fmt.Println(hRule())
	fmt.Println(titleStyle.Render("Spending Comparison"))
	fmt.Println(hRule())
	fmt.Println()

	// Header
	colW := 18
	fmt.Printf("  %s  %s  %s  %s\n",
		lipgloss.NewStyle().Width(22).Render(""),
		lipgloss.NewStyle().Width(colW).Align(lipgloss.Right).Bold(true).Render(labelA),
		lipgloss.NewStyle().Width(colW).Align(lipgloss.Right).Bold(true).Render(labelB),
		lipgloss.NewStyle().Width(colW).Align(lipgloss.Right).Bold(true).Render("Change"),
	)
	fmt.Println("  " + thinRule())

	// Total row
	delta := totalB - totalA
	pctChange := 0.0
	if totalA > 0 {
		pctChange = (delta / totalA) * 100
	}
	deltaStr := formatDelta(delta, pctChange, false)
	fmt.Printf("  %s  %s  %s  %s\n",
		lipgloss.NewStyle().Width(22).Bold(true).Render("TOTAL"),
		lipgloss.NewStyle().Width(colW).Align(lipgloss.Right).Render(fmt.Sprintf("$%.0f (%d)", totalA, countA)),
		lipgloss.NewStyle().Width(colW).Align(lipgloss.Right).Render(fmt.Sprintf("$%.0f (%d)", totalB, countB)),
		lipgloss.NewStyle().Width(colW).Align(lipgloss.Right).Render(deltaStr),
	)
	fmt.Println("  " + thinRule())

	// Build category map for period A
	catMapA := map[string]float64{}
	for _, c := range catsA {
		catMapA[c.Category] = c.Total
	}
	catMapB := map[string]float64{}
	for _, c := range catsB {
		catMapB[c.Category] = c.Total
	}

	// Union of all categories from both periods, ordered by period B total
	seen := map[string]bool{}
	var cats []string
	for _, c := range catsB {
		if !seen[c.Category] {
			cats = append(cats, c.Category)
			seen[c.Category] = true
		}
	}
	for _, c := range catsA {
		if !seen[c.Category] {
			cats = append(cats, c.Category)
			seen[c.Category] = true
		}
	}

	for _, cat := range cats {
		aTotal := catMapA[cat]
		bTotal := catMapB[cat]
		if aTotal == 0 && bTotal == 0 {
			continue
		}
		d := bTotal - aTotal
		p := 0.0
		newCategory := false
		if aTotal > 0 {
			p = (d / aTotal) * 100
		} else if bTotal > 0 {
			newCategory = true
		}
		color := catColor(cat)
		catLabel := lipgloss.NewStyle().Foreground(color).Width(22).Render(truncate(cat, 20))

		aStr := dimStyle.Render(fmt.Sprintf("$%.0f", aTotal))
		if aTotal == 0 {
			aStr = dimStyle.Render("    —    ")
		}
		bStr := amountStyle(bTotal).Render(fmt.Sprintf("$%.0f", bTotal))
		if bTotal == 0 {
			bStr = dimStyle.Render("    —    ")
		}

		fmt.Printf("  %s  %s  %s  %s\n",
			catLabel,
			lipgloss.NewStyle().Width(colW).Align(lipgloss.Right).Render(aStr),
			lipgloss.NewStyle().Width(colW).Align(lipgloss.Right).Render(bStr),
			lipgloss.NewStyle().Width(colW).Align(lipgloss.Right).Render(formatDelta(d, p, newCategory)),
		)
	}

	fmt.Println()
	fmt.Printf("  %s  %s → %s  |  %s → %s\n",
		dimStyle.Render("Periods:"),
		dimStyle.Render(sinceA), dimStyle.Render(untilA),
		dimStyle.Render(sinceB), dimStyle.Render(untilB),
	)
	fmt.Println()
}

func formatDelta(delta, pct float64, isNew bool) string {
	if delta == 0 {
		return dimStyle.Render("  no change")
	}
	if isNew {
		return errorStyle.Render(fmt.Sprintf("+$%.0f (new)", delta))
	}
	style := errorStyle // spending up = red
	if delta < 0 {
		style = successStyle // spending down = green
		return style.Render(fmt.Sprintf("-$%.0f (%.0f%%)", -delta, pct))
	}
	return style.Render(fmt.Sprintf("+$%.0f (+%.0f%%)", delta, pct))
}
