package settings

import (
	"slices"

	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
)

// CurrencyCodes is what the Currency and Display in dropdowns offer: the codes
// open.er-api.com, the service behind FetchRate, returns rates for. Taken from
// that service rather than from golang.org/x/text/currency, whose 2017 table
// lists retired codes such as VEF and lacks live ones such as VES and ZWG, so
// that every choice here can actually be converted.
var CurrencyCodes = []string{
	"AED", "AFN", "ALL", "AMD", "ANG", "AOA", "ARS", "AUD", "AWG", "AZN", "BAM", "BBD",
	"BDT", "BGN", "BHD", "BIF", "BMD", "BND", "BOB", "BRL", "BSD", "BTN", "BWP", "BYN",
	"BZD", "CAD", "CDF", "CHF", "CLF", "CLP", "CNH", "CNY", "COP", "CRC", "CUP", "CVE",
	"CZK", "DJF", "DKK", "DOP", "DZD", "EGP", "ERN", "ETB", "EUR", "FJD", "FKP", "FOK",
	"GBP", "GEL", "GGP", "GHS", "GIP", "GMD", "GNF", "GTQ", "GYD", "HKD", "HNL", "HRK",
	"HTG", "HUF", "IDR", "ILS", "IMP", "INR", "IQD", "IRR", "ISK", "JEP", "JMD", "JOD",
	"JPY", "KES", "KGS", "KHR", "KID", "KMF", "KRW", "KWD", "KYD", "KZT", "LAK", "LBP",
	"LKR", "LRD", "LSL", "LYD", "MAD", "MDL", "MGA", "MKD", "MMK", "MNT", "MOP", "MRU",
	"MUR", "MVR", "MWK", "MXN", "MYR", "MZN", "NAD", "NGN", "NIO", "NOK", "NPR", "NZD",
	"OMR", "PAB", "PEN", "PGK", "PHP", "PKR", "PLN", "PYG", "QAR", "RON", "RSD", "RUB",
	"RWF", "SAR", "SBD", "SCR", "SDG", "SEK", "SGD", "SHP", "SLE", "SLL", "SOS", "SRD",
	"SSP", "STN", "SYP", "SZL", "THB", "TJS", "TMT", "TND", "TOP", "TRY", "TTD", "TVD",
	"TWD", "TZS", "UAH", "UGX", "USD", "UYU", "UZS", "VES", "VND", "VUV", "WST", "XAF",
	"XCD", "XCG", "XDR", "XOF", "XPF", "YER", "ZAR", "ZMW", "ZWG", "ZWL",
}

// currencyPicker is a search dropdown of every code with saved selected. A saved
// value that is not on the list, typed in before this was a dropdown, joins it
// so that opening Settings and saving cannot change it.
func currencyPicker(saved string) *shell.GoldSelect {
	code := backend.CurrencyCode(saved)
	options := CurrencyCodes
	if !slices.Contains(options, code) {
		options = slices.Clone(options)
		options = append(options, code)
		slices.Sort(options)
	}
	picker := shell.NewGoldSelect(options, nil)
	picker.Searchable = true
	picker.SetSelected(code)
	return picker
}
