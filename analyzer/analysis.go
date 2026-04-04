package main

import (
	"math"
	"sort"
	"time"
)

// Velocity calculates a rolling average of daily spending
type VelocityPoint struct {
	Date    string
	DayAvg  float64 // rolling window average
	DayRaw  float64 // raw daily total
}

func calcVelocity(dailies []DailyAgg, windowDays int) []VelocityPoint {
	if len(dailies) == 0 {
		return nil
	}

	// Fill in all days (including zeros)
	dayMap := map[string]float64{}
	for _, d := range dailies {
		dayMap[d.Day] = d.Total
	}

	start, _ := time.Parse("2006-01-02", dailies[0].Day)
	end, _ := time.Parse("2006-01-02", dailies[len(dailies)-1].Day)

	var allDays []struct {
		Date  string
		Total float64
	}
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		ds := d.Format("2006-01-02")
		allDays = append(allDays, struct {
			Date  string
			Total float64
		}{ds, dayMap[ds]})
	}

	// Rolling average
	var result []VelocityPoint
	for i := range allDays {
		windowStart := i - windowDays + 1
		if windowStart < 0 {
			windowStart = 0
		}
		var sum float64
		for j := windowStart; j <= i; j++ {
			sum += allDays[j].Total
		}
		avg := sum / float64(i-windowStart+1)
		result = append(result, VelocityPoint{
			Date:   allDays[i].Date,
			DayAvg: avg,
			DayRaw: allDays[i].Total,
		})
	}
	return result
}

// Hotspot identifies days where spending exceeds mean + N*stddev
type Hotspot struct {
	Date    string
	Amount  float64
	Sigma   float64 // how many std devs above mean
	TopItem string  // biggest merchant that day
}

func findHotspots(dailies []DailyAgg, txs []TxRow, sigmaThreshold float64) []Hotspot {
	if len(dailies) < 5 {
		return nil
	}

	var sum, sumSq float64
	for _, d := range dailies {
		sum += d.Total
		sumSq += d.Total * d.Total
	}
	n := float64(len(dailies))
	mean := sum / n
	stddev := math.Sqrt(sumSq/n - mean*mean)
	if stddev == 0 {
		return nil
	}

	threshold := mean + sigmaThreshold*stddev

	// Index transactions by day
	txByDay := map[string][]TxRow{}
	for _, tx := range txs {
		d := tx.Date[:10]
		txByDay[d] = append(txByDay[d], tx)
	}

	var spots []Hotspot
	for _, d := range dailies {
		if d.Total > threshold {
			sigma := (d.Total - mean) / stddev
			topMerch := ""
			topAmt := 0.0
			for _, tx := range txByDay[d.Day] {
				if tx.Amount > topAmt {
					topAmt = tx.Amount
					topMerch = tx.Merchant
				}
			}
			spots = append(spots, Hotspot{
				Date:    d.Day,
				Amount:  d.Total,
				Sigma:   sigma,
				TopItem: topMerch,
			})
		}
	}

	sort.Slice(spots, func(i, j int) bool { return spots[i].Amount > spots[j].Amount })
	return spots
}

// SpendingSession represents a cluster of purchases in a short time window
type SpendingSession struct {
	Date       string
	TxCount    int
	Total      float64
	Categories []string
	Merchants  []string
	Type       string // "spree", "routine", "normal"
}

func detectSpendingSessions(txs []TxRow, windowHours float64, minTxCount int) []SpendingSession {
	if len(txs) == 0 {
		return nil
	}

	// Group by day (we don't have exact times, so use day as proxy)
	byDay := map[string][]TxRow{}
	for _, tx := range txs {
		d := tx.Date[:10]
		byDay[d] = append(byDay[d], tx)
	}

	var sessions []SpendingSession
	for day, dayTxs := range byDay {
		if len(dayTxs) < minTxCount {
			continue
		}

		var total float64
		catSet := map[string]bool{}
		merchSet := map[string]bool{}
		for _, tx := range dayTxs {
			total += tx.Amount
			catSet[tx.Category] = true
			merchSet[tx.Merchant] = true
		}

		var cats, merchs []string
		for c := range catSet {
			cats = append(cats, c)
		}
		for m := range merchSet {
			merchs = append(merchs, m)
		}

		sessType := "normal"
		if len(dayTxs) >= 5 {
			sessType = "spree"
		} else if len(catSet) == 1 && cats[0] == "Amazon" {
			sessType = "spree"
		}

		sessions = append(sessions, SpendingSession{
			Date:       day,
			TxCount:    len(dayTxs),
			Total:      total,
			Categories: cats,
			Merchants:  merchs,
			Type:       sessType,
		})
	}

	sort.Slice(sessions, func(i, j int) bool { return sessions[i].Total > sessions[j].Total })
	return sessions
}

// RecurringCharge represents a detected recurring/subscription pattern
type RecurringCharge struct {
	Merchant      string
	Category      string
	AvgAmount     float64
	FreqDays      int
	Occurrences   int
	FirstSeen     string
	LastSeen      string
	TotalSpent    float64
	IsActive      bool
	MonthlyCost   float64 // normalized to monthly
	PriceChanged  bool
	AmountHistory []float64
}

func detectRecurring(txs []TxRow, minOccurrences int) []RecurringCharge {
	// Group by merchant
	byMerch := map[string][]TxRow{}
	for _, tx := range txs {
		byMerch[tx.Merchant] = append(byMerch[tx.Merchant], tx)
	}

	var result []RecurringCharge
	for merchant, mTxs := range byMerch {
		if len(mTxs) < minOccurrences {
			continue
		}

		// Sort by date
		sort.Slice(mTxs, func(i, j int) bool { return mTxs[i].Date < mTxs[j].Date })

		// Calculate intervals between purchases
		var intervals []int
		var amounts []float64
		var totalSpent float64
		for i, tx := range mTxs {
			amounts = append(amounts, tx.Amount)
			totalSpent += tx.Amount
			if i > 0 {
				d1, _ := time.Parse("2006-01-02", mTxs[i-1].Date[:10])
				d2, _ := time.Parse("2006-01-02", tx.Date[:10])
				days := int(d2.Sub(d1).Hours() / 24)
				if days > 0 {
					intervals = append(intervals, days)
				}
			}
		}

		if len(intervals) == 0 {
			continue
		}

		// Minimum span guard: first→last must be at least 45 days
		// This eliminates hotel stays, burst same-day purchases, and short travel patterns
		firstDate, _ := time.Parse("2006-01-02", mTxs[0].Date[:10])
		lastDate0, _ := time.Parse("2006-01-02", mTxs[len(mTxs)-1].Date[:10])
		spanDays := int(lastDate0.Sub(firstDate).Hours() / 24)
		if spanDays < 45 {
			continue
		}

		// Median interval
		sortedIntervals := make([]int, len(intervals))
		copy(sortedIntervals, intervals)
		sort.Ints(sortedIntervals)
		medianInterval := sortedIntervals[len(sortedIntervals)/2]

		// Check if intervals are somewhat regular (coefficient of variation < 0.5)
		var intSum float64
		for _, iv := range intervals {
			intSum += float64(iv)
		}
		intMean := intSum / float64(len(intervals))
		var intVarSum float64
		for _, iv := range intervals {
			diff := float64(iv) - intMean
			intVarSum += diff * diff
		}
		cv := math.Sqrt(intVarSum/float64(len(intervals))) / intMean

		// Amount coefficient of variation — subscriptions have stable prices
		var amtSum float64
		for _, a := range amounts {
			amtSum += a
		}
		amtMean := amtSum / float64(len(amounts))
		var amtVarSum float64
		for _, a := range amounts {
			diff := a - amtMean
			amtVarSum += diff * diff
		}
		amtCV := math.Sqrt(amtVarSum/float64(len(amounts))) / amtMean

		// Subscription detection heuristics:
		// 1. Interval regularity: CV < 0.4 for timing
		// 2. Amount stability: CV < 0.3 for price (subs have fixed prices)
		// 3. OR: very frequent (< 7 day interval) with stable amounts
		isSubscription := false
		if amtCV < 0.15 && cv < 0.5 {
			isSubscription = true // very stable price + somewhat regular = subscription
		} else if amtCV < 0.05 && medianInterval >= 20 {
			isSubscription = true // exact same price each time = subscription
		} else if cv < 0.25 && medianInterval >= 25 {
			isSubscription = true // very regular timing = subscription even if price varies slightly
		}

		if !isSubscription {
			continue
		}

		// Average amount (reuse amtMean from above)
		avgAmt := amtMean

		// Monthly cost normalization
		monthlyCost := avgAmt * 30.0 / float64(medianInterval)

		// Price stability check
		priceChanged := false
		if len(amounts) >= 3 {
			first := amounts[0]
			last := amounts[len(amounts)-1]
			if math.Abs(last-first)/first > 0.1 {
				priceChanged = true
			}
		}

		// Active check: last seen within 2x the median interval
		daysSinceLast := int(time.Since(lastDate0).Hours() / 24)
		isActive := daysSinceLast < medianInterval*2+14

		result = append(result, RecurringCharge{
			Merchant:      merchant,
			Category:      mTxs[0].Category,
			AvgAmount:     avgAmt,
			FreqDays:      medianInterval,
			Occurrences:   len(mTxs),
			FirstSeen:     mTxs[0].Date[:10],
			LastSeen:      mTxs[len(mTxs)-1].Date[:10],
			TotalSpent:    totalSpent,
			IsActive:      isActive,
			MonthlyCost:   monthlyCost,
			PriceChanged:  priceChanged,
			AmountHistory: amounts,
		})
	}

	sort.Slice(result, func(i, j int) bool { return result[i].MonthlyCost > result[j].MonthlyCost })
	return result
}

// ValueScore for a transaction: 0 (essential) to 100 (impulse)
func scoreImpulse(tx TxRow, sameDayCount int) int {
	score := 0

	// Category-based baseline
	switch tx.Category {
	case "Groceries", "Auto/Transport", "Health", "Home/Garden", "Housing", "Insurance":
		score += 5 // essential
	case "Subscriptions":
		score += 15
	case "Restaurants":
		score += 35
	case "Amazon":
		score += 40
	case "Furniture":
		score += 20 // usually intentional
	default:
		score += 25
	}

	// Amount factor: very small purchases more impulsive
	if tx.Amount < 20 {
		score += 15
	} else if tx.Amount < 50 {
		score += 10
	} else if tx.Amount > 500 {
		score -= 10 // big purchases are usually planned
	}

	// Same-day clustering: more purchases = more impulsive
	if sameDayCount >= 5 {
		score += 25
	} else if sameDayCount >= 3 {
		score += 15
	} else if sameDayCount >= 2 {
		score += 5
	}

	// Clamp
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}
	return score
}

// ValueBucket classifies a score
func valueBucket(score int) string {
	if score <= 20 {
		return "essential"
	}
	if score <= 45 {
		return "intentional"
	}
	return "impulsive"
}
