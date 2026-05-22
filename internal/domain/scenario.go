package domain

// Scenario describes the encounter a build is being scored against.
// Currently one target per scenario; multi-target AoE scoring is deferred.
// Future fields will include weights for the composite score, hard constraints
// ("must survive Storm Gust"), and time-budget caps; but those are
// scoring-layer concerns that haven't crystallized yet.
type Scenario struct {
	// Target is the iRO mob id the combat sim runs against (Poring=1002,
	// Eddga=1115). Zero means "no combat sim"; derived stats only. The
	// calc shim translates this iRO id to whatever id space its backend
	// uses; the shim returns a clear 4xx error if the id isn't mapped.
	Target int `json:"target,omitempty"`
}
