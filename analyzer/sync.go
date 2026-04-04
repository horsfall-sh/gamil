package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jmoiron/sqlx"
	"google.golang.org/api/gmail/v1"
)

// Regex patterns for email parsing
var (
	// Wells Fargo purchase alerts
	wfAmountRe   = regexp.MustCompile(`(?i)(?:Amount|purchase of)\s*\$\s*([\d,]+\.\d{2})`)
	wfMerchantRe = regexp.MustCompile(`(?i)Merchant\s+detail\s+(.+?)(?:\s+in\s+(.+))?$`)
	wfCardRe     = regexp.MustCompile(`(?i)(?:credit card|card)\s+\.{3}(\d{4})`)

	// Amazon orders
	amzOrderNumRe = regexp.MustCompile(`Order\s*#\s*([\d-]+)`)
	amzTotalRe    = regexp.MustCompile(`(?i)(?:Grand Total|Order Total|Total)[:\s]*\$?([\d,]+\.\d{2})`)
	amzItemRe     = regexp.MustCompile(`(?m)^\s*\d+\s+(.+?)\s+\$[\d,]+\.\d{2}`)

	// BofA vehicle loan
	bofaAmountRe = regexp.MustCompile(`(?i)(?:Amount|payment)[:\s]*\$\s*([\d,]+\.\d{2})`)

	// Schwab RSU
	schwabAmountRe = regexp.MustCompile(`(?i)(?:net shares|total value|amount)[:\s]*\$?([\d,]+\.\d{2})`)
)

type parsedEmail struct {
	messageID string
	threadID  string
	date      string
	from      string
	subject   string
	body      string
	labelIDs  []string
}

func decodeMessageBody(msg *gmail.Message) string {
	if msg.Payload == nil {
		return ""
	}
	// Try to find text/plain part
	body := findPart(msg.Payload, "text/plain")
	if body == "" {
		body = findPart(msg.Payload, "text/html")
	}
	return body
}

func findPart(part *gmail.MessagePart, mimeType string) string {
	if part.MimeType == mimeType && part.Body != nil && part.Body.Data != "" {
		decoded, err := base64.URLEncoding.DecodeString(part.Body.Data)
		if err != nil {
			// Try with padding
			padded := part.Body.Data
			switch len(padded) % 4 {
			case 2:
				padded += "=="
			case 3:
				padded += "="
			}
			decoded, err = base64.URLEncoding.DecodeString(padded)
			if err != nil {
				return ""
			}
		}
		return string(decoded)
	}
	for _, sub := range part.Parts {
		if result := findPart(sub, mimeType); result != "" {
			return result
		}
	}
	return ""
}

func getHeader(msg *gmail.Message, name string) string {
	if msg.Payload == nil {
		return ""
	}
	for _, h := range msg.Payload.Headers {
		if strings.EqualFold(h.Name, name) {
			return h.Value
		}
	}
	return ""
}

func parseEmailDate(msg *gmail.Message) string {
	// Use internal date (epoch ms) for reliability
	if msg.InternalDate > 0 {
		t := time.Unix(msg.InternalDate/1000, 0)
		return t.Format("2006-01-02")
	}
	return time.Now().Format("2006-01-02")
}

func parseAmount(s string) float64 {
	s = strings.ReplaceAll(s, ",", "")
	f, _ := strconv.ParseFloat(s, 64)
	return f
}

// extractSenderName pulls the display name from "Name <email>" format
func extractSenderName(from string) string {
	if idx := strings.Index(from, "<"); idx > 0 {
		name := strings.TrimSpace(from[:idx])
		name = strings.Trim(name, "\"")
		return name
	}
	return from
}

// extractSenderEmail pulls the email from "Name <email>" format
func extractSenderEmail(from string) string {
	if start := strings.Index(from, "<"); start >= 0 {
		if end := strings.Index(from, ">"); end > start {
			return strings.ToLower(from[start+1 : end])
		}
	}
	return strings.ToLower(from)
}

// hasLabel checks if a label is present in the list
func hasLabel(labels []string, label string) bool {
	for _, l := range labels {
		if l == label {
			return true
		}
	}
	return false
}

// isAutomatedSender returns true if the email address looks automated
func isAutomatedSender(email string) bool {
	automated := []string{
		"noreply", "no-reply", "donotreply", "do-not-reply",
		"@notify.", "@email.", "@mail.", "@alerts.", "@info.",
		"@marketing.", "@news.", "@updates.", "@promo.",
		"auto-confirm@", "auto-reply@", "mailer-daemon",
		"calendar-notification@", "notification@",
	}
	lower := strings.ToLower(email)
	for _, pattern := range automated {
		if strings.Contains(lower, pattern) {
			return true
		}
	}
	return false
}

// classifyEmail determines the inbox category for an email (first match wins)
func classifyEmail(from, subject string, labels []string) string {
	email := extractSenderEmail(from)
	subjectLower := strings.ToLower(subject)
	fromLower := strings.ToLower(from)

	// Calendar
	if strings.Contains(email, "calendar-notification@google.com") ||
		strings.Contains(email, "calendar-server@google.com") {
		return "calendar"
	}
	if hasLabel(labels, "CATEGORY_PERSONAL") && containsAny(subjectLower, "invitation:", "rsvp", "event:", "you're invited") {
		return "calendar"
	}

	// Kids / School — senders and subjects
	if containsAny(email, "familysafety@microsoft.com", "family-noreply@microsoft.com") ||
		containsAny(fromLower, "family safety") {
		return "kids"
	}
	if containsAny(subjectLower,
		"steam school", "roblox", "crunchlabs", "crunch labs",
		"classroom", "school", "student", "parent portal",
		"screen time", "activity report") ||
		containsAny(fromLower,
			"brightwheel", "seesaw", "classdojo", "remind.com") {
		return "kids"
	}
	// Edward's activity (child-related Google notifications)
	if containsAny(subjectLower, "edward") && containsAny(subjectLower, "activity", "review") {
		return "kids"
	}

	// Security
	if containsAny(subjectLower,
		"security alert", "password reset", "reset password",
		"sign-in", "signin", "new sign in", "new login",
		"verification code", "verify your", "confirm your email",
		"2fa", "two-factor", "suspicious activity", "unusual activity",
		"was compromised", "unauthorized access",
		"password change", "password was changed") {
		return "security"
	}

	// Financial — senders
	financialSenders := []string{
		"wellsfargo.com", "bankofamerica.com", "citi.com",
		"schwab.com", "paypal.com", "venmo.com", "zelle",
		"ezpassva.com", "ezpass", "linode.com", "akamai.com",
		"xfinity.com", "comcast.com", "privacy.com", "proxyvote.com",
		"mint.com", "turbotax", "irs.gov", "creditkarma.com",
		"auto-confirm@amazon.com",
		"playstation.com", "play.google.com",
		"alpacamarkets.com", "alpaca.markets",
		"rocketmortgage.com",
		"apple.com",
		"openai.com",
		"dropbox.com",
		"googlewallet", "googlecommerce",
	}
	for _, s := range financialSenders {
		if strings.Contains(email, s) {
			return "financial"
		}
	}
	// Financial — subjects
	if containsAny(subjectLower,
		"payment", "invoice", "statement", "balance",
		"credit card", "debit card", "direct deposit",
		"autopay", "auto-pay", "bill is ready", "amount due",
		"transaction was successful", "wire transfer",
		"subscription renew", "will not renew",
		"was authorized", "was declined", "charge at",
		"receipt") {
		return "financial"
	}

	// Deliveries — senders
	deliverySenders := []string{
		"usps.com", "fedex.com", "ups.com", "ontrac.com", "dhl.com",
		"lasership.com",
		"shipment-tracking@amazon.com", "order-update@amazon.com",
		"chewy.com",
	}
	for _, s := range deliverySenders {
		if strings.Contains(email, s) {
			return "deliveries"
		}
	}
	// Deliveries — subjects (works for Walmart, eBay, any sender)
	if containsAny(subjectLower,
		"shipped", "delivered", "out for delivery",
		"tracking number", "your package", "your order has shipped",
		"expected delivery", "delivery scheduled",
		"on its way", "almost here", "heading your way",
		"we shipped your order", "bring any fresh") {
		return "deliveries"
	}

	// Shopping / order management (Walmart, etc. — not financial, not delivery)
	if containsAny(email, "walmart.com") && containsAny(subjectLower,
		"make changes", "low in stock", "order confirmed",
		"we're on it", "items may be") {
		return "deliveries"
	}

	// Newsletters — senders
	newsletterSenders := []string{
		"newsletter@", "digest@", "news@", "weekly@",
		"noreply@medium.com", "substack.com",
		"inspirebh.com", "anthropic.com",
		"morningbrew.com", "thehustle.co", "tldrnewsletter",
		"hancock", // Hancock Farm & Seed
	}
	for _, s := range newsletterSenders {
		if strings.Contains(email, s) {
			return "newsletters"
		}
	}
	// Newsletters — subjects
	if containsAny(subjectLower,
		"newsletter", "digest", "weekly roundup", "monthly update",
		"weekly update", "daily briefing", "weekly recap",
		"this week in", "what's new in") {
		return "newsletters"
	}
	if hasLabel(labels, "CATEGORY_UPDATES") {
		return "newsletters"
	}

	// Social — senders
	socialSenders := []string{
		"discord.com", "discordapp.com",
		"slack.com", "reddit.com", "redditmail.com",
		"twitter.com", "x.com",
		"facebookmail.com", "facebook.com",
		"linkedin.com", "instagram.com",
		"nextdoor.com", "meetup.com",
		"twitch.tv", "youtube.com",
		"whatsapp.com", "telegram.org",
		"photos.google.com",
	}
	for _, s := range socialSenders {
		if strings.Contains(email, s) {
			return "social"
		}
	}
	// Social — subjects (Google Photos sharing, Maps reviews)
	if containsAny(email, "google.com") && containsAny(subjectLower,
		"shared a photo", "sharing photos", "activity on google photos",
		"responded to your post", "reviewed") {
		return "social"
	}
	if hasLabel(labels, "CATEGORY_SOCIAL") {
		return "social"
	}

	// Government / civic
	if containsAny(email, "dmv", "virginia.gov", "va.gov", "irs.gov",
		"ssa.gov", "courts.gov", "mail.mil") {
		return "government"
	}
	if containsAny(fromLower, "department of motor", "governor") {
		return "government"
	}

	// Promotions — senders
	promoSenders := []string{
		"promo@", "offer@", "offers@", "deals@",
		"marketing@", "sale@", "store@", "shop@",
		"lottery", "groupon.com", "retailmenot.com",
		"target.com", "bestbuy.com",
		"homedepot.com", "lowes.com",
		"etsy.com", "wayfair.com",
		"clearancejobs.com",
		"avis.com", "hilton.com", "united.com",
		"deleteme.com",
		"lockly.com",
		"do-not-reply@va-mvp.com",
		"googleuxresearch",
	}
	for _, s := range promoSenders {
		if strings.Contains(email, s) {
			return "promotions"
		}
	}
	// Promotions — subjects
	if containsAny(subjectLower,
		"% off", "limited time", "exclusive offer",
		"free gift", "flash sale", "clearance",
		"don't miss", "act now", "save big",
		"special offer", "coupon", "promo code",
		"earn rewards", "reward points",
		"free trial ends", "trial is ending",
		"thank you for renting", "thank you for choosing to stay",
		"digital key") {
		return "promotions"
	}
	if hasLabel(labels, "CATEGORY_PROMOTIONS") {
		return "promotions"
	}

	// Account / service notifications from automated senders → promotions
	if isAutomatedSender(email) && containsAny(subjectLower,
		"registration confirmed", "welcome to", "subscription confirmed",
		"profile updated", "settings changed",
		"getting started", "tips for", "how to get",
		"thank you for registering", "keep your card",
		"welcome to google", "preorder now",
		"your appointment has been",
		"review your google account", "review your account settings") {
		return "promotions"
	}

	// Needs attention: personal emails from real people
	if hasLabel(labels, "CATEGORY_PERSONAL") {
		if hasLabel(labels, "IMPORTANT") || !isAutomatedSender(email) {
			return "needs_attention"
		}
	}

	// Last resort: if IMPORTANT label from a real person, surface it
	if hasLabel(labels, "IMPORTANT") && !isAutomatedSender(email) {
		return "needs_attention"
	}

	return "other"
}

// containsAny returns true if s contains any of the given substrings
func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// Parsers for each email source

func parseWFPurchase(e parsedEmail) *IngestTransaction {
	m := wfAmountRe.FindStringSubmatch(e.body)
	if m == nil {
		return nil
	}
	amount := parseAmount(m[1])
	if amount <= 0 {
		return nil
	}

	merchant := "Unknown"
	location := ""
	// Try multiline match for merchant detail
	lines := strings.Split(e.body, "\n")
	for i, line := range lines {
		if strings.Contains(strings.ToLower(line), "merchant") {
			// Merchant name is often on the same line or next line
			cleaned := strings.TrimSpace(line)
			if mm := wfMerchantRe.FindStringSubmatch(cleaned); mm != nil {
				merchant = strings.TrimSpace(mm[1])
				if mm[2] != "" {
					location = strings.TrimSpace(mm[2])
				}
			} else if i+1 < len(lines) {
				// Merchant on next line
				merchant = strings.TrimSpace(lines[i+1])
				if i+2 < len(lines) && strings.Contains(strings.ToLower(lines[i+2]), "in ") {
					location = strings.TrimSpace(strings.TrimPrefix(strings.ToLower(lines[i+2]), "in "))
				}
			}
			break
		}
	}

	// Also try extracting from subject
	if merchant == "Unknown" && strings.Contains(e.subject, "purchase") {
		// Subject might contain merchant after "at" or similar
	}

	card := "...7870"
	if cm := wfCardRe.FindStringSubmatch(e.body); cm != nil {
		card = "..." + cm[1]
	}

	return &IngestTransaction{
		Date:     e.date,
		Amount:   amount,
		Merchant: merchant,
		Location: location,
		Card:     card,
		Source:   "wellsfargo",
		TxType:   "purchase",
	}
}

func parseWFPayment(e parsedEmail) *IngestPayment {
	m := wfAmountRe.FindStringSubmatch(e.body)
	if m == nil {
		return nil
	}
	amount := parseAmount(m[1])
	if amount <= 0 {
		return nil
	}

	fromAccount := ""
	toAccount := ""
	lines := strings.Split(e.body, "\n")
	for _, line := range lines {
		lower := strings.ToLower(line)
		if strings.Contains(lower, "from") && strings.Contains(lower, "account") {
			fromAccount = strings.TrimSpace(line)
		}
		if strings.Contains(lower, "to") && strings.Contains(lower, "account") {
			toAccount = strings.TrimSpace(line)
		}
	}

	return &IngestPayment{
		Date:        e.date,
		Amount:      amount,
		FromAccount: fromAccount,
		ToAccount:   toAccount,
		PaymentType: "credit_card_payment",
		Source:      "wellsfargo",
	}
}

func parseAmazonOrder(e parsedEmail) *IngestOrder {
	nm := amzOrderNumRe.FindStringSubmatch(e.body)
	if nm == nil {
		// Try subject
		nm = amzOrderNumRe.FindStringSubmatch(e.subject)
	}
	if nm == nil {
		return nil
	}

	tm := amzTotalRe.FindStringSubmatch(e.body)
	total := 0.0
	if tm != nil {
		total = parseAmount(tm[1])
	}

	var items []string
	itemMatches := amzItemRe.FindAllStringSubmatch(e.body, -1)
	for _, im := range itemMatches {
		items = append(items, strings.TrimSpace(im[1]))
	}
	if len(items) == 0 {
		// Try to extract item names from subject "Ordered: ITEM NAME"
		if strings.Contains(e.subject, "Ordered:") {
			item := strings.TrimSpace(strings.SplitN(e.subject, "Ordered:", 2)[1])
			if item != "" {
				items = []string{item}
			}
		}
	}

	return &IngestOrder{
		Date:      e.date,
		OrderNum:  nm[1],
		Items:     items,
		Total:     total,
		Recipient: "",
	}
}

func parseAmazonDelivery(e parsedEmail) *IngestDelivery {
	status := "shipped"
	if strings.Contains(strings.ToLower(e.subject), "delivered") {
		status = "delivered"
	} else if strings.Contains(strings.ToLower(e.subject), "out for delivery") {
		status = "out_for_delivery"
	}

	desc := e.subject
	if strings.Contains(desc, ":") {
		desc = strings.TrimSpace(strings.SplitN(desc, ":", 2)[1])
	}

	return &IngestDelivery{
		Source:      "amazon",
		Description: desc,
		Status:      status,
		MessageID:   e.messageID,
	}
}

func parseBofAPayment(e parsedEmail) *IngestPayment {
	m := bofaAmountRe.FindStringSubmatch(e.body)
	if m == nil {
		return nil
	}
	amount := parseAmount(m[1])
	if amount <= 0 {
		return nil
	}

	return &IngestPayment{
		Date:        e.date,
		Amount:      amount,
		FromAccount: "BofA Checking",
		ToAccount:   "BofA Vehicle Loan ...7545",
		PaymentType: "loan_payment",
		Source:      "bofa",
	}
}

func parseSchwabIncome(e parsedEmail) *IngestIncome {
	m := schwabAmountRe.FindStringSubmatch(e.body)
	if m == nil {
		return nil
	}
	amount := parseAmount(m[1])
	if amount <= 0 {
		return nil
	}

	incomeType := "rsu_vest"
	desc := "AKAM RSU Vest"
	if strings.Contains(strings.ToLower(e.subject), "transfer") {
		incomeType = "transfer"
		desc = "Schwab Transfer"
	}

	return &IngestIncome{
		Date:        e.date,
		Amount:      amount,
		Source:      "schwab",
		Description: desc,
		IncomeType:  incomeType,
	}
}

// Route an email to the appropriate parser
func routeEmail(e parsedEmail, db *sqlx.DB) (tx int, ord int, pay int, inc int) {
	from := strings.ToLower(e.from)
	subject := strings.ToLower(e.subject)

	switch {
	case strings.Contains(from, "alerts@notify.wellsfargo.com"):
		if strings.Contains(subject, "purchase") || strings.Contains(subject, "credit card") {
			if t := parseWFPurchase(e); t != nil {
				merchant := normalizeMerchant(t.Merchant)
				cat := categorize(t.Merchant)
				if cat == "Other" {
					cat = categorize(merchant)
				}
				h := txHash(t.Date, t.Amount, merchant)
				res, err := db.Exec(
					`INSERT OR IGNORE INTO transactions (date, amount, merchant, location, card, category, source, tx_type, hash)
					 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
					t.Date, t.Amount, merchant, t.Location, t.Card, cat, t.Source, t.TxType, h,
				)
				if err == nil {
					rows, _ := res.RowsAffected()
					tx += int(rows)
				}
			}
		} else if strings.Contains(subject, "payment") {
			if p := parseWFPayment(e); p != nil {
				h := paymentHash(p.Date, p.Amount, p.ToAccount)
				res, err := db.Exec(
					`INSERT OR IGNORE INTO payments (date, amount, from_account, to_account, payment_type, source, hash)
					 VALUES (?, ?, ?, ?, ?, ?, ?)`,
					p.Date, p.Amount, p.FromAccount, p.ToAccount, p.PaymentType, p.Source, h,
				)
				if err == nil {
					rows, _ := res.RowsAffected()
					pay += int(rows)
				}
			}
		}

	case strings.Contains(from, "auto-confirm@amazon.com"):
		if o := parseAmazonOrder(e); o != nil {
			itemsJSON := "[]"
			if len(o.Items) > 0 {
				b, _ := json.Marshal(o.Items)
				itemsJSON = string(b)
			}
			res, err := db.Exec(
				`INSERT OR IGNORE INTO orders (date, order_num, items, total, recipient)
				 VALUES (?, ?, ?, ?, ?)`,
				o.Date, o.OrderNum, itemsJSON, o.Total, o.Recipient,
			)
			if err == nil {
				rows, _ := res.RowsAffected()
				ord += int(rows)
			}
		}

	case strings.Contains(from, "shipment-tracking@amazon.com") ||
		(strings.Contains(from, "order-update@amazon.com") || strings.Contains(from, "amazon.com")) &&
			(strings.Contains(subject, "shipped") || strings.Contains(subject, "delivered") || strings.Contains(subject, "out for delivery")):
		if d := parseAmazonDelivery(e); d != nil {
			now := time.Now().Format(time.RFC3339)
			db.Exec(
				`INSERT OR IGNORE INTO deliveries (source, tracking_num, order_ref, description, status, expected_date, delivered_date, last_updated, message_id)
				 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				d.Source, d.TrackingNum, d.OrderRef, d.Description, d.Status,
				d.ExpectedDate, d.DeliveredDate, now, d.MessageID,
			)
		}

	case strings.Contains(from, "billpay@billpay.bankofamerica.com"):
		if p := parseBofAPayment(e); p != nil {
			h := paymentHash(p.Date, p.Amount, p.ToAccount)
			res, err := db.Exec(
				`INSERT OR IGNORE INTO payments (date, amount, from_account, to_account, payment_type, source, hash)
				 VALUES (?, ?, ?, ?, ?, ?, ?)`,
				p.Date, p.Amount, p.FromAccount, p.ToAccount, p.PaymentType, p.Source, h,
			)
			if err == nil {
				rows, _ := res.RowsAffected()
				pay += int(rows)
			}
		}

	case strings.Contains(from, "donotreply@mail.schwab.com"):
		if strings.Contains(subject, "restricted stock") || strings.Contains(subject, "equity award") || strings.Contains(subject, "rsu") {
			if inc := parseSchwabIncome(e); inc != nil {
				h := incomeHash(inc.Date, inc.Amount, inc.Description)
				res, err := db.Exec(
					`INSERT OR IGNORE INTO income (date, amount, source, description, income_type, hash)
					 VALUES (?, ?, ?, ?, ?, ?)`,
					inc.Date, inc.Amount, inc.Source, inc.Description, inc.IncomeType, h,
				)
				if err == nil {
					rows, _ := res.RowsAffected()
					return 0, 0, 0, int(rows)
				}
			}
		}
	}
	return
}

// Fetch messages from Gmail with concurrent workers
func fetchMessages(svc *gmail.Service, query string, maxResults int64) ([]*gmail.Message, error) {
	var allIDs []string

	call := svc.Users.Messages.List("me").Q(query).MaxResults(maxResults)
	for {
		resp, err := call.Do()
		if err != nil {
			return nil, fmt.Errorf("message list failed: %w", err)
		}
		for _, m := range resp.Messages {
			allIDs = append(allIDs, m.Id)
		}
		if resp.NextPageToken == "" || int64(len(allIDs)) >= maxResults {
			break
		}
		call = svc.Users.Messages.List("me").Q(query).MaxResults(maxResults).PageToken(resp.NextPageToken)
	}

	if len(allIDs) == 0 {
		return nil, nil
	}

	// Fetch full messages with worker pool
	messages := make([]*gmail.Message, len(allIDs))
	var mu sync.Mutex
	var fetchErr error
	var wg sync.WaitGroup

	workers := 5
	idCh := make(chan int, len(allIDs))
	for i := range allIDs {
		idCh <- i
	}
	close(idCh)

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range idCh {
				msg, err := svc.Users.Messages.Get("me", allIDs[idx]).Format("full").Do()
				mu.Lock()
				if err != nil {
					if fetchErr == nil {
						fetchErr = err
					}
				} else {
					messages[idx] = msg
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if fetchErr != nil {
		return nil, fmt.Errorf("message fetch failed: %w", fetchErr)
	}

	// Filter out nils (shouldn't happen if no errors)
	var result []*gmail.Message
	for _, m := range messages {
		if m != nil {
			result = append(result, m)
		}
	}
	return result, nil
}

// needsFullBody returns true if this sender requires full body parsing for financial data
func needsFullBody(from string) bool {
	lower := strings.ToLower(from)
	financialSenders := []string{
		"alerts@notify.wellsfargo.com",
		"auto-confirm@amazon.com",
		"shipment-tracking@amazon.com",
		"order-update@amazon.com",
		"billpay@billpay.bankofamerica.com",
		"donotreply@mail.schwab.com",
		"citicards@info6.citi.com",
		"service@paypal.com",
	}
	for _, s := range financialSenders {
		if strings.Contains(lower, s) {
			return true
		}
	}
	return false
}

func cmdSync(db *sqlx.DB) {
	svc, err := getGmailService()
	if err != nil {
		fmt.Println(errorStyle.Render("  " + err.Error()))
		return
	}

	fmt.Printf("  %s  Fetching unread emails...\n", dimStyle.Render("⟳"))

	// Fetch ALL unread message IDs
	var allIDs []string
	call := svc.Users.Messages.List("me").Q("is:unread").MaxResults(500)
	for {
		resp, err := call.Do()
		if err != nil {
			fmt.Println(errorStyle.Render("  Fetch error: " + err.Error()))
			return
		}
		for _, m := range resp.Messages {
			allIDs = append(allIDs, m.Id)
		}
		if resp.NextPageToken == "" || len(allIDs) >= 500 {
			break
		}
		call = svc.Users.Messages.List("me").Q("is:unread").MaxResults(500).PageToken(resp.NextPageToken)
	}

	if len(allIDs) == 0 {
		fmt.Printf("  %s  Inbox zero!\n", successStyle.Render("✓"))
		setSyncState(db, "last_sync_epoch", fmt.Sprintf("%d", time.Now().Unix()))
		return
	}

	fmt.Printf("  %s  Fetching %d messages...\n", dimStyle.Render("⟳"), len(allIDs))

	// Fetch message metadata with worker pool
	type fetchResult struct {
		msg    *gmail.Message
		format string
	}
	results := make([]fetchResult, len(allIDs))
	var mu sync.Mutex
	var fetchErr error
	var wg sync.WaitGroup

	workers := 5
	idCh := make(chan int, len(allIDs))
	for i := range allIDs {
		idCh <- i
	}
	close(idCh)

	// First pass: fetch metadata for all
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range idCh {
				// Fetch metadata (Subject, From, Date headers + snippet + labels)
				msg, err := svc.Users.Messages.Get("me", allIDs[idx]).
					Format("metadata").
					MetadataHeaders("Subject", "From", "Date").
					Do()
				mu.Lock()
				if err != nil {
					if fetchErr == nil {
						fetchErr = err
					}
				} else {
					results[idx] = fetchResult{msg: msg, format: "metadata"}
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if fetchErr != nil {
		fmt.Println(errorStyle.Render("  Fetch error: " + fetchErr.Error()))
		return
	}

	// Classify all emails and upsert to emails table
	now := time.Now().Format(time.RFC3339)
	catCounts := make(map[string]int)
	var financialIDs []int // indices that need full body fetch for financial parsing

	for i, r := range results {
		if r.msg == nil {
			continue
		}
		msg := r.msg
		from := getHeader(msg, "From")
		subject := getHeader(msg, "Subject")
		category := classifyEmail(from, subject, msg.LabelIds)
		catCounts[category]++

		labelsJSON, _ := json.Marshal(msg.LabelIds)
		upsertEmail(db, EmailRow{
			MessageID:  msg.Id,
			ThreadID:   msg.ThreadId,
			Date:       parseEmailDate(msg),
			Sender:     extractSenderEmail(from),
			SenderName: extractSenderName(from),
			Subject:    subject,
			Snippet:    msg.Snippet,
			Labels:     string(labelsJSON),
			Category:   category,
			IsUnread:   1,
			SyncedAt:   now,
		})

		// Mark for full body fetch if financial
		if needsFullBody(from) {
			financialIDs = append(financialIDs, i)
		}
	}

	// Auto-ack ignored senders
	ignored := loadIgnoredSenders(db)
	var ignoredIDs []string
	for _, r := range results {
		if r.msg == nil {
			continue
		}
		from := getHeader(r.msg, "From")
		sender := extractSenderEmail(from)
		if ignored[sender] {
			ignoredIDs = append(ignoredIDs, r.msg.Id)
			db.Exec(`UPDATE emails SET is_unread = 0 WHERE message_id = ?`, r.msg.Id)
		}
	}
	if len(ignoredIDs) > 0 {
		for i := 0; i < len(ignoredIDs); i += 1000 {
			end := i + 1000
			if end > len(ignoredIDs) {
				end = len(ignoredIDs)
			}
			req := &gmail.BatchModifyMessagesRequest{
				Ids:            ignoredIDs[i:end],
				RemoveLabelIds: []string{"UNREAD"},
			}
			svc.Users.Messages.BatchModify("me", req).Do()
		}
		fmt.Printf("  %s  Auto-acked %d ignored emails\n", dimStyle.Render("⊘"), len(ignoredIDs))
	}

	// Second pass: fetch full body for financial emails and parse
	var totalTx, totalOrd, totalPay, totalInc int
	if len(financialIDs) > 0 {
		bodyIDCh := make(chan int, len(financialIDs))
		for _, idx := range financialIDs {
			bodyIDCh <- idx
		}
		close(bodyIDCh)

		var wg2 sync.WaitGroup
		for w := 0; w < workers; w++ {
			wg2.Add(1)
			go func() {
				defer wg2.Done()
				for idx := range bodyIDCh {
					fullMsg, err := svc.Users.Messages.Get("me", allIDs[idx]).Format("full").Do()
					if err != nil {
						continue
					}
					e := parsedEmail{
						messageID: fullMsg.Id,
						threadID:  fullMsg.ThreadId,
						date:      parseEmailDate(fullMsg),
						from:      getHeader(fullMsg, "From"),
						subject:   getHeader(fullMsg, "Subject"),
						body:      decodeMessageBody(fullMsg),
						labelIDs:  fullMsg.LabelIds,
					}
					tx, ord, pay, inc := routeEmail(e, db)
					mu.Lock()
					totalTx += tx
					totalOrd += ord
					totalPay += pay
					totalInc += inc
					mu.Unlock()
				}
			}()
		}
		wg2.Wait()
	}

	// Update trajectory with current unread count
	label, err := svc.Users.Labels.Get("me", "INBOX").Do()
	if err == nil && label != nil {
		appendTrajectory(int(label.MessagesUnread))
	}

	// Save sync timestamp
	setSyncState(db, "last_sync_epoch", fmt.Sprintf("%d", time.Now().Unix()))

	// Write urgent.txt for shell notification (.zshrc reads this)
	writeUrgentFile(db)

	// Print results
	fmt.Printf("  %s  Synced %d emails", successStyle.Render("✓"), len(allIDs))
	if totalTx+totalOrd+totalPay+totalInc > 0 {
		fmt.Printf(" (%d tx, %d orders, %d payments, %d income)", totalTx, totalOrd, totalPay, totalInc)
	}
	fmt.Println()

	// Category breakdown
	order := []string{"needs_attention", "calendar", "kids", "financial", "deliveries", "security", "newsletters", "social", "government", "promotions", "other"}
	labels := map[string]string{
		"needs_attention": "Needs attention", "calendar": "Calendar", "kids": "Kids",
		"financial": "Financial", "deliveries": "Deliveries", "security": "Security",
		"newsletters": "Newsletters", "social": "Social", "government": "Government",
		"promotions": "Promotions", "other": "Other",
	}
	for _, cat := range order {
		if n, ok := catCounts[cat]; ok && n > 0 {
			fmt.Printf("    %s %s\n", dimStyle.Render(fmt.Sprintf("%3d", n)), labels[cat])
		}
	}
	fmt.Println()
}

func writeUrgentFile(db *sqlx.DB) {
	emails, _ := getEmailsByCategory(db, "needs_attention")
	path := dataDir() + "/urgent.txt"
	f, err := os.Create(path)
	if err != nil {
		return
	}
	defer f.Close()
	for _, e := range emails {
		name := e.SenderName
		if name == "" {
			name = e.Sender
		}
		fmt.Fprintf(f, "%s — %s\n", name, e.Subject)
	}
}

func appendTrajectory(unreadCount int) {
	path := dataDir() + "/trajectory.csv"
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()

	// Check if file needs header
	info, _ := f.Stat()
	if info.Size() == 0 {
		f.WriteString("timestamp,unread_count\n")
	}

	ts := time.Now().Format("2006-01-02T15:04:05")
	f.WriteString(fmt.Sprintf("%s,%d\n", ts, unreadCount))
}
