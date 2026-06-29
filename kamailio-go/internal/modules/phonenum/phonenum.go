// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * phonenum module - phone number parsing and validation.
 * Port of the kamailio phonenum module (src/modules/phonenum).
 *
 * The original C module wraps libphonenumber to parse, validate and
 * format E.164 phone numbers. This Go counterpart provides a simplified
 * implementation that handles common E.164 and national formats using a
 * small region table, so it can be used without the native library.
 *
 * It is safe for concurrent use.
 */

package phonenum

import (
	"fmt"
	"strings"
	"sync"
)

// PhoneNumberInfo holds the parsed components of a phone number.
type PhoneNumberInfo struct {
	Number         string
	CountryCode    string
	NationalNumber string
	Type           string
	IsValid        string
	Carrier        string
	Location       string
}

// regionInfo describes the dialling metadata for a region.
type regionInfo struct {
	CountryCode string
	NationalLen int
}

// PhoneNumModule parses, validates and formats phone numbers.
// It is the Go counterpart of the kamailio phonenum module.
type PhoneNumModule struct {
	mu      sync.RWMutex
	regions map[string]regionInfo
}

// New creates a PhoneNumModule.
func New() *PhoneNumModule {
	m := &PhoneNumModule{regions: make(map[string]regionInfo)}
	m.loadRegions()
	return m
}

// loadRegions populates a small region table used for parsing.
func (m *PhoneNumModule) loadRegions() {
	m.regions["US"] = regionInfo{CountryCode: "1", NationalLen: 10}
	m.regions["GB"] = regionInfo{CountryCode: "44", NationalLen: 10}
	m.regions["DE"] = regionInfo{CountryCode: "49", NationalLen: 11}
	m.regions["FR"] = regionInfo{CountryCode: "33", NationalLen: 9}
	m.regions["CN"] = regionInfo{CountryCode: "86", NationalLen: 11}
}

// Parse parses number in the context of defaultRegion and returns its
// components. The default region is used when the number is not in
// international (E.164) form.
//
//	C: phonenum_parse()
func (m *PhoneNumModule) Parse(number, defaultRegion string) (*PhoneNumberInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	digits := digitsOnly(number)
	if digits == "" {
		return nil, fmt.Errorf("phonenum: empty number")
	}
	info := &PhoneNumberInfo{}
	if strings.HasPrefix(number, "+") || strings.HasPrefix(number, "00") {
		cc, national, ok := m.splitInternational(digits)
		if !ok {
			return nil, fmt.Errorf("phonenum: cannot parse international number %q", number)
		}
		info.CountryCode = cc
		info.NationalNumber = national
	} else {
		region := strings.ToUpper(defaultRegion)
		ri, ok := m.regions[region]
		if !ok {
			return nil, fmt.Errorf("phonenum: unknown region %q", defaultRegion)
		}
		info.CountryCode = ri.CountryCode
		info.NationalNumber = digits
	}
	info.Number = "+" + info.CountryCode + info.NationalNumber
	info.Type = m.classify(info.NationalNumber)
	if m.isValid(info) {
		info.IsValid = "true"
	} else {
		info.IsValid = "false"
	}
	return info, nil
}

// IsValid reports whether number is valid for the given region.
//
//	C: phonenum_is_valid()
func (m *PhoneNumModule) IsValid(number, region string) bool {
	info, err := m.Parse(number, region)
	if err != nil {
		return false
	}
	return info.IsValid == "true"
}

// GetCountry returns the country code (dialling) for number.
//
//	C: phonenum_cc()
func (m *PhoneNumModule) GetCountry(number string) (string, error) {
	info, err := m.Parse(number, "US")
	if err != nil {
		return "", err
	}
	return info.CountryCode, nil
}

// GetType returns the type of number (mobile/fixed/unknown).
//
//	C: phonenum_type()
func (m *PhoneNumModule) GetType(number string) (string, error) {
	info, err := m.Parse(number, "US")
	if err != nil {
		return "", err
	}
	return info.Type, nil
}

// Format returns number reformatted according to format. Supported
// formats are "e164" (+CCNN), "national" (NN) and "international"
// (+CC NN).
//
//	C: phonenum_format()
func (m *PhoneNumModule) Format(number, format string) (string, error) {
	info, err := m.Parse(number, "US")
	if err != nil {
		return "", err
	}
	switch strings.ToLower(format) {
	case "e164":
		return "+" + info.CountryCode + info.NationalNumber, nil
	case "national":
		return info.NationalNumber, nil
	case "international":
		return "+" + info.CountryCode + " " + info.NationalNumber, nil
	default:
		return "", fmt.Errorf("phonenum: unknown format %q", format)
	}
}

// --- helpers ---

// splitInternational splits an international (00 or + stripped) digit
// string into country code and national number.
func (m *PhoneNumModule) splitInternational(digits string) (string, string, bool) {
	for _, ri := range m.regions {
		if strings.HasPrefix(digits, ri.CountryCode) {
			national := digits[len(ri.CountryCode):]
			if len(national) == ri.NationalLen {
				return ri.CountryCode, national, true
			}
		}
	}
	return "", "", false
}

// isValid reports whether the parsed info has a plausible national length.
func (m *PhoneNumModule) isValid(info *PhoneNumberInfo) bool {
	for _, ri := range m.regions {
		if ri.CountryCode == info.CountryCode {
			return len(info.NationalNumber) == ri.NationalLen
		}
	}
	return false
}

// classify guesses the number type from its leading digits.
func (m *PhoneNumModule) classify(national string) string {
	if national == "" {
		return "unknown"
	}
	switch national[0] {
	case '1', '2', '3', '4', '5':
		return "fixed"
	case '6', '7', '8', '9':
		return "mobile"
	default:
		return "unknown"
	}
}

// digitsOnly strips everything except digits from s.
func digitsOnly(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// --- package-level API ---

var defaultModule = New()

// DefaultPhoneNum returns the package-level default PhoneNumModule.
func DefaultPhoneNum() *PhoneNumModule {
	return defaultModule
}

// Init (re)initialises the package-level default module.
func Init() {
	defaultModule = New()
}
