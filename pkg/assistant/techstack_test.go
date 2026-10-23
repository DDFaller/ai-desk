package assistant

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAnalyzeTechStack(t *testing.T) {
	// Create mock HTTP server simulating a Next.js + Tailwind + PostHog + Vercel SaaS app
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-vercel-id", "sfo1::iad1::mock")
		w.Header().Set("server", "Vercel")
		w.Header().Set("x-powered-by", "Next.js")
		w.WriteHeader(http.StatusOK)

		html := `
		<!DOCTYPE html>
		<html>
		<head>
			<link rel="stylesheet" href="/_next/static/css/tailwind.css">
			<script src="https://app.posthog.com/static/array.js"></script>
			<script src="https://js.stripe.com/v3/"></script>
		</head>
		<body>
			<div id="__NEXT_DATA__">{"props":{}}</div>
			<div class="flex items-center justify-between rounded p-4 text-white">
				<button class="btn-primary card-body">Get Started</button>
			</div>
		</body>
		</html>
		`
		_, _ = w.Write([]byte(html))
	}))
	defer server.Close()

	report, err := AnalyzeTechStack(server.URL)
	if err != nil {
		t.Fatalf("AnalyzeTechStack failed: %v", err)
	}

	if report.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", report.StatusCode)
	}

	foundTech := make(map[string]bool)
	for _, tech := range report.DetectedTech {
		foundTech[tech.Name] = true
	}

	expected := []string{
		"Vercel Edge Network",
		"Next.js (React)",
		"PostHog",
		"Stripe",
		"Tailwind CSS",
	}

	for _, exp := range expected {
		if !foundTech[exp] {
			t.Errorf("expected tech %q to be detected, detected: %v", exp, foundTech)
		}
	}

	summary := FormatReportTable(report)
	if !strings.Contains(summary, "Vercel Edge Network") {
		t.Errorf("expected summary to contain Vercel Edge Network: %s", summary)
	}
}
