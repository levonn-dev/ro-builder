// Class-load lock-in checks for the transcendent (trans 2nd-job) tier. See
// classes.shared.ts for the shared class lists, tier-sharding rationale, and
// check logic.
import { runClassLoadChecks, TRANSCENDENT } from "./classes.shared.ts";

runClassLoadChecks(TRANSCENDENT);
