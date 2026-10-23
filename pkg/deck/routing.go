package deck

import (
	"fmt"
	"github.com/falluba/ai-desk/pkg/harness"
	"github.com/falluba/ai-desk/pkg/rotation"
	"strings"
)

// RouteAccount is shared by task launch and the CLI shim.
func RouteAccount(tool, profile, requested string) (string, error) {
	cfg, err := rotation.LoadAccountProfiles("")
	if err != nil {
		return "", err
	}
	if profile == "" {
		profile = cfg.ActiveProfile
	}
	p, ok := cfg.Profiles[profile]
	if !ok {
		return "", fmt.Errorf("unknown account profile %q", profile)
	}
	if len(p.Accounts) == 0 && (requested == "" || requested == "default") {
		return "default", nil
	}
	accounts, err := harness.ListAccounts(tool)
	if err != nil {
		return "", err
	}
	candidates := cfg.FilterAccounts(profile, accounts)
	if requested != "" && requested != "default" {
		for _, a := range candidates {
			if strings.EqualFold(a.ID, requested) || strings.EqualFold(a.Email, requested) {
				return a.ID, nil
			}
		}
		return "", fmt.Errorf("account %q is unavailable for %s in profile %q", requested, tool, profile)
	}
	chosen, err := rotation.NewEngine(nil).SelectNext(candidates, p.Policy, "")
	if err != nil {
		return "", err
	}
	return chosen.ID, nil
}
