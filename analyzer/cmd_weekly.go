package main

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

var (
	weeklyBoldRe = regexp.MustCompile(`\*\*(.+?)\*\*`)
	weeklyH2Re   = regexp.MustCompile(`^## (.+)`)
	weeklyH3Re   = regexp.MustCompile(`^### (.+)`)
	weeklyBullet = regexp.MustCompile(`^[-•] (.+)`)
)

func cmdWeekly() {
	text, err := loadWeeklyInsight()
	if err != nil {
		fmt.Println()
		fmt.Println(hRule())
		fmt.Println(titleStyle.Render("Weekly Insight"))
		fmt.Println(hRule())
		fmt.Println()
		fmt.Println(dimStyle.Render("  " + err.Error()))
		fmt.Println()
		fmt.Println(dimStyle.Render("  The weekly insight task runs every Sunday ~9:15am."))
		fmt.Println(dimStyle.Render("  To trigger manually: gmail refresh-weekly"))
		fmt.Println()
		return
	}

	for _, line := range strings.Split(text, "\n") {
		switch {
		case strings.HasPrefix(line, "# "):
			fmt.Println()
			fmt.Println(hRule())
			fmt.Println(titleStyle.Render(line[2:]))
			fmt.Println(hRule())

		case weeklyH2Re.MatchString(line):
			m := weeklyH2Re.FindStringSubmatch(line)
			color := weeklyH2Color(m[1])
			style := lipgloss.NewStyle().Bold(true).Foreground(color).PaddingLeft(1)
			rule := lipgloss.NewStyle().Foreground(color).Render("  ──────────────────────────────────────────────")
			fmt.Println()
			fmt.Println(style.Render("▌ " + m[1]))
			fmt.Println(rule)

		case weeklyH3Re.MatchString(line):
			m := weeklyH3Re.FindStringSubmatch(line)
			fmt.Println()
			fmt.Println(subtitleStyle.Render("  " + m[1]))

		case weeklyBullet.MatchString(line):
			m := weeklyBullet.FindStringSubmatch(line)
			content := renderWeeklyInline(m[1])
			fmt.Println("    • " + content)

		case strings.HasPrefix(line, "---"):
			// skip

		case strings.TrimSpace(line) == "":
			// skip blank lines inside sections

		default:
			if strings.TrimSpace(line) != "" {
				fmt.Println("  " + renderWeeklyInline(line))
			}
		}
	}
	fmt.Println()

	// Footer: when this was generated
	now := time.Now()
	fmt.Println(dimStyle.Render(fmt.Sprintf("  Last generated: check weekly-insight.md • Next run: Sunday %s ~9:15am",
		nextSunday(now).Format("Jan 2"))))
	fmt.Println()
}

func renderWeeklyInline(s string) string {
	return weeklyBoldRe.ReplaceAllStringFunc(s, func(m string) string {
		inner := weeklyBoldRe.FindStringSubmatch(m)[1]
		return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15")).Render(inner)
	})
}

func weeklyH2Color(heading string) lipgloss.Color {
	lower := strings.ToLower(heading)
	switch {
	case strings.Contains(lower, "spend") || strings.Contains(lower, "financ"):
		return lipgloss.Color("3") // yellow
	case strings.Contains(lower, "subscri") || strings.Contains(lower, "recurring"):
		return lipgloss.Color("14") // cyan
	case strings.Contains(lower, "security") || strings.Contains(lower, "alert"):
		return lipgloss.Color("1") // red
	case strings.Contains(lower, "amazon"):
		return lipgloss.Color("3") // yellow
	case strings.Contains(lower, "savings") || strings.Contains(lower, "tip"):
		return lipgloss.Color("2") // green
	case strings.Contains(lower, "upcoming") || strings.Contains(lower, "ahead"):
		return lipgloss.Color("14") // cyan
	default:
		return lipgloss.Color("15")
	}
}

func nextSunday(t time.Time) time.Time {
	daysUntil := (7 - int(t.Weekday())) % 7
	if daysUntil == 0 {
		daysUntil = 7
	}
	return t.AddDate(0, 0, daysUntil)
}
