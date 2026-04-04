package main

import "strings"

// Merchant categorization heuristics — order matters, first match wins
var merchantRules = []struct {
	Keywords []string
	Category string
}{
	// Travel & Hotels
	{[]string{"HILTON", "MARRIOTT", "HYATT", "HOLIDAY INN", "BEST WESTERN", "HAMPTON INN",
		"DOUBLETREE", "EMBASSY SUITES", "COURTYARD", "FAIRFIELD", "RESIDENCE INN",
		"KENDALL HOTEL", "AIRBNB"}, "Travel"},
	{[]string{"UNITED AIRLI", "DELTA INTERNET", "DELTA AIR", "SOUTHWEST AIR", "AMERICAN AIR",
		"JETBLUE", "SPIRIT AIR", "FRONTIER AIR", "ALASKA AIR", "DFS DANIEL", "AIRPORT"}, "Travel"},
	{[]string{"ENTERPRISE RENT", "HERTZ", "AVIS", "BUDGET RENT", "NATIONAL CAR"}, "Travel"},

	// Kids / Education / Activities
	{[]string{"STEAM SCHOOL", "CHESTNUT FORKS", "ROBLOX", "SP CRUNCHLABS", "PRODIGYGAME",
		"TEBEX.ORG", "STEAMGAMES"}, "Kids"},

	// Pets
	{[]string{"CHEWY.COM", "PETCO", "PETSMART", "VET ", "MEDVET", "CARING HANDS ANIMAL",
		"BANFIELD"}, "Pets"},

	// Insurance / Mortgage / Loans
	{[]string{"ESURANCE", "GEICO", "PROGRESSIVE", "ALLSTATE", "STATE FARM", "USAA"}, "Insurance"},
	{[]string{"ROCKET MORTGAGE", "QUICKEN LOANS"}, "Housing"},

	// Auto / Transport
	{[]string{"SHELL", "EXXON", "BP OIL", "BP#", "BP GAS", "CHEVRON", "WAWA GAS", "SHEETZ", "AUTOZONE",
		"JIFFY LUBE", "CARWASH", "VALVOLINE", "OIL CHANGE", "TIRE", "NAPA AUTO",
		"EZPASSVA", "EZPASS", "CEDAR RUN BP"}, "Auto/Transport"},
	{[]string{"UBER", "UBR*", "LYFT"}, "Auto/Transport"},

	// Internet / Utilities / Home Services
	{[]string{"COMCAST", "XFINITY", "VERIZON", "T-MOBILE", "AT&T", "SPECTRUM",
		"GFL - ENV", "GFL ENV", "TOWN OF WARRENTON"}, "Utilities"},

	// Amazon
	{[]string{"AMAZON", "AMZN"}, "Amazon"},

	// TikTok shopping
	{[]string{"TIKTOK"}, "Online Shopping"},

	// Restaurants / Food
	{[]string{"DOORDASH", "UBEREATS", "UBER EATS", "GRUBHUB", "QUICKWAY", "HIBACHI", "PIZZA",
		"BURGER", "TACO", "CHIPOTLE", "STARBUCKS", "DUNKIN", "MCDONALDS", "WENDYS",
		"CHICK-FIL", "PANERA", "SUBWAY", "RESTAURANT", "DINER", "CAFE", "GRILL",
		"SUSHI", "THAI", "CHINESE", "INDIAN", "MEXICAN", "BBQ", "WINGSTOP",
		"POPEYES", "KFC", "FIVE GUYS", "CAVA", "SWEETGREEN", "NOODLES",
		"JERSEY MIKE", "CJS NEW YORK", "REDZONE BAR", "TST*", "FARM BREWERY",
		"LA COLOMBE", "POWERS FARM", "ABC STORE", "VA ABC"}, "Restaurants"},

	// Furniture
	{[]string{"LA-Z-BOY", "IKEA", "WAYFAIR", "POTTERY BARN", "WEST ELM", "ASHLEY",
		"ROOMS TO GO", "CRATE", "BARREL", "RESTORATION HARDWARE", "CB2"}, "Furniture"},

	// Subscriptions / Digital
	{[]string{"GOOGLE", "OPENAI", "CHATGPT", "CLAUDE", "ANTHROPIC", "NETFLIX", "SPOTIFY",
		"HULU", "DISNEY", "APPLE.COM", "APPLE", "MICROSOFT", "ADOBE", "DROPBOX", "NOTION",
		"GITHUB", "YOUTUBE", "HBO", "PARAMOUNT", "PEACOCK", "CRUNCHYROLL",
		"AUDIBLE", "GRAMMARLY", "INTUIT"}, "Subscriptions"},

	// Groceries
	{[]string{"WALMART", "TARGET", "COSTCO", "KROGER", "SAFEWAY", "ALDI", "LIDL",
		"TRADER JOE", "WHOLE FOODS", "HARRIS TEETER", "GIANT FOOD", "FOOD LION",
		"WEGMANS", "PUBLIX", "SAM'S CLUB", "BJ'S", "HY-VEE"}, "Groceries"},

	// Home / Farm / Garden
	{[]string{"LOWES", "HOME DEPOT", "MENARDS", "ACE HARDWARE", "TRUE VALUE",
		"SHERWIN", "BENJAMIN MOORE", "TRACTOR-SUPPLY", "TRACTOR SUPPLY",
		"AGRI SUPPLY", "JOHNNY'S SELECTED", "MEADOWS FARMS", "CFC FARM",
		"BELVEDERE PLANTAT"}, "Home/Garden"},

	// Health / Medical
	{[]string{"CVS", "WALGREENS", "RITE AID", "PHARMACY", "MEDICAL", "DOCTOR",
		"HOSPITAL", "URGENT CARE", "DENTAL", "OPTOM", "VISION",
		"UROLOGY", "INOVA", "UVA HS", "COLUMBIA ASSOCIATES"}, "Health"},

	// Electronics / Tech
	{[]string{"NEWEGG", "BEST BUY", "MICRO CENTER", "B&H PHOTO", "EBAY"}, "Electronics"},

	// Sports / Recreation
	{[]string{"BLUE RIDGE CYCLERY", "BIKE", "CYCLING", "REI ", "DICK'S SPORT"}, "Recreation"},

	// Shipping
	{[]string{"USPS", "UPS STORE", "FEDEX", "DHL"}, "Shipping"},

	// Personal care
	{[]string{"SQ *WENDY NICHOLE"}, "Personal Care"},
}

func categorize(merchant string) string {
	upper := strings.ToUpper(strings.TrimSpace(merchant))
	for _, rule := range merchantRules {
		for _, kw := range rule.Keywords {
			if strings.Contains(upper, kw) {
				return rule.Category
			}
		}
	}
	return "Other"
}

// aliasRules maps raw merchant substrings (uppercase) to a canonical display name.
// Order matters — first match wins. Applied at ingest time and during recategorize.
var aliasRules = []struct {
	Keywords []string
	Alias    string
}{
	{[]string{"AMAZON MKTPLACE", "AMAZON.COM", "AMZN MKTP", "AMAZON TIP", "AMAZON PRIME"}, "Amazon"},
	{[]string{"COMCAST", "XFINITY"}, "Comcast/Xfinity"},
	{[]string{"CHICK-FIL-A"}, "Chick-fil-A"},
	{[]string{"DOORDASH"}, "DoorDash"},
	{[]string{"UBER EATS", "UBEREATS"}, "Uber Eats"},
	{[]string{"APPLE.COM/BILL"}, "Apple"},
	{[]string{"GOOGLE *YOUTUBEPREMIUM", "GOOGLE *YOUTUBE"}, "YouTube Premium"},
	{[]string{"GOOGLE *FI", "GOOGLE *GOOGLE FI"}, "Google Fi"},
	{[]string{"GOOGLE *TV"}, "Google TV"},
	{[]string{"OPENAI *CHATGPT", "OPENAI"}, "OpenAI"},
	{[]string{"EZPASSVA", "EZPASS"}, "EZPass VA"},
	{[]string{"BP#", "CEDAR RUN BP"}, "BP Gas"},
	{[]string{"TRACTOR-SUPPLY", "TRACTOR SUPPLY"}, "Tractor Supply"},
}

// normalizeMerchant returns a canonical merchant name if a known alias matches,
// otherwise returns the original string unchanged.
func normalizeMerchant(merchant string) string {
	upper := strings.ToUpper(strings.TrimSpace(merchant))
	for _, rule := range aliasRules {
		for _, kw := range rule.Keywords {
			if strings.Contains(upper, kw) {
				return rule.Alias
			}
		}
	}
	return merchant
}
