package assistant

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
)

// TechCategory categorizes a detected technology.
type TechCategory string

const (
	CategoryHosting   TechCategory = "Hosting & CDN"
	CategoryFramework TechCategory = "Frontend Framework"
	CategoryStyling   TechCategory = "UI & Styling"
	CategoryAnalytics TechCategory = "Analytics & Telemetry"
	CategoryAuth      TechCategory = "Authentication"
	CategoryBackend   TechCategory = "Backend & BaaS"
	CategoryPayments  TechCategory = "Payments & Commerce"
	CategorySupport   TechCategory = "Chat & Support"
	CategorySecurity  TechCategory = "Security & Proxy"
)

// TechItem represents one identified component in the tech stack.
type TechItem struct {
	Name        string       `json:"name"`
	Category    TechCategory `json:"category"`
	Confidence  string       `json:"confidence"` // High, Medium
	Evidence    string       `json:"evidence"`
	Description string       `json:"description,omitempty"`
}

// TechStackReport encapsulates the 10-second reverse engineering findings.
type TechStackReport struct {
	TargetURL    string            `json:"target_url"`
	FinalURL     string            `json:"final_url"`
	StatusCode   int               `json:"status_code"`
	LatencyMS    int64             `json:"latency_ms"`
	DetectedTech []TechItem        `json:"detected_tech"`
	Headers      map[string]string `json:"headers"`
	ScannedAt    time.Time         `json:"scanned_at"`
}

// signatureRule defines a pattern to search for in headers or HTML body.
type signatureRule struct {
	name        string
	category    TechCategory
	description string
	checkHeader func(headers http.Header) (match bool, evidence string)
	checkBody   func(body string) (match bool, evidence string)
}

var techSignatures = []signatureRule{
	// --- Hosting & CDN ---
	{
		name:        "Vercel Edge Network",
		category:    CategoryHosting,
		description: "Frontend cloud platform with edge caching & serverless functions",
		checkHeader: func(h http.Header) (bool, string) {
			if id := h.Get("x-vercel-id"); id != "" {
				return true, fmt.Sprintf("Header x-vercel-id: %s", id)
			}
			if s := h.Get("server"); strings.EqualFold(s, "vercel") {
				return true, "Header Server: Vercel"
			}
			return false, ""
		},
	},
	{
		name:        "Cloudflare CDN & WAF",
		category:    CategoryHosting,
		description: "Global CDN, DDoS mitigation, and edge proxy",
		checkHeader: func(h http.Header) (bool, string) {
			if ray := h.Get("cf-ray"); ray != "" {
				return true, fmt.Sprintf("Header cf-ray: %s", ray)
			}
			if s := h.Get("server"); strings.Contains(strings.ToLower(s), "cloudflare") {
				return true, fmt.Sprintf("Header Server: %s", s)
			}
			return false, ""
		},
	},
	{
		name:        "AWS CloudFront",
		category:    CategoryHosting,
		description: "Amazon Web Services content delivery network",
		checkHeader: func(h http.Header) (bool, string) {
			if cf := h.Get("x-amz-cf-id"); cf != "" {
				return true, "Header x-amz-cf-id present"
			}
			if via := h.Get("via"); strings.Contains(strings.ToLower(via), "cloudfront") {
				return true, fmt.Sprintf("Header via: %s", via)
			}
			return false, ""
		},
	},
	{
		name:        "Fly.io",
		category:    CategoryHosting,
		description: "Global application runtime deployed near users",
		checkHeader: func(h http.Header) (bool, string) {
			if f := h.Get("fly-request-id"); f != "" {
				return true, "Header fly-request-id present"
			}
			return false, ""
		},
	},
	{
		name:        "Render",
		category:    CategoryHosting,
		description: "Unified cloud for web services and static sites",
		checkHeader: func(h http.Header) (bool, string) {
			if r := h.Get("render-id"); r != "" {
				return true, "Header render-id present"
			}
			return false, ""
		},
	},
	{
		name:        "Fastly CDN",
		category:    CategoryHosting,
		description: "Edge cloud platform and programmable CDN",
		checkHeader: func(h http.Header) (bool, string) {
			if f := h.Get("x-fastly-request-id"); f != "" {
				return true, "Header x-fastly-request-id present"
			}
			return false, ""
		},
	},

	// --- Frontend Frameworks ---
	{
		name:        "Next.js (React)",
		category:    CategoryFramework,
		description: "The React framework for the web (SSR, ISR, App Router)",
		checkHeader: func(h http.Header) (bool, string) {
			if p := h.Get("x-powered-by"); strings.Contains(strings.ToLower(p), "next.js") {
				return true, fmt.Sprintf("Header x-powered-by: %s", p)
			}
			return false, ""
		},
		checkBody: func(body string) (bool, string) {
			if strings.Contains(body, `id="__NEXT_DATA__"`) {
				return true, "Script element with id=\"__NEXT_DATA__\""
			}
			if strings.Contains(body, `/_next/static/`) {
				return true, "Asset bundle path /_next/static/"
			}
			return false, ""
		},
	},
	{
		name:        "Nuxt.js (Vue)",
		category:    CategoryFramework,
		description: "Intuitive Vue framework for hybrid server-side rendering",
		checkBody: func(body string) (bool, string) {
			if strings.Contains(body, `id="__NUXT__"`) {
				return true, "Element with id=\"__NUXT__\""
			}
			if strings.Contains(body, `/_nuxt/`) {
				return true, "Asset bundle path /_nuxt/"
			}
			return false, ""
		},
	},
	{
		name:        "SvelteKit / Svelte",
		category:    CategoryFramework,
		description: "Cybernetically enhanced web apps with zero-runtime compiler",
		checkBody: func(body string) (bool, string) {
			if strings.Contains(body, `/_app/immutable/`) {
				return true, "SvelteKit bundle path /_app/immutable/"
			}
			if strings.Contains(body, `class="svelte-`) {
				return true, "Svelte scoped CSS class svelte-*"
			}
			return false, ""
		},
	},
	{
		name:        "Remix",
		category:    CategoryFramework,
		description: "Full-stack web framework built on Web Standards",
		checkBody: func(body string) (bool, string) {
			if strings.Contains(body, `window.__remixContext`) {
				return true, "Variable window.__remixContext"
			}
			return false, ""
		},
	},
	{
		name:        "Astro",
		category:    CategoryFramework,
		description: "Content-driven web framework using Islands Architecture",
		checkBody: func(body string) (bool, string) {
			if strings.Contains(body, `data-astro-`) || strings.Contains(body, `astro-island`) {
				return true, "DOM attribute data-astro-* / astro-island"
			}
			return false, ""
		},
	},
	{
		name:        "React",
		category:    CategoryFramework,
		description: "Declarative component-based UI library",
		checkBody: func(body string) (bool, string) {
			if strings.Contains(body, `data-reactroot`) {
				return true, "Attribute data-reactroot"
			}
			return false, ""
		},
	},

	// --- UI & Styling ---
	{
		name:        "Tailwind CSS",
		category:    CategoryStyling,
		description: "Utility-first CSS framework for rapid UI development",
		checkBody: func(body string) (bool, string) {
			if strings.Contains(body, "tailwindcss") || strings.Contains(body, "tailwind.css") {
				return true, "Tailwind CSS stylesheet reference"
			}
			// Search for multiple utility classes in sequence
			re := regexp.MustCompile(`class="[^"]*(?:flex|grid)[^"]*(?:items-center|justify-between)[^"]*(?:rounded|p-|m-|text-)[^"]*"`)
			if re.MatchString(body) {
				return true, "High-density Tailwind utility class signatures"
			}
			return false, ""
		},
	},
	{
		name:        "Radix UI / shadcn/ui",
		category:    CategoryStyling,
		description: "Accessible, unstyled UI primitives and component library",
		checkBody: func(body string) (bool, string) {
			if strings.Contains(body, "data-radix-") || strings.Contains(body, `data-state="`) {
				return true, "Radix state attribute data-radix-* / data-state"
			}
			return false, ""
		},
	},
	{
		name:        "DaisyUI",
		category:    CategoryStyling,
		description: "Tailwind CSS component library",
		checkBody: func(body string) (bool, string) {
			if strings.Contains(body, "btn-primary") && (strings.Contains(body, "badge-") || strings.Contains(body, "card-body")) {
				return true, "DaisyUI component classes (btn-primary, card-body)"
			}
			return false, ""
		},
	},

	// --- Analytics & Telemetry ---
	{
		name:        "PostHog",
		category:    CategoryAnalytics,
		description: "Product analytics, session recording, and feature flags",
		checkBody: func(body string) (bool, string) {
			if strings.Contains(body, "posthog.com") || strings.Contains(body, "posthog.js") || strings.Contains(body, "ph_") {
				return true, "PostHog analytics script / endpoint reference"
			}
			return false, ""
		},
	},
	{
		name:        "Google Analytics / GTM",
		category:    CategoryAnalytics,
		description: "Google Tag Manager & GA4 web analytics",
		checkBody: func(body string) (bool, string) {
			if strings.Contains(body, "googletagmanager.com/gtm.js") || strings.Contains(body, "google-analytics.com") || strings.Contains(body, "gtag(") {
				return true, "Google Tag Manager / gtag script"
			}
			return false, ""
		},
	},
	{
		name:        "Segment",
		category:    CategoryAnalytics,
		description: "Customer data platform (CDP) for event streaming",
		checkBody: func(body string) (bool, string) {
			if strings.Contains(body, "cdn.segment.com/analytics.js") {
				return true, "Segment analytics.js script"
			}
			return false, ""
		},
	},
	{
		name:        "Mixpanel",
		category:    CategoryAnalytics,
		description: "Self-serve product analytics for web and mobile",
		checkBody: func(body string) (bool, string) {
			if strings.Contains(body, "cdn.mxpnl.com") || strings.Contains(body, "mixpanel.com") {
				return true, "Mixpanel SDK reference"
			}
			return false, ""
		},
	},
	{
		name:        "Sentry",
		category:    CategoryAnalytics,
		description: "Application performance monitoring & error tracking",
		checkBody: func(body string) (bool, string) {
			if strings.Contains(body, "browser.sentry-cdn.com") || strings.Contains(body, "sentry.io") {
				return true, "Sentry browser telemetry SDK"
			}
			return false, ""
		},
	},
	{
		name:        "Datadog RUM",
		category:    CategoryAnalytics,
		description: "Real User Monitoring & client-side observability",
		checkBody: func(body string) (bool, string) {
			if strings.Contains(body, "datadoghq-browser-agent") || strings.Contains(body, "datadoghq.com") {
				return true, "Datadog browser agent"
			}
			return false, ""
		},
	},

	// --- Authentication & BaaS ---
	{
		name:        "Clerk",
		category:    CategoryAuth,
		description: "Authentication and user management for Next.js and React",
		checkBody: func(body string) (bool, string) {
			if strings.Contains(body, "clerk.com") || strings.Contains(body, "clerk.browser") || strings.Contains(body, "clerk-js") {
				return true, "Clerk SDK endpoint reference"
			}
			return false, ""
		},
	},
	{
		name:        "Supabase",
		category:    CategoryBackend,
		description: "Open source Firebase alternative (Postgres, Auth, Storage)",
		checkBody: func(body string) (bool, string) {
			if strings.Contains(body, "supabase.co") || strings.Contains(body, "@supabase/supabase-js") {
				return true, "Supabase API domain or client SDK"
			}
			return false, ""
		},
	},
	{
		name:        "Auth0 by Okta",
		category:    CategoryAuth,
		description: "Identity platform for authentication and authorization",
		checkBody: func(body string) (bool, string) {
			if strings.Contains(body, "auth0.com") || strings.Contains(body, "auth0-js") {
				return true, "Auth0 tenant domain / SDK"
			}
			return false, ""
		},
	},

	// --- Payments & Commerce ---
	{
		name:        "Stripe",
		category:    CategoryPayments,
		description: "Financial infrastructure and payment processing",
		checkBody: func(body string) (bool, string) {
			if strings.Contains(body, "js.stripe.com/v3") || strings.Contains(body, "stripe.com") {
				return true, "Stripe.js integration script"
			}
			return false, ""
		},
	},
	{
		name:        "Lemon Squeezy",
		category:    CategoryPayments,
		description: "Merchant of record for SaaS and digital products",
		checkBody: func(body string) (bool, string) {
			if strings.Contains(body, "lemonsqueezy.com") || strings.Contains(body, "lmsqueezy.com") {
				return true, "Lemon Squeezy checkout / script"
			}
			return false, ""
		},
	},

	// --- Chat & Customer Support ---
	{
		name:        "Intercom",
		category:    CategorySupport,
		description: "Customer service platform and conversational messenger",
		checkBody: func(body string) (bool, string) {
			if strings.Contains(body, "widget.intercom.io") || strings.Contains(body, "intercomSettings") {
				return true, "Intercom widget script / config"
			}
			return false, ""
		},
	},
	{
		name:        "Crisp",
		category:    CategorySupport,
		description: "All-in-one business messaging platform",
		checkBody: func(body string) (bool, string) {
			if strings.Contains(body, "client.crisp.chat") || strings.Contains(body, "$crisp") {
				return true, "Crisp chat widget script"
			}
			return false, ""
		},
	},
}

// AnalyzeTechStack fetches a SaaS URL and reverse engineers its tech stack in under 10 seconds.
func AnalyzeTechStack(targetURL string) (*TechStackReport, error) {
	parsed, err := url.Parse(targetURL)
	if err != nil || parsed.Scheme == "" {
		if !strings.HasPrefix(targetURL, "http://") && !strings.HasPrefix(targetURL, "https://") {
			targetURL = "https://" + targetURL
		}
	}

	req, err := http.NewRequestWithContext(context.Background(), "GET", targetURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	// Browser-like User-Agent to prevent bot-blocking
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/133.0.0.0 Safari/537.36 AI-Desk/1.0")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	client := &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("stopped after 5 redirects")
			}
			return nil
		},
	}

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http fetch failed: %w", err)
	}
	defer resp.Body.Close()

	latency := time.Since(start).Milliseconds()

	// Read first 512KB of body for fast scanning
	lr := io.LimitReader(resp.Body, 512*1024)
	bodyBytes, _ := io.ReadAll(lr)
	bodyStr := string(bodyBytes)

	headersMap := make(map[string]string)
	for k := range resp.Header {
		headersMap[k] = resp.Header.Get(k)
	}

	report := &TechStackReport{
		TargetURL:  targetURL,
		FinalURL:   resp.Request.URL.String(),
		StatusCode: resp.StatusCode,
		LatencyMS:  latency,
		Headers:    headersMap,
		ScannedAt:  time.Now(),
	}

	detectedMap := make(map[string]TechItem)

	for _, rule := range techSignatures {
		if rule.checkHeader != nil {
			if ok, ev := rule.checkHeader(resp.Header); ok {
				detectedMap[rule.name] = TechItem{
					Name:        rule.name,
					Category:    rule.category,
					Confidence:  "High",
					Evidence:    ev,
					Description: rule.description,
				}
				continue
			}
		}
		if rule.checkBody != nil {
			if ok, ev := rule.checkBody(bodyStr); ok {
				detectedMap[rule.name] = TechItem{
					Name:        rule.name,
					Category:    rule.category,
					Confidence:  "High",
					Evidence:    ev,
					Description: rule.description,
				}
			}
		}
	}

	// Sort detected items by Category then Name
	for _, item := range detectedMap {
		report.DetectedTech = append(report.DetectedTech, item)
	}
	sort.Slice(report.DetectedTech, func(i, j int) bool {
		if report.DetectedTech[i].Category != report.DetectedTech[j].Category {
			return report.DetectedTech[i].Category < report.DetectedTech[j].Category
		}
		return report.DetectedTech[i].Name < report.DetectedTech[j].Name
	})

	return report, nil
}

// FormatReportTable formats the detection results into an executive terminal summary.
func FormatReportTable(report *TechStackReport) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("\n\033[1;36m=== SaaS Tech Stack Inspector ===\033[0m\n"))
	sb.WriteString(fmt.Sprintf("  • Target:   \033[1m%s\033[0m\n", report.TargetURL))
	if report.FinalURL != report.TargetURL {
		sb.WriteString(fmt.Sprintf("  • Resolved: %s\n", report.FinalURL))
	}
	sb.WriteString(fmt.Sprintf("  • Status:   HTTP %d (%d ms)\n", report.StatusCode, report.LatencyMS))
	sb.WriteString(fmt.Sprintf("  • Detected: %d components\n\n", len(report.DetectedTech)))

	if len(report.DetectedTech) == 0 {
		sb.WriteString("  No known SaaS signatures detected in headers or landing page markup.\n")
		return sb.String()
	}

	byCat := make(map[TechCategory][]TechItem)
	for _, item := range report.DetectedTech {
		byCat[item.Category] = append(byCat[item.Category], item)
	}

	// Order categories
	catOrder := []TechCategory{
		CategoryHosting,
		CategoryFramework,
		CategoryStyling,
		CategoryAnalytics,
		CategoryAuth,
		CategoryBackend,
		CategoryPayments,
		CategorySupport,
		CategorySecurity,
	}

	for _, cat := range catOrder {
		items := byCat[cat]
		if len(items) == 0 {
			continue
		}
		sb.WriteString(fmt.Sprintf("\033[1;35m[%s]\033[0m\n", cat))
		for _, item := range items {
			sb.WriteString(fmt.Sprintf("  • \033[1;32m%-22s\033[0m %-40s\n", item.Name, item.Description))
			sb.WriteString(fmt.Sprintf("    \033[2mEvidence: %s\033[0m\n", item.Evidence))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}
