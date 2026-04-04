package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite"
)

// DB row types
type TxRow struct {
	ID       int64   `db:"id"`
	Date     string  `db:"date"`
	Amount   float64 `db:"amount"`
	Merchant string  `db:"merchant"`
	Location string  `db:"location"`
	Card     string  `db:"card"`
	Category string  `db:"category"`
	Source   string  `db:"source"`
	TxType   string  `db:"tx_type"`
	Hash     string  `db:"hash"`
}

type OrderRow struct {
	ID        int64   `db:"id"`
	Date      string  `db:"date"`
	OrderNum  string  `db:"order_num"`
	Items     string  `db:"items"`
	Total     float64 `db:"total"`
	Recipient string  `db:"recipient"`
}

type PaymentRow struct {
	ID          int64   `db:"id"`
	Date        string  `db:"date"`
	Amount      float64 `db:"amount"`
	FromAccount string  `db:"from_account"`
	ToAccount   string  `db:"to_account"`
	PaymentType string  `db:"payment_type"`
	Source      string  `db:"source"`
	Hash        string  `db:"hash"`
}

type IncomeRow struct {
	ID          int64   `db:"id"`
	Date        string  `db:"date"`
	Amount      float64 `db:"amount"`
	Source      string  `db:"source"`
	Description string  `db:"description"`
	IncomeType  string  `db:"income_type"`
	Hash        string  `db:"hash"`
}

type DeliveryRow struct {
	ID            int64  `db:"id"`
	Source        string `db:"source"`
	TrackingNum   string `db:"tracking_num"`
	OrderRef      string `db:"order_ref"`
	Description   string `db:"description"`
	Status        string `db:"status"`
	ExpectedDate  string `db:"expected_date"`
	DeliveredDate string `db:"delivered_date"`
	LastUpdated   string `db:"last_updated"`
	MessageID     string `db:"message_id"`
}

type ArchiveLogRow struct {
	ID         int64  `db:"id"`
	ArchivedAt string `db:"archived_at"`
	Sender     string `db:"sender"`
	Subject    string `db:"subject"`
	Reason     string `db:"reason"`
	MessageID  string `db:"message_id"`
}

type EmailRow struct {
	MessageID  string `db:"message_id"`
	ThreadID   string `db:"thread_id"`
	Date       string `db:"date"`
	Sender     string `db:"sender"`
	SenderName string `db:"sender_name"`
	Subject    string `db:"subject"`
	Snippet    string `db:"snippet"`
	Labels     string `db:"labels"`
	Category   string `db:"category"`
	IsUnread   int    `db:"is_unread"`
	SyncedAt   string `db:"synced_at"`
}

type IgnoredSenderRow struct {
	ID         int64  `db:"id"`
	Sender     string `db:"sender"`
	SenderName string `db:"sender_name"`
	Reason     string `db:"reason"`
	AddedAt    string `db:"added_at"`
}

type UnsubRow struct {
	ID          int64  `db:"id"`
	Sender      string `db:"sender"`
	SenderName  string `db:"sender_name"`
	UnsubURL    string `db:"unsub_url"`
	UnsubEmail  string `db:"unsub_email"`
	Status      string `db:"status"`
	AddedAt     string `db:"added_at"`
	CompletedAt string `db:"completed_at"`
	MessageID   string `db:"message_id"`
}

type RecurringRow struct {
	ID            int64   `db:"id"`
	Merchant      string  `db:"merchant"`
	Category      string  `db:"category"`
	AvgAmount     float64 `db:"avg_amount"`
	FrequencyDays int     `db:"frequency_days"`
	LastSeen      string  `db:"last_seen"`
	FirstSeen     string  `db:"first_seen"`
	TotalSpent    float64 `db:"total_spent"`
	Count         int     `db:"count"`
	IsActive      int     `db:"is_active"`
}

// Ingest format (from scheduled task JSON)
type IngestTransaction struct {
	Date     string  `json:"date"`
	Amount   float64 `json:"amount"`
	Merchant string  `json:"merchant"`
	Location string  `json:"location"`
	Card     string  `json:"card"`
	Source   string  `json:"source,omitempty"`
	TxType   string  `json:"tx_type,omitempty"`
}

type IngestOrder struct {
	Date      string   `json:"date"`
	OrderNum  string   `json:"order_num"`
	Items     []string `json:"items"`
	Total     float64  `json:"total"`
	Recipient string   `json:"recipient"`
}

type IngestPayment struct {
	Date        string  `json:"date"`
	Amount      float64 `json:"amount"`
	FromAccount string  `json:"from_account"`
	ToAccount   string  `json:"to_account"`
	PaymentType string  `json:"payment_type"`
	Source      string  `json:"source"`
}

type IngestIncome struct {
	Date        string  `json:"date"`
	Amount      float64 `json:"amount"`
	Source      string  `json:"source"`
	Description string  `json:"description"`
	IncomeType  string  `json:"income_type"`
}

type IngestDelivery struct {
	Source        string `json:"source"`
	TrackingNum   string `json:"tracking_num,omitempty"`
	OrderRef      string `json:"order_ref,omitempty"`
	Description   string `json:"description"`
	Status        string `json:"status"` // ordered, shipped, out_for_delivery, delivered
	ExpectedDate  string `json:"expected_date,omitempty"`
	DeliveredDate string `json:"delivered_date,omitempty"`
	MessageID     string `json:"message_id,omitempty"`
}

type IngestArchived struct {
	MessageID string `json:"message_id"`
	Sender    string `json:"sender"`
	Subject   string `json:"subject"`
	Reason    string `json:"reason"` // wf_purchase, amazon_shipping, usps_digest, political, promotional, calendar_notification, payment_reminder
}

type IngestPayload struct {
	Generated    string              `json:"generated"`
	Transactions []IngestTransaction `json:"transactions"`
	Orders       []IngestOrder       `json:"orders"`
	Payments     []IngestPayment     `json:"payments,omitempty"`
	Income       []IngestIncome      `json:"income,omitempty"`
	Deliveries   []IngestDelivery    `json:"deliveries,omitempty"`
	Archived     []IngestArchived    `json:"archived,omitempty"`
}

type TrajectoryPoint struct {
	Timestamp time.Time
	Count     int
}

func dataDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Documents", "Projects", "stash", "gmail")
}

func dbPath() string {
	return filepath.Join(dataDir(), "gmail.db")
}

func openDB() (*sqlx.DB, error) {
	db, err := sqlx.Open("sqlite", dbPath()+"?_journal_mode=WAL")
	if err != nil {
		return nil, err
	}
	if err := initSchema(db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func initSchema(db *sqlx.DB) error {
	schema := `
	CREATE TABLE IF NOT EXISTS transactions (
		id       INTEGER PRIMARY KEY AUTOINCREMENT,
		date     TEXT NOT NULL,
		amount   REAL NOT NULL,
		merchant TEXT NOT NULL,
		location TEXT DEFAULT '',
		card     TEXT DEFAULT '',
		category TEXT DEFAULT '',
		source   TEXT DEFAULT 'wellsfargo',
		tx_type  TEXT DEFAULT 'purchase',
		hash     TEXT UNIQUE NOT NULL
	);

	CREATE TABLE IF NOT EXISTS orders (
		id        INTEGER PRIMARY KEY AUTOINCREMENT,
		date      TEXT NOT NULL,
		order_num TEXT UNIQUE NOT NULL,
		items     TEXT DEFAULT '[]',
		total     REAL NOT NULL,
		recipient TEXT DEFAULT ''
	);

	CREATE TABLE IF NOT EXISTS payments (
		id           INTEGER PRIMARY KEY AUTOINCREMENT,
		date         TEXT NOT NULL,
		amount       REAL NOT NULL,
		from_account TEXT DEFAULT '',
		to_account   TEXT DEFAULT '',
		payment_type TEXT DEFAULT '',
		source       TEXT DEFAULT '',
		hash         TEXT UNIQUE NOT NULL
	);

	CREATE TABLE IF NOT EXISTS income (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		date        TEXT NOT NULL,
		amount      REAL NOT NULL,
		source      TEXT DEFAULT '',
		description TEXT DEFAULT '',
		income_type TEXT DEFAULT '',
		hash        TEXT UNIQUE NOT NULL
	);

	CREATE TABLE IF NOT EXISTS recurring (
		id             INTEGER PRIMARY KEY AUTOINCREMENT,
		merchant       TEXT NOT NULL,
		category       TEXT DEFAULT '',
		avg_amount     REAL DEFAULT 0,
		frequency_days INTEGER DEFAULT 0,
		last_seen      TEXT DEFAULT '',
		first_seen     TEXT DEFAULT '',
		total_spent    REAL DEFAULT 0,
		count          INTEGER DEFAULT 0,
		is_active      INTEGER DEFAULT 1
	);

	CREATE INDEX IF NOT EXISTS idx_tx_date ON transactions(date);
	CREATE INDEX IF NOT EXISTS idx_tx_category ON transactions(category);
	CREATE INDEX IF NOT EXISTS idx_tx_amount ON transactions(amount);
	CREATE INDEX IF NOT EXISTS idx_tx_source ON transactions(source);
	CREATE INDEX IF NOT EXISTS idx_tx_merchant ON transactions(merchant);
	CREATE INDEX IF NOT EXISTS idx_orders_date ON orders(date);
	CREATE INDEX IF NOT EXISTS idx_orders_total ON orders(total);
	CREATE INDEX IF NOT EXISTS idx_payments_date ON payments(date);
	CREATE INDEX IF NOT EXISTS idx_income_date ON income(date);

	CREATE TABLE IF NOT EXISTS deliveries (
		id             INTEGER PRIMARY KEY AUTOINCREMENT,
		source         TEXT DEFAULT '',
		tracking_num   TEXT DEFAULT '',
		order_ref      TEXT DEFAULT '',
		description    TEXT DEFAULT '',
		status         TEXT DEFAULT 'ordered',
		expected_date  TEXT DEFAULT '',
		delivered_date TEXT DEFAULT '',
		last_updated   TEXT NOT NULL,
		message_id     TEXT DEFAULT ''
	);

	CREATE TABLE IF NOT EXISTS archive_log (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		archived_at TEXT NOT NULL,
		sender      TEXT DEFAULT '',
		subject     TEXT DEFAULT '',
		reason      TEXT DEFAULT '',
		message_id  TEXT UNIQUE NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_deliveries_status ON deliveries(status);
	CREATE INDEX IF NOT EXISTS idx_deliveries_expected ON deliveries(expected_date);
	CREATE INDEX IF NOT EXISTS idx_archive_date ON archive_log(archived_at);
	CREATE INDEX IF NOT EXISTS idx_archive_reason ON archive_log(reason);

	CREATE TABLE IF NOT EXISTS sync_state (
		key   TEXT PRIMARY KEY,
		value TEXT NOT NULL
	);

	CREATE TABLE IF NOT EXISTS emails (
		message_id  TEXT PRIMARY KEY,
		thread_id   TEXT DEFAULT '',
		date        TEXT NOT NULL,
		sender      TEXT NOT NULL,
		sender_name TEXT DEFAULT '',
		subject     TEXT NOT NULL,
		snippet     TEXT DEFAULT '',
		labels      TEXT DEFAULT '[]',
		category    TEXT DEFAULT 'other',
		is_unread   INTEGER DEFAULT 1,
		synced_at   TEXT NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_emails_date ON emails(date);
	CREATE INDEX IF NOT EXISTS idx_emails_category ON emails(category);
	CREATE INDEX IF NOT EXISTS idx_emails_unread ON emails(is_unread);

	CREATE TABLE IF NOT EXISTS unsubscribes (
		id            INTEGER PRIMARY KEY AUTOINCREMENT,
		sender        TEXT NOT NULL,
		sender_name   TEXT DEFAULT '',
		unsub_url     TEXT DEFAULT '',
		unsub_email   TEXT DEFAULT '',
		status        TEXT DEFAULT 'pending',
		added_at      TEXT NOT NULL,
		completed_at  TEXT DEFAULT '',
		message_id    TEXT DEFAULT ''
	);
	CREATE INDEX IF NOT EXISTS idx_unsub_status ON unsubscribes(status);
	CREATE INDEX IF NOT EXISTS idx_unsub_sender ON unsubscribes(sender);

	CREATE TABLE IF NOT EXISTS ignored_senders (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		sender      TEXT NOT NULL UNIQUE,
		sender_name TEXT DEFAULT '',
		reason      TEXT DEFAULT '',
		added_at    TEXT NOT NULL
	);
	`
	_, err := db.Exec(schema)
	if err != nil {
		return err
	}

	// Migration: add source/tx_type columns if missing (for existing DBs)
	db.Exec("ALTER TABLE transactions ADD COLUMN source TEXT DEFAULT 'wellsfargo'")
	db.Exec("ALTER TABLE transactions ADD COLUMN tx_type TEXT DEFAULT 'purchase'")

	return nil
}

func txHash(date string, amount float64, merchant string) string {
	d := date
	if len(d) > 10 {
		d = d[:10]
	}
	return fmt.Sprintf("%s|%.2f|%s", d, amount, strings.ToUpper(merchant))
}

func paymentHash(date string, amount float64, to string) string {
	d := date
	if len(d) > 10 {
		d = d[:10]
	}
	return fmt.Sprintf("PAY|%s|%.2f|%s", d, amount, strings.ToUpper(to))
}

func incomeHash(date string, amount float64, desc string) string {
	d := date
	if len(d) > 10 {
		d = d[:10]
	}
	return fmt.Sprintf("INC|%s|%.2f|%s", d, amount, strings.ToUpper(desc))
}

// Sync state helpers

func getSyncState(db *sqlx.DB, key string) string {
	var val string
	err := db.Get(&val, `SELECT value FROM sync_state WHERE key = ?`, key)
	if err != nil {
		return ""
	}
	return val
}

func setSyncState(db *sqlx.DB, key, value string) {
	db.Exec(`INSERT INTO sync_state (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = ?`, key, value, value)
}

// Ingest new data from JSON file
func ingestFromJSON(db *sqlx.DB, path string) (newTx, newOrd, newPay, newInc int, err error) {
	f, err := os.ReadFile(path)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	var payload IngestPayload
	if err := json.Unmarshal(f, &payload); err != nil {
		return 0, 0, 0, 0, err
	}

	for _, tx := range payload.Transactions {
		merchant := normalizeMerchant(tx.Merchant)
		h := txHash(tx.Date, tx.Amount, merchant)
		// Categorize from the original merchant string first; fall back to canonical name.
		// This ensures raw patterns (e.g. "BP#7719...") still match category rules.
		cat := categorize(tx.Merchant)
		if cat == "Other" {
			cat = categorize(merchant)
		}
		src := tx.Source
		if src == "" {
			src = "wellsfargo"
		}
		txType := tx.TxType
		if txType == "" {
			txType = "purchase"
		}
		res, err := db.Exec(
			`INSERT OR IGNORE INTO transactions (date, amount, merchant, location, card, category, source, tx_type, hash)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			tx.Date, tx.Amount, merchant, tx.Location, tx.Card, cat, src, txType, h,
		)
		if err != nil {
			continue
		}
		rows, _ := res.RowsAffected()
		newTx += int(rows)
	}

	for _, o := range payload.Orders {
		itemsJSON, _ := json.Marshal(o.Items)
		res, err := db.Exec(
			`INSERT OR IGNORE INTO orders (date, order_num, items, total, recipient)
			 VALUES (?, ?, ?, ?, ?)`,
			o.Date, o.OrderNum, string(itemsJSON), o.Total, o.Recipient,
		)
		if err != nil {
			continue
		}
		rows, _ := res.RowsAffected()
		newOrd += int(rows)
	}

	for _, p := range payload.Payments {
		h := paymentHash(p.Date, p.Amount, p.ToAccount)
		res, err := db.Exec(
			`INSERT OR IGNORE INTO payments (date, amount, from_account, to_account, payment_type, source, hash)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			p.Date, p.Amount, p.FromAccount, p.ToAccount, p.PaymentType, p.Source, h,
		)
		if err != nil {
			continue
		}
		rows, _ := res.RowsAffected()
		newPay += int(rows)
	}

	for _, inc := range payload.Income {
		h := incomeHash(inc.Date, inc.Amount, inc.Description)
		res, err := db.Exec(
			`INSERT OR IGNORE INTO income (date, amount, source, description, income_type, hash)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			inc.Date, inc.Amount, inc.Source, inc.Description, inc.IncomeType, h,
		)
		if err != nil {
			continue
		}
		rows, _ := res.RowsAffected()
		newInc += int(rows)
	}

	now := time.Now().Format(time.RFC3339)

	for _, d := range payload.Deliveries {
		// Upsert: update status/dates if tracking_num already exists
		if d.TrackingNum != "" {
			existing := 0
			db.Get(&existing, `SELECT COUNT(*) FROM deliveries WHERE tracking_num = ?`, d.TrackingNum)
			if existing > 0 {
				db.Exec(`UPDATE deliveries SET status=?, expected_date=?, delivered_date=?, last_updated=?, description=?
				         WHERE tracking_num=?`,
					d.Status, d.ExpectedDate, d.DeliveredDate, now, d.Description, d.TrackingNum)
				continue
			}
		}
		db.Exec(
			`INSERT OR IGNORE INTO deliveries (source, tracking_num, order_ref, description, status, expected_date, delivered_date, last_updated, message_id)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			d.Source, d.TrackingNum, d.OrderRef, d.Description, d.Status,
			d.ExpectedDate, d.DeliveredDate, now, d.MessageID,
		)
	}

	for _, a := range payload.Archived {
		db.Exec(
			`INSERT OR IGNORE INTO archive_log (archived_at, sender, subject, reason, message_id)
			 VALUES (?, ?, ?, ?, ?)`,
			now, a.Sender, a.Subject, a.Reason, a.MessageID,
		)
	}

	return newTx, newOrd, newPay, newInc, nil
}

// Query helpers

func getTransactions(db *sqlx.DB, since string) ([]TxRow, error) {
	var rows []TxRow
	err := db.Select(&rows, `SELECT * FROM transactions WHERE date >= ? ORDER BY date DESC`, since)
	return rows, err
}

func getAllTransactions(db *sqlx.DB) ([]TxRow, error) {
	var rows []TxRow
	err := db.Select(&rows, `SELECT * FROM transactions ORDER BY date DESC`)
	return rows, err
}

func getOrders(db *sqlx.DB, since string) ([]OrderRow, error) {
	var rows []OrderRow
	err := db.Select(&rows, `SELECT * FROM orders WHERE date >= ? ORDER BY date DESC`, since)
	return rows, err
}

func getAllOrders(db *sqlx.DB) ([]OrderRow, error) {
	var rows []OrderRow
	err := db.Select(&rows, `SELECT * FROM orders ORDER BY date DESC`)
	return rows, err
}

type CategoryAgg struct {
	Category string  `db:"category"`
	Total    float64 `db:"total"`
	Count    int     `db:"count"`
}

func getCategoryTotals(db *sqlx.DB, since string) ([]CategoryAgg, error) {
	var aggs []CategoryAgg
	err := db.Select(&aggs,
		`SELECT category, SUM(amount) as total, COUNT(*) as count
		 FROM transactions WHERE date >= ?
		 GROUP BY category ORDER BY total DESC`, since)
	return aggs, err
}

type MonthlyAgg struct {
	Month string  `db:"month"`
	Total float64 `db:"total"`
	Count int     `db:"count"`
}

func getMonthlyTotals(db *sqlx.DB) ([]MonthlyAgg, error) {
	var aggs []MonthlyAgg
	err := db.Select(&aggs,
		`SELECT substr(date, 1, 7) as month, SUM(amount) as total, COUNT(*) as count
		 FROM transactions GROUP BY month ORDER BY month`)
	return aggs, err
}

type DailyAgg struct {
	Day   string  `db:"day"`
	Total float64 `db:"total"`
	Count int     `db:"count"`
}

func getDailyTotals(db *sqlx.DB, since string) ([]DailyAgg, error) {
	var aggs []DailyAgg
	err := db.Select(&aggs,
		`SELECT substr(date, 1, 10) as day, SUM(amount) as total, COUNT(*) as count
		 FROM transactions WHERE date >= ?
		 GROUP BY day ORDER BY day`, since)
	return aggs, err
}

func getGrandTotal(db *sqlx.DB, since string) (float64, int, error) {
	var total float64
	var count int
	row := db.QueryRow(`SELECT COALESCE(SUM(amount), 0), COUNT(*) FROM transactions WHERE date >= ?`, since)
	err := row.Scan(&total, &count)
	return total, count, err
}

// Merchant frequency for pattern detection
type MerchantFreq struct {
	Merchant  string  `db:"merchant"`
	Category  string  `db:"category"`
	Count     int     `db:"count"`
	Total     float64 `db:"total"`
	AvgAmount float64 `db:"avg_amount"`
	FirstSeen string  `db:"first_seen"`
	LastSeen  string  `db:"last_seen"`
}

func getMerchantFrequency(db *sqlx.DB, since string) ([]MerchantFreq, error) {
	var rows []MerchantFreq
	err := db.Select(&rows,
		`SELECT merchant, category, COUNT(*) as count, SUM(amount) as total,
		 AVG(amount) as avg_amount, MIN(date) as first_seen, MAX(date) as last_seen
		 FROM transactions WHERE date >= ?
		 GROUP BY merchant ORDER BY count DESC`, since)
	return rows, err
}

// Hour-of-day aggregation (needs timestamp, not just date — uses what we have)
type HourAgg struct {
	Hour  int     `db:"hour"`
	Total float64 `db:"total"`
	Count int     `db:"count"`
}

// Day-of-week aggregation
type DowAgg struct {
	Dow   int     `db:"dow"`
	Total float64 `db:"total"`
	Count int     `db:"count"`
	Avg   float64 `db:"avg_total"`
}

func getDowTotals(db *sqlx.DB, since string) ([]DowAgg, error) {
	// SQLite strftime %w: 0=Sunday, 6=Saturday
	var aggs []DowAgg
	err := db.Select(&aggs,
		`SELECT CAST(strftime('%w', date) AS INTEGER) as dow,
		 SUM(amount) as total, COUNT(*) as count,
		 AVG(amount) as avg_total
		 FROM transactions WHERE date >= ?
		 GROUP BY dow ORDER BY dow`, since)
	return aggs, err
}

// Payments and income queries
func getPayments(db *sqlx.DB, since string) ([]PaymentRow, error) {
	var rows []PaymentRow
	err := db.Select(&rows, `SELECT * FROM payments WHERE date >= ? ORDER BY date DESC`, since)
	return rows, err
}

func getMonthlyPayments(db *sqlx.DB) ([]MonthlyAgg, error) {
	var aggs []MonthlyAgg
	err := db.Select(&aggs,
		`SELECT substr(date, 1, 7) as month, SUM(amount) as total, COUNT(*) as count
		 FROM payments GROUP BY month ORDER BY month`)
	return aggs, err
}

func getIncome(db *sqlx.DB, since string) ([]IncomeRow, error) {
	var rows []IncomeRow
	err := db.Select(&rows, `SELECT * FROM income WHERE date >= ? ORDER BY date DESC`, since)
	return rows, err
}

func getMonthlyIncome(db *sqlx.DB) ([]MonthlyAgg, error) {
	var aggs []MonthlyAgg
	err := db.Select(&aggs,
		`SELECT substr(date, 1, 7) as month, SUM(amount) as total, COUNT(*) as count
		 FROM income GROUP BY month ORDER BY month`)
	return aggs, err
}

// Delivery queries

func getActiveDeliveries(db *sqlx.DB) ([]DeliveryRow, error) {
	var rows []DeliveryRow
	err := db.Select(&rows,
		`SELECT * FROM deliveries WHERE status != 'delivered' ORDER BY expected_date ASC`)
	return rows, err
}

func getRecentDeliveries(db *sqlx.DB, days int) ([]DeliveryRow, error) {
	since := time.Now().AddDate(0, 0, -days).Format("2006-01-02")
	var rows []DeliveryRow
	err := db.Select(&rows,
		`SELECT * FROM deliveries WHERE status = 'delivered' AND delivered_date >= ? ORDER BY delivered_date DESC`, since)
	return rows, err
}

// Archive log queries

type ArchiveReasonStat struct {
	Reason string `db:"reason"`
	Count  int    `db:"count"`
}

func getArchiveStats(db *sqlx.DB, since string) ([]ArchiveReasonStat, error) {
	var rows []ArchiveReasonStat
	err := db.Select(&rows,
		`SELECT reason, COUNT(*) as count FROM archive_log WHERE archived_at >= ?
		 GROUP BY reason ORDER BY count DESC`, since)
	return rows, err
}

func getArchiveTotalCount(db *sqlx.DB, since string) (int, error) {
	var count int
	err := db.Get(&count, `SELECT COUNT(*) FROM archive_log WHERE archived_at >= ?`, since)
	return count, err
}

// Daily spend helpers for brief command

func getSpendForDate(db *sqlx.DB, date string) (float64, int, error) {
	var total float64
	var count int
	row := db.QueryRow(`SELECT COALESCE(SUM(amount),0), COUNT(*) FROM transactions WHERE substr(date,1,10) = ?`, date)
	err := row.Scan(&total, &count)
	return total, count, err
}

func getDailyAvg(db *sqlx.DB, days int) (float64, error) {
	since := time.Now().AddDate(0, 0, -days).Format("2006-01-02")
	var total float64
	db.Get(&total, `SELECT COALESCE(SUM(amount),0) FROM transactions WHERE date >= ?`, since)
	return total / float64(days), nil
}

// Range-based query helpers (since + until, inclusive)

func getCategoryTotalsRange(db *sqlx.DB, since, until string) ([]CategoryAgg, error) {
	var aggs []CategoryAgg
	err := db.Select(&aggs,
		`SELECT category, SUM(amount) as total, COUNT(*) as count
		 FROM transactions WHERE date >= ? AND date <= ?
		 GROUP BY category ORDER BY total DESC`, since, until)
	return aggs, err
}

func getGrandTotalRange(db *sqlx.DB, since, until string) (float64, int, error) {
	var total float64
	var count int
	row := db.QueryRow(`SELECT COALESCE(SUM(amount), 0), COUNT(*) FROM transactions WHERE date >= ? AND date <= ?`, since, until)
	err := row.Scan(&total, &count)
	return total, count, err
}

// File-based data loaders (for inbox summary, urgent, trajectory, weekly insight, security)

func loadSummary() (string, error) {
	path := filepath.Join(dataDir(), "inbox-summary.md")
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("no summary file — run: gmail refresh")
	}
	return string(b), nil
}

func loadUrgent() ([]string, error) {
	path := filepath.Join(dataDir(), "urgent.txt")
	b, err := os.ReadFile(path)
	if err != nil || len(strings.TrimSpace(string(b))) == 0 {
		return nil, nil
	}
	var lines []string
	for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	return lines, nil
}

func loadTrajectory() ([]TrajectoryPoint, error) {
	path := filepath.Join(dataDir(), "trajectory.csv")
	f, err := os.Open(path)
	if err != nil {
		return nil, nil
	}
	defer f.Close()

	reader := csv.NewReader(f)
	csvRows, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}

	var points []TrajectoryPoint
	for i, row := range csvRows {
		if i == 0 {
			continue
		}
		if len(row) < 2 {
			continue
		}
		ts, err := time.Parse("2006-01-02T15:04:05", row[0])
		if err != nil {
			ts, err = time.Parse(time.RFC3339, row[0])
			if err != nil {
				continue
			}
		}
		count, _ := strconv.Atoi(strings.TrimSpace(row[1]))
		points = append(points, TrajectoryPoint{Timestamp: ts, Count: count})
	}
	return points, nil
}

func loadWeeklyInsight() (string, error) {
	path := filepath.Join(dataDir(), "weekly-insight.md")
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("no weekly insight yet — runs Sunday mornings via scheduled task")
	}
	return string(b), nil
}

func loadSecurity() ([]string, error) {
	path := filepath.Join(dataDir(), "security.txt")
	b, err := os.ReadFile(path)
	if err != nil || len(strings.TrimSpace(string(b))) == 0 {
		return nil, nil
	}
	var lines []string
	for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	return lines, nil
}

func loadCalendarEvents() ([]string, error) {
	path := filepath.Join(dataDir(), "calendar-events.txt")
	b, err := os.ReadFile(path)
	if err != nil || len(strings.TrimSpace(string(b))) == 0 {
		return nil, nil
	}
	var lines []string
	for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	return lines, nil
}

// Email reference helpers (for numbered ack from summary/urgent output)

func writeEmailRefs(emails []EmailRow) {
	path := filepath.Join(dataDir(), ".email_refs")
	f, err := os.Create(path)
	if err != nil {
		return
	}
	defer f.Close()
	for _, e := range emails {
		fmt.Fprintln(f, e.MessageID)
	}
}

func loadEmailRefs() []string {
	path := filepath.Join(dataDir(), ".email_refs")
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var refs []string
	for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			refs = append(refs, l)
		}
	}
	return refs
}

// Email table helpers

func upsertEmail(db *sqlx.DB, e EmailRow) {
	db.Exec(`INSERT INTO emails (message_id, thread_id, date, sender, sender_name, subject, snippet, labels, category, is_unread, synced_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(message_id) DO UPDATE SET
			subject=excluded.subject, snippet=excluded.snippet, labels=excluded.labels,
			category=excluded.category, is_unread=excluded.is_unread, synced_at=excluded.synced_at`,
		e.MessageID, e.ThreadID, e.Date, e.Sender, e.SenderName, e.Subject,
		e.Snippet, e.Labels, e.Category, e.IsUnread, e.SyncedAt,
	)
}

func getEmailsByCategory(db *sqlx.DB, category string) ([]EmailRow, error) {
	var rows []EmailRow
	err := db.Select(&rows,
		`SELECT * FROM emails WHERE category = ? AND is_unread = 1 ORDER BY date DESC`, category)
	return rows, err
}

func getUnreadEmails(db *sqlx.DB) ([]EmailRow, error) {
	var rows []EmailRow
	err := db.Select(&rows,
		`SELECT * FROM emails WHERE is_unread = 1 ORDER BY date DESC`)
	return rows, err
}

func getEmailsSince(db *sqlx.DB, since time.Time) ([]EmailRow, error) {
	sinceStr := since.Format(time.RFC3339)
	var rows []EmailRow
	err := db.Select(&rows,
		`SELECT * FROM emails WHERE is_unread = 1 AND synced_at >= ? ORDER BY synced_at DESC`, sinceStr)
	return rows, err
}

func getUnreadCountByCategory(db *sqlx.DB) (map[string]int, error) {
	type catCount struct {
		Category string `db:"category"`
		Count    int    `db:"count"`
	}
	var rows []catCount
	err := db.Select(&rows,
		`SELECT category, COUNT(*) as count FROM emails WHERE is_unread = 1 GROUP BY category ORDER BY count DESC`)
	if err != nil {
		return nil, err
	}
	m := make(map[string]int)
	for _, r := range rows {
		m[r.Category] = r.Count
	}
	return m, nil
}
