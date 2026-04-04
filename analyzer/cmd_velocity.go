package main

import (
	"fmt"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/jmoiron/sqlx"
)

func cmdVelocity(db *sqlx.DB, months int) {
	since := sinceDate(months)

	dailies, _ := getDailyTotals(db, since)
	txs, _ := getTransactions(db, since)
	total, count, _ := getGrandTotal(db, since)

	if len(dailies) < 7 {
		fmt.Println(dimStyle.Render("  Not enough data for velocity analysis."))
		return
	}

	fmt.Println()
	fmt.Println(hRule())
	fmt.Println(titleStyle.Render("Spending Velocity"))
	fmt.Println(hRule())

	// Current stats
	daysSince := time.Since(mustParseDate(since)).Hours() / 24
	if daysSince < 1 {
		daysSince = 1
	}
	dailyAvg := total / daysSince
	fmt.Printf("  %s  $%.2f/day avg  |  %d txn over %.0f days\n\n",
		dimStyle.Render("Rate:"),
		dailyAvg, count, daysSince)

	// 7-day rolling velocity sparkline
	velocity := calcVelocity(dailies, 7)
	if len(velocity) > 0 {
		fmt.Println(sectionStyle.Foreground(lipgloss.Color("15")).Render("7-Day Rolling Average"))
		fmt.Println()

		// Sparkline
		var maxV float64
		for _, v := range velocity {
			if v.DayAvg > maxV {
				maxV = v.DayAvg
			}
		}

		// Show last 90 points max
		display := velocity
		if len(display) > 90 {
			display = display[len(display)-90:]
		}

		values := make([]float64, len(display))
		for i, v := range display {
			values[i] = v.DayAvg
		}

		spark := sparkline(values, maxV, func(v float64) lipgloss.Color {
			if v >= 500 {
				return lipgloss.Color("1")
			}
			if v >= 200 {
				return lipgloss.Color("3")
			}
			return lipgloss.Color("2")
		})
		fmt.Println("  " + spark)
		fmt.Printf("  %s%s%s\n",
			dimStyle.Render(display[0].Date[5:]),
			dimStyle.Render(padCenter("", len(display)-len(display[0].Date[5:])-len(display[len(display)-1].Date[5:]))),
			dimStyle.Render(display[len(display)-1].Date[5:]))

		// Current vs previous period
		if len(display) > 14 {
			recent7 := display[len(display)-7:]
			prev7 := display[len(display)-14 : len(display)-7]
			var recentSum, prevSum float64
			for _, v := range recent7 {
				recentSum += v.DayRaw
			}
			for _, v := range prev7 {
				prevSum += v.DayRaw
			}
			recentAvg := recentSum / 7
			prevAvg := prevSum / 7

			change := 0.0
			if prevAvg > 0 {
				change = (recentAvg - prevAvg) / prevAvg * 100
			}

			arrow := "→"
			color := warnStyle
			if change > 10 {
				arrow = "↑"
				color = errorStyle
			} else if change < -10 {
				arrow = "↓"
				color = successStyle
			}

			fmt.Printf("\n  %s  This week: %s ($%.0f/day)  vs  Last week: $%.0f/day  %s\n",
				dimStyle.Render("Trend:"),
				color.Render(fmt.Sprintf("%s %.0f%%", arrow, change)),
				recentAvg, prevAvg,
				dimStyle.Render(""))
		}
	}

	// Hotspots
	fmt.Println()
	fmt.Println(sectionStyle.Foreground(lipgloss.Color("15")).Render("Spending Hotspots (>2σ above mean)"))
	fmt.Println()

	hotspots := findHotspots(dailies, txs, 2.0)
	if len(hotspots) == 0 {
		fmt.Println("    " + dimStyle.Render("No unusual spending days detected."))
	} else {
		limit := 10
		if len(hotspots) < limit {
			limit = len(hotspots)
		}
		for i := 0; i < limit; i++ {
			h := hotspots[i]
			fmt.Printf("    %s  %s  %s  %.1fσ  %s\n",
				errorStyle.Render("!"),
				h.Date,
				amountRed.Render(fmt.Sprintf("$%.2f", h.Amount)),
				h.Sigma,
				dimStyle.Render(truncate(h.TopItem, 35)))
		}
	}

	// Monthly velocity comparison
	monthly, _ := getMonthlyTotals(db)
	if len(monthly) > 1 {
		fmt.Println()
		fmt.Println(sectionStyle.Foreground(lipgloss.Color("15")).Render("Month-over-Month Change"))
		fmt.Println()

		for i := 1; i < len(monthly); i++ {
			prev := monthly[i-1].Total
			curr := monthly[i].Total
			change := 0.0
			if prev > 0 {
				change = (curr - prev) / prev * 100
			}

			arrow := "→"
			color := dimStyle
			if change > 15 {
				arrow = "↑"
				color = errorStyle
			} else if change < -15 {
				arrow = "↓"
				color = successStyle
			}

			fmt.Printf("    %s  %s  %s %s\n",
				dimStyle.Render(monthly[i].Month),
				amountStyle(curr).Render(fmt.Sprintf("$%7.0f", curr)),
				color.Render(fmt.Sprintf("%s %+.0f%%", arrow, change)),
				dimStyle.Render(fmt.Sprintf("(vs $%.0f)", prev)))
		}
	}

	fmt.Println()
}

func mustParseDate(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Now().AddDate(-1, 0, 0)
	}
	return t
}
