package gates

import "fmt"

// evaluateStatusImmunity emits one Result per uncovered relevant
// status the target applies. Severity is fail or warn depending on
// the priority bucket; statuses outside both buckets are skipped
// entirely (they exist on the mob but the gate doesn't care for this
// scenario type; e.g. blind is a nice-to-have on a PvM target, not
// a fail).
//
// Stat-derived immunity (100 VIT for stun, 100 INT for sleep) and
// equipment-tag immunity (Marc → frozen, etc.) both count as coverage.
// See buildCovers.
func evaluateStatusImmunity(in Inputs) []Result {
	threats := targetThreats(in)
	if threats == nil || len(threats.AppliesStatus) == 0 {
		return nil
	}
	qg := resolveQualityGates(in.Profile)
	scenarioType := deriveScenarioType(in)
	prio := resolveStatusPriority(qg, scenarioType, in.Overrides)

	failSet := stringSet(prio.Fail)
	warnSet := stringSet(prio.Warn)

	var results []Result
	for _, status := range threats.AppliesStatus {
		_, isFail := failSet[status]
		_, isWarn := warnSet[status]
		if !isFail && !isWarn {
			// Outside the priority list for this scenario type; skip.
			continue
		}
		if buildCovers(in, status) {
			results = append(results, Result{
				Name:     "status_immunity_" + status,
				Severity: SeverityPass,
				Actual:   "covered",
				Reason:   fmt.Sprintf("build covers %s (stats or equipped item grants immunity)", status),
			})
			continue
		}
		sev := SeverityFail
		if !isFail {
			sev = SeverityWarn
		}
		results = append(results, Result{
			Name:      "status_immunity_" + status,
			Threshold: status,
			Actual:    "uncovered",
			Severity:  sev,
			Reason: fmt.Sprintf("target applies %s but build has no immunity (need %s)",
				status, immunityHowTo(status)),
		})
	}
	return results
}

func stringSet(xs []string) map[string]struct{} {
	m := make(map[string]struct{}, len(xs))
	for _, s := range xs {
		m[s] = struct{}{}
	}
	return m
}

// immunityHowTo returns a one-line hint describing how the build can
// pick up this immunity. Surfaced in the gate Reason so the LLM gets
// concrete remediation advice.
func immunityHowTo(status string) string {
	switch status {
	case "stun":
		return "100 VIT or 97 VIT + 15 LUK or stun-immune card (Marduk-equivalent)"
	case "sleep":
		return "100 INT or Nightmare card"
	case "frozen":
		return "Marc card / Pasana / Bathory / Evil Druid card / fire armor"
	case "silence":
		return "Marduk card"
	case "stone_curse":
		return "Evil Druid card"
	case "curse":
		return "curse-resist gear or LUK reduces chance"
	case "blind":
		return "Pumpkin Pie / Schwartzwald Pine Jubilee / blind-resist gear"
	case "poison":
		return "Argiope card / poison-immune gear"
	case "confusion":
		return "Giearth card"
	}
	return "status-immune card or stat threshold"
}
