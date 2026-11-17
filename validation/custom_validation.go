package validation

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-playground/validator/v10"
)

// mxCacheTTL is how long a definitive mail-host lookup result is reused.
const mxCacheTTL = 10 * time.Minute

// mxCacheMax caps the lookup cache, whose keys come from user input.
const mxCacheMax = 10_000

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

// resolver is the part of *net.Resolver the email_advanced rule uses; tests swap in a fake.
type resolver interface {
	LookupMX(ctx context.Context, name string) ([]*net.MX, error)
	LookupHost(ctx context.Context, host string) ([]string, error)
}

// mxResult is a cached mail-host lookup result.
type mxResult struct {
	ok      bool
	expires time.Time
}

// emailChecker backs the email_advanced rule: syntax, disposable domain and mail host.
type emailChecker struct {
	blocklist map[string]bool
	resolver  resolver
	timeout   time.Duration

	mu    sync.Mutex
	cache map[string]mxResult
}

// newEmailChecker builds an emailChecker from opts, reading opts.BlocklistPath when set.
func newEmailChecker(opts Options) (*emailChecker, error) {
	blocklist, err := loadBlocklist(opts.BlocklistPath)
	if err != nil {
		return nil, err
	}

	return &emailChecker{
		blocklist: blocklist,
		resolver:  net.DefaultResolver,
		timeout:   opts.MXTimeout,
		cache:     make(map[string]mxResult),
	}, nil
}

// valid reports whether email has valid syntax, a non-disposable domain and a mail host.
func (c *emailChecker) valid(email string) bool {
	if !emailRegex.MatchString(email) {
		return false
	}

	// emailRegex allows exactly one "@".
	domain := strings.ToLower(email[strings.IndexByte(email, '@')+1:])

	// A dotless name or an IP literal would resolve via /etc/hosts or without DNS at all.
	if !strings.Contains(domain, ".") || net.ParseIP(domain) != nil || c.blocklist[domain] {
		return false
	}

	return c.hasMailHost(domain)
}

// hasMailHost reports whether domain can receive mail. A failed lookup (timeout, SERVFAIL) says
// nothing about the domain, so it accepts the address and is not cached.
func (c *emailChecker) hasMailHost(domain string) bool {
	now := time.Now()

	c.mu.Lock()
	cached, hit := c.cache[domain]
	c.mu.Unlock()

	if hit && now.Before(cached.expires) {
		return cached.ok
	}

	ok, definitive := c.lookup(domain)
	if !definitive {
		return true
	}

	c.mu.Lock()
	// shortcut: a full cache is dropped wholesale; switch to an LRU if lookups show up in latency.
	if len(c.cache) >= mxCacheMax {
		clear(c.cache)
	}

	c.cache[domain] = mxResult{ok: ok, expires: now.Add(mxCacheTTL)}
	c.mu.Unlock()

	return ok
}

// lookup checks domain for MX records, then for A/AAAA records (RFC 5321 implicit MX).
// definitive is false when DNS itself failed rather than answering "no such host / no records".
func (c *emailChecker) lookup(domain string) (ok, definitive bool) {
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()

	mxs, err := c.resolver.LookupMX(ctx, domain)
	if err == nil && len(mxs) > 0 {
		return true, true
	}

	if err != nil && !isNotFound(err) {
		return false, false
	}

	hosts, err := c.resolver.LookupHost(ctx, domain)
	if err != nil {
		return false, isNotFound(err)
	}

	return len(hosts) > 0, true
}

// isNotFound reports whether err is a definitive DNS "no such host / no records" answer.
func isNotFound(err error) bool {
	var dnsErr *net.DNSError

	return errors.As(err, &dnsErr) && dnsErr.IsNotFound
}

// loadBlocklist reads the newline-delimited disposable domains in path, or returns the seed
// list when path is empty.
func loadBlocklist(path string) (map[string]bool, error) {
	domains := make(map[string]bool)
	if path == "" {
		for _, domain := range seedDisposableDomains {
			domains[domain] = true
		}

		return domains, nil
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("validation: open blocklist: %w", err)
	}

	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		if domain := strings.TrimSpace(scanner.Text()); domain != "" {
			domains[strings.ToLower(domain)] = true
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("validation: read blocklist: %w", err)
	}

	return domains, nil
}

// customRule pairs a validator tag with its validation function.
type customRule struct {
	tag string
	fn  validator.Func
}

// RegisterCustomValidation registers the custom rules (email_advanced, password_strong, slug,
// search, min_int, max_int, file_ext, alphanum_dash, date, date_past, phone_advanced) on engine.
// NewValidator already registers them on gin's binding engine; call this for another engine.
//
// Usage:
//
//	err := v.RegisterCustomValidation(validator.New())
func (v *Validator) RegisterCustomValidation(engine *validator.Validate) error {
	rules := []customRule{
		{"email_advanced", func(fl validator.FieldLevel) bool {
			return v.email.valid(fl.Field().String())
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
		if err := engine.RegisterValidation(rule.tag, rule.fn); err != nil {
			return fmt.Errorf("validation: register %s rule: %w", rule.tag, err)
		}
	}

	return nil
}

// seedDisposableDomains is the blocklist used when Options.BlocklistPath is empty.
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
