package cli

import (
	"github.com/quotaguard/quotaguard/internal/config"
	"github.com/quotaguard/quotaguard/internal/router"
)

func buildRouterPolicyMap(cfg *config.RouterConfig) map[string]router.Weights {
	policies := make(map[string]router.Weights)
	if cfg == nil {
		return policies
	}

	base := router.Weights{
		Safety:      cfg.Weights.Safety,
		Refill:      cfg.Weights.Refill,
		Tier:        cfg.Weights.Tier,
		Reliability: cfg.Weights.Reliability,
		Cost:        cfg.Weights.Cost,
	}
	policies["balanced"] = base

	for _, policy := range cfg.Policies {
		policies[policy.Name] = router.Weights{
			Safety:      policy.Weights.Safety,
			Refill:      policy.Weights.Refill,
			Tier:        policy.Weights.Tier,
			Reliability: policy.Weights.Reliability,
			Cost:        policy.Weights.Cost,
		}
	}

	return policies
}
