package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/jmoiron/sqlx"
	"google.golang.org/api/gmail/v1"
)

// DB category names (ack uses the local DB for these so it matches the summary)
var dbCategories = map[string]bool{
	"financial": true, "promotions": true, "newsletters": true,
	"social": true, "government": true, "calendar": true,
	"security": true, "kids": true, "deliveries": true,
	"needs_attention": true, "other": true,
}

// Gmail query groups for things that don't map 1:1 to a DB category
var ackQueryGroups = map[string]string{
	"amazon":   `from:amazon.com is:unread`,
	"shipping": `from:(shipment-tracking@amazon.com OR order-update@amazon.com OR "usps.com" OR "fedex.com" OR "ups.com") is:unread`,
}

func cmdAck(db *sqlx.DB, group string, args []string) {
	dryRun := false
	force := false
	for _, a := range args {
		switch a {
		case "--dry-run", "-n":
			dryRun = true
		case "--force", "-f":
			force = true
		}
	}

	group = strings.ToLower(group)

	// Numeric ref (e.g. "1", "1,3,5", "1-10", or space-separated: "1" "3" "5")
	if isNumericRef(group) {
		combined := collectNumericArgs(group, args)
		cmdAckByRef(db, combined, dryRun)
		return
	}

	// from: shorthand
	if strings.HasPrefix(group, "from:") {
		cmdAckByFrom(db, group, dryRun)
		return
	}

	// "all" — everything unread in the DB
	if group == "all" {
		if !force && !dryRun {
			fmt.Println(errorStyle.Render("  gmail ack all requires --force (marks ALL unread as read)"))
			fmt.Println(dimStyle.Render("  Use --dry-run to preview first"))
			return
		}
		cmdAckByDB(db, "all", dryRun)
		return
	}

	// category:sender — e.g. "financial:privacy" or "newsletters:substack"
	if idx := strings.Index(group, ":"); idx > 0 {
		cat := group[:idx]
		senderFrag := group[idx+1:]
		if dbCategories[cat] && senderFrag != "" {
			cmdAckByCategoryAndSender(db, cat, senderFrag, dryRun)
			return
		}
	}

	// DB category (financial, promotions, social, etc.)
	if dbCategories[group] {
		cmdAckByDB(db, group, dryRun)
		return
	}

	// Named Gmail query group (amazon, shipping)
	if query, ok := ackQueryGroups[group]; ok {
		cmdAckByQuery(db, group, query, dryRun)
		return
	}

	// Custom Gmail query
	query := group
	if !strings.Contains(group, "is:unread") {
		query = group + " is:unread"
	}
	cmdAckByQuery(db, group, query, dryRun)
}

// ── Ack by DB category ──────────────────────────────────────────────────────

func cmdAckByDB(db *sqlx.DB, category string, dryRun bool) {
	var emails []EmailRow
	var err error
	if category == "all" {
		emails, err = getUnreadEmails(db)
	} else {
		emails, err = getEmailsByCategory(db, category)
	}
	if err != nil || len(emails) == 0 {
		fmt.Printf("  %s  No unread %s emails\n", successStyle.Render("✓"), category)
		return
	}

	// Preview
	fmt.Printf("\n  %s  %d unread %s emails\n", sectionStyle.Render("⬚"), len(emails), category)
	max := 5
	if len(emails) < max {
		max = len(emails)
	}
	for _, e := range emails[:max] {
		name := e.SenderName
		if name == "" {
			name = e.Sender
		}
		fmt.Printf("    • %s — %s\n", dimStyle.Render(truncate(name, 30)), truncate(e.Subject, 50))
	}
	if len(emails) > max {
		fmt.Printf("    %s\n", dimStyle.Render(fmt.Sprintf("... and %d more", len(emails)-max)))
	}
	fmt.Println()

	if dryRun {
		fmt.Println(dimStyle.Render("  (dry run — no changes made)"))
		return
	}
	if len(emails) > 50 && !confirmAck(len(emails)) {
		return
	}

	var ids []string
	for _, e := range emails {
		ids = append(ids, e.MessageID)
	}

	svc, err := getGmailService()
	if err != nil {
		fmt.Println(errorStyle.Render("  " + err.Error()))
		return
	}

	batchMarkRead(svc, db, ids)
	fmt.Printf("  %s  Marked %d %s emails as read\n", successStyle.Render("✓"), len(ids), category)
}

// ── Ack by category + sender ────────────────────────────────────────────────

func cmdAckByCategoryAndSender(db *sqlx.DB, category, senderFrag string, dryRun bool) {
	allEmails, err := getEmailsByCategory(db, category)
	if err != nil || len(allEmails) == 0 {
		fmt.Printf("  %s  No unread %s emails\n", successStyle.Render("✓"), category)
		return
	}

	frag := strings.ToLower(senderFrag)
	var matched []EmailRow
	for _, e := range allEmails {
		if strings.Contains(strings.ToLower(e.Sender), frag) ||
			strings.Contains(strings.ToLower(e.SenderName), frag) {
			matched = append(matched, e)
		}
	}

	if len(matched) == 0 {
		fmt.Printf("  %s  No unread %s emails matching '%s'\n",
			successStyle.Render("✓"), category, senderFrag)
		return
	}

	// Preview
	fmt.Printf("\n  %s  %d unread %s emails from '%s'\n",
		sectionStyle.Render("⬚"), len(matched), category, senderFrag)
	max := 5
	if len(matched) < max {
		max = len(matched)
	}
	for _, e := range matched[:max] {
		name := e.SenderName
		if name == "" {
			name = e.Sender
		}
		fmt.Printf("    • %s — %s\n", dimStyle.Render(truncate(name, 30)), truncate(e.Subject, 50))
	}
	if len(matched) > max {
		fmt.Printf("    %s\n", dimStyle.Render(fmt.Sprintf("... and %d more", len(matched)-max)))
	}
	fmt.Println()

	if dryRun {
		fmt.Println(dimStyle.Render("  (dry run — no changes made)"))
		return
	}

	var ids []string
	for _, e := range matched {
		ids = append(ids, e.MessageID)
	}

	svc, err := getGmailService()
	if err != nil {
		fmt.Println(errorStyle.Render("  " + err.Error()))
		return
	}

	batchMarkRead(svc, db, ids)
	fmt.Printf("  %s  Marked %d %s emails from '%s' as read\n",
		successStyle.Render("✓"), len(ids), category, senderFrag)
}

// ── Ack by numbered ref ─────────────────────────────────────────────────────

func cmdAckByRef(db *sqlx.DB, refStr string, dryRun bool) {
	refs := loadEmailRefs()
	if len(refs) == 0 {
		fmt.Println(errorStyle.Render("  No email refs — run gmail summary or gmail urgent first"))
		return
	}

	nums := parseNumericRefs(refStr)
	var ids []string
	var previews []string

	for _, n := range nums {
		if n < 1 || n > len(refs) {
			fmt.Printf("  %s  #%d out of range (1-%d)\n", warnStyle.Render("⚠"), n, len(refs))
			continue
		}
		msgID := refs[n-1]
		ids = append(ids, msgID)

		var e EmailRow
		err := db.Get(&e, `SELECT * FROM emails WHERE message_id = ?`, msgID)
		if err == nil {
			name := e.SenderName
			if name == "" {
				name = e.Sender
			}
			previews = append(previews, fmt.Sprintf("#%d %s — %s", n, name, truncate(e.Subject, 50)))
		} else {
			previews = append(previews, fmt.Sprintf("#%d %s", n, msgID[:12]))
		}
	}

	if len(ids) == 0 {
		return
	}

	fmt.Println()
	for _, p := range previews {
		fmt.Printf("    %s %s\n", dimStyle.Render("→"), p)
	}
	fmt.Println()

	if dryRun {
		fmt.Println(dimStyle.Render("  (dry run — no changes made)"))
		return
	}

	svc, err := getGmailService()
	if err != nil {
		fmt.Println(errorStyle.Render("  " + err.Error()))
		return
	}

	batchMarkRead(svc, db, ids)
	fmt.Printf("  %s  Marked %d email(s) as read\n", successStyle.Render("✓"), len(ids))
}

// ── Ack by sender ───────────────────────────────────────────────────────────

func cmdAckByFrom(db *sqlx.DB, fromArg string, dryRun bool) {
	senders := strings.TrimPrefix(fromArg, "from:")
	parts := strings.Split(senders, ",")
	var orParts []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			orParts = append(orParts, "from:"+p)
		}
	}
	if len(orParts) == 0 {
		fmt.Println(errorStyle.Render("  No senders specified"))
		return
	}

	query := "(" + strings.Join(orParts, " OR ") + ") is:unread"

	svc, err := getGmailService()
	if err != nil {
		fmt.Println(errorStyle.Render("  " + err.Error()))
		return
	}

	allIDs, samples := collectAckTargets(svc, query, 5)
	if len(allIDs) == 0 {
		fmt.Printf("  %s  No unread emails from %s\n", successStyle.Render("✓"), senders)
		return
	}

	printAckPreview(fromArg, allIDs, samples)
	if dryRun {
		fmt.Println(dimStyle.Render("  (dry run — no changes made)"))
		return
	}
	if len(allIDs) > 50 && !confirmAck(len(allIDs)) {
		return
	}

	batchMarkRead(svc, db, allIDs)
	fmt.Printf("  %s  Marked %d emails from %s as read\n", successStyle.Render("✓"), len(allIDs), senders)
}

// ── Ack by Gmail query ──────────────────────────────────────────────────────

func cmdAckByQuery(db *sqlx.DB, label, query string, dryRun bool) {
	svc, err := getGmailService()
	if err != nil {
		fmt.Println(errorStyle.Render("  " + err.Error()))
		return
	}

	allIDs, samples := collectAckTargets(svc, query, 5)
	if len(allIDs) == 0 {
		fmt.Printf("  %s  No unread emails matching '%s'\n", successStyle.Render("✓"), label)
		return
	}

	printAckPreview(label, allIDs, samples)
	if dryRun {
		fmt.Println(dimStyle.Render("  (dry run — no changes made)"))
		return
	}
	if len(allIDs) > 50 && !confirmAck(len(allIDs)) {
		return
	}

	batchMarkRead(svc, db, allIDs)
	fmt.Printf("  %s  Marked %d %s emails as read\n", successStyle.Render("✓"), len(allIDs), label)
}

// ── Shared helpers ──────────────────────────────────────────────────────────

func isNumericRef(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c != ',' && c != '-' && c != ' ' && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}

// collectNumericArgs merges the first arg with any subsequent numeric args
// so "1" ["3" "5" "--dry-run"] → "1,3,5"
func collectNumericArgs(first string, rest []string) string {
	parts := []string{first}
	for _, a := range rest {
		if isNumericRef(a) {
			parts = append(parts, a)
		}
	}
	return strings.Join(parts, ",")
}

func parseNumericRefs(s string) []int {
	// Normalize: spaces and commas both work as separators
	s = strings.ReplaceAll(s, " ", ",")
	var nums []int
	seen := make(map[int]bool)
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.Contains(part, "-") {
			bounds := strings.SplitN(part, "-", 2)
			lo, _ := strconv.Atoi(bounds[0])
			hi, _ := strconv.Atoi(bounds[1])
			for i := lo; i <= hi; i++ {
				if !seen[i] {
					nums = append(nums, i)
					seen[i] = true
				}
			}
		} else {
			n, _ := strconv.Atoi(part)
			if !seen[n] {
				nums = append(nums, n)
				seen[n] = true
			}
		}
	}
	return nums
}

func collectAckTargets(svc *gmail.Service, query string, maxSamples int) (allIDs []string, samples []string) {
	call := svc.Users.Messages.List("me").Q(query).MaxResults(500)
	for {
		resp, err := call.Do()
		if err != nil {
			fmt.Println(errorStyle.Render("  Search error: " + err.Error()))
			return
		}
		for _, m := range resp.Messages {
			allIDs = append(allIDs, m.Id)
			if len(samples) < maxSamples {
				msg, err := svc.Users.Messages.Get("me", m.Id).Format("metadata").MetadataHeaders("Subject", "From").Do()
				if err == nil {
					subj, from := "", ""
					for _, h := range msg.Payload.Headers {
						switch h.Name {
						case "Subject":
							subj = h.Value
						case "From":
							from = h.Value
						}
					}
					samples = append(samples, fmt.Sprintf("%s — %s",
						dimStyle.Render(truncate(from, 30)),
						truncate(subj, 50)))
				}
			}
		}
		if resp.NextPageToken == "" {
			break
		}
		call = svc.Users.Messages.List("me").Q(query).MaxResults(500).PageToken(resp.NextPageToken)
	}
	return
}

func printAckPreview(label string, allIDs []string, samples []string) {
	fmt.Printf("\n  %s  %d unread emails (%s)\n",
		sectionStyle.Render("⬚"), len(allIDs), label)
	for _, s := range samples {
		fmt.Printf("    • %s\n", s)
	}
	if len(allIDs) > len(samples) {
		fmt.Printf("    %s\n", dimStyle.Render(fmt.Sprintf("... and %d more", len(allIDs)-len(samples))))
	}
	fmt.Println()
}

func confirmAck(count int) bool {
	fmt.Printf("  Mark %d emails as read? [y/N] ", count)
	reader := bufio.NewReader(os.Stdin)
	answer, _ := reader.ReadString('\n')
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(answer)), "y") {
		fmt.Println(dimStyle.Render("  Cancelled"))
		return false
	}
	return true
}

// ackAllFromSender marks all unread emails from a sender as read (Gmail + local DB)
func ackAllFromSender(db *sqlx.DB, senderEmail string) int {
	var emails []EmailRow
	db.Select(&emails, `SELECT * FROM emails WHERE sender = ? AND is_unread = 1`, senderEmail)
	if len(emails) == 0 {
		return 0
	}

	var ids []string
	for _, e := range emails {
		ids = append(ids, e.MessageID)
	}

	svc, err := getGmailService()
	if err != nil {
		// At least update local DB
		for _, id := range ids {
			db.Exec(`UPDATE emails SET is_unread = 0 WHERE message_id = ?`, id)
		}
		return len(ids)
	}

	batchMarkRead(svc, db, ids)
	return len(ids)
}

func batchMarkRead(svc *gmail.Service, db *sqlx.DB, ids []string) {
	for i := 0; i < len(ids); i += 1000 {
		end := i + 1000
		if end > len(ids) {
			end = len(ids)
		}
		chunk := ids[i:end]
		req := &gmail.BatchModifyMessagesRequest{
			Ids:            chunk,
			RemoveLabelIds: []string{"UNREAD"},
		}
		if err := svc.Users.Messages.BatchModify("me", req).Do(); err != nil {
			fmt.Println(errorStyle.Render(fmt.Sprintf("  Batch modify failed (chunk %d-%d): %s", i, end, err.Error())))
			return
		}
	}

	// Update local DB
	for _, id := range ids {
		db.Exec(`UPDATE emails SET is_unread = 0 WHERE message_id = ?`, id)
	}
}

func cmdAckHelp() {
	fmt.Println()
	fmt.Println(titleStyle.Render("gmail ack — mark emails as read"))
	fmt.Println()
	fmt.Println(sectionStyle.Render("  By number (from summary/urgent):"))
	fmt.Printf("    gmail ack 1           single email\n")
	fmt.Printf("    gmail ack 1,3,5       several emails\n")
	fmt.Printf("    gmail ack 1-10        range of emails\n")
	fmt.Println()
	fmt.Println(sectionStyle.Render("  By sender:"))
	fmt.Printf("    gmail ack from:amazon.com\n")
	fmt.Printf("    gmail ack from:wellsfargo.com,amazon.com\n")
	fmt.Println()
	fmt.Println(sectionStyle.Render("  By category (matches summary view):"))
	cats := []string{"financial", "promotions", "newsletters", "social",
		"government", "calendar", "security", "kids", "deliveries", "other"}
	for _, c := range cats {
		fmt.Printf("    gmail ack %-16s\n", c)
	}
	fmt.Printf("    gmail ack all --force    (everything)\n")
	fmt.Println()
	fmt.Println(sectionStyle.Render("  By Gmail query:"))
	fmt.Printf("    gmail ack amazon         all from amazon.com\n")
	fmt.Printf("    gmail ack shipping        shipping notifications\n")
	fmt.Printf("    gmail ack \"subject:meeting\"\n")
	fmt.Println()
	fmt.Println(sectionStyle.Render("  Flags:"))
	fmt.Printf("    --dry-run, -n   Preview without marking\n")
	fmt.Printf("    --force, -f     Required for 'all'\n")
	fmt.Println()
}
