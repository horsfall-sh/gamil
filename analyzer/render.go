package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Styles
var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("14")). // cyan
			PaddingLeft(1)

	subtitleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("8")). // dim
			PaddingLeft(2)

	sectionStyle = lipgloss.NewStyle().
			Bold(true).
			PaddingLeft(2)

	dimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("8"))

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("1")).
			Bold(true)

	successStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("2")).
			Bold(true)

	warnStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("3")).
			Bold(true)

	urgentBadge = lipgloss.NewStyle().
			Background(lipgloss.Color("1")).
			Foreground(lipgloss.Color("15")).
			Bold(true).
			Padding(0, 1)

	matchBadge = lipgloss.NewStyle().
			Background(lipgloss.Color("2")).
			Foreground(lipgloss.Color("15")).
			Bold(true).
			Padding(0, 1)

	flagBadge = lipgloss.NewStyle().
			Background(lipgloss.Color("1")).
			Foreground(lipgloss.Color("15")).
			Bold(true).
			Padding(0, 1)

	borderStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("14"))

	amountBig = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("15"))

	amountGreen = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("2"))

	amountYellow = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("3"))

	amountRed = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("1"))
)

var categoryStyleMap = map[string]lipgloss.Color{
	"Amazon":          lipgloss.Color("3"),   // yellow
	"Restaurants":     lipgloss.Color("5"),   // magenta
	"Furniture":       lipgloss.Color("4"),   // blue
	"Subscriptions":   lipgloss.Color("14"),  // cyan
	"Groceries":       lipgloss.Color("2"),   // green
	"Auto/Transport":  lipgloss.Color("15"),  // white
	"Utilities":       lipgloss.Color("6"),   // teal
	"Home/Garden":     lipgloss.Color("10"),  // bright green
	"Health":          lipgloss.Color("1"),   // red
	"Travel":          lipgloss.Color("13"),  // bright magenta
	"Kids":            lipgloss.Color("11"),  // bright cyan
	"Pets":            lipgloss.Color("208"), // orange
	"Insurance":       lipgloss.Color("12"),  // light blue
	"Housing":         lipgloss.Color("12"),  // light blue
	"Electronics":     lipgloss.Color("14"),  // cyan
	"Recreation":      lipgloss.Color("10"),  // bright green
	"Online Shopping": lipgloss.Color("5"),   // magenta
	"Personal Care":   lipgloss.Color("13"),  // bright magenta
	"Shipping":        lipgloss.Color("12"),  // light blue
	"Other":           lipgloss.Color("8"),   // dim
}

func catColor(cat string) lipgloss.Color {
	if c, ok := categoryStyleMap[cat]; ok {
		return c
	}
	return lipgloss.Color("8")
}

func amountStyle(amount float64) lipgloss.Style {
	if amount >= 500 {
		return amountRed
	}
	if amount >= 100 {
		return amountYellow
	}
	return amountGreen
}

func hRule() string {
	return borderStyle.Render("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
}

func thinRule() string {
	return lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(
		"──────────────────────────────────────────────────────")
}

func barChart(ratio float64, width int, color lipgloss.Color) string {
	blocks := []rune(" ▏▎▍▌▋▊▉█")
	filled := ratio * float64(width)
	full := int(filled)
	frac := filled - float64(full)
	s := strings.Repeat("█", full)
	if frac > 0.125 && full < width {
		s += string(blocks[int(frac*8)])
	}
	for len([]rune(s)) < width {
		s += " "
	}
	return lipgloss.NewStyle().Foreground(color).Render(s)
}

func sparkline(values []float64, maxVal float64, colorFn func(float64) lipgloss.Color) string {
	blocks := []rune("▁▂▃▄▅▆▇█")
	var b strings.Builder
	for _, v := range values {
		idx := 0
		if maxVal > 0 {
			idx = int(v / maxVal * 7)
			if idx > 7 {
				idx = 7
			}
		}
		color := colorFn(v)
		b.WriteString(lipgloss.NewStyle().Foreground(color).Render(string(blocks[idx])))
	}
	return b.String()
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func pad(s string, width int) string {
	if len(s) >= width {
		return s[:width]
	}
	return s + strings.Repeat(" ", width-len(s))
}

func padCenter(s string, width int) string {
	if len(s) >= width {
		return s
	}
	left := (width - len(s)) / 2
	right := width - len(s) - left
	return strings.Repeat(" ", left) + s + strings.Repeat(" ", right)
}

func fmtAmount(amount float64) string {
	if amount >= 10000 {
		return fmt.Sprintf("$%,.0f", amount)
	}
	return fmt.Sprintf("$%.2f", amount)
}
