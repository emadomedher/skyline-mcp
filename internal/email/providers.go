package email

import (
	"context"
	"net"
	"strings"
	"time"
)

// ProviderConfig holds known SMTP/IMAP/POP3 settings for a recognized email provider.
type ProviderConfig struct {
	Name     string
	SMTPHost string
	SMTPPort int
	SMTPTLS  string
	IMAPHost string
	IMAPPort int
	POP3Host string
	POP3Port int
}

// knownProviders maps domain suffixes to their mail server configurations.
// These are the most common providers where we can skip manual configuration.
var knownProviders = map[string]ProviderConfig{
	// Google
	"gmail.com": {
		Name: "Gmail", SMTPHost: "smtp.gmail.com", SMTPPort: 587, SMTPTLS: "starttls",
		IMAPHost: "imap.gmail.com", IMAPPort: 993, POP3Host: "pop.gmail.com", POP3Port: 995,
	},
	"googlemail.com": {
		Name: "Gmail", SMTPHost: "smtp.gmail.com", SMTPPort: 587, SMTPTLS: "starttls",
		IMAPHost: "imap.gmail.com", IMAPPort: 993, POP3Host: "pop.gmail.com", POP3Port: 995,
	},
	// Microsoft
	"outlook.com": {
		Name: "Outlook", SMTPHost: "smtp-mail.outlook.com", SMTPPort: 587, SMTPTLS: "starttls",
		IMAPHost: "outlook.office365.com", IMAPPort: 993, POP3Host: "outlook.office365.com", POP3Port: 995,
	},
	"hotmail.com": {
		Name: "Outlook", SMTPHost: "smtp-mail.outlook.com", SMTPPort: 587, SMTPTLS: "starttls",
		IMAPHost: "outlook.office365.com", IMAPPort: 993, POP3Host: "outlook.office365.com", POP3Port: 995,
	},
	"live.com": {
		Name: "Outlook", SMTPHost: "smtp-mail.outlook.com", SMTPPort: 587, SMTPTLS: "starttls",
		IMAPHost: "outlook.office365.com", IMAPPort: 993, POP3Host: "outlook.office365.com", POP3Port: 995,
	},
	// Microsoft 365 / Office 365
	"office365.com": {
		Name: "Microsoft 365", SMTPHost: "smtp.office365.com", SMTPPort: 587, SMTPTLS: "starttls",
		IMAPHost: "outlook.office365.com", IMAPPort: 993, POP3Host: "outlook.office365.com", POP3Port: 995,
	},
	// Yahoo
	"yahoo.com": {
		Name: "Yahoo", SMTPHost: "smtp.mail.yahoo.com", SMTPPort: 587, SMTPTLS: "starttls",
		IMAPHost: "imap.mail.yahoo.com", IMAPPort: 993, POP3Host: "pop.mail.yahoo.com", POP3Port: 995,
	},
	"yahoo.co.uk": {
		Name: "Yahoo UK", SMTPHost: "smtp.mail.yahoo.com", SMTPPort: 587, SMTPTLS: "starttls",
		IMAPHost: "imap.mail.yahoo.com", IMAPPort: 993, POP3Host: "pop.mail.yahoo.com", POP3Port: 995,
	},
	// iCloud / Apple
	"icloud.com": {
		Name: "iCloud", SMTPHost: "smtp.mail.me.com", SMTPPort: 587, SMTPTLS: "starttls",
		IMAPHost: "imap.mail.me.com", IMAPPort: 993,
	},
	"me.com": {
		Name: "iCloud", SMTPHost: "smtp.mail.me.com", SMTPPort: 587, SMTPTLS: "starttls",
		IMAPHost: "imap.mail.me.com", IMAPPort: 993,
	},
	"mac.com": {
		Name: "iCloud", SMTPHost: "smtp.mail.me.com", SMTPPort: 587, SMTPTLS: "starttls",
		IMAPHost: "imap.mail.me.com", IMAPPort: 993,
	},
	// ProtonMail (Bridge required)
	"protonmail.com": {
		Name: "ProtonMail", SMTPHost: "127.0.0.1", SMTPPort: 1025, SMTPTLS: "starttls",
		IMAPHost: "127.0.0.1", IMAPPort: 1143,
	},
	"proton.me": {
		Name: "ProtonMail", SMTPHost: "127.0.0.1", SMTPPort: 1025, SMTPTLS: "starttls",
		IMAPHost: "127.0.0.1", IMAPPort: 1143,
	},
	// Zoho
	"zoho.com": {
		Name: "Zoho", SMTPHost: "smtp.zoho.com", SMTPPort: 587, SMTPTLS: "starttls",
		IMAPHost: "imap.zoho.com", IMAPPort: 993, POP3Host: "pop.zoho.com", POP3Port: 995,
	},
	// FastMail
	"fastmail.com": {
		Name: "FastMail", SMTPHost: "smtp.fastmail.com", SMTPPort: 587, SMTPTLS: "starttls",
		IMAPHost: "imap.fastmail.com", IMAPPort: 993, POP3Host: "pop.fastmail.com", POP3Port: 995,
	},
	// AOL
	"aol.com": {
		Name: "AOL", SMTPHost: "smtp.aol.com", SMTPPort: 587, SMTPTLS: "starttls",
		IMAPHost: "imap.aol.com", IMAPPort: 993, POP3Host: "pop.aol.com", POP3Port: 995,
	},
	// GMX
	"gmx.com": {
		Name: "GMX", SMTPHost: "smtp.gmx.com", SMTPPort: 587, SMTPTLS: "starttls",
		IMAPHost: "imap.gmx.com", IMAPPort: 993, POP3Host: "pop.gmx.com", POP3Port: 995,
	},
	"gmx.net": {
		Name: "GMX", SMTPHost: "smtp.gmx.net", SMTPPort: 587, SMTPTLS: "starttls",
		IMAPHost: "imap.gmx.net", IMAPPort: 993, POP3Host: "pop.gmx.net", POP3Port: 995,
	},
	// Mail.com
	"mail.com": {
		Name: "Mail.com", SMTPHost: "smtp.mail.com", SMTPPort: 587, SMTPTLS: "starttls",
		IMAPHost: "imap.mail.com", IMAPPort: 993, POP3Host: "pop.mail.com", POP3Port: 995,
	},
	// Yandex
	"yandex.com": {
		Name: "Yandex", SMTPHost: "smtp.yandex.com", SMTPPort: 587, SMTPTLS: "starttls",
		IMAPHost: "imap.yandex.com", IMAPPort: 993, POP3Host: "pop.yandex.com", POP3Port: 995,
	},
	"yandex.ru": {
		Name: "Yandex", SMTPHost: "smtp.yandex.com", SMTPPort: 587, SMTPTLS: "starttls",
		IMAPHost: "imap.yandex.com", IMAPPort: 993, POP3Host: "pop.yandex.com", POP3Port: 995,
	},
}

// knownMXPatterns maps MX record patterns to provider configs.
// Used when the domain itself isn't in knownProviders but the MX
// records point to a known provider (e.g., custom domains on Google Workspace).
var knownMXPatterns = []struct {
	Pattern  string
	Provider ProviderConfig
}{
	{"google.com", ProviderConfig{
		Name: "Google Workspace", SMTPHost: "smtp.gmail.com", SMTPPort: 587, SMTPTLS: "starttls",
		IMAPHost: "imap.gmail.com", IMAPPort: 993, POP3Host: "pop.gmail.com", POP3Port: 995,
	}},
	{"googlemail.com", ProviderConfig{
		Name: "Google Workspace", SMTPHost: "smtp.gmail.com", SMTPPort: 587, SMTPTLS: "starttls",
		IMAPHost: "imap.gmail.com", IMAPPort: 993, POP3Host: "pop.gmail.com", POP3Port: 995,
	}},
	{"outlook.com", ProviderConfig{
		Name: "Microsoft 365", SMTPHost: "smtp.office365.com", SMTPPort: 587, SMTPTLS: "starttls",
		IMAPHost: "outlook.office365.com", IMAPPort: 993, POP3Host: "outlook.office365.com", POP3Port: 995,
	}},
	{"protection.outlook.com", ProviderConfig{
		Name: "Microsoft 365", SMTPHost: "smtp.office365.com", SMTPPort: 587, SMTPTLS: "starttls",
		IMAPHost: "outlook.office365.com", IMAPPort: 993, POP3Host: "outlook.office365.com", POP3Port: 995,
	}},
	{"yahoodns.net", ProviderConfig{
		Name: "Yahoo", SMTPHost: "smtp.mail.yahoo.com", SMTPPort: 587, SMTPTLS: "starttls",
		IMAPHost: "imap.mail.yahoo.com", IMAPPort: 993, POP3Host: "pop.mail.yahoo.com", POP3Port: 995,
	}},
	{"zoho.com", ProviderConfig{
		Name: "Zoho", SMTPHost: "smtp.zoho.com", SMTPPort: 587, SMTPTLS: "starttls",
		IMAPHost: "imap.zoho.com", IMAPPort: 993, POP3Host: "pop.zoho.com", POP3Port: 995,
	}},
}

// LookupResult contains the outcome of a provider detection.
type LookupResult struct {
	Provider   string          // Human-readable provider name, or "" if unknown
	Config     *ProviderConfig // Pre-filled config if recognized, nil if unknown
	MXRecords  []string        // Raw MX records found
	Recognized bool            // Whether the provider was auto-detected
}

// LookupProvider detects the email provider from an email address.
// It checks:
// 1. Direct domain match against known providers
// 2. MX record lookup + pattern matching against known MX patterns
func LookupProvider(ctx context.Context, email string) (*LookupResult, error) {
	parts := strings.SplitN(email, "@", 2)
	if len(parts) != 2 || parts[1] == "" {
		return nil, &InvalidEmailError{Email: email}
	}
	domain := strings.ToLower(parts[1])

	// 1. Direct domain match
	if provider, ok := knownProviders[domain]; ok {
		return &LookupResult{
			Provider:   provider.Name,
			Config:     &provider,
			Recognized: true,
		}, nil
	}

	// 2. MX record lookup
	result := &LookupResult{}
	resolver := net.Resolver{}
	lookupCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	mxRecords, err := resolver.LookupMX(lookupCtx, domain)
	if err != nil {
		// DNS lookup failed — not fatal, user can configure manually
		return result, nil
	}

	for _, mx := range mxRecords {
		host := strings.ToLower(strings.TrimSuffix(mx.Host, "."))
		result.MXRecords = append(result.MXRecords, host)

		// Check MX against known patterns
		for _, pattern := range knownMXPatterns {
			if strings.HasSuffix(host, pattern.Pattern) {
				p := pattern.Provider
				result.Provider = p.Name
				result.Config = &p
				result.Recognized = true
				return result, nil
			}
		}
	}

	return result, nil
}

// InvalidEmailError is returned when an email address is malformed.
type InvalidEmailError struct {
	Email string
}

func (e *InvalidEmailError) Error() string {
	return "invalid email address: " + e.Email
}
