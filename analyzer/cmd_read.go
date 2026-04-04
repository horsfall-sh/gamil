package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/jmoiron/sqlx"
	"golang.org/x/term"
)

func cmdRead(db *sqlx.DB, args []string) {
	if len(args) == 0 {
		fmt.Println(errorStyle.Render("  Usage: gmail read <number(s)>"))
		fmt.Println(dimStyle.Render("  Examples: gmail read 1  ·  gmail read 1-3  ·  gmail read 1 65 42"))
		return
	}

	// Collect and parse ref numbers (reuse the flexible number parser from ack)
	combined := collectNumericArgs(args[0], args[1:])
	refNums := parseNumericRefs(combined)

	if len(refNums) == 0 {
		fmt.Println(errorStyle.Render("  No valid ref numbers"))
		return
	}

	refs := loadEmailRefs()

	// Validate all refs first
	for _, num := range refNums {
		if num > len(refs) || num < 1 {
			fmt.Printf("  %s  Ref %d out of range (have %d) — re-run gmail summary\n",
				errorStyle.Render("!"), num, len(refs))
			return
		}
	}

	svc, err := getGmailService()
	if err != nil {
		fmt.Println(errorStyle.Render("  " + err.Error()))
		return
	}

	// Build full output with all emails, separated by page markers
	var b strings.Builder
	total := len(refNums)

	for i, num := range refNums {
		messageID := refs[num-1]

		var email EmailRow
		err := db.Get(&email, `SELECT * FROM emails WHERE message_id = ?`, messageID)
		if err != nil {
			b.WriteString(fmt.Sprintf("\n  %s  Email #%d not found in DB\n\n", errorStyle.Render("!"), num))
			continue
		}

		msg, err := svc.Users.Messages.Get("me", messageID).Format("full").Do()
		if err != nil {
			b.WriteString(fmt.Sprintf("\n  %s  Failed to fetch #%d: %s\n\n", errorStyle.Render("!"), num, err.Error()))
			continue
		}

		body := decodeMessageBody(msg)
		date := getHeader(msg, "Date")

		renderEmail(&b, email, body, date, num, i+1, total)
	}

	output := b.String()

	// Pipe through less for navigation if content is long or multiple emails
	termHeight := 40
	if h, err := terminalHeight(); err == nil {
		termHeight = h
	}
	lineCount := strings.Count(output, "\n")

	if (lineCount > termHeight-2 || total > 1) && isTerminal() {
		pipeToLess(output)
	} else {
		fmt.Print(output)
	}
}

func renderEmail(b *strings.Builder, email EmailRow, body, date string, refNum, pos, total int) {
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("14"))
	labelStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("8"))

	b.WriteString("\n")

	// Position indicator for multi-email reading
	if total > 1 {
		posLabel := fmt.Sprintf("  [%d/%d] ref #%d", pos, total, refNum)
		b.WriteString(dimStyle.Render(posLabel) + "\n")
	}

	b.WriteString(hRule() + "\n")
	b.WriteString(labelStyle.Render("  From:    ") + formatSender(email.SenderName, email.Sender) + "\n")
	b.WriteString(labelStyle.Render("  Date:    ") + date + "\n")
	b.WriteString(labelStyle.Render("  Subject: ") + headerStyle.Render(email.Subject) + "\n")
	b.WriteString(hRule() + "\n")
	b.WriteString("\n")

	if body == "" {
		b.WriteString(dimStyle.Render("  (no text body — email may be HTML-only)") + "\n")
	} else {
		body = cleanBody(body)
		for _, line := range strings.Split(body, "\n") {
			b.WriteString("  " + line + "\n")
		}
	}

	b.WriteString("\n")
	ackHint := fmt.Sprintf("  gmail ack %d", refNum)
	b.WriteString(dimStyle.Render(ackHint) + "\n")

	// Separator between emails
	if total > 1 {
		b.WriteString("\n")
		b.WriteString(dimStyle.Render("  ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─") + "\n")
	}
}

func formatSender(name, email string) string {
	if name == "" || name == email {
		return email
	}
	return name + " <" + email + ">"
}

func cleanBody(body string) string {
	for strings.Contains(body, "\n\n\n") {
		body = strings.ReplaceAll(body, "\n\n\n", "\n\n")
	}
	body = strings.TrimSpace(body)
	return body
}

func terminalHeight() (int, error) {
	_, h, err := term.GetSize(int(os.Stdout.Fd()))
	return h, err
}

func isTerminal() bool {
	return term.IsTerminal(int(os.Stdout.Fd()))
}

func pipeToLess(content string) {
	// -R: interpret ANSI colors
	// -F: quit if content fits on screen
	// -X: don't clear screen on exit
	// --pattern: jump between emails with n/N (vim-style)
	cmd := exec.Command("less", "-R", "-F", "-X", "--pattern=^━━")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	stdin, err := cmd.StdinPipe()
	if err != nil {
		fmt.Print(content)
		return
	}

	if err := cmd.Start(); err != nil {
		fmt.Print(content)
		return
	}

	io.WriteString(stdin, content)
	stdin.Close()
	cmd.Wait()
}

// parseRefArgs strips flags from args and returns the numeric portion
func parseRefArgs(args []string) []string {
	var clean []string
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			clean = append(clean, a)
		}
	}
	return clean
}

// intInSlice checks if n is in nums (for dedup tracking)
func intInSlice(n int, nums []int) bool {
	for _, v := range nums {
		if v == n {
			return true
		}
	}
	return false
}

func cmdReadHelp() {
	fmt.Println()
	fmt.Println(titleStyle.Render("gmail read — view email content"))
	fmt.Println()
	fmt.Println("  " + lipgloss.NewStyle().Bold(true).Render("Usage:"))
	fmt.Println("    gmail read 1              Read email #1")
	fmt.Println("    gmail read 1-3            Read emails #1 through #3")
	fmt.Println("    gmail read 1 65 42        Read specific emails")
	fmt.Println("    gmail read 1,3,5          Comma-separated")
	fmt.Println()
	fmt.Println("  " + lipgloss.NewStyle().Bold(true).Render("Navigation (in less):"))
	fmt.Println("    n                         Next email")
	fmt.Println("    N                         Previous email")
	fmt.Println("    q                         Quit")
	fmt.Println("    /pattern                  Search within emails")
	fmt.Println("    j/k                       Scroll down/up")
	fmt.Println("    space                     Page down")
	fmt.Println()
	fmt.Println(dimStyle.Render("  Numbers refer to refs from last summary/urgent/digest"))
	fmt.Println()
}

func intToStr(n int) string {
	return strconv.Itoa(n)
}
