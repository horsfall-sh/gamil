package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/jmoiron/sqlx"
)

func sinceDate(months int) string {
	return time.Now().AddDate(0, -months, 0).Format("2006-01-02")
}

func parseMonths(args []string, defaultMonths int) int {
	if len(args) == 0 {
		return defaultMonths
	}
	switch args[0] {
	case "year":
		return 12
	case "all":
		return 120
	default:
		if m, err := strconv.Atoi(args[0]); err == nil {
			return m
		}
	}
	return defaultMonths
}

func main() {
	if len(os.Args) < 2 {
		db := mustDB()
		defer db.Close()
		cmdAutoDispatch(db) // smart dispatch: morning brief or hourly digest
		os.Exit(0)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	// === Inbox commands ===
	case "brief", "b":
		db := mustDB()
		defer db.Close()
		cmdBrief(db)
	case "summary", "show", "s":
		db := mustDB()
		defer db.Close()
		cmdSummary(db)
	case "urgent", "u":
		db := mustDB()
		defer db.Close()
		cmdUrgent(db)
	case "digest", "d":
		db := mustDB()
		defer db.Close()
		cmdDigest(db)
	case "read", "view":
		db := mustDB()
		defer db.Close()
		if len(args) > 0 && (args[0] == "help" || args[0] == "--help") {
			cmdReadHelp()
		} else {
			cmdRead(db, args)
		}
	case "history", "hist", "trajectory", "trend":
		cmdHistory()
	case "weekly", "week", "insight":
		cmdWeekly()
	case "deliveries", "pkgs", "packages", "track":
		db := mustDB()
		defer db.Close()
		cmdDeliveries(db)
	case "archive", "archived", "noise":
		db := mustDB()
		defer db.Close()
		cmdArchive(db)

	// === Financial overview (DB) ===
	case "overview", "o", "dashboard":
		db := mustDB()
		defer db.Close()
		cmdOverview(db)

	// === Spending analysis ===
	case "spending", "spend", "$":
		db := mustDB()
		defer db.Close()
		cmdSpending(db, parseMonths(args, 1))

	// === Amazon audit ===
	case "audit", "reconcile":
		db := mustDB()
		defer db.Close()
		cmdAudit(db, parseMonths(args, 1))

	// === Velocity & hotspots ===
	case "velocity", "vel", "v":
		db := mustDB()
		defer db.Close()
		cmdVelocity(db, parseMonths(args, 3))

	// === Impulse/behavioral ===
	case "impulse", "behavior", "feelings":
		db := mustDB()
		defer db.Close()
		cmdImpulse(db, parseMonths(args, 6))

	// === Recurring/subscriptions ===
	case "patterns", "recurring", "subs":
		db := mustDB()
		defer db.Close()
		cmdPatterns(db, parseMonths(args, 12))

	// === Value scoring ===
	case "value", "score", "garbage":
		db := mustDB()
		defer db.Close()
		cmdValue(db, parseMonths(args, 6))

	// === Period comparison ===
	case "compare", "diff", "vs":
		db := mustDB()
		defer db.Close()
		months := 0 // 0 = default (this month vs last)
		if len(args) > 0 {
			months = parseMonths(args, 0)
		}
		cmdCompare(db, months)

	// === Cash flow ===
	case "cashflow", "cash", "flow":
		db := mustDB()
		defer db.Close()
		cmdCashflow(db, parseMonths(args, 12))

	// === Data management ===
	case "ingest":
		db := mustDB()
		defer db.Close()
		path := dataDir() + "/raw_emails.json"
		if len(args) > 0 {
			path = args[0]
		}
		newTx, newOrd, newPay, newInc, err := ingestFromJSON(db, path)
		if err != nil {
			fmt.Println(errorStyle.Render("  Ingest error: " + err.Error()))
			os.Exit(1)
		}
		fmt.Printf("  %s  %d transactions, %d orders, %d payments, %d income records ingested\n",
			successStyle.Render("✓"), newTx, newOrd, newPay, newInc)

	case "recategorize", "recat":
		db := mustDB()
		defer db.Close()
		cmdRecategorize(db)

	case "reclassify":
		db := mustDB()
		defer db.Close()
		cmdReclassifyEmails(db)

	case "stats":
		db := mustDB()
		defer db.Close()
		cmdStats(db)

	case "sync", "refresh", "r", "fetch":
		db := mustDB()
		defer db.Close()
		cmdSync(db)

	case "auth", "login":
		fmt.Println()
		if err := runAuthFlow(); err != nil {
			fmt.Println(errorStyle.Render("  Auth error: " + err.Error()))
			os.Exit(1)
		}
		fmt.Printf("  %s  Gmail authorized. Run: gmail sync\n", successStyle.Render("✓"))

	case "ignore", "silence", "mute":
		db := mustDB()
		defer db.Close()
		cmdIgnore(db, args)

	case "unsub", "unsubscribe":
		db := mustDB()
		defer db.Close()
		cmdUnsub(db, args)

	case "ack", "mark-read":
		if len(args) == 0 {
			cmdAckHelp()
			os.Exit(0)
		}
		db := mustDB()
		defer db.Close()
		cmdAck(db, args[0], args[1:])

	case "help", "--help", "-h":
		showHelp()

	default:
		fmt.Println(errorStyle.Render("Unknown command: " + cmd))
		showHelp()
		os.Exit(1)
	}
}

func mustDB() *sqlx.DB {
	db, err := openDB()
	if err != nil {
		fmt.Println(errorStyle.Render("  DB error: " + err.Error()))
		os.Exit(1)
	}
	return db
}

func cmdReclassifyEmails(db *sqlx.DB) {
	var emails []EmailRow
	db.Select(&emails, `SELECT * FROM emails`)
	updated := 0
	before := make(map[string]int)
	after := make(map[string]int)
	for _, e := range emails {
		var labelList []string
		json.Unmarshal([]byte(e.Labels), &labelList)
		newCat := classifyEmail(e.Sender+" <"+e.Sender+">", e.Subject, labelList)
		// Use sender_name if available for better from matching
		from := e.Sender
		if e.SenderName != "" {
			from = e.SenderName + " <" + e.Sender + ">"
		}
		newCat = classifyEmail(from, e.Subject, labelList)
		before[e.Category]++
		after[newCat]++
		if newCat != e.Category {
			db.Exec(`UPDATE emails SET category = ? WHERE message_id = ?`, newCat, e.MessageID)
			updated++
		}
	}
	fmt.Printf("  %s  Reclassified %d of %d emails\n", successStyle.Render("✓"), updated, len(emails))
	if updated > 0 {
		order := []string{"needs_attention", "calendar", "kids", "financial", "deliveries", "security", "newsletters", "social", "government", "promotions", "other"}
		for _, cat := range order {
			b, a := before[cat], after[cat]
			if b != a {
				fmt.Printf("    %-18s %d → %d\n", cat, b, a)
			}
		}
	}
	fmt.Println()
}

func cmdRecategorize(db *sqlx.DB) {
	var txs []TxRow
	db.Select(&txs, "SELECT * FROM transactions")
	updated := 0
	for _, tx := range txs {
		newMerchant := normalizeMerchant(tx.Merchant)
		newCat := categorize(newMerchant)
		if newCat != tx.Category || newMerchant != tx.Merchant {
			db.Exec("UPDATE transactions SET merchant = ?, category = ? WHERE id = ?", newMerchant, newCat, tx.ID)
			updated++
		}
	}
	fmt.Printf("  %s  %d transactions updated (merchant aliases + categories, of %d total)\n",
		successStyle.Render("✓"), updated, len(txs))
}

func cmdStats(db *sqlx.DB) {
	var txCount, ordCount, payCount, incCount int
	db.Get(&txCount, "SELECT COUNT(*) FROM transactions")
	db.Get(&ordCount, "SELECT COUNT(*) FROM orders")
	db.Get(&payCount, "SELECT COUNT(*) FROM payments")
	db.Get(&incCount, "SELECT COUNT(*) FROM income")

	var oldest, newest string
	db.Get(&oldest, "SELECT COALESCE(MIN(date), 'none') FROM transactions")
	db.Get(&newest, "SELECT COALESCE(MAX(date), 'none') FROM transactions")

	var catCount int
	db.Get(&catCount, "SELECT COUNT(DISTINCT category) FROM transactions")

	fmt.Println()
	fmt.Println(titleStyle.Render("Database Stats"))
	fmt.Println("  " + thinRule())
	fmt.Printf("  Transactions: %s\n", lipgloss.NewStyle().Bold(true).Render(fmt.Sprintf("%d", txCount)))
	fmt.Printf("  Orders:       %s\n", lipgloss.NewStyle().Bold(true).Render(fmt.Sprintf("%d", ordCount)))
	fmt.Printf("  Payments:     %s\n", lipgloss.NewStyle().Bold(true).Render(fmt.Sprintf("%d", payCount)))
	fmt.Printf("  Income:       %s\n", lipgloss.NewStyle().Bold(true).Render(fmt.Sprintf("%d", incCount)))
	fmt.Printf("  Categories:   %s\n", lipgloss.NewStyle().Bold(true).Render(fmt.Sprintf("%d", catCount)))
	if len(oldest) >= 10 && len(newest) >= 10 {
		fmt.Printf("  Date range:   %s → %s\n", dimStyle.Render(oldest[:10]), dimStyle.Render(newest[:10]))
	}
	fmt.Printf("  DB file:      %s\n", dimStyle.Render(dbPath()))
	fmt.Println()
}

func showHelp() {
	fmt.Printf(`
%s — personal finance from your inbox

%s
  gmail                     Hourly digest (morning brief after 8am)
  gmail digest              Force hourly digest view
  gmail brief               Morning brief (urgent + today + spending)
  gmail summary             Full inbox summary
  gmail read <N>            Read email(s) — n/N to navigate, q to quit
  gmail urgent              Emails needing your reply
  gmail weekly              Claude-generated weekly insight
  gmail deliveries          Package tracking (in-transit + recent)
  gmail archive             Auto-archive transparency report
  gmail history             Unread count trajectory

%s
  gmail overview            Full financial dashboard
  gmail spending [N|year]   Category breakdown & trends
  gmail velocity [N|year]   Spending rate & hotspots
  gmail impulse [N|year]    Behavioral/emotional analysis
  gmail patterns [N|year]   Recurring charges & subscriptions
  gmail value [N|year]      Smart spending scorer (find garbage)
  gmail cashflow [N|year]   Income vs outflow
  gmail compare [N|year]    This period vs prior period
  gmail audit [N|year]      Amazon ↔ Wells Fargo reconciliation

%s
  gmail sync                Fetch new emails from Gmail → DB
  gmail ack <group>         Mark email group as read
  gmail unsub [#|sender]    Manage unsubscribe list
  gmail ignore [#|sender]   Auto-silence senders (no unsub link)
  gmail auth                Authorize Gmail access (first-time)
  gmail ingest [file]       Import data from JSON file
  gmail stats               Database stats
  gmail help                This help

  Ack groups: financial, promotions, newsletters, social, government, amazon, shipping, calendar, all
  By sender:  gmail ack financial:privacy  •  gmail ack newsletters:substack
  Custom ack: gmail ack "from:noreply@github.com"

  Time ranges: number = months (e.g. 6), "year" = 12mo, "all" = everything
`,
		titleStyle.Render("gmail"),
		sectionStyle.Foreground(lipgloss.Color("15")).Render("Inbox:"),
		sectionStyle.Foreground(lipgloss.Color("15")).Render("Finance:"),
		sectionStyle.Foreground(lipgloss.Color("15")).Render("Data:"))
}
