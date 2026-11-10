package validation

import (
	"bufio"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-playground/validator/v10"

	"github.com/jun-develop/junkit/console"
)

// DefaultBlocklistPath is the file path for loading disposable email domains. Set it before
// NewValidator when the working directory is read-only (e.g. under the app's storage dir).
var DefaultBlocklistPath = "storage/bootstrap/email_blocklist.conf"

// emailRegex and the patterns below back the custom validation rules.
var (
	emailRegex        = regexp.MustCompile(`^[a-zA-Z0-9.!#$%&'*+/=?^_` + "`" + `{|}~-]+@[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(?:\.[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$`)
	lowerRegex        = regexp.MustCompile(`[a-z]`)
	upperRegex        = regexp.MustCompile(`[A-Z]`)
	digitRegex        = regexp.MustCompile(`[0-9]`)
	specialRegex      = regexp.MustCompile(`[!@#$%^&*()_+\-=\[\]{};:'",.<>?/\\|]`)
	slugRegex         = regexp.MustCompile(`^[a-z0-9]+(?:[-.][a-z0-9]+)*$`)
	searchRegex       = regexp.MustCompile(`^[a-zA-Z0-9\s]+$`)
	alphanumDashRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
	phoneRegex        = regexp.MustCompile(`^(\+)?([0-9]){7,15}$`)
)

// dynamicDisposableDomains is the disposable email blocklist, guarded by dynamicDomainsMux.
var (
	dynamicDisposableDomains = make(map[string]bool)
	dynamicDomainsMux        sync.RWMutex
)

// LoadDisposableEmailsFromFile replaces the disposable email blocklist with the
// newline-delimited domains in path.
//
// Usage:
//
//	err := validation.LoadDisposableEmailsFromFile("blocklist.conf")
func LoadDisposableEmailsFromFile(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}

	defer file.Close()

	newDomains := make(map[string]bool)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		domain := strings.TrimSpace(scanner.Text())
		if domain != "" {
			newDomains[strings.ToLower(domain)] = true
		}
	}

	if err := scanner.Err(); err != nil {
		return err
	}

	dynamicDomainsMux.Lock()
	dynamicDisposableDomains = newDomains
	dynamicDomainsMux.Unlock()

	return nil
}

// IsDisposableEmail reports whether domain is in the disposable email blocklist (case-insensitive).
func IsDisposableEmail(domain string) bool {
	normalized := strings.ToLower(domain)

	dynamicDomainsMux.RLock()
	defer dynamicDomainsMux.RUnlock()

	return dynamicDisposableDomains[normalized]
}

// CheckMXRecord reports whether domain has at least one DNS MX record.
func CheckMXRecord(domain string) bool {
	mxs, err := net.LookupMX(domain)
	if err != nil {
		console.Warnf("validation: mx lookup for domain %s: %v", domain, err)
		return false
	}

	if len(mxs) == 0 {
		console.Warnf("validation: no mx records found for domain %s", domain)
		return false
	}

	return true
}

// ValidateEmail reports whether email has valid syntax, a non-disposable domain, and live MX
// records. The MX check does a DNS lookup.
func ValidateEmail(email string) bool {
	if !emailRegex.MatchString(email) {
		console.Warnf("validation: invalid email format")
		return false
	}

	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		console.Warnf("validation: invalid email parts: %d", len(parts))
		return false
	}

	domain := strings.ToLower(parts[1])
	if IsDisposableEmail(domain) {
		console.Warnf("validation: disposable email domain detected: %s", domain)
		return false
	}

	return CheckMXRecord(domain)
}

// customRule pairs a validator tag with its validation function.
type customRule struct {
	tag string
	fn  validator.Func
}

// RegisterCustomValidation registers the custom rules on v and loads the default blocklist,
// creating it from the seed list if missing.
//
// Usage:
//
//	validation.RegisterCustomValidation(v)
func RegisterCustomValidation(v *validator.Validate) {
	loadDefaultBlocklist()

	rules := []customRule{
		{"email_advanced", func(fl validator.FieldLevel) bool {
			return ValidateEmail(fl.Field().String())
		}},
		{"password_strong", validatePasswordStrong},
		{"slug", func(fl validator.FieldLevel) bool {
			return slugRegex.MatchString(fl.Field().String())
		}},
		{"search", func(fl validator.FieldLevel) bool {
			return searchRegex.MatchString(fl.Field().String())
		}},
		{"min_int", validateMinInt},
		{"max_int", validateMaxInt},
		{"file_ext", validateFileExt},
		{"alphanum_dash", func(fl validator.FieldLevel) bool {
			return alphanumDashRegex.MatchString(fl.Field().String())
		}},
		{"date", func(fl validator.FieldLevel) bool {
			_, err := time.Parse(time.DateOnly, fl.Field().String())
			return err == nil
		}},
		{"date_past", validateDatePast},
		{"phone_advanced", func(fl validator.FieldLevel) bool {
			return phoneRegex.MatchString(fl.Field().String())
		}},
	}

	for _, rule := range rules {
		if err := v.RegisterValidation(rule.tag, rule.fn); err != nil {
			console.Warnf("validation: register %s rule: %v", rule.tag, err)
		}
	}
}

// seedDisposableDomains is written to DefaultBlocklistPath when that file does not exist.
var seedDisposableDomains = []string{
	"10minutemail.co.za", "10minutemail.com", "10minutemail.net", "20minutemail.com",
	"anonaddy.com", "burnermail.io", "burnthespam.info", "cool.fr.nf", "deadaddress.com", "despam.it",
	"discard.email", "discardmail.com", "discardmail.de", "dispostable.com", "disposableinbox.com",
	"dodgeit.com", "dodgit.com", "dodgit.org", "dropmail.me", "e4ward.com",
	"emailondeck.com", "fake-mail.net", "fakeinbox.com", "fakemail.net", "fakemailgenerator.com",
	"getairmail.com", "getnada.com", "grr.la", "guerrillamail.biz", "guerrillamail.com",
	"guerrillamail.de", "guerrillamail.net", "guerrillamail.org", "incognitomail.com", "incognitomail.org",
	"inboxbear.com", "jetable.fr.nf", "mail-temporaire.fr", "mail.tm", "mailbox52.ga",
	"mailbox90.org", "mailbox92.biz", "mailboxxx.net", "maildrop.cc", "mailexpire.com",
	"mailforspam.com", "mailfreeonline.com", "mailhz.me", "mailimate.com", "mailin8r.com",
	"mailinator.com", "mailismagic.com", "mailnator.com", "mailnesia.com", "mailnull.com",
	"mailpoof.com", "mailscrap.com", "mailsucker.net", "mailtemp.info", "meltmail.com",
	"mintemail.com", "moakt.cc", "mohmal.com", "mohmal.im", "moncourrier.fr.nf",
	"monemail.fr.nf", "monmail.fr.nf", "mt2014.com", "mt2015.com", "mytemp.email",
	"mytrashmail.com", "no-spam.ws", "noclickemail.com", "nogmailspam.info", "nospamfor.us",
	"nowmymail.com", "objectmail.com", "onewaymail.com", "pokemail.net", "proxymail.eu",
	"rcpt.at", "rhyta.com", "safe-mail.net", "sendspamhere.com", "sharklasers.com",
	"shieldedmail.com", "shortmail.net", "skeefmail.com", "smellfear.com", "snakemail.com",
	"sneakemail.com", "sofort-mail.de", "spam.la", "spam4.me", "spamavert.com",
	"spamcannon.com", "spamcero.com", "spamcon.org", "spamcowboy.com", "spamday.com",
}

// loadDefaultBlocklist loads DefaultBlocklistPath, creating it with seed domains if missing.
func loadDefaultBlocklist() {
	if _, err := os.Stat(DefaultBlocklistPath); err != nil {
		if err := createSeedBlocklist(DefaultBlocklistPath); err != nil {
			console.Warnf("validation: create disposable email blocklist at %s: %v",
				DefaultBlocklistPath, err)
			return
		}

		console.Infof("validation: created disposable email blocklist at %s", DefaultBlocklistPath)
	}

	if err := LoadDisposableEmailsFromFile(DefaultBlocklistPath); err != nil {
		console.Warnf("validation: load disposable email blocklist: %v", err)
		return
	}

	console.Infof("validation: loaded disposable email blocklist from %s", DefaultBlocklistPath)
}

// createSeedBlocklist writes seedDisposableDomains to path, creating parent directories.
func createSeedBlocklist(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	return os.WriteFile(path, []byte(strings.Join(seedDisposableDomains, "\n")+"\n"), 0o644)
}

// validatePasswordStrong requires >= 8 characters with lower, upper, digit, and special chars.
func validatePasswordStrong(fl validator.FieldLevel) bool {
	password := fl.Field().String()
	if len(password) < 8 {
		return false
	}

	return lowerRegex.MatchString(password) &&
		upperRegex.MatchString(password) &&
		digitRegex.MatchString(password) &&
		specialRegex.MatchString(password)
}

// validateMinInt reports whether the integer field is >= the rule parameter.
func validateMinInt(fl validator.FieldLevel) bool {
	minVal, err := strconv.ParseInt(fl.Param(), 10, 64)
	if err != nil {
		return false
	}

	switch f := fl.Field(); {
	case f.CanInt():
		return f.Int() >= minVal
	case f.CanUint():
		return minVal <= 0 || f.Uint() >= uint64(minVal)
	}

	return false
}

// validateMaxInt reports whether the integer field is <= the rule parameter.
func validateMaxInt(fl validator.FieldLevel) bool {
	maxVal, err := strconv.ParseInt(fl.Param(), 10, 64)
	if err != nil {
		return false
	}

	switch f := fl.Field(); {
	case f.CanInt():
		return f.Int() <= maxVal
	case f.CanUint():
		return maxVal >= 0 && f.Uint() <= uint64(maxVal)
	}

	return false
}

// validateFileExt reports whether the file extension is in the space-separated rule parameter
// (case-insensitive).
func validateFileExt(fl validator.FieldLevel) bool {
	filename := fl.Field().String()

	allowedStr := fl.Param()
	if allowedStr == "" {
		return false
	}

	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(filename)), ".")
	for _, allowed := range strings.Fields(allowedStr) {
		if ext == strings.ToLower(allowed) {
			return true
		}
	}

	return false
}

// validateDatePast reports whether the YYYY-MM-DD field is before now.
func validateDatePast(fl validator.FieldLevel) bool {
	t, err := time.Parse(time.DateOnly, fl.Field().String())
	if err != nil {
		return false
	}

	return t.Before(time.Now())
}
